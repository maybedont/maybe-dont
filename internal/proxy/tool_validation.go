package proxy

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
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
type ToolLoggingHandler struct{}

// NewLoggingHandler creates a new logging handler
func NewToolLoggingHandler() *ToolLoggingHandler {
	return &ToolLoggingHandler{}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolLoggingHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) error {
	// Logging removed for generic handler
	return nil
}

// CELValidationHandler validates tool call requests using CEL policies
type ToolCELValidationHandler struct{}

// NewCELValidationHandler creates a new CEL validation handler
func NewToolCELValidationHandler() *ToolCELValidationHandler {
	return &ToolCELValidationHandler{}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolCELValidationHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) error {
	// TODO: Implement CEL policy evaluation for tool calls
	return nil
}

// ValidateToolCall validates a tool call request
func (p *Proxy) ValidateToolCall(ctx context.Context, req mcp.CallToolRequest) error {
	return p.validationChain.Handle(ctx, req)
}
