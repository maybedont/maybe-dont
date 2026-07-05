package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/require"
)

// TestTokenBrokerTokenExchangeCaches verifies a token_exchange downstream gets a token and
// that a second request within the token lifetime is served from cache (no second exchange).
func TestTokenBrokerTokenExchangeCaches(t *testing.T) {
	var exchanges int32
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&exchanges, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "downstream-token", "token_type": "Bearer", "expires_in": 3600,
		})
	}))
	defer idp.Close()

	broker := NewTokenBroker(NewExchangeClient(idp.Client(), idp.URL, "gateway", "secret"), idp.Client())
	cfg := config.ClientAuthConfig{Type: config.AuthTypeTokenExchange, Scope: "read"}

	res, err := broker.TokenFor(context.Background(), "sess-1", "api", "https://api.example/mcp/", "subject-token", cfg)
	require.NoError(t, err)
	require.Equal(t, "downstream-token", res.AccessToken)
	require.Equal(t, "rfc8693", res.Flow)

	// Second call should hit the cache.
	res2, err := broker.TokenFor(context.Background(), "sess-1", "api", "https://api.example/mcp/", "subject-token", cfg)
	require.NoError(t, err)
	require.Equal(t, "downstream-token", res2.AccessToken)
	require.Equal(t, int32(1), atomic.LoadInt32(&exchanges), "second call should be served from cache")

	// Eviction forces a fresh exchange.
	broker.EvictSession("sess-1")
	_, err = broker.TokenFor(context.Background(), "sess-1", "api", "https://api.example/mcp/", "subject-token", cfg)
	require.NoError(t, err)
	require.Equal(t, int32(2), atomic.LoadInt32(&exchanges), "post-eviction call should re-exchange")
}

// TestTokenBrokerMissingSubject verifies exchange fails clearly when no caller identity is
// available.
func TestTokenBrokerMissingSubject(t *testing.T) {
	broker := NewTokenBroker(NewExchangeClient(http.DefaultClient, "https://idp.example/token", "g", "s"), http.DefaultClient)
	cfg := config.ClientAuthConfig{Type: config.AuthTypeTokenExchange}
	_, err := broker.TokenFor(context.Background(), "sess", "api", "https://api.example", "", cfg)
	require.Error(t, err)
}

// TestTokenBrokerEnterpriseManagedDiscovery verifies the full ID-JAG chain: resource
// metadata discovery, AS metadata validation (id-jag profile), ID-JAG exchange, and
// redemption at the downstream AS.
func TestTokenBrokerEnterpriseManagedDiscovery(t *testing.T) {
	// Downstream MCP server hosts protected resource metadata + AS metadata + token endpoint.
	var downstream *httptest.Server
	var idp *httptest.Server
	downstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(ProtectedResourceMetadata{
				Resource:             downstream.URL,
				AuthorizationServers: []string{downstream.URL},
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                 downstream.URL,
				"token_endpoint":                         downstream.URL + "/oauth2/token",
				"authorization_grant_profiles_supported": []string{GrantProfileIDJAG},
			})
		case "/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "downstream-access-token", "token_type": "Bearer", "expires_in": 3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer downstream.Close()

	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// IdP issues the ID-JAG.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issued_token_type": TokenTypeIDJAG, "access_token": "the-id-jag",
			"token_type": "N_A", "expires_in": 300,
		})
	}))
	defer idp.Close()

	broker := NewTokenBroker(NewExchangeClient(idp.Client(), idp.URL, "gateway", "secret"), downstream.Client())
	cfg := config.ClientAuthConfig{Type: config.AuthTypeEnterpriseManaged}

	res, err := broker.TokenFor(context.Background(), "sess-1", "asana", downstream.URL, "subject-token", cfg)
	require.NoError(t, err)
	require.Equal(t, "downstream-access-token", res.AccessToken)
	require.Equal(t, "id-jag", res.Flow)
}

// TestTokenBrokerEnterpriseManagedRejectsNonEMADownstream verifies discovery fails clearly
// when the downstream AS does not advertise the id-jag grant profile.
func TestTokenBrokerEnterpriseManagedRejectsNonEMADownstream(t *testing.T) {
	var downstream *httptest.Server
	downstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(ProtectedResourceMetadata{
				Resource: downstream.URL, AuthorizationServers: []string{downstream.URL},
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": downstream.URL, "token_endpoint": downstream.URL + "/oauth2/token",
				// No id-jag grant profile advertised.
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer downstream.Close()

	broker := NewTokenBroker(NewExchangeClient(http.DefaultClient, "https://idp/token", "g", "s"), downstream.Client())
	cfg := config.ClientAuthConfig{Type: config.AuthTypeEnterpriseManaged}
	_, err := broker.TokenFor(context.Background(), "s", "asana", downstream.URL, "subject", cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "id-jag grant profile")
}
