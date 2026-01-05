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

// mockClientConfigProvider implements ClientConfigProvider for testing
type mockClientConfigProvider struct {
	configs map[string]config.ClientConfig
}

func (m *mockClientConfigProvider) GetClientConfigs() map[string]config.ClientConfig {
	return m.configs
}

// mockToolsProvider implements RegisteredToolsProvider for testing
type mockToolsProvider struct {
	tools []string
}

func (m *mockToolsProvider) ListRegisteredTools() []string {
	return m.tools
}

func TestListDownstreamServers_BasicResponse(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create handler with mock providers
	cfg := &config.Config{}
	cfg.NativeTools.ListServers.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, logger)

	// Set up mock providers
	handler.SetClientConfigProvider(&mockClientConfigProvider{
		configs: map[string]config.ClientConfig{
			"github": {
				Type:          "http",
				DownstreamURL: "https://api.githubcopilot.com/mcp/",
			},
			"aws-docs": {
				Type:    "stdio",
				Command: "uvx",
				Args:    []string{"awslabs.aws-documentation-mcp-server@latest"},
			},
		},
	})

	handler.SetToolsProvider(&mockToolsProvider{
		tools: []string{
			"github__create_issue",
			"github__search_code",
			"aws-docs__search_documentation",
			"aws-docs__read_documentation",
			"maybedont__list_downstream_servers", // Should be excluded from counts
		},
	})

	// Create request
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListDownstreamServers
	req.Params.Arguments = map[string]interface{}{}

	// Call handler
	result, err := handler.handleListDownstreamServers(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	// Parse response
	var response ListDownstreamServersResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify response
	assert.Equal(t, 2, response.TotalServers)
	assert.Equal(t, 4, response.TotalTools) // Excludes native tools

	// Find servers by name
	var github, awsDocs *DownstreamServerInfo
	for i := range response.Servers {
		switch response.Servers[i].Name {
		case "github":
			github = &response.Servers[i]
		case "aws-docs":
			awsDocs = &response.Servers[i]
		}
	}

	require.NotNil(t, github)
	assert.Equal(t, "http", github.Type)
	assert.Equal(t, "https://api.githubcopilot.com/mcp/", github.URL)
	assert.Equal(t, 2, github.ToolsCount)
	assert.True(t, github.ToolsDiscovered)
	assert.Empty(t, github.Tools) // Not requested

	require.NotNil(t, awsDocs)
	assert.Equal(t, "stdio", awsDocs.Type)
	assert.Equal(t, "uvx awslabs.aws-documentation-mcp-server@latest", awsDocs.Command)
	assert.Equal(t, 2, awsDocs.ToolsCount)
	assert.True(t, awsDocs.ToolsDiscovered)
	assert.Empty(t, awsDocs.Tools) // Not requested
}

func TestListDownstreamServers_IncludeTools(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListServers.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, logger)

	handler.SetClientConfigProvider(&mockClientConfigProvider{
		configs: map[string]config.ClientConfig{
			"github": {
				Type:          "http",
				DownstreamURL: "https://api.githubcopilot.com/mcp/",
			},
		},
	})

	handler.SetToolsProvider(&mockToolsProvider{
		tools: []string{
			"github__create_issue",
			"github__search_code",
		},
	})

	// Create request with include_tools=true
	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListDownstreamServers
	req.Params.Arguments = map[string]interface{}{
		"include_tools": true,
	}

	result, err := handler.handleListDownstreamServers(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Parse response
	var response ListDownstreamServersResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify tools are included (without prefix)
	require.Len(t, response.Servers, 1)
	github := response.Servers[0]
	assert.Len(t, github.Tools, 2)
	assert.Contains(t, github.Tools, "create_issue")
	assert.Contains(t, github.Tools, "search_code")
}

func TestListDownstreamServers_NoToolsDiscovered(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListServers.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, logger)

	// Server configured but no tools discovered (e.g., pass-through auth)
	handler.SetClientConfigProvider(&mockClientConfigProvider{
		configs: map[string]config.ClientConfig{
			"github": {
				Type:          "http",
				DownstreamURL: "https://api.githubcopilot.com/mcp/",
			},
		},
	})

	handler.SetToolsProvider(&mockToolsProvider{
		tools: []string{}, // No tools registered
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListDownstreamServers
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleListDownstreamServers(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	var response ListDownstreamServersResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	require.Len(t, response.Servers, 1)
	github := response.Servers[0]
	assert.Equal(t, 0, github.ToolsCount)
	assert.False(t, github.ToolsDiscovered)
}

func TestListDownstreamServers_NoProviderError(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListServers.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, logger)
	// Don't set providers

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListDownstreamServers

	result, err := handler.handleListDownstreamServers(ctx, req)
	require.NoError(t, err) // Returns error result, not error
	require.NotNil(t, result)
	require.True(t, result.IsError)
}

func TestListDownstreamServers_SessionSpecificTools(t *testing.T) {
	// This test verifies that session-specific tools (from pass-through auth clients)
	// are included in the tool count, not just globally registered tools.
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListServers.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, logger)

	// Configure two servers: one with global tools, one with pass-through (no global tools)
	handler.SetClientConfigProvider(&mockClientConfigProvider{
		configs: map[string]config.ClientConfig{
			"aws-docs": {
				Type:          "http",
				DownstreamURL: "https://knowledge-mcp.global.api.aws",
			},
			"github": {
				Type:          "http",
				DownstreamURL: "https://api.githubcopilot.com/mcp/",
				// In real usage, this would have pass-through auth enabled
			},
		},
	})

	// Only aws-docs has globally registered tools (discovered at startup)
	// GitHub has no global tools (pass-through auth skipped startup probe)
	handler.SetToolsProvider(&mockToolsProvider{
		tools: []string{
			"aws-docs__search_documentation",
			"aws-docs__read_documentation",
		},
	})

	// Set up session provider with GitHub tools discovered during session
	handler.SetSessionProvider(&mockSessionProvider{
		clientTools: map[string][]SessionClientTools{
			"test-session-123": {
				{
					ClientName: "github",
					Tools:      []string{"create_issue", "search_code", "get_file_contents"},
				},
			},
		},
	})

	// Create context with session ID (simulating a real session)
	ctx := withMockSession(context.Background(), "test-session-123")

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListDownstreamServers
	req.Params.Arguments = map[string]interface{}{
		"include_tools": true,
	}

	result, err := handler.handleListDownstreamServers(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	var response ListDownstreamServersResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify total counts include both global and session-specific tools
	assert.Equal(t, 2, response.TotalServers)
	assert.Equal(t, 5, response.TotalTools) // 2 aws-docs + 3 github

	// Find servers by name
	var github, awsDocs *DownstreamServerInfo
	for i := range response.Servers {
		switch response.Servers[i].Name {
		case "github":
			github = &response.Servers[i]
		case "aws-docs":
			awsDocs = &response.Servers[i]
		}
	}

	// GitHub should show session-specific tools
	require.NotNil(t, github)
	assert.Equal(t, 3, github.ToolsCount)
	assert.True(t, github.ToolsDiscovered)
	assert.Len(t, github.Tools, 3)
	assert.Contains(t, github.Tools, "create_issue")
	assert.Contains(t, github.Tools, "search_code")
	assert.Contains(t, github.Tools, "get_file_contents")

	// AWS should show globally registered tools
	require.NotNil(t, awsDocs)
	assert.Equal(t, 2, awsDocs.ToolsCount)
	assert.True(t, awsDocs.ToolsDiscovered)
}

func TestListDownstreamServers_SessionToolsMergedWithGlobal(t *testing.T) {
	// This test verifies that when a client has both global and session-specific tools,
	// they are merged without duplicates.
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListServers.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, logger)

	handler.SetClientConfigProvider(&mockClientConfigProvider{
		configs: map[string]config.ClientConfig{
			"github": {
				Type:          "http",
				DownstreamURL: "https://api.githubcopilot.com/mcp/",
			},
		},
	})

	// Some tools registered globally
	handler.SetToolsProvider(&mockToolsProvider{
		tools: []string{
			"github__create_issue",
			"github__search_code",
		},
	})

	// Session has overlapping + additional tools
	handler.SetSessionProvider(&mockSessionProvider{
		clientTools: map[string][]SessionClientTools{
			"test-session": {
				{
					ClientName: "github",
					Tools:      []string{"create_issue", "get_file_contents"}, // create_issue is duplicate
				},
			},
		},
	})

	ctx := withMockSession(context.Background(), "test-session")

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListDownstreamServers
	req.Params.Arguments = map[string]interface{}{
		"include_tools": true,
	}

	result, err := handler.handleListDownstreamServers(ctx, req)
	require.NoError(t, err)

	var response ListDownstreamServersResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Should have 3 unique tools (no duplicates)
	require.Len(t, response.Servers, 1)
	github := response.Servers[0]
	assert.Equal(t, 3, github.ToolsCount)
	assert.Len(t, github.Tools, 3)
	assert.Contains(t, github.Tools, "create_issue")
	assert.Contains(t, github.Tools, "search_code")
	assert.Contains(t, github.Tools, "get_file_contents")
}

func TestListDownstreamServers_NoServersConfigured(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.ListServers.Enabled = true

	handler := NewNativeToolsHandler(cfg, logger, logger)

	// Set up mock providers with empty configs
	handler.SetClientConfigProvider(&mockClientConfigProvider{
		configs: map[string]config.ClientConfig{}, // No servers configured
	})

	handler.SetToolsProvider(&mockToolsProvider{
		tools: []string{}, // No tools registered
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolListDownstreamServers
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleListDownstreamServers(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	// Parse response
	var response ListDownstreamServersResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify response indicates no servers
	assert.Equal(t, 0, response.TotalServers)
	assert.Equal(t, 0, response.TotalTools)
	assert.Empty(t, response.Servers)

	// Verify helpful message is included
	assert.NotEmpty(t, response.Message, "Response should include a message when no servers are configured")
	assert.Contains(t, response.Message, "No downstream MCP servers")
}

// withMockSession creates a context with a mock session ID for testing.
// This uses a test-specific context key that GetSessionIDFromContext checks.
func withMockSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, testSessionIDKey, sessionID)
}
