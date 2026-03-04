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

// TestListSessions_DefaultShowsOnlyActive verifies that the default behavior (include_inactive=false)
// only returns sessions with downstream clients, while reporting inactive count separately.
func TestListSessions_DefaultShowsOnlyActive(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "session-active-1",
				ClientIP:  "192.168.1.100",
				UserAgent: "Claude-Code/1.0.0",
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
					{Name: "aws-docs", ToolCount: 12},
				},
			},
			{
				SessionID: "session-phantom-1",
				// No IP, no UA, no downstream clients (phantom from GET cycling)
			},
			{
				SessionID: "session-active-2",
				ClientIP:  "10.0.0.50",
				UserAgent: "MCP-Client/2.0",
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 3},
				},
			},
			{
				SessionID: "session-phantom-2",
				// Another phantom session
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

	// Only active sessions should be in the list
	require.Len(t, response.Sessions, 2)
	assert.Equal(t, "session-active-1", response.Sessions[0].SessionID)
	assert.Equal(t, "session-active-2", response.Sessions[1].SessionID)

	// Counts should reflect all sessions
	assert.Equal(t, 2, response.ActiveSessionCount)
	assert.Equal(t, 2, response.InactiveSessionCount)
	assert.Equal(t, 4, response.TotalSessions)
}

// TestListSessions_IncludeInactive verifies that setting include_inactive=true returns all sessions.
func TestListSessions_IncludeInactive(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "session-active",
				ClientIP:  "192.168.1.100",
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
				},
			},
			{
				SessionID: "session-phantom",
			},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]any{
		"include_inactive": true,
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
	assert.Equal(t, "session-active", response.Sessions[0].SessionID)
	assert.Equal(t, "session-phantom", response.Sessions[1].SessionID)

	// Counts
	assert.Equal(t, 1, response.ActiveSessionCount)
	assert.Equal(t, 1, response.InactiveSessionCount)
	assert.Equal(t, 2, response.TotalSessions)
}

// TestListSessions_AllInactive verifies behavior when all sessions are inactive (no downstream clients).
func TestListSessions_AllInactive(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{SessionID: "phantom-1"},
			{SessionID: "phantom-2"},
			{SessionID: "phantom-3"},
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

	// No sessions in list (all inactive, default excludes them)
	assert.Empty(t, response.Sessions)
	assert.Equal(t, 0, response.ActiveSessionCount)
	assert.Equal(t, 3, response.InactiveSessionCount)
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
	assert.Equal(t, 0, response.ActiveSessionCount)
	assert.Equal(t, 0, response.InactiveSessionCount)
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
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
				},
			},
			{
				SessionID: "aaa-session",
				DownstreamClients: []DownstreamClientInfo{
					{Name: "aws-docs", ToolCount: 10},
				},
			},
			{
				SessionID: "mmm-session",
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

	// All 3 sessions are active (have downstream clients), so all should appear sorted
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
	assert.Equal(t, 1, response.ActiveSessionCount)
	assert.Equal(t, 0, response.InactiveSessionCount)
	assert.Equal(t, 1, response.TotalSessions)
	assert.Empty(t, response.Sessions[0].UserAgent)
}

// TestListSessions_HasDownstreamClients verifies the HasDownstreamClients helper method.
func TestListSessions_HasDownstreamClients(t *testing.T) {
	tests := []struct {
		name     string
		session  SessionInfo
		expected bool
	}{
		{
			name: "has clients",
			session: SessionInfo{
				DownstreamClients: []DownstreamClientInfo{{Name: "github", ToolCount: 5}},
			},
			expected: true,
		},
		{
			name:     "nil clients",
			session:  SessionInfo{},
			expected: false,
		},
		{
			name: "empty clients",
			session: SessionInfo{
				DownstreamClients: []DownstreamClientInfo{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.session.HasDownstreamClients())
		})
	}
}
