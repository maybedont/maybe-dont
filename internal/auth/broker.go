package auth

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
)

// tokenExpiryMargin is subtracted from a token's lifetime so the gateway refreshes
// slightly before actual expiry.
const tokenExpiryMargin = 30 * time.Second

// downstreamASTTL is how long resolved downstream authorization server metadata is cached.
const downstreamASTTL = time.Hour

// DownstreamASInfo holds resolved metadata for a downstream resource authorization server.
type DownstreamASInfo struct {
	Issuer        string
	TokenEndpoint string
	resolvedAt    time.Time
}

// TokenBroker acquires and caches downstream access tokens on behalf of the authenticated
// user. Tokens are cached per (session, client) and evicted when the session ends.
type TokenBroker struct {
	exchange   *ExchangeClient
	httpClient *http.Client

	mu           sync.Mutex
	downstreamAS map[string]*DownstreamASInfo // clientName -> resolved AS metadata
	tokens       map[tokenKey]*cachedToken    // {session,client} -> token
}

type tokenKey struct {
	sessionID  string
	clientName string
}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

// TokenResult carries the outcome of a token acquisition for audit enrichment.
type TokenResult struct {
	AccessToken     string
	Flow            string // "rfc8693" or "id-jag"
	Audience        string
	ScopesRequested string
	ScopesGranted   string
}

// NewTokenBroker creates a token broker using the given exchange client.
func NewTokenBroker(exchange *ExchangeClient, httpClient *http.Client) *TokenBroker {
	return &TokenBroker{
		exchange:     exchange,
		httpClient:   httpClient,
		downstreamAS: make(map[string]*DownstreamASInfo),
		tokens:       make(map[tokenKey]*cachedToken),
	}
}

// TokenFor returns a downstream access token for the given session and client, acquiring
// (and caching) one via token exchange if necessary. subjectToken is the caller's
// validated gateway access token.
func (b *TokenBroker) TokenFor(ctx context.Context, sessionID, clientName, downstreamURL, subjectToken string, cfg config.ClientAuthConfig) (*TokenResult, error) {
	key := tokenKey{sessionID: sessionID, clientName: clientName}

	b.mu.Lock()
	if t, ok := b.tokens[key]; ok && time.Now().Before(t.expiresAt) {
		value := t.value
		b.mu.Unlock()
		return &TokenResult{AccessToken: value, Flow: flowFor(cfg)}, nil
	}
	b.mu.Unlock()

	if subjectToken == "" {
		return nil, fmt.Errorf("no upstream identity available for token exchange (client %q)", clientName)
	}

	var (
		resp   *TokenResponse
		result *TokenResult
		err    error
	)
	switch cfg.EffectiveType() {
	case config.AuthTypeTokenExchange:
		resp, result, err = b.doTokenExchange(ctx, downstreamURL, subjectToken, cfg)
	case config.AuthTypeEnterpriseManaged:
		resp, result, err = b.doEnterpriseManaged(ctx, clientName, downstreamURL, subjectToken, cfg)
	default:
		return nil, fmt.Errorf("client %q does not use a token-exchange auth type", clientName)
	}
	if err != nil {
		return nil, err
	}

	b.cacheToken(key, resp)
	return result, nil
}

// doTokenExchange performs a plain RFC 8693 exchange.
func (b *TokenBroker) doTokenExchange(ctx context.Context, downstreamURL, subjectToken string, cfg config.ClientAuthConfig) (*TokenResponse, *TokenResult, error) {
	audience := cfg.Audience
	if audience == "" {
		audience = config.OriginOf(downstreamURL)
	}
	resp, err := b.exchange.ExchangeAccessToken(ctx, subjectToken, audience, cfg.Scope)
	if err != nil {
		return nil, nil, fmt.Errorf("token exchange failed: %w", err)
	}
	return resp, &TokenResult{
		AccessToken:     resp.AccessToken,
		Flow:            "rfc8693",
		Audience:        audience,
		ScopesRequested: cfg.Scope,
		ScopesGranted:   resp.Scope,
	}, nil
}

// doEnterpriseManaged performs the full ID-JAG -> JWT-bearer chain against the
// downstream's resource authorization server.
func (b *TokenBroker) doEnterpriseManaged(ctx context.Context, clientName, downstreamURL, subjectToken string, cfg config.ClientAuthConfig) (*TokenResponse, *TokenResult, error) {
	as, err := b.discoverDownstreamAS(ctx, clientName, downstreamURL, cfg)
	if err != nil {
		return nil, nil, err
	}
	resource := cfg.Resource
	if resource == "" {
		resource = config.OriginOf(downstreamURL)
	}
	idjag, err := b.exchange.ExchangeForIDJAG(ctx, subjectToken, as.Issuer, resource, cfg.Scope)
	if err != nil {
		return nil, nil, fmt.Errorf("id-jag exchange failed: %w", err)
	}
	resp, err := b.exchange.RedeemIDJAG(ctx, as.TokenEndpoint, idjag.AccessToken)
	if err != nil {
		return nil, nil, fmt.Errorf("id-jag redemption at downstream AS failed: %w", err)
	}
	return resp, &TokenResult{
		AccessToken:     resp.AccessToken,
		Flow:            "id-jag",
		Audience:        as.Issuer,
		ScopesRequested: cfg.Scope,
		ScopesGranted:   resp.Scope,
	}, nil
}

// cacheToken records a token for a session/client with an expiry derived from expires_in.
func (b *TokenBroker) cacheToken(key tokenKey, resp *TokenResponse) {
	ttl := time.Duration(resp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute // conservative default when the AS omits expires_in
	}
	b.mu.Lock()
	b.tokens[key] = &cachedToken{
		value:     resp.AccessToken,
		expiresAt: time.Now().Add(ttl - tokenExpiryMargin),
	}
	b.mu.Unlock()
}

// discoverDownstreamAS resolves and caches a downstream server's resource authorization
// server, verifying it advertises support for the ID-JAG grant profile.
func (b *TokenBroker) discoverDownstreamAS(ctx context.Context, clientName, downstreamURL string, cfg config.ClientAuthConfig) (*DownstreamASInfo, error) {
	b.mu.Lock()
	if info, ok := b.downstreamAS[clientName]; ok && time.Since(info.resolvedAt) < downstreamASTTL {
		b.mu.Unlock()
		return info, nil
	}
	b.mu.Unlock()

	resource := cfg.Resource
	if resource == "" {
		resource = config.OriginOf(downstreamURL)
	}

	// 1. Fetch RFC 9728 protected resource metadata to find the authorization server.
	prm, err := fetchProtectedResourceMetadata(ctx, b.httpClient, resource)
	if err != nil {
		return nil, fmt.Errorf("discover downstream %q resource metadata: %w", clientName, err)
	}
	if len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("downstream %q protected resource metadata lists no authorization_servers", clientName)
	}
	asIssuer := prm.AuthorizationServers[0]

	// 2. Fetch the authorization server metadata and verify id-jag support.
	asMeta, err := fetchAuthorizationServerMetadata(ctx, b.httpClient, asIssuer)
	if err != nil {
		return nil, fmt.Errorf("discover downstream %q authorization server metadata: %w", clientName, err)
	}
	if !containsString(asMeta.AuthorizationGrantProfilesSupported, GrantProfileIDJAG) {
		return nil, fmt.Errorf("downstream %q authorization server %q does not advertise support for the id-jag grant profile", clientName, asIssuer)
	}

	info := &DownstreamASInfo{
		Issuer:        asMeta.Issuer,
		TokenEndpoint: asMeta.TokenEndpoint,
		resolvedAt:    time.Now(),
	}
	b.mu.Lock()
	b.downstreamAS[clientName] = info
	b.mu.Unlock()
	return info, nil
}

// EvictSession removes all cached tokens for a session (called on session teardown).
func (b *TokenBroker) EvictSession(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k := range b.tokens {
		if k.sessionID == sessionID {
			delete(b.tokens, k)
		}
	}
}

func flowFor(cfg config.ClientAuthConfig) string {
	if cfg.EffectiveType() == config.AuthTypeEnterpriseManaged {
		return "id-jag"
	}
	return "rfc8693"
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
