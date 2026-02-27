package gateway

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
)

// ActionReason is a typed string explaining why the action was taken
type ActionReason string

// ActionReason constants explain why the action was taken
const (
	// ActionReasonRequestPolicy indicates a request validation policy caused the deny
	ActionReasonRequestPolicy ActionReason = "request_policy"
	// ActionReasonResponsePolicy indicates a response validation policy caused the deny or redact
	ActionReasonResponsePolicy ActionReason = "response_policy"
	// ActionReasonAuditMode indicates the action differs from recommendation due to audit_only mode
	ActionReasonAuditMode ActionReason = "audit_mode"
	// ActionReasonFailOpen indicates evaluation couldn't complete (budget exhausted, timeout, errors)
	ActionReasonFailOpen ActionReason = "fail_open"
)

// DeriveAuditActionReason computes the action_reason for an audit entry based on
// validation result flags. denyReason is used when the result is denied; pass ""
// if no specific deny reason should be recorded (e.g., CLI/Action handlers where
// the deny action itself is self-explanatory).
//
// Priority: denied → denyReason, audit_mode bypass → "audit_mode", fail_open → "fail_open".
func DeriveAuditActionReason(allowed, auditModeBypass, failedOpen bool, denyReason ActionReason) string {
	if !allowed {
		return string(denyReason)
	}
	if auditModeBypass {
		return string(ActionReasonAuditMode)
	}
	if failedOpen {
		return string(ActionReasonFailOpen)
	}
	return ""
}

// ValidationResult represents the result of a single validation check
type ValidationResult struct {
	PolicyName string              `json:"policy_name"`
	PolicyType string              `json:"policy_type"` // "cel" or "ai"
	Action     config.PolicyAction `json:"action"`      // "allow" or "deny"
	Mode       config.PolicyMode   `json:"mode"`        // "audit_only" or empty (can block)
	Message    string              `json:"message,omitempty"`
	Error      string              `json:"error,omitempty"`
	DurationMs int64               `json:"duration_ms"` // Time taken to evaluate this policy in milliseconds
}

// AsyncValidationResult wraps ValidationResults with optional async completion support.
// When all policies are audit_only, the Completion channel allows deferred collection
// of results while the main request proceeds immediately.
type AsyncValidationResult struct {
	// Immediate results (available when function returns)
	Results ValidationResults

	// For async completion (nil if no async work pending)
	// When non-nil, the caller should read from this channel in a goroutine
	// to receive complete AI results after all async policies finish.
	Completion <-chan AsyncCompletion
}

// AsyncCompletion contains the final results from async policy evaluation.
// This is sent on the Completion channel after all background evaluations complete.
type AsyncCompletion struct {
	// AIDetails contains complete AI validation results including async policies
	AIDetails *AuditAIResult

	// EvaluationMs is the total wall-clock time for all policies to complete
	EvaluationMs int64
}

// ValidationResults represents all validation results for a request
type ValidationResults struct {
	Results    []ValidationResult `json:"results"`
	Allowed    bool               `json:"allowed"`
	Message    string             `json:"message,omitempty"`
	Error      string             `json:"error,omitempty"`
	AllowCount int                `json:"allow_count"`
	DenyCount  int                `json:"deny_count"`
	// RecommendedAction is what validation would recommend if fully evaluated.
	// Empty when fail-open prevents complete evaluation.
	RecommendedAction config.PolicyAction `json:"recommended_action,omitempty"`
	// ActionReason explains why the action was taken.
	// Empty when action == recommended_action with no special circumstances.
	ActionReason ActionReason `json:"action_reason,omitempty"`
	// FailedOpen indicates validation couldn't complete and defaulted to allow.
	// When true, RecommendedAction should be empty and ActionReason should be ActionReasonFailOpen.
	FailedOpen bool `json:"-"`
	// AuditModeBypass indicates the request was allowed despite a deny recommendation
	// because the deciding rule was in audit_only mode.
	AuditModeBypass bool `json:"-"`
	// RulesDetails contains detailed deterministic rules validation results for audit logging
	// This is only populated by the rules validation handler
	RulesDetails *AuditRulesResult `json:"rules_details,omitempty"`
	// AIDetails contains detailed AI validation results for audit logging
	// This is only populated by the AI validation handler
	AIDetails *AuditAIResult `json:"ai_details,omitempty"`
	// AsyncCompletion is set when there are audit_only policies still evaluating in the background.
	// The caller should read from this channel in a goroutine to receive complete AI results.
	// This field is not serialized.
	AsyncCompletion <-chan AsyncCompletion `json:"-"`
}

// DenyingRuleNames returns the names of all rules that issued a deny action
func (v *ValidationResults) DenyingRuleNames() []string {
	var names []string
	for _, result := range v.Results {
		if result.Action == config.PolicyActionDeny {
			names = append(names, result.PolicyName)
		}
	}
	return names
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
		if results.RulesDetails != nil {
			finalResults.RulesDetails = results.RulesDetails
		}
		if results.AIDetails != nil {
			finalResults.AIDetails = results.AIDetails
		}
		// Propagate async completion channel from AI handler
		if results.AsyncCompletion != nil {
			finalResults.AsyncCompletion = results.AsyncCompletion
		}

		// Propagate fail-open and audit-mode flags (any handler can set these)
		if results.FailedOpen {
			finalResults.FailedOpen = true
		}
		if results.AuditModeBypass {
			finalResults.AuditModeBypass = true
			// Capture the recommended action from the handler that would have denied
			if results.RecommendedAction != "" {
				finalResults.RecommendedAction = results.RecommendedAction
			}
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
		finalResults.RecommendedAction = config.PolicyActionDeny
		// An enforced deny overrides any audit-mode bypass — the request was
		// actually denied, so no bypass occurred. This matches finalizeResults
		// in PolicyEvaluator.
		finalResults.AuditModeBypass = false
	} else if foundAllow {
		finalResults.Allowed = true
		finalResults.Message = allowMessage
		// RecommendedAction is set by AuditModeBypass handler if applicable
		if !finalResults.AuditModeBypass && !finalResults.FailedOpen {
			finalResults.RecommendedAction = config.PolicyActionAllow
		}
	} else {
		finalResults.Allowed = true
		// Only set RecommendedAction to allow if not overridden by audit mode or fail-open
		if !finalResults.AuditModeBypass && !finalResults.FailedOpen {
			finalResults.RecommendedAction = config.PolicyActionAllow
		}
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
	// Extract blocking budget from context if available
	budget, _ := ctx.Value(blockingBudgetKey).(*BlockingBudget)
	return h.engine.EvaluateToolCall(ctx, req, budget)
}

// ValidateToolCall validates a tool call request
func (g *Gateway) ValidateToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	return g.validationChain.Handle(ctx, req)
}
