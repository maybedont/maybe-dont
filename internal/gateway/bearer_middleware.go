package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/maybedont/maybe-dont/internal/auth"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// authComponents holds the runtime pieces for Enterprise-Managed Authorization. It is
// nil when authentication is disabled and no downstream requires token exchange.
type authComponents struct {
	// validator validates incoming Bearer access tokens (nil when auth is disabled).
	validator *auth.Validator
	// broker performs downstream on-behalf-of token exchange (nil when unused).
	broker *auth.TokenBroker
	// issuer mints embedded-AS access tokens (embedded_as mode only).
	issuer *auth.Issuer
	// idjagValidator validates ID-JAGs at the embedded AS token endpoint (embedded_as mode only).
	idjagValidator *auth.IDJAGValidator

	resourceMetadataURL  string // full URL advertised in WWW-Authenticate challenges
	resourceMetadataPath string // path the PRM handler is served at (matches the URL above)
	prmJSON              []byte // cached RFC 9728 protected resource metadata
	asMetaJSON           []byte // cached RFC 8414 authorization server metadata (embedded_as)

	resource         string
	scopesSupported  []string
	allowedClientIDs map[string]bool
	accessTokenTTL   time.Duration
	idpClientID      string
	embeddedAS       bool
}

// authHTTPClient is the HTTP client used for discovery and token exchange.
func authHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// initAuth wires up the Enterprise-Managed Authorization components based on config.
// It performs network I/O (IdP discovery, JWKS setup) and fails fast on error, consistent
// with the gateway's "failed initialization aborts startup" policy.
func (g *Gateway) initAuth(ctx context.Context, logDir string) error {
	cfg := g.config

	downstreamNeedsExchange := false
	for _, client := range cfg.DownstreamMCPServers {
		if client.Auth.IsTokenExchange() {
			downstreamNeedsExchange = true
			break
		}
	}

	if !cfg.AuthEnabled() && !downstreamNeedsExchange {
		return nil // Nothing to set up
	}

	httpClient := authHTTPClient()
	ac := &authComponents{
		resource:        cfg.Server.Auth.Resource,
		scopesSupported: cfg.Server.Auth.ScopesSupported,
		idpClientID:     cfg.IdP.ClientID,
	}

	// Resolve IdP endpoints (issuer, jwks_uri, token_endpoint) from discovery/overrides.
	endpoints, err := auth.ResolveIdP(ctx, httpClient, cfg.IdP)
	if err != nil {
		return fmt.Errorf("resolve identity provider: %w", err)
	}

	if cfg.AuthEnabled() {
		ac.resourceMetadataURL = auth.ResourceMetadataURL(cfg.Server.Auth.Resource)
		// Serve the PRM handler at the exact path of the advertised URL so that clients
		// following the WWW-Authenticate challenge reach it (RFC 9728 path insertion).
		if u, perr := url.Parse(ac.resourceMetadataURL); perr == nil && u.Path != "" {
			ac.resourceMetadataPath = u.Path
		} else {
			ac.resourceMetadataPath = "/.well-known/oauth-protected-resource"
		}

		switch cfg.Server.Auth.Mode {
		case config.AuthModeJWTValidation:
			ac.validator, err = auth.NewJWKSValidator(ctx, endpoints.JWKSURI, endpoints.Issuer, cfg.Server.Auth.Audience)
			if err != nil {
				return fmt.Errorf("initialize token validator: %w", err)
			}
			ac.prmJSON, err = buildProtectedResourceMetadata(cfg, cfg.Server.Auth.AuthorizationServers)
			if err != nil {
				return err
			}

		case config.AuthModeEmbeddedAS:
			ac.embeddedAS = true
			embedded := cfg.Server.Auth.EmbeddedAS

			keyPath := embedded.SigningKeyFile
			if keyPath == "" {
				keyPath = filepath.Join(logDir, "as-signing-key.pem")
			}
			signingKey, err := auth.LoadOrCreateSigningKey(keyPath)
			if err != nil {
				return fmt.Errorf("load embedded AS signing key: %w", err)
			}
			ac.issuer = auth.NewIssuer(embedded.Issuer, signingKey)
			ac.accessTokenTTL = time.Duration(embedded.AccessTokenTTLSeconds) * time.Second

			// The gateway validates its own self-issued tokens with the local public key.
			ac.validator = auth.NewStaticValidator(ac.issuer.PublicKey(), embedded.Issuer, cfg.Server.Auth.Audience)

			// ID-JAGs are validated against the IdP's JWKS; aud must be the embedded AS issuer.
			ac.idjagValidator, err = auth.NewIDJAGValidator(ctx, endpoints.JWKSURI, endpoints.Issuer, embedded.Issuer, cfg.Server.Auth.Resource)
			if err != nil {
				return fmt.Errorf("initialize id-jag validator: %w", err)
			}

			if len(embedded.AllowedClientIDs) > 0 {
				ac.allowedClientIDs = make(map[string]bool, len(embedded.AllowedClientIDs))
				for _, id := range embedded.AllowedClientIDs {
					ac.allowedClientIDs[id] = true
				}
			}

			ac.prmJSON, err = buildProtectedResourceMetadata(cfg, []string{embedded.Issuer})
			if err != nil {
				return err
			}
			ac.asMetaJSON, err = buildAuthorizationServerMetadata(embedded.Issuer)
			if err != nil {
				return err
			}
		}
	}

	if downstreamNeedsExchange {
		if endpoints.TokenEndpoint == "" {
			return fmt.Errorf("downstream token exchange requires an IdP token endpoint (set idp.token_endpoint or idp.openid_connect_discovery_url)")
		}
		exchange := auth.NewExchangeClient(httpClient, endpoints.TokenEndpoint, cfg.IdP.ClientID, cfg.IdP.ClientSecret)
		ac.broker = auth.NewTokenBroker(exchange, httpClient)
	}

	g.authComponents = ac
	g.logger.Info(ctx, "Enterprise-Managed Authorization initialized",
		zap.String("mode", cfg.Server.Auth.Mode),
		zap.Bool("downstream_token_exchange", downstreamNeedsExchange),
		zap.String("issuer", endpoints.Issuer))
	return nil
}

// buildProtectedResourceMetadata builds the cached RFC 9728 metadata JSON.
func buildProtectedResourceMetadata(cfg *config.Config, authServers []string) ([]byte, error) {
	prm := auth.ProtectedResourceMetadata{
		Resource:               cfg.Server.Auth.Resource,
		AuthorizationServers:   authServers,
		ScopesSupported:        cfg.Server.Auth.ScopesSupported,
		BearerMethodsSupported: []string{"header"},
	}
	data, err := json.Marshal(prm)
	if err != nil {
		return nil, fmt.Errorf("marshal protected resource metadata: %w", err)
	}
	return data, nil
}

// authorizationServerMetadata is the subset of RFC 8414 metadata the embedded AS advertises.
type authorizationServerMetadata struct {
	Issuer                              string   `json:"issuer"`
	TokenEndpoint                       string   `json:"token_endpoint"`
	GrantTypesSupported                 []string `json:"grant_types_supported"`
	AuthorizationGrantProfilesSupported []string `json:"authorization_grant_profiles_supported"`
	TokenEndpointAuthMethodsSupported   []string `json:"token_endpoint_auth_methods_supported"`
}

// buildAuthorizationServerMetadata builds the cached RFC 8414 metadata JSON for the
// embedded resource authorization server.
func buildAuthorizationServerMetadata(issuer string) ([]byte, error) {
	meta := authorizationServerMetadata{
		Issuer:                              issuer,
		TokenEndpoint:                       strings.TrimRight(issuer, "/") + "/oauth2/token",
		GrantTypesSupported:                 []string{auth.GrantTypeJWTBearer},
		AuthorizationGrantProfilesSupported: []string{auth.GrantProfileIDJAG},
		TokenEndpointAuthMethodsSupported:   []string{"client_secret_basic", "none"},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal authorization server metadata: %w", err)
	}
	return data, nil
}

// bearerAuthMiddleware validates the incoming Bearer token and injects the resulting
// identity into the request context. It fails closed: any missing/invalid token yields a
// 401 with an RFC 6750 WWW-Authenticate challenge pointing at the resource metadata.
func (g *Gateway) bearerAuthMiddleware(next http.Handler) http.Handler {
	ac := g.authComponents
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := extractBearerToken(r.Header.Get("Authorization"))
		if raw == "" {
			g.writeBearerChallenge(w, "")
			return
		}

		identity, err := ac.validator.ValidateAccessToken(r.Context(), raw)
		if err != nil {
			// Never log the token itself.
			g.logger.Debug(r.Context(), "Bearer token validation failed", zap.Error(err))
			g.writeBearerChallenge(w, "invalid_token")
			return
		}

		ctx := WithIdentity(r.Context(), identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeBearerChallenge writes a 401 response with an RFC 6750 WWW-Authenticate header.
func (g *Gateway) writeBearerChallenge(w http.ResponseWriter, errorCode string) {
	var b strings.Builder
	b.WriteString("Bearer")
	if errorCode != "" {
		b.WriteString(fmt.Sprintf(` error=%q`, errorCode))
	}
	if g.authComponents != nil && g.authComponents.resourceMetadataURL != "" {
		if errorCode != "" {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf(` resource_metadata=%q`, g.authComponents.resourceMetadataURL))
	}
	w.Header().Set("WWW-Authenticate", b.String())
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

// extractBearerToken extracts the token from an Authorization header value, matching the
// "Bearer " scheme case-insensitively.
func extractBearerToken(header string) string {
	const prefix = "bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// isWellKnownOrTokenPath reports whether a request path is an unauthenticated OAuth
// discovery or token endpoint that must bypass Bearer/header auth.
func isWellKnownOrTokenPath(path string) bool {
	return strings.HasPrefix(path, "/.well-known/") || path == "/oauth2/token"
}

// bearerEnabled reports whether incoming Bearer validation is active.
func (g *Gateway) bearerEnabled() bool {
	return g.authComponents != nil && g.authComponents.validator != nil
}

// wrapWithBearer wraps an MCP handler with Bearer validation when auth is enabled.
func (g *Gateway) wrapWithBearer(next http.Handler) http.Handler {
	if g.bearerEnabled() {
		return g.bearerAuthMiddleware(next)
	}
	return next
}

// registerAuthRoutes registers the unauthenticated OAuth discovery and token endpoints on
// the mux when the corresponding auth mode is active.
func (g *Gateway) registerAuthRoutes(ctx context.Context, mux *http.ServeMux) {
	ac := g.authComponents
	if ac == nil {
		return
	}
	if len(ac.prmJSON) > 0 {
		path := ac.resourceMetadataPath
		if path == "" {
			path = "/.well-known/oauth-protected-resource"
		}
		mux.HandleFunc(path, g.handleProtectedResourceMetadata)
		g.logger.Info(ctx, "Protected resource metadata endpoint registered", zap.String("path", path))
	}
	if ac.embeddedAS {
		mux.HandleFunc("/.well-known/oauth-authorization-server", g.handleAuthorizationServerMetadata)
		mux.HandleFunc("/oauth2/token", g.handleTokenEndpoint)
		g.logger.Info(ctx, "Embedded authorization server endpoints registered")
	}
}
