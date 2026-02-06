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

func TestListSessions_BasicResponse(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create handler with mock providers
	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	// Set up mock session provider with multiple sessions
	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{
			{
				SessionID: "session-123",
				ClientIP:  "192.168.1.100",
				UserAgent: "Claude-Code/1.0.0",
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 5},
					{Name: "aws-docs", ToolCount: 12},
				},
			},
			{
				SessionID: "session-456",
				ClientIP:  "10.0.0.50",
				UserAgent: "MCP-Client/2.0",
				DownstreamClients: []DownstreamClientInfo{
					{Name: "github", ToolCount: 3},
				},
			},
		},
	})

	// Create request
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]interface{}{}

	// Call handler
	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	// Parse response
	var response ListSessionsResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify response
	assert.Equal(t, 2, response.TotalSessions)
	require.Len(t, response.Sessions, 2)

	// Sessions should be sorted by ID
	assert.Equal(t, "session-123", response.Sessions[0].SessionID)
	assert.Equal(t, "192.168.1.100", response.Sessions[0].ClientIP)
	assert.Equal(t, "Claude-Code/1.0.0", response.Sessions[0].UserAgent)
	require.Len(t, response.Sessions[0].DownstreamClients, 2)
	assert.Equal(t, "github", response.Sessions[0].DownstreamClients[0].Name)
	assert.Equal(t, 5, response.Sessions[0].DownstreamClients[0].ToolCount)
	assert.Equal(t, "aws-docs", response.Sessions[0].DownstreamClients[1].Name)
	assert.Equal(t, 12, response.Sessions[0].DownstreamClients[1].ToolCount)

	assert.Equal(t, "session-456", response.Sessions[1].SessionID)
	assert.Equal(t, "10.0.0.50", response.Sessions[1].ClientIP)
	assert.Equal(t, "MCP-Client/2.0", response.Sessions[1].UserAgent)
	require.Len(t, response.Sessions[1].DownstreamClients, 1)
	assert.Equal(t, "github", response.Sessions[1].DownstreamClients[0].Name)
	assert.Equal(t, 3, response.Sessions[1].DownstreamClients[0].ToolCount)
}

func TestListSessions_EmptyResponse(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	// Set up mock session provider with no sessions
	handler.SetSessionProvider(&mockSessionProvider{
		sessions: []SessionInfo{},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListSessions
	req.Params.Arguments = map[string]interface{}{}

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
	assert.Empty(t, response.Sessions)
}

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

func TestListSessions_SortedBySessionID(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	// Set up mock session provider with sessions in non-alphabetical order
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
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleListSessions(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	var response ListSessionsResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify sessions are sorted alphabetically by session ID
	require.Len(t, response.Sessions, 3)
	assert.Equal(t, "aaa-session", response.Sessions[0].SessionID)
	assert.Equal(t, "mmm-session", response.Sessions[1].SessionID)
	assert.Equal(t, "zzz-session", response.Sessions[2].SessionID)
}

func TestListSessions_UserAgentOmittedWhenEmpty(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListSessions.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, "")

	// Set up mock session provider with sessions that have no UserAgent
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
	assert.Equal(t, 1, response.TotalSessions)
	assert.Empty(t, response.Sessions[0].UserAgent)
}
