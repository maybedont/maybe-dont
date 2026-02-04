package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// MockAIProviderClient is a test double for AIProviderClient.
// It supports configurable responses, delays, and error injection for testing.
type MockAIProviderClient struct {
	// Response is the default response to return from Generate.
	Response AICompletionResult

	// Error is the error to return from Generate (if set, takes precedence over Response).
	Error error

	// Delay is the simulated delay before returning a response.
	// The mock respects context cancellation during this delay.
	Delay time.Duration

	// Info is the provider info to return from ProviderInfo.
	Info AIProviderInfo

	// GenerateFunc allows custom logic for Generate calls.
	// If set, this function is called instead of using Response/Error.
	GenerateFunc func(ctx context.Context, req AIRequest) (AICompletionResult, error)

	// RecordedRequests stores all requests received by Generate for test assertions.
	RecordedRequests []AIRequest

	mu sync.Mutex
}

// NewMockAIProviderClient creates a new mock AI provider client with sensible defaults.
func NewMockAIProviderClient() *MockAIProviderClient {
	return &MockAIProviderClient{
		Response: AICompletionResult{
			RawText:           `{"allowed":true,"message":"Mock allowed"}`,
			ParsedJSON:        json.RawMessage(`{"allowed":true,"message":"Mock allowed"}`),
			ProviderRequestID: "mock-request-id",
		},
		Info: AIProviderInfo{
			Provider:     "mock",
			Model:        "mock-model",
			EndpointHost: "mock.example.com",
			EndpointPath: "/v1/mock",
		},
		RecordedRequests: make([]AIRequest, 0),
	}
}

// Generate implements AIProviderClient by returning configurable mock responses.
// It respects context cancellation and simulates configured delays.
func (m *MockAIProviderClient) Generate(ctx context.Context, req AIRequest) (AICompletionResult, error) {
	m.mu.Lock()
	m.RecordedRequests = append(m.RecordedRequests, req)

	// If custom function is set, use it
	if m.GenerateFunc != nil {
		fn := m.GenerateFunc
		m.mu.Unlock()
		return fn(ctx, req)
	}

	// Get configured response/error/delay
	response := m.Response
	err := m.Error
	delay := m.Delay
	m.mu.Unlock()

	// Simulate delay with context cancellation support
	if delay > 0 {
		select {
		case <-time.After(delay):
			// Delay completed
		case <-ctx.Done():
			// Context was canceled during delay
			return AICompletionResult{}, &AIProviderError{
				Category:  ErrCategoryCanceled,
				Message:   "request canceled during mock delay",
				Retryable: false,
				Cause:     ctx.Err(),
			}
		}
	}

	if err != nil {
		return AICompletionResult{}, err
	}

	return response, nil
}

// ProviderInfo implements AIProviderClient by returning the configured info.
func (m *MockAIProviderClient) ProviderInfo() AIProviderInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Info
}

// GetRecordedRequests returns a copy of all recorded requests for test assertions.
func (m *MockAIProviderClient) GetRecordedRequests() []AIRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]AIRequest, len(m.RecordedRequests))
	copy(result, m.RecordedRequests)
	return result
}

// Reset clears all recorded requests and resets to default state.
func (m *MockAIProviderClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedRequests = make([]AIRequest, 0)
	m.Error = nil
	m.GenerateFunc = nil
}

// SetResponse sets the response to return from subsequent Generate calls.
func (m *MockAIProviderClient) SetResponse(response AICompletionResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Response = response
	m.Error = nil
}

// SetError sets the error to return from subsequent Generate calls.
func (m *MockAIProviderClient) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Error = err
}

// SetDelay sets the delay for subsequent Generate calls.
func (m *MockAIProviderClient) SetDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Delay = delay
}

// SetProviderInfo sets the provider info to return from ProviderInfo.
func (m *MockAIProviderClient) SetProviderInfo(info AIProviderInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Info = info
}

// SetGenerateFunc sets a custom function for Generate calls.
// This allows complex test scenarios with request-dependent responses.
func (m *MockAIProviderClient) SetGenerateFunc(fn func(ctx context.Context, req AIRequest) (AICompletionResult, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GenerateFunc = fn
}
