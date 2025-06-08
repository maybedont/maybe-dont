package proxy

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
func (h *ToolLoggingHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResult, error) {
	h.auditLogger.Info("Audit log: Tool call request",
		zap.Any("request", req),
	)
	return ValidationResult{
		PolicyName: "Audit Logging",
		PolicyType: "audit",
		Allowed:    true,
		Message:    "Request logged for audit",
	}, nil
}
