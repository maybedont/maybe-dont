package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// OAuth grant types and token type identifiers used by token exchange (RFC 8693) and
// the JWT bearer grant (RFC 7523).
const (
	GrantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	GrantTypeJWTBearer     = "urn:ietf:params:oauth:grant-type:jwt-bearer"

	TokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token"
	TokenTypeIDJAG       = "urn:ietf:params:oauth:token-type:id-jag"

	// GrantProfileIDJAG is advertised in authorization server metadata to signal support
	// for the Identity Assertion JWT Authorization Grant profile.
	GrantProfileIDJAG = "urn:ietf:params:oauth:grant-profile:id-jag"
)

// TokenResponse is the parsed OAuth token endpoint response.
type TokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	Scope           string `json:"scope"`
}

// OAuthError represents a standard OAuth 2.0 error response (RFC 6749 §5.2).
type OAuthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
	StatusCode  int    `json:"-"`
}

func (e *OAuthError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("oauth error %q: %s", e.Code, e.Description)
	}
	return fmt.Sprintf("oauth error %q", e.Code)
}

// ExchangeClient performs token exchange against the IdP and JWT-bearer grants against
// downstream resource authorization servers. It authenticates as a confidential client
// using the configured client_id/client_secret.
type ExchangeClient struct {
	httpClient    *http.Client
	tokenEndpoint string // IdP token endpoint
	clientID      string
	clientSecret  string
}

// NewExchangeClient creates an ExchangeClient bound to the IdP token endpoint.
func NewExchangeClient(httpClient *http.Client, tokenEndpoint, clientID, clientSecret string) *ExchangeClient {
	return &ExchangeClient{
		httpClient:    httpClient,
		tokenEndpoint: tokenEndpoint,
		clientID:      clientID,
		clientSecret:  clientSecret,
	}
}

// ExchangeAccessToken performs a plain RFC 8693 on-behalf-of exchange at the IdP,
// returning an access token audience-restricted to the given audience.
func (c *ExchangeClient) ExchangeAccessToken(ctx context.Context, subjectToken, audience, scope string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", GrantTypeTokenExchange)
	form.Set("subject_token", subjectToken)
	form.Set("subject_token_type", TokenTypeAccessToken)
	form.Set("audience", audience)
	if scope != "" {
		form.Set("scope", scope)
	}
	return c.postForm(ctx, c.tokenEndpoint, form, true)
}

// ExchangeForIDJAG requests an Identity Assertion JWT Authorization Grant (ID-JAG) from
// the IdP, targeting a downstream resource authorization server.
func (c *ExchangeClient) ExchangeForIDJAG(ctx context.Context, subjectToken, resourceAS, resource, scope string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", GrantTypeTokenExchange)
	form.Set("requested_token_type", TokenTypeIDJAG)
	form.Set("audience", resourceAS)
	if resource != "" {
		form.Set("resource", resource)
	}
	form.Set("subject_token", subjectToken)
	form.Set("subject_token_type", TokenTypeAccessToken)
	if scope != "" {
		form.Set("scope", scope)
	}
	return c.postForm(ctx, c.tokenEndpoint, form, true)
}

// RedeemIDJAG presents an ID-JAG to a downstream resource authorization server's token
// endpoint via the JWT bearer grant, returning the downstream access token.
func (c *ExchangeClient) RedeemIDJAG(ctx context.Context, tokenEndpoint, idjag string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", GrantTypeJWTBearer)
	form.Set("assertion", idjag)
	form.Set("client_id", c.clientID)
	// The downstream AS authenticates the client; include credentials when available.
	return c.postForm(ctx, tokenEndpoint, form, c.clientSecret != "")
}

// postForm posts a form-encoded token request and parses the response. When withClientAuth
// is true, the gateway's confidential-client credentials are sent via HTTP Basic auth.
func (c *ExchangeClient) postForm(ctx context.Context, endpoint string, form url.Values, withClientAuth bool) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if withClientAuth && c.clientID != "" {
		req.SetBasicAuth(url.QueryEscape(c.clientID), url.QueryEscape(c.clientSecret))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request to %s failed: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Attempt to parse a structured OAuth error; fall back to a generic error.
		var oerr OAuthError
		if json.Unmarshal(body, &oerr) == nil && oerr.Code != "" {
			oerr.StatusCode = resp.StatusCode
			return nil, &oerr
		}
		return nil, fmt.Errorf("token request to %s returned status %d", endpoint, resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token response from %s missing access_token", endpoint)
	}
	return &tokenResp, nil
}
