# Gateway Authentication Header

## Overview

Add a simple header-based authentication mechanism to the MCP gateway for internal use. When enabled via environment variable, all HTTP-based requests must include a valid header value to access the gateway. The header value serves as a caller identifier for audit logging, not a secret token.

## Requirements

1. **Optional enforcement**: If `MAYBE_DONT_REQUIRED_HEADER_NAME` or `MAYBE_DONT_REQUIRED_HEADER_VALUE` is not set, no authentication is required
2. **Early rejection**: Validate the header as early as possible in the HTTP request lifecycle, before any MCP processing
3. **HTTP 401 response**: Return `401 Unauthorized` when authentication fails
4. **HTTP-based transports**: Authentication applies to HTTP and SSE transports (whichever is active based on `server.type`)
5. **Helpful 404 responses**: Return informative 404 for requests to inactive transport endpoints
6. **Caller logging**: Log the caller identifier at session initialization for audit correlation

## Design Decisions

### Header Configuration

| Setting | Environment Variable | Default |
|---------|---------------------|---------|
| Header name | `MAYBE_DONT_REQUIRED_HEADER_NAME` | (none - auth disabled) |
| Allowed values | `MAYBE_DONT_REQUIRED_HEADER_VALUE` | (none - auth disabled) |

Both environment variables must be set to enable authentication.

**Example configuration:**
```bash
export MAYBE_DONT_REQUIRED_HEADER_NAME="X-MaybeDont-Caller"
export MAYBE_DONT_REQUIRED_HEADER_VALUE="*@maybedont.ai,service-account-1"
```

**Rationale for custom header approach:**
- Avoids collision with `Authorization` header used by pass-through authentication
- Pass-through configs often forward `Authorization` to downstream servers
- Custom header keeps gateway auth separate from downstream auth
- Caller identifier for audit, not a secret token

### Value Matching

`MAYBE_DONT_REQUIRED_HEADER_VALUE` accepts a comma-separated list of allowed values. Each value can be:

1. **Glob pattern** (contains `*`): Wildcard matching where `*` matches one or more characters
   - `*@maybedont.ai` matches `dan@maybedont.ai`, `service@maybedont.ai`
   - `*@maybedont.ai` does NOT match `@maybedont.ai` (requires at least one character)

2. **Exact match** (no `*`): Case-sensitive exact string comparison
   - `service-account-1` matches only `service-account-1`

**Matching logic**: Header value is valid if it matches ANY entry in the comma-separated list.

**Pattern validation** (at startup):
- Glob patterns must contain at least one non-`*` character (e.g., `*` alone is invalid)
- Glob patterns must compile to valid regex
- Invalid patterns cause startup failure (fail fast)

### Header Name Matching

- **Case-insensitive**: Per HTTP specification (RFC 7230), header names are case-insensitive

### Startup Logging

When authentication is enabled, log at server startup:
```
INFO  Required header authentication enabled  header=X-MaybeDont-Caller  allowed_values=[*@maybedont.ai, service-account-1]
```

This confirms the configuration is active and shows both the required header name and the allowed values/patterns. Logging the allowed values helps verify glob patterns are configured correctly.

### Caller Logging

When authentication is enabled, the caller identifier is logged at session initialization:
```
INFO  Session initialized  session_id=abc123  caller=dan@maybedont.ai
```

This enables correlation between MCP sessions and callers for audit purposes.

### Transport Scope

Transports are mutually exclusive via the `server.type` configuration field. Authentication applies to whichever HTTP-based transport is active.

| Transport | Protected | Notes |
|-----------|-----------|-------|
| HTTP (`type: http`) | Yes | Streamable HTTP transport |
| SSE (`type: sse`) | Yes | Server-Sent Events transport |
| STDIO (`type: stdio`) | No | No HTTP layer, not applicable |

**Inactive endpoint handling**: When a transport is not active, requests to its endpoint return a helpful 404:
- `type: http` → requests to `/sse` return `404 Not Found: SSE transport not enabled. Server configured for HTTP transport.`
- `type: sse` → requests to `/mcp` return `404 Not Found: HTTP transport not enabled. Server configured for SSE transport.`

### Value Handling

- Empty or whitespace-only `MAYBE_DONT_REQUIRED_HEADER_NAME` = auth disabled
- Empty or whitespace-only `MAYBE_DONT_REQUIRED_HEADER_VALUE` = auth disabled
- Missing header when auth enabled = 401
- Header value doesn't match any allowed pattern/value = 401
- 401 responses include descriptive error message (but do not reveal allowed values)

## Implementation Approach

Use standard Go HTTP middleware wrapping the mcp-go servers (`StreamableHTTPServer` or `SSEServer`).

**Key implementation points (addressing review comments):**
1. Create `http.ServeMux`, mount appropriate MCP server at its endpoint (`/mcp` for HTTP, `/sse` for SSE)
2. Add 404 handler for inactive transport endpoint with helpful message
3. Wrap mux with auth middleware
4. Use `WithStreamableHTTPServer` or `WithSSEServer` option to inject custom server while preserving `Start()` flow
5. Ensures TLS handling and shutdown behavior are preserved

## Code Changes

### 1. New file: `internal/gateway/auth_middleware.go`

```go
package gateway

import (
    "fmt"
    "net/http"
    "os"
    "regexp"
    "strings"
)

const (
    // RequiredHeaderValueEnvVar is the environment variable for allowed header values
    RequiredHeaderValueEnvVar = "MAYBE_DONT_REQUIRED_HEADER_VALUE"
    // RequiredHeaderNameEnvVar is the environment variable for the header name
    RequiredHeaderNameEnvVar = "MAYBE_DONT_REQUIRED_HEADER_NAME"
)

// AllowedValue represents a single allowed header value (exact match or compiled glob)
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

// Matches checks if the given value matches this allowed value
func (av AllowedValue) Matches(value string) bool {
    if av.IsGlob {
        return av.Regex.MatchString(value)
    }
    return value == av.Original
}

// AuthMiddleware returns HTTP middleware that validates the required header.
// If no allowed values are configured, the middleware passes all requests through.
// This middleware only validates - caller extraction happens in HTTPContextFunc.
func AuthMiddleware(config *CallerAuthConfig, next http.Handler) http.Handler {
    if config == nil || !config.Enabled {
        return next
    }

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// MatchesAny checks if the value matches any allowed value
func (c *CallerAuthConfig) MatchesAny(value string) bool {
    for _, av := range c.AllowedValues {
        if av.Matches(value) {
            return true
        }
    }
    return false
}

// OriginalValues returns the original pattern strings for logging
func (c *CallerAuthConfig) OriginalValues() []string {
    values := make([]string, len(c.AllowedValues))
    for i, av := range c.AllowedValues {
        values[i] = av.Original
    }
    return values
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
```

### 2. Modify `internal/gateway/gateway.go` - Add auth config to Gateway struct

```go
type Gateway struct {
    // ... existing fields ...
    callerAuthConfig *CallerAuthConfig // Loaded once at startup
}

// NewGateway creates a new Gateway instance
func NewGateway(cfg *config.Config, logger *Logger) (*Gateway, error) {
    // ... existing initialization ...

    // Load and validate caller auth config (fail fast on invalid patterns)
    authConfig, err := LoadCallerAuthConfig()
    if err != nil {
        return nil, fmt.Errorf("invalid caller auth configuration: %w", err)
    }

    return &Gateway{
        // ... existing fields ...
        callerAuthConfig: authConfig,
    }, nil
}
```

### 3. Modify `internal/gateway/server.go` - `initHTTPServer`

```go
func (g *Gateway) initHTTPServer(ctx context.Context) error {
    srv, err := g.initMCPServer()
    if err != nil {
        return fmt.Errorf("failed to initialize MCP server: %w", err)
    }

    // Create streamable HTTP server with auth extraction context function
    httpSrv := server.NewStreamableHTTPServer(srv,
        server.WithEndpointPath("/mcp"),
        server.WithHTTPContextFunc(g.extractAuthFromRequest),
    )

    g.server = srv

    // Log if auth is enabled (config already validated at Gateway creation)
    if g.callerAuthConfig.Enabled {
        g.logger.Info(ctx, "Required header authentication enabled",
            zap.String("header", g.callerAuthConfig.HeaderName),
            zap.Strings("allowed_values", g.callerAuthConfig.OriginalValues()))
    }

    // Create mux and mount the MCP handler
    mux := http.NewServeMux()
    mux.Handle("/mcp", httpSrv)

    // Add helpful 404 for SSE endpoint when HTTP transport is active
    mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "Not Found: SSE transport not enabled. Server configured for HTTP transport.", http.StatusNotFound)
    })

    // Wrap with auth middleware (uses pre-validated config from Gateway)
    handler := AuthMiddleware(g.callerAuthConfig, mux)

    // Create custom HTTP server
    httpServer := &http.Server{
        Addr:    g.config.Server.ListenAddr,
        Handler: handler,
    }

    // Use WithStreamableHTTPServer to preserve internal behavior
    // while using our custom server with auth middleware
    // ... (rest of startup/shutdown logic preserved)
}
```

### 4. Modify `internal/gateway/server.go` - `initSSEServer`

```go
func (g *Gateway) initSSEServer(ctx context.Context) error {
    srv, err := g.initMCPServer()
    if err != nil {
        return fmt.Errorf("failed to initialize MCP server: %w", err)
    }

    // Create SSE server with context function for caller extraction
    // Note: SSE uses WithSSEContextFunc (same signature as WithHTTPContextFunc)
    sseSrv := server.NewSSEServer(srv,
        server.WithSSEEndpoint("/sse"),
        server.WithMessageEndpoint("/message"),
        server.WithSSEContextFunc(g.extractAuthFromRequest),
    )

    g.server = srv

    // Log if auth is enabled (config already validated at Gateway creation)
    if g.callerAuthConfig.Enabled {
        g.logger.Info(ctx, "Required header authentication enabled",
            zap.String("header", g.callerAuthConfig.HeaderName),
            zap.Strings("allowed_values", g.callerAuthConfig.OriginalValues()))
    }

    // Create mux and mount the SSE handler
    mux := http.NewServeMux()
    mux.Handle("/sse", sseSrv)
    mux.Handle("/message", sseSrv)

    // Add helpful 404 for HTTP endpoint when SSE transport is active
    mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "Not Found: HTTP transport not enabled. Server configured for SSE transport.", http.StatusNotFound)
    })

    // Wrap with auth middleware (uses pre-validated config from Gateway)
    handler := AuthMiddleware(g.callerAuthConfig, mux)

    // Create custom HTTP server
    httpServer := &http.Server{
        Addr:    g.config.Server.ListenAddr,
        Handler: handler,
    }

    // Use WithSSEServer to preserve internal behavior
    // while using our custom server with auth middleware
    // ... (rest of startup/shutdown logic preserved)
}
```

### 5. Modify `internal/gateway/server.go` - `extractAuthFromRequest`

The caller extraction happens in `HTTPContextFunc`/`SSEContextFunc` to avoid context layering issues
(the middleware validates, this function populates context):

```go
// extractAuthFromRequest extracts authentication info from HTTP request headers
// and adds caller identifier to context for session logging.
// Used by both HTTP (WithHTTPContextFunc) and SSE (WithSSEContextFunc) transports.
func (g *Gateway) extractAuthFromRequest(ctx context.Context, r *http.Request) context.Context {
    // ... existing pass-through auth extraction ...

    // Extract caller identifier if auth is enabled
    // (middleware already validated the value, we just need to store it)
    if g.callerAuthConfig != nil && g.callerAuthConfig.Enabled {
        caller := r.Header.Get(g.callerAuthConfig.HeaderName)
        if caller != "" {
            ctx = context.WithValue(ctx, callerContextKey, caller)
        }
    }

    return ctx
}
```

### 6. Modify `internal/gateway/session.go` - Log caller at session init

```go
func (g *Gateway) initSession(ctx context.Context, sessionID string) (*Session, error) {
    // ... existing session initialization ...

    // Log caller identifier if available (set by extractAuthFromRequest)
    if caller := ctx.Value(callerContextKey); caller != nil {
        g.logger.Info(ctx, "Session initialized",
            zap.String("session_id", sessionID),
            zap.String("caller", caller.(string)))
    }

    // ... rest of session initialization ...
}
```

### 7. Tests: `internal/gateway/auth_middleware_test.go`

```go
func TestAuthMiddleware_NotConfigured(t *testing.T) {
    // When no values configured, all requests pass through
}

func TestAuthMiddleware_ExactMatch(t *testing.T) {
    // Exact value matches pass, non-matches return 401
}

func TestAuthMiddleware_GlobPattern(t *testing.T) {
    // "*@maybedont.ai" matches "dan@maybedont.ai"
    // "*@maybedont.ai" does NOT match "@maybedont.ai" (requires 1+ char)
}

func TestAuthMiddleware_MultipleValues(t *testing.T) {
    // Comma-separated: "*@maybedont.ai,service-account-1"
    // Matches glob OR exact value
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
    // Missing header returns 401
}

func TestAuthMiddleware_CaseInsensitiveHeaderName(t *testing.T) {
    // Header name matching is case-insensitive per HTTP spec
}

func TestExtractAuthFromRequest_CallerInContext(t *testing.T) {
    // extractAuthFromRequest stores caller in context when auth enabled
}

func TestAllowedValue_Matches(t *testing.T) {
    // Table-driven tests for AllowedValue.Matches():
    // - "*@maybedont.ai" matches "dan@maybedont.ai" ✓
    // - "*@maybedont.ai" matches "x@maybedont.ai" ✓
    // - "*@maybedont.ai" does NOT match "@maybedont.ai" ✗
    // - "*@maybedont.ai" does NOT match "dan@other.com" ✗
    // - "prefix-*" matches "prefix-anything" ✓
    // - "*-suffix" matches "anything-suffix" ✓
    // - Exact match "foo" matches "foo" ✓
    // - Exact match "foo" does NOT match "bar" ✗
}

func TestLoadCallerAuthConfig_ParsesCommaSeparated(t *testing.T) {
    // "a,b,c" → ["a", "b", "c"]
    // " a , b , c " → ["a", "b", "c"] (trimmed)
    // "a,,b" → ["a", "b"] (empty entries ignored)
}

func TestLoadCallerAuthConfig_DisabledWhenNameNotSet(t *testing.T) {
    // Auth disabled when MAYBE_DONT_REQUIRED_HEADER_NAME not set
}

func TestLoadCallerAuthConfig_DisabledWhenValueNotSet(t *testing.T) {
    // Auth disabled when MAYBE_DONT_REQUIRED_HEADER_VALUE not set
}

func TestLoadCallerAuthConfig_FailsOnInvalidGlobPattern(t *testing.T) {
    // "*" alone returns error (must have non-'*' character)
    // "**" returns error
    // "***@foo" is valid (multiple * allowed if has other chars)
}

func TestLoadCallerAuthConfig_PreCompilesGlobPatterns(t *testing.T) {
    // Verify AllowedValue.Regex is populated for glob patterns
    // Verify AllowedValue.Regex is nil for exact matches
}

func TestInactiveTransportEndpoint_HTTP(t *testing.T) {
    // When type=http, /sse returns 404 with helpful message
}

func TestInactiveTransportEndpoint_SSE(t *testing.T) {
    // When type=sse, /mcp returns 404 with helpful message
}
```

## Client Configuration Example

**Gateway configuration:**
```bash
export MAYBE_DONT_REQUIRED_HEADER_NAME="X-MaybeDont-Caller"
export MAYBE_DONT_REQUIRED_HEADER_VALUE="*@maybedont.ai,service-account-1"
```

**Claude Desktop (`claude_desktop_config.json`):**
```json
{
  "mcpServers": {
    "maybe-dont": {
      "url": "http://localhost:8080/mcp",
      "headers": {
        "X-MaybeDont-Caller": "dan@maybedont.ai"
      }
    }
  }
}
```

**With custom header name:**
```bash
export MAYBE_DONT_REQUIRED_HEADER_NAME="X-Custom-Caller"
export MAYBE_DONT_REQUIRED_HEADER_VALUE="*@mycompany.com"
```

## Behavior Summary

| Scenario | Result |
|----------|--------|
| `MAYBE_DONT_REQUIRED_HEADER_NAME` not set | All requests pass through (no auth) |
| `MAYBE_DONT_REQUIRED_HEADER_VALUE` not set | All requests pass through (no auth) |
| Auth enabled, header missing | 401 Unauthorized |
| Auth enabled, value doesn't match any pattern | 401 Unauthorized |
| Auth enabled, value matches exact entry | Request proceeds, caller logged |
| Auth enabled, value matches glob pattern | Request proceeds, caller logged |
| Header name varies in case | Works (case-insensitive) |
| Request to `/sse` when `type: http` | 404 with message indicating HTTP transport active |
| Request to `/mcp` when `type: sse` | 404 with message indicating SSE transport active |

## Security Considerations

1. **Caller identifier, not secret**: The header value is a caller identifier for audit purposes, not a secret token. It is intentionally logged.
2. **Pattern in environment variable**: Follows 12-factor app principles; allowed patterns configured via env var
3. **Custom header**: Avoids interference with pass-through `Authorization` headers
4. **HTTPS recommended**: In production, use TLS to protect header values in transit
5. **Glob validation**: The `*` wildcard requires at least one character match, preventing empty prefix attacks

## Future Enhancements (Out of Scope)

- Config file support for allowed values
- Caller identifier expiration/rotation
- Per-caller rate limiting

## Files to Create/Modify

| File | Action |
|------|--------|
| `internal/gateway/auth_middleware.go` | Create (middleware, config loading, glob matching) |
| `internal/gateway/auth_middleware_test.go` | Create |
| `internal/gateway/gateway.go` | Modify to add `callerAuthConfig` field and load at startup |
| `internal/gateway/server.go` | Modify `initHTTPServer`, `initSSEServer`, and `extractAuthFromRequest` |
| `internal/gateway/session.go` | Modify to log caller at session init |
