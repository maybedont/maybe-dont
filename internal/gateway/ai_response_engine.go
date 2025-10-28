package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.uber.org/zap"
)

// AIResponsePolicy represents a single AI response policy rule
type AIResponsePolicy struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Prompt      string `yaml:"prompt"`
	Action      string `yaml:"action"` // allow, deny, or redact
	Message     string `yaml:"message"`
}

// AIResponsePolicyEngine handles AI policy evaluation for responses
type AIResponsePolicyEngine struct {
	logger    *zap.Logger
	ctxLogger *ContextLogger
	policies  []AIResponsePolicy
	mu        sync.RWMutex
	endpoint  string
	model     string
	apiKey    string
	client    *openai.Client
}

// InitAIResponsePolicyEngine initializes the AI response policy engine
func InitAIResponsePolicyEngine(logger *zap.Logger, engine *AIResponsePolicyEngine) error {
	engine.logger = logger
	engine.ctxLogger = NewContextLogger(logger)
	client := openai.NewClient(
		option.WithAPIKey(engine.apiKey),
	)
	engine.client = &client
	return nil
}

// LoadPolicies loads AI response policies from configuration
func (e *AIResponsePolicyEngine) LoadPolicies(policies []config.AIResponsePolicy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info("Loading AI response policies", zap.Int("count", len(policies)))

	// Validate each policy
	for _, policy := range policies {
		// Validate action
		if policy.Action != "allow" && policy.Action != "deny" && policy.Action != "redact" {
			return fmt.Errorf("invalid action %s for AI response policy %s", policy.Action, policy.Name)
		}

		// Store the policy
		e.policies = append(e.policies, AIResponsePolicy{
			Name:        policy.Name,
			Description: policy.Description,
			Prompt:      policy.Prompt,
			Action:      policy.Action,
			Message:     policy.Message,
		})
	}

	e.logger.Info("Loaded AI response policies", zap.Int("count", len(e.policies)))
	return nil
}

// AIResponseEvaluation represents the AI's response evaluation
type AIResponseEvaluation struct {
	Allowed         bool   `json:"allowed"`
	Message         string `json:"message"`
	RedactedContent string `json:"redacted_content"`
}

// EvaluateResponse evaluates a response against all policies
func (e *AIResponsePolicyEngine) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	e.ctxLogger.Info(ctx, "Evaluating response with AI policies",
		zap.String("tool", req.Params.Name),
		zap.Int("policy_count", len(e.policies)),
	)

	// Track all policy evaluations
	var results ResponseValidationResults
	results.Results = make([]ResponseValidationResult, 0)
	results.Allowed = true

	// Create a channel to collect results from goroutines
	type policyResult struct {
		result ResponseValidationResult
		err    error
	}
	resultChan := make(chan policyResult, len(e.policies))

	// Format the response for the AI once
	responseStr := e.formatResponseForAI(result)

	// Launch a goroutine for each policy
	for _, policy := range e.policies {
		go func(p AIResponsePolicy) {
			// Create a new context for this goroutine
			policyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:        "ai_response_evaluation",
				Description: openai.String("AI evaluation of a tool response"),
				Schema:      GenerateSchema[AIResponseEvaluation](),
				Strict:      openai.Bool(true),
			}

			// Call the AI API with the actual response
			chatCompletion, err := e.client.Chat.Completions.New(policyCtx, openai.ChatCompletionNewParams{
				Messages: []openai.ChatCompletionMessageParamUnion{
					openai.UserMessage(fmt.Sprintf(p.Prompt, responseStr)),
				},
				Model: openai.ChatModel(e.model),
				ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
					OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
						JSONSchema: schemaParam,
					},
				},
			})
			if err != nil {
				resultChan <- policyResult{
					result: ResponseValidationResult{
						PolicyName: p.Name,
						PolicyType: "ai",
						Allowed:    false,
						Error:      fmt.Sprintf("Failed to evaluate response policy: %v", err),
					},
					err: err,
				}
				return
			}

			if len(chatCompletion.Choices) == 0 {
				resultChan <- policyResult{
					result: ResponseValidationResult{
						PolicyName: p.Name,
						PolicyType: "ai",
						Allowed:    false,
						Error:      "No response from AI model",
					},
					err: fmt.Errorf("no response from AI model"),
				}
				return
			}

			// Parse the response as JSON
			var evaluation AIResponseEvaluation
			err = json.Unmarshal([]byte(chatCompletion.Choices[0].Message.Content), &evaluation)
			if err != nil {
				resultChan <- policyResult{
					result: ResponseValidationResult{
						PolicyName: p.Name,
						PolicyType: "ai",
						Allowed:    false,
						Error:      fmt.Sprintf("Failed to parse AI response: %v", err),
					},
					err: err,
				}
				return
			}

			// Create validation result based on policy action and AI response
			validationResult := ResponseValidationResult{
				PolicyName: p.Name,
				PolicyType: "ai",
				Message:    evaluation.Message,
				Allowed:    evaluation.Allowed,
			}

			// Handle redaction
			if p.Action == "redact" && evaluation.RedactedContent != "" {
				validationResult.RedactedContent = evaluation.RedactedContent
			}

			resultChan <- policyResult{
				result: validationResult,
				err:    nil,
			}
		}(policy)
	}

	// Collect results from all goroutines
	var redactedContent *string
	for i := 0; i < len(e.policies); i++ {
		result := <-resultChan
		if result.err != nil {
			e.ctxLogger.Error(ctx, "Response policy evaluation failed",
				zap.String("policy", result.result.PolicyName),
				zap.Error(result.err),
			)
		}
		results.Results = append(results.Results, result.result)

		if result.result.Allowed {
			results.AllowCount++
		} else {
			results.DenyCount++
			results.Allowed = false
		}

		if result.result.RedactedContent != "" {
			results.RedactCount++
			content := result.result.RedactedContent
			redactedContent = &content
		}
	}

	// Set redacted content if any redaction occurred
	if redactedContent != nil {
		results.RedactedContent = redactedContent
	}

	// Set final result
	if results.DenyCount > 0 {
		results.Allowed = false
		results.Message = "Response denied by AI policy"
	} else if results.RedactCount > 0 {
		results.Message = "Response content redacted by AI policy"
	} else if results.AllowCount > 0 {
		results.Message = "All AI response policies passed"
	} else {
		results.Message = "No AI response policies matched"
	}

	e.ctxLogger.Info(ctx, "Response evaluation complete",
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
		zap.Int("deny_count", results.DenyCount),
		zap.Int("redact_count", results.RedactCount),
	)

	return results, nil
}

// formatResponseForAI formats the response for AI evaluation
func (e *AIResponsePolicyEngine) formatResponseForAI(result *mcp.CallToolResult) string {
	formatted := fmt.Sprintf("IsError: %v\n", result.IsError)

	if len(result.Content) > 0 {
		formatted += "Content:\n"
		for i, item := range result.Content {
			switch v := item.(type) {
			case mcp.TextContent:
				formatted += fmt.Sprintf("  [%d] Text: %s\n", i, v.Text)
			case mcp.ImageContent:
				formatted += fmt.Sprintf("  [%d] Image (MIME: %s, Data length: %d bytes)\n", i, v.MIMEType, len(v.Data))
			case mcp.EmbeddedResource:
				formatted += fmt.Sprintf("  [%d] Resource: %+v\n", i, v.Resource)
			}
		}
	}

	if result.Meta != nil && len(result.Meta.AdditionalFields) > 0 {
		formatted += fmt.Sprintf("Meta: %+v\n", result.Meta.AdditionalFields)
	}

	return formatted
}

// ResponseAIValidationHandler handles AI response validation
type ResponseAIValidationHandler struct {
	logger *zap.Logger
	engine *AIResponsePolicyEngine
}

// NewResponseAIValidationHandler creates a new AI response validation handler
func NewResponseAIValidationHandler(logger *zap.Logger, engine *AIResponsePolicyEngine) *ResponseAIValidationHandler {
	return &ResponseAIValidationHandler{
		logger: logger,
		engine: engine,
	}
}

// HandleResponse implements ResponseValidationHandler
func (h *ResponseAIValidationHandler) HandleResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	return h.engine.EvaluateResponse(ctx, req, result)
}
