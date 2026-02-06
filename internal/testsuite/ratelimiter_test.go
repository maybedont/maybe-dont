package testsuite

import (
	"bytes"
	"context"
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
