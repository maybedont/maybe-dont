package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSessionProvider implements SessionProvider for testing
type mockSessionProvider struct {
	sessions      []SessionInfo
	clientTools   map[string][]SessionClientTools // sessionID -> client tools
	validSessions map[string]bool                 // sessions that exist in SessionManager
}

func (m *mockSessionProvider) GetActiveSessions() []SessionInfo {
	return m.sessions
}

func (m *mockSessionProvider) GetSessionClientTools(sessionID string) []SessionClientTools {
	if m.clientTools == nil {
		return nil
	}
	return m.clientTools[sessionID]
}

func (m *mockSessionProvider) HasSession(sessionID string) bool {
	if m.validSessions == nil {
		// Default: derive from sessions list
		for _, s := range m.sessions {
			if s.SessionID == sessionID {
				return true
			}
		}
		return false
	}
	return m.validSessions[sessionID]
}

// TestListSessions_DefaultShowsOnlyConnected verifies that the default behavior (include_disconnected=false)
// only returns sessions with an active SSE connection, while reporting disconnected count separately.
func TestListSessions_DefaultShowsOnlyConnected(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "session-connected-1",
				ClientIP:  "192.168.1.100",
				UserAgent: "Claude-Code/1.0.0",
				Connected: true,
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
					{Name: "aws-docs", ToolCount: 12},
				},
			},
			{
				SessionID: "session-disconnected-1",
				Connected: false,
				// No IP, no UA, no downstream clients (phantom from GET cycling)
			},
			{
				SessionID: "session-connected-2",
				ClientIP:  "10.0.0.50",
				UserAgent: "MCP-Client/2.0",
				Connected: true,
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 3},
				},
			},
			{
				SessionID: "session-disconnected-2",
				Connected: false,
				// Another disconnected session
			},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	var response ListSessionsResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Only connected sessions should be in the list
	require.Len(t, response.Sessions, 2)
	assert.Equal(t, "session-connected-1", response.Sessions[0].SessionID)
	assert.Equal(t, "session-connected-2", response.Sessions[1].SessionID)

	// Counts should reflect all sessions
	assert.Equal(t, 2, response.ConnectedCount)
	assert.Equal(t, 2, response.DisconnectedCount)
	assert.Equal(t, 4, response.TotalSessions)
}

// TestListSessions_IncludeDisconnected verifies that setting include_disconnected=true returns all sessions.
func TestListSessions_IncludeDisconnected(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "session-connected",
				ClientIP:  "192.168.1.100",
				Connected: true,
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
				},
			},
			{
				SessionID: "session-disconnected",
				Connected: false,
			},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{
		"include_disconnected": true,
	}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	var response ListSessionsResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// All sessions should be returned
	require.Len(t, response.Sessions, 2)
	assert.Equal(t, "session-connected", response.Sessions[0].SessionID)
	assert.Equal(t, "session-disconnected", response.Sessions[1].SessionID)

	// Counts
	assert.Equal(t, 1, response.ConnectedCount)
	assert.Equal(t, 1, response.DisconnectedCount)
	assert.Equal(t, 2, response.TotalSessions)
}

// TestListSessions_AllDisconnected verifies behavior when all sessions are disconnected.
func TestListSessions_AllDisconnected(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{SessionID: "disconnected-1", Connected: false},
			{SessionID: "disconnected-2", Connected: false},
			{SessionID: "disconnected-3", Connected: false},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)

	var response ListSessionsResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// No sessions in list (all disconnected, default excludes them)
	assert.Empty(t, response.Sessions)
	assert.Equal(t, 0, response.ConnectedCount)
	assert.Equal(t, 3, response.DisconnectedCount)
	assert.Equal(t, 3, response.TotalSessions)
}

// TestListSessions_EmptyResponse verifies behavior when there are no sessions at all.
func TestListSessions_EmptyResponse(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	var response ListSessionsResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	assert.Equal(t, 0, response.TotalSessions)
	assert.Equal(t, 0, response.ConnectedCount)
	assert.Equal(t, 0, response.DisconnectedCount)
	assert.Empty(t, response.Sessions)
}

// TestListSessions_NoProviderError verifies an error result when no session provider is set.
func TestListSessions_NoProviderError(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")
	// Don't set session provider

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err) // Returns error result, not error
	require.NotNil(t, result)
	require.True(t, result.IsError)
}

// TestListSessions_SortedBySessionID verifies sessions are returned sorted alphabetically by ID.
func TestListSessions_SortedBySessionID(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "zzz-session",
				Connected: true,
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
				},
			},
			{
				SessionID: "aaa-session",
				Connected: true,
				DownstreamClients: []DownstreamClientInfo{
					{Name: "aws-docs", ToolCount: 10},
				},
			},
			{
				SessionID: "mmm-session",
				Connected: true,
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
					{Name: "aws-docs", ToolCount: 10},
				},
			},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	var response ListSessionsResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// All 3 sessions are connected, so all should appear sorted
	require.Len(t, response.Sessions, 3)
	assert.Equal(t, "aaa-session", response.Sessions[0].SessionID)
	assert.Equal(t, "mmm-session", response.Sessions[1].SessionID)
	assert.Equal(t, "zzz-session", response.Sessions[2].SessionID)
}

// TestListSessions_UserAgentOmittedWhenEmpty verifies that user_agent is omitted from JSON
// when it's empty, thanks to the omitempty tag on SessionInfo.
func TestListSessions_UserAgentOmittedWhenEmpty(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "session-no-ua",
				ClientIP:  "192.168.1.100",
				UserAgent: "", // Empty User-Agent
				Connected: true,
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
				},
			},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)

	// Verify that "user_agent" key is NOT present in JSON output when empty (omitempty)
	assert.NotContains(t, textContent.Text, "user_agent")

	// Also verify the response parses correctly
	var response ListSessionsResponse
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)
	assert.Equal(t, 1, response.ConnectedCount)
	assert.Equal(t, 0, response.DisconnectedCount)
	assert.Equal(t, 1, response.TotalSessions)
	assert.Empty(t, response.Sessions[0].UserAgent)
}

// TestListSessions_ConnectedWithoutDownstreamClients verifies that a session with Connected=true
// but no downstream clients still appears as connected. This is the key behavioral change:
// "connected" refers to the SSE connection state, not whether downstream clients exist.
func TestListSessions_ConnectedWithoutDownstreamClients(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "session-connected-no-clients",
				ClientIP:  "192.168.1.100",
				Connected: true,
				// No downstream clients
			},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)

	var response ListSessionsResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Session should appear as connected even without downstream clients
	require.Len(t, response.Sessions, 1)
	assert.Equal(t, "session-connected-no-clients", response.Sessions[0].SessionID)
	assert.True(t, response.Sessions[0].Connected)
	assert.Equal(t, 1, response.ConnectedCount)
	assert.Equal(t, 0, response.DisconnectedCount)
}

// TestListSessions_DisconnectedWithDownstreamClients verifies that a session with Connected=false
// but WITH downstream clients still appears as disconnected. The SSE connection state is the
// authoritative signal, not the presence of downstream clients.
func TestListSessions_DisconnectedWithDownstreamClients(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "session-disconnected-with-clients",
				ClientIP:  "192.168.1.100",
				Connected: false,
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
				},
			},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)

	var response ListSessionsResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Session should NOT appear by default (disconnected)
	assert.Empty(t, response.Sessions)
	assert.Equal(t, 0, response.ConnectedCount)
	assert.Equal(t, 1, response.DisconnectedCount)

	// But should appear with include_disconnected=true
	req.Params.Arguments = map[string]any{"include_disconnected": true}
	result, err = handler.handleListSessions(ctx, req)
	require.NoError(t, err)

	textContent, ok = mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	require.Len(t, response.Sessions, 1)
	assert.Equal(t, "session-disconnected-with-clients", response.Sessions[0].SessionID)
	assert.False(t, response.Sessions[0].Connected)
	assert.Len(t, response.Sessions[0].DownstreamClients, 1)
}
