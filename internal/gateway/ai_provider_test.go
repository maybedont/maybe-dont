package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAIProviderClient_FactorySelection verifies that the factory returns
// the correct provider adapter based on configuration.
func TestNewAIProviderClient_FactorySelection(t *testing.T) {
	tests := []struct {
		name             string
		provider         string
		endpoint         string
		expectedProvider string
	}{
		{
			name:             "empty provider defaults to openai",
			provider:         "",
			endpoint:         "",
			expectedProvider: ProviderOpenAI,
		},
		{
			name:             "explicit openai provider",
			provider:         "openai",
			endpoint:         "",
			expectedProvider: ProviderOpenAI,
		},
		{
			name:             "openai_compatible provider",
			provider:         "openai_compatible",
			endpoint:         "https://example.com/v1/chat/completions",
			expectedProvider: ProviderOpenAICompatible,
		},
		{
			name:             "anthropic provider",
			provider:         "anthropic",
			endpoint:         "",
			expectedProvider: ProviderAnthropic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Validation.AI.Provider = tt.provider
			cfg.Validation.AI.Endpoint = tt.endpoint
			cfg.Validation.AI.APIKey = "test-key"
			cfg.Validation.AI.Model = "test-model"

			client := NewAIProviderClient(cfg)
			info := client.ProviderInfo()

			assert.Equal(t, tt.expectedProvider, info.Provider)
		})
	}
}

// TestNewAIProviderClient_PanicsOnUnknownProvider verifies that the factory
// panics when given an unknown provider (indicating a config validation bug).
func TestNewAIProviderClient_PanicsOnUnknownProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "unknown_provider"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "test-model"

	assert.Panics(t, func() {
		NewAIProviderClient(cfg)
	})
}

// TestOpenAIProvider_DefaultEndpoint verifies that the OpenAI provider uses
// the default endpoint when none is configured.
func TestOpenAIProvider_DefaultEndpoint(t *testing.T) {
	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "gpt-4o-mini"

	client := NewAIProviderClient(cfg)
	info := client.ProviderInfo()

	assert.Equal(t, "api.openai.com", info.EndpointHost)
	assert.Equal(t, "/v1/chat/completions", info.EndpointPath)
}

// TestOpenAIProvider_CustomEndpoint verifies that the OpenAI provider uses
// the configured endpoint as-is without appending any path.
func TestOpenAIProvider_CustomEndpoint(t *testing.T) {
	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai"
	cfg.Validation.AI.Endpoint = "https://my-proxy.example.com/openai/v1/chat/completions"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "gpt-4o-mini"

	client := NewAIProviderClient(cfg)
	info := client.ProviderInfo()

	assert.Equal(t, "my-proxy.example.com", info.EndpointHost)
	assert.Equal(t, "/openai/v1/chat/completions", info.EndpointPath)
}

// TestOpenAICompatibleProvider_FullURL verifies that the OpenAI-compatible
// provider uses the full URL from config without appending any path.
func TestOpenAICompatibleProvider_FullURL(t *testing.T) {
	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai_compatible"
	cfg.Validation.AI.Endpoint = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "gemini-2.5-flash"

	client := NewAIProviderClient(cfg)
	info := client.ProviderInfo()

	assert.Equal(t, "generativelanguage.googleapis.com", info.EndpointHost)
	assert.Equal(t, "/v1beta/openai/chat/completions", info.EndpointPath)
}

// TestAnthropicProvider_DefaultEndpoint verifies that the Anthropic provider
// uses the default endpoint when none is configured.
func TestAnthropicProvider_DefaultEndpoint(t *testing.T) {
	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "anthropic"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "claude-sonnet-4-5-20250929"

	client := NewAIProviderClient(cfg)
	info := client.ProviderInfo()

	assert.Equal(t, "api.anthropic.com", info.EndpointHost)
	assert.Equal(t, "/v1/messages", info.EndpointPath)
}

func TestOpenAIProvider_Generate_Success(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

		// Read and verify request body
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var reqBody map[string]any
		err = json.Unmarshal(body, &reqBody)
		require.NoError(t, err)

		assert.Equal(t, "test-model", reqBody["model"])
		assert.NotEmpty(t, reqBody["messages"])

		// Return success response
		resp := map[string]any{
			"id": "chatcmpl-123",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": `{"allowed":true,"message":"test passed"}`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create provider with test server URL
	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-api-key"
	cfg.Validation.AI.Model = "test-model"

	client := NewAIProviderClient(cfg)

	// Make request
	result, err := client.Generate(context.Background(), AIRequest{
		UserPrompt: "test prompt",
	})

	require.NoError(t, err)
	assert.Equal(t, `{"allowed":true,"message":"test passed"}`, result.RawText)
	assert.Equal(t, "chatcmpl-123", result.ProviderRequestID)
}

// TestOpenAIProvider_Generate_WithResponseSchema verifies structured output support.
func TestOpenAIProvider_Generate_WithResponseSchema(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"id":      "chatcmpl-123",
			"choices": []map[string]any{{"message": map[string]any{"content": `{"result":"ok"}`}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "test-model"

	client := NewAIProviderClient(cfg)

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"result": map[string]any{"type": "string"},
		},
	}

	_, err := client.Generate(context.Background(), AIRequest{
		UserPrompt:     "test",
		ResponseSchema: schema,
	})

	require.NoError(t, err)

	// Verify response_format was set
	responseFormat, ok := receivedBody["response_format"].(map[string]any)
	require.True(t, ok, "response_format should be set")
	assert.Equal(t, "json_schema", responseFormat["type"])
}

// TestOpenAIProvider_Generate_MaxTokensTranslation verifies that max_tokens is
// translated to max_completion_tokens in the wire format for OpenAI.
func TestOpenAIProvider_Generate_MaxTokensTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"id":      "chatcmpl-123",
			"choices": []map[string]any{{"message": map[string]any{"content": `{}`}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "test-model"
	cfg.Validation.AI.Parameters = map[string]any{
		"max_tokens": 4096,
	}

	client := NewAIProviderClient(cfg)

	_, err := client.Generate(context.Background(), AIRequest{UserPrompt: "test"})

	require.NoError(t, err)
	// Should be translated to max_completion_tokens on the wire
	assert.Equal(t, float64(4096), receivedBody["max_completion_tokens"])
	// max_tokens should not be present
	assert.Nil(t, receivedBody["max_tokens"], "max_tokens should be translated to max_completion_tokens")
}

// TestOpenAIProvider_Generate_CustomHeaders verifies custom headers are sent.
func TestOpenAIProvider_Generate_CustomHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header

		resp := map[string]any{
			"id":      "chatcmpl-123",
			"choices": []map[string]any{{"message": map[string]any{"content": `{}`}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "test-model"
	cfg.Validation.AI.Headers = map[string]string{
		"X-Custom-Header": "custom-value",
	}

	client := NewAIProviderClient(cfg)

	_, err := client.Generate(context.Background(), AIRequest{UserPrompt: "test"})

	require.NoError(t, err)
	assert.Equal(t, "custom-value", receivedHeaders.Get("X-Custom-Header"))
}

// TestOpenAICompatibleProvider_QueryParams verifies query params are appended.
func TestOpenAICompatibleProvider_QueryParams(t *testing.T) {
	var receivedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery

		resp := map[string]any{
			"id":      "123",
			"choices": []map[string]any{{"message": map[string]any{"content": `{}`}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai_compatible"
	cfg.Validation.AI.Endpoint = server.URL + "/v1/chat/completions"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "test-model"
	cfg.Validation.AI.QueryParams = map[string]string{
		"api-version": "2024-02-15-preview",
	}

	client := NewAIProviderClient(cfg)

	_, err := client.Generate(context.Background(), AIRequest{UserPrompt: "test"})

	require.NoError(t, err)
	assert.Contains(t, receivedQuery, "api-version=2024-02-15-preview")
}

// TestAnthropicProvider_Generate_Success verifies successful Anthropic API calls.
func TestAnthropicProvider_Generate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Anthropic-specific headers
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		// Read and verify request body
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		_ = json.Unmarshal(body, &reqBody)

		assert.Equal(t, "claude-test", reqBody["model"])
		assert.NotNil(t, reqBody["max_tokens"]) // Required for Anthropic

		// Return Anthropic-format response
		resp := map[string]any{
			"id": "msg_123",
			"content": []map[string]any{
				{"type": "text", "text": `{"allowed":true,"message":"anthropic test"}`},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "anthropic"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-api-key"
	cfg.Validation.AI.Model = "claude-test"

	client := NewAIProviderClient(cfg)

	result, err := client.Generate(context.Background(), AIRequest{
		UserPrompt: "test prompt",
	})

	require.NoError(t, err)
	assert.Equal(t, `{"allowed":true,"message":"anthropic test"}`, result.RawText)
	assert.Equal(t, "msg_123", result.ProviderRequestID)
}

// TestAnthropicProvider_Generate_MaxTokensFromConfig verifies max_tokens is read from config.
func TestAnthropicProvider_Generate_MaxTokensFromConfig(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"id":      "msg_123",
			"content": []map[string]any{{"type": "text", "text": `{}`}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "anthropic"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "claude-test"
	cfg.Validation.AI.Parameters = map[string]any{
		"max_tokens": 8192,
	}

	client := NewAIProviderClient(cfg)

	_, err := client.Generate(context.Background(), AIRequest{UserPrompt: "test"})

	require.NoError(t, err)
	// JSON numbers unmarshal as float64
	assert.Equal(t, float64(8192), receivedBody["max_tokens"])
}

// TestAnthropicProvider_Generate_WithSystemPrompt verifies system prompt is sent correctly.
func TestAnthropicProvider_Generate_WithSystemPrompt(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"id":      "msg_123",
			"content": []map[string]any{{"type": "text", "text": `{}`}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "anthropic"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "claude-test"

	client := NewAIProviderClient(cfg)

	_, err := client.Generate(context.Background(), AIRequest{
		SystemPrompt: "You are a security validator.",
		UserPrompt:   "test",
	})

	require.NoError(t, err)
	assert.Equal(t, "You are a security validator.", receivedBody["system"])
}

// TestProviderError_ErrorInterface verifies AIProviderError implements error correctly.
func TestProviderError_ErrorInterface(t *testing.T) {
	err := &AIProviderError{
		Category:  ErrCategoryRateLimited,
		Message:   "too many requests",
		Retryable: true,
	}

	assert.Equal(t, "rate_limited: too many requests", err.Error())
}

// TestProviderError_Unwrap verifies error unwrapping works.
func TestProviderError_Unwrap(t *testing.T) {
	cause := context.DeadlineExceeded
	err := &AIProviderError{
		Category:  ErrCategoryTimeout,
		Message:   "request timed out",
		Retryable: false,
		Cause:     cause,
	}

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestMockAIProviderClient_RecordsRequests verifies the mock records requests.
func TestMockAIProviderClient_RecordsRequests(t *testing.T) {
	mock := NewMockAIProviderClient()

	_, _ = mock.Generate(context.Background(), AIRequest{UserPrompt: "prompt1"})
	_, _ = mock.Generate(context.Background(), AIRequest{UserPrompt: "prompt2"})

	requests := mock.GetRecordedRequests()
	require.Len(t, requests, 2)
	assert.Equal(t, "prompt1", requests[0].UserPrompt)
	assert.Equal(t, "prompt2", requests[1].UserPrompt)
}

// TestMockAIProviderClient_ErrorInjection verifies error injection works.
func TestMockAIProviderClient_ErrorInjection(t *testing.T) {
	mock := NewMockAIProviderClient()
	mock.SetError(&AIProviderError{
		Category:  ErrCategoryRateLimited,
		Message:   "test error",
		Retryable: true,
	})

	_, err := mock.Generate(context.Background(), AIRequest{UserPrompt: "test"})

	require.Error(t, err)
	var providerErr *AIProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrCategoryRateLimited, providerErr.Category)
}

// TestMockAIProviderClient_Delay verifies delay simulation works.
func TestMockAIProviderClient_Delay(t *testing.T) {
	mock := NewMockAIProviderClient()
	mock.SetDelay(50 * time.Millisecond)

	start := time.Now()
	_, err := mock.Generate(context.Background(), AIRequest{UserPrompt: "test"})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
}

// TestMockAIProviderClient_ContextCancellation verifies context cancellation during delay.
func TestMockAIProviderClient_ContextCancellation(t *testing.T) {
	mock := NewMockAIProviderClient()
	mock.SetDelay(1 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := mock.Generate(ctx, AIRequest{UserPrompt: "test"})

	require.Error(t, err)
	var providerErr *AIProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrCategoryCanceled, providerErr.Category)
}

// TestParseEndpointURL verifies URL parsing for audit logging.
func TestParseEndpointURL(t *testing.T) {
	tests := []struct {
		endpoint     string
		expectedHost string
		expectedPath string
	}{
		{
			endpoint:     "https://api.openai.com/v1/chat/completions",
			expectedHost: "api.openai.com",
			expectedPath: "/v1/chat/completions",
		},
		{
			endpoint:     "https://my-resource.openai.azure.com/openai/deployments/my-deployment/chat/completions?api-version=2024",
			expectedHost: "my-resource.openai.azure.com",
			expectedPath: "/openai/deployments/my-deployment/chat/completions",
		},
		{
			endpoint:     "http://localhost:8080/v1/chat/completions",
			expectedHost: "localhost:8080",
			expectedPath: "/v1/chat/completions",
		},
		{
			endpoint:     "",
			expectedHost: "",
			expectedPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			host, path := parseEndpointURL(tt.endpoint)
			assert.Equal(t, tt.expectedHost, host)
			assert.Equal(t, tt.expectedPath, path)
		})
	}
}

// TestOpenAIProvider_ErrorNormalization verifies error categories are set correctly.
func TestOpenAIProvider_ErrorNormalization(t *testing.T) {
	tests := []struct {
		name             string
		statusCode       int
		expectedCategory string
		expectedRetry    bool
	}{
		{
			name:             "rate limited",
			statusCode:       429,
			expectedCategory: ErrCategoryRateLimited,
			expectedRetry:    true,
		},
		{
			name:             "unauthorized",
			statusCode:       401,
			expectedCategory: ErrCategoryAuthError,
			expectedRetry:    false,
		},
		{
			name:             "forbidden",
			statusCode:       403,
			expectedCategory: ErrCategoryAuthError,
			expectedRetry:    false,
		},
		{
			name:             "bad request",
			statusCode:       400,
			expectedCategory: ErrCategoryInvalidRequest,
			expectedRetry:    false,
		},
		{
			name:             "server error",
			statusCode:       500,
			expectedCategory: ErrCategoryAPIError,
			expectedRetry:    true,
		},
		{
			name:             "service unavailable",
			statusCode:       503,
			expectedCategory: ErrCategoryAPIError,
			expectedRetry:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error":"test error"}`))
			}))
			defer server.Close()

			cfg := &config.Config{}
			cfg.Validation.AI.Provider = "openai"
			cfg.Validation.AI.Endpoint = server.URL
			cfg.Validation.AI.APIKey = "test-key"
			cfg.Validation.AI.Model = "test-model"

			client := NewAIProviderClient(cfg)

			_, err := client.Generate(context.Background(), AIRequest{UserPrompt: "test"})

			require.Error(t, err)
			var providerErr *AIProviderError
			require.ErrorAs(t, err, &providerErr)
			assert.Equal(t, tt.expectedCategory, providerErr.Category)
			assert.Equal(t, tt.expectedRetry, providerErr.Retryable)
		})
	}
}

// TestOpenAIProvider_EmptyContent verifies that an empty content string from the
// model is classified as a no_response error rather than returning a misleading
// empty result.
func TestOpenAIProvider_EmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "chatcmpl-empty",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "",
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "test-model"

	client := NewAIProviderClient(cfg)
	_, err := client.Generate(context.Background(), AIRequest{UserPrompt: "test"})

	require.Error(t, err)
	var providerErr *AIProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrCategoryNoResponse, providerErr.Category)
	assert.Contains(t, providerErr.Message, "empty response content")
}

// TestOpenAIProvider_TimeoutMessage verifies that timeout errors include the
// provider name for easier troubleshooting.
func TestOpenAIProvider_TimeoutMessage(t *testing.T) {
	// Server that never responds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Validation.AI.Provider = "openai"
	cfg.Validation.AI.Endpoint = server.URL
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Model = "test-model"

	client := NewAIProviderClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Generate(ctx, AIRequest{UserPrompt: "test"})

	require.Error(t, err)
	var providerErr *AIProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrCategoryTimeout, providerErr.Category)
	assert.Contains(t, providerErr.Message, "OpenAI")
}
