package proxy

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// ValidationResult represents the result of a single validation check
type ValidationResult struct {
	PolicyName string `json:"policy_name"`
	PolicyType string `json:"policy_type"` // "cel" or "ai"
	Allowed    bool   `json:"allowed"`
	Message    string `json:"message"`
	Error      string `json:"error,omitempty"`
}

// ValidationResults represents all validation results for a request
type ValidationResults struct {
	Results []ValidationResult `json:"results"`
	Allowed bool               `json:"allowed"`
	Message string             `json:"message"`
}

// ValidationHandler defines the interface for tool call validation handlers
// (now using mcp.CallToolRequest)
type ToolValidationHandler interface {
	HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResult, error)
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
func (c *ToolValidationChain) Handle(ctx context.Context, req mcp.CallToolRequest) (*ValidationResults, error) {
	results := &ValidationResults{
		Results: make([]ValidationResult, 0),
		Allowed: true,
	}

	for _, handler := range c.handlers {
		result, err := handler.HandleToolCall(ctx, req)
		if err != nil {
			result.Error = err.Error()
		}
		results.Results = append(results.Results, result)

		// If any validation fails, the overall result is not allowed
		if !result.Allowed {
			results.Allowed = false
			results.Message = result.Message
		}
	}

	return results, nil
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
func (h *ToolCELValidationHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResult, error) {
	// Evaluate policies
	allowed, message, err := h.engine.EvaluateToolCall(req)
	if err != nil {
		return ValidationResult{
			PolicyName: "CEL Policy",
			PolicyType: "cel",
			Allowed:    false,
			Error:      err.Error(),
		}, nil
	}

	return ValidationResult{
		PolicyName: "CEL Policy",
		PolicyType: "cel",
		Allowed:    allowed,
		Message:    message,
	}, nil
}

// CELValidationHandler validates tool call requests using CEL policies
type ToolAIValidationHandler struct {
	logger *zap.Logger
	engine *AIPolicyEngine
}

// NewAIValidationHandler creates a new CEL validation handler
func NewToolAIValidationHandler(logger *zap.Logger, engine *AIPolicyEngine) *ToolAIValidationHandler {
	return &ToolAIValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolAIValidationHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResult, error) {
	// Evaluate policies
	allowed, message, err := h.engine.EvaluateToolCall(ctx, req)
	if err != nil {
		return ValidationResult{
			PolicyName: "AI Policy",
			PolicyType: "ai",
			Allowed:    false,
			Error:      err.Error(),
		}, nil
	}

	return ValidationResult{
		PolicyName: "AI Policy",
		PolicyType: "ai",
		Allowed:    allowed,
		Message:    message,
	}, nil
}

// ValidateToolCall validates a tool call request
func (p *Proxy) ValidateToolCall(ctx context.Context, req mcp.CallToolRequest) (*ValidationResults, error) {
	return p.validationChain.Handle(ctx, req)
}
