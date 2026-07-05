package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ProtectedResourceMetadata is the RFC 9728 protected resource metadata document.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
}

// authorizationServerMetadataURL builds the RFC 8414 authorization server metadata URL
// for an issuer identifier.
func authorizationServerMetadataURL(issuer string) string {
	return strings.TrimRight(issuer, "/") + "/.well-known/oauth-authorization-server"
}

// fetchAuthorizationServerMetadata fetches RFC 8414 authorization server metadata for a
// downstream resource authorization server. Unlike the IdP discovery document, this only
// requires a token_endpoint (JWKS is not needed since the gateway does not validate tokens
// issued by the downstream AS).
func fetchAuthorizationServerMetadata(ctx context.Context, httpClient *http.Client, issuer string) (*DiscoveryDocument, error) {
	url := authorizationServerMetadataURL(issuer)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build AS metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch AS metadata from %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AS metadata fetch from %s returned status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read AS metadata body: %w", err)
	}

	var doc DiscoveryDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse AS metadata from %s: %w", url, err)
	}
	if doc.TokenEndpoint == "" {
		return nil, fmt.Errorf("AS metadata from %s missing required token_endpoint", url)
	}
	return &doc, nil
}

// fetchProtectedResourceMetadata fetches the RFC 9728 protected resource metadata for a
// resource identifier.
func fetchProtectedResourceMetadata(ctx context.Context, httpClient *http.Client, resource string) (*ProtectedResourceMetadata, error) {
	url := ResourceMetadataURL(resource)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build resource metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch resource metadata from %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resource metadata fetch from %s returned status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read resource metadata body: %w", err)
	}

	var meta ProtectedResourceMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("parse resource metadata from %s: %w", url, err)
	}
	return &meta, nil
}
