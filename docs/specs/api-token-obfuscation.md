# API Token Obfuscation Specification

## Status
**Draft** - Pending Review


## Overview

This specification documents the approach for obfuscating the metrics API token embedded in the Maybe Don't Gateway binary to prevent casual extraction via tools like `strings` or hex editors.

## Problem Statement

The current build process injects the metrics API token directly into the binary using Go's `-ldflags -X` mechanism:

```makefile
go build -ldflags "-X 'main.metricsAPIToken=$(METRICS_API_TOKEN)'" ...
```

This results in the token being stored as a plain string literal in the binary, easily extractable with:

```bash
strings maybe-dont | grep -i "token\|key\|api"
```

## Recommended Solution: Proxy Architecture (Remove Embedded Key)

> **Note**: Garble obfuscation was previously considered and implemented, but has been removed due to build performance impact and debugging concerns. See "Historical Note" section for details.

The recommended long-term solution is to **remove the embedded API key entirely** and use a proxy-based authentication pattern. This approach:

1. Eliminates the need for obfuscation
2. Solves the key rotation problem for on-premise deployments
3. Provides revocation capability per-build
4. Enables better audit trails

See "Proxy-Based Authentication" section below for the full design.

### Previous Approach: garble with -literals (Deprioritized)

Garble was previously used as a drop-in replacement for `go build` to obfuscate string literals:

```makefile
# This approach was implemented and later removed
build-release:
	garble -literals -tiny -seed=random build -ldflags "..." -o $(BINARY_NAME) ./
```

This was removed in commit `2ed3419` due to:
- ~2x build time overhead
- Required `large` CI runner
- `-tiny` flag made production debugging difficult (silent panics)

## Trade-off Analysis

### Alternative Approaches Considered

| Approach | Extraction Difficulty | Build Complexity | Runtime Impact | Notes |
|----------|----------------------|------------------|----------------|-------|
| Plain ldflags | Trivial | None | None | Current approach |
| XOR obfuscation | Moderate | Low | Negligible | Custom encode/decode logic |
| garble -literals | High | Moderate | <5% | Was implemented, removed due to build perf |
| **Proxy architecture** | Very High | High | Network dependent | **Recommended** - eliminates embedded key |

### Why Proxy Architecture?

1. **Eliminates the root problem** - No embedded key to protect
2. **Key rotation independence** - Backend changes don't affect clients
3. **Revocation capability** - Can blocklist specific builds
4. **Audit trail** - Know which versions are actively reporting

### Why Not Garble? (Lessons Learned)

Garble was implemented and later removed:

1. **Build performance** - ~2x slower builds, required `large` CI runner
2. **Debugging impact** - `-tiny` flag caused silent panics without stack traces
3. **Doesn't solve key rotation** - Embedded key still can't be rotated without new builds
4. **Security through obscurity** - Determined attackers can still extract

## garble Impact Analysis

### Build-Time Performance

| Metric | Impact |
|--------|--------|
| Initial build | ~2x slower (two passes required) |
| Incremental builds | Comparable to `go build` (cache-aware) |
| CI/CD impact | Moderate; consider caching `.cache/garble` |

Garble performs two builds: one to load and type-check input code, and one to produce the obfuscated output. The first build of a project will be slower, but subsequent incremental builds leverage Go's build cache.

### Runtime Performance

| Flag | Runtime Impact | Notes |
|------|---------------|-------|
| Default (no flags) | Negligible | Only identifiers obfuscated |
| `-literals` | <5% typical | String reconstruction at runtime |
| `-tiny` | 2-5% smaller binary | Strips debug info, panics silently |

The `-literals` flag replaces string literals with more complex expressions that resolve to the same value at runtime. For most applications, the overhead is minimal (<5%), but performance-critical string-heavy code should be benchmarked.

### Debugging Impact

#### Development Builds

**Recommendation**: Do NOT use garble for development builds.

- Use plain `go build` for local development
- Preserves full debugging capability with `dlv` or IDE debuggers
- Stack traces remain readable
- Source maps work correctly

#### Production/Release Builds

| Aspect | Impact | Mitigation |
|--------|--------|------------|
| Stack traces | Obfuscated symbols | Use `garble reverse` with saved seed |
| Panic output | Symbols unreadable | Save build seed for post-mortem analysis |
| Line numbers | Obfuscated | Use consistent seed per version |
| Source positions | Removed with `-tiny` | Avoid `-tiny` if debugging needed |

**Important**: To decode obfuscated stack traces:

```bash
# Save the seed used during build
garble -seed=random build ...
# Record the seed from output

# Later, reverse a stack trace
garble -seed=<saved-seed> reverse /path/to/panic.txt
```

#### Field Debugging

For production issues:
1. Reproduce with development build if possible
2. Use `garble reverse` with the release seed to decode stack traces
3. Structured logging (zap) remains functional - log messages are not obfuscated
4. Consider keeping one non-obfuscated binary per release for debugging

### Binary Size Impact

| Configuration | Size Change |
|--------------|-------------|
| Default garble | +5-10% |
| With `-literals` | +5-15% |
| With `-tiny` | -2-5% (net reduction) |

### Known Limitations

1. **Exported methods** - Never obfuscated (required for interfaces/reflection)
2. **Constants** - `const` declarations cannot be obfuscated (compile-time resolved)
3. **Go plugins** - Not supported
4. **Reflection-heavy code** - May require `//go:embed` hints for types used with JSON/reflection
5. **Build info APIs** - `runtime/debug.ReadBuildInfo()` will not work
6. **Timezone loading** - May be affected if relying on `runtime.GOROOT`

### Security Considerations

Obfuscation is **not encryption**. A determined attacker with sufficient resources can still:

- Use runtime analysis to extract secrets
- Reverse literal obfuscation with tools like GoStringUngarbler
- Analyze control flow to understand logic

Garble raises the bar significantly against:
- Casual inspection (`strings`, hex editors)
- Automated credential scanning
- Low-effort reverse engineering

For highly sensitive credentials, consider runtime fetching from a secrets manager instead.

## On-Premise Deployment Considerations

### The Key Rotation Problem

Even with effective obfuscation, embedding API keys in binaries creates a fundamental problem for on-premise deployments: **key rotation breaks deployed software**.

When the metrics API key needs to be rotated (due to compromise, policy, or routine security hygiene), all previously shipped binaries containing the old key will stop functioning. This creates an untenable situation:

- Customers running older versions lose metrics reporting silently
- No mechanism exists to update the key without shipping new binaries
- Forces unnecessary upgrade cycles for a backend credential change
- Old binaries in the wild become a support burden

### Recommended Architecture: Proxy-Based Authentication

Instead of embedding the actual API key, route metrics through an intermediate proxy service that:

1. **Holds the real API key** server-side (can be rotated without client changes)
2. **Rate limits** requests per identified client
3. **Authenticates clients** via a non-obvious protocol

#### Client Identification via Binary Hash

Each release binary has a unique SHA256 hash. This hash can be:

1. Computed at build time and embedded in the binary via ldflags
2. Sent to the proxy as part of an obfuscated authentication protocol
3. Validated server-side against a registry of known shipped builds

```go
// Embedded at build time
var binaryHash string // SHA256 of the release binary

// Used at runtime for proxy authentication
func getBinaryIdentifier() string {
    return binaryHash
}
```

#### Protocol Obfuscation

To make the authentication mechanism non-obvious to reverse engineering, the proxy protocol should:

1. **Split identifying information across multiple headers**
   - Don't use a single `X-Binary-Hash` header
   - Fragment the hash across 2-3 headers with misleading names

2. **Add decoy headers with garbage data**
   - Include headers that look meaningful but are ignored
   - Vary garbage content per request to frustrate pattern analysis

3. **Use non-obvious header names**
   - Avoid names like `X-Auth`, `X-Token`, `X-Identity`
   - Use mundane names like `X-Cache-Hint`, `X-Correlation-Id`, `X-Request-Flags`

4. **Encode values non-obviously**
   - Don't send raw hex SHA256
   - Transform, split, or interleave with noise

Example protocol design:

```
POST /v1/ingest HTTP/1.1
Host: metrics-proxy.example.com
Content-Type: application/json
X-Trace-Context: a]3f8b2c1e...     # First 16 chars of hash, with garbage prefix
X-Request-Flags: 7k#9d4e5f6a...    # Next 16 chars, different garbage prefix
X-Cache-Hint: m!1b2c3d4e...        # Next 16 chars
X-Session-Nonce: x@5f6a7b8c...     # Final 16 chars
X-Debug-Level: 3                   # Decoy - ignored
X-Retry-Policy: exponential        # Decoy - ignored
X-Client-Timestamp: 1705432800     # Decoy - ignored
```

The proxy reconstructs the hash by:
1. Stripping the 2-character garbage prefix from each relevant header
2. Concatenating in the correct order
3. Validating against the known builds registry

#### Proxy Responsibilities

| Function | Description |
|----------|-------------|
| Hash validation | Verify concatenated hash matches a known shipped build |
| Rate limiting | Per-hash rate limits to prevent abuse |
| Key injection | Add the real API key to upstream requests |
| Anomaly detection | Flag unusual patterns (replay, enumeration attempts) |
| Metrics aggregation | Optional: batch/aggregate before forwarding |

#### Security Properties

This approach provides:

- **Key rotation independence** - Backend key changes don't affect clients
- **Revocation capability** - Individual build hashes can be blocklisted
- **Audit trail** - Know which versions are actively reporting
- **Defense in depth** - Multiple layers of validation
- **Graceful degradation** - Proxy can return success even if upstream fails

It does NOT provide:

- **True authentication** - A determined attacker can extract the hash and protocol
- **Replay prevention** - Without timestamps/nonces, requests can be replayed
- **Tamper resistance** - Modified binaries could send any hash

This is security through obscurity combined with practical access control. The goal is raising the effort required to abuse the system, not making it cryptographically impossible.

### Implementation Phases

**~~Phase 1: Obfuscation (Short-term)~~** - Deprioritized
- ~~Implement garble for release builds~~
- Was implemented and removed due to build performance impact
- See "Historical Note" section below for details

**Phase 2: Proxy Architecture (Medium-term)** - Recommended Next Step
- Deploy metrics proxy service
- Implement hash-based client identification
- Update client to use proxy with obfuscated protocol
- Remove direct API key from binary

**Phase 3: Enhanced Security (Long-term)**
- Add request signing with embedded private key
- Implement timestamp-based replay prevention
- Consider certificate pinning for proxy connection

## Historical Note: Garble Was Implemented and Removed

Garble was previously implemented in this project and subsequently removed. The commit history shows:

| Commit | Date | Description |
|--------|------|-------------|
| `cd82aac` | Nov 2, 2025 | Added garble to CI |
| `3007c0a` | Nov 3, 2025 | Fixed garble to work without const (refactored metrics config passing) |
| `2ed3419` | Nov 16, 2025 | **Removed garble** - "fix: speed up build by removing garble" |

### Configuration That Was Removed

```yaml
# .goreleaser.yaml (removed)
builds:
  - tool: garble
    command: "-literals"
    flags: [ "-tiny", "-seed=random", "build"]
```

The build was also downgraded from `runs-on: large` to `runs-on: ubuntu-latest`, indicating garble's resource requirements were significant.

### Why It Was Removed

The stated reason was **build performance**. However, the `-tiny` flag also had debugging implications:
- Panics exit silently without stack traces
- Source positions removed from binary
- Makes production debugging significantly harder

### Recommendation Going Forward

Rather than re-adding garble for API key obfuscation, the longer-term solution is to:

1. **Remove the embedded API key entirely**
2. **Implement Phase 2 (Proxy Architecture)** as described above
3. This eliminates the need for obfuscation and solves the key rotation problem

If garble is reconsidered in the future for other reasons:
- Avoid `-tiny` flag to preserve debuggability
- Consider whether `-literals` overhead is acceptable
- Ensure CI has adequate resources (previously required `large` runner)

## Implementation Checklist

### ~~Phase 1: Obfuscation~~ (Deprioritized)

Garble obfuscation has been deprioritized in favor of moving directly to the proxy architecture, which eliminates the need for embedded API keys entirely.

- [x] ~~Install garble~~ (was implemented, then removed)
- [x] ~~Add build target using garble~~ (was implemented, then removed)
- [x] ~~Update `.goreleaser.yaml`~~ (was implemented, then removed)
- [ ] ~~Document seed storage process~~ (not needed if not using garble)
- [ ] ~~Benchmark runtime performance~~ (not needed if not using garble)

### Phase 2: Proxy Architecture

- [ ] Design and document proxy API specification
- [ ] Implement metrics proxy service
- [ ] Create known-builds registry (hash -> version mapping)
- [ ] Implement hash computation at build time
- [ ] Update goreleaser to record binary hashes
- [ ] Implement obfuscated header protocol in client
- [ ] Add rate limiting to proxy
- [ ] Deploy proxy infrastructure
- [ ] Remove direct API key embedding from client
- [ ] Update opt-out documentation

### Phase 3: Enhanced Security (Optional)

- [ ] Evaluate request signing requirements
- [ ] Implement timestamp-based replay prevention
- [ ] Consider certificate pinning for proxy connection
- [ ] Add anomaly detection to proxy

## References

- [garble GitHub Repository](https://github.com/burrowers/garble)
- [Go Obfuscation Techniques (DEV Community)](https://dev.to/shrsv/securing-golang-binaries-obfuscation-techniques-that-work-1e1)
- [Ungarble: Deobfuscating Golang (Invokere)](https://invokere.com/posts/2025/03/ungarble-deobfuscating-golang-with-binary-ninja/)
