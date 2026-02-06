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

// Anthropic API path and version header.
const (
	anthropicMessagesPath = "/messages"
	anthropicVersion      = "2023-06-01"
)

// Default max_tokens for Anthropic (required parameter).
// Kept low because Anthropic counts max_tokens against rate limits (not actual output).
// Policy validation responses are small JSON (~100-200 tokens).
const anthropicDefaultMaxTokens = 256

// anthropicProvider implements AIProviderClient using the Anthropic REST API.
type anthropicProvider struct {
	endpoint    string            // Full endpoint URL (base + path)
	apiKey      string
	model       string
	parameters  map[string]any
	headers     map[string]string
	httpClient  *http.Client
	info        AIProviderInfo
}

// newAnthropicProvider creates a new Anthropic provider adapter.
// It uses the default Anthropic endpoint if not configured.
func newAnthropicProvider(cfg *config.Config) AIProviderClient {
	aiCfg := cfg.Validation.AI

	// Use default endpoint if not specified
	endpoint := aiCfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultAnthropicEndpoint
	}

	// Append messages path to base URL
	fullEndpoint := strings.TrimSuffix(endpoint, "/") + anthropicMessagesPath

	host, path := parseEndpointURL(fullEndpoint)

	return &anthropicProvider{
		endpoint:   fullEndpoint,
		apiKey:     aiCfg.APIKey,
		model:      aiCfg.Model,
		parameters: aiCfg.Parameters,
		headers:    aiCfg.Headers,
		httpClient: &http.Client{},
		info: AIProviderInfo{
			Provider:     ProviderAnthropic,
			Model:        aiCfg.Model,
			EndpointHost: host,
			EndpointPath: path,
		},
	}
}

// Generate implements AIProviderClient by calling the Anthropic messages API.
func (p *anthropicProvider) Generate(ctx context.Context, req AIRequest) (AICompletionResult, error) {
	return retryOperation(ctx, func() (AICompletionResult, int, error) {
		return p.doGenerate(ctx, req)
	})
}

// doGenerate performs a single API call without retry logic.
func (p *anthropicProvider) doGenerate(ctx context.Context, req AIRequest) (AICompletionResult, int, error) {
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

	// Set Anthropic-specific headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey) // Anthropic uses x-api-key, not Bearer
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	// Add custom headers from config (can override defaults if needed)
	for k, v := range p.headers {
		httpReq.Header.Set(k, v)
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
		// Include rate limit info in result even on error (useful for 429s)
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

// buildRequestBody constructs the Anthropic messages API request body.
func (p *anthropicProvider) buildRequestBody(req AIRequest) ([]byte, error) {
	// Build messages array (Anthropic uses different structure than OpenAI)
	messages := []map[string]any{
		{
			"role":    "user",
			"content": req.UserPrompt,
		},
	}

	// Build request body
	body := map[string]any{
		"model":    p.model,
		"messages": messages,
	}

	// Add system prompt if provided (Anthropic has a top-level "system" field)
	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	// max_tokens is required for Anthropic - use default if not in parameters
	maxTokens := anthropicDefaultMaxTokens
	if v, ok := p.parameters["max_tokens"]; ok {
		mt, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("invalid max_tokens in provider parameters: %w", err)
		}
		maxTokens = mt
	}
	if v, ok := req.Parameters["max_tokens"]; ok {
		mt, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("invalid max_tokens in request parameters: %w", err)
		}
		maxTokens = mt
	}
	body["max_tokens"] = maxTokens

	// Add response format for structured output if schema provided
	// Anthropic supports native JSON schema via output_config.format
	if req.ResponseSchema != nil {
		body["output_config"] = map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"schema": req.ResponseSchema,
			},
		}
	}

	// Add other parameters from config (excluding max_tokens which is handled above)
	for k, v := range p.parameters {
		if k != "max_tokens" {
			body[k] = v
		}
	}

	// Override with request-level parameters if provided
	for k, v := range req.Parameters {
		if k != "max_tokens" {
			body[k] = v
		}
	}

	return json.Marshal(body)
}

// parseResponse parses the Anthropic messages API response.
func (p *anthropicProvider) parseResponse(respBody []byte) (AICompletionResult, int, error) {
	var resp struct {
		ID         string `json:"id"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryParseError,
			Message:   "failed to parse Anthropic response",
			Retryable: false,
			Cause:     err,
		}
	}

	// Find the text content block
	var content string
	for _, block := range resp.Content {
		if block.Type == "text" {
			content = block.Text
			break
		}
	}

	if content == "" {
		return AICompletionResult{}, 0, &AIProviderError{
			Category:  ErrCategoryNoResponse,
			Message:   "Anthropic returned no text content",
			Retryable: false,
		}
	}

	return AICompletionResult{
		RawText:           content,
		ParsedJSON:        json.RawMessage(content),
		ProviderRequestID: resp.ID,
		StopReason:        resp.StopReason,
		WasTruncated:      resp.StopReason == "max_tokens",
	}, 200, nil
}

// parseRateLimitHeaders extracts rate limit information from Anthropic response headers.
func (p *anthropicProvider) parseRateLimitHeaders(resp *http.Response) *RateLimitInfo {
	info := &RateLimitInfo{Provider: ProviderAnthropic}

	// Parse request limits
	if v := resp.Header.Get("anthropic-ratelimit-requests-limit"); v != "" {
		info.RequestsLimit, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("anthropic-ratelimit-requests-remaining"); v != "" {
		info.RequestsRemaining, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("anthropic-ratelimit-requests-reset"); v != "" {
		info.RequestsReset, _ = time.Parse(time.RFC3339, v)
	}

	// Parse token limits
	if v := resp.Header.Get("anthropic-ratelimit-tokens-limit"); v != "" {
		info.TokensLimit, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("anthropic-ratelimit-tokens-remaining"); v != "" {
		info.TokensRemaining, _ = strconv.Atoi(v)
	}
	if v := resp.Header.Get("anthropic-ratelimit-tokens-reset"); v != "" {
		info.TokensReset, _ = time.Parse(time.RFC3339, v)
	}

	// Parse retry-after header (present on 429 responses)
	if v := resp.Header.Get("retry-after"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			info.RetryAfter = time.Duration(secs) * time.Second
		}
	}

	return info
}

// normalizeError converts HTTP errors to AIProviderError with appropriate categories.
func (p *anthropicProvider) normalizeError(err error, statusCode int) *AIProviderError {
	// Check for context errors first
	if errors.Is(err, context.DeadlineExceeded) {
		return &AIProviderError{
			Category:  ErrCategoryTimeout,
			Message:   "request timed out",
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
	case 529: // Anthropic-specific: overloaded
		return &AIProviderError{
			Category:  ErrCategoryRateLimited,
			Message:   "Anthropic API overloaded",
			Retryable: true,
			Cause:     err,
		}
	case 429: // Rate limited
		return &AIProviderError{
			Category:  ErrCategoryRateLimited,
			Message:   "rate limited",
			Retryable: true,
			Cause:     err,
		}
	case 401, 403:
		return &AIProviderError{
			Category:  ErrCategoryAuthError,
			Message:   "authentication failed",
			Retryable: false,
			Cause:     err,
		}
	case 400, 422:
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
func (p *anthropicProvider) ProviderInfo() AIProviderInfo {
	return p.info
}

// toInt converts a value to int, returning an error if conversion fails.
// Handles int, int64, and float64 (JSON numbers are parsed as float64).
func toInt(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}
