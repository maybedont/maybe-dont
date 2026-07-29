package gateway

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// getDiscoverToolsDefinition returns the MCP tool definition for discover_tools
func (h *NativeToolsHandler) getDiscoverToolsDefinition() mcp.Tool {
	return mcp.Tool{
		Name: ToolDiscoverTools,
		Description: "Discover tools from downstream MCP servers that require authentication. " +
			"Call this to connect to servers like GitHub that need your credentials. " +
			"Returns the list of newly discovered tools that are now available for use.",
		DeferLoading: true,
		Annotations: mcp.ToolAnnotation{
			ReadOnlyHint: boolPtr(true),
		},
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"client": map[string]interface{}{
					"type":        "string",
					"description": "Optional: specific client to discover (e.g., 'github'). If omitted, discovers all pass-through clients.",
				},
			},
		},
	}
}

// handleDiscoverTools handles the maybedont__discover_tools tool call
func (h *NativeToolsHandler) handleDiscoverTools(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Parse parameters
	var clientName string
	if args, ok := req.Params.Arguments.(map[string]interface{}); ok && args != nil {
		if val, ok := args["client"].(string); ok {
			clientName = val
		}
	}

	// Check if discovery provider is available
	if h.discoveryProvider == nil {
		h.logger.Error(ctx, "Discovery provider not set")
		return mcp.NewToolResultError("Internal error: discovery provider not available"), nil
	}

	// Get session ID from context
	sessionID, hasSession := GetSessionIDFromContext(ctx)
	if !hasSession {
		h.logger.Error(ctx, "No session context available for discovery")
		return mcp.NewToolResultError("No active session. This tool requires an active MCP session to discover tools."), nil
	}

	// Trigger discovery
	result, err := h.discoveryProvider.DiscoverPassThroughTools(ctx, sessionID, clientName)
	if err != nil {
		h.logger.Error(ctx, "Failed to discover pass-through tools",
			zap.Error(err),
			zap.String("client", clientName))
		return mcp.NewToolResultError("Failed to discover tools: " + err.Error()), nil
	}

	// Log discovery results - use DEBUG for shared results (singleflight deduplication),
	// INFO only for the request that actually performed the discovery
	totalDiscovered := 0
	for _, client := range result.DiscoveredClients {
		totalDiscovered += client.ToolCount
	}
	logFields := []zap.Field{
		zap.String("session_id", sessionID),
		zap.Int("clients_discovered", len(result.DiscoveredClients)),
		zap.Int("total_tools", totalDiscovered),
		zap.Int("already_connected", len(result.AlreadyConnected)),
		zap.Int("errors", len(result.Errors)),
	}
	if result.Shared {
		h.logger.Debug(ctx, "Pass-through tool discovery completed (shared result)", logFields...)
	} else {
		h.logger.Info(ctx, "Pass-through tool discovery completed", logFields...)
	}

	// Marshal response to JSON
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		h.logger.Error(ctx, "Failed to marshal discovery result", zap.Error(err))
		return mcp.NewToolResultError("Failed to format response"), nil
	}

	return mcp.NewToolResultText(string(jsonBytes)), nil
}
