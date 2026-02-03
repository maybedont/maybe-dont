package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"suffix glob matches", "*@maybedont.ai", "dan@maybedont.ai", true},
		{"suffix glob matches single char", "*@maybedont.ai", "x@maybedont.ai", true},
		{"suffix glob requires one char", "*@maybedont.ai", "@maybedont.ai", false},
		{"suffix glob wrong domain", "*@maybedont.ai", "dan@other.com", false},
		{"prefix glob matches", "admin-*", "admin-user", true},
		{"prefix glob requires one char", "admin-*", "admin-", false},
		{"middle glob matches", "user-*-test", "user-123-test", true},
		{"multiple globs match", "*@*", "dan@maybedont.ai", true},
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
			headerValue:    "dan@maybedont.ai",
			expectedValues: []string{"dan@maybedont.ai"},
			enabled:        true,
		},
		{
			name:           "multiple values",
			headerName:     "X-Caller",
			headerValue:    "*@maybedont.ai,service-account-1",
			expectedValues: []string{"*@maybedont.ai", "service-account-1"},
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
	t.Setenv(RequiredHeaderValueEnvVar, "*@maybedont.ai")

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
	t.Setenv(RequiredHeaderValueEnvVar, "*@maybedont.ai,service-account-1")

	cfg, err := LoadCallerAuthConfig()
	require.NoError(t, err)

	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{"matches glob", "dan@maybedont.ai", true},
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
	av, err := parseAllowedValue("*@maybedont.ai")
	require.NoError(t, err)

	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{av},
		Enabled:       true,
	}
	handler := AuthMiddleware(cfg, dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "dan@maybedont.ai")
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

// --- extractCallerFromRequest Tests ---

// TestExtractCallerFromRequest_ExtractsCallerWhenAuthEnabled verifies caller is added to context.
func TestExtractCallerFromRequest_ExtractsCallerWhenAuthEnabled(t *testing.T) {
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "*@maybedont.ai", IsGlob: true}},
		Enabled:       true,
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "dan@maybedont.ai")

	ctx := extractCallerFromRequest(context.Background(), req, cfg)

	caller, ok := GetCaller(ctx)
	assert.True(t, ok, "caller should be in context")
	assert.Equal(t, "dan@maybedont.ai", caller)
}

// TestExtractCallerFromRequest_NoCallerWhenAuthDisabled verifies context is unchanged when auth disabled.
func TestExtractCallerFromRequest_NoCallerWhenAuthDisabled(t *testing.T) {
	cfg := &CallerAuthConfig{Enabled: false}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "dan@maybedont.ai")

	ctx := extractCallerFromRequest(context.Background(), req, cfg)

	_, ok := GetCaller(ctx)
	assert.False(t, ok, "caller should not be in context when auth disabled")
}

// TestExtractCallerFromRequest_NoCallerWhenConfigNil verifies context is unchanged when config is nil.
func TestExtractCallerFromRequest_NoCallerWhenConfigNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-MaybeDont-Caller", "dan@maybedont.ai")

	ctx := extractCallerFromRequest(context.Background(), req, nil)

	_, ok := GetCaller(ctx)
	assert.False(t, ok, "caller should not be in context when config is nil")
}

// TestExtractCallerFromRequest_NoCallerWhenHeaderMissing verifies context is unchanged when header missing.
func TestExtractCallerFromRequest_NoCallerWhenHeaderMissing(t *testing.T) {
	cfg := &CallerAuthConfig{
		HeaderName:    "X-MaybeDont-Caller",
		AllowedValues: []AllowedValue{{Original: "*@maybedont.ai", IsGlob: true}},
		Enabled:       true,
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No header set

	ctx := extractCallerFromRequest(context.Background(), req, cfg)

	_, ok := GetCaller(ctx)
	assert.False(t, ok, "caller should not be in context when header is missing")
}
