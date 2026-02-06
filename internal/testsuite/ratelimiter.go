package testsuite

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/maybedont/maybe-dont/internal/gateway"
)

// RateLimiter tracks and enforces rate limits per provider.
type RateLimiter struct {
	mu sync.Mutex

	// Per-provider rate limits (requests per minute)
	providerLimits map[string]int

	// Default limit for unlisted providers
	defaultLimit int

	// Minimum delay between requests (ms)
	delayBetweenRequestsMs int

	// Buffer to add after hitting rate limit (ms)
	rateLimitBufferMs int

	// Per-provider request tracking
	providerRequests map[string][]time.Time

	// Whether to wait on rate limit (--wait flag)
	waitOnLimit bool

	// Output for progress messages
	output io.Writer

	// Whether output is a TTY (for progress bars)
	isTTY bool

	// Provider stopped due to rate limit (429)
	providerStopped map[string]bool

	// Learned limits from response headers (per-provider)
	learnedLimits map[string]*LearnedLimits
}

// LearnedLimits tracks rate limits learned from provider response headers.
type LearnedLimits struct {
	// Request limits
	RequestsLimit     int
	RequestsRemaining int
	RequestsReset     time.Time

	// Token limits
	TokensLimit     int
	TokensRemaining int
	TokensReset     time.Time

	// Last update time
	LastUpdated time.Time
}

// RateLimiterConfig configures the rate limiter.
type RateLimiterConfig struct {
	// Per-provider limits from suite.yaml
	ProviderLimits map[string]ProviderRateLimit

	// Default limit for unlisted providers
	DefaultLimit int

	// Minimum delay between requests
	DelayBetweenRequestsMs int

	// Buffer after rate limit window
	RateLimitBufferMs int

	// Override all limits (from --rpm flag)
	OverrideRPM int

	// Wait on rate limit instead of failing
	WaitOnLimit bool

	// Output writer for progress (nil = os.Stdout)
	Output io.Writer
}

// NewRateLimiter creates a new rate limiter with the given configuration.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	rl := &RateLimiter{
		providerLimits:         make(map[string]int),
		providerRequests:       make(map[string][]time.Time),
		providerStopped:        make(map[string]bool),
		learnedLimits:          make(map[string]*LearnedLimits),
		defaultLimit:           DefaultRequestsPerMinute,
		delayBetweenRequestsMs: DefaultDelayBetweenRequestsMs,
		rateLimitBufferMs:      DefaultRateLimitBufferMs,
		waitOnLimit:            cfg.WaitOnLimit,
		output:                 cfg.Output,
	}

	if rl.output == nil {
		rl.output = os.Stdout
	}

	// Check if output is a TTY
	if f, ok := rl.output.(*os.File); ok {
		stat, _ := f.Stat()
		rl.isTTY = (stat.Mode() & os.ModeCharDevice) != 0
	}

	// Apply configuration
	if cfg.DefaultLimit > 0 {
		rl.defaultLimit = cfg.DefaultLimit
	}
	if cfg.DelayBetweenRequestsMs > 0 {
		rl.delayBetweenRequestsMs = cfg.DelayBetweenRequestsMs
	}
	if cfg.RateLimitBufferMs > 0 {
		rl.rateLimitBufferMs = cfg.RateLimitBufferMs
	}

	// Copy provider limits
	for provider, limit := range cfg.ProviderLimits {
		rl.providerLimits[provider] = limit.RequestsPerMinute
	}

	// Apply CLI override if specified
	if cfg.OverrideRPM > 0 {
		rl.defaultLimit = cfg.OverrideRPM
		// Override all provider limits
		for provider := range rl.providerLimits {
			rl.providerLimits[provider] = cfg.OverrideRPM
		}
	}

	return rl
}

// GetLimit returns the rate limit for a provider.
func (rl *RateLimiter) GetLimit(provider string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limit, ok := rl.providerLimits[provider]; ok {
		return limit
	}
	return rl.defaultLimit
}

// IsProviderStopped returns true if the provider was stopped due to 429.
func (rl *RateLimiter) IsProviderStopped(provider string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.providerStopped[provider]
}

// StopProvider marks a provider as stopped due to rate limiting.
func (rl *RateLimiter) StopProvider(provider string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.providerStopped[provider] = true
}

// WaitBeforeRequest waits as needed before making a request to respect rate limits.
// Returns nil if the request can proceed, or an error if the provider is stopped.
// If ctx is cancelled, returns ctx.Err().
func (rl *RateLimiter) WaitBeforeRequest(ctx context.Context, provider string) error {
	rl.mu.Lock()

	// Check if provider is stopped
	if rl.providerStopped[provider] {
		rl.mu.Unlock()
		return fmt.Errorf("provider %s stopped due to rate limiting", provider)
	}

	limit := rl.defaultLimit
	if l, ok := rl.providerLimits[provider]; ok {
		limit = l
	}

	// Clean old requests (older than 1 minute)
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	var recent []time.Time
	for _, t := range rl.providerRequests[provider] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	rl.providerRequests[provider] = recent

	// Calculate wait time
	var waitDuration time.Duration

	// Check if we've hit the per-minute limit
	if len(recent) >= limit {
		// Need to wait until oldest request falls outside window
		oldestInWindow := recent[0]
		waitUntil := oldestInWindow.Add(time.Minute)
		waitDuration = time.Until(waitUntil)

		if waitDuration > 0 {
			if !rl.waitOnLimit {
				// Without --wait, stop the provider
				rl.providerStopped[provider] = true
				rl.mu.Unlock()
				return fmt.Errorf("rate limit reached for provider %s (limit: %d/min)", provider, limit)
			}

			// With --wait, we'll wait and show progress
			rl.mu.Unlock()
			return rl.waitWithProgress(ctx, provider, waitDuration)
		}
	}

	// Apply minimum delay between requests
	if len(recent) > 0 {
		lastRequest := recent[len(recent)-1]
		minDelay := time.Duration(rl.delayBetweenRequestsMs) * time.Millisecond
		timeSinceLast := now.Sub(lastRequest)
		if timeSinceLast < minDelay {
			waitDuration = minDelay - timeSinceLast
		}
	}

	rl.mu.Unlock()

	// Wait if needed
	if waitDuration > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
		}
	}

	return nil
}

// RecordRequest records that a request was made to a provider.
func (rl *RateLimiter) RecordRequest(provider string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.providerRequests[provider] = append(rl.providerRequests[provider], time.Now())
}

// UpdateFromResponse updates rate limit tracking from API response headers.
// This enables dynamic rate limiting based on actual provider limits.
func (rl *RateLimiter) UpdateFromResponse(info *gateway.RateLimitInfo) {
	if info == nil {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	learned := rl.learnedLimits[info.Provider]
	if learned == nil {
		learned = &LearnedLimits{}
		rl.learnedLimits[info.Provider] = learned
	}

	// Update with learned values (only if non-zero)
	if info.RequestsLimit > 0 {
		learned.RequestsLimit = info.RequestsLimit
	}
	if info.RequestsRemaining >= 0 {
		learned.RequestsRemaining = info.RequestsRemaining
	}
	if !info.RequestsReset.IsZero() {
		learned.RequestsReset = info.RequestsReset
	}

	if info.TokensLimit > 0 {
		learned.TokensLimit = info.TokensLimit
	}
	if info.TokensRemaining >= 0 {
		learned.TokensRemaining = info.TokensRemaining
	}
	if !info.TokensReset.IsZero() {
		learned.TokensReset = info.TokensReset
	}

	learned.LastUpdated = time.Now()
}

// GetLearnedLimits returns the learned limits for a provider, if any.
func (rl *RateLimiter) GetLearnedLimits(provider string) *LearnedLimits {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.learnedLimits[provider]
}

// Handle429 handles a 429 response from a provider.
// If --wait is set, waits for the rate limit window to reset plus buffer.
// Otherwise, stops the provider.
// Deprecated: Use Handle429WithInfo for precise wait times from headers.
func (rl *RateLimiter) Handle429(ctx context.Context, provider string) error {
	return rl.Handle429WithInfo(ctx, provider, nil)
}

// Handle429WithInfo handles a 429 response, using RateLimitInfo for precise wait times.
// If info contains RetryAfter, uses that duration instead of guessing.
func (rl *RateLimiter) Handle429WithInfo(ctx context.Context, provider string, info *gateway.RateLimitInfo) error {
	rl.mu.Lock()
	if !rl.waitOnLimit {
		rl.providerStopped[provider] = true
		rl.mu.Unlock()
		return fmt.Errorf("received 429 from provider %s", provider)
	}
	rl.mu.Unlock()

	// Determine wait duration from headers or fallback to default
	var waitDuration time.Duration
	var waitSource string

	if info != nil && info.RetryAfter > 0 {
		// Use precise retry-after from provider
		waitDuration = info.RetryAfter + time.Duration(rl.rateLimitBufferMs)*time.Millisecond
		waitSource = "retry-after header"
	} else if info != nil && !info.RequestsReset.IsZero() {
		// Calculate from reset timestamp
		waitDuration = time.Until(info.RequestsReset) + time.Duration(rl.rateLimitBufferMs)*time.Millisecond
		waitSource = "reset timestamp"
	} else {
		// Fallback to configured wait (60s + buffer)
		waitDuration = time.Minute + time.Duration(rl.rateLimitBufferMs)*time.Millisecond
		waitSource = "default"
	}

	return rl.waitWithProgressAndInfo(ctx, provider, waitDuration, waitSource, info)
}

// waitWithProgress waits for the given duration, showing progress if output is TTY.
func (rl *RateLimiter) waitWithProgress(ctx context.Context, provider string, duration time.Duration) error {
	return rl.waitWithProgressAndInfo(ctx, provider, duration, "", nil)
}

// waitWithProgressAndInfo waits with detailed rate limit info shown.
func (rl *RateLimiter) waitWithProgressAndInfo(ctx context.Context, provider string, duration time.Duration, waitSource string, info *gateway.RateLimitInfo) error {
	if duration <= 0 {
		return nil
	}

	startTime := time.Now()
	endTime := startTime.Add(duration)

	// Print initial message with rate limit details
	if rl.isTTY {
		_, _ = fmt.Fprintf(rl.output, "\n⏳ Rate limit reached for %s\n", provider)
		if info != nil {
			if info.RequestsLimit > 0 {
				_, _ = fmt.Fprintf(rl.output, "   Requests: %d/%d remaining\n", info.RequestsRemaining, info.RequestsLimit)
			}
			if info.TokensLimit > 0 {
				_, _ = fmt.Fprintf(rl.output, "   Tokens: %d/%d remaining\n", info.TokensRemaining, info.TokensLimit)
			}
		}
		if waitSource != "" {
			_, _ = fmt.Fprintf(rl.output, "   Waiting %v (from %s)...\n", duration.Round(time.Second), waitSource)
		}
	} else {
		msg := fmt.Sprintf("[%s] Rate limit reached for %s", time.Now().Format("15:04:05"), provider)
		if info != nil && info.RequestsLimit > 0 {
			msg += fmt.Sprintf(" | Requests: %d/%d", info.RequestsRemaining, info.RequestsLimit)
		}
		if info != nil && info.TokensLimit > 0 {
			msg += fmt.Sprintf(" | Tokens: %d/%d", info.TokensRemaining, info.TokensLimit)
		}
		if waitSource != "" {
			msg += fmt.Sprintf(" | Waiting %v (%s)", duration.Round(time.Second), waitSource)
		} else {
			msg += fmt.Sprintf(" | Waiting %v", duration.Round(time.Second))
		}
		_, _ = fmt.Fprintf(rl.output, "%s\n", msg)
	}

	// Update progress periodically
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if rl.isTTY {
				_, _ = fmt.Fprintf(rl.output, "\r\033[K") // Clear line
			}
			return ctx.Err()
		case <-ticker.C:
			remaining := time.Until(endTime)
			if remaining <= 0 {
				if rl.isTTY {
					_, _ = fmt.Fprintf(rl.output, "\r\033[K") // Clear line
					_, _ = fmt.Fprintf(rl.output, "✓ Resuming after rate limit pause\n\n")
				} else {
					_, _ = fmt.Fprintf(rl.output, "[%s] Resuming after rate limit pause\n",
						time.Now().Format("15:04:05"))
				}
				return nil
			}

			if rl.isTTY {
				// Progress bar - single line that updates in place
				elapsed := time.Since(startTime)
				progress := float64(elapsed) / float64(duration)
				barWidth := 30
				filled := int(progress * float64(barWidth))
				// \r returns to start of line, \033[K clears to end of line
				_, _ = fmt.Fprintf(rl.output, "\r   [%s%s] %ds remaining\033[K",
					repeatChar('=', filled)+">",
					repeatChar(' ', barWidth-filled-1),
					int(remaining.Seconds()))
			}
		}
	}
}

// repeatChar returns a string of n copies of the given character.
func repeatChar(c byte, n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
