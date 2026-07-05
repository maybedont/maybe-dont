package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// newTestKey generates an Ed25519 keypair for signing test tokens.
func newTestKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return priv
}

// signToken signs a JWT with the given claims using EdDSA and the provided key.
func signToken(t *testing.T, key ed25519.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := tok.SignedString(key)
	require.NoError(t, err)
	return signed
}

// TestValidateAccessToken verifies token validation across the key security dimensions:
// valid tokens are accepted and their claims extracted; expired, wrong-issuer,
// wrong-audience, and wrong-signature tokens are rejected.
func TestValidateAccessToken(t *testing.T) {
	const issuer = "https://idp.example"
	const audience = "https://maybedont.example/mcp"

	key := newTestKey(t)
	otherKey := newTestKey(t)
	validator := NewStaticValidator(key.Public(), issuer, audience)
	now := time.Now()

	tests := []struct {
		name      string
		token     string
		wantErr   bool
		wantSub   string
		wantEmail string
		wantScope []string
	}{
		{
			name: "valid token extracts identity",
			token: signToken(t, key, jwt.MapClaims{
				"iss": issuer, "aud": audience, "sub": "user-1",
				"email": "user@example.com", "scope": "read write",
				"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			}),
			wantSub: "user-1", wantEmail: "user@example.com", wantScope: []string{"read", "write"},
		},
		{
			name: "expired token rejected",
			token: signToken(t, key, jwt.MapClaims{
				"iss": issuer, "aud": audience, "sub": "user-1",
				"iat": now.Add(-2 * time.Hour).Unix(), "exp": now.Add(-time.Hour).Unix(),
			}),
			wantErr: true,
		},
		{
			name: "wrong issuer rejected",
			token: signToken(t, key, jwt.MapClaims{
				"iss": "https://evil.example", "aud": audience, "sub": "user-1",
				"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			}),
			wantErr: true,
		},
		{
			name: "wrong audience rejected",
			token: signToken(t, key, jwt.MapClaims{
				"iss": issuer, "aud": "https://other.example", "sub": "user-1",
				"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			}),
			wantErr: true,
		},
		{
			name: "wrong signing key rejected",
			token: signToken(t, otherKey, jwt.MapClaims{
				"iss": issuer, "aud": audience, "sub": "user-1",
				"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			}),
			wantErr: true,
		},
		{
			name: "missing sub rejected",
			token: signToken(t, key, jwt.MapClaims{
				"iss": issuer, "aud": audience,
				"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			}),
			wantErr: true,
		},
		{
			name:    "malformed token rejected",
			token:   "not-a-jwt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity, err := validator.ValidateAccessToken(context.Background(), tt.token)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantSub, identity.Subject)
			require.Equal(t, tt.wantEmail, identity.Email)
			require.Equal(t, tt.wantScope, identity.Scopes)
			require.Equal(t, tt.token, identity.RawToken)
		})
	}
}

// TestValidateAccessTokenRejectsHS256 verifies that symmetric (HS256) tokens are rejected
// even if an attacker knows a shared secret, because only asymmetric algorithms are allowed.
func TestValidateAccessTokenRejectsHS256(t *testing.T) {
	const issuer = "https://idp.example"
	const audience = "https://maybedont.example/mcp"
	key := newTestKey(t)
	validator := NewStaticValidator(key.Public(), issuer, audience)

	hsToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": issuer, "aud": audience, "sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := hsToken.SignedString([]byte("shared-secret"))
	require.NoError(t, err)

	_, err = validator.ValidateAccessToken(context.Background(), signed)
	require.Error(t, err, "HS256 tokens must be rejected by the asymmetric-only allowlist")
}
