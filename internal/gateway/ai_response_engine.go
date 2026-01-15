package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.uber.org/zap"
)

// AIResponsePolicy represents a single AI response policy rule
type AIResponsePolicy struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Prompt      string              `yaml:"prompt"`
	Action      config.PolicyAction `yaml:"action"` // allow, deny, or redact
	Message     string              `yaml:"message"`
	Mode        config.PolicyMode   `yaml:"mode"` // enabled, audit_only, or disabled
}

// AIResponsePolicyEngine handles AI policy evaluation for responses
type AIResponsePolicyEngine struct {
	logger              *config.SessionLogger
	policies            []AIResponsePolicy
	mu                  sync.RWMutex
	endpoint            string
	model               string
	apiKey              string
	client              *openai.Client
	maxRuleEvaluationMs int // Max time for any single rule to complete
}

// InitAIResponsePolicyEngine initializes the AI response policy engine
func InitAIResponsePolicyEngine(ctx context.Context, logger *config.SessionLogger, engine *AIResponsePolicyEngine) error {
	engine.logger = logger
	client := openai.NewClient(
		option.WithAPIKey(engine.apiKey),
	)
	engine.client = &client
	return nil
}

// LoadPolicies loads AI response policies from configuration
// defaultMode is the top-level mode that applies to all policies unless overridden per-rule
func (e *AIResponsePolicyEngine) LoadPolicies(policies []config.AIResponsePolicy, defaultMode config.PolicyMode) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info(context.Background(), "Loading AI response policies",
		zap.Int("count", len(policies)),
		zap.String("default_mode", string(defaultMode)),
	)

	// Validate each policy
	for _, policy := range policies {
		// Resolve effective mode for this policy
		effectiveMode := config.ResolvePolicyMode(policy.Mode, defaultMode)

		e.logger.Info(context.Background(), "Loading AI response policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Skip disabled policies
		if effectiveMode == config.PolicyModeDisabled {
			e.logger.Info(context.Background(), "Skipping disabled AI response policy",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Validate action, a response policy can be 'allow', 'deny' or 'redact'
		if policy.Action != config.PolicyActionAllow && policy.Action != config.PolicyActionDeny && policy.Action != config.PolicyActionRedact {
			return fmt.Errorf("invalid action %s for AI response policy %s", policy.Action, policy.Name)
		}

		// Store the policy with resolved mode
		e.policies = append(e.policies, AIResponsePolicy{
			Name:        policy.Name,
			Description: policy.Description,
			Prompt:      policy.Prompt,
			Action:      policy.Action,
			Message:     policy.Message,
			Mode:        effectiveMode,
		})
	}

	e.logger.Info(context.Background(), "Loaded AI response policies", zap.Int("count", len(e.policies)))
	return nil
}

// AIResponseEvaluation represents the AI's response evaluation
type AIResponseEvaluation struct {
	Allowed         bool   `json:"allowed"`
	Message         string `json:"message"`
	RedactedContent string `json:"redacted_content"`
}

// aiResponseRuleResult represents the result of evaluating a single AI response rule
type aiResponseRuleResult struct {
	policy       AIResponsePolicy
	result       string // "allow", "deny", "redact", or "error"
	message      string
	redacted     string
	evaluationMs int64
	err          error
}

// EvaluateResponse evaluates a response against all policies.
// The optional budget parameter enables blocking time tracking for cumulative budget management.
// When budget is nil, no blocking time is tracked.
func (e *AIResponsePolicyEngine) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, toolResult *mcp.CallToolResult, budget *BlockingBudget) (ResponseValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	phaseStart := time.Now()
	var phaseTracker *PhaseTracker
	if budget != nil {
		phaseTracker = budget.StartPhase()
	}

	e.logger.Info(ctx, "Evaluating response with AI policies",
		zap.String("tool", req.Params.Name),
		zap.Int("policy_count", len(e.policies)),
	)

	// Track all policy evaluations
	var results ResponseValidationResults
	results.Results = make([]ResponseValidationResult, 0)
	results.Allowed = true

	// If no policies, return early
	if len(e.policies) == 0 {
		var blockedMs, evaluationMs int64
		if phaseTracker != nil {
			blockedMs, evaluationMs = phaseTracker.Finalize()
		} else {
			evaluationMs = time.Since(phaseStart).Milliseconds()
		}

		results.Message = "No AI response policies configured"
		results.AIDetails = &AuditAIResult{
			Action:       "allow",
			BlockedMs:    blockedMs,
			EvaluationMs: evaluationMs,
			Results:      []AuditAIRuleResult{},
		}
		return results, nil
	}

	// Create a cancellable context for early termination
	evalCtx, cancelEval := context.WithCancel(ctx)
	defer cancelEval()

	// Create a channel to collect results from goroutines
	resultChan := make(chan aiResponseRuleResult, len(e.policies))

	// Format the response for the AI once
	responseStr := e.formatResponseForAI(toolResult)

	// Determine timeout for individual rule evaluation
	// Use the configured max rule evaluation time, but also respect the blocking budget
	ruleTimeout := time.Duration(e.maxRuleEvaluationMs) * time.Millisecond
	if ruleTimeout <= 0 {
		ruleTimeout = 10 * time.Second // Default fallback
	}

	// If we have a blocking budget, cap the timeout to the remaining budget
	if budget != nil {
		remainingMs := budget.RemainingMs()
		if remainingMs > 0 {
			remainingDuration := time.Duration(remainingMs) * time.Millisecond
			if remainingDuration < ruleTimeout {
				ruleTimeout = remainingDuration
				e.logger.Debug(ctx, "Capping rule timeout to remaining blocking budget",
					zap.Int64("remaining_ms", remainingMs),
					zap.Duration("rule_timeout", ruleTimeout))
			}
		} else {
			// Budget exhausted - use a minimal timeout to allow quick failures
			ruleTimeout = 100 * time.Millisecond
			e.logger.Warn(ctx, "Blocking budget exhausted before response AI validation, using minimal timeout")
		}
	}

	// Launch a goroutine for each policy
	for _, policy := range e.policies {
		go func(p AIResponsePolicy) {
			// Track timing for this policy evaluation
			startTime := time.Now()

			// Create a new context for this goroutine with timeout
			policyCtx, cancel := context.WithTimeout(evalCtx, ruleTimeout)
			defer cancel()

			schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:        "ai_response_evaluation",
				Description: openai.String("AI evaluation of a tool response"),
				Schema:      GenerateSchema[AIResponseEvaluation](),
				Strict:      openai.Bool(true),
			}

			// Call the AI API with the actual response
			chatCompletion, err := e.client.Chat.Completions.New(policyCtx, openai.ChatCompletionNewParams{
				Messages: []openai.ChatCompletionMessageParamUnion{
					openai.UserMessage(fmt.Sprintf(p.Prompt, responseStr)),
				},
				Model: openai.ChatModel(e.model),
				ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
					OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
						JSONSchema: schemaParam,
					},
				},
			})

			durationMs := time.Since(startTime).Milliseconds()

			if err != nil {
				resultChan <- aiResponseRuleResult{
					policy:       p,
					result:       "error",
					message:      fmt.Sprintf("Failed to evaluate response policy: %v", err),
					evaluationMs: durationMs,
					err:          err,
				}
				return
			}

			if len(chatCompletion.Choices) == 0 {
				resultChan <- aiResponseRuleResult{
					policy:       p,
					result:       "error",
					message:      "No response from AI model",
					evaluationMs: durationMs,
					err:          fmt.Errorf("no response from AI model"),
				}
				return
			}

			// Parse the response as JSON
			var evaluation AIResponseEvaluation
			err = json.Unmarshal([]byte(chatCompletion.Choices[0].Message.Content), &evaluation)
			if err != nil {
				resultChan <- aiResponseRuleResult{
					policy:       p,
					result:       "error",
					message:      fmt.Sprintf("Failed to parse AI response: %v", err),
					evaluationMs: durationMs,
					err:          err,
				}
				return
			}

			// Determine result based on AI evaluation
			var resultStr string
			if !evaluation.Allowed {
				resultStr = "deny"
			} else if p.Action == config.PolicyActionRedact && evaluation.RedactedContent != "" {
				resultStr = "redact"
			} else {
				resultStr = "allow"
			}

			resultChan <- aiResponseRuleResult{
				policy:       p,
				result:       resultStr,
				message:      evaluation.Message,
				redacted:     evaluation.RedactedContent,
				evaluationMs: durationMs,
				err:          nil,
			}
		}(policy)
	}

	// Collect results with early termination support
	ruleResults := make([]AuditAIRuleResult, 0, len(e.policies))
	var decidingRule, decidingReason string
	finalAction := "allow"
	earlyTerminated := false
	decided := false
	var redactedContent *string

	for i := 0; i < len(e.policies); i++ {
		ruleResult := <-resultChan

		// Build audit result for this rule
		auditResult := AuditAIRuleResult{
			Rule:         ruleResult.policy.Name,
			Action:       string(ruleResult.policy.Action),
			Mode:         modeToAuditString(ruleResult.policy.Mode),
			Result:       ruleResult.result,
			EvaluationMs: ruleResult.evaluationMs,
		}
		if ruleResult.err != nil {
			auditResult.Error = ruleResult.err.Error()
		}
		ruleResults = append(ruleResults, auditResult)

		// Also add to legacy results for compatibility
		results.Results = append(results.Results, ResponseValidationResult{
			PolicyName:      ruleResult.policy.Name,
			PolicyType:      "ai",
			Action:          config.PolicyAction(ruleResult.result),
			Mode:            ruleResult.policy.Mode,
			Message:         ruleResult.message,
			RedactedContent: ruleResult.redacted,
			DurationMs:      ruleResult.evaluationMs,
			Error:           func() string { if ruleResult.err != nil { return ruleResult.err.Error() }; return "" }(),
		})

		if ruleResult.err != nil {
			e.logger.Error(ctx, "Response policy evaluation failed",
				zap.String("policy", ruleResult.policy.Name),
				zap.Error(ruleResult.err),
			)
		}

		// Check if this rule triggers early termination (for enabled rules only)
		if !decided && ruleResult.policy.Mode == config.PolicyModeEnabled {
			switch ruleResult.result {
			case "deny":
				if phaseTracker != nil {
					phaseTracker.MarkDecided()
				}
				finalAction = "deny"
				decidingRule = ruleResult.policy.Name
				decidingReason = ruleResult.message
				results.Allowed = false
				decided = true
				earlyTerminated = true
				cancelEval() // Cancel remaining evaluations
			case "error":
				// Errors on enabled policies fail open (allow) - don't block responses due to AI failures
				// The error is still logged and recorded in audit, but doesn't affect the decision
				e.logger.Warn(ctx, "AI response policy evaluation failed, failing open",
					zap.String("rule", ruleResult.policy.Name),
					zap.Error(ruleResult.err),
				)
			case "redact":
				if finalAction == "allow" {
					finalAction = "redact"
				}
				if ruleResult.redacted != "" {
					redactedContent = &ruleResult.redacted
				}
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

	// Set final result
	switch finalAction {
	case "deny":
		results.Allowed = false
		results.Message = decidingReason
		if results.Message == "" {
			results.Message = "Response denied by AI policy"
		}
	case "redact":
		results.Message = "Response content redacted by AI policy"
	default:
		results.Message = "All AI response policies passed"
	}

	// Build AIDetails for audit
	aiDetails := &AuditAIResult{
		Action:       finalAction,
		BlockedMs:    blockedMs,
		EvaluationMs: evaluationMs,
		Results:      ruleResults,
	}
	if decidingRule != "" {
		aiDetails.DecidingRule = decidingRule
		aiDetails.Reason = decidingReason
	}
	results.AIDetails = aiDetails

	e.logger.Info(ctx, "Response evaluation complete",
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
		zap.String("final_action", finalAction),
		zap.Bool("early_terminated", earlyTerminated),
		zap.Int64("blocked_ms", blockedMs),
		zap.Int64("evaluation_ms", evaluationMs),
	)

	return results, nil
}

// formatResponseForAI formats the response for AI evaluation
func (e *AIResponsePolicyEngine) formatResponseForAI(result *mcp.CallToolResult) string {
	formatted := fmt.Sprintf("IsError: %v\n", result.IsError)

	if len(result.Content) > 0 {
		formatted += "Content:\n"
		for i, item := range result.Content {
			switch v := item.(type) {
			case mcp.TextContent:
				formatted += fmt.Sprintf("  [%d] Text: %s\n", i, v.Text)
			case mcp.ImageContent:
				formatted += fmt.Sprintf("  [%d] Image (MIME: %s, Data length: %d bytes)\n", i, v.MIMEType, len(v.Data))
			case mcp.EmbeddedResource:
				formatted += fmt.Sprintf("  [%d] Resource: %+v\n", i, v.Resource)
			}
		}
	}

	if result.Meta != nil && len(result.Meta.AdditionalFields) > 0 {
		formatted += fmt.Sprintf("Meta: %+v\n", result.Meta.AdditionalFields)
	}

	return formatted
}

// ResponseAIValidationHandler handles AI response validation
type ResponseAIValidationHandler struct {
	logger *config.SessionLogger
	engine *AIResponsePolicyEngine
}

// NewResponseAIValidationHandler creates a new AI response validation handler
func NewResponseAIValidationHandler(logger *config.SessionLogger, engine *AIResponsePolicyEngine) *ResponseAIValidationHandler {
	return &ResponseAIValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleResponse implements ResponseValidationHandler
func (h *ResponseAIValidationHandler) HandleResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	// Extract blocking budget from context if available
	budget := BlockingBudgetFromContext(ctx)
	return h.engine.EvaluateResponse(ctx, req, result, budget)
}
