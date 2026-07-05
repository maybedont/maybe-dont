package auth

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// newTestIDJAGValidator builds an IDJAGValidator that verifies against a fixed public key,
// bypassing the remote JWKS fetch for unit testing.
func newTestIDJAGValidator(pub ed25519.PublicKey, issuer, audience, resource string) *IDJAGValidator {
	return &IDJAGValidator{
		resolver: staticKeyfunc{key: pub},
		issuer:   issuer,
		audience: audience,
		resource: resource,
		replay:   newReplayCache(),
	}
}

// signIDJAG signs an ID-JAG with the required typ header.
func signIDJAG(t *testing.T, key ed25519.PrivateKey, typ string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	if typ != "" {
		tok.Header["typ"] = typ
	}
	signed, err := tok.SignedString(key)
	require.NoError(t, err)
	return signed
}

// TestIDJAGValidate covers the ID-JAG validation rules: correct typ header, required
// claims, audience/resource binding, freshness, and replay protection.
func TestIDJAGValidate(t *testing.T) {
	const issuer = "https://idp.example"
	const asAudience = "https://maybedont.example"
	const resource = "https://maybedont.example/mcp"

	key := newTestKey(t)
	now := time.Now()

	base := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": issuer, "aud": asAudience, "sub": "user-1",
			"client_id": "client-a", "jti": "jti-" + time.Now().Format("150405.000000000"),
			"resource": resource, "scope": "read",
			"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		}
	}

	tests := []struct {
		name    string
		typ     string
		mutate  func(jwt.MapClaims)
		wantErr bool
	}{
		{name: "valid id-jag", typ: idjagTyp},
		{name: "wrong typ header", typ: "jwt", wantErr: true},
		{name: "missing typ header", typ: "", wantErr: true},
		{name: "missing client_id", typ: idjagTyp, mutate: func(c jwt.MapClaims) { delete(c, "client_id") }, wantErr: true},
		{name: "missing jti", typ: idjagTyp, mutate: func(c jwt.MapClaims) { delete(c, "jti") }, wantErr: true},
		{name: "wrong audience", typ: idjagTyp, mutate: func(c jwt.MapClaims) { c["aud"] = "https://other" }, wantErr: true},
		{name: "resource mismatch", typ: idjagTyp, mutate: func(c jwt.MapClaims) { c["resource"] = "https://other/mcp" }, wantErr: true},
		{name: "stale iat", typ: idjagTyp, mutate: func(c jwt.MapClaims) { c["iat"] = now.Add(-time.Hour).Unix() }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestIDJAGValidator(key.Public().(ed25519.PublicKey), issuer, asAudience, resource)
			claims := base()
			if tt.mutate != nil {
				tt.mutate(claims)
			}
			assertion := signIDJAG(t, key, tt.typ, claims)

			grant, err := v.Validate(context.Background(), assertion)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "user-1", grant.Subject)
			require.Equal(t, "client-a", grant.ClientID)
			require.Equal(t, []string{"read"}, grant.Scopes)
		})
	}
}

// TestIDJAGReplayRejected verifies that a second presentation of the same jti is rejected.
func TestIDJAGReplayRejected(t *testing.T) {
	const issuer = "https://idp.example"
	const asAudience = "https://maybedont.example"
	key := newTestKey(t)
	v := newTestIDJAGValidator(key.Public().(ed25519.PublicKey), issuer, asAudience, "")
	now := time.Now()

	assertion := signIDJAG(t, key, idjagTyp, jwt.MapClaims{
		"iss": issuer, "aud": asAudience, "sub": "user-1", "client_id": "client-a",
		"jti": "unique-jti-123", "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	})

	_, err := v.Validate(context.Background(), assertion)
	require.NoError(t, err, "first use should succeed")

	_, err = v.Validate(context.Background(), assertion)
	require.Error(t, err, "replayed jti must be rejected")
}
