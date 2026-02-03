# Gateway Authentication Token

## Overview

Add a simple bearer token authentication mechanism to the MCP gateway for internal use. When enabled via environment variable, all HTTP requests must include a valid `Authorization` header to access the gateway.

## Requirements

1. **Optional enforcement**: If `MAYBE_DONT_AUTH_TOKEN` is not set, no authentication is required
2. **Early rejection**: Validate the token as early as possible in the HTTP request lifecycle, before any MCP processing
3. **HTTP 401 response**: Return `401 Unauthorized` when authentication fails
4. **HTTP transport only**: Authentication applies only to HTTP server mode (not STDIO)

## Design

### Configuration

- **Environment variable**: `MAYBE_DONT_AUTH_TOKEN`
- **No config file support**: This is intentionally env-var only for internal use
- **Value format**: Any non-empty string serves as the expected token

### Header Format

Clients must send the token using standard HTTP Bearer authentication:

```
Authorization: Bearer <token>
```

This follows RFC 6750 and is widely supported by HTTP clients.

### Implementation Approach

Use standard Go HTTP middleware wrapping the mcp-go `StreamableHTTPServer`. This is idiomatic because:

1. `StreamableHTTPServer` implements `http.Handler` interface
2. Middleware can reject requests before MCP processing begins
3. Returns proper HTTP status codes (not JSON-RPC errors)
4. Follows Go's compositional HTTP handler pattern

### Code Changes

#### 1. New file: `internal/gateway/auth_middleware.go`

```go
package gateway

import (
    "net/http"
    "os"
    "strings"
)

const (
    // AuthTokenEnvVar is the environment variable name for the gateway auth token
    AuthTokenEnvVar = "MAYBE_DONT_AUTH_TOKEN"
)

// AuthMiddleware returns HTTP middleware that validates bearer tokens.
// If expectedToken is empty, the middleware passes all requests through.
func AuthMiddleware(expectedToken string, next http.Handler) http.Handler {
    // If no token configured, pass through without authentication
    if expectedToken == "" {
        return next
    }

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")

        // Check for Bearer token
        if !strings.HasPrefix(authHeader, "Bearer ") {
            http.Error(w, "Unauthorized: missing or invalid Authorization header", http.StatusUnauthorized)
            return
        }

        token := strings.TrimPrefix(authHeader, "Bearer ")
        if token != expectedToken {
            http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
            return
        }

        // Token valid, proceed to next handler
        next.ServeHTTP(w, r)
    })
}

// GetAuthToken reads the authentication token from the environment.
// Returns empty string if not set (authentication disabled).
func GetAuthToken() string {
    return os.Getenv(AuthTokenEnvVar)
}
```

#### 2. Modify `internal/gateway/server.go` - `initHTTPServer`

Change the HTTP server initialization to wrap the handler with auth middleware:

```go
func (g *Gateway) initHTTPServer(ctx context.Context) error {
    srv, err := g.initMCPServer()
    if err != nil {
        return fmt.Errorf("failed to initialize MCP server: %w", err)
    }

    // Create HTTP server with auth extraction context function
    httpSrv := server.NewStreamableHTTPServer(srv,
        server.WithEndpointPath("/mcp"),
        server.WithHTTPContextFunc(g.extractAuthFromRequest),
    )

    g.server = srv

    // Get auth token from environment
    authToken := GetAuthToken()
    if authToken != "" {
        g.logger.Info(ctx, "Gateway authentication enabled")
    }

    // Wrap handler with auth middleware
    handler := AuthMiddleware(authToken, httpSrv)

    // Create HTTP server with custom handler instead of using httpSrv.Start()
    httpServer := &http.Server{
        Addr:    g.config.Server.ListenAddr,
        Handler: handler,
    }

    // Create error channel for startup confirmation
    errChan := make(chan error, 1)

    // Start server in a goroutine
    go func() {
        defer close(errChan)
        if err := httpServer.ListenAndServe(); err != nil {
            if errors.Is(err, http.ErrServerClosed) {
                return
            }
            g.logger.Error(context.Background(), "HTTP server failed", zap.Error(err))
            errChan <- err
        }
    }()

    // Monitor context for cancellation
    go func() {
        <-ctx.Done()
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        if err := httpServer.Shutdown(shutdownCtx); err != nil {
            g.logger.Error(shutdownCtx, "Error shutting down HTTP server", zap.Error(err))
        }
    }()

    // Check for startup errors (with timeout)
    select {
    case err := <-errChan:
        if err != nil {
            return fmt.Errorf("HTTP server startup failed: %w", err)
        }
    case <-time.After(100 * time.Millisecond):
        // No immediate error, assume successful startup
    }

    g.logger.Info(ctx, "HTTP server started", zap.String("listen_addr", g.config.Server.ListenAddr))

    return nil
}
```

#### 3. Add tests: `internal/gateway/auth_middleware_test.go`

```go
package gateway

import (
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestAuthMiddleware_NoTokenConfigured(t *testing.T) {
    // When no token is configured, all requests should pass through
    handler := AuthMiddleware("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))

    req := httptest.NewRequest("GET", "/", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
    expectedToken := "test-secret-token"
    handler := AuthMiddleware(expectedToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))

    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Authorization", "Bearer test-secret-token")
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
    expectedToken := "test-secret-token"
    handler := AuthMiddleware(expectedToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))

    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Authorization", "Bearer wrong-token")
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
    expectedToken := "test-secret-token"
    handler := AuthMiddleware(expectedToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))

    req := httptest.NewRequest("GET", "/", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddleware_MalformedHeader(t *testing.T) {
    tests := []struct {
        name   string
        header string
    }{
        {"no bearer prefix", "test-secret-token"},
        {"basic auth", "Basic dXNlcjpwYXNz"},
        {"empty bearer", "Bearer "},
        {"bearer lowercase", "bearer test-secret-token"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            expectedToken := "test-secret-token"
            handler := AuthMiddleware(expectedToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(http.StatusOK)
            }))

            req := httptest.NewRequest("GET", "/", nil)
            req.Header.Set("Authorization", tt.header)
            rec := httptest.NewRecorder()

            handler.ServeHTTP(rec, req)

            assert.Equal(t, http.StatusUnauthorized, rec.Code)
        })
    }
}

func TestGetAuthToken(t *testing.T) {
    // Save original value
    original := os.Getenv(AuthTokenEnvVar)
    defer os.Setenv(AuthTokenEnvVar, original)

    t.Run("not set", func(t *testing.T) {
        os.Unsetenv(AuthTokenEnvVar)
        assert.Equal(t, "", GetAuthToken())
    })

    t.Run("set to value", func(t *testing.T) {
        os.Setenv(AuthTokenEnvVar, "my-secret")
        assert.Equal(t, "my-secret", GetAuthToken())
    })
}
```

### Client Configuration Example

For Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "maybe-dont": {
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "Bearer your-secret-token-here"
      }
    }
  }
}
```

### Behavior Summary

| Scenario | Result |
|----------|--------|
| `MAYBE_DONT_AUTH_TOKEN` not set | All requests pass through (no auth) |
| Token set, no `Authorization` header | 401 Unauthorized |
| Token set, wrong token | 401 Unauthorized |
| Token set, correct `Bearer <token>` | Request proceeds |
| Token set, non-Bearer format | 401 Unauthorized |

### Security Considerations

1. **Token in environment variable**: Follows 12-factor app principles; avoid logging the token value
2. **Constant-time comparison**: Consider using `subtle.ConstantTimeCompare` to prevent timing attacks (optional for internal use)
3. **HTTPS recommended**: In production, use TLS to protect the token in transit
4. **No token rotation**: This simple implementation doesn't support token rotation; restart required to change token

### Future Enhancements (Out of Scope)

- Config file support with `server.auth.token`
- Multiple valid tokens
- Token expiration
- SSE transport support (would need different auth mechanism)
- Rate limiting on failed auth attempts

## Testing Plan

1. Unit tests for `AuthMiddleware` function covering all scenarios
2. Integration test: start gateway with `MAYBE_DONT_AUTH_TOKEN` set, verify 401 without header
3. Integration test: verify 200 with correct token
4. Manual test with Claude Desktop configuration

## Files to Create/Modify

| File | Action |
|------|--------|
| `internal/gateway/auth_middleware.go` | Create |
| `internal/gateway/auth_middleware_test.go` | Create |
| `internal/gateway/server.go` | Modify `initHTTPServer` |
