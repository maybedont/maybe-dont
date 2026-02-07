package gateway

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// CELResponsePolicy represents a single CEL response policy rule
type CELResponsePolicy struct {
	Name                 string              `yaml:"name"`
	Description          string              `yaml:"description"`
	Expression           string              `yaml:"expression"`
	Action               config.PolicyAction `yaml:"action"` // allow, deny, or redact
	Message              string              `yaml:"message"`
	Mode                 config.PolicyMode   `yaml:"mode"` // enabled, audit_only, or disabled
	RedactionPattern     string              `yaml:"redaction_pattern"`
	RedactionReplacement string              `yaml:"redaction_replacement"`
}

// CELResponsePolicyEngine handles CEL policy evaluation for responses
type CELResponsePolicyEngine struct {
	logger   *config.SessionLogger
	env      *cel.Env
	policies []CELResponsePolicy
	mu       sync.RWMutex
}

// NewCELResponsePolicyEngine creates a new CEL response policy engine
func NewCELResponsePolicyEngine(ctx context.Context, logger *config.SessionLogger) (*CELResponsePolicyEngine, error) {
	// Create CEL environment with custom functions
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		cel.Variable("response", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELResponsePolicyEngine{
		logger:   logger,
		env:      env,
		policies: make([]CELResponsePolicy, 0),
	}, nil
}

// LoadPolicies loads response policies from configuration
// topLevelMode is the top-level mode that applies to all policies (audit_only makes all rules audit_only)
func (e *CELResponsePolicyEngine) LoadPolicies(policies []config.ResponsePolicy, topLevelMode config.PolicyMode, includeDisabled ...bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if we should include disabled policies (default: false)
	loadDisabled := len(includeDisabled) > 0 && includeDisabled[0]

	// Track seen policy names to detect duplicates
	seenNames := make(map[string]bool)

	// Validate and compile each policy
	for _, policy := range policies {
		// Check for duplicate names
		if seenNames[policy.Name] {
			return fmt.Errorf("duplicate policy name '%s' in CEL response rules", policy.Name)
		}
		seenNames[policy.Name] = true

		// Skip disabled policies (enabled: false) unless includeDisabled is set
		if !policy.IsEnabled() && !loadDisabled {
			e.logger.Debug(context.Background(), "Skipping disabled response policy",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Resolve effective mode for this policy
		// Top-level audit_only applies to all rules; per-rule audit_only is additive
		effectiveMode := config.ResolvePolicyMode(topLevelMode, policy.Mode)

		e.logger.Debug(context.Background(), "Loading response policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Compile the expression
		_, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("failed to compile response policy %s: %w", policy.Name, issues.Err())
		}

		// Validate action, a response policy can be 'allow', 'deny' or 'redact'
		if policy.Action != config.PolicyActionAllow && policy.Action != config.PolicyActionDeny && policy.Action != config.PolicyActionRedact {
			return fmt.Errorf("invalid action '%s' for response policy %s: must be 'allow', 'deny', or 'redact'", policy.Action, policy.Name)
		}

		// Store the compiled policy with resolved mode
		e.policies = append(e.policies, CELResponsePolicy{
			Name:                 policy.Name,
			Description:          policy.Description,
			Expression:           policy.Expression,
			Action:               policy.Action,
			Message:              policy.Message,
			Mode:                 effectiveMode,
			RedactionPattern:     policy.RedactionPattern,
			RedactionReplacement: policy.RedactionReplacement,
		})
	}

	e.logger.Debug(context.Background(), "Loaded CEL response policies", zap.Int("count", len(e.policies)))
	return nil
}

// EvaluateResponse evaluates a response against all policies.
// The optional budget parameter enables blocking time tracking for cumulative budget management.
// When budget is nil, no blocking time is tracked.
func (e *CELResponsePolicyEngine) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult, budget *BlockingBudget) (ResponseValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	phaseStart := time.Now()
	var phaseTracker *PhaseTracker
	if budget != nil {
		phaseTracker = budget.StartPhase()
	}

	e.logger.Debug(ctx, "Evaluating response with CEL policies",
		zap.String("tool", req.Params.Name),
		zap.Int("policy_count", len(e.policies)),
	)

	// Extract response content for CEL evaluation
	responseData := e.extractResponseData(result)

	// Create evaluation context
	vars := map[string]interface{}{
		"request": map[string]interface{}{
			"method": req.Method,
			"params": map[string]interface{}{
				"name":      req.Params.Name,
				"arguments": req.Params.Arguments,
			},
		},
		"response": responseData,
	}

	results := ResponseValidationResults{
		Results: make([]ResponseValidationResult, 0),
		Allowed: true,
	}

	// Track per-rule results for audit
	ruleResults := make([]AuditRulesRuleResult, 0, len(e.policies))
	var decidingRule, decidingReason string
	finalAction := "allow"
	earlyTerminated := false
	var redactedContent *string

	// Evaluate each policy in order
	for _, policy := range e.policies {
		// Track timing for this policy evaluation
		ruleStart := time.Now()

		e.logger.Debug(ctx, "Evaluating CEL response policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("expression", policy.Expression),
		)

		// Compile the expression
		ast, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			ruleDurationMs := time.Since(ruleStart).Milliseconds()
			e.logger.Error(ctx, "CEL response policy compilation error",
				zap.String("rule", policy.Name),
				zap.Int64("evaluation_ms", ruleDurationMs),
				zap.Error(issues.Err()),
			)
			ruleResults = append(ruleResults, AuditRulesRuleResult{
				Rule:         policy.Name,
				Action:       string(policy.Action),
				Mode:         modeToAuditString(policy.Mode),
				Result:       "error",
				EvaluationMs: ruleDurationMs,
				Error:        formatAuditError("compile_error", issues.Err()),
			})
			// Fail-open on compilation error for enabled rules
			if !policy.Mode.IsAuditOnly() {
				if phaseTracker != nil {
					phaseTracker.MarkDecided()
				}
				results.FailedOpen = true
				earlyTerminated = true
				break
			}
			continue
		}

		// Create program
		prg, err := e.env.Program(ast)
		if err != nil {
			ruleDurationMs := time.Since(ruleStart).Milliseconds()
			e.logger.Error(ctx, "CEL response policy program error",
				zap.String("rule", policy.Name),
				zap.Int64("evaluation_ms", ruleDurationMs),
				zap.Error(err),
			)
			ruleResults = append(ruleResults, AuditRulesRuleResult{
				Rule:         policy.Name,
				Action:       string(policy.Action),
				Mode:         modeToAuditString(policy.Mode),
				Result:       "error",
				EvaluationMs: ruleDurationMs,
				Error:        formatAuditError("program_error", err),
			})
			// Fail-open on program creation error for enabled rules
			if !policy.Mode.IsAuditOnly() {
				if phaseTracker != nil {
					phaseTracker.MarkDecided()
				}
				results.FailedOpen = true
				earlyTerminated = true
				break
			}
			continue
		}

		// Evaluate the expression
		out, _, err := prg.Eval(vars)
		ruleDurationMs := time.Since(ruleStart).Milliseconds()

		if err != nil {
			e.logger.Error(ctx, "CEL response policy evaluation error",
				zap.String("rule", policy.Name),
				zap.Int64("evaluation_ms", ruleDurationMs),
				zap.Error(err),
			)
			ruleResults = append(ruleResults, AuditRulesRuleResult{
				Rule:         policy.Name,
				Action:       string(policy.Action),
				Mode:         modeToAuditString(policy.Mode),
				Result:       "error",
				EvaluationMs: ruleDurationMs,
				Error:        formatAuditError("eval_error", err),
			})
			// Fail-open on evaluation error for enabled rules
			if !policy.Mode.IsAuditOnly() {
				if phaseTracker != nil {
					phaseTracker.MarkDecided()
				}
				results.FailedOpen = true
				earlyTerminated = true
				break
			}
			continue
		}

		// Check result
		matched, ok := out.Value().(bool)
		if !ok {
			ruleResults = append(ruleResults, AuditRulesRuleResult{
				Rule:         policy.Name,
				Action:       string(policy.Action),
				Mode:         modeToAuditString(policy.Mode),
				Result:       "error",
				EvaluationMs: ruleDurationMs,
				Error:        "policy did not return a boolean",
			})
			// Fail-open on type error for enabled rules
			if !policy.Mode.IsAuditOnly() {
				if phaseTracker != nil {
					phaseTracker.MarkDecided()
				}
				results.FailedOpen = true
				earlyTerminated = true
				break
			}
			continue
		}

		e.logger.Debug(ctx, "CEL response policy evaluation result",
			zap.String("name", policy.Name),
			zap.Bool("matched", matched),
			zap.String("action", string(policy.Action)),
			zap.Int64("evaluation_ms", ruleDurationMs),
		)

		// Determine the effective result based on expression match and action
		// If matched, the result is the action; if not matched, the result is the opposite
		var ruleResult string
		if matched {
			ruleResult = string(policy.Action) // "allow", "deny", or "redact"
		} else {
			// No match: deny/redact rules effectively allow, allow rules effectively deny
			if policy.Action == config.PolicyActionAllow {
				ruleResult = string(config.PolicyActionDeny)
			} else {
				ruleResult = string(config.PolicyActionAllow)
			}
		}

		ruleResults = append(ruleResults, AuditRulesRuleResult{
			Rule:         policy.Name,
			Action:       string(policy.Action),
			Mode:         modeToAuditString(policy.Mode),
			Result:       ruleResult,
			EvaluationMs: ruleDurationMs,
		})

		if matched {
			// Add to legacy results for compatibility
			results.Results = append(results.Results, ResponseValidationResult{
				PolicyName: policy.Name,
				PolicyType: "cel",
				Action:     policy.Action,
				Mode:       policy.Mode,
				Message:    policy.Message,
				DurationMs: ruleDurationMs,
			})

			switch policy.Action {
			case config.PolicyActionDeny:
				// Only affect final decision if mode is enabled (not audit_only)
				if !policy.Mode.IsAuditOnly() {
					if phaseTracker != nil {
						phaseTracker.MarkDecided()
					}
					finalAction = "deny"
					decidingRule = policy.Name
					decidingReason = policy.Message
					results.Allowed = false
					if results.Message == "" {
						results.Message = policy.Message
					}
					earlyTerminated = true
					break // Early termination on first enabled deny
				}

			case config.PolicyActionRedact:
				// Apply redaction
				content := e.getTextContent(result)
				if content != "" {
					redacted := e.applyRedaction(content, policy.RedactionPattern, policy.RedactionReplacement)
					// Update the result entry with redacted content
					results.Results[len(results.Results)-1].RedactedContent = redacted

					// Only actually apply redaction if mode is enabled (not audit_only)
					if !policy.Mode.IsAuditOnly() {
						if finalAction == "allow" {
							finalAction = "redact"
						}
						redactedContent = &redacted
					}
				}

			case config.PolicyActionAllow:
				// Allow just passes through
			}

			if earlyTerminated {
				break
			}
		}
	}

	// Finalize phase timing
	var blockedMs, evaluationMs int64
	if phaseTracker != nil {
		blockedMs, evaluationMs = phaseTracker.Finalize()
	} else {
		evaluationMs = time.Since(phaseStart).Milliseconds()
	}

	// Set redacted content if any redaction occurred
	if redactedContent != nil {
		results.RedactedContent = redactedContent
	}

	// Set final result message if not already set
	if results.Message == "" {
		if results.FailedOpen {
			results.Message = "CEL response evaluation failed, allowing response (fail-open)"
		} else {
			switch finalAction {
			case "deny":
				results.Message = decidingReason
				if results.Message == "" {
					results.Message = "Response denied by CEL policy"
				}
			case "redact":
				results.Message = "Response content redacted by CEL policy"
			default:
				results.Message = "No CEL response policies matched"
			}
		}
	}

	// Set RecommendedAction based on final action (unless fail-open)
	if !results.FailedOpen {
		switch finalAction {
		case "deny":
			results.RecommendedAction = config.PolicyActionDeny
		case "redact":
			results.RecommendedAction = config.PolicyActionRedact
		default:
			results.RecommendedAction = config.PolicyActionAllow
		}
	}

	// Build CELDetails for audit
	celDetails := &AuditRulesResult{
		Action:       finalAction,
		BlockedMs:    blockedMs,
		EvaluationMs: evaluationMs,
		Results:      ruleResults,
	}
	if decidingRule != "" {
		celDetails.DecidingRule = decidingRule
		celDetails.Reason = decidingReason
	}
	results.RulesDetails = celDetails

	e.logger.Debug(ctx, "CEL response policy evaluation complete",
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
		zap.String("final_action", finalAction),
		zap.Bool("early_terminated", earlyTerminated),
		zap.Bool("failed_open", results.FailedOpen),
		zap.Int64("blocked_ms", blockedMs),
		zap.Int64("evaluation_ms", evaluationMs),
	)

	return results, nil
}

// extractResponseData extracts relevant data from the response for CEL evaluation
func (e *CELResponsePolicyEngine) extractResponseData(result *mcp.CallToolResult) map[string]interface{} {
	data := map[string]interface{}{
		"isError": result.IsError,
	}

	// Extract content
	if len(result.Content) > 0 {
		contentItems := make([]map[string]interface{}, 0, len(result.Content))
		for _, item := range result.Content {
			switch v := item.(type) {
			case mcp.TextContent:
				contentItems = append(contentItems, map[string]interface{}{
					"type": "text",
					"text": v.Text,
				})
			case mcp.ImageContent:
				contentItems = append(contentItems, map[string]interface{}{
					"type":     "image",
					"data":     v.Data,
					"mimeType": v.MIMEType,
				})
			case mcp.EmbeddedResource:
				contentItems = append(contentItems, map[string]interface{}{
					"type":     "resource",
					"resource": v.Resource,
				})
			}
		}
		data["content"] = contentItems
	}

	// Extract meta
	if result.Meta != nil {
		data["meta"] = result.Meta.AdditionalFields
	}

	return data
}

// getTextContent extracts the first text content from the result
func (e *CELResponsePolicyEngine) getTextContent(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}

	for _, item := range result.Content {
		if textContent, ok := item.(mcp.TextContent); ok {
			return textContent.Text
		}
	}

	return ""
}

// applyRedaction applies redaction pattern to content
func (e *CELResponsePolicyEngine) applyRedaction(content, pattern, replacement string) string {
	if pattern == "" {
		// Default: redact entire content
		return "[REDACTED]"
	}

	if replacement == "" {
		replacement = "[REDACTED]"
	}

	// Compile and apply regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		e.logger.Error(context.Background(), "Failed to compile redaction pattern",
			zap.String("pattern", pattern),
			zap.Error(err),
		)
		return content
	}

	return re.ReplaceAllString(content, replacement)
}

// ResponseCELValidationHandler handles CEL response validation
type ResponseCELValidationHandler struct {
	logger *config.SessionLogger
	engine *CELResponsePolicyEngine
}

// NewResponseCELValidationHandler creates a new CEL response validation handler
func NewResponseCELValidationHandler(logger *config.SessionLogger, engine *CELResponsePolicyEngine) *ResponseCELValidationHandler {
	return &ResponseCELValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleResponse implements ResponseValidationHandler
func (h *ResponseCELValidationHandler) HandleResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	// Extract blocking budget from context if available
	budget := BlockingBudgetFromContext(ctx)
	return h.engine.EvaluateResponse(ctx, req, result, budget)
}
