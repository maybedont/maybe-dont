package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
)

// OpenAI API path appended to base URL for the openai provider.
const openAIChatCompletionsPath = "/chat/completions"

// openAIProvider implements AIProviderClient using the OpenAI REST API.
// This adapter is used for the "openai" provider (with default or custom base URL).
type openAIProvider struct {
	endpoint    string // Full endpoint URL (base + path for openai)
	apiKey      string
	model       string
	parameters  map[string]any
	headers     map[string]string
	queryParams map[string]string
	httpClient  *http.Client
	info        AIProviderInfo
}

// newOpenAIProvider creates a new OpenAI provider adapter.
// It uses the default OpenAI endpoint if not configured.
func newOpenAIProvider(cfg *config.Config) AIProviderClient {
	aiCfg := cfg.Validation.AI

	// Use default endpoint if not specified
	endpoint := aiCfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultOpenAIEndpoint
	}

	// Append chat completions path to base URL
	fullEndpoint := strings.TrimSuffix(endpoint, "/") + openAIChatCompletionsPath

	host, path := parseEndpointURL(fullEndpoint)

	return &openAIProvider{
		endpoint:    fullEndpoint,
		apiKey:      aiCfg.APIKey,
		model:       aiCfg.Model,
		parameters:  aiCfg.Parameters,
		headers:     aiCfg.Headers,
		queryParams: aiCfg.QueryParams,
		httpClient:  &http.Client{},
		info: AIProviderInfo{
			Provider:     ProviderOpenAI,
			Model:        aiCfg.Model,
			EndpointHost: host,
			EndpointPath: path,
		},
	}
}

// Generate implements AIProviderClient by calling the OpenAI chat completions API.
func (p *openAIProvider) Generate(ctx context.Context, req AIRequest) (AICompletionResult, error) {
	return retryOperation(ctx, func() (AICompletionResult, int, error) {
		return p.doGenerate(ctx, req)
	})
}

// doGenerate performs a single API call without retry logic.
func (p *openAIProvider) doGenerate(ctx context.Context, req AIRequest) (AICompletionResult, int, error) {
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

	// Create HTTP request
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

	// Add query params if configured
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

	// Parse rate limit headers (available even on error responses)
	rateLimitInfo := p.parseRateLimitHeaders(resp)

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		result := AICompletionResult{RateLimitInfo: rateLimitInfo}
		return result, resp.StatusCode, p.normalizeError(fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody)), resp.StatusCode)
	}

	// Parse response body
	result, statusCode, err := p.parseResponse(respBody)
	if err != nil {
		return result, statusCode, err
	}

	// Add rate limit info to successful result
	result.RateLimitInfo = rateLimitInfo
	return result, statusCode, nil
}

// buildRequestBody constructs the OpenAI chat completions request body.
func (p *openAIProvider) buildRequestBody(req AIRequest) ([]byte, error) {
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

	// OpenAI deprecated max_tokens in favor of max_completion_tokens for newer
	// models. Translate so callers can use the canonical max_tokens name.
	if v, ok := body["max_tokens"]; ok {
		body["max_completion_tokens"] = v
		delete(body, "max_tokens")
	}

	return json.Marshal(body)
}

// parseResponse parses the OpenAI chat completions response.
func (p *openAIProvider) parseResponse(respBody []byte) (AICompletionResult, int, error) {
	var resp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryParseError,
			Message:   "failed to parse OpenAI response",
			Retryable: false,
			Cause:     err,
		}
	}

	if len(resp.Choices) == 0 {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryNoResponse,
			Message:   "OpenAI returned no choices",
			Retryable: false,
		}
	}

	choice := resp.Choices[0]
	content := choice.Message.Content

	if content == "" {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryNoResponse,
			Message:   "OpenAI returned empty response content",
			Retryable: false,
		}
	}

	return AICompletionResult{
		RawText:           content,
		ParsedJSON:        json.RawMessage(content),
		ProviderRequestID: resp.ID,
		StopReason:        choice.FinishReason,
		WasTruncated:      choice.FinishReason == "length",
	}, http.StatusOK, nil
}

// parseRateLimitHeaders extracts rate limit information from OpenAI response headers.
func (p *openAIProvider) parseRateLimitHeaders(resp *http.Response) *RateLimitInfo {
	info := &RateLimitInfo{Provider: ProviderOpenAI}

	// Parse request limits
	if v := resp.Header.Get("x-ratelimit-limit-requests"); v != "" {
		info.RequestsLimit, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("x-ratelimit-remaining-requests"); v != "" {
		info.RequestsRemaining, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("x-ratelimit-reset-requests"); v != "" {
		info.RequestsReset = parseOpenAIResetTime(v)
	}

	// Parse token limits
	if v := resp.Header.Get("x-ratelimit-limit-tokens"); v != "" {
		info.TokensLimit, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("x-ratelimit-remaining-tokens"); v != "" {
		info.TokensRemaining, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("x-ratelimit-reset-tokens"); v != "" {
		info.TokensReset = parseOpenAIResetTime(v)
	}

	// Parse retry-after header (present on 429 responses)
	if v := resp.Header.Get("retry-after"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			info.RetryAfter = time.Duration(secs) * time.Second
		}
	}

	return info
}

// parseOpenAIResetTime parses OpenAI's reset time format (e.g., "1m30s", "500ms", "2s").
func parseOpenAIResetTime(v string) time.Time {
	// OpenAI uses duration format like "1m30s", "500ms", "2s"
	d, err := time.ParseDuration(v)
	if err != nil {
		return time.Time{}
	}
	return time.Now().Add(d)
}

// normalizeError converts HTTP errors to AIProviderError with appropriate categories.
func (p *openAIProvider) normalizeError(err error, statusCode int) *AIProviderError {
	// Check for context errors first
	if errors.Is(err, context.DeadlineExceeded) {
		return &AIProviderError{
			Category:  ErrCategoryTimeout,
			Message:   "request to OpenAI timed out",
			Retryable: false,
			Cause:     err,
		}
	}
	if errors.Is(err, context.Canceled) {
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
func (p *openAIProvider) ProviderInfo() AIProviderInfo {
	return p.info
}
