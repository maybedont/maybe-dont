package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
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
	cfg                 *config.Config   // Full config needed for AIProviderClient factory
	providerClient      AIProviderClient // Provider-agnostic AI client
	maxRuleEvaluationMs int              // Max time for any single rule to complete
}

// InitAIResponsePolicyEngine initializes the AI response policy engine
func InitAIResponsePolicyEngine(ctx context.Context, logger *config.SessionLogger, engine *AIResponsePolicyEngine) error {
	engine.logger = logger
	// Only create client if not already set (allows injecting mock for tests)
	if engine.providerClient == nil {
		engine.providerClient = NewAIProviderClient(engine.cfg)
	}
	return nil
}

// LoadPolicies loads AI response policies from configuration
// topLevelMode is the top-level mode that applies to all policies (audit_only makes all rules audit_only)
func (e *AIResponsePolicyEngine) LoadPolicies(policies []config.AIResponsePolicy, topLevelMode config.PolicyMode) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Track seen policy names to detect duplicates
	seenNames := make(map[string]bool)

	// Validate each policy
	for _, policy := range policies {
		// Check for duplicate names
		if seenNames[policy.Name] {
			return fmt.Errorf("duplicate policy name '%s' in AI response rules", policy.Name)
		}
		seenNames[policy.Name] = true

		// Skip disabled policies (enabled: false)
		if !policy.IsEnabled() {
			e.logger.Debug(context.Background(), "Skipping disabled AI response policy",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Resolve effective mode for this policy
		// Top-level audit_only applies to all rules; per-rule audit_only is additive
		effectiveMode := config.ResolvePolicyMode(topLevelMode, policy.Mode)

		e.logger.Debug(context.Background(), "Loading AI response policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Reject prompts containing %s — the engine appends response content automatically
		if strings.Contains(policy.Prompt, "%s") {
			return fmt.Errorf("policy '%s' prompt must not contain %%s placeholder; the engine appends response content automatically", policy.Name)
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

	e.logger.Debug(context.Background(), "Loaded AI response policies", zap.Int("count", len(e.policies)))
	return nil
}

// AIResponseEvaluation represents the AI's response evaluation
type AIResponseEvaluation struct {
	Allowed         bool   `json:"allowed"`
	Message         string `json:"message"`
	RedactedContent string `json:"redacted_content"`
}

// DetermineResponseDecision maps a response policy's action type and model output to a
// final decision string. Redact rules check for redacted_content; deny rules check the
// allowed field. This is the single source of truth for response decision logic —
// both the production engine and the test executor must use this function.
func DetermineResponseDecision(action config.PolicyAction, allowed bool, redactedContent string) string {
	if action == config.PolicyActionRedact {
		if redactedContent != "" {
			return "redact"
		}
		return "allow"
	}
	if !allowed {
		return "deny"
	}
	return "allow"
}

// aiResponseRuleResult represents the result of evaluating a single AI response rule
type aiResponseRuleResult struct {
	policy       AIResponsePolicy
	result       string // "allow", "deny", "redact", or "error"
	message      string
	redacted     string
	evaluationMs int64
	err          error  // Actual error if result is "error"
	errCategory  string // Error classification: "api_error", "timeout", "canceled", "parse_error", "no_response"
}

// EvaluateResponse evaluates a response against all policies.
// The optional budget parameter enables blocking time tracking for cumulative budget management.
// When budget is nil, no blocking time is tracked.
//
// Async behavior for audit_only policies:
// - When ALL policies are audit_only, the function returns immediately with Allowed=true
//   and a non-nil AsyncCompletion channel. Results are collected in the background.
// - When there are ENABLED policies, the function blocks until all enabled policies complete
//   (or one denies). If audit_only policies are still running, they continue in the background
//   and results are sent on the AsyncCompletion channel.
func (e *AIResponsePolicyEngine) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, toolResult *mcp.CallToolResult, budget *BlockingBudget) (ResponseValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	phaseStart := time.Now()
	var phaseTracker *PhaseTracker
	if budget != nil {
		phaseTracker = budget.StartPhase()
	}

	e.logger.Debug(ctx, "Evaluating response with AI policies",
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

	// Count enabled and audit_only policies
	var enabledPolicies, auditOnlyPolicies int
	for _, p := range e.policies {
		if p.Mode.IsAuditOnly() {
			auditOnlyPolicies++
		} else {
			enabledPolicies++
		}
	}
	allAuditOnly := enabledPolicies == 0 && auditOnlyPolicies > 0

	// Create a channel to collect results from goroutines
	resultChan := make(chan aiResponseRuleResult, len(e.policies))

	// Format the response for the AI once
	responseStr := e.formatResponseForAI(toolResult)

	// Determine timeout for individual rule evaluation
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
			startTime := time.Now()

			// Use a detached context (not derived from request context) so all policies
			// complete even after the request returns or an early decision is made.
			// This ensures the audit log captures complete results from all policies.
			policyCtx, cancel := context.WithTimeout(context.Background(), ruleTimeout)
			defer cancel()

			// Build the prompt by appending the response content.
			// The engine owns context injection — policy authors write detection logic only.
			userPrompt := p.Prompt + "\n\nResponse content:\n" + responseStr

			// Call the AI API using the provider-agnostic interface
			result, err := e.providerClient.Generate(policyCtx, AIRequest{
				Model:          e.cfg.Validation.AI.Model,
				UserPrompt:     userPrompt,
				ResponseSchema: GenerateSchema[AIResponseEvaluation](),
				Parameters:     e.cfg.Validation.AI.Parameters,
			})

			durationMs := time.Since(startTime).Milliseconds()

			if err != nil {
				errCategory := classifyProviderError(err)
				resultChan <- aiResponseRuleResult{
					policy:       p,
					result:       "error",
					message:      fmt.Sprintf("Failed to evaluate response policy: %v", err),
					evaluationMs: durationMs,
					err:          err,
					errCategory:  errCategory,
				}
				return
			}

			// Sanitize invalid escape sequences that AI models may produce
			// (e.g., C:\Windows → \W) when echoing text from policy prompts.
			var evaluation AIResponseEvaluation
			err = json.Unmarshal(SanitizeJSONEscapes(result.ParsedJSON), &evaluation)
			if err != nil {
				resultChan <- aiResponseRuleResult{
					policy:       p,
					result:       "error",
					message:      fmt.Sprintf("Failed to parse AI response: %v", err),
					evaluationMs: durationMs,
					err:          err,
					errCategory:  "parse_error",
				}
				return
			}

			resultStr := DetermineResponseDecision(p.Action, evaluation.Allowed, evaluation.RedactedContent)

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

	// If all policies are audit_only, return immediately with async completion
	if allAuditOnly {
		if phaseTracker != nil {
			phaseTracker.MarkDecided()
		}

		completionChan := make(chan AsyncCompletion, 1)

		// Extract request_id and session_id for async logging (original ctx may be cancelled)
		requestID := "-"
		if rid, ok := ctx.Value(config.RequestIDKey).(string); ok {
			requestID = rid
		}
		sessionID := "-"
		if sid, ok := ctx.Value(config.SessionIDKey).(string); ok {
			sessionID = sid
		}

		go func() {
			defer close(completionChan)

			// Create a context with request_id and session_id for async logging
			asyncCtx := context.WithValue(context.Background(), config.RequestIDKey, requestID)
			asyncCtx = context.WithValue(asyncCtx, config.SessionIDKey, sessionID)

			auditResults := make([]AuditAIRuleResult, 0, len(e.policies))
			for i := 0; i < len(e.policies); i++ {
				result := <-resultChan
				auditResult := AuditAIRuleResult{
					Rule:         result.policy.Name,
					Action:       string(result.policy.Action),
					Mode:         "audit_only",
					Result:       result.result,
					EvaluationMs: result.evaluationMs,
				}
				if result.err != nil {
					auditResult.Error = formatAuditError(result.errCategory, result.err)
				}
				auditResults = append(auditResults, auditResult)

				// Log successful results (errors are logged at ERROR level elsewhere)
				if result.err == nil {
					e.logger.Debug(asyncCtx, "AI response policy evaluation result (async)",
						zap.String("rule", result.policy.Name),
						zap.String("action", string(result.policy.Action)),
						zap.String("result", result.result),
						zap.Int64("evaluation_ms", result.evaluationMs),
					)
				}
			}

			evaluationMs := time.Since(phaseStart).Milliseconds()
			if phaseTracker != nil {
				_, evaluationMs = phaseTracker.Finalize()
			}

			aiDetails := &AuditAIResult{
				Action:       "allow",
				BlockedMs:    0,
				EvaluationMs: evaluationMs,
				Results:      auditResults,
			}

			completionChan <- AsyncCompletion{
				AIDetails:    aiDetails,
				EvaluationMs: evaluationMs,
			}

			e.logger.Debug(asyncCtx, "Response evaluation complete (async)",
				zap.Bool("allowed", true),
				zap.Int64("blocked_ms", int64(0)),
				zap.Int64("evaluation_ms", evaluationMs),
			)
		}()

		results.Allowed = true
		results.Message = "All policies are audit_only, proceeding without blocking"
		results.AsyncCompletion = completionChan

		e.logger.Debug(ctx, "AI response validation returning immediately - all policies are audit_only")
		return results, nil
	}

	// Regular synchronous flow for enabled policies
	ruleResults := make([]AuditAIRuleResult, 0, len(e.policies))
	var decidingRule, decidingReason string
	finalAction := "allow"
	decided := false
	var redactedContent *string
	var auditOnlyDeny bool // Track if any audit_only policy returned deny
	var failedOpen bool    // Track if we failed open due to errors

	remainingEnabled := enabledPolicies
	remainingTotal := len(e.policies)

	for remainingTotal > 0 {
		// Check if all enabled policies have completed
		if !decided && remainingEnabled == 0 {
			if phaseTracker != nil {
				phaseTracker.MarkDecided()
			}
			decided = true
		}

		// If decided and there are still audit_only policies running, continue async
		if decided && remainingTotal > 0 {
			if auditOnlyPolicies > 0 && remainingTotal > 0 {
				completionChan := make(chan AsyncCompletion, 1)

				capturedResults := make([]AuditAIRuleResult, len(ruleResults))
				copy(capturedResults, ruleResults)
				capturedRemaining := remainingTotal
				capturedDecidingRule := decidingRule
				capturedDecidingReason := decidingReason
				capturedFinalAction := finalAction

				var capturedBlockedMs int64
				if phaseTracker != nil {
					capturedBlockedMs, _ = phaseTracker.Finalize()
				}

				// Extract request_id and session_id for async logging (original ctx may be cancelled)
				requestID := "-"
				if rid, ok := ctx.Value(config.RequestIDKey).(string); ok {
					requestID = rid
				}
				sessionID := "-"
				if sid, ok := ctx.Value(config.SessionIDKey).(string); ok {
					sessionID = sid
				}

				go func() {
					defer close(completionChan)

					// Create a context with request_id and session_id for async logging
					asyncCtx := context.WithValue(context.Background(), config.RequestIDKey, requestID)
					asyncCtx = context.WithValue(asyncCtx, config.SessionIDKey, sessionID)

					asyncResults := capturedResults
					for i := 0; i < capturedRemaining; i++ {
						result := <-resultChan
						auditResult := AuditAIRuleResult{
							Rule:         result.policy.Name,
							Action:       string(result.policy.Action),
							Result:       result.result,
							EvaluationMs: result.evaluationMs,
						}
						if result.policy.Mode == config.PolicyModeAuditOnly {
							auditResult.Mode = "audit_only"
						}
						if result.err != nil {
							auditResult.Error = formatAuditError(result.errCategory, result.err)
						}
						asyncResults = append(asyncResults, auditResult)

						// Log successful results (errors are logged at ERROR level elsewhere)
						if result.err == nil {
							e.logger.Debug(asyncCtx, "AI response policy evaluation result (async continuation)",
								zap.String("rule", result.policy.Name),
								zap.String("result", result.result),
								zap.Int64("evaluation_ms", result.evaluationMs),
							)
						}
					}

					evaluationMs := time.Since(phaseStart).Milliseconds()

					aiDetails := &AuditAIResult{
						Action:       capturedFinalAction,
						BlockedMs:    capturedBlockedMs,
						EvaluationMs: evaluationMs,
						Results:      asyncResults,
					}
					if capturedDecidingRule != "" {
						aiDetails.DecidingRule = capturedDecidingRule
						aiDetails.Reason = capturedDecidingReason
					}

					completionChan <- AsyncCompletion{
						AIDetails:    aiDetails,
						EvaluationMs: evaluationMs,
					}
				}()

				results.AsyncCompletion = completionChan
			}
			break
		}

		select {
		case ruleResult := <-resultChan:
			remainingTotal--
			if !ruleResult.policy.Mode.IsAuditOnly() {
				remainingEnabled--
			}

			auditResult := AuditAIRuleResult{
				Rule:         ruleResult.policy.Name,
				Action:       string(ruleResult.policy.Action),
				Mode:         modeToAuditString(ruleResult.policy.Mode),
				Result:       ruleResult.result,
				EvaluationMs: ruleResult.evaluationMs,
			}
			if ruleResult.err != nil {
				auditResult.Error = formatAuditError(ruleResult.errCategory, ruleResult.err)
			}
			ruleResults = append(ruleResults, auditResult)

			// Track audit_only denies for AuditModeBypass
			if ruleResult.policy.Mode == config.PolicyModeAuditOnly && ruleResult.result == "deny" {
				auditOnlyDeny = true
			}

			results.Results = append(results.Results, ResponseValidationResult{
				PolicyName:      ruleResult.policy.Name,
				PolicyType:      "ai",
				Action:          config.PolicyAction(ruleResult.result),
				Mode:            ruleResult.policy.Mode,
				Message:         ruleResult.message,
				RedactedContent: ruleResult.redacted,
				DurationMs:      ruleResult.evaluationMs,
				Error:           func() string { if ruleResult.err != nil { return formatAuditError(ruleResult.errCategory, ruleResult.err) }; return "" }(),
			})

			// Log results
			if ruleResult.err != nil {
				// ERROR: Full error details for all errors
				// With detached contexts, cancellations should only occur due to external factors
				// like gateway shutdown, not from early termination logic
				e.logger.Error(ctx, "AI response policy evaluation failed",
					zap.String("rule", ruleResult.policy.Name),
					zap.String("category", ruleResult.errCategory),
					zap.Int64("evaluation_ms", ruleResult.evaluationMs),
					zap.Error(ruleResult.err),
				)
			} else {
				e.logger.Debug(ctx, "AI response policy evaluation result",
					zap.String("rule", ruleResult.policy.Name),
					zap.String("action", string(ruleResult.policy.Action)),
					zap.String("result", ruleResult.result),
					zap.Int64("evaluation_ms", ruleResult.evaluationMs),
				)
			}

			if !decided && !ruleResult.policy.Mode.IsAuditOnly() {
				switch ruleResult.result {
				case "deny":
					// Note: We don't cancel remaining goroutines - they continue to completion
					// so the audit log captures complete results from all policies
					if phaseTracker != nil {
						phaseTracker.MarkDecided()
					}
					finalAction = "deny"
					decidingRule = ruleResult.policy.Name
					decidingReason = ruleResult.message
					results.Allowed = false
					decided = true
				case "error":
					failedOpen = true
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

		case <-time.After(100 * time.Millisecond):
			continue
		}
	}

	var blockedMs, evaluationMs int64
	if phaseTracker != nil {
		blockedMs, evaluationMs = phaseTracker.Finalize()
	} else {
		evaluationMs = time.Since(phaseStart).Milliseconds()
	}

	if redactedContent != nil && finalAction == "redact" {
		results.RedactedContent = redactedContent
	}

	// Set FailedOpen flag
	results.FailedOpen = failedOpen

	switch finalAction {
	case "deny":
		results.Allowed = false
		results.Message = decidingReason
		if results.Message == "" {
			results.Message = "Response denied by AI policy"
		}
		results.RecommendedAction = config.PolicyActionDeny
	case "redact":
		results.Message = "Response content redacted by AI policy"
		results.RecommendedAction = config.PolicyActionRedact
	default:
		if failedOpen {
			results.Message = "AI response validation failed, allowing response (fail-open)"
			// RecommendedAction stays empty for fail-open (unknown recommendation)
		} else if auditOnlyDeny {
			results.AuditModeBypass = true
			results.RecommendedAction = config.PolicyActionDeny
			results.Message = "AI response policy would deny but mode is audit_only"
		} else {
			results.Message = "All AI response policies passed"
			results.RecommendedAction = config.PolicyActionAllow
		}
	}

	if results.AsyncCompletion == nil {
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
	}

	e.logger.Debug(ctx, "Response evaluation complete",
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
		zap.String("final_action", finalAction),
		zap.Bool("failed_open", failedOpen),
		zap.Bool("audit_mode_bypass", auditOnlyDeny),
		zap.Int64("blocked_ms", blockedMs),
		zap.Int64("evaluation_ms", evaluationMs),
		zap.Bool("has_async", results.AsyncCompletion != nil),
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
