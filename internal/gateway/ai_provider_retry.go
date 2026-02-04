package gateway

import (
	"context"
	"math"
	"math/rand"
	"net/http"
	"time"
)

// Retry configuration matching SDK behavior for parity.
// Both OpenAI and Anthropic SDKs use identical parameters.
const (
	retryMaxAttempts  = 2      // 1 initial + 2 retries = 3 total attempts
	retryInitialDelay = 500 * time.Millisecond
	retryMaxDelay     = 8 * time.Second
	retryJitterFactor = 0.25 // ±25% jitter
)

// retryableStatusCodes defines HTTP status codes that warrant retry.
// Matches SDK behavior: 429 (rate limit), 5xx (server errors).
var retryableStatusCodes = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusInternalServerError: true, // 500
	http.StatusBadGateway:          true, // 502
	http.StatusServiceUnavailable:  true, // 503
	http.StatusGatewayTimeout:      true, // 504
}

// isRetryableStatusCode returns true if the status code warrants retry.
func isRetryableStatusCode(statusCode int) bool {
	return retryableStatusCodes[statusCode]
}

// isRetryableError returns true if the error is retryable.
// This checks for transient network errors and context errors.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Context errors are not retryable (deadline/cancellation)
	if err == context.DeadlineExceeded || err == context.Canceled {
		return false
	}
	// Transient network errors are retryable
	// The error could be a temporary network issue - let the retry logic handle it
	return true
}

// retryOperation executes the operation with exponential backoff and jitter.
// It returns the result and any error from the final attempt.
//
// The operation function should return:
// - The result value
// - HTTP status code (0 if not applicable, e.g., network error)
// - Error (nil on success)
//
// Retry logic matches SDK behavior:
// - Retries on 429, 5xx status codes
// - Retries on transient network errors
// - Does NOT retry on 4xx (except 429), context errors, or parse errors
// - Uses exponential backoff with jitter
func retryOperation[T any](ctx context.Context, operation func() (T, int, error)) (T, error) {
	var result T
	var lastErr error
	var statusCode int

	for attempt := 0; attempt <= retryMaxAttempts; attempt++ {
		// Check context before each attempt
		if ctx.Err() != nil {
			var zero T
			return zero, ctx.Err()
		}

		result, statusCode, lastErr = operation()

		// Success - return immediately
		if lastErr == nil {
			return result, nil
		}

		// Determine if we should retry
		shouldRetry := false
		if statusCode != 0 && isRetryableStatusCode(statusCode) {
			shouldRetry = true
		} else if statusCode == 0 && isRetryableError(lastErr) {
			// Network error without status code
			shouldRetry = true
		}

		// If not retryable or no more attempts, return the error
		if !shouldRetry || attempt == retryMaxAttempts {
			var zero T
			return zero, lastErr
		}

		// Calculate backoff delay with jitter
		delay := calculateBackoffDelay(attempt)

		// Wait for delay or context cancellation
		select {
		case <-time.After(delay):
			// Continue to next attempt
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}

	// Should not reach here, but return last error if we do
	var zero T
	return zero, lastErr
}

// calculateBackoffDelay computes the delay for the given attempt using
// exponential backoff with jitter. Matches SDK behavior.
func calculateBackoffDelay(attempt int) time.Duration {
	// Exponential backoff: initial * 2^attempt
	delay := float64(retryInitialDelay) * math.Pow(2, float64(attempt))

	// Cap at max delay
	if delay > float64(retryMaxDelay) {
		delay = float64(retryMaxDelay)
	}

	// Add jitter: delay * (1 + random(-jitter, +jitter))
	jitter := (rand.Float64()*2 - 1) * retryJitterFactor // -0.25 to +0.25
	delay = delay * (1 + jitter)

	return time.Duration(delay)
}
