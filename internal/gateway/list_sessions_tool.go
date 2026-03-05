package gateway

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// ListSessionsResponse is the response for the list_sessions tool
type ListSessionsResponse struct {
	Sessions          []SessionInfo `json:"sessions"`
	ConnectedCount    int           `json:"connected_count"`
	DisconnectedCount int           `json:"disconnected_count"`
	TotalSessions     int           `json:"total_sessions"`
}

// getListSessionsToolDefinition returns the MCP tool definition for list_sessions
func (h *NativeToolsHandler) getListSessionsToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name:         ToolListSessions,
		Description:  "[EXPERIMENTAL] List upstream client sessions connected to this gateway. By default only sessions with an active SSE connection are shown; disconnected sessions are reported as a count. Set include_disconnected to true to include all sessions.",
		DeferLoading: true,
		Annotations: mcp.ToolAnnotation{
			ReadOnlyHint: boolPtr(true),
		},
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"include_disconnected": map[string]any{
					"type":        "boolean",
					"description": "When true, include sessions with no active SSE connection. Defaults to false.",
				},
			},
		},
	}
}

// handleListSessions handles the maybedont__list_sessions tool call
func (h *NativeToolsHandler) handleListSessions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Debug(ctx, "Handling list_sessions request")

	// Check if session provider is available
	if h.sessionProvider == nil {
		h.logger.Error(ctx, "Session provider not set")
		return mcp.NewToolResultError("Internal error: session provider not available"), nil
	}

	// Parse include_disconnected parameter
	includeDisconnected := false
	if args, ok := req.Params.Arguments.(map[string]any); ok {
		if val, exists := args["include_disconnected"]; exists {
			if b, ok := val.(bool); ok {
				includeDisconnected = b
			}
		}
	}

	// Get all sessions
	allSessions := h.sessionProvider.GetActiveSessions()

	// Separate connected from disconnected
	var connected, disconnected []SessionInfo
	for _, s := range allSessions {
		if s.Connected {
			connected = append(connected, s)
		} else {
			disconnected = append(disconnected, s)
		}
	}

	// Choose which sessions to include in the response
	sessions := connected
	if includeDisconnected {
		sessions = allSessions
	}

	// Sort sessions by ID for consistent output
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].SessionID < sessions[j].SessionID
	})

	response := ListSessionsResponse{
		Sessions:          sessions,
		ConnectedCount:    len(connected),
		DisconnectedCount: len(disconnected),
		TotalSessions:     len(allSessions),
	}

	// Marshal response to JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		h.logger.Error(ctx, "Failed to marshal response", zap.Error(err))
		return mcp.NewToolResultError("Failed to format response"), nil
	}

	h.logger.Info(ctx, "Listed sessions",
		zap.Int("connected", len(connected)),
		zap.Int("disconnected", len(disconnected)),
		zap.Bool("include_disconnected", includeDisconnected))

	return mcp.NewToolResultText(string(jsonBytes)), nil
}
