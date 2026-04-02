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

// CELPolicy represents a single CEL policy rule with separate expressions
// for MCP tool calls and CLI commands.
type CELPolicy struct {
	Name          string              `yaml:"name"`
	Description   string              `yaml:"description"`
	MCPExpression string              `yaml:"mcp_expression"` // CEL expression for MCP tool calls
	CLIExpression string              `yaml:"cli_expression"` // CEL expression for CLI commands
	Action        config.PolicyAction `yaml:"action"`         // allow or deny
	Message       string              `yaml:"message"`
	Mode          config.PolicyMode   `yaml:"mode"` // "audit_only" or "enforce"
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
		cel.Variable("cli", cel.DynType),
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
func (e *CELPolicyEngine) LoadPolicies(policies []config.Policy, topLevelMode config.PolicyMode, includeDisabled ...bool) error {
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
			return fmt.Errorf("duplicate policy name '%s' in CEL request rules", policy.Name)
		}
		seenNames[policy.Name] = true

		// Skip disabled policies (enabled: false) unless includeDisabled is set
		if !policy.IsEnabled() && !loadDisabled {
			e.logger.Debug(context.Background(), "Skipping disabled request policy",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Resolve effective mode for this policy
		// Top-level audit_only applies to all rules; per-rule audit_only is additive
		effectiveMode := config.ResolvePolicyMode(topLevelMode, policy.Mode)

		// Resolve MCP expression with fallback from legacy Expression field.
		// If MCPExpression is empty, fall back to Expression for backwards compatibility.
		mcpExpr := policy.MCPExpression
		if mcpExpr == "" {
			mcpExpr = policy.Expression // Fallback to legacy field
		}

		e.logger.Debug(context.Background(), "Loading request policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Validate that at least one expression type is provided
		if mcpExpr == "" && policy.CLIExpression == "" {
			return fmt.Errorf("policy %s has no mcp_expression or cli_expression", policy.Name)
		}

		// Compile the MCP expression if provided (CLI expression compilation is deferred to CLI evaluation)
		if mcpExpr != "" {
			_, issues := e.env.Compile(mcpExpr)
			if issues != nil && issues.Err() != nil {
				return fmt.Errorf("failed to compile mcp_expression for policy %s: %w", policy.Name, issues.Err())
			}
		}

		// Validate CLI expression compiles if provided (actual evaluation deferred to CLI evaluation)
		if policy.CLIExpression != "" {
			_, issues := e.env.Compile(policy.CLIExpression)
			if issues != nil && issues.Err() != nil {
				return fmt.Errorf("failed to compile cli_expression for policy %s: %w", policy.Name, issues.Err())
			}
		}

		// Validate action, request validation can only be 'allow' or 'deny'
		if policy.Action != config.PolicyActionAllow && policy.Action != config.PolicyActionDeny {
			return fmt.Errorf("invalid action '%s' for policy %s: must be 'allow' or 'deny'", policy.Action, policy.Name)
		}

		// Store the policy with resolved MCP expression and CLI expression
		e.policies = append(e.policies, CELPolicy{
			Name:          policy.Name,
			Description:   policy.Description,
			MCPExpression: mcpExpr,              // Resolved with fallback
			CLIExpression: policy.CLIExpression, // No fallback, CLI-specific
			Action:        policy.Action,
			Message:       policy.Message,
			Mode:          effectiveMode,
		})
	}

	e.logger.Debug(context.Background(), "Loaded CEL request policies", zap.Int("count", len(e.policies)))
	return nil
}

// EvaluateToolCall evaluates a tool call request against policies that have an MCP expression.
// Policies with only a CLI expression are skipped (they can only match CLI commands).
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
		zap.Strings("argument_keys", extractMCPArgumentKeys(req.Params.Arguments)),
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
	auditOnlyDeny := false

	// Evaluate each policy in order
	for _, policy := range e.policies {
		// Skip policies without MCP expression (CLI-only rules)
		if policy.MCPExpression == "" {
			e.logger.Debug(ctx, "Skipping policy without MCP expression",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Track timing for this policy evaluation
		ruleStart := time.Now()

		e.logger.Debug(ctx, "Evaluating CEL policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
		)

		// Compile the MCP expression for tool call evaluation
		ast, issues := e.env.Compile(policy.MCPExpression)
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
			// Fail-open on compilation error for enforced rules
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
			// Fail-open on program creation error for enforced rules
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
			// Fail-open on evaluation error for enforced rules
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
			// Fail-open on type error for enforced rules
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
				// Only count toward final decision if mode is enforce (not audit_only)
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
				} else {
					auditOnlyDeny = true
				}
			} else if policy.Action == config.PolicyActionAllow {
				// Only count toward final decision if mode is enforce (not audit_only)
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
		results.RecommendedAction = config.PolicyActionDeny
	} else if auditOnlyDeny {
		results.Allowed = true
		results.AuditModeBypass = true
		results.RecommendedAction = config.PolicyActionDeny
		results.Message = "CEL policy would deny but mode is audit_only"
	} else if results.FailedOpen {
		results.Allowed = true
		results.Message = "CEL evaluation failed, allowing request (fail-open)"
	} else if results.AllowCount > 0 {
		results.Allowed = true
		results.RecommendedAction = config.PolicyActionAllow
		// Find the first allow message
		for _, r := range results.Results {
			if r.Action == config.PolicyActionAllow && !r.Mode.IsAuditOnly() && r.Message != "" {
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
// Returns empty string for enforce mode (omit in JSON), "audit_only" otherwise.
func modeToAuditString(mode config.PolicyMode) string {
	if mode == config.PolicyModeAuditOnly {
		return "audit_only"
	}
	return "" // Omit "enforce" mode in audit output
}

// EvaluateCLICommand evaluates a CLI command against all policies.
// Only policies with CLIExpression are evaluated (others are skipped).
// The optional budget parameter enables blocking time tracking for cumulative budget management.
// When budget is nil, no blocking time is tracked.
func (e *CELPolicyEngine) EvaluateCLICommand(ctx context.Context, req *CLIValidationRequest, budget *BlockingBudget) (ValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	phaseStart := time.Now()
	var phaseTracker *PhaseTracker
	if budget != nil {
		phaseTracker = budget.StartPhase()
	}

	e.logger.Debug(ctx, "Evaluating CLI command with CEL policies",
		zap.String("command", req.Command),
		zap.Strings("argument_flags", extractCLIArgumentFlags(req.Arguments)),
		zap.Int("argument_count", len(req.Arguments)),
		zap.Int("policy_count", len(e.policies)),
	)

	// Create evaluation context using BuildCLIContext
	vars := BuildCLIContext(req)

	results := ValidationResults{
		Results: make([]ValidationResult, 0),
	}

	// Track per-rule results for audit
	ruleResults := make([]AuditRulesRuleResult, 0, len(e.policies))
	var decidingRule, decidingReason string
	finalAction := "allow"
	earlyTerminated := false
	auditOnlyDeny := false

	// Evaluate each policy in order
	for _, policy := range e.policies {
		// Skip policies without CLI expression (MCP-only rules)
		if policy.CLIExpression == "" {
			e.logger.Debug(ctx, "Skipping policy without CLI expression",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Track timing for this policy evaluation
		ruleStart := time.Now()

		e.logger.Debug(ctx, "Evaluating CLI CEL policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
		)

		// Compile the CLI expression
		ast, issues := e.env.Compile(policy.CLIExpression)
		if issues != nil && issues.Err() != nil {
			ruleDurationMs := time.Since(ruleStart).Milliseconds()
			e.logger.Error(ctx, "CLI CEL policy compilation error",
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
			// Fail-open on compilation error for enforced rules
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
			e.logger.Error(ctx, "CLI CEL policy program error",
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
			// Fail-open on program creation error for enforced rules
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
			e.logger.Error(ctx, "CLI CEL policy evaluation error",
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
			// Fail-open on evaluation error for enforced rules
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
			// Fail-open on type error for enforced rules
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

		e.logger.Debug(ctx, "CLI CEL policy evaluation result",
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
				// Only count toward final decision if mode is enforce (not audit_only)
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
				} else {
					auditOnlyDeny = true
				}
			} else if policy.Action == config.PolicyActionAllow {
				// Only count toward final decision if mode is enforce (not audit_only)
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
		results.RecommendedAction = config.PolicyActionDeny
	} else if auditOnlyDeny {
		results.Allowed = true
		results.AuditModeBypass = true
		results.RecommendedAction = config.PolicyActionDeny
		results.Message = "CEL policy would deny but mode is audit_only"
	} else if results.FailedOpen {
		results.Allowed = true
		results.Message = "CEL evaluation failed, allowing request (fail-open)"
	} else if results.AllowCount > 0 {
		results.Allowed = true
		results.RecommendedAction = config.PolicyActionAllow
		// Find the first allow message
		for _, r := range results.Results {
			if r.Action == config.PolicyActionAllow && !r.Mode.IsAuditOnly() && r.Message != "" {
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

	e.logger.Debug(ctx, "CLI CEL policy evaluation complete",
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

// BuildCLIContext creates the CEL evaluation context for a CLI command.
// This is used when evaluating cli_expression rules. The returned map contains
// the "cli" variable with command, arguments, working_directory, and client_info fields.
func BuildCLIContext(req *CLIValidationRequest) map[string]interface{} {
	// Build client_info map with empty string defaults for nil ClientInfo
	clientInfo := map[string]interface{}{
		"hostname":    "",
		"username":    "",
		"os":          "",
		"arch":        "",
		"shell":       "",
		"cli_version": "",
	}
	if req.ClientInfo != nil {
		clientInfo = map[string]interface{}{
			"hostname":    req.ClientInfo.Hostname,
			"username":    req.ClientInfo.Username,
			"os":          req.ClientInfo.OS,
			"arch":        req.ClientInfo.Arch,
			"shell":       req.ClientInfo.Shell,
			"cli_version": req.ClientInfo.CLIVersion,
		}
	}

	return map[string]interface{}{
		"cli": map[string]interface{}{
			"command":           req.Command,
			"arguments":         req.Arguments,
			"working_directory": req.WorkingDirectory,
			"client_info":       clientInfo,
		},
	}
}

// extractCLIArgumentFlags extracts flag names from CLI arguments without exposing values.
// This is used for debug logging to avoid leaking sensitive data like tokens and passwords.
// Examples:
//   - ["--token", "secret123"] -> ["--token", "[value]"]
//   - ["--token=secret123"] -> ["--token"]
//   - ["-p", "password"] -> ["-p", "[value]"]
//   - ["subcommand", "--flag"] -> ["subcommand", "--flag"]
func extractCLIArgumentFlags(args []string) []string {
	if len(args) == 0 {
		return []string{}
	}

	result := make([]string, 0, len(args))
	skipNext := false

	for i, arg := range args {
		if skipNext {
			result = append(result, "[value]")
			skipNext = false
			continue
		}

		if len(arg) > 0 && arg[0] == '-' {
			// This is a flag
			if idx := indexOf(arg, '='); idx > 0 {
				// Format: --flag=value -> just keep --flag
				result = append(result, arg[:idx])
			} else {
				// Format: --flag or -f (value might be next arg)
				result = append(result, arg)
				// Check if next arg looks like a value (not a flag)
				if i+1 < len(args) && len(args[i+1]) > 0 && args[i+1][0] != '-' {
					skipNext = true
				}
			}
		} else {
			// Positional argument - could be a subcommand or a value
			result = append(result, arg)
		}
	}
	return result
}

// indexOf returns the index of the first occurrence of char in s, or -1 if not found.
func indexOf(s string, char byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == char {
			return i
		}
	}
	return -1
}

// extractMCPArgumentKeys extracts parameter keys from MCP tool arguments without exposing values.
// This is used for debug logging to avoid leaking sensitive data.
func extractMCPArgumentKeys(args any) []string {
	if args == nil {
		return []string{}
	}

	// map[string]any and map[string]interface{} are the same type
	if v, ok := args.(map[string]any); ok {
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		return keys
	}
	return []string{}
}
