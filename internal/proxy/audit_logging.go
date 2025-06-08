package proxy

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// LoggingHandler logs tool call request details
type ToolLoggingHandler struct {
	logger *zap.Logger
}

// NewLoggingHandler creates a new logging handler
func NewToolLoggingHandler(logger *zap.Logger) *ToolLoggingHandler {
	return &ToolLoggingHandler{
		logger: logger,
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolLoggingHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResult, error) {
	h.logger.Info("Validating tool call",
		zap.Any("request", req),
	)
	return ValidationResult{
		PolicyName: "Logging",
		PolicyType: "logging",
		Allowed:    true,
		Message:    "Request logged successfully",
	}, nil
}
