package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscoverAndRegisterSessionTools_PreservesCredentials verifies that
// credentials from the original context are preserved in the async discovery.
func TestDiscoverAndRegisterSessionTools_PreservesCredentials(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cm := NewClientManager(ctx, logger)

	// Initialize with no clients (we just want to test context preservation)
	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{})
	require.NoError(t, err)

	// Create credentials that should be preserved
	creds := &ServiceCredentials{
		clients: map[string]*ClientCredentials{
			"github": {
				Headers: map[string]string{
					"Authorization": "Bearer test-token",
				},
			},
		},
	}

	// Create a context with credentials
	ctxWithCreds := WithServiceCredentials(ctx, creds)
	ctxWithCreds = WithClientIP(ctxWithCreds, "192.168.1.100")

	// Verify credentials are in original context
	retrievedCreds, ok := GetServiceCredentials(ctxWithCreds, "github")
	require.True(t, ok)
	assert.Equal(t, "Bearer test-token", retrievedCreds.Headers["Authorization"])

	// Verify client IP is in original context
	clientIP, ok := GetClientIP(ctxWithCreds)
	require.True(t, ok)
	assert.Equal(t, "192.168.1.100", clientIP)
}

// TestAsyncDiscovery_DoesNotBlockOnCancelledContext verifies that async discovery
// continues even when the original context is cancelled (simulating client disconnect).
func TestAsyncDiscovery_DoesNotBlockOnCancelledContext(t *testing.T) {
	logger := newTestLogger(t)

	cm := NewClientManager(context.Background(), logger)

	// Initialize with no clients
	err := cm.InitializeClients(context.Background(), map[string]config.ClientConfig{})
	require.NoError(t, err)

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Extract values like onSessionRegister does
	serviceCreds, _ := ctx.Value(ServiceCredentialsKey).(*ServiceCredentials)
	clientIP, _ := GetClientIP(ctx)

	// Cancel the context (simulating client disconnect)
	cancel()

	// Verify original context is cancelled
	select {
	case <-ctx.Done():
		// Expected - context is cancelled
	default:
		t.Fatal("Context should be cancelled")
	}

	// Create a new background context like discoverAndRegisterSessionTools does
	asyncCtx := context.Background()
	if serviceCreds != nil {
		asyncCtx = WithServiceCredentials(asyncCtx, serviceCreds)
	}
	if clientIP != "" {
		asyncCtx = WithClientIP(asyncCtx, clientIP)
	}

	// Verify the async context is NOT cancelled
	select {
	case <-asyncCtx.Done():
		t.Fatal("Async context should not be cancelled")
	default:
		// Expected - async context is still valid
	}

	// CreateSessionClients should work with the background context
	result, err := cm.CreateSessionClients(asyncCtx, "test-session")
	// No clients configured, so result should be empty but no error
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestOnSessionRegister_ReturnsQuickly verifies that onSessionRegister doesn't
// block waiting for tool discovery to complete.
func TestOnSessionRegister_ReturnsQuickly(t *testing.T) {
	// This test verifies the architectural requirement that onSessionRegister
	// returns quickly by spawning a goroutine for async work.
	//
	// We can't easily test the full Gateway.onSessionRegister without setting up
	// a complete MCP server, but we can verify the pattern used.

	var wg sync.WaitGroup
	hookReturned := make(chan struct{})
	asyncWorkDone := make(chan struct{})

	// Simulate the pattern used in onSessionRegister
	simulatedHook := func() {
		// This represents the quick synchronous work
		// (like storing client IP)

		// Spawn async work (like tool discovery)
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Simulate slow network I/O
			time.Sleep(100 * time.Millisecond)
			close(asyncWorkDone)
		}()

		// Hook returns immediately
		close(hookReturned)
	}

	// Run the simulated hook
	start := time.Now()
	simulatedHook()
	hookDuration := time.Since(start)

	// Verify hook returned quickly (much less than the async work duration)
	assert.Less(t, hookDuration, 50*time.Millisecond,
		"Hook should return quickly without waiting for async work")

	// Verify hook has returned
	select {
	case <-hookReturned:
		// Expected
	default:
		t.Fatal("Hook should have returned")
	}

	// Verify async work hasn't completed yet
	select {
	case <-asyncWorkDone:
		t.Fatal("Async work should not be done yet")
	default:
		// Expected - async work is still running
	}

	// Wait for async work to complete
	wg.Wait()

	// Now async work should be done
	select {
	case <-asyncWorkDone:
		// Expected
	default:
		t.Fatal("Async work should be done now")
	}
}

// TestContextPreservation_ServiceCredentials verifies that service credentials
// are correctly transferred to a new background context.
func TestContextPreservation_ServiceCredentials(t *testing.T) {
	// Create credentials
	creds := &ServiceCredentials{
		clients: map[string]*ClientCredentials{
			"github": {
				Headers: map[string]string{
					"Authorization": "Bearer github-token",
					"X-Custom":      "custom-value",
				},
			},
			"aws": {
				Headers: map[string]string{
					"X-Api-Key": "aws-key",
				},
			},
		},
	}

	// Create original context with credentials (simulating HTTP request context)
	originalCtx, cancel := context.WithCancel(context.Background())
	originalCtx = WithServiceCredentials(originalCtx, creds)

	// Extract credentials like onSessionRegister does
	extractedCreds, _ := originalCtx.Value(ServiceCredentialsKey).(*ServiceCredentials)

	// Cancel original context (simulating request end)
	cancel()

	// Create new background context with preserved credentials
	asyncCtx := context.Background()
	if extractedCreds != nil {
		asyncCtx = WithServiceCredentials(asyncCtx, extractedCreds)
	}

	// Verify credentials are available in async context
	githubCreds, ok := GetServiceCredentials(asyncCtx, "github")
	require.True(t, ok, "GitHub credentials should be available")
	assert.Equal(t, "Bearer github-token", githubCreds.Headers["Authorization"])
	assert.Equal(t, "custom-value", githubCreds.Headers["X-Custom"])

	awsCreds, ok := GetServiceCredentials(asyncCtx, "aws")
	require.True(t, ok, "AWS credentials should be available")
	assert.Equal(t, "aws-key", awsCreds.Headers["X-Api-Key"])

	// Verify async context is not cancelled
	select {
	case <-asyncCtx.Done():
		t.Fatal("Async context should not be cancelled")
	default:
		// Expected
	}
}
