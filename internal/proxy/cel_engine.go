package proxy

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sudermanjr/maybe-dont/internal/config"
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
	// Create CEL environment with custom functions
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		cel.Variable("auth", cel.DynType),
		cel.Variable("response", cel.DynType),
		cel.Function("hasSecrets",
			cel.Overload("hasSecrets_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					// TODO: Implement secret detection
					return types.Bool(false)
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

	// Validate and compile each policy
	for _, policy := range policies {
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

	return nil
}

// Evaluate evaluates a tool call request against all policies
func (e *CELPolicyEngine) Evaluate(req mcp.CallToolRequest) (bool, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Convert request params to a map that CEL can understand
	params := make(map[string]interface{})
	if req.Request.Params.Meta != nil {
		params["meta"] = map[string]interface{}{
			"additionalFields": req.Request.Params.Meta.AdditionalFields,
		}
	}

	// Create evaluation context
	vars := map[string]interface{}{
		"request": map[string]interface{}{
			"method": req.Request.Method,
			"params": params,
		},
	}

	// Evaluate each policy in order
	for _, policy := range e.policies {
		// Compile the expression
		ast, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			return false, "", fmt.Errorf("failed to compile policy %s: %w", policy.Name, issues.Err())
		}

		// Create program
		prg, err := e.env.Program(ast)
		if err != nil {
			return false, "", fmt.Errorf("failed to create program for policy %s: %w", policy.Name, err)
		}

		// Evaluate the expression
		out, _, err := prg.Eval(vars)
		if err != nil {
			return false, "", fmt.Errorf("failed to evaluate policy %s: %w", policy.Name, err)
		}

		// Check result
		result, ok := out.Value().(bool)
		if !ok {
			return false, "", fmt.Errorf("policy %s did not return a boolean", policy.Name)
		}

		// If policy matches and is a deny rule, deny the request
		if result && policy.Action == "deny" {
			return false, policy.Message, nil
		}

		// If policy matches and is an allow rule, allow the request
		if result && policy.Action == "allow" {
			return true, "", nil
		}
	}

	// If no policies matched, deny by default
	return false, "no matching policy found", nil
}
