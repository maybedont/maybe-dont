package proxy

import (
	"context"
	"fmt"
	"strconv"
	"sync"

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
	logger        *zap.Logger
	policies      []AIPolicy
	mu            sync.RWMutex
	endpoint      string
	model         string
	timeout       int
	maxTokens     int
	apiKey        string
	defaultPolicy string // allow or deny
	client        *openai.Client
}

// NewAIPolicyEngine creates a new AI policy engine
func InitAIPolicyEngine(logger *zap.Logger, engine *AIPolicyEngine) error {
	// Validate default policy
	if engine.defaultPolicy != "allow" && engine.defaultPolicy != "deny" {
		return fmt.Errorf("invalid default policy: %s", engine.defaultPolicy)
	}

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

// Evaluate evaluates a tool call request against all policies
func (e *AIPolicyEngine) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest) (bool, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	e.logger.Debug("policies", zap.Any("policies", e.policies))
	// Evaluate each policy in order
	for _, policy := range e.policies {
		// Call the AI API
		chatCompletion, err := e.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("Say this is a test"),
			},
			Model: openai.ChatModelGPT4o,
		})
		if err != nil {
			return false, "", fmt.Errorf("failed to evaluate tool call: %w", err)
		}

		// Check result
		result, err := strconv.ParseBool(chatCompletion.Choices[0].Message.Content)
		if err != nil {
			return false, "", fmt.Errorf("failed to parse result: %w", err)
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

	// If no policies matched, use the default policy
	if e.defaultPolicy == "allow" {
		e.logger.Info("allowing tool call by default policy", zap.Any("request", req))
		return true, "", nil
	}
	e.logger.Info("denying tool call by default policy", zap.Any("request", req))
	return false, "no matching policy found", nil
}
