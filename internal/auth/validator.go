package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// asymmetricAlgs is the allowlist of signature algorithms accepted for incoming tokens
// and ID-JAGs. Symmetric (HS*) and "none" are intentionally excluded: tokens are signed
// by a remote party whose public keys we fetch, so only asymmetric verification is valid.
var asymmetricAlgs = []string{
	"RS256", "RS384", "RS512",
	"PS256", "PS384", "PS512",
	"ES256", "ES384", "ES512",
	"EdDSA",
}

// clockSkew is the tolerance applied to time-based claims (exp/nbf/iat).
const clockSkew = 60 * time.Second

// Identity is the validated identity extracted from an incoming access token.
type Identity struct {
	Subject   string         // sub
	Email     string         // email (optional)
	ClientID  string         // client_id / azp (optional)
	Scopes    []string       // parsed `scope` claim
	Claims    map[string]any // full claim set (for CEL policy access)
	ExpiresAt time.Time
	// RawToken is retained only in memory for downstream token exchange. It must never
	// be logged or written to the audit log.
	RawToken string
}

// keyResolver abstracts signature-key lookup so the Validator can use either a remote
// JWKS (jwt_validation mode) or a local verification key (embedded_as mode).
type keyResolver interface {
	Keyfunc(token *jwt.Token) (any, error)
}

// staticKeyfunc adapts a single verification key (e.g. the embedded AS public key) to the
// keyResolver interface.
type staticKeyfunc struct {
	key any
}

func (s staticKeyfunc) Keyfunc(_ *jwt.Token) (any, error) { return s.key, nil }

// Validator validates incoming Bearer access tokens.
type Validator struct {
	resolver keyResolver
	issuer   string
	audience string
}

// NewJWKSValidator creates a Validator that verifies token signatures against a remote
// JWKS endpoint with automatic caching and key-rotation refresh.
func NewJWKSValidator(ctx context.Context, jwksURL, issuer, audience string) (*Validator, error) {
	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("initialize JWKS keyfunc for %s: %w", jwksURL, err)
	}
	return &Validator{resolver: kf, issuer: issuer, audience: audience}, nil
}

// NewStaticValidator creates a Validator that verifies token signatures against a single
// in-process verification key (used for embedded-AS self-issued tokens).
func NewStaticValidator(key any, issuer, audience string) *Validator {
	return &Validator{resolver: staticKeyfunc{key: key}, issuer: issuer, audience: audience}
}

// ValidateAccessToken parses and validates a raw access token, returning the extracted
// identity. Any validation failure returns an error (callers map this to a 401).
func (v *Validator) ValidateAccessToken(_ context.Context, raw string) (*Identity, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods(asymmetricAlgs),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockSkew),
	)

	claims := jwt.MapClaims{}
	if _, err := parser.ParseWithClaims(raw, claims, v.resolver.Keyfunc); err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	return identityFromClaims(claims, raw)
}

// identityFromClaims extracts an Identity from validated claims.
func identityFromClaims(claims jwt.MapClaims, raw string) (*Identity, error) {
	id := &Identity{
		Claims:   map[string]any(claims),
		RawToken: raw,
	}

	if sub, _ := claims["sub"].(string); sub != "" {
		id.Subject = sub
	} else {
		return nil, fmt.Errorf("token missing required sub claim")
	}
	if email, ok := claims["email"].(string); ok {
		id.Email = email
	}
	// client_id (RFC 8693 style) or azp (OIDC) may carry the client identifier.
	if cid, ok := claims["client_id"].(string); ok {
		id.ClientID = cid
	} else if azp, ok := claims["azp"].(string); ok {
		id.ClientID = azp
	}
	id.Scopes = parseScopes(claims["scope"])
	if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
		id.ExpiresAt = exp.Time
	}
	return id, nil
}

// parseScopes splits a space-separated `scope` claim into individual scope values.
func parseScopes(raw any) []string {
	s, ok := raw.(string)
	if !ok || s == "" {
		return nil
	}
	return strings.Fields(s)
}
