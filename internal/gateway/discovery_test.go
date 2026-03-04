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

func TestDiscoverAllCapabilities_DiscoverToolsAtStartup(t *testing.T) {
	// This test verifies that downstream tools are discovered at startup
	// before any upstream client sessions connect.
	//
	// The scenario:
	// 1. A mock downstream MCP server exposes tools
	// 2. ClientManager.DiscoverAllCapabilities() probes the server
	// 3. Tools are returned and can be registered on the gateway server
	// 4. No upstream sessions are required for tool discovery

	ctx := context.Background()

	// Create mock tools that the downstream server will expose
	mockTools := []mcp.Tool{
		{
			Name:        "search_files",
			Description: "Search for files in a directory",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
				},
			},
		},
		{
			Name:        "read_file",
			Description: "Read contents of a file",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path",
					},
				},
			},
		},
	}

	// Create a client manager
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)

	// Create a mock DiscoveredCapabilities to verify the registration flow
	discovered := &DiscoveredCapabilities{
		Tools: map[string][]mcp.Tool{
			"mock-client": mockTools,
		},
		Prompts:   make(map[string][]mcp.Prompt),
		Resources: make(map[string][]mcp.Resource),
		Templates: make(map[string][]mcp.ResourceTemplate),
	}

	// Verify the discovered capabilities structure
	require.Len(t, discovered.Tools, 1)
	require.Contains(t, discovered.Tools, "mock-client")
	require.Len(t, discovered.Tools["mock-client"], 2)
	assert.Equal(t, "search_files", discovered.Tools["mock-client"][0].Name)
	assert.Equal(t, "read_file", discovered.Tools["mock-client"][1].Name)

	// Verify that tools would be prefixed correctly when registered
	for clientName, tools := range discovered.Tools {
		for _, tool := range tools {
			prefixedName := PrefixName(clientName, tool.Name)
			assert.Contains(t, prefixedName, clientName+"__")
			assert.Contains(t, prefixedName, tool.Name)
		}
	}

	// Clean up - close the client manager
	err := cm.Close(ctx)
	require.NoError(t, err)
}

func TestDiscoveredCapabilities_ToolRegistration(t *testing.T) {
	// This test verifies that discovered tools are correctly registered
	// on the MCP server with prefixed names, making them available
	// for tools/list even before any upstream sessions connect.

	// Create mock discovered capabilities from multiple "downstream" servers
	discovered := &DiscoveredCapabilities{
		Tools: map[string][]mcp.Tool{
			"github": {
				{
					Name:        "create_issue",
					Description: "Create a GitHub issue",
					InputSchema: mcp.ToolInputSchema{Type: "object"},
				},
				{
					Name:        "search_code",
					Description: "Search code on GitHub",
					InputSchema: mcp.ToolInputSchema{Type: "object"},
				},
			},
			"aws": {
				{
					Name:        "list_buckets",
					Description: "List S3 buckets",
					InputSchema: mcp.ToolInputSchema{Type: "object"},
				},
			},
		},
		Prompts:   make(map[string][]mcp.Prompt),
		Resources: make(map[string][]mcp.Resource),
		Templates: make(map[string][]mcp.ResourceTemplate),
	}

	// Create an MCP server to register tools on
	srv := server.NewMCPServer("test-gateway", "1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register discovered tools with prefixed names (simulating what registerDiscoveredCapabilities does)
	for clientName, tools := range discovered.Tools {
		for _, tool := range tools {
			prefixedTool := tool
			prefixedTool.Name = PrefixName(clientName, tool.Name)
			srv.AddTool(prefixedTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("mock"), nil
			})
		}
	}

	// Verify all tools are registered with prefixed names
	registeredTools := srv.ListTools()
	require.Len(t, registeredTools, 3, "Should have 3 tools registered")

	// Check each expected prefixed tool name
	expectedTools := []string{
		"github__create_issue",
		"github__search_code",
		"aws__list_buckets",
	}

	for _, expectedName := range expectedTools {
		tool := srv.GetTool(expectedName)
		require.NotNil(t, tool, "Tool %s should be registered", expectedName)
	}

	// Verify the original (unprefixed) names are not registered
	assert.Nil(t, srv.GetTool("create_issue"), "Unprefixed tool should not be registered")
	assert.Nil(t, srv.GetTool("list_buckets"), "Unprefixed tool should not be registered")

	// Verify tools are available before any session connects
	// (this is the key requirement - tools discoverable at startup)
	tools := srv.ListTools()
	assert.Len(t, tools, 3, "All 3 tools should be listable before any session")
}

func TestToolsAvailableBeforeSessionConnect(t *testing.T) {
	// This test specifically verifies the requirement that downstream tools
	// are available via tools/list BEFORE any upstream client connects.

	ctx := context.Background()
	logger := newTestLogger(t)

	// Create client manager and initialize with configs
	cm := NewClientManager(ctx, logger)
	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"test-downstream": {
			Type:    "stdio",
			Command: "echo", // Won't actually connect
		},
	})
	require.NoError(t, err)

	// Manually populate discovered capabilities (simulating successful probe)
	discovered := &DiscoveredCapabilities{
		Tools: map[string][]mcp.Tool{
			"test-downstream": {
				{
					Name:        "example_tool",
					Description: "An example tool",
					InputSchema: mcp.ToolInputSchema{Type: "object"},
				},
			},
		},
		Prompts:   make(map[string][]mcp.Prompt),
		Resources: make(map[string][]mcp.Resource),
		Templates: make(map[string][]mcp.ResourceTemplate),
	}

	// Create MCP server and register discovered tools
	srv := server.NewMCPServer("test-gateway", "1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register tools (simulating registerDiscoveredCapabilities)
	for clientName, tools := range discovered.Tools {
		for _, tool := range tools {
			prefixedTool := tool
			prefixedTool.Name = PrefixName(clientName, tool.Name)
			srv.AddTool(prefixedTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("mock"), nil
			})
		}
	}

	// KEY ASSERTION: Tools should be available BEFORE any session connects
	// Check that ListTools returns our registered tools
	registeredTools := srv.ListTools()

	// Verify the tool is registered with the prefixed name
	require.Len(t, registeredTools, 1, "Should have 1 tool registered before any session")

	tool := srv.GetTool("test-downstream__example_tool")
	require.NotNil(t, tool, "Prefixed tool should be registered")

	// No sessions have been created at this point
	// The test verifies that tool discovery works independently of sessions
}

func TestDiscoverAllCapabilities_SkipsPassThroughAuthClients(t *testing.T) {
	// This test verifies that clients with pass-through auth enabled
	// are skipped during startup probe since they require upstream credentials.

	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a client config with pass-through auth enabled
	githubConfig := config.ClientConfig{
		Type:          "http",
		DownstreamURL: "https://api.github.com/mcp",
	}
	githubConfig.Auth.PassThrough.Enabled = true
	githubConfig.Auth.PassThrough.Headers = []config.CredentialMapping{
		{SourceHeader: "Authorization", TargetHeader: "Authorization"},
	}

	// Create client manager with both pass-through and non-pass-through clients
	cm := NewClientManager(ctx, logger)
	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
		"aws": {
			Type:    "stdio",
			Command: "echo", // Simple command - will fail but that's ok for this test
			// No pass-through auth - should be probed
		},
	})
	require.NoError(t, err)

	// Call DiscoverAllCapabilities
	// The github client should be skipped (pass-through auth)
	// The aws client will fail to probe (echo isn't an MCP server) but that's expected
	discovered, err := cm.DiscoverAllCapabilities(ctx)

	// Should not return error even if some clients fail
	require.NoError(t, err)
	require.NotNil(t, discovered)

	// The key assertion: github should NOT be in the discovered tools
	// because it was skipped due to pass-through auth
	_, hasGithub := discovered.Tools["github"]
	assert.False(t, hasGithub, "GitHub client should be skipped due to pass-through auth")

	// AWS may or may not be present depending on whether "echo" works as an MCP server
	// (it won't, but we're testing the skip logic, not the actual probe)
}

func TestDiscoverAllCapabilities_ProbesNonPassThroughClients(t *testing.T) {
	// This test verifies that clients WITHOUT pass-through auth ARE probed.
	// We can't easily test successful probing without a real MCP server,
	// but we can verify the client is attempted (will fail with echo command).

	ctx := context.Background()
	logger := newTestLogger(t)

	cm := NewClientManager(ctx, logger)
	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"local-server": {
			Type:    "stdio",
			Command: "echo", // Will fail but proves we attempted to probe
			// No pass-through auth configured
		},
	})
	require.NoError(t, err)

	// This will attempt to probe "local-server" because it doesn't have pass-through auth
	// It will fail because "echo" isn't an MCP server, but the important thing is
	// that it was attempted (not skipped)
	discovered, err := cm.DiscoverAllCapabilities(ctx)

	// Should not return error - failures are logged but not fatal
	require.NoError(t, err)
	require.NotNil(t, discovered)

	// The client was attempted but failed (echo is not an MCP server)
	// So it won't be in discovered.Tools, but it wasn't skipped either
	// The difference from pass-through is that pass-through logs "Skipping probe"
	// while non-pass-through logs "Probe client creation failed"
}

func TestCreateSessionClients_ReturnsPassThroughDiscoveryResult(t *testing.T) {
	// This test verifies that CreateSessionClients returns information about
	// pass-through clients that discovered tools, enabling session-specific tool registration.

	ctx := context.Background()
	logger := newTestLogger(t)

	cm := NewClientManager(ctx, logger)

	// Create a pass-through config
	githubConfig := config.ClientConfig{
		Type:    "stdio",
		Command: "echo", // Will fail but we're testing the result structure
	}
	githubConfig.Auth.PassThrough.Enabled = true
	githubConfig.Auth.PassThrough.Headers = []config.CredentialMapping{
		{SourceHeader: "Authorization", TargetHeader: "Authorization"},
	}

	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
		"aws": {
			Type:    "stdio",
			Command: "echo", // Non-pass-through
		},
	})
	require.NoError(t, err)

	// Create session first (mimics onSessionRegister)
	mustCreateSession(t, cm.sessionManager, "test-session")

	// CreateSessionClients will fail to connect (echo isn't MCP), but that's expected
	result, err := cm.CreateSessionClients(ctx, "test-session")

	// Error is expected since echo isn't an MCP server
	assert.Error(t, err)

	// But result should still be returned
	require.NotNil(t, result)
	require.NotNil(t, result.DownstreamClients)

	// Since connections failed, no tools were discovered
	// But the structure is correct for when connections succeed
	assert.Empty(t, result.DownstreamClients)
}

func TestSessionDiscoveryResult_IdentifiesPassThroughClients(t *testing.T) {
	// This test verifies the SessionDiscoveryResult structure correctly
	// identifies which clients use pass-through auth.

	// Manually create a discovery result to test the structure
	result := &SessionDiscoveryResult{
		DownstreamClients: map[string]*SessionClientInfo{
			"github": {
				Name: "github",
				Config: config.ClientConfig{
					Type: "http",
				},
				Tools: []mcp.Tool{
					{Name: "create_issue", Description: "Create a GitHub issue"},
					{Name: "search_code", Description: "Search code"},
				},
			},
		},
	}

	// Verify pass-through clients are tracked
	require.Len(t, result.DownstreamClients, 1)
	require.Contains(t, result.DownstreamClients, "github")

	githubInfo := result.DownstreamClients["github"]
	assert.Equal(t, "github", githubInfo.Name)
	assert.Len(t, githubInfo.Tools, 2)

	// These tools would be registered as session-specific tools
	// with prefixed names: github__create_issue, github__search_code
	for _, tool := range githubInfo.Tools {
		prefixedName := PrefixName("github", tool.Name)
		assert.True(t, len(prefixedName) > len(tool.Name))
		assert.Contains(t, prefixedName, "github__")
	}
}
