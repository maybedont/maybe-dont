package proxy

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// ValidationResult represents the result of a single validation check
type ValidationResult struct {
	PolicyName string `json:"policy_name"`
	PolicyType string `json:"policy_type"` // "cel" or "ai"
	Allowed    bool   `json:"allowed"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ValidationResults represents all validation results for a request
type ValidationResults struct {
	Results    []ValidationResult `json:"results"`
	Allowed    bool               `json:"allowed"`
	Message    string             `json:"message,omitempty"`
	Error      string             `json:"error,omitempty"`
	AllowCount int                `json:"allow_count"`
	DenyCount  int                `json:"deny_count"`
}

// ToolValidationHandler defines the interface for tool validation handlers
type ToolValidationHandler interface {
	HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error)
}

// ToolValidationChain implements a chain of validation handlers
type ToolValidationChain struct {
	handlers []ToolValidationHandler
}

// NewToolValidationChain creates a new validation chain
func NewToolValidationChain(handlers ...ToolValidationHandler) *ToolValidationChain {
	return &ToolValidationChain{
		handlers: handlers,
	}
}

// Handle processes a tool call request through the validation chain
func (c *ToolValidationChain) Handle(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	var finalResults ValidationResults
	var finalError error

	for _, handler := range c.handlers {
		results, err := handler.HandleToolCall(ctx, req)
		if err != nil {
			if finalError == nil {
				finalError = err
			} else {
				finalError = fmt.Errorf("%w %w", finalError, err)
			}
			continue
		}

		finalResults.Results = append(finalResults.Results, results.Results...)
		finalResults.AllowCount += results.AllowCount
		finalResults.DenyCount += results.DenyCount
	}

	return finalResults, finalError
}

// ToolCELValidationHandler handles CEL policy validation
type ToolCELValidationHandler struct {
	logger *zap.Logger
	engine *CELPolicyEngine
}

// NewToolCELValidationHandler creates a new CEL validation handler
func NewToolCELValidationHandler(logger *zap.Logger, engine *CELPolicyEngine) *ToolCELValidationHandler {
	return &ToolCELValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolCELValidationHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	return h.engine.EvaluateToolCall(req)
}

// ToolAIValidationHandler handles AI policy validation
type ToolAIValidationHandler struct {
	logger *zap.Logger
	engine *AIPolicyEngine
}

// NewToolAIValidationHandler creates a new AI validation handler
func NewToolAIValidationHandler(logger *zap.Logger, engine *AIPolicyEngine) *ToolAIValidationHandler {
	return &ToolAIValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolAIValidationHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	return h.engine.EvaluateToolCall(ctx, req)
}

// ValidateToolCall validates a tool call request
func (p *Proxy) ValidateToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	return p.validationChain.Handle(ctx, req)
}
