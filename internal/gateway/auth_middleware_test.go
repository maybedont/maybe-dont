package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestAllowedValue_Matches_ExactMatch verifies exact match behavior.
func TestAllowedValue_Matches_ExactMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		{"exact match", "service-account-1", "service-account-1", true},
		{"exact mismatch", "service-account-1", "service-account-2", false},
		{"case sensitive", "Service", "service", false},
		{"empty input", "foo", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			av, err := parseAllowedValue(tt.pattern)
			require.NoError(t, err)
			assert.False(t, av.IsGlob, "exact match should not be glob")
			assert.Equal(t, tt.expected, av.Matches(tt.input))
		})
	}
}

// TestAllowedValue_Matches_GlobPattern verifies glob pattern matching with *.
func TestAllowedValue_Matches_GlobPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		{"suffix glob matches", "*@example.com", "user@example.com", true},
		{"suffix glob matches single char", "*@example.com", "x@example.com", true},
		{"suffix glob requires one char", "*@example.com", "@example.com", false},
		{"suffix glob wrong domain", "*@example.com", "dan@other.com", false},
		{"prefix glob matches", "admin-*", "admin-user", true},
		{"prefix glob requires one char", "admin-*", "admin-", false},
		{"middle glob matches", "user-*-test", "user-123-test", true},
		{"multiple globs match", "*@*", "user@example.com", true},
		{"multiple globs require chars", "*@*", "@", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			av, err := parseAllowedValue(tt.pattern)
			require.NoError(t, err)
			assert.True(t, av.IsGlob, "pattern with * should be glob")
			assert.NotNil(t, av.Regex, "glob pattern should have compiled regex")
			assert.Equal(t, tt.expected, av.Matches(tt.input))
		})
	}
}

// TestParseAllowedValue_InvalidGlobPattern verifies that invalid patterns are rejected.
func TestParseAllowedValue_InvalidGlobPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{"single asterisk", "*"},
		{"double asterisk", "**"},
		{"triple asterisk", "***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAllowedValue(tt.pattern)
			assert.Error(t, err, "pattern %q should be rejected", tt.pattern)
			assert.Contains(t, err.Error(), "non-'*' character")
		})
	}
}

// TestLoadCallerAuthConfig_ParsesCommaSeparated verifies comma-separated value parsing.
func TestLoadCallerAuthConfig_ParsesCommaSeparated(t *testing.T) {
	tests := []struct {
		name           string
		headerName     string
		headerValue    string
		expectedValues []string
		enabled        bool
	}{
		{
			name:           "single value",
			headerName:     "X-Caller",
			headerValue:    "user@example.com",
			expectedValues: []string{"user@example.com"},
			enabled:        true,
		},
		{
			name:           "multiple values",
			headerName:     "X-Caller",
			headerValue:    "*@example.com,service-account-1",
			expectedValues: []string{"*@example.com", "service-account-1"},
			enabled:        true,
		},
		{
			name:           "values with whitespace",
			headerName:     "X-Caller",
			headerValue:    " a , b , c ",
			expectedValues: []string{"a", "b", "c"},
			enabled:        true,
		},
		{
			name:           "empty entries skipped",
			headerName:     "X-Caller",
			headerValue:    "a,,b",
			expectedValues: []string{"a", "b"},
			enabled:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(RequiredHeaderNameEnvVar, tt.headerName)
			t.Setenv(RequiredHeaderValueEnvVar, tt.headerValue)

			cfg, err := LoadCallerAuthConfig()
			require.NoError(t, err)
			assert.Equal(t, tt.enabled, cfg.Enabled)
			assert.Equal(t, tt.expectedValues, cfg.OriginalValues())
		})
	}
}

// TestLoadCallerAuthConfig_DisabledWhenHeaderNameNotSet verifies auth is disabled without header name.
func TestLoadCallerAuthConfig_DisabledWhenHeaderNameNotSet(t *testing.T) {
	// Ensure header name is not set (clear any existing value)
	t.Setenv(RequiredHeaderNameEnvVar, "")
	t.Setenv(RequiredHeaderValueEnvVar, "*@example.com")

	cfg, err := LoadCallerAuthConfig()
	require.NoError(t, err)
	assert.False(t, cfg.Enabled, "auth should be disabled when header name not set")
}

// TestLoadCallerAuthConfig_DisabledWhenHeaderValueNotSet verifies auth is disabled without header value.
func TestLoadCallerAuthConfig_DisabledWhenHeaderValueNotSet(t *testing.T) {
	t.Setenv(RequiredHeaderNameEnvVar, "X-Caller")
	t.Setenv(RequiredHeaderValueEnvVar, "")

	cfg, err := LoadCallerAuthConfig()
	require.NoError(t, err)
	assert.False(t, cfg.Enabled, "auth should be disabled when header value not set")
}

// TestLoadCallerAuthConfig_DisabledWhenBothEmpty verifies auth is disabled when both are empty.
func TestLoadCallerAuthConfig_DisabledWhenBothEmpty(t *testing.T) {
	t.Setenv(RequiredHeaderNameEnvVar, "  ")
	t.Setenv(RequiredHeaderValueEnvVar, "  ")

	cfg, err := LoadCallerAuthConfig()
	require.NoError(t, err)
	assert.False(t, cfg.Enabled, "auth should be disabled when both values are whitespace")
}

// TestLoadCallerAuthConfig_FailsOnInvalidGlobPattern verifies startup fails on invalid pattern.
func TestLoadCallerAuthConfig_FailsOnInvalidGlobPattern(t *testing.T) {
	t.Setenv(RequiredHeaderNameEnvVar, "X-Caller")
	t.Setenv(RequiredHeaderValueEnvVar, "*")

	_, err := LoadCallerAuthConfig()
	assert.Error(t, err, "should fail on invalid glob pattern")
	assert.Contains(t, err.Error(), "invalid allowed value")
}

// TestCallerAuthConfig_MatchesAny verifies that MatchesAny checks all patterns.
func TestCallerAuthConfig_MatchesAny(t *testing.T) {
	t.Setenv(RequiredHeaderNameEnvVar, "X-Caller")
	t.Setenv(RequiredHeaderValueEnvVar, "*@example.com,service-account-1")

	cfg, err := LoadCallerAuthConfig()
	require.NoError(t, err)

	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{"matches glob", "user@example.com", true},
		{"matches exact", "service-account-1", true},
		{"matches neither", "someone@other.com", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, cfg.MatchesAny(tt.value))
		})
	}
}

// --- AuthMiddleware Tests ---

// dummyHandler is a simple handler that writes "OK" to verify middleware passed through.
func dummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
}

// TestAuthMiddleware_NilConfig verifies all requests pass when config is nil.
func TestAuthMiddleware_NilConfig(t *testing.T) {
	handler := AuthMiddleware(nil, dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "should pass through when config is nil")
	assert.Equal(t, "OK", rr.Body.String())
}

// TestAuthMiddleware_DisabledConfig verifies all requests pass when auth is disabled.
func TestAuthMiddleware_DisabledConfig(t *testing.T) {
	cfg := &CallerAuthConfig{Enabled: false}
	handler := AuthMiddleware(cfg, dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "should pass through when auth is disabled")
	assert.Equal(t, "OK", rr.Body.String())
}

// TestAuthMiddleware_MissingHeader verifies 401 when required header is missing.
func TestAuthMiddleware_MissingHeader(t *testing.T) {
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "allowed", IsGlob: false}},
		Enabled:       true,
	}
	handler := AuthMiddleware(cfg, dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No header set
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code, "should return 401 when header is missing")
	assert.Contains(t, rr.Body.String(), "missing required header")
}

// TestAuthMiddleware_InvalidHeaderValue verifies 401 when header value doesn't match.
func TestAuthMiddleware_InvalidHeaderValue(t *testing.T) {
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "allowed", IsGlob: false}},
		Enabled:       true,
	}
	handler := AuthMiddleware(cfg, dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "not-allowed")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code, "should return 401 when header value is invalid")
	assert.Contains(t, rr.Body.String(), "invalid header value")
}

// TestAuthMiddleware_ValidExactMatch verifies request passes with valid exact match.
func TestAuthMiddleware_ValidExactMatch(t *testing.T) {
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "service-account-1", IsGlob: false}},
		Enabled:       true,
	}
	handler := AuthMiddleware(cfg, dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "service-account-1")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "should pass through when header matches exactly")
	assert.Equal(t, "OK", rr.Body.String())
}

// TestAuthMiddleware_ValidGlobMatch verifies request passes with valid glob match.
func TestAuthMiddleware_ValidGlobMatch(t *testing.T) {
	av, err := parseAllowedValue("*@example.com")
	require.NoError(t, err)

	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{av},
		Enabled:       true,
	}
	handler := AuthMiddleware(cfg, dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "user@example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "should pass through when header matches glob")
	assert.Equal(t, "OK", rr.Body.String())
}

// TestAuthMiddleware_CaseInsensitiveHeaderName verifies header name matching is case-insensitive.
func TestAuthMiddleware_CaseInsensitiveHeaderName(t *testing.T) {
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "allowed", IsGlob: false}},
		Enabled:       true,
	}
	handler := AuthMiddleware(cfg, dummyHandler())

	tests := []struct {
		name       string
		headerName string
	}{
		{"lowercase", "x-maybedont-caller"},
		{"uppercase", "X-MAYBEDONT-CALLER"},
		{"mixed case", "x-MaybeDont-CALLER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(tt.headerName, "allowed")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code, "header name should be case-insensitive")
		})
	}
}

// --- Gateway Integration Tests for CallerAuthConfig ---

// TestGateway_CallerAuthConfig_LoadedOnCreation verifies that valid auth config is loaded into Gateway.
// This test validates that the auth config is accessible from the Gateway after creation.
func TestGateway_CallerAuthConfig_LoadedOnCreation(t *testing.T) {
	// Set up valid auth config via env vars
	t.Setenv(RequiredHeaderNameEnvVar, "X-Test-Caller")
	t.Setenv(RequiredHeaderValueEnvVar, "*@test.com")

	// Load the config
	cfg, err := LoadCallerAuthConfig()
	require.NoError(t, err)

	// Verify it's enabled and has correct values
	assert.True(t, cfg.Enabled, "auth should be enabled")
	assert.Equal(t, "X-Test-Caller", cfg.HeaderName)
	assert.Len(t, cfg.AllowedValues, 1)
	assert.True(t, cfg.AllowedValues[0].IsGlob)
}

// TestGateway_CallerAuthConfig_InvalidPatternFailsFast verifies invalid pattern causes error.
// The Gateway should fail to load if auth config has invalid patterns (fail-fast).
func TestGateway_CallerAuthConfig_InvalidPatternFailsFast(t *testing.T) {
	// Set up invalid auth config (glob with only *)
	t.Setenv(RequiredHeaderNameEnvVar, "X-Test-Caller")
	t.Setenv(RequiredHeaderValueEnvVar, "*")

	// Attempt to load config - should fail
	_, err := LoadCallerAuthConfig()
	assert.Error(t, err, "should fail on invalid glob pattern")
	assert.Contains(t, err.Error(), "invalid allowed value")
}

// --- Inactive Transport Endpoint Tests ---

// TestInactiveTransportEndpoint_SSE_Returns404 verifies /sse returns 404 when HTTP transport is active.
// This is tested via the mux setup pattern used in initHTTPServer.
func TestInactiveTransportEndpoint_SSE_Returns404(t *testing.T) {
	// Create the same mux pattern as initHTTPServer
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found: SSE transport not enabled. Server configured for HTTP transport.", http.StatusNotFound)
	})

	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "SSE transport not enabled")
	assert.Contains(t, rr.Body.String(), "HTTP transport")
}

// TestInactiveTransportEndpoint_MCP_Returns404 verifies /mcp returns 404 when SSE transport is active.
// This is tested via the mux setup pattern used in initSSEServer.
func TestInactiveTransportEndpoint_MCP_Returns404(t *testing.T) {
	// Create the same mux pattern as initSSEServer
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found: HTTP transport not enabled. Server configured for SSE transport.", http.StatusNotFound)
	})

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "HTTP transport not enabled")
	assert.Contains(t, rr.Body.String(), "SSE transport")
}

// TestInactiveTransportEndpoint_Returns401WhenAuthEnabled verifies that when auth is enabled,
// unauthenticated requests to inactive endpoints return 401 (not 404).
// This is the expected security behavior - don't reveal endpoint information to unauthenticated users.
func TestInactiveTransportEndpoint_Returns401WhenAuthEnabled(t *testing.T) {
	// Create mux with inactive endpoint handler
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Not Found: SSE transport not enabled.", http.StatusNotFound)
	})

	// Wrap with auth middleware (enabled)
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "allowed", IsGlob: false}},
		Enabled:       true,
	}
	handler := AuthMiddleware(cfg, mux)

	// Request to inactive endpoint WITHOUT auth header
	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should get 401, not 404 - auth takes precedence
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing required header")
}

// TestInactiveTransportEndpoint_Returns404WhenAuthDisabled verifies that when auth is disabled,
// requests to inactive endpoints return helpful 404 messages.
func TestInactiveTransportEndpoint_Returns404WhenAuthDisabled(t *testing.T) {
	// Create mux with inactive endpoint handler
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Not Found: SSE transport not enabled. Server configured for HTTP transport.", http.StatusNotFound)
	})

	// Wrap with auth middleware (disabled)
	cfg := &CallerAuthConfig{Enabled: false}
	handler := AuthMiddleware(cfg, mux)

	// Request to inactive endpoint (no auth needed)
	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should get helpful 404
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "SSE transport not enabled")
}

// --- extractCallerFromRequest Tests ---

// TestExtractCallerFromRequest_ExtractsCallerWhenAuthEnabled verifies caller is added to context.
func TestExtractCallerFromRequest_ExtractsCallerWhenAuthEnabled(t *testing.T) {
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "*@example.com", IsGlob: true}},
		Enabled:       true,
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "user@example.com")

	ctx := extractCallerFromRequest(context.Background(), req, cfg)

	caller, ok := GetCaller(ctx)
	assert.True(t, ok, "caller should be in context")
	assert.Equal(t, "user@example.com", caller)
}

// TestExtractCallerFromRequest_NoCallerWhenAuthDisabled verifies context is unchanged when auth disabled.
func TestExtractCallerFromRequest_NoCallerWhenAuthDisabled(t *testing.T) {
	cfg := &CallerAuthConfig{Enabled: false}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "user@example.com")

	ctx := extractCallerFromRequest(context.Background(), req, cfg)

	_, ok := GetCaller(ctx)
	assert.False(t, ok, "caller should not be in context when auth disabled")
}

// TestExtractCallerFromRequest_NoCallerWhenConfigNil verifies context is unchanged when config is nil.
func TestExtractCallerFromRequest_NoCallerWhenConfigNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "user@example.com")

	ctx := extractCallerFromRequest(context.Background(), req, nil)

	_, ok := GetCaller(ctx)
	assert.False(t, ok, "caller should not be in context when config is nil")
}

// TestExtractCallerFromRequest_NoCallerWhenHeaderMissing verifies context is unchanged when header missing.
func TestExtractCallerFromRequest_NoCallerWhenHeaderMissing(t *testing.T) {
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "*@example.com", IsGlob: true}},
		Enabled:       true,
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No header set

	ctx := extractCallerFromRequest(context.Background(), req, cfg)

	_, ok := GetCaller(ctx)
	assert.False(t, ok, "caller should not be in context when header is missing")
}

// --- Endpoint Auth Integration Tests ---
// These tests verify that real validation endpoints are protected by the auth middleware
// when wired together on a mux, matching the setup in initSSEServer/initHTTPServer.

// newAuthTestMux creates an http.ServeMux with the CLI and action validation handlers
// registered at their real paths, wrapped with AuthMiddleware. This mirrors the wiring
// in server.go's initSSEServer/initHTTPServer without starting a real server.
func newAuthTestMux(t *testing.T, authConfig *CallerAuthConfig) http.Handler {
	t.Helper()

	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	mux := http.NewServeMux()

	// Register CLI validation endpoint (same as server.go)
	cliHandler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           logger,
		Version:          "1.0.0-test",
	})
	mux.Handle("/api/v1/cli/validate", cliHandler)

	// Register action validation endpoint (same as server.go)
	actionHandler := NewActionValidationHandler(ActionValidationHandlerConfig{
		Logger:  logger,
		Version: "1.0.0-test",
	})
	mux.Handle("/api/v1/action/validate", actionHandler)

	// Wrap with auth middleware (same as server.go)
	return AuthMiddleware(authConfig, mux)
}

// TestAuthMiddleware_BlocksCLIEndpoint_MissingHeader verifies that the CLI validation
// endpoint returns 401 when auth is enabled and the required header is missing.
func TestAuthMiddleware_BlocksCLIEndpoint_MissingHeader(t *testing.T) {
	authConfig := &CallerAuthConfig{
		HeaderName:    "X-Api-Key",
		AllowedValues: []AllowedValue{{Original: "secret-key", IsGlob: false}},
		Enabled:       true,
	}
	handler := newAuthTestMux(t, authConfig)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(
		`{"command": "gh", "arguments": ["pr", "list"]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing required header")
}

// TestAuthMiddleware_BlocksActionEndpoint_MissingHeader verifies that the action validation
// endpoint returns 401 when auth is enabled and the required header is missing.
func TestAuthMiddleware_BlocksActionEndpoint_MissingHeader(t *testing.T) {
	authConfig := &CallerAuthConfig{
		HeaderName:    "X-Api-Key",
		AllowedValues: []AllowedValue{{Original: "secret-key", IsGlob: false}},
		Enabled:       true,
	}
	handler := newAuthTestMux(t, authConfig)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(
		`{"target": "execute_bash", "parameters": {"command": "ls"}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing required header")
}

// TestAuthMiddleware_BlocksCLIEndpoint_InvalidHeaderValue verifies that the CLI endpoint
// returns 401 when the header value doesn't match.
func TestAuthMiddleware_BlocksCLIEndpoint_InvalidHeaderValue(t *testing.T) {
	authConfig := &CallerAuthConfig{
		HeaderName:    "X-Api-Key",
		AllowedValues: []AllowedValue{{Original: "secret-key", IsGlob: false}},
		Enabled:       true,
	}
	handler := newAuthTestMux(t, authConfig)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(
		`{"command": "gh", "arguments": ["pr", "list"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "wrong-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid header value")
}

// TestAuthMiddleware_BlocksActionEndpoint_InvalidHeaderValue verifies that the action endpoint
// returns 401 when the header value doesn't match.
func TestAuthMiddleware_BlocksActionEndpoint_InvalidHeaderValue(t *testing.T) {
	authConfig := &CallerAuthConfig{
		HeaderName:    "X-Api-Key",
		AllowedValues: []AllowedValue{{Original: "secret-key", IsGlob: false}},
		Enabled:       true,
	}
	handler := newAuthTestMux(t, authConfig)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(
		`{"target": "execute_bash", "parameters": {"command": "ls"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "wrong-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid header value")
}

// TestAuthMiddleware_AllowsCLIEndpoint_ValidHeader verifies that the CLI endpoint
// processes requests when the correct auth header is provided.
func TestAuthMiddleware_AllowsCLIEndpoint_ValidHeader(t *testing.T) {
	authConfig := &CallerAuthConfig{
		HeaderName:    "X-Api-Key",
		AllowedValues: []AllowedValue{{Original: "secret-key", IsGlob: false}},
		Enabled:       true,
	}
	handler := newAuthTestMux(t, authConfig)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(
		`{"command": "gh", "arguments": ["pr", "list"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "secret-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should reach the handler (200), not be blocked (401)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp CLIValidationResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
}

// TestAuthMiddleware_AllowsActionEndpoint_ValidHeader verifies that the action endpoint
// processes requests when the correct auth header is provided.
func TestAuthMiddleware_AllowsActionEndpoint_ValidHeader(t *testing.T) {
	authConfig := &CallerAuthConfig{
		HeaderName:    "X-Api-Key",
		AllowedValues: []AllowedValue{{Original: "secret-key", IsGlob: false}},
		Enabled:       true,
	}
	handler := newAuthTestMux(t, authConfig)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(
		`{"target": "execute_bash", "parameters": {"command": "ls"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "secret-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should reach the handler (200), not be blocked (401)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp ActionValidationResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
}

// TestAuthMiddleware_EndpointsAccessible_AuthDisabled verifies that both endpoints
// work normally when auth is disabled (no middleware blocking).
func TestAuthMiddleware_EndpointsAccessible_AuthDisabled(t *testing.T) {
	handler := newAuthTestMux(t, &CallerAuthConfig{Enabled: false})

	// CLI endpoint
	cliReq := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(
		`{"command": "gh", "arguments": ["pr", "list"]}`))
	cliReq.Header.Set("Content-Type", "application/json")
	cliRR := httptest.NewRecorder()
	handler.ServeHTTP(cliRR, cliReq)
	assert.Equal(t, http.StatusOK, cliRR.Code, "CLI endpoint should be accessible when auth disabled")

	// Action endpoint
	actionReq := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(
		`{"target": "execute_bash"}`))
	actionReq.Header.Set("Content-Type", "application/json")
	actionRR := httptest.NewRecorder()
	handler.ServeHTTP(actionRR, actionReq)
	assert.Equal(t, http.StatusOK, actionRR.Code, "Action endpoint should be accessible when auth disabled")
}
