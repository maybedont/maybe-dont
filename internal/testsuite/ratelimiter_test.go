package testsuite

import (
	"bytes"
	"context"
	"testing"
	"time"

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
