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
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Prompt      string `yaml:"prompt"`
	Message     string `yaml:"message"`
}

// AIPolicyEngine handles AI policy evaluation
type AIPolicyEngine struct {
	logger   *zap.Logger
	policies []AIPolicy
	mu       sync.RWMutex
	endpoint string
	model    string
	apiKey   string
	client   *openai.Client
}

// NewAIPolicyEngine creates a new AI policy engine
func InitAIPolicyEngine(logger *zap.Logger, engine *AIPolicyEngine) error {
	// Set the logger
	engine.logger = logger

	client := openai.NewClient(
		option.WithAPIKey(engine.apiKey),
	)
	engine.client = &client
	return nil
}

// LoadPolicies loads AI policies from configuration
func (e *AIPolicyEngine) LoadPolicies(policies []config.AIPolicy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate each policy
	for _, policy := range policies {

		// Store the compiled policy
		e.policies = append(e.policies, AIPolicy{
			Name:        policy.Name,
			Description: policy.Description,
			Prompt:      policy.Prompt,
			Message:     policy.Message,
		})
	}

	return nil
}

type AIResponse struct {
	Allowed bool   `json:"allowed"`
	Message string `json:"message"`
}

// Evaluate evaluates a tool call request against all policies
func (e *AIPolicyEngine) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest) (ValidationResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Extract sessionID from context
	sessionID, _ := GetSessionID(ctx)

	// Track all policy evaluations
	var results ValidationResults
	results.Results = make([]ValidationResult, 0)

	// Create a channel to collect results from goroutines
	type policyResult struct {
		result ValidationResult
		err    error
	}
	resultChan := make(chan policyResult, len(e.policies))

	// Format the tool call request for the AI once
	toolCallStr := fmt.Sprintf("Tool: %s\nArguments: %v", req.Params.Name, req.Params.Arguments)

	// Launch a goroutine for each policy
	for _, policy := range e.policies {
		go func(p AIPolicy) {
			// Create a new context for this goroutine
			policyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:        "ai_response",
				Description: openai.String("Response from the AI policy engine"),
				Schema:      GenerateSchema[AIResponse](),
				Strict:      openai.Bool(true),
			}

			// Call the AI API with the actual tool call request
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
			if err != nil {
				resultChan <- policyResult{
					result: ValidationResult{
						PolicyName: p.Name,
						PolicyType: "ai",
						Allowed:    false,
						Error:      fmt.Sprintf("Failed to evaluate policy: %v", err),
					},
					err: err,
				}
				return
			}

			if len(chatCompletion.Choices) == 0 {
				resultChan <- policyResult{
					result: ValidationResult{
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
			var result AIResponse
			err = json.Unmarshal([]byte(chatCompletion.Choices[0].Message.Content), &result)
			if err != nil {
				resultChan <- policyResult{
					result: ValidationResult{
						PolicyName: p.Name,
						PolicyType: "ai",
						Allowed:    false,
						Error:      fmt.Sprintf("Failed to parse result: %v", err),
					},
					err: err,
				}
				return
			}

			// Create validation result based on policy action and AI response
			validationResult := ValidationResult{
				PolicyName: p.Name,
				PolicyType: "ai",
				Message:    result.Message,
				Allowed:    result.Allowed,
			}

			resultChan <- policyResult{
				result: validationResult,
				err:    nil,
			}
		}(policy)
	}

	// Collect results from all goroutines
	for i := 0; i < len(e.policies); i++ {
		result := <-resultChan
		if result.err != nil {
			e.logger.Error("Policy evaluation failed",
				zap.String("session_id", sessionID),
				zap.String("policy", result.result.PolicyName),
				zap.Error(result.err),
			)
		}
		results.Results = append(results.Results, result.result)
		if result.result.Allowed {
			results.AllowCount++
		} else {
			results.DenyCount++
		}
	}

	// Set final result
	if results.DenyCount > 0 {
		results.Allowed = false
		results.Message = "Maybe Don't, A policy failed."
	} else if results.AllowCount > 0 {
		results.Allowed = true
		results.Message = "All policies passed, maybe do."
	} else {
		results.Allowed = true // Default to allow if no policies matched
		results.Message = "No policies matched"
	}

	e.logger.Info("Tool call evaluation complete",
		zap.String("session_id", sessionID),
		zap.Any("results", results),
		zap.Bool("allowed", results.Allowed),
		zap.String("message", results.Message),
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
