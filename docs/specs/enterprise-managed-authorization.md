# Enterprise-Managed Authorization (EMA) for Maybe Don't Gateway

## Status

Draft — see [README.md](README.md)

> **Relationship to existing work**: This spec absorbs and extends
> `docs/plans/2026-02-20-obo-token-exchange-spec.md` (Draft). That document describes plain
> RFC 8693 OBO token exchange. Enterprise-Managed Authorization (EMA) is the MCP-standardized
> formalization of the same problem, adding the Identity Assertion JWT Authorization Grant
> (ID-JAG), RFC 9728 discovery, and RFC 7523 JWT-bearer token acquisition. Where the two
> documents overlap (the `idp:` config section, JWKS validation middleware, 401 + protected
> resource metadata, audit enrichment), this spec is authoritative; the OBO doc's
> `auth.type: token_exchange` downstream option is retained here unchanged as one of the
> supported downstream auth types.

## 1. Overview

### 1.1 What the standard is

Enterprise-Managed Authorization (MCP extension, SEP-990,
[spec](https://github.com/modelcontextprotocol/ext-auth/blob/main/specification/stable/enterprise-managed-authorization.mdx))
moves MCP authorization decisions into the enterprise Identity Provider (IdP). Instead of every
user clicking through per-server OAuth consent, the flow is:

1. **SSO**: user signs in to the MCP client (Claude, VS Code, …) via the enterprise IdP
   (OIDC or SAML) and the client holds an ID Token (or refresh token for SAML).
2. **Token exchange (RFC 8693)**: the client asks the IdP to exchange that identity assertion
   for an **ID-JAG** (Identity Assertion JWT Authorization Grant,
   `requested_token_type=urn:ietf:params:oauth:token-type:id-jag`,
   `audience=<resource authorization server>`). The IdP evaluates admin policy
   ("may this user use this client with this MCP server, with these scopes?") and issues a
   short-lived signed JWT (`typ: oauth-id-jag+jwt`) with claims
   `iss/sub/aud/resource/client_id/jti/exp/iat/scope[/email]`.
3. **JWT-bearer grant (RFC 7523)**: the client presents the ID-JAG at the MCP server's
   **Resource Authorization Server** token endpoint
   (`grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer&assertion=<id-jag>`). The AS
   validates the ID-JAG against the IdP's JWKS (signature, `aud` = its own issuer, `exp`,
   `iat`, `jti` replay) and issues an access token audience-restricted to the MCP server.
4. **Resource access**: the client calls the MCP server with `Authorization: Bearer <token>`.

Discovery is standard OAuth plumbing:

- The MCP server advertises its Resource Authorization Server via **RFC 9728 Protected
  Resource Metadata** (`/.well-known/oauth-protected-resource`), referenced from the
  `WWW-Authenticate: Bearer resource_metadata="…"` header on 401 responses.
- The Resource Authorization Server advertises EMA support via
  `"authorization_grant_profiles_supported": ["urn:ietf:params:oauth:grant-profile:id-jag"]`
  in its RFC 8414 authorization server metadata.

### 1.2 Where Maybe Don't fits

The gateway sits in the middle, so it plays **both** EMA roles:

```
                    upstream (gateway = MCP Server / Resource Server)
MCP Client ──Bearer token──▶ Maybe Don't ──Bearer token──▶ Downstream MCP servers
                                   │        downstream (gateway = MCP Client)
                                   ▼
                             Enterprise IdP
                    (ID-JAG issuance, JWKS, token exchange)
```

- **Upstream role (Resource Server)**: the gateway must challenge unauthenticated requests
  with `WWW-Authenticate` + protected resource metadata, and validate Bearer access tokens on
  `/mcp`. The validated identity feeds the **policy engine** (CEL `auth` variable, AI prompt
  context) and the **audit log** — this is the gateway's core value-add: identity-aware
  allow/deny decisions with a per-user audit trail.
- **Resource Authorization Server**: someone must issue the gateway's access tokens from
  ID-JAGs. Two deployment modes (both supported, see §3): *external* (the enterprise IdP or a
  corporate AS is the gateway's AS; gateway only validates JWTs) and *embedded* (the gateway
  hosts a minimal token endpoint itself).
- **Downstream role (MCP Client on behalf of the user)**: for downstream MCP servers that
  support EMA, the gateway exchanges the caller's token at the IdP for an ID-JAG targeting the
  downstream's Resource AS, then performs the JWT-bearer grant there, caches the resulting
  access token per session, and attaches it to downstream calls. For non-EMA downstreams the
  existing plain RFC 8693 exchange (`token_exchange`) and header pass-through remain available.

### 1.3 Goals

- Gateway acts as a spec-compliant EMA Resource Server (401 challenge, RFC 9728 metadata,
  Bearer validation).
- Validated user identity (`sub`, `email`, scopes, full claims) exposed to the CEL policy
  engine (`auth` variable), optionally to AI validation, and written to the audit log.
- Identity bound to MCP sessions; per-session downstream credentials.
- Downstream auth types: `pass_through` (existing), `token_exchange` (plain RFC 8693, per the
  OBO draft), `enterprise_managed` (full ID-JAG → JWT-bearer chain with RFC 9728/8414
  discovery).
- Optional embedded Resource Authorization Server so the gateway is EMA-capable without any
  corporate AS in front of it.
- Fail-closed authentication (unlike the validation blocking budget, which fails open).

### 1.4 Non-goals

- Acting as an IdP (no SSO, no user database, no ID token issuance).
- OAuth for the REST endpoints (`/api/v1/cli/validate`, `/api/v1/action/validate`,
  `/api/v1/intercept`) — they keep the existing `X-Maybe-Dont-Client-ID` +
  caller-header-auth scheme. Future work.
- Dynamic client registration (RFC 7591) and MCP's classic user-interactive OAuth flow. The
  gateway targets enterprise deployments where clients are pre-registered at the IdP.
- Multi-replica shared state (token cache and `jti` replay cache are in-memory; documented
  limitation).

## 2. Where it is implemented — component map

New package **`internal/auth/`** holds all OAuth/JWT logic, keeping `internal/gateway` focused
on MCP proxying. Integration points are thin touches on existing files.

| Concern | Location | New/Change |
|---|---|---|
| Config structs (`idp:`, `server.auth:`, downstream `auth.type`) | `internal/config/config.go` + `internal/config/defaults/maybe-dont.yaml` | Change |
| Env-var plumbing for new downstream auth fields | `internal/config/config.go` (`knownClientConfigFields`, `applyClientConfigField`) | Change |
| OIDC discovery document fetch/cache | `internal/auth/discovery.go` | New |
| JWKS cache + JWT validation | `internal/auth/validator.go` | New |
| ID-JAG validation (embedded AS mode) | `internal/auth/idjag.go` | New |
| Embedded AS: signing key, token issuance, `jti` replay cache | `internal/auth/issuer.go` | New |
| RFC 8693 token-exchange client + RFC 7523 JWT-bearer client | `internal/auth/exchange.go` | New |
| Downstream token broker (per-session cache, per-auth-type strategy) | `internal/auth/broker.go` | New |
| Bearer middleware + `WWW-Authenticate` challenge | `internal/gateway/bearer_middleware.go` | New |
| Well-known endpoints (`oauth-protected-resource`, `oauth-authorization-server`) + embedded AS `/oauth2/token` | `internal/gateway/wellknown.go` | New |
| Mux wiring | `internal/gateway/server.go` (`initHTTPServer` ~line 785, `initSSEServer` ~line 647) | Change |
| Identity in request context | `internal/gateway/context.go` | Change |
| Identity bound to session | `internal/gateway/session.go` (`Session` struct) | Change |
| Downstream header func for exchanged tokens | `internal/gateway/client_manager.go` (`createClient` ~line 324, alongside `createAuthHeaderFunc` ~line 959) | Change |
| Startup-skip + lazy discovery for identity-bound clients | `internal/gateway/client_manager.go` (`DiscoverAllCapabilities` ~line 70), `internal/gateway/server.go` (`hasPassThroughCredentials` ~line 1020, `ensurePassThroughToolsDiscovered` ~line 242) | Change |
| CEL `auth` variable population | `internal/gateway/cel_engine.go` (vars map ~line 225) — variable already declared at line 52 | Change |
| Audit enrichment | `internal/gateway/audit_entry.go` (`UpstreamRequestInfo` ~line 88), populated in `internal/gateway/gateway.go` (`HandleToolCall` ~line 408) | Change |
| Existing caller-header auth interplay | `internal/gateway/auth_middleware.go` (exempt `/.well-known/*`) | Change |

**Dependency to add**: `github.com/lestrrat-go/jwx/v3` (`jwk` for JWKS caching with kid-based
refresh, `jwt` for parse/validate/sign, `jws` for signature ops). One dependency covers
validation, ID-JAG checks, and embedded-AS issuance. Token-exchange HTTP calls use stdlib
`net/http` form posts — no OAuth client library needed.

## 3. Deployment modes and phasing

Implement in three phases; each is independently shippable and useful.

**Phase 1 — Upstream Resource Server (external AS mode).** `idp:` config, JWKS Bearer
validation middleware on the MCP endpoints, 401 + `WWW-Authenticate`, RFC 9728 protected
resource metadata endpoint, identity → context/session/CEL/audit. In this mode the gateway's
Resource Authorization Server is external — typically the enterprise IdP itself (e.g. an Okta
custom authorization server, which natively supports the ID-JAG grant via Cross App Access).
The gateway merely validates the resulting JWT access tokens. This alone makes the gateway
EMA-compatible for IdPs that can act as the resource AS, and it delivers the policy-engine and
audit wins.

**Phase 2 — Downstream on-behalf-of.** The token broker, `auth.type: token_exchange` (plain
RFC 8693, exactly as in the OBO draft) and `auth.type: enterprise_managed` (ID-JAG →
JWT-bearer with downstream discovery), per-session token cache, audit token-exchange fields.

**Phase 3 — Embedded Resource Authorization Server (optional mode).** The gateway hosts
`/.well-known/oauth-authorization-server` (advertising the id-jag grant profile) and
`/oauth2/token` accepting `grant_type=jwt-bearer` with an ID-JAG assertion; validates the
ID-JAG against the IdP's JWKS; issues its own short-lived signed access tokens. This makes the
gateway EMA-compliant standalone — the differentiator for deployments whose IdP cannot act as
a resource AS for a self-hosted service.

## 4. Configuration design

```yaml
# ── The enterprise IdP (shared by upstream validation and downstream exchange) ──
idp:
  issuer: https://acme.okta.example            # expected `iss` in tokens and ID-JAGs
  # Discovery: either the OIDC discovery URL (default: {issuer}/.well-known/openid-configuration)
  openid_connect_discovery_url: https://acme.okta.example/.well-known/openid-configuration
  # …or explicit overrides (both required if discovery URL omitted):
  jwks_url: ""
  token_endpoint: ""
  # Gateway's confidential-client credentials at the IdP (required for downstream exchange):
  client_id: maybedont-gateway
  client_secret: ${MAYBEDONT_IDP_CLIENT_SECRET}

# ── Upstream auth: how the gateway authenticates incoming MCP requests ──
server:
  auth:
    mode: disabled            # disabled | jwt_validation | embedded_as
    # Public identity of this gateway as an OAuth protected resource (RFC 9728 `resource`).
    # Required when mode != disabled. Also the default expected `aud`.
    resource: https://maybedont.example/mcp
    audience: ""              # override expected `aud` (default: resource)
    scopes_supported: []      # advertised in protected resource metadata

    # mode: jwt_validation — external Resource AS issues the tokens; gateway validates.
    authorization_servers:    # advertised in protected resource metadata
      - https://acme.okta.example/oauth2/default

    # mode: embedded_as — gateway is its own Resource AS.
    embedded_as:
      issuer: https://maybedont.example          # AS issuer identifier (external URL)
      access_token_ttl_seconds: 3600
      signing_key_file: ""    # default: {state_dir}/as-signing-key.pem, auto-generated Ed25519
      allowed_client_ids: []  # optional allowlist matched against ID-JAG `client_id`

# ── Downstream auth: per-server strategy ──
downstream_mcp_servers:
  asana:                      # EMA-capable downstream: full ID-JAG chain
    type: http
    url: https://mcp.asana.example/
    auth:
      type: enterprise_managed
      resource: ""            # RFC 9728 resource identifier; default: derived from url (origin)
      scope: "tasks.read tasks.write"   # optional

  legacy-api:                 # plain RFC 8693 OBO (per the existing OBO draft)
    type: http
    url: https://api.legacy.example/mcp/
    auth:
      type: token_exchange
      audience: ""            # default: derived from url (origin)
      scope: ""

  internal-wiki:              # existing mechanism, now expressible via auth.type
    type: http
    url: https://wiki.internal.example/mcp/
    auth:
      type: pass_through
      headers:
        - source_header: X-API-Key
          target_header: Authorization
```

**Backward compatibility**: `auth.pass_through.enabled: true` (the current shape) keeps
working. At load time, config normalization maps it to `auth.type: pass_through`. Setting both
`auth.type` and a conflicting legacy block is a startup error (fail fast).

**Startup validation** (extend `validateConfig` in `internal/config/config.go`):

- `server.auth.mode` ∈ {disabled, jwt_validation, embedded_as}; invalid → startup error.
- `mode != disabled` → `idp.issuer` and (discovery URL or `jwks_url`) required; `resource`
  required and must be an absolute https URL (http allowed only for localhost, for dev).
- `mode: embedded_as` → `embedded_as.issuer` defaults to the origin of `resource`.
- Any downstream with `auth.type` ∈ {token_exchange, enterprise_managed} →
  requires `idp.client_id` + `idp.client_secret` + token endpoint (discovery or explicit),
  **and** `server.auth.mode != disabled` (the subject token comes from the validated upstream
  request), **and** downstream `type` ∈ {http, sse} (stdio cannot carry a Bearer header —
  startup error).
- New downstream env-var fields registered in `knownClientConfigFields`:
  `auth_type`, `auth_resource`, `auth_audience`, `auth_scope` →
  e.g. `MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_ASANA_AUTH_TYPE=enterprise_managed`.
- Keep `internal/config/defaults/maybe-dont.yaml` in sync (commented-out example block, as is
  done for pass-through today).

## 5. Detailed design with pseudo code

### 5.1 `internal/auth/discovery.go` — OIDC / AS metadata discovery

```
type DiscoveryDocument struct {
    Issuer                             string   `json:"issuer"`
    TokenEndpoint                      string   `json:"token_endpoint"`
    JWKSURI                            string   `json:"jwks_uri"`
    AuthorizationGrantProfilesSupported []string `json:"authorization_grant_profiles_supported"`
}

// FetchDiscovery fetches and validates a discovery document.
// Used for (a) the IdP at startup, (b) downstream resource AS metadata at first use.
func FetchDiscovery(ctx, httpClient, url string) (*DiscoveryDocument, error):
    resp = httpClient.GET(url, timeout=10s)
    if resp.status != 200 → error "discovery fetch failed: {status}"
    doc = json.Decode(resp.body)
    if doc.Issuer == "" or doc.JWKSURI == "" → error   // fail fast, no defaults
    return doc

// ResolveIdP is called once at startup when server.auth.mode != disabled or any
// downstream uses token_exchange/enterprise_managed.
// Precedence: explicit jwks_url/token_endpoint override discovery values.
// Startup FAILS if discovery is unreachable and no explicit overrides exist
// (consistent with "failed client initialization causes gateway startup failure").
func ResolveIdP(ctx, cfg config.IdPConfig) (*IdPEndpoints, error)
```

RFC 9728 protected resource metadata for downstreams is fetched lazily (see §5.7) from
`{resource-origin}/.well-known/oauth-protected-resource[{resource-path}]` per RFC 9728 path
insertion rules.

### 5.2 `internal/auth/validator.go` — JWKS cache and access-token validation

Wraps `jwx/v3`'s `jwk.Cache` (auto-refresh honoring HTTP cache headers, plus
refresh-on-unknown-`kid` for key rotation).

```
type Identity struct {
    Subject   string            // sub
    Email     string            // email (optional claim)
    ClientID  string            // client_id / azp if present
    Scopes    []string          // split of `scope` claim
    Claims    map[string]any    // full claim set for CEL
    ExpiresAt time.Time
    RawToken  string            // kept ONLY in-memory for downstream exchange; never logged
}

type Validator struct {
    jwksCache *jwk.Cache        // registered for idpEndpoints.JWKSURI
    issuer    string            // expected iss
    audience  string            // expected aud
    allowedAlgs []jwa.SignatureAlgorithm  // RS256, ES256, EdDSA — explicitly NO none/HS*
}

func (v *Validator) ValidateAccessToken(ctx, raw string) (*Identity, error):
    keySet = v.jwksCache.Lookup(ctx, v.jwksURL)
    tok, err = jwt.Parse(raw,
        jwt.WithKeySet(keySet),                 // signature via kid match
        jwt.WithIssuer(v.issuer),
        jwt.WithAudience(v.audience),
        jwt.WithAcceptableSkew(60s),            // exp/nbf/iat clock skew
        jwt.WithValidate(true))
    if err → return nil, err                    // caller maps to 401
    return identityFromClaims(tok, raw)
```

In `mode: embedded_as` the same Validator validates the gateway's **own** tokens instead
(issuer = `embedded_as.issuer`, key = the local signing key, not a remote JWKS).

### 5.3 `internal/gateway/bearer_middleware.go` — upstream enforcement

Wiring in `initHTTPServer` (`server.go` ~line 809) — and identically in `initSSEServer` for
`/sse` + `/message`:

```
mux := http.NewServeMux()
if authMode != disabled:
    mux.Handle("/mcp", g.bearerAuthMiddleware(g.mcpContextMiddleware(mcpHandler)))
    mux.Handle("/.well-known/oauth-protected-resource", protectedResourceMetadataHandler)
    if authMode == embedded_as:
        mux.Handle("/.well-known/oauth-authorization-server", asMetadataHandler)
        mux.Handle("/oauth2/token", asTokenEndpointHandler)
else:
    mux.Handle("/mcp", g.mcpContextMiddleware(mcpHandler))    // today's behavior
... REST endpoints unchanged ...
handler := AuthMiddleware(g.callerAuthConfig, mux)            // existing outer wrap kept
```

Bearer enforcement scope: **MCP routes only**. REST endpoints keep their existing scheme.
`/.well-known/*` and `/oauth2/token` must be reachable unauthenticated, which requires one
change to the existing header-auth middleware (`auth_middleware.go`): skip enforcement for
`/.well-known/` paths and `/oauth2/token`. (Discovery breaks otherwise when both auth
mechanisms are enabled.)

```
func (g *Gateway) bearerAuthMiddleware(next http.Handler) http.Handler:
    return func(w, r):
        raw = extractBearer(r.Header.Get("Authorization"))    // case-insensitive "Bearer " prefix
        if raw == "":
            challenge(w, `Bearer resource_metadata="{resource-origin}/.well-known/oauth-protected-resource"`, 401)
            return
        identity, err = g.tokenValidator.ValidateAccessToken(r.Context(), raw)
        if err != nil:
            // RFC 6750 §3: invalid_token for malformed/expired/wrong-audience
            challenge(w, `Bearer error="invalid_token", resource_metadata="…"`, 401)
            g.logger.Debug("bearer validation failed", zap.Error(err))   // NEVER log the token
            return
        ctx = WithIdentity(r.Context(), identity)             // new context key in context.go
        next.ServeHTTP(w, r.WithContext(ctx))
```

Fail-closed: any validation error → 401. There is no audit-only mode for authentication
itself (policy rules remain the place for audit-only behavior).

**Session binding** (`session.go` + `server.go` hooks): on `onSessionRegister`, copy the
request identity onto the `Session` (`Session.Identity *auth.Identity`, guarded by the
existing mutex). On each subsequent request for an existing session (in
`mcpContextMiddleware` / `AddOnRequestInitialization`), if the session has an identity and the
current request's `sub` differs → reject with 401 and log at ERROR: prevents one user's
session being driven by another user's token. Session cleanup already deletes sessions and
their downstream clients, which also drops cached downstream tokens (§5.7).

### 5.4 `internal/gateway/wellknown.go` — RFC 9728 protected resource metadata

```
GET /.well-known/oauth-protected-resource → 200 application/json
{
  "resource": cfg.Server.Auth.Resource,
  "authorization_servers":
      mode==jwt_validation → cfg.Server.Auth.AuthorizationServers
      mode==embedded_as    → [cfg.Server.Auth.EmbeddedAS.Issuer],
  "scopes_supported": cfg.Server.Auth.ScopesSupported,
  "bearer_methods_supported": ["header"]
}
```

Static content computed once at startup; handler just writes cached bytes.
Method guard: GET/HEAD only, others → 405.

### 5.5 `internal/auth/issuer.go` + `idjag.go` — embedded Resource Authorization Server (Phase 3)

**AS metadata** (RFC 8414):

```
GET /.well-known/oauth-authorization-server → 200
{
  "issuer": embeddedAS.Issuer,
  "token_endpoint": "{issuer}/oauth2/token",
  "grant_types_supported": ["urn:ietf:params:oauth:grant-type:jwt-bearer"],
  "authorization_grant_profiles_supported": ["urn:ietf:params:oauth:grant-profile:id-jag"],
  "token_endpoint_auth_methods_supported": ["none"]     // see client-auth note below
}
```

**Signing key management**:

```
func LoadOrCreateSigningKey(path string) (ed25519.PrivateKey, error):
    if file exists → parse PKCS8 PEM; fail fast on parse error
    else → generate ed25519 key, write PEM with 0600 perms into the state dir
    // Tokens are only validated by this same gateway, so no JWKS publication is needed
    // and algorithm compatibility with third parties is not a concern. EdDSA: small, fast, safe.
```

**Token endpoint**:

```
POST /oauth2/token   (Content-Type: application/x-www-form-urlencoded)

func (h *ASTokenHandler) ServeHTTP(w, r):
    if r.Method != POST → 405
    form = r.ParseForm()
    if form.grant_type != "urn:ietf:params:oauth:grant-type:jwt-bearer":
        → 400 {"error":"unsupported_grant_type"}
    assertion = form.assertion
    if assertion == "" → 400 {"error":"invalid_request","error_description":"assertion required"}

    grant, err = h.idjagValidator.Validate(ctx, assertion)   // below
    if err → 400 {"error":"invalid_grant","error_description": safeReason(err)}   // no token echo

    if len(cfg.AllowedClientIDs) > 0 and grant.ClientID not in cfg.AllowedClientIDs:
        → 400 {"error":"unauthorized_client"}

    // Scope policy: intersect requested (grant.Scope) with scopes_supported if configured;
    // empty scopes_supported → grant as requested (IdP already policy-checked the scopes).
    granted = intersectOrPassthrough(grant.Scopes, cfg.ScopesSupported)

    accessToken = h.issuer.Issue(IssueParams{
        Subject: grant.Subject, Email: grant.Email, ClientID: grant.ClientID,
        Audience: cfg.Server.Auth.Resource,        // audience-restricted to the MCP server (the gateway)
        Scopes: granted, TTL: cfg.AccessTokenTTL,
    })
    w.Header("Cache-Control", "no-store")
    → 200 {"token_type":"Bearer","access_token":accessToken,
           "expires_in":ttlSeconds,"scope":join(granted," ")}
```

**ID-JAG validation** (`idjag.go`) — the security-critical piece:

```
func (v *IDJAGValidator) Validate(ctx, assertion string) (*IDJAGClaims, error):
    // 1. Header typ MUST be "oauth-id-jag+jwt" (jwt.WithTypedClaim / manual header check)
    // 2. Signature against the IdP's JWKS (same jwk.Cache as §5.2)
    // 3. iss == idp.issuer          (exact match)
    // 4. aud == embeddedAS.Issuer   (exact match — the gateway-as-AS, NOT the resource)
    // 5. exp valid with 60s skew; iat not unreasonably old (> idjag max age 10m → reject)
    // 6. Required claims present: jti, sub, client_id — missing → error (fail fast)
    // 7. resource claim, if present, MUST equal cfg.Server.Auth.Resource
    // 8. jti replay check:
    if !v.replayCache.StoreIfAbsent(jti, exp):    // in-memory, TTL = exp, capacity-bounded
        return error "replayed jti"
    return claims

// replayCache: map[string]time.Time + mutex; periodic sweep of expired entries;
// hard cap (e.g. 100k entries) → on overflow reject new grants with a clear error rather
// than silently dropping replay protection (fail closed). Multi-replica deployments need
// sticky routing or an external cache — documented limitation.
```

### 5.6 `internal/auth/exchange.go` — RFC 8693 + RFC 7523 clients (Phase 2)

```
// Plain OBO exchange at the IdP (auth.type: token_exchange — unchanged from the OBO draft):
func (c *ExchangeClient) ExchangeAccessToken(ctx, subjectToken, audience, scope) (*TokenResponse, error):
    POST idp.tokenEndpoint  (form):
        grant_type=urn:ietf:params:oauth:grant-type:token-exchange
        subject_token=<subjectToken>
        subject_token_type=urn:ietf:params:oauth:token-type:access_token
        audience=<audience>
        [scope=<scope>]
        client_id / client_secret            // confidential client auth (HTTP Basic also fine)
    → {access_token, expires_in, scope, ...} or OAuth error passthrough

// ID-JAG acquisition at the IdP (auth.type: enterprise_managed), same endpoint:
func (c *ExchangeClient) ExchangeForIDJAG(ctx, subjectToken, resourceAS, resource, scope) (*TokenResponse, error):
    POST idp.tokenEndpoint  (form):
        grant_type=urn:ietf:params:oauth:grant-type:token-exchange
        requested_token_type=urn:ietf:params:oauth:token-type:id-jag
        audience=<resourceAS issuer>          // the DOWNSTREAM's resource authorization server
        resource=<downstream resource identifier>
        subject_token=<the caller's validated gateway token>
        subject_token_type=urn:ietf:params:oauth:token-type:access_token
        [scope=…], client_id, client_secret
    → {issued_token_type: "urn:ietf:params:oauth:token-type:id-jag",
       access_token: <the ID-JAG>, token_type: "N_A", expires_in}

// JWT-bearer grant at the downstream's resource AS:
func (c *ExchangeClient) RedeemIDJAG(ctx, tokenEndpoint, idjag string) (*TokenResponse, error):
    POST tokenEndpoint (form):
        grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer
        assertion=<idjag>
        client_id=idp.client_id
    → {token_type: "Bearer", access_token, expires_in, scope}
```

> **Spec note**: the ID-JAG draft describes the *SSO client* doing the exchange with an ID
> token or refresh token as subject. A middle-tier gateway holds neither — it holds the user's
> access token. Okta's Cross App Access and the RFC 8693 framework permit
> `subject_token_type=access_token`, but whether the IdP will mint an ID-JAG for it is **IdP
> policy**. This spec therefore (a) defaults to `access_token` subject type, (b) surfaces IdP
> errors verbatim to the audit log and as a clear MCP error, and (c) documents that
> `enterprise_managed` downstreams require IdP support for middle-tier exchange (as of 2026,
> Okta XAA supports this; the gateway must be registered as a confidential client with token
> exchange enabled).

### 5.7 `internal/auth/broker.go` — downstream token broker

One strategy object per downstream auth type, resolved at config load; the broker owns caching.

```
type TokenBroker struct {
    exchange   *ExchangeClient
    httpClient *http.Client
    mu         sync.Mutex
    // Cached downstream discovery (per client name): resource AS issuer + token endpoint.
    downstreamAS map[string]*DownstreamASInfo
    // Cached exchanged tokens: key = {sessionID, clientName}.
    // Tied to session lifetime; evicted on session delete and on expiry.
    tokens map[tokenKey]*cachedToken     // cachedToken{value, expiresAt}
}

func (b *TokenBroker) TokenFor(ctx, sessionID, clientName string, cfg ClientAuthConfig) (string, error):
    if t, ok = b.tokens[{sessionID, clientName}]; ok and t.expiresAt > now+30s:
        return t.value
    subject = IdentityFromContext(ctx).RawToken        // the caller's validated gateway token
    if subject == "" → error "no upstream identity available for token exchange"

    switch cfg.Type:
    case token_exchange:
        aud = cfg.Audience or origin(cfg.URL)
        resp = b.exchange.ExchangeAccessToken(ctx, subject, aud, cfg.Scope)
    case enterprise_managed:
        as = b.discoverDownstreamAS(ctx, clientName, cfg)     // below, cached
        idjag = b.exchange.ExchangeForIDJAG(ctx, subject, as.Issuer, cfg.ResourceOr(originOf(cfg.URL)), cfg.Scope)
        resp = b.exchange.RedeemIDJAG(ctx, as.TokenEndpoint, idjag.AccessToken)
    store in cache with expiresAt = now + resp.ExpiresIn - 30s safety margin
    return resp.AccessToken

func (b *TokenBroker) discoverDownstreamAS(ctx, name, cfg) (*DownstreamASInfo, error):
    // 1. GET {resource}/.well-known/oauth-protected-resource   (RFC 9728 path rules)
    // 2. Pick authorization_servers[0]  (log if >1; future: config override auth.authorization_server)
    // 3. GET that AS's metadata (/.well-known/oauth-authorization-server per RFC 8414)
    // 4. Verify authorization_grant_profiles_supported contains
    //    "urn:ietf:params:oauth:grant-profile:id-jag" — if absent → clear startup/first-use
    //    error telling the operator this downstream doesn't support EMA.
    // 5. Cache {Issuer, TokenEndpoint} per client name (TTL 1h).

func (b *TokenBroker) EvictSession(sessionID)   // called from SessionManager.DeleteSession
```

**Wiring into `client_manager.go`** — mirrors the pass-through pattern exactly
(`createClient`, http case ~line 368; sse case analogous):

```
case cfg.Auth.Type in {token_exchange, enterprise_managed}:
    headerFunc = func(ctx, req) http.Header:
        token, err = cm.tokenBroker.TokenFor(ctx, sessionIDFrom(ctx), name, cfg.Auth)
        if err:
            // Header funcs can't return errors in mcp-go; log at DEBUG and send no
            // Authorization header — downstream will 401, which surfaces as a tool error.
            // ADDITIONALLY: HandleToolCall pre-flights the broker (below) so the common
            // path fails with a clean, specific error before the downstream call.
            return headers
        headers.Set("Authorization", "Bearer "+token)
    httpOpts = append(httpOpts, transport.WithHTTPHeaderFunc(headerFunc))
```

**Pre-flight in `gateway.go` `HandleToolCall`** (after validation passes, before
`clientInfo.Client.CallTool` ~line 561): if the target client's auth type is
token_exchange/enterprise_managed, call `TokenFor` once (warms the cache) and on error return
a `PolicyDeniedError`-style MCP error: `"token exchange failed for downstream '{name}':
{oauth error + description}"`, and record it in the audit entry. This keeps the header func a
cache read in the normal case.

**Lazy discovery**: identity-bound clients cannot be discovered at startup (no user token
exists). Reuse the pass-through machinery:

- `DiscoverAllCapabilities` (~client_manager.go:70): extend the skip predicate from
  `Auth.PassThrough.Enabled` to `cfg.Auth.RequiresPerSessionCredentials()` (pass_through OR
  token_exchange OR enterprise_managed).
- `hasPassThroughCredentials` (~server.go:1020): for the new types, the credential present
  check is "request context has a validated Identity".
- `maybedont__discover_tools` and the `WithToolFilter` lazy path work unchanged once the two
  predicates above are widened.

### 5.8 Identity → policy engine and audit

**Context** (`context.go`): add `identityContextKey` + `WithIdentity` / `IdentityFromContext`.
`mcpContextMiddleware` already enriches context; the bearer middleware runs before it and
injects the identity.

**CEL** (`cel_engine.go`): the `auth` variable is already declared (line 52) but never bound.
Populate it in the vars map built at ~line 225 (and mirror in the response-engine vars):

```
vars["auth"] = map[string]any{
    "authenticated": identity != nil,
    "user": map[string]any{           // zero values when unauthenticated
        "sub":    identity.Subject,
        "email":  identity.Email,
        "scopes": identity.Scopes,
        "claims": identity.Claims,    // full claim map for org-specific claims (groups, dept…)
    },
}
```

Example rule this unlocks (goes in docs + `cel-policy-authoring` skill examples):

```yaml
- name: interns-cannot-delete-repos
  mcp_expression: |
    tool.name == "github__delete_repo" &&
    !("eng-admins" in get(auth.user.claims, "groups", []))
  action: deny
  message: "Repo deletion requires eng-admins group membership"
```

**AI engine**: append a short identity line to the AI validation request context
("Authenticated user: sub=…, email=…, scopes=…") — claims only, never the token. Optional,
flag-gated (`request_validation.ai.include_user_identity`, default true when auth enabled).

**Audit** (`audit_entry.go`, matching the OBO draft's field table):

```
type UpstreamRequestInfo struct {
    ...existing fields...
    Sponsor       *AuditSponsor       `json:"sponsor,omitempty"`        // {sub, email}
    ActingParty   string              `json:"acting_party,omitempty"`   // idp.client_id when exchange used
    TokenExchange *AuditTokenExchange `json:"token_exchange,omitempty"` // {client, audience, scopes_requested, scopes_granted, flow: "rfc8693"|"id-jag"}
}
```

Populate sponsor from the context identity in `NewAuditContext`/`HandleToolCall`
(gateway.go ~line 408); populate TokenExchange from the broker result during pre-flight.
**Never** write raw tokens, `Authorization` values, or the ID-JAG into the audit log or app
logs (existing security rule; enforce in review).

### 5.9 Error handling summary

| Failure | Behavior |
|---|---|
| No/invalid/expired Bearer on `/mcp` | 401 + `WWW-Authenticate` challenge (fail closed) |
| Token `sub` differs from session identity | 401, ERROR log (no token contents) |
| IdP discovery unreachable at startup (auth enabled, no overrides) | startup failure |
| JWKS temporarily unreachable at request time | 401 `invalid_token` (fail closed) + ERROR log |
| ID-JAG invalid at embedded AS | 400 `invalid_grant` |
| Replayed `jti` / replay cache full | 400 `invalid_grant` / ERROR log |
| Token exchange rejected by IdP | MCP tool error with OAuth `error` + `error_description`; audited; downstream not called |
| Downstream AS lacks id-jag profile | clear operator-facing error naming the client and the missing metadata field |
| stdio downstream with exchange auth type | startup validation error |

### 5.10 Testing plan

Follow repo conventions (table-driven, testify, purpose comment per test; test-first for the
validation logic).

- `internal/auth/validator_test.go` — table-driven over: valid token, expired, wrong iss,
  wrong aud, alg=none, HS256 (rejected), unknown kid (triggers JWKS refresh), skew edges.
  Uses `httptest` JWKS server + locally generated RSA/EC/Ed25519 keys.
- `internal/auth/idjag_test.go` — typ header wrong, missing jti/client_id, aud mismatch,
  resource mismatch, replayed jti, replay-cache capacity behavior.
- `internal/auth/exchange_test.go` — httptest IdP: asserts exact form fields per §5.6 for all
  three calls; OAuth error passthrough.
- `internal/auth/broker_test.go` — cache hit/miss/expiry, per-session eviction, downstream
  discovery (mock RFC 9728 + RFC 8414 endpoints), missing id-jag profile error.
- `internal/gateway/bearer_middleware_test.go` — 401 challenge format, happy path injects
  identity, well-known reachable unauthenticated, existing header-auth interplay.
- `internal/gateway/wellknown_test.go` — metadata JSON contents per config permutations.
- `internal/gateway/cel_engine_test.go` — new cases: rules referencing `auth.user.*`, both
  authenticated and unauthenticated activations.
- `internal/gateway/gateway_test.go` — end-to-end: mock IdP + mock EMA downstream
  (httptest MCP server whose PRM/AS metadata point at a mock AS); assert the downstream saw
  the exchanged token, audit entry contains sponsor/acting_party/token_exchange.
- `internal/config/config_test.go` — new fields load from YAML + env override
  (`MAYBE_DONT_SERVER_AUTH_MODE`, `MAYBE_DONT_IDP_ISSUER`,
  `MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_X_AUTH_TYPE`…), validation failure cases from §4.
- Manual verification recipe (see Appendix A): run gateway with `mode: embedded_as` against a
  scripted fake IdP, mint an ID-JAG with a test key, walk the full curl sequence: 401
  challenge → PRM → AS metadata → jwt-bearer → authenticated `tools/list`.

### 5.11 Implementation order (maps to phases)

1. Config structs + validation + defaults + env plumbing (§4) — no behavior change when
   `mode: disabled` (the default).
2. `internal/auth`: discovery, validator (Phase 1 core).
3. Bearer middleware + well-known PRM + context/session identity + CEL `auth` + audit sponsor
   fields (Phase 1 complete — ship it).
4. Exchange client + broker + client_manager wiring + lazy-discovery predicate widening +
   audit token_exchange fields (Phase 2 — `token_exchange` first, then `enterprise_managed`).
5. Embedded AS: issuer, ID-JAG validator, token endpoint, AS metadata (Phase 3).
6. Docs: update this spec's status, `docs/specs/README.md`, defaults YAML comments, and flag
   for the maybedont.ai/docs checklist in the PR description (per CLAUDE.md).

### 5.12 Security considerations checklist (for the security-review skill pass)

- Never log tokens, `Authorization` headers, assertions, or `client_secret` (existing rule).
- Signature alg allowlist; reject `none`/HMAC on asymmetric paths.
- Exact-match `iss`/`aud`; `resource` binding on ID-JAG; 60s skew only on time claims.
- Fail closed everywhere on the auth path; the fail-open blocking budget does NOT apply to
  authentication.
- Signing key file 0600 in state dir; document rotation (delete file → restart → new key
  invalidates outstanding embedded-AS tokens — acceptable for short TTLs).
- Replay cache is fail-closed on overflow.
- Session-identity binding prevents cross-user session reuse.
- `client_secret` via `${ENV}` expansion (already supported by `expandEnvironmentVariables`).
- TLS assumed at the deployment edge; `resource`/issuer URLs must be https (localhost exempt).
- In-memory caches bounded; per-session eviction hooked to existing session cleanup.

## Appendix A: End-to-end sequence (gateway with embedded AS + EMA downstream)

```
MCP Client                Maybe Don't                    IdP                 Downstream MCP
    │  tools/list (no token)   │                          │                        │
    │─────────────────────────▶│ 401 WWW-Authenticate:    │                        │
    │◀─────────────────────────│  resource_metadata=…     │                        │
    │  GET /.well-known/oauth-protected-resource          │                        │
    │─────────────────────────▶│ {authorization_servers:[gateway-AS]}              │
    │  GET /.well-known/oauth-authorization-server        │                        │
    │─────────────────────────▶│ {…grant_profiles:[id-jag]}                        │
    │  RFC8693: ID-token → ID-JAG (aud=gateway-AS)        │                        │
    │────────────────────────────────────────────────────▶│  (admin policy check)  │
    │◀────────────────────────────────────────────────────│  ID-JAG                │
    │  POST /oauth2/token jwt-bearer(ID-JAG)              │                        │
    │─────────────────────────▶│ validate vs IdP JWKS,    │                        │
    │◀─────────────────────────│ issue gateway token      │                        │
    │  tools/call + Bearer     │                          │                        │
    │─────────────────────────▶│ validate → CEL/AI policy (auth.user.*) → allow    │
    │                          │  RFC8693: gateway-token → ID-JAG (aud=downstream-AS)
    │                          │─────────────────────────▶│                        │
    │                          │◀─────────────────────────│ ID-JAG                 │
    │                          │  jwt-bearer at downstream AS → downstream token   │
    │                          │───────────────────────────────────────────────────▶
    │                          │  tools/call + Bearer <downstream token>           │
    │                          │───────────────────────────────────────────────────▶
    │◀─────────────────────────│  response validation → audit {sponsor, acting_party, token_exchange}
```

## Appendix B: New dependency

`github.com/lestrrat-go/jwx/v3` — JWKS cache (`jwk.Cache` with kid-miss refresh), JWT
parse/validate/sign, EdDSA support. Actively maintained, no transitive heavyweights. Run
`go mod tidy` after adding (repo convention).

## Appendix C: References

- Blog announcement: <https://blog.modelcontextprotocol.io/posts/enterprise-managed-auth/>
- MCP extension spec (SEP-990):
  <https://github.com/modelcontextprotocol/ext-auth/blob/main/specification/stable/enterprise-managed-authorization.mdx>
- Identity Assertion JWT Authorization Grant: draft-ietf-oauth-identity-assertion-authz-grant
- RFC 8693 (OAuth 2.0 Token Exchange), RFC 7523 (JWT Bearer Grant),
  RFC 9728 (Protected Resource Metadata), RFC 8414 (Authorization Server Metadata),
  RFC 6750 (Bearer Token Usage)
- Existing repo docs: `docs/plans/2026-02-20-obo-token-exchange-spec.md`,
  `docs/specs/gateway-auth-header-design.md`
