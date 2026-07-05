package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// idjagTyp is the required JWT `typ` header value for an Identity Assertion JWT
// Authorization Grant.
const idjagTyp = "oauth-id-jag+jwt"

// maxIDJAGAge bounds how old an ID-JAG's iat may be, limiting the replay window.
const maxIDJAGAge = 10 * time.Minute

// replayCacheCap bounds the replay cache to prevent unbounded memory growth. When full,
// new grants are rejected (fail closed) rather than silently skipping replay protection.
const replayCacheCap = 100_000

// IDJAGClaims holds the validated claims of an Identity Assertion JWT.
type IDJAGClaims struct {
	Subject  string
	Email    string
	ClientID string
	Resource string
	Scopes   []string
	JTI      string
	ExpireAt time.Time
}

// IDJAGValidator validates ID-JAGs presented to the embedded resource authorization
// server: signature against the IdP JWKS, required claims, audience binding, and single
// use (jti replay protection).
type IDJAGValidator struct {
	resolver keyResolver
	issuer   string // expected iss (the IdP)
	audience string // expected aud (this gateway's embedded AS issuer)
	resource string // expected resource claim (this gateway's resource id), if present
	replay   *replayCache
}

// NewIDJAGValidator creates an ID-JAG validator using the shared IdP JWKS keyfunc.
func NewIDJAGValidator(ctx context.Context, jwksURL, issuer, audience, resource string) (*IDJAGValidator, error) {
	v, err := NewJWKSValidator(ctx, jwksURL, issuer, audience)
	if err != nil {
		return nil, err
	}
	return &IDJAGValidator{
		resolver: v.resolver,
		issuer:   issuer,
		audience: audience,
		resource: resource,
		replay:   newReplayCache(),
	}, nil
}

// NewIDJAGValidatorFromKey creates an ID-JAG validator that verifies signatures against a
// fixed public key instead of a remote JWKS. Useful when the IdP signing key is
// provisioned out of band, and for testing.
func NewIDJAGValidatorFromKey(key any, issuer, audience, resource string) *IDJAGValidator {
	return &IDJAGValidator{
		resolver: staticKeyfunc{key: key},
		issuer:   issuer,
		audience: audience,
		resource: resource,
		replay:   newReplayCache(),
	}
}

// Validate verifies an ID-JAG assertion and returns its claims. The returned error is
// suitable for mapping to an OAuth invalid_grant response.
func (v *IDJAGValidator) Validate(_ context.Context, assertion string) (*IDJAGClaims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods(asymmetricAlgs),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockSkew),
	)

	claims := jwt.MapClaims{}
	token, err := parser.ParseWithClaims(assertion, claims, v.resolver.Keyfunc)
	if err != nil {
		return nil, fmt.Errorf("id-jag validation failed: %w", err)
	}

	// The typ header must identify this as an ID-JAG.
	if typ, _ := token.Header["typ"].(string); typ != idjagTyp {
		return nil, fmt.Errorf("id-jag has wrong typ header %q (expected %q)", typ, idjagTyp)
	}

	out := &IDJAGClaims{
		Email:  stringClaim(claims, "email"),
		Scopes: parseScopes(claims["scope"]),
	}
	if out.Subject = stringClaim(claims, "sub"); out.Subject == "" {
		return nil, fmt.Errorf("id-jag missing required sub claim")
	}
	if out.ClientID = stringClaim(claims, "client_id"); out.ClientID == "" {
		return nil, fmt.Errorf("id-jag missing required client_id claim")
	}
	if out.JTI = stringClaim(claims, "jti"); out.JTI == "" {
		return nil, fmt.Errorf("id-jag missing required jti claim")
	}
	out.Resource = stringClaim(claims, "resource")

	// iat must be present and recent to bound the replay window.
	iat, err := claims.GetIssuedAt()
	if err != nil || iat == nil {
		return nil, fmt.Errorf("id-jag missing required iat claim")
	}
	if time.Since(iat.Time) > maxIDJAGAge+clockSkew {
		return nil, fmt.Errorf("id-jag is too old (iat beyond max age)")
	}

	// If the resource claim is present it must match this gateway's resource identifier.
	if out.Resource != "" && v.resource != "" && out.Resource != v.resource {
		return nil, fmt.Errorf("id-jag resource claim %q does not match this resource server", out.Resource)
	}

	exp, _ := claims.GetExpirationTime()
	if exp == nil {
		return nil, fmt.Errorf("id-jag missing required exp claim")
	}
	out.ExpireAt = exp.Time

	// Replay protection: reject a jti we have already seen (fail closed on overflow).
	if !v.replay.storeIfAbsent(out.JTI, out.ExpireAt) {
		return nil, fmt.Errorf("id-jag jti has already been used (replay detected or cache full)")
	}

	return out, nil
}

func stringClaim(claims jwt.MapClaims, key string) string {
	s, _ := claims[key].(string)
	return s
}

// replayCache is an in-memory, capacity-bounded set of seen jti values with per-entry
// expiry. It is not shared across replicas; multi-replica deployments require sticky
// routing or an external cache.
type replayCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
	lastGC  time.Time
}

func newReplayCache() *replayCache {
	return &replayCache{entries: make(map[string]time.Time), lastGC: time.Now()}
}

// storeIfAbsent records jti with its expiry, returning false if it was already present
// or if the cache is full (fail closed).
func (r *replayCache) storeIfAbsent(jti string, exp time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.gcLocked()

	if _, seen := r.entries[jti]; seen {
		return false
	}
	if len(r.entries) >= replayCacheCap {
		return false
	}
	r.entries[jti] = exp
	return true
}

// gcLocked sweeps expired entries at most once per minute. Caller must hold the lock.
func (r *replayCache) gcLocked() {
	now := time.Now()
	if now.Sub(r.lastGC) < time.Minute {
		return
	}
	for jti, exp := range r.entries {
		if now.After(exp) {
			delete(r.entries, jti)
		}
	}
	r.lastGC = now
}
