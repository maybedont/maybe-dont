package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/invopop/jsonschema"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// AIPolicy represents a single AI policy rule
type AIPolicy struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Prompt      string `yaml:"prompt"`
	Action      string `yaml:"action"` // allow or deny
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

		// Validate action
		if policy.Action != "allow" && policy.Action != "deny" {
			return fmt.Errorf("invalid action %s for policy %s", policy.Action, policy.Name)
		}

		// Store the compiled policy
		e.policies = append(e.policies, AIPolicy{
			Name:        policy.Name,
			Description: policy.Description,
			Prompt:      policy.Prompt,
			Action:      policy.Action,
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

	e.logger.Debug("policies", zap.Any("policies", e.policies))

	// Track all policy evaluations
	var results ValidationResults

	// Evaluate each policy in order
	for _, policy := range e.policies {
		// Format the tool call request for the AI
		toolCallStr := fmt.Sprintf("Tool: %s\nArguments: %v", req.Params.Name, req.Params.Arguments)

		schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
			Name:        "ai_response",
			Description: openai.String("Response from the AI policy engine"),
			Schema:      GenerateSchema[AIResponse](),
			Strict:      openai.Bool(true),
		}

		// Call the AI API with the actual tool call request
		chatCompletion, err := e.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage(fmt.Sprintf(policy.Prompt, toolCallStr)),
			},
			Model: openai.ChatModel(e.model),
			ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
					JSONSchema: schemaParam,
				},
			},
		})
		if err != nil {
			return ValidationResults{}, fmt.Errorf("failed to evaluate tool call: %w", err)
		}

		if len(chatCompletion.Choices) == 0 {
			return ValidationResults{}, fmt.Errorf("no response from AI model")
		}

		// Check result
		// Parse the response as JSON
		var result AIResponse
		err = json.Unmarshal([]byte(chatCompletion.Choices[0].Message.Content), &result)
		if err != nil {
			return ValidationResults{}, fmt.Errorf("failed to parse result: %w", err)
		}
		e.logger.Debug("result", zap.Any("result", result))

		// If policy matches and is a deny rule, deny the request
		if result.Allowed && policy.Action == "deny" {
			// Record policy evaluation
			results.Results = append(results.Results, ValidationResult{
				PolicyName: policy.Name,
				PolicyType: "ai",
				Allowed:    false,
				Message:    result.Message,
			})
			results.DenyCount++
		}

		// If policy matches and is an allow rule, allow the request
		if result.Allowed && policy.Action == "allow" {
			// Record policy evaluation
			results.Results = append(results.Results, ValidationResult{
				PolicyName: policy.Name,
				PolicyType: "ai",
				Allowed:    true,
				Message:    result.Message,
			})
			results.AllowCount++
		}
	}

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
