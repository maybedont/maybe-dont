package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maybedont/maybe-dont/internal/auth"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestExtractBearerToken verifies case-insensitive scheme matching and whitespace handling.
func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"standard", "Bearer abc123", "abc123"},
		{"lowercase scheme", "bearer abc123", "abc123"},
		{"mixed case", "BeArEr abc123", "abc123"},
		{"extra whitespace", "Bearer   abc123  ", "abc123"},
		{"missing scheme", "abc123", ""},
		{"empty", "", ""},
		{"basic auth", "Basic dXNlcg==", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, extractBearerToken(tt.header))
		})
	}
}

// newTestGatewayWithAuth builds a minimal Gateway whose auth components validate tokens
// signed by the returned key.
func newTestGatewayWithAuth(t *testing.T) (*Gateway, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	const issuer = "https://idp.example"
	const audience = "https://maybedont.example/mcp"
	g := &Gateway{
		logger: config.NewSessionLogger(zaptest.NewLogger(t)),
		authComponents: &authComponents{
			validator:           auth.NewStaticValidator(priv.Public(), issuer, audience),
			resourceMetadataURL: "https://maybedont.example/.well-known/oauth-protected-resource",
		},
	}
	return g, priv
}

// TestBearerAuthMiddleware verifies the middleware challenges unauthenticated requests and
// injects identity for valid tokens.
func TestBearerAuthMiddleware(t *testing.T) {
	g, key := newTestGatewayWithAuth(t)
	const issuer = "https://idp.example"
	const audience = "https://maybedont.example/mcp"

	var gotSubject string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := IdentityFromContext(r.Context()); ok {
			gotSubject = id.Subject
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := g.bearerAuthMiddleware(next)

	t.Run("missing token yields 401 challenge", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")
	})

	t.Run("invalid token yields invalid_token challenge", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer garbage")
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
	})

	t.Run("valid token injects identity", func(t *testing.T) {
		gotSubject = ""
		token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
			"iss": issuer, "aud": audience, "sub": "alice",
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		})
		signed, err := token.SignedString(key)
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+signed)
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "alice", gotSubject)
	})
}

// TestHandleProtectedResourceMetadata verifies the RFC 9728 endpoint returns the cached
// metadata JSON and rejects non-GET methods.
func TestHandleProtectedResourceMetadata(t *testing.T) {
	g := &Gateway{
		logger:         config.NewSessionLogger(zaptest.NewLogger(t)),
		authComponents: &authComponents{prmJSON: []byte(`{"resource":"https://maybedont.example/mcp"}`)},
	}

	rec := httptest.NewRecorder()
	g.handleProtectedResourceMetadata(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"resource":"https://maybedont.example/mcp"}`, rec.Body.String())

	rec2 := httptest.NewRecorder()
	g.handleProtectedResourceMetadata(rec2, httptest.NewRequest(http.MethodDelete, "/.well-known/oauth-protected-resource", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec2.Code)
}
