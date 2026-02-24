# OBO Token Exchange — Product Enhancement Spec

**Date**: 2026-02-20
**Author**: daniel
**Status**: Draft
**Related**: [Blog design doc](https://github.com/maybedont/maybedont-site) — `docs/plans/2026-02-20-obo-token-exchange-design.md` on branch `degroff/obo-token-exchange`

## Overview

Product enhancements needed for Maybe Don't to support end-to-end RFC 8693 (On-Behalf-Of) token exchange as an MCP gateway. This enables delegated identity for agents: the user's token is exchanged for a scoped-down, audience-bound, short-lived token before any downstream API call.

## Architecture Context

```
Agent → Maybe Don't (MCP Gateway) → IdP (token exchange) → Downstream API

1. Agent hits Maybe Don't, gets 401 + resource metadata pointing to IdP
2. Agent authenticates with IdP (device grant / auth code + PKCE), gets token (aud=MaybeDont)
3. Agent sends MCP tool call with Bearer token
4. Maybe Don't validates token (JWKS), runs policy engine (allow/deny)
5. Maybe Don't performs RFC 8693 token exchange → new token (same sub, aud=downstream, act=MaybeDont)
6. Maybe Don't calls downstream API with exchanged token
7. Audit log records: sub, act, tool, scopes, decision
```

## Configuration

### Config Shape

```yaml
idp:
  client_id: maybedont-gateway
  client_secret: ${MAYBEDONT_CLIENT_SECRET}
  openid_connect_discovery_url: https://idp.example/.well-known/openid-configuration
  issuer: https://idp.example
  audience: maybedont-gateway

downstream_mcp_servers:
  github:
    type: http
    url: https://api.github.com/mcp/
    auth:
      type: token_exchange          # opt into OBO
      scope: repo read:org          # optional — omit to let IdP decide

  internal-wiki:
    type: http
    url: https://wiki.internal.com/mcp/
    auth:
      type: pass_through            # existing mechanism
      headers:
        - source_header: X-API-Key
          target_header: Authorization
```

### `idp:` Section

| Field | Required | Description |
|-------|----------|-------------|
| `client_id` | Yes | Maybe Don't's client ID at the IdP. Used for token exchange authentication. |
| `client_secret` | Yes | Maybe Don't's client secret. Should come from environment variable. |
| `openid_connect_discovery_url` | Yes* | OIDC discovery endpoint. Derives JWKS endpoint + token endpoint. |
| `jwks_url` | No | Override: explicit JWKS endpoint for token validation. |
| `token_endpoint` | No | Override: explicit token endpoint for exchange requests. |
| `issuer` | Yes | Expected `iss` claim in incoming tokens. |
| `audience` | Yes | Expected `aud` claim in incoming tokens. Tokens not addressed to Maybe Don't are rejected. |

*Either `openid_connect_discovery_url` or both `jwks_url` + `token_endpoint` must be provided.

### `auth.type: token_exchange` Downstream Option

| Field | Required | Description |
|-------|----------|-------------|
| `type` | Yes | Must be `token_exchange`. |
| `audience` | No | Override audience for the exchange. Default: derived from downstream `url` (origin). |
| `scope` | No | Scopes to request in the exchange. Default: omitted (IdP decides). |

## Enhancements

### 1. `idp:` Config Section

New top-level config section. Parsed at startup. Validated: if any `downstream_mcp_servers` entry uses `auth.type: token_exchange`, `idp:` must be present (startup error if missing).

If `openid_connect_discovery_url` is provided, fetch and cache the discovery document at startup to derive `jwks_uri` and `token_endpoint`. Allow explicit overrides.

### 2. JWKS Token Validation Middleware

Validate incoming Bearer tokens against IdP's JWKS endpoint.

- Verify JWT signature using keys from JWKS endpoint
- Validate `iss` matches configured `issuer`
- Validate `aud` matches configured `audience`
- Validate `exp` (reject expired tokens)
- Extract claims (`sub`, `email`, etc.) to request context for use by policy engine and audit
- JWKS key cache with TTL + refresh on unknown `kid` (handles key rotation)

### 3. 401 + Protected Resource Metadata

When no valid Bearer token is present on an authenticated endpoint, return:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer resource_metadata="https://maybedont.example/.well-known/oauth-protected-resource"
```

Maybe Don't hosts the `/.well-known/oauth-protected-resource` endpoint (RFC 9728) that returns:

```json
{
  "resource": "https://maybedont.example",
  "authorization_servers": ["https://idp.example"],
  "scopes_supported": ["openid", "profile"],
  "bearer_methods_supported": ["header"]
}
```

The `authorization_servers` value is derived from the configured `openid_connect_discovery_url`.

### 4. RFC 8693 Token Exchange Client

Confidential client that exchanges user tokens at the IdP's token endpoint.

Request:
```http
POST /oauth/token HTTP/1.1
Content-Type: application/x-www-form-urlencoded

grant_type=urn:ietf:params:oauth:grant-type:token-exchange
&subject_token=<user_access_token>
&subject_token_type=urn:ietf:params:oauth:token-type:access_token
&audience=<downstream_api_audience>
&scope=<configured_scopes>
&client_id=maybedont-gateway
&client_secret=<secret>
```

Response includes the exchanged token with:
- Same `sub` (user identity preserved)
- New `aud` (downstream API)
- `act` claim (delegation chain showing Maybe Don't as the acting party)

Error handling: if the IdP rejects the exchange (insufficient grants, invalid token, etc.), return an appropriate error to the agent without calling the downstream API.

### 5. `auth.type: token_exchange` Downstream Option

New auth type for `downstream_mcp_servers`. When a tool call targets a downstream with this auth type:

1. Extract the user's Bearer token from the incoming request
2. Derive audience from downstream `url` (use origin), or use explicit `audience` override
3. Perform token exchange (enhancement #4)
4. Attach the exchanged token as `Authorization: Bearer <exchanged_token>` on the downstream request

Startup validation: if `auth.type: token_exchange` is configured but `idp:` is missing, fail with a clear error message.

### 6. Audit Enrichment

Enrich audit log entries with delegation chain information when token exchange is used:

| Field | Source | Description |
|-------|--------|-------------|
| `upstream_request.sponsor.sub` | User token `sub` claim | The human who authorized this agent session |
| `upstream_request.sponsor.email` | User token `email` claim | Human's email (if present in token) |
| `upstream_request.acting_party` | Maybe Don't's `client_id` | The party performing the action on behalf of the user |
| `upstream_request.token_exchange.audience` | Exchange request | Which downstream the token was exchanged for |
| `upstream_request.token_exchange.scopes_requested` | Exchange request | Scopes requested |
| `upstream_request.token_exchange.scopes_granted` | Exchange response | Scopes actually granted by the IdP |

This enables compliance queries like: "Show all actions taken on behalf of user X" and "Show all downstream services accessed by Maybe Don't acting as user Y."

## Dependencies

```
1 (idp config) → 2 (JWKS validation)
1 (idp config) → 3 (401 + resource metadata)
1 (idp config) → 4 (token exchange client)
4 (token exchange) → 5 (downstream auth type)
2 (JWKS validation) → 6 (audit enrichment)
```

## What Exists Today vs What's New

| Capability | Status |
|-----------|--------|
| Downstream MCP server proxying | Exists |
| Pass-through auth headers | Exists |
| AI policy engine (allow/deny) | Exists |
| CEL rules engine | Exists |
| Audit logging with caller info | Exists |
| CLI request validation | Exists |
| `idp:` config section | **New** |
| JWKS token validation | **New** |
| 401 + Protected Resource Metadata | **New** |
| RFC 8693 token exchange | **New** |
| `auth.type: token_exchange` | **New** |
| Delegation chain in audit | **New** |
