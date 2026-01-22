package gateway

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// ResponseValidationResult represents the result of a single response validation check
type ResponseValidationResult struct {
	PolicyName      string              `json:"policy_name"`
	PolicyType      string              `json:"policy_type"` // "cel" or "ai"
	Action          config.PolicyAction `json:"action"`      // "allow", "deny", or "redact"
	Mode            config.PolicyMode   `json:"mode"`        // "enabled", "audit_only", or "disabled"
	Message         string              `json:"message,omitempty"`
	Error           string              `json:"error,omitempty"`
	RedactedContent string              `json:"redacted_content,omitempty"` // For redaction actions
	DurationMs      int64               `json:"duration_ms"`                // Time taken to evaluate this policy in milliseconds
}

// ResponseValidationResults represents all validation results for a response
type ResponseValidationResults struct {
	Results         []ResponseValidationResult `json:"results"`
	Allowed         bool                       `json:"allowed"`
	Message         string                     `json:"message,omitempty"`
	Error           string                     `json:"error,omitempty"`
	RedactedContent *string                    `json:"redacted_content,omitempty"` // Final redacted content if any redaction occurred
	// RecommendedAction is what validation would recommend if fully evaluated.
	// Empty when fail-open prevents complete evaluation.
	RecommendedAction config.PolicyAction `json:"recommended_action,omitempty"`
	// FailedOpen indicates validation couldn't complete and defaulted to allow.
	FailedOpen bool `json:"-"`
	// AuditModeBypass indicates the response was allowed despite a deny/redact recommendation
	// because the deciding rule was in audit_only mode.
	AuditModeBypass bool `json:"-"`
	// RulesDetails contains detailed deterministic rules validation results for audit logging
	// This is only populated by the rules response validation handler
	RulesDetails *AuditRulesResult `json:"rules_details,omitempty"`
	// AIDetails contains detailed AI validation results for audit logging
	// This is only populated by the AI response validation handler
	AIDetails *AuditAIResult `json:"ai_details,omitempty"`
	// AsyncCompletion is set when there are audit_only policies still evaluating in the background.
	// The caller should read from this channel in a goroutine to receive complete AI results.
	// This field is not serialized.
	AsyncCompletion <-chan AsyncCompletion `json:"-"`
}

// AllowCount returns the number of results with allow action
func (r *ResponseValidationResults) AllowCount() int {
	count := 0
	for _, result := range r.Results {
		if result.Action == config.PolicyActionAllow {
			count++
		}
	}
	return count
}

// DenyCount returns the number of results with deny action
func (r *ResponseValidationResults) DenyCount() int {
	count := 0
	for _, result := range r.Results {
		if result.Action == config.PolicyActionDeny {
			count++
		}
	}
	return count
}

// RedactCount returns the number of results with redact action
func (r *ResponseValidationResults) RedactCount() int {
	count := 0
	for _, result := range r.Results {
		if result.Action == config.PolicyActionRedact {
			count++
		}
	}
	return count
}

// ResponseValidationHandler defines the interface for response validation handlers
type ResponseValidationHandler interface {
	HandleResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error)
}

// ResponseValidationChain implements a chain of response validation handlers
type ResponseValidationChain struct {
	handlers []ResponseValidationHandler
	logger   *config.SessionLogger
}

// NewResponseValidationChain creates a new response validation chain
func NewResponseValidationChain(logger *config.SessionLogger, handlers ...ResponseValidationHandler) *ResponseValidationChain {
	return &ResponseValidationChain{
		handlers: handlers,
		logger:   logger,
	}
}

// Handle processes a response through the validation chain
func (c *ResponseValidationChain) Handle(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	var finalResults ResponseValidationResults
	finalResults.Results = make([]ResponseValidationResult, 0)
	finalResults.Allowed = true // Default to allowed

	var currentResult = result

	for _, handler := range c.handlers {
		// Audit trail: 2 : Log the HandleResponse : Response audit log : github__search_pull_requests
		results, err := handler.HandleResponse(ctx, req, currentResult)
		if err != nil {
			c.logger.Error(ctx, "Response validation handler error",
				zap.Error(err),
			)
			// Continue processing other handlers
			continue
		}

		finalResults.Results = append(finalResults.Results, results.Results...)

		// If a handler denied the response, mark overall as denied
		if !results.Allowed {
			finalResults.Allowed = false
			if finalResults.Message == "" {
				finalResults.Message = results.Message
			}
		}

		// If a handler redacted content, update the current result
		if results.RedactedContent != nil {
			// Apply redaction to the current result
			if len(currentResult.Content) > 0 {
				// Update the first text content item
				for i := range currentResult.Content {
					if textContent, ok := currentResult.Content[i].(mcp.TextContent); ok {
						textContent.Text = *results.RedactedContent
						currentResult.Content[i] = textContent
						break
					}
				}
			}
			finalResults.RedactedContent = results.RedactedContent
		}

		// Propagate rules and AI details from handlers
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
			if results.RecommendedAction != "" {
				finalResults.RecommendedAction = results.RecommendedAction
			}
		}
	}

	// Set final message if not already set
	if finalResults.Message == "" {
		if finalResults.DenyCount() > 0 {
			finalResults.Message = "Response denied by policy"
		} else if finalResults.RedactCount() > 0 {
			finalResults.Message = "Response content redacted"
		} else {
			finalResults.Message = "Response validation passed"
		}
	}

	return finalResults, nil
}
