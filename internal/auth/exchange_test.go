package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExchangeAccessToken verifies the RFC 8693 token-exchange request is well-formed and
// the response is parsed.
func TestExchangeAccessToken(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		gotForm = r.PostForm
		user, pass, ok := r.BasicAuth()
		require.True(t, ok, "client auth expected")
		require.Equal(t, "gateway", user)
		require.Equal(t, "secret", pass)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "exchanged-token", "token_type": "Bearer",
			"expires_in": 3600, "scope": "read",
		})
	}))
	defer srv.Close()

	client := NewExchangeClient(srv.Client(), srv.URL, "gateway", "secret")
	resp, err := client.ExchangeAccessToken(context.Background(), "subject-token", "https://api.example", "read write")
	require.NoError(t, err)
	require.Equal(t, "exchanged-token", resp.AccessToken)
	require.Equal(t, 3600, resp.ExpiresIn)

	require.Equal(t, GrantTypeTokenExchange, gotForm.Get("grant_type"))
	require.Equal(t, "subject-token", gotForm.Get("subject_token"))
	require.Equal(t, TokenTypeAccessToken, gotForm.Get("subject_token_type"))
	require.Equal(t, "https://api.example", gotForm.Get("audience"))
	require.Equal(t, "read write", gotForm.Get("scope"))
}

// TestExchangeForIDJAG verifies the ID-JAG request carries the id-jag requested token type
// and the target resource authorization server as audience.
func TestExchangeForIDJAG(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issued_token_type": TokenTypeIDJAG, "access_token": "the-id-jag",
			"token_type": "N_A", "expires_in": 300,
		})
	}))
	defer srv.Close()

	client := NewExchangeClient(srv.Client(), srv.URL, "gateway", "secret")
	resp, err := client.ExchangeForIDJAG(context.Background(), "subject", "https://as.downstream", "https://mcp.downstream", "tasks.read")
	require.NoError(t, err)
	require.Equal(t, "the-id-jag", resp.AccessToken)

	require.Equal(t, GrantTypeTokenExchange, gotForm.Get("grant_type"))
	require.Equal(t, TokenTypeIDJAG, gotForm.Get("requested_token_type"))
	require.Equal(t, "https://as.downstream", gotForm.Get("audience"))
	require.Equal(t, "https://mcp.downstream", gotForm.Get("resource"))
}

// TestExchangeOAuthErrorPassthrough verifies structured OAuth errors are surfaced.
func TestExchangeOAuthErrorPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid_grant", "error_description": "token expired",
		})
	}))
	defer srv.Close()

	client := NewExchangeClient(srv.Client(), srv.URL, "gateway", "secret")
	_, err := client.ExchangeAccessToken(context.Background(), "subject", "aud", "")
	require.Error(t, err)

	var oerr *OAuthError
	require.True(t, errors.As(err, &oerr), "expected an OAuthError")
	require.Equal(t, "invalid_grant", oerr.Code)
	require.Equal(t, "token expired", oerr.Description)
}
