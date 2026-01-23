package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// CELPolicy represents a single CEL policy rule
type CELPolicy struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Expression  string              `yaml:"expression"`
	Action      config.PolicyAction `yaml:"action"` // allow or deny
	Message     string              `yaml:"message"`
	Mode        config.PolicyMode   `yaml:"mode"` // enabled, audit_only, or disabled
}

// CELPolicyEngine handles CEL policy evaluation.
//
// NOTE: Unlike AI engines, CEL evaluation is synchronous even for audit_only policies.
// This is intentional because CEL evaluation is fast (<10ms) and deterministic with no
// external API calls. The minimal blocking from audit_only rules is acceptable given
// the complexity cost of async infrastructure for sub-10ms operations.
//
// If CEL execution time increases significantly in the future (e.g., complex expressions,
// large datasets, or external data lookups), consider implementing async behavior similar
// to AI engines. See docs/specs/validation-chain-audit-schema.md "Async Behavior Scope".
type CELPolicyEngine struct {
	logger   *config.SessionLogger
	env      *cel.Env
	policies []CELPolicy
	mu       sync.RWMutex
}

// NewCELPolicyEngine creates a new CEL policy engine
func NewCELPolicyEngine(ctx context.Context, logger *config.SessionLogger) (*CELPolicyEngine, error) {
	// Create CEL environment with custom functions and safe field access
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		cel.Variable("auth", cel.DynType),
		cel.Variable("response", cel.DynType),
		cel.Function("has",
			cel.Overload("has_map_string", []*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType}, cel.BoolType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					obj, ok := lhs.(traits.Mapper)
					if !ok {
						return types.Bool(false)
					}
					field, ok := rhs.(types.String)
					if !ok {
						return types.Bool(false)
					}
					_, found := obj.Find(field)
					return types.Bool(found)
				}),
			),
		),
		cel.Function("get",
			cel.Overload("get_map_string_dyn", []*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.DynType}, cel.DynType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					if len(args) != 3 {
						return types.NewErr("get() requires exactly 3 arguments")
					}
					obj, ok := args[0].(traits.Mapper)
					if !ok {
						return args[2]
					}
					field, ok := args[1].(types.String)
					if !ok {
						return args[2]
					}
					val, found := obj.Find(field)
					if !found {
						return args[2]
					}
					return val
				}),
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELPolicyEngine{
		logger:   logger,
		env:      env,
		policies: make([]CELPolicy, 0),
	}, nil
}

// LoadPolicies loads policies from configuration
// topLevelMode is the top-level mode that applies to all policies (audit_only makes all rules audit_only)
func (e *CELPolicyEngine) LoadPolicies(policies []config.Policy, topLevelMode config.PolicyMode) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Track seen policy names to detect duplicates
	seenNames := make(map[string]bool)

	// Validate and compile each policy
	for _, policy := range policies {
		// Check for duplicate names
		if seenNames[policy.Name] {
			return fmt.Errorf("duplicate policy name '%s' in CEL request rules", policy.Name)
		}
		seenNames[policy.Name] = true

		// Skip disabled policies (enabled: false)
		if !policy.IsEnabled() {
			e.logger.Debug(context.Background(), "Skipping disabled request policy",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Resolve effective mode for this policy
		// Top-level audit_only applies to all rules; per-rule audit_only is additive
		effectiveMode := config.ResolvePolicyMode(topLevelMode, policy.Mode)

		e.logger.Debug(context.Background(), "Loading request policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Compile the expression
		_, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("failed to compile policy %s: %w", policy.Name, issues.Err())
		}

		// Validate action, request validation can only be 'allow' or 'deny'
		if policy.Action != config.PolicyActionAllow && policy.Action != config.PolicyActionDeny {
			return fmt.Errorf("invalid action '%s' for policy %s: must be 'allow' or 'deny'", policy.Action, policy.Name)
		}

		// Store the compiled policy with resolved mode
		e.policies = append(e.policies, CELPolicy{
			Name:        policy.Name,
			Description: policy.Description,
			Expression:  policy.Expression,
			Action:      policy.Action,
			Message:     policy.Message,
			Mode:        effectiveMode,
		})
	}

	e.logger.Debug(context.Background(), "Loaded CEL request policies", zap.Int("count", len(e.policies)))
	return nil
}

// EvaluateToolCall evaluates a tool call request against all policies.
// The optional budget parameter enables blocking time tracking for cumulative budget management.
// When budget is nil, no blocking time is tracked.
func (e *CELPolicyEngine) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest, budget *BlockingBudget) (ValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	phaseStart := time.Now()
	var phaseTracker *PhaseTracker
	if budget != nil {
		phaseTracker = budget.StartPhase()
	}

	e.logger.Debug(ctx, "Evaluating tool call with CEL policies",
		zap.String("tool", req.Params.Name),
		zap.Any("arguments", req.Params.Arguments),
		zap.Int("policy_count", len(e.policies)),
	)

	// Create evaluation context with proper structure
	// Handle nil Meta by converting to empty map for CEL evaluation
	var meta interface{}
	if req.Request.Params.Meta != nil {
		meta = req.Request.Params.Meta
	} else {
		meta = map[string]interface{}{}
	}

	vars := map[string]interface{}{
		"request": map[string]interface{}{
			"method": req.Method,
			"params": map[string]interface{}{
				"name":      req.Params.Name,
				"arguments": req.Params.Arguments,
				"meta":      meta,
			},
		},
	}

	results := ValidationResults{
		Results: make([]ValidationResult, 0),
	}

	// Track per-rule results for audit
	ruleResults := make([]AuditRulesRuleResult, 0, len(e.policies))
	var decidingRule, decidingReason string
	finalAction := "allow"
	earlyTerminated := false

	// Evaluate each policy in order
	for _, policy := range e.policies {
		// Track timing for this policy evaluation
		ruleStart := time.Now()

		e.logger.Debug(ctx, "Evaluating CEL policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
		)

		// Compile the expression
		ast, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			ruleDurationMs := time.Since(ruleStart).Milliseconds()
			e.logger.Error(ctx, "CEL policy compilation error",
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
			e.logger.Error(ctx, "CEL policy program error",
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
			e.logger.Error(ctx, "CEL policy evaluation error",
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
		result, ok := out.Value().(bool)
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

		e.logger.Debug(ctx, "CEL policy evaluation result",
			zap.String("name", policy.Name),
			zap.Bool("result", result),
			zap.String("action", string(policy.Action)),
			zap.Int64("evaluation_ms", ruleDurationMs),
		)

		// Determine the effective result based on expression match and action
		// If matched, the result is the action; if not matched, the result is the opposite
		var ruleResult string
		if result {
			ruleResult = string(policy.Action) // "allow" or "deny"
		} else {
			// No match: deny rules effectively allow, allow rules effectively deny
			if policy.Action == config.PolicyActionDeny {
				ruleResult = string(config.PolicyActionAllow)
			} else {
				ruleResult = string(config.PolicyActionDeny)
			}
		}

		ruleResults = append(ruleResults, AuditRulesRuleResult{
			Rule:         policy.Name,
			Action:       string(policy.Action),
			Mode:         modeToAuditString(policy.Mode),
			Result:       ruleResult,
			EvaluationMs: ruleDurationMs,
		})

		// Also add to legacy results for compatibility
		if result {
			results.Results = append(results.Results, ValidationResult{
				PolicyName: policy.Name,
				PolicyType: "cel",
				Action:     policy.Action,
				Mode:       policy.Mode,
				Message:    policy.Message,
				DurationMs: ruleDurationMs,
			})

			if policy.Action == config.PolicyActionDeny {
				// Only count toward final decision if mode is enabled (not audit_only)
				if !policy.Mode.IsAuditOnly() {
					if phaseTracker != nil {
						phaseTracker.MarkDecided()
					}
					finalAction = "deny"
					decidingRule = policy.Name
					decidingReason = policy.Message
					results.DenyCount++
					earlyTerminated = true
					break // Early termination on first enabled deny
				}
			} else if policy.Action == config.PolicyActionAllow {
				// Only count toward final decision if mode is enabled (not audit_only)
				if !policy.Mode.IsAuditOnly() {
					results.AllowCount++
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

	// Set final result
	if finalAction == "deny" {
		results.Allowed = false
		results.Message = decidingReason
	} else if results.FailedOpen {
		results.Allowed = true
		results.Message = "CEL evaluation failed, allowing request (fail-open)"
	} else if results.AllowCount > 0 {
		results.Allowed = true
		// Find the first allow message
		for _, r := range results.Results {
			if r.Action == config.PolicyActionAllow && r.Mode == "" && r.Message != "" {
				results.Message = r.Message
				break
			}
		}
	} else {
		results.Allowed = true // Default to allow if no policies matched
		results.Message = "No policies matched"
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

	e.logger.Debug(ctx, "CEL policy evaluation complete",
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

// modeToAuditString converts PolicyMode to audit string representation.
// Returns empty string for enabled mode (omit in JSON), "audit_only" otherwise.
func modeToAuditString(mode config.PolicyMode) string {
	if mode == config.PolicyModeAuditOnly {
		return "audit_only"
	}
	return "" // Omit "enabled" mode in audit output
}
