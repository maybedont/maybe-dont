package gateway

import (
	"context"
	"testing"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContextPreservation_Credentials verifies that credentials from the original
// context are preserved when passed to downstream operations like lazy discovery.
func TestContextPreservation_Credentials(t *testing.T) {
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

// TestLazyDiscovery_WorksWithBackgroundContext verifies that lazy discovery
// continues working even when the original context is cancelled (simulating client disconnect).
// This ensures context values are properly extracted before any async work begins.
func TestLazyDiscovery_WorksWithBackgroundContext(t *testing.T) {
	logger := newTestLogger(t)

	cm := NewClientManager(context.Background(), logger)

	// Initialize with no clients
	err := cm.InitializeClients(context.Background(), map[string]config.ClientConfig{})
	require.NoError(t, err)

	// Create a context that we'll cancel, with a request_id set
	ctx, cancel := context.WithCancel(context.Background())
	ctx = WithRequestID(ctx, "original-request-id")

	// Extract values like onSessionRegister does
	serviceCreds, _ := ctx.Value(ServiceCredentialsKey).(*ServiceCredentials)
	clientIP, _ := GetClientIP(ctx)
	requestID, _ := GetRequestID(ctx)

	// Cancel the context (simulating client disconnect)
	cancel()

	// Verify original context is cancelled
	select {
	case <-ctx.Done():
		// Expected - context is cancelled
	default:
		t.Fatal("Context should be cancelled")
	}

	// Create a new background context with preserved values
	// (this pattern is used when we need work to continue after request ends)
	asyncCtx := context.Background()
	if serviceCreds != nil {
		asyncCtx = WithServiceCredentials(asyncCtx, serviceCreds)
	}
	if clientIP != "" {
		asyncCtx = WithClientIP(asyncCtx, clientIP)
	}
	if requestID != "" {
		asyncCtx = WithRequestID(asyncCtx, requestID)
	}

	// Verify the new context is NOT cancelled
	select {
	case <-asyncCtx.Done():
		t.Fatal("New context should not be cancelled")
	default:
		// Expected - new context is still valid
	}

	// Verify request_id was preserved
	asyncRequestID, hasRequestID := GetRequestID(asyncCtx)
	require.True(t, hasRequestID, "Request ID should be preserved in new context")
	assert.Equal(t, "original-request-id", asyncRequestID)

	// CreateSessionClients should work with the background context
	result, err := cm.CreateSessionClients(asyncCtx, "test-session")
	// No clients configured, so result should be empty but no error
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestContextPreservation_RequestID verifies that request_id is correctly
// transferred to a new background context, enabling log correlation between
// the triggering request and subsequent operations.
func TestContextPreservation_RequestID(t *testing.T) {
	// Create original context with request_id (simulating HTTP request context)
	originalCtx, cancel := context.WithCancel(context.Background())
	originalCtx = WithRequestID(originalCtx, "test-request-id-12345")

	// Extract request_id like onSessionRegister does
	extractedRequestID, hasRequestID := GetRequestID(originalCtx)
	require.True(t, hasRequestID, "Request ID should be present in original context")
	assert.Equal(t, "test-request-id-12345", extractedRequestID)

	// Cancel original context (simulating request end)
	cancel()

	// Create new background context with preserved request_id
	newCtx := context.Background()
	if extractedRequestID != "" {
		newCtx = WithRequestID(newCtx, extractedRequestID)
	}

	// Verify request_id is available in new context
	retrievedRequestID, ok := GetRequestID(newCtx)
	require.True(t, ok, "Request ID should be available in new context")
	assert.Equal(t, "test-request-id-12345", retrievedRequestID)

	// Verify new context is not cancelled
	select {
	case <-newCtx.Done():
		t.Fatal("New context should not be cancelled")
	default:
		// Expected
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
	newCtx := context.Background()
	if extractedCreds != nil {
		newCtx = WithServiceCredentials(newCtx, extractedCreds)
	}

	// Verify credentials are available in new context
	githubCreds, ok := GetServiceCredentials(newCtx, "github")
	require.True(t, ok, "GitHub credentials should be available")
	assert.Equal(t, "Bearer github-token", githubCreds.Headers["Authorization"])
	assert.Equal(t, "custom-value", githubCreds.Headers["X-Custom"])

	awsCreds, ok := GetServiceCredentials(newCtx, "aws")
	require.True(t, ok, "AWS credentials should be available")
	assert.Equal(t, "aws-key", awsCreds.Headers["X-Api-Key"])

	// Verify new context is not cancelled
	select {
	case <-newCtx.Done():
		t.Fatal("New context should not be cancelled")
	default:
		// Expected
	}
}
