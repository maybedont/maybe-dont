package gateway

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// CELResponsePolicy represents a single CEL response policy rule
type CELResponsePolicy struct {
	Name                 string              `yaml:"name"`
	Description          string              `yaml:"description"`
	Expression           string              `yaml:"expression"`
	Action               config.PolicyAction `yaml:"action"` // allow, deny, or redact
	Message              string              `yaml:"message"`
	Mode                 config.PolicyMode   `yaml:"mode"` // enabled, audit_only, or disabled
	RedactionPattern     string              `yaml:"redaction_pattern"`
	RedactionReplacement string              `yaml:"redaction_replacement"`
}

// CELResponsePolicyEngine handles CEL policy evaluation for responses
type CELResponsePolicyEngine struct {
	logger   *config.SessionLogger
	env      *cel.Env
	policies []CELResponsePolicy
	mu       sync.RWMutex
}

// NewCELResponsePolicyEngine creates a new CEL response policy engine
func NewCELResponsePolicyEngine(ctx context.Context, logger *config.SessionLogger) (*CELResponsePolicyEngine, error) {
	// Create CEL environment with custom functions
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		cel.Variable("response", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELResponsePolicyEngine{
		logger:   logger,
		env:      env,
		policies: make([]CELResponsePolicy, 0),
	}, nil
}

// LoadPolicies loads CEL response policies from configuration
// defaultMode is the top-level mode that applies to all policies unless overridden per-rule
func (e *CELResponsePolicyEngine) LoadPolicies(policies []config.CELResponsePolicy, defaultMode config.PolicyMode) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info(context.Background(), "Loading CEL response policies",
		zap.Int("count", len(policies)),
		zap.String("default_mode", string(defaultMode)),
	)

	// Validate and compile each policy
	for _, policy := range policies {
		// Resolve effective mode for this policy
		effectiveMode := config.ResolvePolicyMode(policy.Mode, defaultMode)

		e.logger.Info(context.Background(), "Loading CEL response policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
			zap.String("mode", string(effectiveMode)),
		)

		// Skip disabled policies
		if effectiveMode == config.PolicyModeDisabled {
			e.logger.Info(context.Background(), "Skipping disabled CEL response policy",
				zap.String("name", policy.Name),
			)
			continue
		}

		// Compile the expression
		_, issues := e.env.Compile(policy.Expression)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("failed to compile response policy %s: %w", policy.Name, issues.Err())
		}

		// Validate action, a response policy can be 'allow', 'deny' or 'redact'
		if policy.Action != config.PolicyActionAllow && policy.Action != config.PolicyActionDeny && policy.Action != config.PolicyActionRedact {
			return fmt.Errorf("invalid action '%s' for response policy %s: must be 'allow', 'deny', or 'redact'", policy.Action, policy.Name)
		}

		// Store the compiled policy with resolved mode
		e.policies = append(e.policies, CELResponsePolicy{
			Name:                 policy.Name,
			Description:          policy.Description,
			Expression:           policy.Expression,
			Action:               policy.Action,
			Message:              policy.Message,
			Mode:                 effectiveMode,
			RedactionPattern:     policy.RedactionPattern,
			RedactionReplacement: policy.RedactionReplacement,
		})
	}

	e.logger.Info(context.Background(), "Loaded CEL response policies", zap.Int("count", len(e.policies)))
	return nil
}

// EvaluateResponse evaluates a response against all policies
func (e *CELResponsePolicyEngine) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	e.logger.Info(ctx, "Evaluating response with CEL policies",
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
		// Track timing for this policy evaluation
		startTime := time.Now()

		e.logger.Debug(ctx, "Evaluating CEL response policy",
			zap.String("name", policy.Name),
			zap.String("action", string(policy.Action)),
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

		// Calculate duration
		durationMs := time.Since(startTime).Milliseconds()

		e.logger.Debug(ctx, "CEL response policy evaluation result",
			zap.String("name", policy.Name),
			zap.Bool("matched", matched),
			zap.String("action", string(policy.Action)),
			zap.Int64("duration_ms", durationMs),
		)

		if matched {
			switch policy.Action {
			case config.PolicyActionDeny:
				results.Results = append(results.Results, ResponseValidationResult{
					PolicyName: policy.Name,
					PolicyType: "cel",
					Action:     config.PolicyActionDeny,
					Mode:       policy.Mode,
					Message:    policy.Message,
					DurationMs: durationMs,
				})
				// Only affect final decision if mode is enabled (not audit_only)
				if policy.Mode == config.PolicyModeEnabled {
					results.Allowed = false
					if results.Message == "" {
						results.Message = policy.Message
					}
				}

			case config.PolicyActionRedact:
				// Apply redaction
				content := e.getTextContent(result)
				if content != "" {
					redacted := e.applyRedaction(content, policy.RedactionPattern, policy.RedactionReplacement)

					results.Results = append(results.Results, ResponseValidationResult{
						PolicyName:      policy.Name,
						PolicyType:      "cel",
						Action:          config.PolicyActionRedact,
						Mode:            policy.Mode,
						Message:         policy.Message,
						RedactedContent: redacted,
						DurationMs:      durationMs,
					})

					// Only actually apply redaction if mode is enabled (not audit_only)
					if policy.Mode == config.PolicyModeEnabled {
						redactedContent = &redacted
					}
				}

			case config.PolicyActionAllow:
				results.Results = append(results.Results, ResponseValidationResult{
					PolicyName: policy.Name,
					PolicyType: "cel",
					Action:     config.PolicyActionAllow,
					Mode:       policy.Mode,
					Message:    policy.Message,
					DurationMs: durationMs,
				})
			}
		}
	}

	// Set redacted content if any redaction occurred
	if redactedContent != nil {
		results.RedactedContent = redactedContent
	}

	// Set final result message if not already set
	if results.Message == "" {
		if results.DenyCount() > 0 {
			results.Message = "Response denied by CEL policy"
		} else if results.RedactCount() > 0 {
			results.Message = "Response content redacted by CEL policy"
		} else {
			results.Message = "No CEL response policies matched"
		}
	}

	e.logger.Info(ctx, "CEL response policy evaluation complete",
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
		zap.Int("deny_count", results.DenyCount()),
		zap.Int("redact_count", results.RedactCount()),
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
		e.logger.Error(context.Background(), "Failed to compile redaction pattern",
			zap.String("pattern", pattern),
			zap.Error(err),
		)
		return content
	}

	return re.ReplaceAllString(content, replacement)
}

// ResponseCELValidationHandler handles CEL response validation
type ResponseCELValidationHandler struct {
	logger *config.SessionLogger
	engine *CELResponsePolicyEngine
}

// NewResponseCELValidationHandler creates a new CEL response validation handler
func NewResponseCELValidationHandler(logger *config.SessionLogger, engine *CELResponsePolicyEngine) *ResponseCELValidationHandler {
	return &ResponseCELValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleResponse implements ResponseValidationHandler
func (h *ResponseCELValidationHandler) HandleResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	return h.engine.EvaluateResponse(ctx, req, result)
}
