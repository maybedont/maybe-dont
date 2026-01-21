package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// AIClient defines the interface for AI API calls used by policy engines.
// This abstraction allows for mocking in tests.
type AIClient interface {
	// CreateChatCompletion sends a chat completion request and returns the response.
	CreateChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)
}

// OpenAIClient wraps the OpenAI SDK client and implements AIClient.
type OpenAIClient struct {
	client *openai.Client
}

// NewOpenAIClient creates a new OpenAI client wrapper.
func NewOpenAIClient(apiKey string) *OpenAIClient {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &OpenAIClient{client: &client}
}

// CreateChatCompletion implements AIClient by calling the OpenAI API.
func (c *OpenAIClient) CreateChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return c.client.Chat.Completions.New(ctx, params)
}

// MockAIPolicy represents a mock policy configuration for testing.
// It tracks call timing and supports configurable responses and delays.
type MockAIPolicy struct {
	Name        string
	Response    AIResponse
	Delay       time.Duration
	Called      bool
	CalledAt    time.Time
	CompletedAt time.Time
	WasCanceled bool
	mu          sync.Mutex
}

// MarkCalled records that this policy was invoked.
func (m *MockAIPolicy) MarkCalled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called = true
	m.CalledAt = time.Now()
}

// MarkCompleted records that this policy finished evaluation.
func (m *MockAIPolicy) MarkCompleted() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CompletedAt = time.Now()
}

// MarkCanceled records that this policy was canceled.
func (m *MockAIPolicy) MarkCanceled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WasCanceled = true
}

// GetStats returns a copy of the mock policy stats for assertions.
func (m *MockAIPolicy) GetStats() (called bool, calledAt, completedAt time.Time, wasCanceled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Called, m.CalledAt, m.CompletedAt, m.WasCanceled
}

// MockAIClient is a test double for AIClient that returns configurable responses.
// It supports simulating delays and tracking calls for test assertions.
type MockAIClient struct {
	// Responses maps policy names to their mock configurations
	Responses map[string]*MockAIPolicy

	// DefaultResponse is used when a policy name is not found in Responses
	DefaultResponse AIResponse

	// DefaultDelay is applied when a policy doesn't have a specific delay
	DefaultDelay time.Duration

	// ErrorOnCall if set, returns this error instead of a response
	ErrorOnCall error

	mu sync.Mutex
}

// NewMockAIClient creates a new mock AI client with default allow response.
func NewMockAIClient() *MockAIClient {
	return &MockAIClient{
		Responses: make(map[string]*MockAIPolicy),
		DefaultResponse: AIResponse{
			Allowed: true,
			Message: "Mock allowed",
		},
	}
}

// AddPolicy adds a mock policy configuration for testing.
func (m *MockAIClient) AddPolicy(policy *MockAIPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses[policy.Name] = policy
}

// CreateChatCompletion implements AIClient by returning mock responses.
// It respects context cancellation and simulates configured delays.
func (m *MockAIClient) CreateChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	m.mu.Lock()
	if m.ErrorOnCall != nil {
		err := m.ErrorOnCall
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()

	// Extract policy name from the request (simple heuristic - look for tool name in message)
	var policyName string
	var mockPolicy *MockAIPolicy
	var response AIResponse
	var delay time.Duration

	m.mu.Lock()
	// Find matching mock policy (we'll match on any key for now)
	for name, policy := range m.Responses {
		policyName = name
		mockPolicy = policy
		response = policy.Response
		delay = policy.Delay
		break
	}
	if mockPolicy == nil {
		response = m.DefaultResponse
		delay = m.DefaultDelay
	}
	m.mu.Unlock()

	if mockPolicy != nil {
		mockPolicy.MarkCalled()
	}

	// Simulate delay with context cancellation support
	if delay > 0 {
		select {
		case <-time.After(delay):
			// Delay completed
		case <-ctx.Done():
			// Context was canceled during delay
			if mockPolicy != nil {
				mockPolicy.MarkCanceled()
			}
			return nil, ctx.Err()
		}
	}

	if mockPolicy != nil {
		mockPolicy.MarkCompleted()
	}

	// Build mock response
	content := `{"allowed":` + boolToString(response.Allowed) + `,"message":"` + response.Message + `"}`

	return &openai.ChatCompletion{
		ID:    "mock-completion-" + policyName,
		Model: "mock-model",
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
	}, nil
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
