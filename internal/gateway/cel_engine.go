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

// CELPolicyEngine handles CEL policy evaluation
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

// LoadPolicies loads CEL policies from configuration
// defaultMode is the top-level mode that applies to all policies unless overridden per-rule
func (e *CELPolicyEngine) LoadPolicies(policies []config.CELPolicy, defaultMode config.PolicyMode) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info(context.Background(), "Loading CEL policies",
		zap.Int("count", len(policies)),
		zap.String("default_mode", string(defaultMode)),
	)

	// Validate and compile each policy
	for _, policy := range policies {
		// Resolve effective mode for this policy
		effectiveMode := config.ResolvePolicyMode(policy.Mode, defaultMode)

		e.logger.Info(context.Background(), "Loading CEL policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Skip disabled policies - don't even compile them
		if effectiveMode == config.PolicyModeDisabled {
			e.logger.Info(context.Background(), "Skipping disabled CEL policy",
				zap.String("name", policy.Name),
			)
			continue
		}

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

	e.logger.Info(context.Background(), "Loaded CEL policies", zap.Int("count", len(e.policies)))
	return nil
}

// Evaluate evaluates a tool call request against all policies
func (e *CELPolicyEngine) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	e.logger.Info(ctx, "Evaluating tool call with CEL policies",
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
	allowMatched := false
	denyMatched := false
	allowMsg := ""
	denyMsg := ""

	// Evaluate each policy in order
	for _, policy := range e.policies {
		// Track timing for this policy evaluation
		startTime := time.Now()

		e.logger.Debug(ctx, "Evaluating CEL policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("expression", policy.Expression),
		)

		// Compile the expression
		ast, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			return ValidationResults{}, fmt.Errorf("failed to compile policy %s: %w", policy.Name, issues.Err())
		}

		// Create program
		prg, err := e.env.Program(ast)
		if err != nil {
			return ValidationResults{}, fmt.Errorf("failed to create program for policy %s: %w", policy.Name, err)
		}

		// Evaluate the expression
		out, _, err := prg.Eval(vars)
		if err != nil {
			return ValidationResults{}, fmt.Errorf("failed to evaluate policy %s: %w", policy.Name, err)
		}

		// Calculate duration
		durationMs := time.Since(startTime).Milliseconds()

		// Check result
		result, ok := out.Value().(bool)
		if !ok {
			return ValidationResults{}, fmt.Errorf("policy %s did not return a boolean", policy.Name)
		}

		e.logger.Debug(ctx, "CEL policy evaluation result",
			zap.String("name", policy.Name),
			zap.Bool("result", result),
			zap.String("action", string(policy.Action)),
			zap.Int64("duration_ms", durationMs),
		)

		if result && policy.Action == config.PolicyActionDeny {
			results.Results = append(results.Results, ValidationResult{
				PolicyName: policy.Name,
				PolicyType: "cel",
				Action:     config.PolicyActionDeny,
				Mode:       policy.Mode,
				Message:    policy.Message,
				DurationMs: durationMs,
			})
			// Only count toward final decision if mode is enabled (not audit_only)
			if policy.Mode == config.PolicyModeEnabled {
				results.DenyCount++
				if !denyMatched {
					denyMatched = true
					denyMsg = policy.Message
				}
			}
		}
		if result && policy.Action == config.PolicyActionAllow {
			results.Results = append(results.Results, ValidationResult{
				PolicyName: policy.Name,
				PolicyType: "cel",
				Action:     config.PolicyActionAllow,
				Mode:       policy.Mode,
				Message:    policy.Message,
				DurationMs: durationMs,
			})
			// Only count toward final decision if mode is enabled (not audit_only)
			if policy.Mode == config.PolicyModeEnabled {
				results.AllowCount++
				if !allowMatched {
					allowMatched = true
					allowMsg = policy.Message
				}
			}
		}
	}

	// Set final result
	if denyMatched {
		results.Allowed = false
		results.Message = denyMsg
	} else if allowMatched {
		results.Allowed = true
		results.Message = allowMsg
	} else {
		results.Allowed = true // Default to allow if no policies matched
		results.Message = "No policies matched"
		results.Results = append(results.Results, ValidationResult{
			PolicyName: "CEL Policy Engine",
			PolicyType: "cel",
			Action:     config.PolicyActionAllow,
			Mode:       config.PolicyModeEnabled,
			Message:    "No policies matched",
		})
	}

	e.logger.Info(ctx, "CEL policy evaluation complete",
		zap.Any("results", results),
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
	)

	return results, nil
}
