package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHandleOAuthMetadata(t *testing.T) {
	// Create a minimal config for testing
	cfg := &config.Config{
		Server: struct {
			Type       config.ServerType `mapstructure:"type"`
			ListenAddr string            `mapstructure:"listen_addr"`
			OAuth      struct {
				Enabled                 bool   `mapstructure:"enabled"`
				AuthorizationServer     string `mapstructure:"authorization_server"`
				TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
				Realm                   string `mapstructure:"realm"`
				CORS                    struct {
					Enabled        bool     `mapstructure:"enabled"`
					AllowedOrigins []string `mapstructure:"allowed_origins"`
					MaxAge         int      `mapstructure:"max_age"`
				} `mapstructure:"cors"`
			} `mapstructure:"oauth"`
			SSE struct {
				TLS struct {
					Enabled  bool   `mapstructure:"enabled"`
					CertFile string `mapstructure:"cert_file"`
					KeyFile  string `mapstructure:"key_file"`
				} `mapstructure:"tls"`
			} `mapstructure:"sse"`
		}{
			Type:       "http",
			ListenAddr: "localhost:8080",
		},
		DownstreamMCPServers: map[string]config.ClientConfig{
			"test": {
				Type:    "stdio",
				Command: "echo",
			},
		},
		Audit: struct {
			Enabled bool   `mapstructure:"enabled"`
			Path    string `mapstructure:"path"`
		}{
			Enabled: false,
			Path:    "/dev/null",
		},
		Logging: struct {
			LogLevel string `mapstructure:"level"`
			Path     string `mapstructure:"path"`
		}{
			LogLevel: "info",
		},
	}

	logger := zap.NewNop()
	gateway, err := New(cfg, logger)
	require.NoError(t, err)

	tests := []struct {
		name             string
		method           string
		host             string
		expectedStatus   int
		expectedResource string
	}{
		{
			name:             "GET request returns metadata",
			method:           "GET",
			host:             "example.com:8080",
			expectedStatus:   http.StatusOK,
			expectedResource: "http://example.com:8080",
		},
		{
			name:           "POST request returns method not allowed",
			method:         "POST",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:             "GET with localhost",
			method:           "GET",
			host:             "localhost:8080",
			expectedStatus:   http.StatusOK,
			expectedResource: "http://localhost:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/.well-known/oauth-protected-resource", nil)
			if tt.host != "" {
				req.Host = tt.host
			}

			w := httptest.NewRecorder()
			gateway.handleOAuthMetadata(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				// Check content type
				assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

				// Check cache headers
				assert.Equal(t, "no-cache, no-store, must-revalidate", w.Header().Get("Cache-Control"))

				// Parse and validate JSON response
				var metadata OAuthMetadata
				err := json.Unmarshal(w.Body.Bytes(), &metadata)
				require.NoError(t, err)

				// Validate required fields
				assert.Equal(t, tt.expectedResource, metadata.Resource)
				assert.Contains(t, metadata.BearerMethodsSupported, "header")
				assert.Contains(t, metadata.ScopesSupported, "mcp:read")
				assert.Contains(t, metadata.ScopesSupported, "mcp:write")
			}

			if tt.expectedStatus == http.StatusMethodNotAllowed {
				assert.Equal(t, "GET", w.Header().Get("Allow"))
			}
		})
	}
}

func TestOAuthMetadataJSONStructure(t *testing.T) {
	metadata := OAuthMetadata{
		Resource:               "https://example.com",
		AuthorizationServers:   []string{"https://auth.example.com"},
		BearerMethodsSupported: []string{"header", "body"},
		ScopesSupported:        []string{"mcp:read", "mcp:write"},
		ResourceDocumentation:  "https://docs.example.com",
	}

	jsonData, err := json.Marshal(metadata)
	require.NoError(t, err)

	// Verify that the JSON contains all expected fields
	jsonStr := string(jsonData)
	assert.Contains(t, jsonStr, `"resource":"https://example.com"`)
	assert.Contains(t, jsonStr, `"authorization_servers":["https://auth.example.com"]`)
	assert.Contains(t, jsonStr, `"bearer_methods_supported":["header","body"]`)
	assert.Contains(t, jsonStr, `"scopes_supported":["mcp:read","mcp:write"]`)
	assert.Contains(t, jsonStr, `"resource_documentation":"https://docs.example.com"`)
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		expectedToken string
	}{
		{
			name:          "valid bearer token",
			authHeader:    "Bearer abc123token",
			expectedToken: "abc123token",
		},
		{
			name:          "bearer token with extra spaces",
			authHeader:    "Bearer   abc123token   ",
			expectedToken: "abc123token",
		},
		{
			name:          "empty authorization header",
			authHeader:    "",
			expectedToken: "",
		},
		{
			name:          "non-bearer authorization",
			authHeader:    "Basic dXNlcjpwYXNz",
			expectedToken: "",
		},
		{
			name:          "bearer without token",
			authHeader:    "Bearer",
			expectedToken: "",
		},
		{
			name:          "case sensitive bearer",
			authHeader:    "bearer abc123token",
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/mcp", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			token := extractBearerToken(req)
			assert.Equal(t, tt.expectedToken, token)
		})
	}
}

func TestValidateBearerToken(t *testing.T) {
	cfg := &config.Config{
		Server: struct {
			Type       config.ServerType `mapstructure:"type"`
			ListenAddr string            `mapstructure:"listen_addr"`
			OAuth      struct {
				Enabled                 bool   `mapstructure:"enabled"`
				AuthorizationServer     string `mapstructure:"authorization_server"`
				TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
				Realm                   string `mapstructure:"realm"`
				CORS                    struct {
					Enabled        bool     `mapstructure:"enabled"`
					AllowedOrigins []string `mapstructure:"allowed_origins"`
					MaxAge         int      `mapstructure:"max_age"`
				} `mapstructure:"cors"`
			} `mapstructure:"oauth"`
			SSE struct {
				TLS struct {
					Enabled  bool   `mapstructure:"enabled"`
					CertFile string `mapstructure:"cert_file"`
					KeyFile  string `mapstructure:"key_file"`
				} `mapstructure:"tls"`
			} `mapstructure:"sse"`
		}{
			Type:       "http",
			ListenAddr: "localhost:8080",
		},
		DownstreamMCPServers: map[string]config.ClientConfig{
			"test": {
				Type:    "stdio",
				Command: "echo",
			},
		},
		Audit: struct {
			Enabled bool   `mapstructure:"enabled"`
			Path    string `mapstructure:"path"`
		}{
			Enabled: false,
			Path:    "/dev/null",
		},
		Logging: struct {
			LogLevel string `mapstructure:"level"`
			Path     string `mapstructure:"path"`
		}{
			LogLevel: "info",
		},
	}

	logger := zap.NewNop()
	gateway, err := New(cfg, logger)
	require.NoError(t, err)

	tests := []struct {
		name        string
		token       string
		expectError bool
		errorType   string
	}{
		{
			name:        "valid token",
			token:       "valid_token_123456789",
			expectError: false,
		},
		{
			name:        "empty token",
			token:       "",
			expectError: true,
			errorType:   "missing_token",
		},
		{
			name:        "short invalid token",
			token:       "short",
			expectError: true,
			errorType:   "invalid_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gateway.validateBearerToken(tt.token)

			if tt.expectError {
				require.Error(t, err)
				tokenErr, ok := err.(*TokenValidationError)
				require.True(t, ok, "Expected TokenValidationError")
				assert.Equal(t, tt.errorType, tokenErr.Type)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBuildWWWAuthenticateHeader(t *testing.T) {
	cfg := &config.Config{
		Server: struct {
			Type       config.ServerType `mapstructure:"type"`
			ListenAddr string            `mapstructure:"listen_addr"`
			OAuth      struct {
				Enabled                 bool   `mapstructure:"enabled"`
				AuthorizationServer     string `mapstructure:"authorization_server"`
				TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
				Realm                   string `mapstructure:"realm"`
				CORS                    struct {
					Enabled        bool     `mapstructure:"enabled"`
					AllowedOrigins []string `mapstructure:"allowed_origins"`
					MaxAge         int      `mapstructure:"max_age"`
				} `mapstructure:"cors"`
			} `mapstructure:"oauth"`
			SSE struct {
				TLS struct {
					Enabled  bool   `mapstructure:"enabled"`
					CertFile string `mapstructure:"cert_file"`
					KeyFile  string `mapstructure:"key_file"`
				} `mapstructure:"tls"`
			} `mapstructure:"sse"`
		}{
			Type:       "http",
			ListenAddr: "localhost:8080",
			OAuth: struct {
				Enabled                 bool   `mapstructure:"enabled"`
				AuthorizationServer     string `mapstructure:"authorization_server"`
				TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
				Realm                   string `mapstructure:"realm"`
				CORS                    struct {
					Enabled        bool     `mapstructure:"enabled"`
					AllowedOrigins []string `mapstructure:"allowed_origins"`
					MaxAge         int      `mapstructure:"max_age"`
				} `mapstructure:"cors"`
			}{
				Enabled: true,
				Realm:   "test-realm",
			},
		},
		DownstreamMCPServers: map[string]config.ClientConfig{
			"test": {
				Type:    "stdio",
				Command: "echo",
			},
		},
		Audit: struct {
			Enabled bool   `mapstructure:"enabled"`
			Path    string `mapstructure:"path"`
		}{
			Enabled: false,
			Path:    "/dev/null",
		},
		Logging: struct {
			LogLevel string `mapstructure:"level"`
			Path     string `mapstructure:"path"`
		}{
			LogLevel: "info",
		},
	}

	logger := zap.NewNop()
	gateway, err := New(cfg, logger)
	require.NoError(t, err)

	tests := []struct {
		name             string
		host             string
		errorType        string
		expectedContains []string
	}{
		{
			name:      "with error type",
			host:      "example.com:8080",
			errorType: "invalid_token",
			expectedContains: []string{
				`realm="test-realm"`,
				`resource_metadata="http://example.com:8080/.well-known/oauth-protected-resource"`,
				`error="invalid_token"`,
			},
		},
		{
			name:      "without error type",
			host:      "localhost:8080",
			errorType: "",
			expectedContains: []string{
				`realm="test-realm"`,
				`resource_metadata="http://localhost:8080/.well-known/oauth-protected-resource"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/mcp", nil)
			req.Host = tt.host

			header := gateway.buildWWWAuthenticateHeader(req, tt.errorType)

			for _, expected := range tt.expectedContains {
				assert.Contains(t, header, expected)
			}

			if tt.errorType == "" {
				assert.NotContains(t, header, "error=")
			}
		})
	}
}

func TestOAuthMiddleware(t *testing.T) {
	// Test handler that just returns 200 OK
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	tests := []struct {
		name            string
		oauthEnabled    bool
		authHeader      string
		expectedStatus  int
		expectedWWWAuth bool
		expectedBody    string
	}{
		{
			name:            "OAuth disabled - request passes through",
			oauthEnabled:    false,
			authHeader:      "",
			expectedStatus:  http.StatusOK,
			expectedWWWAuth: false,
			expectedBody:    "success",
		},
		{
			name:            "OAuth enabled, valid token",
			oauthEnabled:    true,
			authHeader:      "Bearer valid_token_123456789",
			expectedStatus:  http.StatusOK,
			expectedWWWAuth: false,
			expectedBody:    "success",
		},
		{
			name:            "OAuth enabled, missing token",
			oauthEnabled:    true,
			authHeader:      "",
			expectedStatus:  http.StatusUnauthorized,
			expectedWWWAuth: true,
			expectedBody:    "Bearer token is required",
		},
		{
			name:            "OAuth enabled, invalid token",
			oauthEnabled:    true,
			authHeader:      "Bearer short",
			expectedStatus:  http.StatusUnauthorized,
			expectedWWWAuth: true,
			expectedBody:    "Invalid bearer token format",
		},
		{
			name:            "OAuth enabled, non-bearer auth",
			oauthEnabled:    true,
			authHeader:      "Basic dXNlcjpwYXNz",
			expectedStatus:  http.StatusUnauthorized,
			expectedWWWAuth: true,
			expectedBody:    "Bearer token is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Server: struct {
					Type       config.ServerType `mapstructure:"type"`
					ListenAddr string            `mapstructure:"listen_addr"`
					OAuth      struct {
						Enabled                 bool   `mapstructure:"enabled"`
						AuthorizationServer     string `mapstructure:"authorization_server"`
						TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
						Realm                   string `mapstructure:"realm"`
						CORS                    struct {
							Enabled        bool     `mapstructure:"enabled"`
							AllowedOrigins []string `mapstructure:"allowed_origins"`
							MaxAge         int      `mapstructure:"max_age"`
						} `mapstructure:"cors"`
					} `mapstructure:"oauth"`
					SSE struct {
						TLS struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
				}{
					Type:       "http",
					ListenAddr: "localhost:8080",
					OAuth: struct {
						Enabled                 bool   `mapstructure:"enabled"`
						AuthorizationServer     string `mapstructure:"authorization_server"`
						TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
						Realm                   string `mapstructure:"realm"`
						CORS                    struct {
							Enabled        bool     `mapstructure:"enabled"`
							AllowedOrigins []string `mapstructure:"allowed_origins"`
							MaxAge         int      `mapstructure:"max_age"`
						} `mapstructure:"cors"`
					}{
						Enabled: tt.oauthEnabled,
						Realm:   "mcp-server",
					},
				},
				DownstreamMCPServers: map[string]config.ClientConfig{
					"test": {
						Type:    "stdio",
						Command: "echo",
					},
				},
				Audit: struct {
					Enabled bool   `mapstructure:"enabled"`
					Path    string `mapstructure:"path"`
				}{
					Enabled: false,
					Path:    "/dev/null",
				},
				Logging: struct {
					LogLevel string `mapstructure:"level"`
					Path     string `mapstructure:"path"`
				}{
					LogLevel: "info",
				},
			}

			logger := zap.NewNop()
			gateway, err := New(cfg, logger)
			require.NoError(t, err)

			// Create middleware-wrapped handler
			handler := gateway.oauthMiddleware(testHandler)

			// Create request
			req := httptest.NewRequest("GET", "/mcp", nil)
			req.Host = "localhost:8080"
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Execute request
			handler.ServeHTTP(w, req)

			// Check status code
			assert.Equal(t, tt.expectedStatus, w.Code)

			// Check WWW-Authenticate header
			wwwAuth := w.Header().Get("WWW-Authenticate")
			if tt.expectedWWWAuth {
				assert.NotEmpty(t, wwwAuth, "Expected WWW-Authenticate header")
				assert.Contains(t, wwwAuth, `realm="mcp-server"`)
				assert.Contains(t, wwwAuth, `resource_metadata=`)
			} else {
				assert.Empty(t, wwwAuth, "Did not expect WWW-Authenticate header")
			}

			// Check response body contains expected text
			assert.Contains(t, w.Body.String(), tt.expectedBody)
		})
	}
}

func TestWellKnownCORSMiddleware(t *testing.T) {
	// Test handler that returns success
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	tests := []struct {
		name                   string
		corsEnabled            bool
		allowedOrigins         []string
		requestPath            string
		requestMethod          string
		requestOrigin          string
		expectedStatus         int
		expectedCORSHeaders    bool
		expectedAllowedOrigin  string
		expectedAllowMethods   string
		expectedAllowHeaders   string
		expectedMaxAge         string
	}{
		{
			name:                "CORS disabled - no headers added",
			corsEnabled:         false,
			allowedOrigins:      []string{"https://example.com"},
			requestPath:         "/.well-known/oauth-protected-resource",
			requestMethod:       "GET",
			requestOrigin:       "https://example.com",
			expectedStatus:      http.StatusOK,
			expectedCORSHeaders: false,
		},
		{
			name:                "Non .well-known path - no CORS headers",
			corsEnabled:         true,
			allowedOrigins:      []string{"https://example.com"},
			requestPath:         "/mcp",
			requestMethod:       "GET",
			requestOrigin:       "https://example.com",
			expectedStatus:      http.StatusOK,
			expectedCORSHeaders: false,
		},
		{
			name:                  "Allowed origin - GET request",
			corsEnabled:          true,
			allowedOrigins:       []string{"https://example.com", "https://claude.ai"},
			requestPath:          "/.well-known/oauth-protected-resource",
			requestMethod:        "GET",
			requestOrigin:        "https://claude.ai",
			expectedStatus:       http.StatusOK,
			expectedCORSHeaders:  true,
			expectedAllowedOrigin: "https://claude.ai",
			expectedAllowMethods: "GET, OPTIONS",
			expectedAllowHeaders: "Authorization, Content-Type, mcp-protocol-version",
		},
		{
			name:                  "Wildcard origin allows all",
			corsEnabled:          true,
			allowedOrigins:       []string{"*"},
			requestPath:          "/.well-known/oauth-protected-resource",
			requestMethod:        "GET",
			requestOrigin:        "https://any-domain.com",
			expectedStatus:       http.StatusOK,
			expectedCORSHeaders:  true,
			expectedAllowedOrigin: "https://any-domain.com",
			expectedAllowMethods: "GET, OPTIONS",
			expectedAllowHeaders: "Authorization, Content-Type, mcp-protocol-version",
		},
		{
			name:                "Disallowed origin - no headers",
			corsEnabled:         true,
			allowedOrigins:      []string{"https://example.com"},
			requestPath:         "/.well-known/oauth-protected-resource",
			requestMethod:       "GET",
			requestOrigin:       "https://evil.com",
			expectedStatus:      http.StatusOK,
			expectedCORSHeaders: false,
		},
		{
			name:                  "Preflight OPTIONS request - allowed origin",
			corsEnabled:          true,
			allowedOrigins:       []string{"https://claude.ai"},
			requestPath:          "/.well-known/oauth-protected-resource",
			requestMethod:        "OPTIONS",
			requestOrigin:        "https://claude.ai",
			expectedStatus:       http.StatusNoContent,
			expectedCORSHeaders:  true,
			expectedAllowedOrigin: "https://claude.ai",
			expectedAllowMethods: "GET, OPTIONS",
			expectedAllowHeaders: "Authorization, Content-Type, mcp-protocol-version",
			expectedMaxAge:      "86400",
		},
		{
			name:                "Preflight OPTIONS request - disallowed origin",
			corsEnabled:         true,
			allowedOrigins:      []string{"https://example.com"},
			requestPath:         "/.well-known/oauth-protected-resource",
			requestMethod:       "OPTIONS",
			requestOrigin:       "https://evil.com",
			expectedStatus:      http.StatusNoContent,
			expectedCORSHeaders: false,
		},
		{
			name:                  "MCP suffix path also gets CORS",
			corsEnabled:          true,
			allowedOrigins:       []string{"https://mcp-inspector.com"},
			requestPath:          "/.well-known/oauth-protected-resource/mcp",
			requestMethod:        "GET",
			requestOrigin:        "https://mcp-inspector.com",
			expectedStatus:       http.StatusOK,
			expectedCORSHeaders:  true,
			expectedAllowedOrigin: "https://mcp-inspector.com",
			expectedAllowMethods: "GET, OPTIONS",
			expectedAllowHeaders: "Authorization, Content-Type, mcp-protocol-version",
		},
		{
			name:                "Empty origin header - no CORS headers",
			corsEnabled:         true,
			allowedOrigins:      []string{"*"},
			requestPath:         "/.well-known/oauth-protected-resource",
			requestMethod:       "GET",
			requestOrigin:       "",
			expectedStatus:      http.StatusOK,
			expectedCORSHeaders: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create gateway with test config
			cfg := &config.Config{
				Server: struct {
					Type       config.ServerType `mapstructure:"type"`
					ListenAddr string            `mapstructure:"listen_addr"`
					OAuth      struct {
						Enabled                 bool   `mapstructure:"enabled"`
						AuthorizationServer     string `mapstructure:"authorization_server"`
						TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
						Realm                   string `mapstructure:"realm"`
						CORS                    struct {
							Enabled        bool     `mapstructure:"enabled"`
							AllowedOrigins []string `mapstructure:"allowed_origins"`
							MaxAge         int      `mapstructure:"max_age"`
						} `mapstructure:"cors"`
					} `mapstructure:"oauth"`
					SSE struct {
						TLS struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
				}{
					Type:       "http",
					ListenAddr: "localhost:8080",
				},
				DownstreamMCPServers: map[string]config.ClientConfig{
					"test": {
						Type:    "stdio",
						Command: "echo",
					},
				},
				Audit: struct {
					Enabled bool   `mapstructure:"enabled"`
					Path    string `mapstructure:"path"`
				}{
					Enabled: false,
					Path:    "/dev/null",
				},
				Logging: struct {
					LogLevel string `mapstructure:"level"`
					Path     string `mapstructure:"path"`
				}{
					LogLevel: "info",
				},
			}

			// Configure OAuth and CORS settings
			cfg.Server.OAuth.CORS.Enabled = tt.corsEnabled
			cfg.Server.OAuth.CORS.AllowedOrigins = tt.allowedOrigins
			cfg.Server.OAuth.CORS.MaxAge = 86400

			logger := zap.NewNop()
			gateway, err := New(cfg, logger)
			require.NoError(t, err)

			// Create request
			req := httptest.NewRequest(tt.requestMethod, tt.requestPath, nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}

			// Apply middleware
			handler := gateway.wellKnownCORSMiddleware(testHandler)

			// Execute request
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// Check status
			assert.Equal(t, tt.expectedStatus, w.Code)

			// Check CORS headers
			if tt.expectedCORSHeaders {
				assert.Equal(t, tt.expectedAllowedOrigin, w.Header().Get("Access-Control-Allow-Origin"))
				assert.Equal(t, tt.expectedAllowMethods, w.Header().Get("Access-Control-Allow-Methods"))
				assert.Equal(t, tt.expectedAllowHeaders, w.Header().Get("Access-Control-Allow-Headers"))
				if tt.expectedMaxAge != "" {
					assert.Equal(t, tt.expectedMaxAge, w.Header().Get("Access-Control-Max-Age"))
				}
			} else {
				assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
				assert.Empty(t, w.Header().Get("Access-Control-Allow-Methods"))
				assert.Empty(t, w.Header().Get("Access-Control-Allow-Headers"))
				assert.Empty(t, w.Header().Get("Access-Control-Max-Age"))
			}
		})
	}
}
