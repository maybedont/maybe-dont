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
	"github.com/openai/openai-go/option"
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
	maxBlockingMs       int
	maxRuleEvaluationMs int
	client              *openai.Client
}

// InitAIPolicyEngine creates a new AI policy engine
func InitAIPolicyEngine(logger *config.SessionLogger, engine *AIPolicyEngine) error {
	// Set the logger
	engine.logger = logger

	client := openai.NewClient(
		option.WithAPIKey(engine.apiKey),
	)
	engine.client = &client
	return nil
}

// LoadPolicies loads AI policies from configuration
// defaultMode is the top-level mode that applies to all policies unless overridden per-rule
func (e *AIPolicyEngine) LoadPolicies(policies []config.AIPolicy, defaultMode config.PolicyMode) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info(context.Background(), "Loading AI policies",
		zap.Int("count", len(policies)),
		zap.String("default_mode", string(defaultMode)),
	)

	// Validate each policy
	for _, policy := range policies {
		// Resolve effective mode for this policy
		effectiveMode := config.ResolvePolicyMode(policy.Mode, defaultMode)

		e.logger.Info(context.Background(), "Loading AI policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Skip disabled policies
		if effectiveMode == config.PolicyModeDisabled {
			e.logger.Info(context.Background(), "Skipping disabled AI policy",
				zap.String("name", policy.Name),
			)
			continue
		}

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

	e.logger.Info(context.Background(), "Loaded AI policies", zap.Int("count", len(e.policies)))
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
	err          string              // Error description if result is "error"
}

// EvaluateToolCall evaluates a tool call request against all policies with early termination
func (e *AIPolicyEngine) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var results ValidationResults
	results.Results = make([]ValidationResult, 0)

	if len(e.policies) == 0 {
		results.Allowed = true
		results.Message = "No policies configured"
		return results, nil
	}

	// Track timing
	evalStartTime := time.Now()

	// Count enabled policies to determine blocking behavior
	var enabledPolicies int
	for _, p := range e.policies {
		if p.Mode == config.PolicyModeEnabled {
			enabledPolicies++
		}
	}
	allAuditOnly := enabledPolicies == 0

	// Create a cancellable context for early termination
	evalCtx, cancelEval := context.WithCancel(ctx)
	defer cancelEval()

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

			// Create context with rule timeout
			policyCtx, cancel := context.WithTimeout(evalCtx, ruleTimeout)
			defer cancel()

			schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:        "ai_response",
				Description: openai.String("Response from the AI policy engine"),
				Schema:      GenerateSchema[AIResponse](),
				Strict:      openai.Bool(true),
			}

			// Call the AI API
			chatCompletion, err := e.client.Chat.Completions.New(policyCtx, openai.ChatCompletionNewParams{
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
				errMsg := "api_error"
				if policyCtx.Err() == context.DeadlineExceeded {
					errMsg = "timeout"
				} else if policyCtx.Err() == context.Canceled {
					errMsg = "canceled"
				}
				resultChan <- aiRuleResult{
					rule:         p.Name,
					action:       p.Action,
					mode:         p.Mode,
					result:       "error",
					evaluationMs: durationMs,
					err:          errMsg,
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
					err:          "no_response",
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
					err:          "parse_error",
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

	// Collect results with early termination support
	var auditResults []AuditAIRuleResult
	var decidingRule string
	var decidingReason string
	var blockedMs int64
	var decided bool
	var finalAction = "allow"

	// Determine max blocking time
	maxBlockingDuration := time.Duration(e.maxBlockingMs) * time.Millisecond
	if maxBlockingDuration <= 0 {
		maxBlockingDuration = 5 * time.Second // Default fallback
	}
	blockingDeadline := evalStartTime.Add(maxBlockingDuration)

	// If all policies are audit_only, don't block at all
	if allAuditOnly {
		blockedMs = 0
		decided = true
		finalAction = "allow"
	}

	// Collect results
	remainingPolicies := len(e.policies)
	for remainingPolicies > 0 {
		// Check if we've exceeded blocking time (only matters if not yet decided)
		if !decided && time.Now().After(blockingDeadline) {
			blockedMs = time.Since(evalStartTime).Milliseconds()
			decided = true
			finalAction = "allow" // Fail open on timeout
			e.logger.Warn(ctx, "AI validation exceeded max blocking time, failing open",
				zap.Int64("blocked_ms", blockedMs),
				zap.Int("remaining_policies", remainingPolicies),
			)
		}

		// Use select with timeout to avoid blocking forever
		select {
		case result := <-resultChan:
			remainingPolicies--

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
			if result.err != "" {
				auditResult.Error = result.err
			}
			auditResults = append(auditResults, auditResult)

			// Check if this result should trigger early termination
			if !decided && result.mode == config.PolicyModeEnabled {
				switch result.result {
				case "deny":
					// Early termination: first enabled deny
					blockedMs = time.Since(evalStartTime).Milliseconds()
					decided = true
					finalAction = "deny"
					decidingRule = result.rule
					decidingReason = result.message
					cancelEval() // Cancel remaining goroutines
				case "error":
					// Errors on enabled policies cause deny (fail closed)
					blockedMs = time.Since(evalStartTime).Milliseconds()
					decided = true
					finalAction = "deny"
					decidingRule = result.rule
					decidingReason = fmt.Sprintf("Rule evaluation failed: %s", result.err)
					cancelEval()
				}
			}

			// Log errors
			if result.err != "" {
				e.logger.Error(ctx, "Policy evaluation error",
					zap.String("rule", result.rule),
					zap.String("error", result.err),
				)
			}

		case <-time.After(100 * time.Millisecond):
			// Continue checking, allows us to monitor blocking deadline
			continue
		}
	}

	// If we never decided (all enabled policies passed), record blocking time now
	if !decided {
		blockedMs = time.Since(evalStartTime).Milliseconds()
	}

	// Calculate total evaluation time
	evaluationMs := time.Since(evalStartTime).Milliseconds()

	// Build the AIDetails for audit logging
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

	// Build legacy ValidationResults for compatibility
	results.Allowed = finalAction == "allow"
	if finalAction == "deny" {
		results.Message = "Maybe Don't, A policy failed."
		results.DenyCount = 1
	} else {
		results.Message = "All policies passed, maybe do."
		results.AllowCount = 1
	}
	results.AIDetails = aiDetails

	e.logger.Info(ctx, "Tool call evaluation complete",
		zap.Bool("allowed", results.Allowed),
		zap.String("deciding_rule", decidingRule),
		zap.Int64("blocked_ms", blockedMs),
		zap.Int64("evaluation_ms", evaluationMs),
	)

	return results, nil
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
