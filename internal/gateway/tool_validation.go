package gateway

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
)

// ValidationResult represents the result of a single validation check
type ValidationResult struct {
	PolicyName string              `json:"policy_name"`
	PolicyType string              `json:"policy_type"` // "cel" or "ai"
	Action     config.PolicyAction `json:"action"`      // "allow" or "deny"
	Mode       config.PolicyMode   `json:"mode"`        // "enabled", "audit_only", or "disabled"
	Message    string              `json:"message,omitempty"`
	Error      string              `json:"error,omitempty"`
	DurationMs int64               `json:"duration_ms"` // Time taken to evaluate this policy in milliseconds
}

// ValidationResults represents all validation results for a request
type ValidationResults struct {
	Results    []ValidationResult `json:"results"`
	Allowed    bool               `json:"allowed"`
	Message    string             `json:"message,omitempty"`
	Error      string             `json:"error,omitempty"`
	AllowCount int                `json:"allow_count"`
	DenyCount  int                `json:"deny_count"`
	// CELDetails contains detailed CEL validation results for audit logging
	// This is only populated by the CEL validation handler
	CELDetails *AuditCELResult `json:"cel_details,omitempty"`
	// AIDetails contains detailed AI validation results for audit logging
	// This is only populated by the AI validation handler
	AIDetails *AuditAIResult `json:"ai_details,omitempty"`
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

	var foundDeny bool
	var foundAllow bool
	var denyMessage string
	var allowMessage string

	for _, handler := range c.handlers {
		// Audit trail: 1 : Log the HandleToolCall : Tool call audit log : github__search_pull_requests
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

		// Propagate CEL and AI details from handlers
		if results.CELDetails != nil {
			finalResults.CELDetails = results.CELDetails
		}
		if results.AIDetails != nil {
			finalResults.AIDetails = results.AIDetails
		}

		if !foundDeny && results.DenyCount > 0 && results.Message != "" {
			denyMessage = results.Message
			foundDeny = true
		}
		if !foundAllow && results.AllowCount > 0 && results.Message != "" {
			allowMessage = results.Message
			foundAllow = true
		}
	}

	// If any handler denied, overall Allowed is false
	if foundDeny {
		finalResults.Allowed = false
		finalResults.Message = denyMessage
	} else if foundAllow {
		finalResults.Allowed = true
		finalResults.Message = allowMessage
	} else {
		finalResults.Allowed = true
	}

	return finalResults, finalError
}

// ToolCELValidationHandler handles CEL policy validation
type ToolCELValidationHandler struct {
	logger *config.SessionLogger
	engine *CELPolicyEngine
}

// NewToolCELValidationHandler creates a new CEL validation handler
func NewToolCELValidationHandler(logger *config.SessionLogger, engine *CELPolicyEngine) *ToolCELValidationHandler {
	return &ToolCELValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *ToolCELValidationHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	// Extract blocking budget from context if available
	budget, _ := ctx.Value(blockingBudgetKey).(*BlockingBudget)
	return h.engine.EvaluateToolCall(ctx, req, budget)
}

// blockingBudgetKeyType is used for context key to avoid collisions
type blockingBudgetKeyType struct{}

// blockingBudgetKey is the context key for BlockingBudget
var blockingBudgetKey = blockingBudgetKeyType{}

// WithBlockingBudget returns a new context with the BlockingBudget attached
func WithBlockingBudget(ctx context.Context, budget *BlockingBudget) context.Context {
	return context.WithValue(ctx, blockingBudgetKey, budget)
}

// BlockingBudgetFromContext extracts the BlockingBudget from context if available
func BlockingBudgetFromContext(ctx context.Context) *BlockingBudget {
	budget, _ := ctx.Value(blockingBudgetKey).(*BlockingBudget)
	return budget
}

// ToolAIValidationHandler handles AI policy validation
type ToolAIValidationHandler struct {
	logger *config.SessionLogger
	engine *AIPolicyEngine
}

// NewToolAIValidationHandler creates a new AI validation handler
func NewToolAIValidationHandler(logger *config.SessionLogger, engine *AIPolicyEngine) *ToolAIValidationHandler {
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
func (g *Gateway) ValidateToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	return g.validationChain.Handle(ctx, req)
}
