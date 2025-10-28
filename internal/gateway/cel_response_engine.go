package gateway

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// CELResponsePolicy represents a single CEL response policy rule
type CELResponsePolicy struct {
	Name                 string `yaml:"name"`
	Description          string `yaml:"description"`
	Expression           string `yaml:"expression"`
	Action               string `yaml:"action"` // allow, deny, or redact
	Message              string `yaml:"message"`
	RedactionPattern     string `yaml:"redaction_pattern"`
	RedactionReplacement string `yaml:"redaction_replacement"`
}

// CELResponsePolicyEngine handles CEL policy evaluation for responses
type CELResponsePolicyEngine struct {
	logger    *zap.Logger
	ctxLogger *ContextLogger
	env       *cel.Env
	policies  []CELResponsePolicy
	mu        sync.RWMutex
}

// NewCELResponsePolicyEngine creates a new CEL response policy engine
func NewCELResponsePolicyEngine(logger *zap.Logger) (*CELResponsePolicyEngine, error) {
	// Create CEL environment with custom functions
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		cel.Variable("response", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELResponsePolicyEngine{
		logger:    logger,
		ctxLogger: NewContextLogger(logger),
		env:       env,
		policies:  make([]CELResponsePolicy, 0),
	}, nil
}

// LoadPolicies loads CEL response policies from configuration
func (e *CELResponsePolicyEngine) LoadPolicies(policies []config.CELResponsePolicy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info("Loading CEL response policies", zap.Int("count", len(policies)))

	// Validate and compile each policy
	for _, policy := range policies {
		e.logger.Info("Loading CEL response policy",
			zap.String("name", policy.Name),
			zap.String("action", policy.Action),
		)

		// Compile the expression
		_, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("failed to compile response policy %s: %w", policy.Name, issues.Err())
		}

		// Validate action
		if policy.Action != "allow" && policy.Action != "deny" && policy.Action != "redact" {
			return fmt.Errorf("invalid action %s for response policy %s", policy.Action, policy.Name)
		}

		// Store the compiled policy
		e.policies = append(e.policies, CELResponsePolicy{
			Name:                 policy.Name,
			Description:          policy.Description,
			Expression:           policy.Expression,
			Action:               policy.Action,
			Message:              policy.Message,
			RedactionPattern:     policy.RedactionPattern,
			RedactionReplacement: policy.RedactionReplacement,
		})
	}

	e.logger.Info("Loaded CEL response policies", zap.Int("count", len(e.policies)))
	return nil
}

// EvaluateResponse evaluates a response against all policies
func (e *CELResponsePolicyEngine) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Extract sessionID from context
	sessionID, _ := GetSessionID(ctx)

	e.logger.Info("Evaluating response with CEL policies",
		zap.String("session_id", sessionID),
		zap.String("tool", req.Params.Name),
		zap.Int("policy_count", len(e.policies)),
	)

	// Extract response content for CEL evaluation
	responseData := e.extractResponseData(result)

	// Create evaluation context
	vars := map[string]interface{}{
		"request": map[string]interface{}{
			"method": req.Method,
			"params": map[string]interface{}{
				"name":      req.Params.Name,
				"arguments": req.Params.Arguments,
			},
		},
		"response": responseData,
	}

	results := ResponseValidationResults{
		Results: make([]ResponseValidationResult, 0),
		Allowed: true,
	}

	var redactedContent *string

	// Evaluate each policy in order
	for _, policy := range e.policies {
		e.ctxLogger.Debug(ctx, "Evaluating CEL response policy",
			zap.String("name", policy.Name),
			zap.String("action", policy.Action),
			zap.String("expression", policy.Expression),
		)

		// Compile the expression
		ast, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			return ResponseValidationResults{}, fmt.Errorf("failed to compile response policy %s: %w", policy.Name, issues.Err())
		}

		// Create program
		prg, err := e.env.Program(ast)
		if err != nil {
			return ResponseValidationResults{}, fmt.Errorf("failed to create program for response policy %s: %w", policy.Name, err)
		}

		// Evaluate the expression
		out, _, err := prg.Eval(vars)
		if err != nil {
			return ResponseValidationResults{}, fmt.Errorf("failed to evaluate response policy %s: %w", policy.Name, err)
		}

		// Check result
		matched, ok := out.Value().(bool)
		if !ok {
			return ResponseValidationResults{}, fmt.Errorf("response policy %s did not return a boolean", policy.Name)
		}

		e.ctxLogger.Debug(ctx, "CEL response policy evaluation result",
			zap.String("name", policy.Name),
			zap.Bool("matched", matched),
			zap.String("action", policy.Action),
		)

		if matched {
			switch policy.Action {
			case "deny":
				results.Results = append(results.Results, ResponseValidationResult{
					PolicyName: policy.Name,
					PolicyType: "cel",
					Allowed:    false,
					Message:    policy.Message,
				})
				results.DenyCount++
				results.Allowed = false
				if results.Message == "" {
					results.Message = policy.Message
				}

			case "redact":
				// Apply redaction
				content := e.getTextContent(result)
				if content != "" {
					redacted := e.applyRedaction(content, policy.RedactionPattern, policy.RedactionReplacement)
					redactedContent = &redacted

					results.Results = append(results.Results, ResponseValidationResult{
						PolicyName:      policy.Name,
						PolicyType:      "cel",
						Allowed:         true,
						Message:         policy.Message,
						RedactedContent: redacted,
					})
					results.RedactCount++
				}

			case "allow":
				results.Results = append(results.Results, ResponseValidationResult{
					PolicyName: policy.Name,
					PolicyType: "cel",
					Allowed:    true,
					Message:    policy.Message,
				})
				results.AllowCount++
			}
		}
	}

	// Set redacted content if any redaction occurred
	if redactedContent != nil {
		results.RedactedContent = redactedContent
	}

	// Set final result message if not already set
	if results.Message == "" {
		if results.DenyCount > 0 {
			results.Message = "Response denied by CEL policy"
		} else if results.RedactCount > 0 {
			results.Message = "Response content redacted by CEL policy"
		} else {
			results.Message = "No CEL response policies matched"
		}
	}

	e.logger.Info("CEL response policy evaluation complete",
		zap.String("session_id", sessionID),
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
		zap.Int("deny_count", results.DenyCount),
		zap.Int("redact_count", results.RedactCount),
	)

	return results, nil
}

// extractResponseData extracts relevant data from the response for CEL evaluation
func (e *CELResponsePolicyEngine) extractResponseData(result *mcp.CallToolResult) map[string]interface{} {
	data := map[string]interface{}{
		"isError": result.IsError,
	}

	// Extract content
	if len(result.Content) > 0 {
		contentItems := make([]map[string]interface{}, 0, len(result.Content))
		for _, item := range result.Content {
			switch v := item.(type) {
			case mcp.TextContent:
				contentItems = append(contentItems, map[string]interface{}{
					"type": "text",
					"text": v.Text,
				})
			case mcp.ImageContent:
				contentItems = append(contentItems, map[string]interface{}{
					"type":     "image",
					"data":     v.Data,
					"mimeType": v.MIMEType,
				})
			case mcp.EmbeddedResource:
				contentItems = append(contentItems, map[string]interface{}{
					"type":     "resource",
					"resource": v.Resource,
				})
			}
		}
		data["content"] = contentItems
	}

	// Extract meta
	if result.Meta != nil {
		data["meta"] = result.Meta.AdditionalFields
	}

	return data
}

// getTextContent extracts the first text content from the result
func (e *CELResponsePolicyEngine) getTextContent(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}

	for _, item := range result.Content {
		if textContent, ok := item.(mcp.TextContent); ok {
			return textContent.Text
		}
	}

	return ""
}

// applyRedaction applies redaction pattern to content
func (e *CELResponsePolicyEngine) applyRedaction(content, pattern, replacement string) string {
	if pattern == "" {
		// Default: redact entire content
		return "[REDACTED]"
	}

	if replacement == "" {
		replacement = "[REDACTED]"
	}

	// Compile and apply regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		e.logger.Error("Failed to compile redaction pattern",
			zap.String("pattern", pattern),
			zap.Error(err),
		)
		return content
	}

	return re.ReplaceAllString(content, replacement)
}

// ResponseCELValidationHandler handles CEL response validation
type ResponseCELValidationHandler struct {
	logger *zap.Logger
	engine *CELResponsePolicyEngine
}

// NewResponseCELValidationHandler creates a new CEL response validation handler
func NewResponseCELValidationHandler(logger *zap.Logger, engine *CELResponsePolicyEngine) *ResponseCELValidationHandler {
	return &ResponseCELValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleResponse implements ResponseValidationHandler
func (h *ResponseCELValidationHandler) HandleResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	return h.engine.EvaluateResponse(ctx, req, result)
}
