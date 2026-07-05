// Package auth implements OAuth/JWT primitives for Enterprise-Managed Authorization (EMA):
// incoming Bearer token validation (resource-server role), RFC 8693 / RFC 7523 token
// exchange for downstream on-behalf-of calls, and an optional embedded resource
// authorization server that issues access tokens from Identity Assertion JWT grants.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/maybedont/maybe-dont/internal/config"
)

// DiscoveryDocument represents the subset of OAuth/OIDC discovery metadata the gateway
// consumes (RFC 8414 authorization server metadata / OIDC discovery).
type DiscoveryDocument struct {
	Issuer                              string   `json:"issuer"`
	TokenEndpoint                       string   `json:"token_endpoint"`
	JWKSURI                             string   `json:"jwks_uri"`
	AuthorizationGrantProfilesSupported []string `json:"authorization_grant_profiles_supported"`
}

// FetchDiscovery fetches and minimally validates a discovery document. It fails fast
// (no silent defaults) when required fields are missing.
func FetchDiscovery(ctx context.Context, httpClient *http.Client, discoveryURL string) (*DiscoveryDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery document from %s: %w", discoveryURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery fetch from %s returned status %d", discoveryURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read discovery body: %w", err)
	}

	var doc DiscoveryDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse discovery document from %s: %w", discoveryURL, err)
	}
	if doc.Issuer == "" || doc.JWKSURI == "" {
		return nil, fmt.Errorf("discovery document from %s missing required issuer or jwks_uri", discoveryURL)
	}
	return &doc, nil
}

// IdPEndpoints holds the resolved endpoints for the enterprise Identity Provider.
type IdPEndpoints struct {
	Issuer        string
	JWKSURI       string
	TokenEndpoint string
}

// ResolveIdP resolves the IdP endpoints at startup. Explicit config overrides
// (jwks_url, token_endpoint) take precedence over discovery-derived values. It fails
// fast if discovery is unreachable and no explicit overrides fill the gap.
func ResolveIdP(ctx context.Context, httpClient *http.Client, cfg config.IdPConfig) (*IdPEndpoints, error) {
	endpoints := &IdPEndpoints{
		Issuer:        cfg.Issuer,
		JWKSURI:       cfg.JWKSURL,
		TokenEndpoint: cfg.TokenEndpoint,
	}

	// Fetch discovery to fill any endpoints not explicitly overridden.
	if cfg.OpenIDConnectDiscoveryURL != "" && (endpoints.JWKSURI == "" || endpoints.TokenEndpoint == "") {
		doc, err := FetchDiscovery(ctx, httpClient, cfg.OpenIDConnectDiscoveryURL)
		if err != nil {
			return nil, fmt.Errorf("resolve IdP endpoints: %w", err)
		}
		if endpoints.JWKSURI == "" {
			endpoints.JWKSURI = doc.JWKSURI
		}
		if endpoints.TokenEndpoint == "" {
			endpoints.TokenEndpoint = doc.TokenEndpoint
		}
		if endpoints.Issuer == "" {
			endpoints.Issuer = doc.Issuer
		}
	}

	if endpoints.Issuer == "" {
		return nil, fmt.Errorf("resolve IdP endpoints: issuer is not configured")
	}
	if endpoints.JWKSURI == "" {
		return nil, fmt.Errorf("resolve IdP endpoints: jwks_uri could not be determined (set idp.jwks_url or idp.openid_connect_discovery_url)")
	}
	return endpoints, nil
}

// ResourceMetadataURL builds the RFC 9728 protected-resource-metadata URL for a
// resource identifier, inserting any path component after the well-known segment per
// RFC 9728 section 3.1.
func ResourceMetadataURL(resource string) string {
	origin := config.OriginOf(resource)
	path := strings.TrimPrefix(resource, origin)
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return origin + "/.well-known/oauth-protected-resource"
	}
	return origin + "/.well-known/oauth-protected-resource" + path
}
