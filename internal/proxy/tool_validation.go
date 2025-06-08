package proxy

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// ValidationHandler defines the interface for tool call validation handlers
// (now using mcp.CallToolRequest)
type ToolValidationHandler interface {
	HandleToolCall(ctx context.Context, req mcp.CallToolRequest) error
}

// ValidationChain represents a chain of validation handlers
// (now using mcp.CallToolRequest)
type ToolValidationChain struct {
	handlers []ToolValidationHandler
}

// NewValidationChain creates a new validation chain
func NewToolValidationChain(handlers ...ToolValidationHandler) *ToolValidationChain {
	return &ToolValidationChain{
		handlers: handlers,
	}
}

// Handle processes a tool call request through the validation chain
func (c *ToolValidationChain) Handle(ctx context.Context, req mcp.CallToolRequest) error {
	for _, handler := range c.handlers {
		if err := handler.HandleToolCall(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

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
func (h *ToolLoggingHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) error {
	h.logger.Info("Validating tool call",
		zap.Any("request", req),
	)
	return nil
}

// CELValidationHandler validates tool call requests using CEL policies
type ToolCELValidationHandler struct {
	logger *zap.Logger
	engine *CELPolicyEngine
}

// NewCELValidationHandler creates a new CEL validation handler
func NewToolCELValidationHandler(logger *zap.Logger, engine *CELPolicyEngine) *ToolCELValidationHandler {
	return &ToolCELValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolCELValidationHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) error {
	// Evaluate policies
	allowed, message, err := h.engine.EvaluateToolCall(req)
	if err != nil {
		return fmt.Errorf("policy evaluation failed: %w", err)
	}

	if !allowed {
		return fmt.Errorf("policy violation: %s", message)
	}

	return nil
}

// ValidateToolCall validates a tool call request
func (p *Proxy) ValidateToolCall(ctx context.Context, req mcp.CallToolRequest) error {
	return p.validationChain.Handle(ctx, req)
}
