package gateway

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// LoggingHandler logs tool call request details
type ToolLoggingHandler struct {
	auditLogger *zap.Logger
}

// NewLoggingHandler creates a new logging handler
func NewToolLoggingHandler(auditLogger *zap.Logger) *ToolLoggingHandler {
	return &ToolLoggingHandler{
		auditLogger: auditLogger,
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolLoggingHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	// Log the tool call for audit purposes
	h.auditLogger.Info("Tool call audit log",
		zap.String("method", req.Method),
		zap.String("tool_name", req.Params.Name),
		zap.Any("arguments", req.Params.Arguments),
	)

	// Return audit result - always allows but provides audit trail
	return ValidationResults{
		Results: []ValidationResult{
			{
				PolicyName: "Audit Logging",
				PolicyType: "audit",
				Allowed:    true,
				Message:    "Tool call logged for audit",
			},
		},
		Allowed: true,
	}, nil
}
