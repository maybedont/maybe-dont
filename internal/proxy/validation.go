package proxy

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// ValidationHandler defines the interface for tool call validation handlers
// (now using mcp.CallToolRequest)
type ValidationHandler interface {
	Handle(ctx context.Context, req mcp.CallToolRequest) error
}

// ValidationChain represents a chain of validation handlers
// (now using mcp.CallToolRequest)
type ValidationChain struct {
	handlers []ValidationHandler
}

// NewValidationChain creates a new validation chain
func NewValidationChain(handlers ...ValidationHandler) *ValidationChain {
	return &ValidationChain{
		handlers: handlers,
	}
}

// Handle processes a tool call request through the validation chain
func (c *ValidationChain) Handle(ctx context.Context, req mcp.CallToolRequest) error {
	for _, handler := range c.handlers {
		if err := handler.Handle(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// LoggingHandler logs tool call request details
type LoggingHandler struct{}

// NewLoggingHandler creates a new logging handler
func NewLoggingHandler() *LoggingHandler {
	return &LoggingHandler{}
}

// Handle implements ValidationHandler
func (h *LoggingHandler) Handle(ctx context.Context, req mcp.CallToolRequest) error {
	// Logging removed for generic handler
	return nil
}

// CELValidationHandler validates tool call requests using CEL policies
type CELValidationHandler struct{}

// NewCELValidationHandler creates a new CEL validation handler
func NewCELValidationHandler() *CELValidationHandler {
	return &CELValidationHandler{}
}

// Handle implements ValidationHandler
func (h *CELValidationHandler) Handle(ctx context.Context, req mcp.CallToolRequest) error {
	// TODO: Implement CEL policy evaluation for tool calls
	return nil
}

// ValidateToolCall validates a tool call request
func (p *Proxy) ValidateToolCall(ctx context.Context, req mcp.CallToolRequest) error {
	return p.validationChain.Handle(ctx, req)
}
