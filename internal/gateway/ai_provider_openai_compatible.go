package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/maybedont/maybe-dont/internal/config"
)

// openAICompatibleProvider implements AIProviderClient using direct REST calls
// to any OpenAI-compatible endpoint. Unlike the openai provider, this uses the
// full URL from config without appending any path.
//
// Use cases include: Google Gemini, LiteLLM, Azure OpenAI, vLLM, Ollama, OpenRouter.
type openAICompatibleProvider struct {
	endpoint    string            // Full endpoint URL from config
	apiKey      string
	model       string
	parameters  map[string]any
	headers     map[string]string
	queryParams map[string]string
	httpClient  *http.Client
	info        AIProviderInfo
}

// newOpenAICompatibleProvider creates a new OpenAI-compatible provider adapter.
// The endpoint must be the full URL to the chat completions API.
func newOpenAICompatibleProvider(cfg *config.Config) AIProviderClient {
	aiCfg := cfg.Validation.AI

	// Endpoint is required for openai_compatible (validated in config)
	endpoint := aiCfg.Endpoint

	host, path := parseEndpointURL(endpoint)

	return &openAICompatibleProvider{
		endpoint:    endpoint,
		apiKey:      aiCfg.APIKey,
		model:       aiCfg.Model,
		parameters:  aiCfg.Parameters,
		headers:     aiCfg.Headers,
		queryParams: aiCfg.QueryParams,
		httpClient:  &http.Client{},
		info: AIProviderInfo{
			Provider:     ProviderOpenAICompatible,
			Model:        aiCfg.Model,
			EndpointHost: host,
			EndpointPath: path,
		},
	}
}

// Generate implements AIProviderClient by calling the OpenAI-compatible chat completions API.
// NOTE: This adapter includes retry logic for parity with SDK-backed providers.
// If specific openai_compatible endpoints require different retry behavior,
// this can be made configurable in a future enhancement.
func (p *openAICompatibleProvider) Generate(ctx context.Context, req AIRequest) (AICompletionResult, error) {
	return retryOperation(ctx, func() (AICompletionResult, int, error) {
		return p.doGenerate(ctx, req)
	})
}

// doGenerate performs a single API call without retry logic.
func (p *openAICompatibleProvider) doGenerate(ctx context.Context, req AIRequest) (AICompletionResult, int, error) {
	// Build request body
	body, err := p.buildRequestBody(req)
	if err != nil {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryInvalidRequest,
			Message:   "failed to build request body",
			Retryable: false,
			Cause:     err,
		}
	}

	// Create HTTP request using full endpoint URL
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryInvalidRequest,
			Message:   "failed to create HTTP request",
			Retryable: false,
			Cause:     err,
		}
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Add custom headers from config
	for k, v := range p.headers {
		httpReq.Header.Set(k, v)
	}

	// Add query params if configured (e.g., api-version for Azure)
	if len(p.queryParams) > 0 {
		q := httpReq.URL.Query()
		for k, v := range p.queryParams {
			q.Set(k, v)
		}
		httpReq.URL.RawQuery = q.Encode()
	}

	// Execute request
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return AICompletionResult{}, 0, p.normalizeError(err, 0)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return AICompletionResult{}, resp.StatusCode, &AIProviderError{
			Category:  ErrCategoryAPIError,
			Message:   "failed to read response body",
			Retryable: false,
			Cause:     err,
		}
	}

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		return AICompletionResult{}, resp.StatusCode, p.normalizeError(fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody)), resp.StatusCode)
	}

	// Parse response
	return p.parseResponse(respBody)
}

// buildRequestBody constructs the OpenAI-format chat completions request body.
func (p *openAICompatibleProvider) buildRequestBody(req AIRequest) ([]byte, error) {
	// Build messages array
	messages := make([]map[string]any, 0, 2)

	if req.SystemPrompt != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}

	messages = append(messages, map[string]any{
		"role":    "user",
		"content": req.UserPrompt,
	})

	// Build request body
	body := map[string]any{
		"model":    p.model,
		"messages": messages,
	}

	// Add response_format for structured output if schema provided
	// Note: Not all OpenAI-compatible providers support this feature.
	// For providers without JSON schema support, consider using the
	// anthropic provider or embedding schema instructions in the prompt.
	if req.ResponseSchema != nil {
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "response",
				"schema": req.ResponseSchema,
				"strict": true,
			},
		}
	}

	// Add parameters from config
	for k, v := range p.parameters {
		body[k] = v
	}

	// Override with request-level parameters if provided
	for k, v := range req.Parameters {
		body[k] = v
	}

	return json.Marshal(body)
}

// parseResponse parses the OpenAI-format chat completions response.
func (p *openAICompatibleProvider) parseResponse(respBody []byte) (AICompletionResult, int, error) {
	var resp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryParseError,
			Message:   "failed to parse response",
			Retryable: false,
			Cause:     err,
		}
	}

	if len(resp.Choices) == 0 {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryNoResponse,
			Message:   "provider returned no choices",
			Retryable: false,
		}
	}

	content := resp.Choices[0].Message.Content

	return AICompletionResult{
		RawText:           content,
		ParsedJSON:        json.RawMessage(content),
		ProviderRequestID: resp.ID,
	}, http.StatusOK, nil
}

// normalizeError converts HTTP errors to AIProviderError with appropriate categories.
func (p *openAICompatibleProvider) normalizeError(err error, statusCode int) *AIProviderError {
	// Check for context errors first
	if err == context.DeadlineExceeded {
		return &AIProviderError{
			Category:  ErrCategoryTimeout,
			Message:   "request timed out",
			Retryable: false,
			Cause:     err,
		}
	}
	if err == context.Canceled {
		return &AIProviderError{
			Category:  ErrCategoryCanceled,
			Message:   "request was canceled",
			Retryable: false,
			Cause:     err,
		}
	}

	// Categorize by status code
	switch statusCode {
	case http.StatusTooManyRequests:
		return &AIProviderError{
			Category:  ErrCategoryRateLimited,
			Message:   "rate limited",
			Retryable: true,
			Cause:     err,
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &AIProviderError{
			Category:  ErrCategoryAuthError,
			Message:   "authentication failed",
			Retryable: false,
			Cause:     err,
		}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return &AIProviderError{
			Category:  ErrCategoryInvalidRequest,
			Message:   "invalid request",
			Retryable: false,
			Cause:     err,
		}
	}

	// 5xx errors are retryable
	if statusCode >= 500 && statusCode < 600 {
		return &AIProviderError{
			Category:  ErrCategoryAPIError,
			Message:   fmt.Sprintf("server error: %v", err),
			Retryable: true,
			Cause:     err,
		}
	}

	// Network errors (no status code) are generally retryable
	if statusCode == 0 {
		return &AIProviderError{
			Category:  ErrCategoryAPIError,
			Message:   fmt.Sprintf("network error: %v", err),
			Retryable: true,
			Cause:     err,
		}
	}

	// Other errors
	return &AIProviderError{
		Category:  ErrCategoryAPIError,
		Message:   fmt.Sprintf("API error: %v", err),
		Retryable: false,
		Cause:     err,
	}
}

// ProviderInfo implements AIProviderClient.
func (p *openAICompatibleProvider) ProviderInfo() AIProviderInfo {
	return p.info
}
