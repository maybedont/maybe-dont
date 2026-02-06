package gateway

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativeTools_SessionExpiredError verifies that native tools return SessionExpiredError
// when the session is not found in SessionManager, with recovery instructions to call
// maybedont__discover_tools.
func TestNativeTools_SessionExpiredError(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
	}{
		{
			name:     "get_audit_log returns session expired error",
			toolName: ToolGetAuditLog,
		},
		{
			name:     "generate_audit_report returns session expired error",
			toolName: ToolGenerateAuditReport,
		},
		{
			name:     "list_downstream_servers returns session expired error",
			toolName: ToolListDownstreamServers,
		},
		{
			name:     "list_sessions returns session expired error",
			toolName: ToolListSessions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newTestLogger(t)

			// Create handler with all native tools enabled
			cfg := &config.Config{}
			cfg.NativeTools.AuditLog.Enabled = true
			cfg.NativeTools.AuditReport.Enabled = true
			cfg.NativeTools.ListServers.Enabled = true
			cfg.NativeTools.ListSessions.Enabled = true

			handler := NewNativeToolsHandler(cfg, logger, "")

			// Set up mock session provider that reports session as invalid
			handler.SetSessionProvider(&mockSessionProvider{
				validSessions: map[string]bool{
					"valid-session": true,
					// "stale-session" is NOT in the map, so HasSession returns false
				},
			})

			// Create context with a stale session ID (not in SessionManager)
			ctx := withMockSession(context.Background(), "stale-session")

			// Create request
			req := mcp.CallToolRequest{}
			req.Params.Name = tt.toolName

			// Call through HandleToolCall (not the individual handler)
			result, err := handler.HandleToolCall(ctx, req)

			// Should return SessionExpiredError
			require.Error(t, err)
			require.Nil(t, result)

			// Verify it's a SessionExpiredError
			var sessionErr *SessionExpiredError
			require.ErrorAs(t, err, &sessionErr)

			// Verify the error message contains recovery instructions
			assert.Contains(t, sessionErr.Error(), "maybedont__discover_tools")
			assert.Contains(t, sessionErr.Error(), "Session expired")
			assert.Contains(t, sessionErr.SessionID, "stale-session")
		})
	}
}

// TestNativeTools_DiscoverToolsExemptFromSessionValidation verifies that discover_tools
// does NOT require a valid session in SessionManager (since it's the recovery mechanism).
func TestNativeTools_DiscoverToolsExemptFromSessionValidation(t *testing.T) {
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, "")

	// Set up mock session provider that reports session as invalid
	handler.SetSessionProvider(&mockSessionProvider{
		validSessions: map[string]bool{}, // Empty - no valid sessions
	})

	// Set up a mock discovery provider
	handler.SetDiscoveryProvider(&mockDiscoveryProvider{
		result: &DiscoveryResult{
			DiscoveredClients: []DiscoveredClientInfo{},
			AlreadyConnected:  []string{},
		},
	})

	// Create context with a session ID (even though it's not in SessionManager)
	ctx := withMockSession(context.Background(), "any-session")

	// Create request for discover_tools
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools

	// Call through HandleToolCall
	result, err := handler.HandleToolCall(ctx, req)

	// Should NOT return SessionExpiredError - discover_tools is exempt
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestNativeTools_ValidSessionAllowed verifies that native tools work when the session
// is valid in SessionManager.
func TestNativeTools_ValidSessionAllowed(t *testing.T) {
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	// Set up mock session provider with a valid session
	handler.SetSessionProvider(&mockSessionProvider{
		validSessions: map[string]bool{
			"valid-session": true,
		},
		sessions: []SessionInfo{
			{SessionID: "valid-session"},
		},
	})

	// Create context with a valid session ID
	ctx := withMockSession(context.Background(), "valid-session")

	// Create request
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions

	// Call through HandleToolCall
	result, err := handler.HandleToolCall(ctx, req)

	// Should succeed
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestNativeTools_NoSessionInContext verifies that native tools return SessionExpiredError
// when there's no session ID in context at all.
func TestNativeTools_NoSessionInContext(t *testing.T) {
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	// Set up mock session provider
	handler.SetSessionProvider(&mockSessionProvider{})

	// Create context WITHOUT a session ID
	ctx := context.Background()

	// Create request
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions

	// Call through HandleToolCall
	result, err := handler.HandleToolCall(ctx, req)

	// Should return SessionExpiredError
	require.Error(t, err)
	require.Nil(t, result)

	var sessionErr *SessionExpiredError
	require.ErrorAs(t, err, &sessionErr)
	assert.Contains(t, sessionErr.Error(), "maybedont__discover_tools")
	assert.Contains(t, sessionErr.Reason, "no session established")
}

// mockDiscoveryProvider is defined in discover_tools_tool_test.go
