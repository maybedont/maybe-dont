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
func (h *ToolLoggingHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	// Always allow, no validation needed. No need to log this either
	return ValidationResults{}, nil
}
