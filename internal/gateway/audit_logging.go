package gateway

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// ToolLoggingHandler logs tool call request details
type ToolLoggingHandler struct {
	auditLogger *config.SessionLogger
}

// NewToolLoggingHandler creates a new logging handler
func NewToolLoggingHandler(auditLogger *config.SessionLogger) *ToolLoggingHandler {
	return &ToolLoggingHandler{
		auditLogger: auditLogger,
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolLoggingHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	// Log the tool call for audit purposes
	h.auditLogger.Info(ctx, "Tool call audit log",
		zap.String("method", req.Method),
		zap.String("tool_name", req.Params.Name),
		zap.Any("arguments", req.Params.Arguments),
	)

	// TODO : This could use some work, it is confusing since this will end up in the audit log to call the policy 'audit logging'.
	//        Is this really just an implicit policy to audit the use of this native tool?
	// Return audit result - always allows but provides audit trail
	return ValidationResults{
		Results: []ValidationResult{
			{
				PolicyName: "Audit Logging",
				PolicyType: "audit",
				Action:     config.PolicyActionAllow,
				Mode:       "", // Empty mode = can block (not audit_only)
				Message:    "Tool call logged for audit",
			},
		},
		Allowed: true,
	}, nil
}
