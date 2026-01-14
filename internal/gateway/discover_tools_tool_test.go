package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDiscoveryProvider implements PassThroughDiscoveryProvider for testing
type mockDiscoveryProvider struct {
	result *DiscoveryResult
	err    error
}

func (m *mockDiscoveryProvider) DiscoverPassThroughTools(ctx context.Context, sessionID string, clientName string) (*DiscoveryResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestDiscoverTools_ToolDefinition(t *testing.T) {
	// Verify the tool definition is correctly configured
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, logger, "")

	tools := handler.GetTools()

	// Find discover_tools in the list
	var discoverTool *mcp.Tool
	for i := range tools {
		if tools[i].Name == ToolDiscoverTools {
			discoverTool = &tools[i]
			break
		}
	}

	require.NotNil(t, discoverTool, "discover_tools should be in the tools list")
	assert.Equal(t, ToolDiscoverTools, discoverTool.Name)
	assert.Contains(t, discoverTool.Description, "Discover tools")
	assert.NotNil(t, discoverTool.Annotations.ReadOnlyHint)
	assert.True(t, *discoverTool.Annotations.ReadOnlyHint)
}

func TestDiscoverTools_RequiresSession(t *testing.T) {
	// Verify that the tool returns an error when no session is available
	ctx := context.Background() // No session
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, logger, "")
	handler.SetDiscoveryProvider(&mockDiscoveryProvider{
		result: &DiscoveryResult{},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleDiscoverTools(ctx, req)
	require.NoError(t, err) // Returns error result, not error
	require.NotNil(t, result)
	require.True(t, result.IsError, "Should return error result when no session")

	// Check error message
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "No active session")
}

func TestDiscoverTools_RequiresDiscoveryProvider(t *testing.T) {
	// Verify that the tool returns an error when discovery provider is not set
	ctx := withMockSession(context.Background(), "test-session")
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, logger, "")
	// Don't set discovery provider

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleDiscoverTools(ctx, req)
	require.NoError(t, err) // Returns error result, not error
	require.NotNil(t, result)
	require.True(t, result.IsError, "Should return error result when provider not set")

	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "discovery provider not available")
}

func TestDiscoverTools_SuccessfulDiscovery(t *testing.T) {
	// Test successful discovery of pass-through tools
	ctx := withMockSession(context.Background(), "test-session-123")
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, logger, "")
	handler.SetDiscoveryProvider(&mockDiscoveryProvider{
		result: &DiscoveryResult{
			DiscoveredClients: []DiscoveredClientInfo{
				{
					ClientName: "github",
					ToolCount:  3,
					Tools:      []string{"create_issue", "search_code", "get_file_contents"},
				},
			},
			AlreadyConnected: []string{},
			Errors:           []DiscoveryError{},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleDiscoverTools(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	// Parse response
	var response DiscoveryResult
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify discovered clients
	require.Len(t, response.DiscoveredClients, 1)
	github := response.DiscoveredClients[0]
	assert.Equal(t, "github", github.ClientName)
	assert.Equal(t, 3, github.ToolCount)
	assert.Len(t, github.Tools, 3)
	assert.Contains(t, github.Tools, "create_issue")
	assert.Contains(t, github.Tools, "search_code")
	assert.Contains(t, github.Tools, "get_file_contents")

	assert.Empty(t, response.AlreadyConnected)
	assert.Empty(t, response.Errors)
}

func TestDiscoverTools_MultipleClients(t *testing.T) {
	// Test discovery of multiple pass-through clients
	ctx := withMockSession(context.Background(), "test-session")
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, logger, "")
	handler.SetDiscoveryProvider(&mockDiscoveryProvider{
		result: &DiscoveryResult{
			DiscoveredClients: []DiscoveredClientInfo{
				{
					ClientName: "github",
					ToolCount:  2,
					Tools:      []string{"create_issue", "search_code"},
				},
				{
					ClientName: "gitlab",
					ToolCount:  1,
					Tools:      []string{"create_merge_request"},
				},
			},
			AlreadyConnected: []string{},
			Errors:           []DiscoveryError{},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools

	result, err := handler.handleDiscoverTools(ctx, req)
	require.NoError(t, err)
	require.False(t, result.IsError)

	var response DiscoveryResult
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	assert.Len(t, response.DiscoveredClients, 2)
}

func TestDiscoverTools_AlreadyConnectedClients(t *testing.T) {
	// Test that already connected clients are reported correctly
	ctx := withMockSession(context.Background(), "test-session")
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, logger, "")
	handler.SetDiscoveryProvider(&mockDiscoveryProvider{
		result: &DiscoveryResult{
			DiscoveredClients: []DiscoveredClientInfo{},
			AlreadyConnected:  []string{"github", "gitlab"},
			Errors:            []DiscoveryError{},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools

	result, err := handler.handleDiscoverTools(ctx, req)
	require.NoError(t, err)
	require.False(t, result.IsError)

	var response DiscoveryResult
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	assert.Empty(t, response.DiscoveredClients)
	assert.Len(t, response.AlreadyConnected, 2)
	assert.Contains(t, response.AlreadyConnected, "github")
	assert.Contains(t, response.AlreadyConnected, "gitlab")
}

func TestDiscoverTools_WithErrors(t *testing.T) {
	// Test that discovery errors are reported correctly
	ctx := withMockSession(context.Background(), "test-session")
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, logger, "")
	handler.SetDiscoveryProvider(&mockDiscoveryProvider{
		result: &DiscoveryResult{
			DiscoveredClients: []DiscoveredClientInfo{
				{
					ClientName: "github",
					ToolCount:  2,
					Tools:      []string{"create_issue", "search_code"},
				},
			},
			AlreadyConnected: []string{},
			Errors: []DiscoveryError{
				{
					ClientName: "gitlab",
					Error:      "connection refused",
				},
			},
		},
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools

	result, err := handler.handleDiscoverTools(ctx, req)
	require.NoError(t, err)
	require.False(t, result.IsError)

	var response DiscoveryResult
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Partial success: one client discovered, one error
	assert.Len(t, response.DiscoveredClients, 1)
	assert.Len(t, response.Errors, 1)
	assert.Equal(t, "gitlab", response.Errors[0].ClientName)
	assert.Equal(t, "connection refused", response.Errors[0].Error)
}

func TestDiscoverTools_FilterByClientName(t *testing.T) {
	// Test that the client parameter is passed to the discovery provider
	ctx := withMockSession(context.Background(), "test-session")
	logger := newTestLogger(t)

	cfg := &config.Config{}
	// Create a provider that verifies the client name is passed
	var capturedClientName string
	handler := NewNativeToolsHandler(cfg, logger, logger, "")
	handler.SetDiscoveryProvider(&mockDiscoveryProviderWithCapture{
		result: &DiscoveryResult{
			DiscoveredClients: []DiscoveredClientInfo{
				{
					ClientName: "github",
					ToolCount:  1,
					Tools:      []string{"create_issue"},
				},
			},
		},
		captureClientName: &capturedClientName,
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools
	req.Params.Arguments = map[string]interface{}{
		"client": "github",
	}

	result, err := handler.handleDiscoverTools(ctx, req)
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Verify the client name was passed to the provider
	assert.Equal(t, "github", capturedClientName)
}

func TestDiscoverTools_ProviderError(t *testing.T) {
	// Test that provider errors are handled correctly
	ctx := withMockSession(context.Background(), "test-session")
	logger := newTestLogger(t)

	cfg := &config.Config{}
	handler := NewNativeToolsHandler(cfg, logger, logger, "")
	handler.SetDiscoveryProvider(&mockDiscoveryProvider{
		err: errors.New("provider failed"),
	})

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolDiscoverTools

	result, err := handler.handleDiscoverTools(ctx, req)
	require.NoError(t, err) // Returns error result, not error
	require.NotNil(t, result)
	require.True(t, result.IsError)

	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "Failed to discover tools")
	assert.Contains(t, textContent.Text, "provider failed")
}

// mockDiscoveryProviderWithCapture captures the client name for verification
type mockDiscoveryProviderWithCapture struct {
	result            *DiscoveryResult
	captureClientName *string
}

func (m *mockDiscoveryProviderWithCapture) DiscoverPassThroughTools(ctx context.Context, sessionID string, clientName string) (*DiscoveryResult, error) {
	if m.captureClientName != nil {
		*m.captureClientName = clientName
	}
	return m.result, nil
}

func TestLazyDiscovery_PassThroughClientsNotAvailableAtStartup(t *testing.T) {
	// This test demonstrates the lazy discovery scenario:
	// 1. Pass-through auth clients are NOT discovered at startup
	// 2. AI agent sees discover_tools tool but not the downstream tools
	// 3. AI agent calls discover_tools with credentials
	// 4. Tools are then discovered and registered

	ctx := context.Background()
	logger := newTestLogger(t)

	// Step 1: Initialize client manager with pass-through config
	cm := NewClientManager(ctx, logger)
	githubConfig := config.ClientConfig{
		Type:          "http",
		DownstreamURL: "https://api.github.com/mcp",
	}
	githubConfig.Auth.PassThrough.Enabled = true
	githubConfig.Auth.PassThrough.Headers = []config.CredentialMapping{
		{SourceHeader: "X-GitHub-Token", TargetHeader: "Authorization"},
	}

	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	})
	require.NoError(t, err)

	// Step 2: Discover capabilities at startup (simulating gateway startup)
	discovered, err := cm.DiscoverAllCapabilities(ctx)
	require.NoError(t, err)

	// KEY ASSERTION: GitHub tools are NOT discovered at startup
	// because pass-through auth clients are skipped during startup probe
	_, hasGithub := discovered.Tools["github"]
	assert.False(t, hasGithub, "GitHub tools should NOT be discovered at startup (pass-through auth)")

	// Step 3: Verify the config is still available for lazy discovery
	configs := cm.GetClientConfigs()
	require.Contains(t, configs, "github", "GitHub config should still be available")
	assert.True(t, configs["github"].Auth.PassThrough.Enabled, "Pass-through should be enabled")

	// Step 4: At this point, an AI agent would:
	// - See maybedont__discover_tools in the tools list
	// - NOT see any github__* tools
	// - Call maybedont__discover_tools to trigger discovery
	// - After discovery, github__* tools become available

	// This is verified by the handler tests above
}

func TestLazyDiscovery_DiscoverToolsIsAlwaysVisible(t *testing.T) {
	// Verify that maybedont__discover_tools is always visible in the tools list,
	// even when no downstream tools have been discovered yet and all other
	// native tools are disabled
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = false
	cfg.NativeTools.AuditReport.Enabled = false
	cfg.NativeTools.ListServers.Enabled = false
	cfg.NativeTools.ListSessions.Enabled = false

	handler := NewNativeToolsHandler(cfg, logger, logger, "")

	// Get tools - discover_tools is always enabled
	tools := handler.GetTools()

	// Should have exactly one tool: discover_tools (always enabled)
	require.Len(t, tools, 1)
	assert.Equal(t, ToolDiscoverTools, tools[0].Name)

	// Verify the description helps AI understand what the tool does
	assert.Contains(t, tools[0].Description, "Discover tools")
	assert.Contains(t, tools[0].Description, "authentication")
}

func TestDiscoveryResult_JSONStructure(t *testing.T) {
	// Verify the JSON structure of DiscoveryResult for AI consumption
	result := &DiscoveryResult{
		DiscoveredClients: []DiscoveredClientInfo{
			{
				ClientName: "github",
				ToolCount:  45,
				Tools:      []string{"create_issue", "search_code", "list_repos"},
			},
		},
		AlreadyConnected: []string{"aws"},
		Errors: []DiscoveryError{
			{
				ClientName: "gitlab",
				Error:      "authentication failed",
			},
		},
	}

	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)

	// Parse back to verify structure
	var parsed DiscoveryResult
	err = json.Unmarshal(jsonBytes, &parsed)
	require.NoError(t, err)

	assert.Len(t, parsed.DiscoveredClients, 1)
	assert.Equal(t, "github", parsed.DiscoveredClients[0].ClientName)
	assert.Equal(t, 45, parsed.DiscoveredClients[0].ToolCount)

	assert.Len(t, parsed.AlreadyConnected, 1)
	assert.Equal(t, "aws", parsed.AlreadyConnected[0])

	assert.Len(t, parsed.Errors, 1)
	assert.Equal(t, "gitlab", parsed.Errors[0].ClientName)
}
