package gateway

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
)

const (
	// RequiredHeaderNameEnvVar is the environment variable for the required header name.
	RequiredHeaderNameEnvVar = "MAYBE_DONT_REQUIRED_HEADER_NAME"
	// RequiredHeaderValueEnvVar is the environment variable for allowed header values.
	RequiredHeaderValueEnvVar = "MAYBE_DONT_REQUIRED_HEADER_VALUE"
)

// AllowedValue represents a single allowed header value (exact match or compiled glob).
type AllowedValue struct {
	Original string         // Original pattern string
	IsGlob   bool           // True if contains '*'
	Regex    *regexp.Regexp // Pre-compiled regex (nil for exact match)
}

// CallerAuthConfig holds the parsed and validated authentication configuration.
// Glob patterns are pre-compiled at startup for performance.
type CallerAuthConfig struct {
	HeaderName    string
	AllowedValues []AllowedValue
	Enabled       bool
}

// Matches checks if the given value matches this allowed value.
func (av AllowedValue) Matches(value string) bool {
	if av.IsGlob {
		return av.Regex.MatchString(value)
	}
	return value == av.Original
}

// MatchesAny checks if the value matches any allowed value.
func (c *CallerAuthConfig) MatchesAny(value string) bool {
	for _, av := range c.AllowedValues {
		if av.Matches(value) {
			return true
		}
	}
	return false
}

// OriginalValues returns the original pattern strings for logging.
func (c *CallerAuthConfig) OriginalValues() []string {
	values := make([]string, len(c.AllowedValues))
	for i, av := range c.AllowedValues {
		values[i] = av.Original
	}
	return values
}

// AuthMiddleware returns HTTP middleware that validates the required header.
// If no allowed values are configured, the middleware passes all requests through.
// This middleware only validates - caller extraction happens in HTTPContextFunc.
func AuthMiddleware(config *CallerAuthConfig, next http.Handler) http.Handler {
	if config == nil || !config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// OAuth discovery and token endpoints must be reachable without the caller header
		// so clients can bootstrap authentication.
		if isWellKnownOrTokenPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Header lookup is case-insensitive in Go's http.Header
		headerValue := r.Header.Get(config.HeaderName)

		if headerValue == "" {
			http.Error(w, "Unauthorized: missing required header", http.StatusUnauthorized)
			return
		}

		if !config.MatchesAny(headerValue) {
			http.Error(w, "Unauthorized: invalid header value", http.StatusUnauthorized)
			return
		}

		// Validation passed - proceed to next handler
		// Caller extraction happens in HTTPContextFunc to avoid context layering issues
		next.ServeHTTP(w, r)
	})
}

// extractCallerFromRequest extracts the caller identifier from the request header
// and adds it to the context. This is used by HTTPContextFunc/SSEContextFunc.
// Returns the context unchanged if auth is not enabled or header is missing.
func extractCallerFromRequest(ctx context.Context, r *http.Request, config *CallerAuthConfig) context.Context {
	if config == nil || !config.Enabled {
		return ctx
	}

	caller := r.Header.Get(config.HeaderName)
	if caller == "" {
		return ctx
	}

	return WithCaller(ctx, caller)
}

// LoadCallerAuthConfig reads and validates authentication configuration from environment.
// Returns error if glob patterns are invalid (fail fast at startup).
// Both MAYBE_DONT_REQUIRED_HEADER_NAME and MAYBE_DONT_REQUIRED_HEADER_VALUE
// must be set to enable authentication.
func LoadCallerAuthConfig() (*CallerAuthConfig, error) {
	headerName := strings.TrimSpace(os.Getenv(RequiredHeaderNameEnvVar))
	rawValues := strings.TrimSpace(os.Getenv(RequiredHeaderValueEnvVar))

	// Auth disabled if either env var is not set
	if headerName == "" || rawValues == "" {
		return &CallerAuthConfig{Enabled: false}, nil
	}

	// Parse and validate comma-separated values
	var allowedValues []AllowedValue
	for _, v := range strings.Split(rawValues, ",") {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}

		av, err := parseAllowedValue(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed value %q: %w", trimmed, err)
		}
		allowedValues = append(allowedValues, av)
	}

	if len(allowedValues) == 0 {
		return &CallerAuthConfig{Enabled: false}, nil
	}

	return &CallerAuthConfig{
		HeaderName:    headerName,
		AllowedValues: allowedValues,
		Enabled:       true,
	}, nil
}

// parseAllowedValue parses and validates a single allowed value.
// Glob patterns are pre-compiled to regex for performance.
func parseAllowedValue(value string) (AllowedValue, error) {
	if !strings.Contains(value, "*") {
		// Exact match - no validation needed
		return AllowedValue{Original: value, IsGlob: false}, nil
	}

	// Glob pattern - validate and compile
	// Must have at least one non-'*' character
	nonWildcard := strings.ReplaceAll(value, "*", "")
	if len(nonWildcard) == 0 {
		return AllowedValue{}, fmt.Errorf("glob pattern must contain at least one non-'*' character")
	}

	// Convert glob to regex: escape regex special chars, replace * with .+
	regexPattern := "^" + regexp.QuoteMeta(value) + "$"
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, `.+`)

	compiled, err := regexp.Compile(regexPattern)
	if err != nil {
		return AllowedValue{}, fmt.Errorf("failed to compile glob pattern: %w", err)
	}

	return AllowedValue{
		Original: value,
		IsGlob:   true,
		Regex:    compiled,
	}, nil
}
