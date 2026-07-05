package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// LoadOrCreateSigningKey loads an Ed25519 signing key from a PEM file, generating and
// persisting a new one (0600) if the file does not exist. Ed25519 is used because the
// embedded AS's tokens are only validated by this same gateway, so there is no need for
// JWKS publication or third-party algorithm compatibility.
func LoadOrCreateSigningKey(path string) (ed25519.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("signing key %s is not valid PEM", path)
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse signing key %s: %w", path, err)
		}
		edKey, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("signing key %s is not an Ed25519 key", path)
		}
		return edKey, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read signing key %s: %w", path, err)
	}

	// Generate a new key and persist it with restrictive permissions.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal signing key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write signing key %s: %w", path, err)
	}
	return priv, nil
}

// IssueParams holds the inputs for minting an embedded-AS access token.
type IssueParams struct {
	Subject  string
	Email    string
	ClientID string
	Audience string
	Scopes   []string
	TTL      time.Duration
}

// Issuer mints short-lived access tokens signed with the embedded AS signing key.
type Issuer struct {
	signingKey ed25519.PrivateKey
	issuer     string
}

// NewIssuer creates an Issuer for the given issuer identifier and signing key.
func NewIssuer(issuer string, signingKey ed25519.PrivateKey) *Issuer {
	return &Issuer{signingKey: signingKey, issuer: issuer}
}

// PublicKey returns the verification key corresponding to the signing key.
func (i *Issuer) PublicKey() ed25519.PublicKey {
	return i.signingKey.Public().(ed25519.PublicKey)
}

// Issue mints and signs an access token from the given parameters.
func (i *Issuer) Issue(p IssueParams) (string, int, error) {
	now := time.Now()
	expiresIn := int(p.TTL.Seconds())
	claims := jwt.MapClaims{
		"iss": i.issuer,
		"sub": p.Subject,
		"aud": p.Audience,
		"iat": now.Unix(),
		"exp": now.Add(p.TTL).Unix(),
		"jti": uuid.NewString(),
	}
	if p.Email != "" {
		claims["email"] = p.Email
	}
	if p.ClientID != "" {
		claims["client_id"] = p.ClientID
	}
	if len(p.Scopes) > 0 {
		claims["scope"] = joinScopes(p.Scopes)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(i.signingKey)
	if err != nil {
		return "", 0, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresIn, nil
}

// joinScopes joins scope values with a single space (OAuth scope encoding).
func joinScopes(scopes []string) string {
	out := ""
	for idx, s := range scopes {
		if idx > 0 {
			out += " "
		}
		out += s
	}
	return out
}
