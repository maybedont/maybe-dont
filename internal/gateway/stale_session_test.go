package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClientSession implements server.ClientSession for testing
type mockClientSession struct {
	sessionID string
}

func (m *mockClientSession) SessionID() string {
	return m.sessionID
}

func (m *mockClientSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return nil
}

func (m *mockClientSession) Initialize() {}

func (m *mockClientSession) Initialized() bool {
	return true
}

// TestOnRequestInitialization_StaleSessionDetection tests that when an AI agent
// uses a stale session ID (one that doesn't exist in our SessionManager) to call
// a downstream tool, we return a helpful error instead of a generic "tool not found".
func TestOnRequestInitialization_StaleSessionDetection(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a ClientManager with configured downstream clients
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Configure a "github" downstream client
	githubConfig := config.ClientConfig{
		Type:    "http",
		URL:     "https://api.github.com",
		Command: "",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create a minimal Gateway with just the ClientManager and logger
	g := &Gateway{
		clientManager: cm,
		logger:        logger,
	}

	// Simulate a stale session ID that doesn't exist in our SessionManager
	staleSessionID := "stale-session-12345"

	// Create a mock session and add it to the context using the SDK's method
	mockSession := &mockClientSession{sessionID: staleSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// Simulate a tools/call request for a downstream tool
	toolCallRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "github__list_pull_requests",
			"arguments": map[string]interface{}{
				"owner": "maybedont",
				"repo":  "maybe-dont",
			},
		},
	}

	msgBytes, err := json.Marshal(toolCallRequest)
	require.NoError(t, err)

	// Call our onRequestInitialization hook
	// The session ID doesn't exist in our SessionManager, so it should return an error
	hookErr := g.onRequestInitialization(ctxWithSession, 1, json.RawMessage(msgBytes))

	// Verify we get a SessionExpiredError
	require.Error(t, hookErr, "Should return an error for stale session")
	assert.True(t, IsSessionExpiredError(hookErr), "Error should be a SessionExpiredError")

	// Verify the error message contains helpful guidance
	errMsg := hookErr.Error()
	assert.Contains(t, errMsg, "Session expired")
	assert.Contains(t, errMsg, "github__list_pull_requests")
	assert.Contains(t, errMsg, "maybedont__discover_tools")
	assert.Contains(t, errMsg, "re-establish")
}

// TestOnRequestInitialization_ValidSession tests that when a session exists
// in our SessionManager, the request is allowed through.
func TestOnRequestInitialization_ValidSession(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a ClientManager with configured downstream clients
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Configure a "github" downstream client
	githubConfig := config.ClientConfig{
		Type:    "http",
		URL:     "https://api.github.com",
		Command: "",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create the session in our SessionManager BEFORE the request
	validSessionID := "valid-session-12345"
	cm.sessionManager.CreateSession(validSessionID)

	// Create a minimal Gateway
	g := &Gateway{
		clientManager: cm,
		logger:        logger,
	}

	// Create a mock session and add it to the context
	mockSession := &mockClientSession{sessionID: validSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// Simulate a tools/call request for a downstream tool
	toolCallRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "github__list_pull_requests",
			"arguments": map[string]interface{}{
				"owner": "maybedont",
				"repo":  "maybe-dont",
			},
		},
	}

	msgBytes, err := json.Marshal(toolCallRequest)
	require.NoError(t, err)

	// Call our onRequestInitialization hook
	// The session ID exists in our SessionManager, so it should NOT return an error
	hookErr := g.onRequestInitialization(ctxWithSession, 1, json.RawMessage(msgBytes))

	// Verify no error (request should proceed)
	assert.NoError(t, hookErr, "Should not return an error for valid session")
}

// TestOnRequestInitialization_NativeToolNotAffected tests that native tools
// (like maybedont__discover_tools) are not affected by stale session detection.
func TestOnRequestInitialization_NativeToolNotAffected(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a ClientManager with configured downstream clients
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Configure a "github" downstream client (NOT "maybedont")
	githubConfig := config.ClientConfig{
		Type:    "http",
		URL:     "https://api.github.com",
		Command: "",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create a minimal Gateway
	g := &Gateway{
		clientManager: cm,
		logger:        logger,
	}

	// Use a stale session ID that doesn't exist
	staleSessionID := "stale-session-12345"
	mockSession := &mockClientSession{sessionID: staleSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// Simulate a tools/call request for a NATIVE tool (maybedont__discover_tools)
	// Note: "maybedont" is NOT a configured downstream client
	toolCallRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "maybedont__discover_tools",
		},
	}

	msgBytes, err := json.Marshal(toolCallRequest)
	require.NoError(t, err)

	// Call our onRequestInitialization hook
	// Even though the session is stale, the tool prefix "maybedont" is not a configured
	// downstream client, so we should NOT return a stale session error
	hookErr := g.onRequestInitialization(ctxWithSession, 1, json.RawMessage(msgBytes))

	// Verify no error (native tools should work regardless of session state)
	assert.NoError(t, hookErr, "Should not return an error for native tools")
}

// TestOnRequestInitialization_NonToolCallMethods tests that non-tools/call methods
// are not affected by stale session detection.
func TestOnRequestInitialization_NonToolCallMethods(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a ClientManager with configured downstream clients
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	githubConfig := config.ClientConfig{
		Type:    "http",
		URL:     "https://api.github.com",
		Command: "",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create a minimal Gateway
	g := &Gateway{
		clientManager: cm,
		logger:        logger,
	}

	// Use a stale session ID
	staleSessionID := "stale-session-12345"
	mockSession := &mockClientSession{sessionID: staleSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// Test various non-tools/call methods
	methods := []string{"tools/list", "initialize", "ping", "prompts/list", "resources/list"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			request := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  method,
			}

			msgBytes, err := json.Marshal(request)
			require.NoError(t, err)

			// Call our hook - non-tools/call methods should pass through
			hookErr := g.onRequestInitialization(ctxWithSession, 1, json.RawMessage(msgBytes))
			assert.NoError(t, hookErr, "Method %s should not trigger stale session check", method)
		})
	}
}

// TestOnRequestInitialization_UnknownClientPrefix tests that tools with prefixes
// that don't match any configured downstream client are allowed through.
func TestOnRequestInitialization_UnknownClientPrefix(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a ClientManager with only "github" configured
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	githubConfig := config.ClientConfig{
		Type:    "http",
		URL:     "https://api.github.com",
		Command: "",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create a minimal Gateway
	g := &Gateway{
		clientManager: cm,
		logger:        logger,
	}

	// Use a stale session ID
	staleSessionID := "stale-session-12345"
	mockSession := &mockClientSession{sessionID: staleSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// Simulate a tools/call request for a tool with an UNKNOWN client prefix
	toolCallRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "azure__list_vms", // "azure" is NOT configured
		},
	}

	msgBytes, err := json.Marshal(toolCallRequest)
	require.NoError(t, err)

	// Call our hook - unknown prefix should pass through (let SDK handle "tool not found")
	hookErr := g.onRequestInitialization(ctxWithSession, 1, json.RawMessage(msgBytes))
	assert.NoError(t, hookErr, "Unknown client prefix should not trigger stale session error")
}

// TestCreateSingleSessionClient_CreatesSessionIfNotExists tests that
// CreateSingleSessionClient creates a new session if the session doesn't exist.
// This is critical for discover_tools to work after server restart.
func TestCreateSingleSessionClient_CreatesSessionIfNotExists(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Don't create a session - simulate stale session after server restart
	staleSessionID := "stale-session-12345"

	// Verify session doesn't exist initially
	assert.False(t, cm.HasSession(staleSessionID), "Session should not exist initially")

	// Configure a mock client (we can't actually connect, but we can test session creation)
	// We'll use a config that will fail to connect, but the session should still be created
	cfg := config.ClientConfig{
		Type:    "stdio",
		Command: "nonexistent-command-that-will-fail",
	}

	// Call CreateSingleSessionClient with the stale session ID
	// This will fail to create the client (command doesn't exist), but should create the session
	_, err := cm.CreateSingleSessionClient(ctx, staleSessionID, "test-client", cfg)

	// The client creation might fail (command not found), but that's OK
	// The important thing is that the session was created
	if err != nil {
		// Expected - the command doesn't exist
		t.Logf("Expected error creating client: %v", err)
	}

	// Verify session was created (this is the key assertion)
	assert.True(t, cm.HasSession(staleSessionID), "Session should be created even if client creation fails later")
}

// TestCreateSingleSessionClient_UsesExistingSession tests that
// CreateSingleSessionClient reuses an existing session.
func TestCreateSingleSessionClient_UsesExistingSession(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Create session first
	validSessionID := "valid-session-12345"
	cm.sessionManager.CreateSession(validSessionID)

	// Verify session exists
	assert.True(t, cm.HasSession(validSessionID), "Session should exist")

	// Get the session and check initial state
	session, exists := cm.sessionManager.GetSession(validSessionID)
	require.True(t, exists)
	initialClients := len(session.GetAllClients())

	// Try to create a client (will fail, but shouldn't create a new session)
	cfg := config.ClientConfig{
		Type:    "stdio",
		Command: "nonexistent-command",
	}

	_, _ = cm.CreateSingleSessionClient(ctx, validSessionID, "test-client", cfg)

	// Verify we still have the same session (not a new one)
	assert.True(t, cm.HasSession(validSessionID), "Session should still exist")

	// Verify no new sessions were created
	allSessions := cm.sessionManager.GetAllSessions()
	assert.Len(t, allSessions, 1, "Should only have one session")
	assert.Equal(t, validSessionID, allSessions[0], "Session ID should match")

	// The number of clients might be the same (if creation failed) or +1 (if succeeded)
	// The key is that we didn't create a duplicate session
	_ = initialClients // Acknowledge we checked initial state
}

// TestOnRequestInitialization_MessageTypes tests that both json.RawMessage
// and []byte message types are handled correctly.
func TestOnRequestInitialization_MessageTypes(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	githubConfig := config.ClientConfig{
		Type: "http",
		URL:  "https://api.github.com",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	g := &Gateway{
		clientManager: cm,
		logger:        logger,
	}

	staleSessionID := "stale-session-12345"
	mockSession := &mockClientSession{sessionID: staleSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	toolCallRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "github__list_pull_requests",
		},
	}

	msgBytes, err := json.Marshal(toolCallRequest)
	require.NoError(t, err)

	// Test with json.RawMessage (this is what the SDK actually passes)
	t.Run("json.RawMessage", func(t *testing.T) {
		hookErr := g.onRequestInitialization(ctxWithSession, 1, json.RawMessage(msgBytes))
		require.Error(t, hookErr)
		assert.True(t, IsSessionExpiredError(hookErr))
	})

	// Test with []byte (for completeness)
	t.Run("[]byte", func(t *testing.T) {
		hookErr := g.onRequestInitialization(ctxWithSession, 1, msgBytes)
		require.Error(t, hookErr)
		assert.True(t, IsSessionExpiredError(hookErr))
	})

	// Test with unsupported type (should pass through)
	t.Run("unsupported type", func(t *testing.T) {
		hookErr := g.onRequestInitialization(ctxWithSession, 1, "string message")
		assert.NoError(t, hookErr, "Unsupported message type should pass through")
	})
}
