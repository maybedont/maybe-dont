package gateway

import (
	"context"
	"fmt"
	"sync"

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
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Expression  string `yaml:"expression"`
	Action      string `yaml:"action"` // allow or deny
	Message     string `yaml:"message"`
}

// CELPolicyEngine handles CEL policy evaluation
type CELPolicyEngine struct {
	logger   *zap.Logger
	env      *cel.Env
	policies []CELPolicy
	mu       sync.RWMutex
}

// NewCELPolicyEngine creates a new CEL policy engine
func NewCELPolicyEngine(logger *zap.Logger) (*CELPolicyEngine, error) {
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
func (e *CELPolicyEngine) LoadPolicies(policies []config.CELPolicy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info("Loading CEL policies", zap.Int("count", len(policies)))

	// Validate and compile each policy
	for _, policy := range policies {
		e.logger.Info("Loading CEL policy",
			zap.String("name", policy.Name),
			zap.String("action", policy.Action),
		)

		// Compile the expression
		_, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("failed to compile policy %s: %w", policy.Name, issues.Err())
		}

		// Validate action
		if policy.Action != "allow" && policy.Action != "deny" {
			return fmt.Errorf("invalid action %s for policy %s", policy.Action, policy.Name)
		}

		// Store the compiled policy
		e.policies = append(e.policies, CELPolicy{
			Name:        policy.Name,
			Description: policy.Description,
			Expression:  policy.Expression,
			Action:      policy.Action,
			Message:     policy.Message,
		})
	}

	e.logger.Info("Loaded CEL policies", zap.Int("count", len(e.policies)))
	return nil
}

// Evaluate evaluates a tool call request against all policies
func (e *CELPolicyEngine) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Extract sessionID from context
	sessionID, _ := GetSessionID(ctx)

	e.logger.Info("Evaluating tool call with CEL policies",
		zap.String("session_id", sessionID),
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
		e.logger.Debug("Evaluating CEL policy",
			zap.String("session_id", sessionID),
			zap.String("name", policy.Name),
			zap.String("action", policy.Action),
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

		// Check result
		result, ok := out.Value().(bool)
		if !ok {
			return ValidationResults{}, fmt.Errorf("policy %s did not return a boolean", policy.Name)
		}

		e.logger.Debug("CEL policy evaluation result",
			zap.String("session_id", sessionID),
			zap.String("name", policy.Name),
			zap.Bool("result", result),
			zap.String("action", policy.Action),
		)

		if result && policy.Action == "deny" {
			results.Results = append(results.Results, ValidationResult{
				PolicyName: policy.Name,
				PolicyType: "cel",
				Allowed:    false,
				Message:    policy.Message,
			})
			results.DenyCount++
			if !denyMatched {
				denyMatched = true
				denyMsg = policy.Message
			}
		}
		if result && policy.Action == "allow" {
			results.Results = append(results.Results, ValidationResult{
				PolicyName: policy.Name,
				PolicyType: "cel",
				Allowed:    true,
				Message:    policy.Message,
			})
			results.AllowCount++
			if !allowMatched {
				allowMatched = true
				allowMsg = policy.Message
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
			Allowed:    true,
			Message:    "No policies matched",
		})
	}

	e.logger.Info("CEL policy evaluation complete",
		zap.String("session_id", sessionID),
		zap.Any("results", results),
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
	)

	return results, nil
}
