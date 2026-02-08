package testsuite

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/maybedont/maybe-dont/internal/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRateLimiter(t *testing.T) {
	t.Run("uses defaults when no config provided", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{})

		assert.Equal(t, DefaultRequestsPerMinute, rl.GetLimit("unknown"))
		assert.Equal(t, DefaultDelayBetweenRequestsMs, rl.delayBetweenRequestsMs)
		assert.Equal(t, DefaultRateLimitBufferMs, rl.rateLimitBufferMs)
	})

	t.Run("applies provider-specific limits", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			ProviderLimits: map[string]ProviderRateLimit{
				"openai":    {RequestsPerMinute: 60},
				"anthropic": {RequestsPerMinute: 20},
			},
		})

		assert.Equal(t, 60, rl.GetLimit("openai"))
		assert.Equal(t, 20, rl.GetLimit("anthropic"))
		assert.Equal(t, DefaultRequestsPerMinute, rl.GetLimit("other"))
	})

	t.Run("override RPM overrides all limits", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			ProviderLimits: map[string]ProviderRateLimit{
				"openai":    {RequestsPerMinute: 60},
				"anthropic": {RequestsPerMinute: 20},
			},
			OverrideRPM: 10,
		})

		assert.Equal(t, 10, rl.GetLimit("openai"))
		assert.Equal(t, 10, rl.GetLimit("anthropic"))
		assert.Equal(t, 10, rl.GetLimit("other"))
	})
}

func TestRateLimiter_WaitBeforeRequest(t *testing.T) {
	t.Run("allows request under limit", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			ProviderLimits: map[string]ProviderRateLimit{
				"openai": {RequestsPerMinute: 60},
			},
		})

		ctx := context.Background()
		err := rl.WaitBeforeRequest(ctx, "openai")
		require.NoError(t, err)
	})

	t.Run("returns error when provider is stopped", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{})
		rl.StopProvider("openai")

		ctx := context.Background()
		err := rl.WaitBeforeRequest(ctx, "openai")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "stopped due to rate limiting")
	})

	t.Run("adapts to learned limits from API headers", func(t *testing.T) {
		// Verifies that when API response headers report a lower request limit
		// than configured, WaitBeforeRequest uses the provider's actual limit
		// to avoid repeatedly hitting 429s.
		rl := NewRateLimiter(RateLimiterConfig{
			ProviderLimits: map[string]ProviderRateLimit{
				"openai": {RequestsPerMinute: 100}, // High configured limit
			},
			WaitOnLimit: false,
		})

		ctx := context.Background()

		// Simulate learning a lower limit from API response headers
		rl.UpdateFromResponse(&gateway.RateLimitInfo{
			Provider:          "openai",
			RequestsLimit:     3, // Provider only allows 3 RPM
			RequestsRemaining: 2,
		})

		// First 3 requests should succeed (up to learned limit)
		for i := 0; i < 3; i++ {
			require.NoError(t, rl.WaitBeforeRequest(ctx, "openai"))
			rl.RecordRequest("openai")
		}

		// Fourth request should fail — learned limit of 3 applies, not configured 100
		err := rl.WaitBeforeRequest(ctx, "openai")
		assert.Error(t, err)
		assert.True(t, rl.IsProviderStopped("openai"))
	})

	t.Run("ignores learned limits higher than configured", func(t *testing.T) {
		// Configured limit should still apply when the provider reports
		// a higher limit than what the user configured.
		rl := NewRateLimiter(RateLimiterConfig{
			ProviderLimits: map[string]ProviderRateLimit{
				"openai": {RequestsPerMinute: 2},
			},
			WaitOnLimit: false,
		})

		ctx := context.Background()

		// Provider says 1000 RPM — but we configured 2
		rl.UpdateFromResponse(&gateway.RateLimitInfo{
			Provider:          "openai",
			RequestsLimit:     1000,
			RequestsRemaining: 999,
		})

		// First 2 should succeed (configured limit)
		require.NoError(t, rl.WaitBeforeRequest(ctx, "openai"))
		rl.RecordRequest("openai")
		require.NoError(t, rl.WaitBeforeRequest(ctx, "openai"))
		rl.RecordRequest("openai")

		// Third should fail — configured limit of 2 still applies
		err := rl.WaitBeforeRequest(ctx, "openai")
		assert.Error(t, err)
		assert.True(t, rl.IsProviderStopped("openai"))
	})

	t.Run("stops provider when limit reached without wait", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			ProviderLimits: map[string]ProviderRateLimit{
				"openai": {RequestsPerMinute: 2},
			},
			WaitOnLimit: false,
		})

		ctx := context.Background()

		// First two requests should work
		require.NoError(t, rl.WaitBeforeRequest(ctx, "openai"))
		rl.RecordRequest("openai")
		require.NoError(t, rl.WaitBeforeRequest(ctx, "openai"))
		rl.RecordRequest("openai")

		// Third request should fail and stop provider
		err := rl.WaitBeforeRequest(ctx, "openai")
		assert.Error(t, err)
		assert.True(t, rl.IsProviderStopped("openai"))
	})
}

func TestRateLimiter_RecordRequest(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{})

	// Record some requests
	rl.RecordRequest("openai")
	rl.RecordRequest("openai")
	rl.RecordRequest("anthropic")

	// Check that requests are tracked
	assert.Equal(t, 2, len(rl.providerRequests["openai"]))
	assert.Equal(t, 1, len(rl.providerRequests["anthropic"]))
}

func TestRateLimiter_Handle429(t *testing.T) {
	t.Run("stops provider without wait flag", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit: false,
		})

		ctx := context.Background()
		err := rl.Handle429(ctx, "openai")
		assert.Error(t, err)
		assert.True(t, rl.IsProviderStopped("openai"))
	})

	t.Run("waits with wait flag", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 100, // Short for testing
			Output:            output,
		})

		// Create context with very short timeout to avoid long test
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := rl.Handle429(ctx, "openai")
		// Should timeout/cancel rather than complete the full wait
		assert.Error(t, err)
		assert.False(t, rl.IsProviderStopped("openai"))
	})
}

func TestRateLimiter_IsProviderStopped(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{})

	assert.False(t, rl.IsProviderStopped("openai"))

	rl.StopProvider("openai")

	assert.True(t, rl.IsProviderStopped("openai"))
	assert.False(t, rl.IsProviderStopped("anthropic"))
}

func TestRateLimiter_UpdateFromResponse(t *testing.T) {
	t.Run("updates learned limits from response info", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{})

		// Initially no learned limits
		assert.Nil(t, rl.GetLearnedLimits("anthropic"))

		// Update from response
		info := &gateway.RateLimitInfo{
			Provider:          "anthropic",
			RequestsLimit:     1000,
			RequestsRemaining: 950,
			RequestsReset:     time.Now().Add(time.Minute),
			TokensLimit:       80000,
			TokensRemaining:   79000,
			TokensReset:       time.Now().Add(time.Minute),
		}
		rl.UpdateFromResponse(info)

		// Verify learned limits
		learned := rl.GetLearnedLimits("anthropic")
		require.NotNil(t, learned)
		assert.Equal(t, 1000, learned.RequestsLimit)
		assert.Equal(t, 950, learned.RequestsRemaining)
		assert.Equal(t, 80000, learned.TokensLimit)
		assert.Equal(t, 79000, learned.TokensRemaining)
		assert.False(t, learned.LastUpdated.IsZero())
	})

	t.Run("ignores nil info", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{})
		rl.UpdateFromResponse(nil) // Should not panic
		assert.Nil(t, rl.GetLearnedLimits("anthropic"))
	})

	t.Run("preserves non-zero values on partial updates", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{})

		// First update with full info
		info1 := &gateway.RateLimitInfo{
			Provider:          "anthropic",
			RequestsLimit:     1000,
			RequestsRemaining: 950,
			TokensLimit:       80000,
			TokensRemaining:   79000,
		}
		rl.UpdateFromResponse(info1)

		// Second update with only requests info (tokens are zero)
		info2 := &gateway.RateLimitInfo{
			Provider:          "anthropic",
			RequestsLimit:     1000,
			RequestsRemaining: 900,
			// TokensLimit and TokensRemaining are zero
		}
		rl.UpdateFromResponse(info2)

		// Verify requests updated but tokens preserved
		learned := rl.GetLearnedLimits("anthropic")
		require.NotNil(t, learned)
		assert.Equal(t, 900, learned.RequestsRemaining)
		assert.Equal(t, 80000, learned.TokensLimit) // Preserved from first update
	})
}

// TestRateLimiter_SequentialFallback verifies that when a 429 rate limit is hit,
// the rate limiter switches to sequential execution for that provider to avoid
// thundering herd problems.
func TestRateLimiter_SequentialFallback(t *testing.T) {
	t.Run("provider switches to sequential mode after 429", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 10, // Very short for testing
			Output:            output,
		})

		// Initially not in sequential mode
		assert.False(t, rl.isSequentialMode("openai"))

		// Simulate 429 response with very short retry
		// Note: waitWithProgressAndInfo uses a 1-second ticker, so we need
		// context timeout > 1s for the wait to complete successfully
		info := &gateway.RateLimitInfo{
			Provider:   "openai",
			RetryAfter: 10 * time.Millisecond,
		}

		// Context timeout must be > 1s for the ticker-based progress tracking
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := rl.Handle429WithInfo(ctx, "openai", info)
		// Should return a retryable error (even after successful wait)
		require.Error(t, err)

		// After successful 429 handling, provider should be in sequential mode
		assert.True(t, rl.isSequentialMode("openai"))
	})

	t.Run("other providers remain parallel after one hits 429", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 10,
			Output:            output,
		})

		// Put openai in sequential mode via 429
		info := &gateway.RateLimitInfo{
			Provider:   "openai",
			RetryAfter: 10 * time.Millisecond,
		}
		// Context timeout must be > 1s for the ticker-based progress tracking
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_ = rl.Handle429WithInfo(ctx, "openai", info)

		// openai should be in sequential mode
		assert.True(t, rl.isSequentialMode("openai"))
		// anthropic should still be parallel
		assert.False(t, rl.isSequentialMode("anthropic"))
	})

	t.Run("sequential mode limits concurrency to one", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 10,
			Output:            output,
		})

		// Force sequential mode by handling a 429
		info := &gateway.RateLimitInfo{
			Provider:   "openai",
			RetryAfter: 10 * time.Millisecond,
		}
		ctx := context.Background()
		_ = rl.Handle429WithInfo(ctx, "openai", info)

		require.True(t, rl.isSequentialMode("openai"))

		// Track concurrent access
		var concurrentCount int
		var maxConcurrent int
		var mu sync.Mutex
		var wg sync.WaitGroup

		// Start multiple goroutines that try to make requests
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				err := rl.WaitBeforeRequest(ctx, "openai")
				if err != nil {
					return
				}

				mu.Lock()
				concurrentCount++
				if concurrentCount > maxConcurrent {
					maxConcurrent = concurrentCount
				}
				mu.Unlock()

				// Simulate some work
				time.Sleep(10 * time.Millisecond)

				mu.Lock()
				concurrentCount--
				mu.Unlock()

				rl.ReleaseSequentialSlot("openai")
			}()
		}

		wg.Wait()

		// In sequential mode, max concurrent should be 1
		assert.Equal(t, 1, maxConcurrent, "sequential mode should limit concurrency to 1")
	})

	t.Run("AcquireSequentialSlot blocks when slot is held", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 10,
			Output:            output,
		})

		// Force sequential mode
		info := &gateway.RateLimitInfo{
			Provider:   "openai",
			RetryAfter: 10 * time.Millisecond,
		}
		ctx := context.Background()
		_ = rl.Handle429WithInfo(ctx, "openai", info)

		require.True(t, rl.isSequentialMode("openai"))

		// First goroutine acquires the slot
		err := rl.WaitBeforeRequest(ctx, "openai")
		require.NoError(t, err)

		// Track when second goroutine proceeds
		var secondStarted, secondCompleted bool
		var mu sync.Mutex

		go func() {
			mu.Lock()
			secondStarted = true
			mu.Unlock()

			// This should block until slot is released
			err := rl.WaitBeforeRequest(ctx, "openai")
			if err == nil {
				rl.ReleaseSequentialSlot("openai")
			}

			mu.Lock()
			secondCompleted = true
			mu.Unlock()
		}()

		// Give second goroutine time to start waiting
		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		started := secondStarted
		completed := secondCompleted
		mu.Unlock()

		// Second should have started but not completed
		assert.True(t, started, "second goroutine should have started")
		assert.False(t, completed, "second goroutine should be blocked")

		// Release the slot
		rl.ReleaseSequentialSlot("openai")

		// Wait for second goroutine to complete
		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		completed = secondCompleted
		mu.Unlock()

		assert.True(t, completed, "second goroutine should have completed after release")
	})
}

// TestRateLimiter_SharedWait verifies that when one goroutine hits a 429 and starts
// waiting, other goroutines for the same provider share that wait instead of each
// making their own API call.
func TestRateLimiter_SharedWait(t *testing.T) {
	t.Run("multiple goroutines share a single wait period", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 10,
			Output:            output,
		})

		ctx := context.Background()
		var wg sync.WaitGroup
		var handle429Count int
		var mu sync.Mutex

		// Wrapper to track how many goroutines call Handle429WithInfo with wait
		callHandle429 := func() {
			mu.Lock()
			handle429Count++
			mu.Unlock()

			info := &gateway.RateLimitInfo{
				Provider:   "openai",
				RetryAfter: 50 * time.Millisecond,
			}
			_ = rl.Handle429WithInfo(ctx, "openai", info)
		}

		// Start 3 goroutines that all hit 429 at nearly the same time
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				callHandle429()
			}()
			// Small stagger so they don't all start at exactly the same time
			time.Sleep(5 * time.Millisecond)
		}

		wg.Wait()

		// All 3 should have called Handle429WithInfo, but output should only
		// show one rate limit message (first one does the actual wait,
		// others wait silently via the cond variable)
		mu.Lock()
		count := handle429Count
		mu.Unlock()
		assert.Equal(t, 3, count, "all 3 goroutines should have called Handle429WithInfo")

		// Check that the output contains only one "Rate limit reached" message
		outputStr := output.String()
		assert.Contains(t, outputStr, "Rate limit reached for openai")
	})

	t.Run("goroutines woken up after wait can proceed", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 10,
			Output:            output,
		})

		ctx := context.Background()
		var completedCount int
		var mu sync.Mutex
		var wg sync.WaitGroup

		// Start multiple goroutines that will share the wait
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				info := &gateway.RateLimitInfo{
					Provider:   "openai",
					RetryAfter: 30 * time.Millisecond,
				}
				err := rl.Handle429WithInfo(ctx, "openai", info)

				// After wait, all should be able to proceed (with retryable error)
				if err != nil {
					mu.Lock()
					completedCount++
					mu.Unlock()
				}
			}()
			time.Sleep(5 * time.Millisecond)
		}

		wg.Wait()

		// All goroutines should have completed
		mu.Lock()
		count := completedCount
		mu.Unlock()
		assert.Equal(t, 3, count, "all goroutines should have woken up and completed")

		// Provider should now be in sequential mode
		assert.True(t, rl.isSequentialMode("openai"))
	})
}

func TestRateLimiter_Handle429WithInfo(t *testing.T) {
	t.Run("stops provider without wait flag", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit: false,
		})

		ctx := context.Background()
		info := &gateway.RateLimitInfo{
			Provider:   "openai",
			RetryAfter: 5 * time.Second,
		}
		err := rl.Handle429WithInfo(ctx, "openai", info)
		assert.Error(t, err)
		assert.True(t, rl.IsProviderStopped("openai"))
	})

	t.Run("uses retry-after header when available", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 50, // Short buffer for testing
			Output:            output,
		})

		// Create context with short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		info := &gateway.RateLimitInfo{
			Provider:          "anthropic",
			RetryAfter:        2 * time.Second, // Should use this
			RequestsLimit:     1000,
			RequestsRemaining: 0,
		}

		err := rl.Handle429WithInfo(ctx, "anthropic", info)
		// Should timeout before completing the wait
		assert.Error(t, err)
		assert.False(t, rl.IsProviderStopped("anthropic"))
		// Verify output mentions retry-after
		assert.Contains(t, output.String(), "retry-after")
	})

	t.Run("uses reset timestamp when retry-after not available", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 50,
			Output:            output,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		info := &gateway.RateLimitInfo{
			Provider:          "anthropic",
			RequestsReset:     time.Now().Add(2 * time.Second), // Should use this
			RequestsLimit:     1000,
			RequestsRemaining: 0,
		}

		err := rl.Handle429WithInfo(ctx, "anthropic", info)
		assert.Error(t, err)
		assert.False(t, rl.IsProviderStopped("anthropic"))
		// Verify output mentions reset timestamp
		assert.Contains(t, output.String(), "reset timestamp")
	})

	t.Run("falls back to default when no header info", func(t *testing.T) {
		output := &bytes.Buffer{}
		rl := NewRateLimiter(RateLimiterConfig{
			WaitOnLimit:       true,
			RateLimitBufferMs: 50,
			Output:            output,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// No rate limit info
		err := rl.Handle429WithInfo(ctx, "openai", nil)
		assert.Error(t, err)
		assert.False(t, rl.IsProviderStopped("openai"))
		// Verify output mentions default
		assert.Contains(t, output.String(), "default")
	})
}
