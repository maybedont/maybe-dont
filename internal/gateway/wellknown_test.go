package gateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maybedont/maybe-dont/internal/auth"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestEmbeddedASTokenEndpoint verifies the embedded authorization server accepts a valid
// ID-JAG via the jwt-bearer grant and issues an access token that validates against the
// gateway's own issuer.
func TestEmbeddedASTokenEndpoint(t *testing.T) {
	const idpIssuer = "https://idp.example"
	const asIssuer = "https://maybedont.example"
	const resource = "https://maybedont.example/mcp"

	// IdP signing key (signs the ID-JAG).
	_, idpKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	// Embedded AS signing key (signs issued access tokens).
	_, asKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	idjagValidator := auth.NewIDJAGValidatorFromKey(idpKey.Public(), idpIssuer, asIssuer, resource)
	issuer := auth.NewIssuer(asIssuer, asKey)

	g := &Gateway{
		logger: config.NewSessionLogger(zaptest.NewLogger(t)),
		authComponents: &authComponents{
			issuer:         issuer,
			idjagValidator: idjagValidator,
			resource:       resource,
			accessTokenTTL: time.Hour,
			embeddedAS:     true,
		},
	}

	// Mint a valid ID-JAG.
	now := time.Now()
	idjagTok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"iss": idpIssuer, "aud": asIssuer, "sub": "alice", "client_id": "vscode",
		"jti": "jti-1", "resource": resource, "scope": "read",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	})
	idjagTok.Header["typ"] = "oauth-id-jag+jwt"
	idjag, err := idjagTok.SignedString(idpKey)
	require.NoError(t, err)

	// POST the jwt-bearer grant.
	form := url.Values{}
	form.Set("grant_type", auth.GrantTypeJWTBearer)
	form.Set("assertion", idjag)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	g.handleTokenEndpoint(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var resp struct {
		TokenType   string `json:"token_type"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "Bearer", resp.TokenType)
	require.NotEmpty(t, resp.AccessToken)

	// The issued token must validate against the gateway's own issuer/audience.
	validator := auth.NewStaticValidator(issuer.PublicKey(), asIssuer, resource)
	identity, err := validator.ValidateAccessToken(context.Background(), resp.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "alice", identity.Subject)
}

// TestEmbeddedASTokenEndpointRejectsBadGrant verifies unsupported grant types are rejected.
func TestEmbeddedASTokenEndpointRejectsBadGrant(t *testing.T) {
	g := &Gateway{
		logger:         config.NewSessionLogger(zaptest.NewLogger(t)),
		authComponents: &authComponents{embeddedAS: true},
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	g.handleTokenEndpoint(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "unsupported_grant_type")
}
