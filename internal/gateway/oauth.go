package gateway

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// TokenValidationError represents different types of token validation failures
type TokenValidationError struct {
	Type        string // missing_token, invalid_token, expired_token, insufficient_scope
	Description string
}

func (e *TokenValidationError) Error() string {
	return e.Description
}

// OAuthMetadata represents the OAuth 2.0 Protected Resource Metadata
// as defined in RFC 9728
type OAuthMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	ResourceDocumentation  string   `json:"resource_documentation,omitempty"`
}

// handleOAuthMetadata handles the /.well-known/oauth-protected-resource endpoint
// as required by RFC 9728 for OAuth 2.0 Protected Resource Metadata
func (g *Gateway) handleOAuthMetadata(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Construct the resource URL based on the request
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	// Use the Host header to construct the resource URL
	host := r.Host
	if host == "" {
		// Fallback to configured listen address if Host header is missing
		host = g.config.Server.ListenAddr
	}

	resourceURL := fmt.Sprintf("%s://%s", scheme, host)

	// Create the OAuth metadata response
	metadata := OAuthMetadata{
		AuthorizationServers: []string{g.config.Server.OAuth.AuthorizationServer},
		Resource:             resourceURL,
		// Set default bearer methods - header is the most common
		BearerMethodsSupported: []string{"header"},
		// Set basic MCP scopes
		ScopesSupported: []string{"mcp:read", "mcp:write"},
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Encode and send the response
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		g.logger.Error("Failed to encode OAuth metadata response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	g.logger.Debug("Served OAuth metadata",
		zap.String("resource", metadata.Resource),
		zap.String("client_ip", getClientIP(r)))
}

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}

	return r.RemoteAddr
}

// validateBearerToken validates a bearer token
// For now, this is a placeholder that can be extended with actual JWT validation
// or calls to an introspection endpoint
func (g *Gateway) validateBearerToken(token string) error {
	if token == "" {
		return &TokenValidationError{
			Type:        "missing_token",
			Description: "Bearer token is required",
		}
	}

	// TODO: Implement actual token validation logic
	// This could involve:
	// 1. JWT parsing and signature verification
	// 2. Token introspection endpoint calls
	// 3. Expiration checks
	// 4. Scope validation

	// For now, we'll accept any non-empty token as valid
	// This should be replaced with real validation logic
	if len(token) < 10 {
		return &TokenValidationError{
			Type:        "invalid_token",
			Description: "Invalid bearer token format",
		}
	}

	return nil
}

// extractBearerToken extracts the bearer token from the Authorization header
func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	// Check if the header starts with "Bearer "
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return ""
	}

	// Extract the token part (remove "Bearer " prefix)
	return strings.TrimSpace(authHeader[7:])
}

// buildWWWAuthenticateHeader builds the WWW-Authenticate header value
func (g *Gateway) buildWWWAuthenticateHeader(r *http.Request, errorType string) string {
	// Construct the resource metadata URL
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	host := r.Host
	if host == "" {
		host = g.config.Server.ListenAddr
	}

	metadataURL := fmt.Sprintf("%s://%s/.well-known/oauth-protected-resource", scheme, host)

	// Build the WWW-Authenticate header
	header := fmt.Sprintf(`Bearer realm="%s", resource_metadata="%s"`,
		g.config.Server.OAuth.Realm, metadataURL)

	if errorType != "" {
		header += fmt.Sprintf(`, error="%s"`, errorType)
	}

	return header
}

// oauthMiddleware is the OAuth token validation middleware
func (g *Gateway) oauthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip OAuth validation if not enabled
		if !g.config.Server.OAuth.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Extract the bearer token
		token := extractBearerToken(r)

		// Validate the token
		if err := g.validateBearerToken(token); err != nil {
			var tokenErr *TokenValidationError
			if tokenValidationErr, ok := err.(*TokenValidationError); ok {
				tokenErr = tokenValidationErr
			} else {
				// Default error type for unexpected validation errors
				tokenErr = &TokenValidationError{
					Type:        "invalid_token",
					Description: "Token validation failed",
				}
			}

			// Set WWW-Authenticate header
			w.Header().Set("WWW-Authenticate", g.buildWWWAuthenticateHeader(r, tokenErr.Type))
			w.Header().Set("Content-Type", "application/json")

			// Log the validation failure
			g.logger.Warn("OAuth token validation failed",
				zap.String("error_type", tokenErr.Type),
				zap.String("error_description", tokenErr.Description),
				zap.String("client_ip", getClientIP(r)),
				zap.String("user_agent", r.Header.Get("User-Agent")))

			// Return 401 Unauthorized
			http.Error(w, `{"error": "unauthorized", "error_description": "`+tokenErr.Description+`"}`,
				http.StatusUnauthorized)
			return
		}

		// Token is valid, continue to the next handler
		g.logger.Debug("OAuth token validation successful",
			zap.String("client_ip", getClientIP(r)))

		next.ServeHTTP(w, r)
	})
}

// wellKnownCORSMiddleware adds CORS headers to .well-known endpoints
func (g *Gateway) wellKnownCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.logger.Debug("CORS middleware processing request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("origin", r.Header.Get("Origin")),
			zap.String("user_agent", r.Header.Get("User-Agent")),
		)

		// Only apply CORS to .well-known paths
		if !strings.HasPrefix(r.URL.Path, "/.well-known/") {
			g.logger.Debug("CORS middleware skipping - not .well-known path",
				zap.String("path", r.URL.Path),
			)
			next.ServeHTTP(w, r)
			return
		}

		g.logger.Debug("CORS middleware processing .well-known path",
			zap.String("path", r.URL.Path),
		)

		// Check if CORS is enabled
		if !g.config.Server.OAuth.CORS.Enabled {
			g.logger.Debug("CORS middleware skipping - CORS disabled in config")
			next.ServeHTTP(w, r)
			return
		}

		g.logger.Debug("CORS is enabled in config",
			zap.Any("allowed_origins", g.config.Server.OAuth.CORS.AllowedOrigins),
			zap.Int("max_age", g.config.Server.OAuth.CORS.MaxAge),
		)

		origin := r.Header.Get("Origin")
		
		// Check if origin is allowed
		originAllowed := false
		matchedOrigin := ""
		for _, allowedOrigin := range g.config.Server.OAuth.CORS.AllowedOrigins {
			if allowedOrigin == "*" || allowedOrigin == origin {
				originAllowed = true
				matchedOrigin = allowedOrigin
				break
			}
		}

		g.logger.Debug("CORS origin check result",
			zap.String("request_origin", origin),
			zap.Bool("origin_allowed", originAllowed),
			zap.String("matched_origin", matchedOrigin),
		)

		// Handle preflight OPTIONS request
		if r.Method == http.MethodOptions {
			g.logger.Debug("Handling CORS preflight OPTIONS request",
				zap.String("origin", origin),
				zap.Bool("origin_allowed", originAllowed),
			)

			if originAllowed && origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, mcp-protocol-version")
				w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", g.config.Server.OAuth.CORS.MaxAge))
				
				g.logger.Debug("CORS preflight headers set",
					zap.String("Access-Control-Allow-Origin", origin),
					zap.String("Access-Control-Allow-Methods", "GET, OPTIONS"),
					zap.String("Access-Control-Allow-Headers", "Authorization, Content-Type, mcp-protocol-version"),
					zap.Int("Access-Control-Max-Age", g.config.Server.OAuth.CORS.MaxAge),
				)
			} else {
				g.logger.Debug("CORS preflight rejected - origin not allowed or empty",
					zap.String("origin", origin),
					zap.Bool("origin_allowed", originAllowed),
				)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Add CORS headers for actual requests
		if originAllowed && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, mcp-protocol-version")
			
			g.logger.Debug("CORS headers set for actual request",
				zap.String("Access-Control-Allow-Origin", origin),
				zap.String("Access-Control-Allow-Methods", "GET, OPTIONS"),
				zap.String("Access-Control-Allow-Headers", "Authorization, Content-Type, mcp-protocol-version"),
			)
		} else {
			g.logger.Debug("CORS headers not set for actual request",
				zap.String("origin", origin),
				zap.Bool("origin_allowed", originAllowed),
				zap.String("reason", func() string {
					if !originAllowed {
						return "origin not allowed"
					}
					if origin == "" {
						return "origin header empty"
					}
					return "unknown"
				}()),
			)
		}

		next.ServeHTTP(w, r)
	})
}
