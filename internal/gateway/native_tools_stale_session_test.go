package gateway

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativeTools_StaleSession_Integration tests that native tools return
// SessionExpiredError when the session doesn't exist in the real SessionManager.
// This is an integration test using real ClientManager/SessionManager components.
func TestNativeTools_StaleSession_Integration(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a real ClientManager (which contains the real SessionManager)
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Configure a downstream client (needed for ClientManager to be fully initialized)
	githubConfig := config.ClientConfig{
		Type: "http",
		URL:  "https://api.github.com",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create NativeToolsHandler with real ClientManager as session provider
	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true
	cfg.NativeTools.ListServers.Enabled = true
	cfg.NativeTools.AuditLog.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")
	handler.SetSessionProvider(cm) // Use real ClientManager as session provider

	// Use a stale session ID that doesn't exist in SessionManager
	staleSessionID := "stale-session-integration-test"

	// Verify session doesn't exist
	assert.False(t, cm.HasSession(staleSessionID), "Session should not exist initially")

	// Create context with the stale session using MCP server's session mechanism
	mockSession := &mockClientSession{sessionID: staleSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// Test each native tool (except discover_tools which is exempt)
	tools := []string{
		ToolListSessions,
		ToolListDownstreamServers,
		ToolGetAuditLog,
	}

	for _, toolName := range tools {
		t.Run(toolName, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Name = toolName

			// Call through HandleToolCall
			result, err := handler.HandleToolCall(ctxWithSession, req)

			// Should return SessionExpiredError
			require.Error(t, err, "Should return error for stale session")
			require.Nil(t, result)

			// Verify it's a SessionExpiredError
			assert.True(t, IsSessionExpiredError(err), "Error should be SessionExpiredError")

			// Verify the error message contains recovery instructions
			errMsg := err.Error()
			assert.Contains(t, errMsg, "Session expired")
			assert.Contains(t, errMsg, "maybedont__discover_tools")
			assert.Contains(t, errMsg, toolName)
		})
	}
}

// TestNativeTools_ValidSession_Integration tests that native tools work
// when the session exists in the real SessionManager.
func TestNativeTools_ValidSession_Integration(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a real ClientManager
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Configure a downstream client
	githubConfig := config.ClientConfig{
		Type: "http",
		URL:  "https://api.github.com",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create NativeToolsHandler with real ClientManager
	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")
	handler.SetSessionProvider(cm)

	// Create a valid session in the SessionManager BEFORE making the request
	validSessionID := "valid-session-integration-test"
	cm.sessionManager.CreateSession(validSessionID)

	// Verify session exists
	assert.True(t, cm.HasSession(validSessionID), "Session should exist")

	// Create context with the valid session
	mockSession := &mockClientSession{sessionID: validSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// Call list_sessions - should succeed
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions

	result, err := handler.HandleToolCall(ctxWithSession, req)

	// Should succeed
	require.NoError(t, err, "Should not return error for valid session")
	require.NotNil(t, result)
	assert.False(t, result.IsError, "Result should not be an error")
}

// TestNativeTools_DiscoverTools_ExemptFromSessionValidation_Integration tests that
// discover_tools works even with a stale session (since it's the recovery mechanism).
func TestNativeTools_DiscoverTools_ExemptFromSessionValidation_Integration(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a real ClientManager
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Configure a downstream client
	githubConfig := config.ClientConfig{
		Type: "http",
		URL:  "https://api.github.com",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create NativeToolsHandler with real ClientManager
	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, "")
	handler.SetSessionProvider(cm)

	// Create a minimal Gateway to use as discovery provider
	g := &Gateway{
		clientManager: cm,
		logger:        logger,
		config:        cfg,
	}
	handler.SetDiscoveryProvider(g)

	// Use a stale session ID that doesn't exist
	staleSessionID := "stale-session-discover-test"
	assert.False(t, cm.HasSession(staleSessionID), "Session should not exist initially")

	// Create context with the stale session
	mockSession := &mockClientSession{sessionID: staleSessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// Call discover_tools - should NOT return SessionExpiredError
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools

	result, err := handler.HandleToolCall(ctxWithSession, req)

	// Should NOT return SessionExpiredError (discover_tools is exempt)
	if err != nil {
		// If there's an error, it should NOT be SessionExpiredError
		assert.False(t, IsSessionExpiredError(err),
			"discover_tools should not return SessionExpiredError even with stale session")
	}

	// If we got a result, it should be valid
	if result != nil {
		// discover_tools might return an error result (e.g., if no pass-through clients)
		// but it should have been processed, not rejected at session validation
		t.Logf("discover_tools returned result: IsError=%v", result.IsError)
	}
}

// TestNativeTools_SessionExpiredAfterCreation_Integration tests the scenario where
// a session is created, then expires (is removed), and then a native tool is called.
// This simulates what happens after a server restart or session timeout.
func TestNativeTools_SessionExpiredAfterCreation_Integration(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a real ClientManager
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	githubConfig := config.ClientConfig{
		Type: "http",
		URL:  "https://api.github.com",
	}
	require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	}))

	// Create NativeToolsHandler
	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")
	handler.SetSessionProvider(cm)

	// Create a session
	sessionID := "session-will-expire"
	cm.sessionManager.CreateSession(sessionID)
	assert.True(t, cm.HasSession(sessionID), "Session should exist after creation")

	// Create context with the session
	mockSession := &mockClientSession{sessionID: sessionID}
	srv := server.NewMCPServer("test", "1.0.0")
	ctxWithSession := srv.WithContext(ctx, mockSession)

	// First call should succeed
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions

	result, err := handler.HandleToolCall(ctxWithSession, req)
	require.NoError(t, err, "First call should succeed")
	require.NotNil(t, result)

	// Now simulate session expiry by removing it from SessionManager
	err = cm.sessionManager.DeleteSession(ctx, sessionID)
	require.NoError(t, err, "Should be able to delete session")
	assert.False(t, cm.HasSession(sessionID), "Session should be removed")

	// Second call should fail with SessionExpiredError
	result2, err2 := handler.HandleToolCall(ctxWithSession, req)
	require.Error(t, err2, "Second call should fail after session expiry")
	require.Nil(t, result2)
	assert.True(t, IsSessionExpiredError(err2), "Should be SessionExpiredError")
	assert.Contains(t, err2.Error(), "maybedont__discover_tools")
}
