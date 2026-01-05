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
	Sessions      []SessionInfo `json:"sessions"`
	TotalSessions int           `json:"total_sessions"`
}

// getListSessionsToolDefinition returns the MCP tool definition for list_sessions
func (h *NativeToolsHandler) getListSessionsToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name:        ToolListSessions,
		Description: "List all active upstream client sessions connected to this gateway, including their session IDs and connected downstream clients.",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
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

	// Get active sessions
	sessions := h.sessionProvider.GetActiveSessions()

	// Sort sessions by ID for consistent output
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].SessionID < sessions[j].SessionID
	})

	response := ListSessionsResponse{
		Sessions:      sessions,
		TotalSessions: len(sessions),
	}

	// Marshal response to JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		h.logger.Error(ctx, "Failed to marshal response", zap.Error(err))
		return mcp.NewToolResultError("Failed to format response"), nil
	}

	h.logger.Info(ctx, "Listed active sessions",
		zap.Int("session_count", len(sessions)))

	return mcp.NewToolResultText(string(jsonBytes)), nil
}
