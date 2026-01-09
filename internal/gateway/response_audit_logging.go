package gateway

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// ResponseLoggingHandler logs response details for audit purposes
type ResponseLoggingHandler struct {
	auditLogger *config.SessionLogger
}

// NewResponseLoggingHandler creates a new response logging handler
func NewResponseLoggingHandler(auditLogger *config.SessionLogger) *ResponseLoggingHandler {
	return &ResponseLoggingHandler{
		auditLogger: auditLogger,
	}
}

// HandleResponse implements ResponseValidationHandler
func (h *ResponseLoggingHandler) HandleResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	// Log the response for audit purposes
	h.auditLogger.Info(ctx, "Response audit log",
		zap.String("tool_name", req.Params.Name),
		zap.Bool("is_error", result.IsError),
		zap.Int("content_items", len(result.Content)),
	)

	// Return audit result - always allows but provides audit trail
	return ResponseValidationResults{
		Results: []ResponseValidationResult{
			{
				PolicyName: "Response Audit Logging",
				PolicyType: "audit",
				Action:     config.PolicyActionAllow,
				Message:    "Response logged for audit",
			},
		},
		Allowed: true,
	}, nil
}
