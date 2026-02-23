// Package gateway provides the core gateway functionality including AI provider clients.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
)

// AIProviderClient is the provider-agnostic interface for AI API calls.
// This interface decouples validation engines from specific AI providers (OpenAI, Anthropic, etc.).
// Each provider adapter implements this interface to translate between the common AIRequest/AICompletionResult
// types and the provider's native API format.
type AIProviderClient interface {
	// Generate sends a completion request to the AI provider and returns the result.
	// The adapter handles all provider-specific details: authentication, request format,
	// response parsing, and structured output enforcement.
	Generate(ctx context.Context, req AIRequest) (AICompletionResult, error)

	// ProviderInfo returns metadata about the configured provider for audit logging.
	ProviderInfo() AIProviderInfo
}

// AIRequest is a vendor-neutral structure for AI completion requests.
// Adapters translate this to the provider's native request format.
type AIRequest struct {
	// Model is the model identifier (e.g., "gpt-5-mini", "claude-sonnet-4-5-20250929").
	Model string

	// SystemPrompt is the optional system prompt/instructions.
	// For providers that support explicit system prompts (Anthropic), this is passed separately.
	// For providers without system prompt support, it may be prepended to UserPrompt.
	SystemPrompt string

	// UserPrompt is the main user message content.
	UserPrompt string

	// ResponseSchema is the optional JSON schema for structured output.
	// This is the output from jsonschema.GenerateSchema[T]().
	// Each adapter handles this appropriately for its provider:
	// - OpenAI: Uses response_format with json_schema
	// - Anthropic: Uses output_config.format with json_schema
	// - Others: May embed schema in prompt and validate locally
	ResponseSchema any

	// Parameters contains provider-specific parameters from config (e.g., max_tokens, temperature).
	// Each adapter reads the parameters it needs.
	Parameters map[string]any

	// Metadata contains optional key-value pairs for audit/correlation purposes.
	Metadata map[string]string
}

// AICompletionResult is the provider-agnostic response from an AI completion request.
type AICompletionResult struct {
	// RawText is the raw text content from the AI response.
	RawText string

	// ParsedJSON contains the parsed JSON response when ResponseSchema was provided.
	// This is the validated JSON that conforms to the requested schema.
	ParsedJSON json.RawMessage

	// ProviderRequestID is the provider's unique identifier for this request.
	// Used for debugging and correlating with provider logs.
	// May be empty if the provider doesn't return a request ID.
	ProviderRequestID string

	// RateLimitInfo contains rate limit information from response headers.
	// May be nil if the provider doesn't return rate limit headers.
	RateLimitInfo *RateLimitInfo

	// StopReason indicates why the model stopped generating.
	// Anthropic: "end_turn", "max_tokens", "stop_sequence"
	// OpenAI: "stop", "length", "content_filter"
	StopReason string

	// WasTruncated is true if the response was truncated due to max_tokens.
	// Convenience field derived from StopReason.
	WasTruncated bool
}

// RateLimitInfo captures rate limit state from provider response headers.
// Used for dynamic rate limiting and CLI output.
type RateLimitInfo struct {
	// Provider is the provider name (e.g., "anthropic", "openai").
	Provider string

	// RequestsLimit is the maximum requests allowed in the current window.
	// 0 means unknown (header not present).
	RequestsLimit int

	// RequestsRemaining is the number of requests remaining in the current window.
	RequestsRemaining int

	// RequestsReset is when the request limit resets.
	RequestsReset time.Time

	// TokensLimit is the maximum tokens allowed in the current window.
	// 0 means unknown (header not present).
	TokensLimit int

	// TokensRemaining is the number of tokens remaining in the current window.
	TokensRemaining int

	// TokensReset is when the token limit resets.
	TokensReset time.Time

	// RetryAfter is the duration to wait before retrying (from 429 responses).
	// Zero means no retry-after header was present.
	RetryAfter time.Duration
}

// AIProviderInfo contains metadata about the configured AI provider for audit logging.
type AIProviderInfo struct {
	// Provider is the provider name: "openai", "openai_compatible", or "anthropic".
	Provider string

	// Model is the configured model identifier.
	Model string

	// EndpointHost is the host (and port if non-default) of the API endpoint.
	// Does not include scheme or query parameters.
	EndpointHost string

	// EndpointPath is the path component of the API endpoint.
	// Does not include scheme, host, or query parameters (query params may contain secrets).
	EndpointPath string
}

// AIProviderError represents a normalized error from an AI provider.
// This allows validation engines to handle errors consistently across providers.
type AIProviderError struct {
	// Category classifies the error type for consistent handling:
	// - "api_error": General API error (retryable for 5xx)
	// - "timeout": Context deadline exceeded
	// - "canceled": Context was canceled
	// - "parse_error": Response parsing or JSON validation failed
	// - "no_response": Provider returned empty response
	// - "rate_limited": HTTP 429 (retryable)
	// - "auth_error": HTTP 401/403 (non-retryable)
	// - "invalid_request": HTTP 400/422 (non-retryable)
	Category string

	// Message is the human-readable error message.
	Message string

	// Retryable indicates whether the operation can be retried.
	Retryable bool

	// Cause is the underlying error, if any.
	Cause error
}

// Error implements the error interface.
func (e *AIProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (cause: %v)", e.Category, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Category, e.Message)
}

// Unwrap returns the underlying error for use with errors.Is/errors.As.
func (e *AIProviderError) Unwrap() error {
	return e.Cause
}

// Error category constants for consistent error classification across providers.
const (
	ErrCategoryAPIError       = "api_error"
	ErrCategoryTimeout        = "timeout"
	ErrCategoryCanceled       = "canceled"
	ErrCategoryParseError     = "parse_error"
	ErrCategoryNoResponse     = "no_response"
	ErrCategoryRateLimited    = "rate_limited"
	ErrCategoryAuthError      = "auth_error"
	ErrCategoryInvalidRequest = "invalid_request"
)

// Provider constants for configuration and factory selection.
const (
	ProviderOpenAI           = "openai"
	ProviderOpenAICompatible = "openai_compatible"
	ProviderAnthropic        = "anthropic"
)

// Default endpoints for known providers.
const (
	DefaultOpenAIEndpoint    = "https://api.openai.com/v1/chat/completions"
	DefaultAnthropicEndpoint = "https://api.anthropic.com/v1/messages"
)

// NewAIProviderClient creates an AIProviderClient based on the configuration.
// It selects the appropriate adapter based on cfg.Validation.AI.Provider.
//
// This function panics if called with an unknown provider value, as this indicates
// a bug in configuration validation (which should catch invalid providers at startup).
func NewAIProviderClient(cfg *config.Config) AIProviderClient {
	aiCfg := cfg.Validation.AI
	provider := aiCfg.Provider

	// Default to OpenAI for backward compatibility (deprecation warning logged during config load)
	if provider == "" {
		provider = ProviderOpenAI
	}

	switch provider {
	case ProviderOpenAI:
		return newOpenAIProvider(cfg)
	case ProviderOpenAICompatible:
		return newOpenAICompatibleProvider(cfg)
	case ProviderAnthropic:
		return newAnthropicProvider(cfg)
	default:
		// BUG: Configuration validation should have caught this invalid provider.
		// If we reach here, there's a bug in config validation logic.
		panic(fmt.Sprintf("BUG: unknown AI provider %q - config validation should have rejected this", provider))
	}
}

// parseEndpointURL extracts host and path from an endpoint URL.
// Query parameters are stripped from the path to avoid logging secrets.
func parseEndpointURL(endpoint string) (host, path string) {
	if endpoint == "" {
		return "", ""
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		// If we can't parse, return the endpoint as-is in host field
		return endpoint, ""
	}

	host = u.Host
	path = u.Path
	// Note: Query params intentionally not included (may contain secrets like api-version tokens)

	return host, path
}
