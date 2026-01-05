package gateway

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// DownstreamServerInfo represents information about a downstream MCP server
type DownstreamServerInfo struct {
	Name            string   `json:"name"`
	Type            string   `json:"type"`
	URL             string   `json:"url,omitempty"`
	Command         string   `json:"command,omitempty"`
	ToolsDiscovered bool     `json:"tools_discovered"`
	ToolsCount      int      `json:"tools_count"`
	Tools           []string `json:"tools,omitempty"`
}

// ListDownstreamServersResponse is the response for the list_downstream_servers tool
type ListDownstreamServersResponse struct {
	Servers      []DownstreamServerInfo `json:"servers"`
	TotalServers int                    `json:"total_servers"`
	TotalTools   int                    `json:"total_tools"`
	Message      string                 `json:"message,omitempty"`
}

// getListDownstreamServersToolDefinition returns the MCP tool definition for list_downstream_servers
func (h *NativeToolsHandler) getListDownstreamServersToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name:        ToolListDownstreamServers,
		Description: "List all configured downstream MCP servers connected through this gateway, including their connection type and available tools count.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"include_tools": map[string]interface{}{
					"type":        "boolean",
					"description": "Include list of tool names for each server (default: false)",
					"default":     false,
				},
			},
		},
	}
}

// handleListDownstreamServers handles the maybedont__list_downstream_servers tool call
func (h *NativeToolsHandler) handleListDownstreamServers(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Debug(ctx, "Handling list_downstream_servers request")

	// Parse parameters
	includeTools := false
	if args, ok := req.Params.Arguments.(map[string]interface{}); ok && args != nil {
		if val, ok := args["include_tools"].(bool); ok {
			includeTools = val
		}
	}

	// Check if providers are available
	if h.clientConfigProvider == nil {
		h.logger.Error(ctx, "Client config provider not set")
		return mcp.NewToolResultError("Internal error: client config provider not available"), nil
	}

	// Get client configurations
	configs := h.clientConfigProvider.GetClientConfigs()

	// Get registered tools if we have a provider (these are globally registered tools)
	var registeredTools []string
	if h.toolsProvider != nil {
		registeredTools = h.toolsProvider.ListRegisteredTools()
	}

	// Build tool map by server name for quick lookup
	toolsByServer := make(map[string][]string)
	for _, toolName := range registeredTools {
		// Skip native tools
		if IsNativeTool(toolName) {
			continue
		}
		// Parse prefixed name to get server name
		serverName, originalToolName, err := ParsePrefixedName(toolName)
		if err != nil {
			continue
		}
		toolsByServer[serverName] = append(toolsByServer[serverName], originalToolName)
	}

	// Also include session-specific tools (for pass-through auth clients)
	// These are discovered when a session connects with credentials
	if h.sessionProvider != nil {
		sessionID, hasSession := GetSessionIDFromContext(ctx)
		if hasSession {
			sessionTools := h.sessionProvider.GetSessionClientTools(sessionID)
			for _, clientTools := range sessionTools {
				// Merge with existing tools, avoiding duplicates
				existingTools := make(map[string]bool)
				for _, t := range toolsByServer[clientTools.ClientName] {
					existingTools[t] = true
				}
				for _, toolName := range clientTools.Tools {
					if !existingTools[toolName] {
						toolsByServer[clientTools.ClientName] = append(toolsByServer[clientTools.ClientName], toolName)
					}
				}
			}
		}
	}

	// Build response
	var servers []DownstreamServerInfo
	totalTools := 0

	// Sort server names for consistent output
	serverNames := make([]string, 0, len(configs))
	for name := range configs {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	for _, name := range serverNames {
		cfg := configs[name]
		serverTools := toolsByServer[name]
		toolsCount := len(serverTools)
		totalTools += toolsCount

		info := DownstreamServerInfo{
			Name:            name,
			Type:            cfg.Type,
			ToolsDiscovered: toolsCount > 0,
			ToolsCount:      toolsCount,
		}

		// Set URL or command based on type
		switch cfg.Type {
		case "http", "sse":
			url := cfg.DownstreamURL
			if url == "" {
				url = cfg.URL
			}
			info.URL = url
		case "stdio":
			// Build command string
			if len(cfg.CommandArgs) > 0 || len(cfg.Args) > 0 {
				args := cfg.CommandArgs
				if len(args) == 0 {
					args = cfg.Args
				}
				info.Command = cfg.Command + " " + strings.Join(args, " ")
			} else {
				info.Command = cfg.Command
			}
		}

		// Include tool names if requested
		if includeTools && len(serverTools) > 0 {
			sort.Strings(serverTools)
			info.Tools = serverTools
		}

		servers = append(servers, info)
	}

	response := ListDownstreamServersResponse{
		Servers:      servers,
		TotalServers: len(servers),
		TotalTools:   totalTools,
	}

	// Add helpful message when no servers are configured
	if len(servers) == 0 {
		response.Message = "No downstream MCP servers are configured. Add servers to the 'downstream_mcp_servers' section of your configuration file."
	}

	// Marshal response to JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		h.logger.Error(ctx, "Failed to marshal response", zap.Error(err))
		return mcp.NewToolResultError("Failed to format response"), nil
	}

	h.logger.Info(ctx, "Listed downstream servers",
		zap.Int("server_count", len(servers)),
		zap.Int("total_tools", totalTools))

	return mcp.NewToolResultText(string(jsonBytes)), nil
}
