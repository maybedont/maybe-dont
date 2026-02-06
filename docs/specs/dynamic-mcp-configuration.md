# Dynamic MCP Configuration

**Status:** Draft
**Author:** degroff
**Created:** 2025-02-05

## Problem Statement

Currently, the maybe-dont gateway requires all downstream MCP servers to be pre-configured by an administrator. This creates friction in environments where:

1. Administrators want to audit and apply policy to MCP traffic without dictating which MCP servers developers can use
2. Engineering teams are decentralized and it's impractical for a single admin to know all MCP servers in use
3. Developers want autonomy to add new MCP integrations without waiting for admin configuration changes

### Current Architecture Limitation

```
┌─────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  AI Agent   │────▶│  Maybe-Dont     │────▶│  Pre-configured │
│  (Claude)   │     │  Gateway        │     │  MCP Servers    │
└─────────────┘     └─────────────────┘     └─────────────────┘
                           │
                    Requires admin to
                    configure each MCP
                    server in advance
```

### Desired Architecture

```
┌─────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  AI Agent   │────▶│  Maybe-Dont     │────▶│  Any MCP Server │
│  (Claude)   │     │  Gateway        │     │  (dynamic)      │
└─────────────┘     └─────────────────┘     └─────────────────┘
                           │
                    Developer specifies
                    target MCP server
                    per-request/session
```

## Goals

1. **Remove admin bottleneck** - Developers can route traffic through the gateway to any MCP server without pre-configuration
2. **Developer autonomy** - Each developer controls which MCPs they use via their local agent configuration
3. **Minimal config changes** - Require minimal modifications to existing agent MCP configurations
4. **Header pass-through** - Support passing authorization headers and other necessary headers to downstream MCP servers
5. **Resource efficiency** - No significant increase in CPU, memory, or connection overhead compared to static configuration
6. **Maintain security posture** - Continue to support policy evaluation (CEL and AI) on dynamically-routed traffic

## Constraints

### Transport Type: HTTP Only

This feature will support **HTTP transport only**. Rationale:

- **SSE is deprecated** in the MCP ecosystem
- **STDIO is not applicable** - routing stdio over an external proxy doesn't make architectural sense and would require the gateway to spawn arbitrary processes (security/resource concern)

### Session Isolation

Dynamically configured MCP connections must be session-scoped:
- A developer's dynamic MCP configuration should not be visible to or usable by other sessions
- Session expiration should clean up dynamic connections

### No Arbitrary Code Execution

The gateway must not spawn processes or execute arbitrary commands based on client input.

## Prior Art Research

### Existing MCP Gateway Solutions

| Solution | Dynamic Routing | Pre-config Required |
|----------|-----------------|---------------------|
| [Microsoft MCP Gateway](https://github.com/microsoft/mcp-gateway) | Tool-based routing | Yes - servers registered via control plane |
| [IBM ContextForge](https://ibm.github.io/mcp-context-forge/) | UUID-based routing | Yes - admin registration required |
| [Envoy AI Gateway](https://aigateway.envoyproxy.io/blog/mcp-implementation/) | Policy-based routing | Yes - JSON/K8s config |

**Finding:** No existing MCP gateway supports true "bring your own MCP server" at the client level.

### Related Tools

| Tool | Approach |
|------|----------|
| [mcp-remote](https://github.com/geelen/mcp-remote) | stdio wrapper that bridges to remote servers via CLI args |
| mcp-proxy | Rust bidirectional proxy with OAuth support |
| [Envoy Dynamic Forward Proxy](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/dynamic_forward_proxy_filter) | Host header rewriting for dynamic upstream |

### MCP Client Header Support

| Client | HTTP Headers Support | Notes |
|--------|---------------------|-------|
| Claude Code | Config supports `headers` field | [Bug: headers not transmitted](https://github.com/anthropics/claude-code/issues/14977) |
| Claude Desktop | Remote servers via Connectors | [No direct config](https://github.com/jlowin/fastmcp/issues/1789) |
| mcp-remote | `--header` CLI args | Works via stdio wrapper |

## Options for Upstream Specification

This section outlines approaches for how clients tell the gateway which downstream MCP server to connect to.

---

### Option 1: Query Parameter

The client encodes the target MCP server URL as a query parameter.

**Client Configuration:**
```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://gateway.example.com/mcp?upstream=https://api.githubcopilot.com/mcp/"
    }
  }
}
```

**Gateway Behavior:**
1. Extract `upstream` query parameter from incoming request URL
2. Validate URL (allowlist, blocklist, or open)
3. Create session-scoped connection to upstream
4. Proxy MCP traffic with policy evaluation

**Pros:**
- Works universally regardless of client header support
- Simple to implement
- Easy to debug (URL visible in logs)
- Compatible with URL-based configuration in all clients

**Cons:**
- Feels "hacky" - URLs containing URLs
- Requires URL encoding for complex upstream URLs
- Upstream URLs with query strings become complicated: `?upstream=https://example.com/mcp?token=abc` requires encoding
- URL length limits could be a concern for complex configurations

**Encoding Example:**
```
# Simple case
?upstream=https://api.github.com/mcp

# With query string (encoded)
?upstream=https%3A%2F%2Fexample.com%2Fmcp%3Ftoken%3Dabc
```

---

### Option 2: Custom Header

The client sends the target MCP server URL in a custom HTTP header.

**Client Configuration:**
```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://gateway.example.com/mcp",
      "headers": {
        "X-MCP-Upstream": "https://api.githubcopilot.com/mcp/"
      }
    }
  }
}
```

**Gateway Behavior:**
1. Extract `X-MCP-Upstream` header from incoming request
2. Validate URL
3. Create session-scoped connection to upstream
4. Proxy MCP traffic with policy evaluation

**Pros:**
- Clean separation of concerns (URL is gateway, header specifies target)
- No URL encoding issues
- Headers can contain complex values including query strings
- More RESTful/standard approach

**Cons:**
- Depends on client header support (currently inconsistent - see research above)
- [Claude Code has a bug](https://github.com/anthropics/claude-code/issues/14977) where headers aren't transmitted
- Claude Desktop doesn't support direct header configuration for remote servers
- Harder to debug (headers not visible in simple URL inspection)

---

### Option 3: Path-Based Routing

The target MCP server is encoded in the URL path.

**Client Configuration:**
```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://gateway.example.com/proxy/api.githubcopilot.com/mcp/"
    }
  }
}
```

**Gateway Behavior:**
1. Extract path after `/proxy/` prefix
2. Reconstruct upstream URL (assumes HTTPS by default)
3. Create session-scoped connection to upstream
4. Proxy MCP traffic with policy evaluation

**Pros:**
- Clean URL structure
- Easy to read and understand
- No query string encoding issues for simple URLs
- Familiar pattern (similar to CORS proxies)

**Cons:**
- Assumes schema (HTTPS) - need escape hatch for HTTP
- Path encoding issues for URLs with query strings
- Cannot easily represent: `https://example.com/mcp?token=abc&region=us`
- URL structure becomes ambiguous: is `/proxy/a.com/b/c` targeting `a.com/b/c` or `a.com/b` with path `/c`?

**Schema Handling Options:**
```
# Default HTTPS
/proxy/api.github.com/mcp

# Explicit schema prefix
/proxy/https/api.github.com/mcp
/proxy/http/internal.example.com/mcp
```

---

### Option 4: Session Initialization Header

The client sends the upstream URL only during the MCP `initialize` handshake. The gateway caches this for the session.

**Client Configuration:**
```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://gateway.example.com/mcp",
      "headers": {
        "X-MCP-Upstream": "https://api.githubcopilot.com/mcp/"
      }
    }
  }
}
```

**Gateway Behavior:**
1. On `initialize` request, extract `X-MCP-Upstream` header
2. Store upstream URL in session state
3. All subsequent requests in session use cached upstream
4. Session expiration cleans up connection

**Pros:**
- Header only needed on first request
- Clean session-based model
- Aligns with MCP session lifecycle
- Reduces per-request overhead after initialization

**Cons:**
- Same header support limitations as Option 2
- More complex state management
- What if header is missing on initialize? Error or fallback?
- Mid-session upstream changes require new session

---

### Option 5: Dedicated Registration Endpoint

The client first calls a registration endpoint to establish the upstream, then uses a returned session token for MCP traffic.

**Workflow:**
```
1. Client: POST /register
   Body: { "upstream": "https://api.github.com/mcp" }

2. Gateway: Returns session token
   Response: { "session_id": "abc123", "mcp_url": "/mcp/abc123" }

3. Client: Configure MCP to use /mcp/abc123
```

**Client Configuration (after registration):**
```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://gateway.example.com/mcp/abc123"
    }
  }
}
```

**Pros:**
- Explicit, RESTful registration flow
- Session URL is clean with no encoding issues
- Could support additional registration-time options (allowed tools, policy overrides)
- Works with any client that supports URLs

**Cons:**
- Two-step process - registration before MCP usage
- Requires out-of-band registration (curl, CLI tool, web UI)
- Session tokens could expire, requiring re-registration
- More complex developer workflow
- Not self-contained in agent configuration

---

### Option 6: Host Header / Subdomain Routing

The target MCP server is encoded as a subdomain of the gateway.

**Client Configuration:**
```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://github.gateway.example.com/mcp"
    }
  }
}
```

**Gateway Behavior:**
1. Extract subdomain from Host header (e.g., `github`)
2. Map subdomain to upstream URL via pattern or lookup
3. Proxy MCP traffic

**Mapping Options:**
```yaml
# Pattern-based (assumes convention)
# github.gateway.example.com → https://api.github.com/mcp

# Or explicit mapping in config
dynamic_routing:
  github: "https://api.githubcopilot.com/mcp/"
  linear: "https://api.linear.app/mcp"
```

**Pros:**
- Clean, intuitive URL structure
- Familiar pattern (virtual hosting)
- No encoding issues
- Easy to remember and type

**Cons:**
- Requires wildcard DNS configuration (`*.gateway.example.com`)
- Requires wildcard TLS certificate
- Subdomain must be a valid DNS label (no dots, limited characters)
- Still requires some form of mapping (not fully dynamic)
- Complex URLs like `api.githubcopilot.com` don't map cleanly to subdomains

---

### Option 7: mcp-remote Compatibility Mode

Support the same URL patterns that mcp-remote uses, enabling compatibility with existing configurations.

**Client Configuration:**
```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://gateway.example.com/mcp?url=https://api.github.com/mcp&header=Authorization:Bearer%20token"
    }
  }
}
```

**Gateway Behavior:**
1. Parse query parameters matching mcp-remote conventions
2. `url` parameter specifies upstream
3. `header` parameters specify pass-through headers
4. Proxy with extracted configuration

**Pros:**
- Compatible with existing mcp-remote configurations
- Familiar to users of mcp-remote
- Self-contained - headers and URL in single config line

**Cons:**
- Complex URL encoding required
- URL can become very long with multiple headers
- Mixing concerns (URL + auth) in query string
- Still has the "URL in URL" awkwardness

---

## Authentication Pass-Through

Regardless of which upstream specification option is chosen, we need a mechanism for passing authentication headers to the upstream MCP server.

### Challenge

For dynamic MCP configuration, authentication requirements are unknown ahead of time:
- GitHub uses `Authorization: Bearer <token>`
- Other servers may use `X-API-Key`, `X-Token`, or custom headers
- Some servers require multiple headers

### Approaches

#### A. Explicit Header Mapping

Client explicitly specifies which headers to pass through.

```json
{
  "headers": {
    "X-MCP-Upstream": "https://api.github.com/mcp",
    "X-MCP-Pass-Header": "Authorization",
    "Authorization": "Bearer ghp_xxxx"
  }
}
```

Gateway extracts headers named in `X-MCP-Pass-Header` and forwards them.

**Pros:** Explicit, secure, no accidental header leakage
**Cons:** Verbose configuration, requires knowing header names

#### B. Header Prefix Convention

Headers with a specific prefix are automatically forwarded.

```json
{
  "headers": {
    "X-MCP-Upstream": "https://api.github.com/mcp",
    "X-MCP-Forward-Authorization": "Bearer ghp_xxxx"
  }
}
```

Gateway strips `X-MCP-Forward-` prefix and forwards as `Authorization`.

**Pros:** Clear convention, flexible
**Cons:** Requires header renaming in client config

#### C. Allowlist-Based Pass-Through

Gateway has a configurable allowlist of headers to pass through.

```yaml
dynamic_routing:
  pass_through_headers:
    - Authorization
    - X-API-Key
    - X-Token
```

**Pros:** Simple client config, centralized control
**Cons:** Admin must anticipate all header names, less flexible

#### D. Pass All Headers (Filtered)

Gateway passes all headers except a denylist (e.g., `Host`, `Content-Length`, gateway-specific headers).

**Pros:** Maximum flexibility, minimal client config
**Cons:** Risk of leaking unintended headers, security concern

---

## Open Questions for Review

### Upstream Specification

1. **Which option(s) should we implement?**
   - Single approach for simplicity?
   - Multiple approaches for flexibility (e.g., header with query parameter fallback)?

2. **URL validation requirements?**
   - Open (any URL allowed)?
   - Allowlist (only approved domains)?
   - Blocklist (deny known-bad patterns)?
   - Mixed (allowlist for production, open for development)?

3. **Schema handling for path-based routing?**
   - If we use path-based, how do we handle HTTP vs HTTPS?
   - Default to HTTPS with explicit HTTP escape hatch?

### Authentication

4. **Which header pass-through approach?**
   - Explicit mapping vs prefix convention vs allowlist vs pass-all?

5. **Should we support the existing `pass_through` config format?**
   - Current format: `source_header`, `target_header`, `format`
   - Could dynamic routing use a simplified version?

### Session Management

6. **Session lifecycle for dynamic connections?**
   - Same timeout as static connections?
   - Different timeout for dynamic (shorter for security)?

7. **Tool prefixing for dynamic connections?**
   - Current pattern: `{client_name}__tool_name`
   - What's the client name for dynamic connections?
   - Derive from upstream URL? (e.g., `github-com__tool_name`)
   - Client-specified name?

### Security

8. **Rate limiting for dynamic connections?**
   - Per-session limits?
   - Per-upstream-domain limits?

9. **Audit logging for dynamic routing?**
   - Log upstream URL in audit entries?
   - Redact sensitive parts of URLs (tokens in query strings)?

### Resource Management

10. **Connection pooling for dynamic upstreams?**
    - Reuse connections across sessions to same upstream?
    - Or fully isolated per-session connections?

---

## Comparison Matrix

| Criteria | Query Param | Header | Path-Based | Session Init | Registration | Subdomain | mcp-remote |
|----------|-------------|--------|------------|--------------|--------------|-----------|------------|
| Client compatibility | High | Medium | High | Medium | High | High | Medium |
| Implementation complexity | Low | Low | Medium | Medium | High | High | Medium |
| URL cleanliness | Low | High | Medium | High | High | High | Low |
| Handles complex URLs | Medium | High | Low | High | High | Low | Medium |
| Self-contained config | Yes | Yes | Yes | Yes | No | Partial | Yes |
| Works today (no client fixes) | Yes | Partial | Yes | Partial | Yes | Yes | Yes |

---

## Next Steps

1. Review options and select preferred approach(es)
2. Define authentication pass-through strategy
3. Detail session management for dynamic connections
4. Design audit logging format for dynamic routing
5. Implementation plan

---

## References

- [Microsoft MCP Gateway](https://github.com/microsoft/mcp-gateway)
- [IBM ContextForge](https://ibm.github.io/mcp-context-forge/)
- [Envoy AI Gateway MCP Support](https://aigateway.envoyproxy.io/blog/mcp-implementation/)
- [mcp-remote](https://github.com/geelen/mcp-remote)
- [Claude Code Header Bug #14977](https://github.com/anthropics/claude-code/issues/14977)
- [MCP Authorization Specification](https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization)
