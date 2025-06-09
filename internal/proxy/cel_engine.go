package proxy

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
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
	logger        *zap.Logger
	env           *cel.Env
	policies      []CELPolicy
	mu            sync.RWMutex
	defaultPolicy string // allow or deny
}

// NewCELPolicyEngine creates a new CEL policy engine
func NewCELPolicyEngine(logger *zap.Logger) (*CELPolicyEngine, error) {
	// Create CEL environment with custom functions
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		cel.Variable("auth", cel.DynType),
		cel.Variable("response", cel.DynType),
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
func (e *CELPolicyEngine) EvaluateToolCall(req mcp.CallToolRequest) (ValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Create evaluation context with proper structure
	vars := map[string]interface{}{
		"request": map[string]interface{}{
			"method": req.Request.Method,
			"params": map[string]interface{}{
				"name":      req.Params.Name,
				"arguments": req.Params.Arguments,
				"meta":      req.Request.Params.Meta,
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

		e.logger.Debug("evaluating tool call", zap.Any("vars", vars), zap.Any("policy", policy))

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

	// Set top-level fields
	if denyMatched {
		results.Allowed = false
		results.Message = denyMsg
	} else if allowMatched {
		results.Allowed = true
		results.Message = allowMsg
	} else {
		results.Allowed = false
		results.Message = "Denied by default policy"
	}

	return results, nil
}
