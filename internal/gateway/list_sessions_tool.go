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
	Sessions             []SessionInfo `json:"sessions"`
	ActiveSessionCount   int           `json:"active_session_count"`
	InactiveSessionCount int           `json:"inactive_session_count"`
	TotalSessions        int           `json:"total_sessions"`
}

// getListSessionsToolDefinition returns the MCP tool definition for list_sessions
func (h *NativeToolsHandler) getListSessionsToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name:         ToolListSessions,
		Description:  "[EXPERIMENTAL] List active upstream client sessions connected to this gateway. By default only sessions with downstream clients are shown; inactive sessions are reported as a count. Set include_inactive to true to include all sessions.",
		DeferLoading: true,
		Annotations: mcp.ToolAnnotation{
			ReadOnlyHint: boolPtr(true),
		},
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"include_inactive": map[string]any{
					"type":        "boolean",
					"description": "When true, include sessions with no downstream clients connected. Defaults to false.",
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

	// Parse include_inactive parameter
	includeInactive := false
	if args, ok := req.Params.Arguments.(map[string]any); ok {
		if val, exists := args["include_inactive"]; exists {
			if b, ok := val.(bool); ok {
				includeInactive = b
			}
		}
	}

	// Get all sessions
	allSessions := h.sessionProvider.GetActiveSessions()

	// Separate active (has downstream clients) from inactive
	var active, inactive []SessionInfo
	for _, s := range allSessions {
		if s.HasDownstreamClients() {
			active = append(active, s)
		} else {
			inactive = append(inactive, s)
		}
	}

	// Choose which sessions to include in the response
	sessions := active
	if includeInactive {
		sessions = allSessions
	}

	// Sort sessions by ID for consistent output
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].SessionID < sessions[j].SessionID
	})

	response := ListSessionsResponse{
		Sessions:             sessions,
		ActiveSessionCount:   len(active),
		InactiveSessionCount: len(inactive),
		TotalSessions:        len(allSessions),
	}

	// Marshal response to JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		h.logger.Error(ctx, "Failed to marshal response", zap.Error(err))
		return mcp.NewToolResultError("Failed to format response"), nil
	}

	h.logger.Info(ctx, "Listed sessions",
		zap.Int("active", len(active)),
		zap.Int("inactive", len(inactive)),
		zap.Bool("include_inactive", includeInactive))

	return mcp.NewToolResultText(string(jsonBytes)), nil
}
