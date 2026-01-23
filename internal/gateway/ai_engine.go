package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/openai/openai-go"
	"go.uber.org/zap"
)

// AIPolicy represents a single AI policy rule
type AIPolicy struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Prompt      string              `yaml:"prompt"`
	Action      config.PolicyAction `yaml:"action"` // allow or deny
	Message     string              `yaml:"message"`
	Mode        config.PolicyMode   `yaml:"mode"` // enabled, audit_only, or disabled
}

// AIPolicyEngine handles AI policy evaluation
type AIPolicyEngine struct {
	logger              *config.SessionLogger
	policies            []AIPolicy
	mu                  sync.RWMutex
	endpoint            string
	model               string
	apiKey              string
	maxRuleEvaluationMs int
	client              AIClient
}

// InitAIPolicyEngine creates a new AI policy engine
func InitAIPolicyEngine(logger *config.SessionLogger, engine *AIPolicyEngine) error {
	// Set the logger
	engine.logger = logger

	// Only create client if not already set (allows injecting mock for tests)
	if engine.client == nil {
		engine.client = NewOpenAIClient(engine.apiKey)
	}
	return nil
}

// LoadPolicies loads AI policies from configuration
// topLevelMode is the top-level mode that applies to all policies (audit_only makes all rules audit_only)
func (e *AIPolicyEngine) LoadPolicies(policies []config.AIPolicy, topLevelMode config.PolicyMode) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Track seen policy names to detect duplicates
	seenNames := make(map[string]bool)

	// Validate each policy
	for _, policy := range policies {
		// Check for duplicate names
		if seenNames[policy.Name] {
			return fmt.Errorf("duplicate policy name '%s' in AI request rules", policy.Name)
		}
		seenNames[policy.Name] = true

		// Skip disabled policies (enabled: false)
		if !policy.IsEnabled() {
			e.logger.Debug(context.Background(), "Skipping disabled AI policy",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Resolve effective mode for this policy
		// Top-level audit_only applies to all rules; per-rule audit_only is additive
		effectiveMode := config.ResolvePolicyMode(topLevelMode, policy.Mode)

		e.logger.Debug(context.Background(), "Loading AI policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Validate action, request validation can only be 'allow' or 'deny'
		if policy.Action != config.PolicyActionAllow && policy.Action != config.PolicyActionDeny {
			return fmt.Errorf("invalid action '%s' for policy %s: must be 'allow' or 'deny'", policy.Action, policy.Name)
		}

		// Store the compiled policy with resolved mode
		e.policies = append(e.policies, AIPolicy{
			Name:        policy.Name,
			Description: policy.Description,
			Prompt:      policy.Prompt,
			Action:      policy.Action,
			Message:     policy.Message,
			Mode:        effectiveMode,
		})
	}

	e.logger.Debug(context.Background(), "Loaded AI request policies", zap.Int("count", len(e.policies)))
	return nil
}

type AIResponse struct {
	Allowed bool   `json:"allowed"`
	Message string `json:"message"`
}

// aiRuleResult represents the result of a single AI rule evaluation (internal use)
type aiRuleResult struct {
	rule         string              // Rule name
	action       config.PolicyAction // Rule's configured action
	mode         config.PolicyMode   // Rule's mode
	result       string              // "allow", "deny", or "error"
	message      string              // AI response message
	evaluationMs int64               // Time for this rule to complete
	err          error               // Actual error if result is "error"
	errCategory  string              // Error classification: "api_error", "timeout", "canceled", "parse_error", "no_response"
}

// EvaluateToolCall evaluates a tool call request against all policies with early termination.
// The optional budget parameter enables blocking time tracking for cumulative budget management.
// When budget is nil, no blocking time is tracked and a default timeout is used.
//
// Async behavior for audit_only policies:
// - When ALL policies are audit_only, the function returns immediately with Allowed=true
//   and a non-nil AsyncCompletion channel. Results are collected in the background.
// - When there are ENABLED policies, the function blocks until all enabled policies complete
//   (or one denies). If audit_only policies are still running, they continue in the background
//   and results are sent on the AsyncCompletion channel.
func (e *AIPolicyEngine) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest, budget *BlockingBudget) (ValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var results ValidationResults
	results.Results = make([]ValidationResult, 0)

	if len(e.policies) == 0 {
		results.Allowed = true
		results.Message = "No policies configured"
		return results, nil
	}

	// Track timing using shared budget if available
	var phaseTracker *PhaseTracker
	if budget != nil {
		phaseTracker = budget.StartPhase()
	}
	evalStartTime := time.Now()

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

	// Channel to collect results from goroutines
	resultChan := make(chan aiRuleResult, len(e.policies))

	// Format the tool call request for the AI once
	toolCallStr := fmt.Sprintf("Tool: %s\nArguments: %v", req.Params.Name, req.Params.Arguments)

	// Determine timeout for individual rule evaluation
	ruleTimeout := time.Duration(e.maxRuleEvaluationMs) * time.Millisecond
	if ruleTimeout <= 0 {
		ruleTimeout = 10 * time.Second // Default fallback
	}

	// Launch a goroutine for each policy
	for _, policy := range e.policies {
		go func(p AIPolicy) {
			startTime := time.Now()

			// Use a detached context (not derived from request context) so all policies
			// complete even after the request returns or an early decision is made.
			// This ensures the audit log captures complete results from all policies.
			policyCtx, cancel := context.WithTimeout(context.Background(), ruleTimeout)
			defer cancel()

			schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:        "ai_response",
				Description: openai.String("Response from the AI policy engine"),
				Schema:      GenerateSchema[AIResponse](),
				Strict:      openai.Bool(true),
			}

			// Call the AI API
			chatCompletion, err := e.client.CreateChatCompletion(policyCtx, openai.ChatCompletionNewParams{
				Messages: []openai.ChatCompletionMessageParamUnion{
					openai.UserMessage(fmt.Sprintf(p.Prompt, toolCallStr)),
				},
				Model: openai.ChatModel(e.model),
				ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
					OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
						JSONSchema: schemaParam,
					},
				},
			})

			durationMs := time.Since(startTime).Milliseconds()

			// Handle errors
			if err != nil {
				errCategory := classifyContextError(policyCtx, "api_error")
				resultChan <- aiRuleResult{
					rule:         p.Name,
					action:       p.Action,
					mode:         p.Mode,
					result:       "error",
					evaluationMs: durationMs,
					err:          err,
					errCategory:  errCategory,
				}
				return
			}

			if len(chatCompletion.Choices) == 0 {
				resultChan <- aiRuleResult{
					rule:         p.Name,
					action:       p.Action,
					mode:         p.Mode,
					result:       "error",
					evaluationMs: durationMs,
					err:          fmt.Errorf("API returned empty choices"),
					errCategory:  "no_response",
				}
				return
			}

			// Parse the response as JSON
			var aiResp AIResponse
			if err := json.Unmarshal([]byte(chatCompletion.Choices[0].Message.Content), &aiResp); err != nil {
				resultChan <- aiRuleResult{
					rule:         p.Name,
					action:       p.Action,
					mode:         p.Mode,
					result:       "error",
					evaluationMs: durationMs,
					err:          err,
					errCategory:  "parse_error",
				}
				return
			}

			// Determine result based on rule action and AI response
			// - deny rule + AI says false -> result is "deny"
			// - deny rule + AI says true -> result is "allow" (no issue found)
			// - allow rule + AI says true -> result is "allow" (gate passed)
			// - allow rule + AI says false -> result is "deny" (gate failed)
			var resultAction string
			if p.Action == config.PolicyActionDeny {
				if aiResp.Allowed {
					resultAction = "allow" // No issue found
				} else {
					resultAction = "deny" // Issue found, deny
				}
			} else { // allow policy
				if aiResp.Allowed {
					resultAction = "allow" // Gate passed
				} else {
					resultAction = "deny" // Gate failed, implicit deny
				}
			}

			resultChan <- aiRuleResult{
				rule:         p.Name,
				action:       p.Action,
				mode:         p.Mode,
				result:       resultAction,
				message:      aiResp.Message,
				evaluationMs: durationMs,
			}
		}(policy)
	}

	// If all policies are audit_only, return immediately with async completion
	if allAuditOnly {
		// Mark phase as decided immediately to avoid consuming budget
		if phaseTracker != nil {
			phaseTracker.MarkDecided()
		}

		// Create completion channel for async results
		completionChan := make(chan AsyncCompletion, 1)

		// Start background goroutine to collect all results
		go func() {
			defer close(completionChan)

			auditResults := make([]AuditAIRuleResult, 0, len(e.policies))
			for i := 0; i < len(e.policies); i++ {
				result := <-resultChan
				auditResult := AuditAIRuleResult{
					Rule:         result.rule,
					Action:       string(result.action),
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
					e.logger.Debug(context.Background(), "AI policy evaluation result (async)",
						zap.String("rule", result.rule),
						zap.String("action", string(result.action)),
						zap.String("result", result.result),
						zap.Int64("evaluation_ms", result.evaluationMs),
					)
				}
			}

			evaluationMs := time.Since(evalStartTime).Milliseconds()

			// Finalize phase tracking
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

			e.logger.Debug(context.Background(), "Tool call evaluation complete (async)",
				zap.Bool("allowed", true),
				zap.Int64("blocked_ms", int64(0)),
				zap.Int64("evaluation_ms", evaluationMs),
			)
		}()

		// Return immediately with allow decision and async completion channel
		results.Allowed = true
		results.Message = "All policies are audit_only, proceeding without blocking"
		results.AllowCount = 1
		results.AsyncCompletion = completionChan

		e.logger.Debug(ctx, "AI validation returning immediately - all policies are audit_only")
		return results, nil
	}

	// Regular synchronous flow for enabled policies
	var auditResults []AuditAIRuleResult
	var decidingRule string
	var decidingReason string
	var blockedMs int64
	var decided bool
	var finalAction = "allow"
	var auditOnlyDeny bool // Track if any audit_only policy returned deny
	var failedOpen bool    // Track if we failed open due to errors

	// BlockingBudget is required - fail fast if not provided
	if budget == nil {
		return ValidationResults{}, fmt.Errorf("BlockingBudget is required for AI validation - this indicates a code path that bypasses budget initialization")
	}

	// Determine blocking deadline from shared budget
	var blockingDeadline time.Time
	if budget.IsExhausted() {
		blockedMs = 0
		decided = true
		finalAction = "allow"
		failedOpen = true
		e.logger.Warn(ctx, "AI validation skipping blocking - budget already exhausted")
	} else {
		blockingDeadline = budget.BlockingDeadline()
	}

	// Track how many enabled policies we're waiting for
	remainingEnabled := enabledPolicies
	remainingTotal := len(e.policies)

	// Collect results - block only until all enabled policies complete or deny
	for remainingTotal > 0 {
		// Check if we've exceeded blocking time (only matters if not yet decided)
		if !decided && !blockingDeadline.IsZero() && time.Now().After(blockingDeadline) {
			if phaseTracker != nil {
				phaseTracker.MarkDecided()
			}
			blockedMs = time.Since(evalStartTime).Milliseconds()
			decided = true
			finalAction = "allow" // Fail open on timeout
			failedOpen = true
			e.logger.Warn(ctx, "AI validation exceeded max blocking time, failing open",
				zap.Int64("blocked_ms", blockedMs),
				zap.Int("remaining_policies", remainingTotal),
			)
		}

		// Check if all enabled policies have completed (decision can be made)
		if !decided && remainingEnabled == 0 {
			if phaseTracker != nil {
				phaseTracker.MarkDecided()
			}
			blockedMs = time.Since(evalStartTime).Milliseconds()
			decided = true
			// finalAction remains "allow" since no enabled policy denied
		}

		// If decided and there are still audit_only policies running, continue async
		if decided && remainingTotal > 0 {
			// Create completion channel for remaining audit_only results
			if auditOnlyPolicies > 0 && remainingTotal > 0 {
				completionChan := make(chan AsyncCompletion, 1)

				// Capture current state for background goroutine
				capturedResults := make([]AuditAIRuleResult, len(auditResults))
				copy(capturedResults, auditResults)
				capturedRemaining := remainingTotal
				capturedDecidingRule := decidingRule
				capturedDecidingReason := decidingReason
				capturedFinalAction := finalAction
				capturedBlockedMs := blockedMs

				go func() {
					defer close(completionChan)

					asyncResults := capturedResults
					for i := 0; i < capturedRemaining; i++ {
						result := <-resultChan
						auditResult := AuditAIRuleResult{
							Rule:         result.rule,
							Action:       string(result.action),
							Result:       result.result,
							EvaluationMs: result.evaluationMs,
						}
						if result.mode == config.PolicyModeAuditOnly {
							auditResult.Mode = "audit_only"
						}
						if result.err != nil {
							auditResult.Error = formatAuditError(result.errCategory, result.err)
						}
						asyncResults = append(asyncResults, auditResult)

						// Log successful results (errors are logged at ERROR level elsewhere)
						if result.err == nil {
							e.logger.Debug(context.Background(), "AI policy evaluation result (async continuation)",
								zap.String("rule", result.rule),
								zap.String("result", result.result),
								zap.Int64("evaluation_ms", result.evaluationMs),
							)
						}
					}

					evaluationMs := time.Since(evalStartTime).Milliseconds()

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

		// Use select with timeout to avoid blocking forever
		select {
		case result := <-resultChan:
			remainingTotal--
			if !result.mode.IsAuditOnly() {
				remainingEnabled--
			}

			// Build audit result entry
			auditResult := AuditAIRuleResult{
				Rule:         result.rule,
				Action:       string(result.action),
				Result:       result.result,
				EvaluationMs: result.evaluationMs,
			}
			if result.mode == config.PolicyModeAuditOnly {
				auditResult.Mode = "audit_only"
			}
			if result.err != nil {
				auditResult.Error = formatAuditError(result.errCategory, result.err)
			}
			auditResults = append(auditResults, auditResult)

			// Track audit_only denies for AuditModeBypass
			if result.mode == config.PolicyModeAuditOnly && result.result == "deny" {
				auditOnlyDeny = true
			}

			// Check if this result should trigger early termination
			if !decided && !result.mode.IsAuditOnly() {
				switch result.result {
				case "deny":
					// Early termination: first enabled deny
					// Note: We don't cancel remaining goroutines - they continue to completion
					// so the audit log captures complete results from all policies
					if phaseTracker != nil {
						phaseTracker.MarkDecided()
					}
					blockedMs = time.Since(evalStartTime).Milliseconds()
					decided = true
					finalAction = "deny"
					decidingRule = result.rule
					decidingReason = result.message
				case "error":
					// Errors on enabled policies fail open (allow)
					failedOpen = true
					e.logger.Warn(ctx, "AI policy evaluation failed, failing open",
						zap.String("rule", result.rule),
						zap.Error(result.err),
					)
				}
			}

			// Log results
			if result.err != nil {
				// ERROR: Full error details for all errors
				// With detached contexts, cancellations should only occur due to external factors
				// like gateway shutdown, not from early termination logic
				e.logger.Error(ctx, "AI policy evaluation failed",
					zap.String("rule", result.rule),
					zap.String("category", result.errCategory),
					zap.Int64("evaluation_ms", result.evaluationMs),
					zap.Error(result.err),
				)
			} else {
				e.logger.Debug(ctx, "AI policy evaluation result",
					zap.String("rule", result.rule),
					zap.String("action", string(result.action)),
					zap.String("result", result.result),
					zap.Int64("evaluation_ms", result.evaluationMs),
				)
			}

		case <-time.After(100 * time.Millisecond):
			// Continue checking, allows us to monitor blocking deadline
			continue
		}
	}

	// Finalize phase tracking and get accurate blocked/evaluation times
	var evaluationMs int64
	if phaseTracker != nil {
		var trackedBlockedMs int64
		trackedBlockedMs, evaluationMs = phaseTracker.Finalize()
		if !allAuditOnly {
			blockedMs = trackedBlockedMs
		}
	} else {
		if !decided {
			blockedMs = time.Since(evalStartTime).Milliseconds()
		}
		evaluationMs = time.Since(evalStartTime).Milliseconds()
	}

	// Build the AIDetails for audit logging (only if not using async completion)
	if results.AsyncCompletion == nil {
		aiDetails := &AuditAIResult{
			Action:       finalAction,
			BlockedMs:    blockedMs,
			EvaluationMs: evaluationMs,
			Results:      auditResults,
		}
		if decidingRule != "" {
			aiDetails.DecidingRule = decidingRule
			aiDetails.Reason = decidingReason
		}
		results.AIDetails = aiDetails
	}

	// Build ValidationResults
	results.Allowed = finalAction == "allow"
	results.FailedOpen = failedOpen
	if finalAction == "deny" {
		results.Message = "Maybe Don't, A policy failed."
		results.DenyCount = 1
		results.RecommendedAction = config.PolicyActionDeny
	} else if failedOpen {
		results.Message = "AI validation failed, allowing request (fail-open)"
		results.AllowCount = 1
		// RecommendedAction stays empty for fail-open (unknown recommendation)
	} else if auditOnlyDeny {
		results.AuditModeBypass = true
		results.RecommendedAction = config.PolicyActionDeny
		results.Message = "AI policy would deny but mode is audit_only"
		results.AllowCount = 1
	} else {
		results.Message = "All policies passed, maybe do."
		results.AllowCount = 1
		results.RecommendedAction = config.PolicyActionAllow
	}

	e.logger.Debug(ctx, "Tool call evaluation complete",
		zap.Bool("allowed", results.Allowed),
		zap.String("deciding_rule", decidingRule),
		zap.Bool("failed_open", failedOpen),
		zap.Bool("audit_mode_bypass", auditOnlyDeny),
		zap.Int64("blocked_ms", blockedMs),
		zap.Int64("evaluation_ms", evaluationMs),
		zap.Bool("has_async", results.AsyncCompletion != nil),
	)

	return results, nil
}

// classifyContextError returns the error category based on context state.
// Returns "timeout" for deadline exceeded, "canceled" for context cancellation,
// or the provided defaultCategory otherwise.
func classifyContextError(ctx context.Context, defaultCategory string) string {
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout"
	} else if ctx.Err() == context.Canceled {
		return "canceled"
	}
	return defaultCategory
}

// formatAuditError formats an error for the audit log as "category: message".
// The message is truncated to 100 characters to keep audit logs concise.
func formatAuditError(category string, err error) string {
	if err == nil {
		return category
	}

	msg := err.Error()

	// Truncate long messages for audit log (keep concise)
	const maxLen = 100
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "..."
	}

	return fmt.Sprintf("%s: %s", category, msg)
}

func GenerateSchema[T any]() interface{} {
	// Structured Outputs uses a subset of JSON schema
	// These flags are necessary to comply with the subset
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}
