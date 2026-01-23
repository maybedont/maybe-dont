# Lazy Discovery Synchronization

## Overview

This specification documents the synchronization mechanism used to deduplicate concurrent tool discovery requests for the same session.

## Problem Statement

There are two code paths that can trigger tool discovery for a session:

1. **Lazy discovery during `tools/list`**: When `tools/list` requests arrive before downstream clients have been discovered
2. **Explicit discovery via `maybedont__discover_tools`**: When the native tool is called (often triggered by stale session detection)

When multiple concurrent requests hit either path for the same session, each request was independently triggering discovery. This resulted in:

1. Multiple unnecessary connections to downstream MCP servers
2. Wasted resources (network, memory)
3. Potential for duplicate tool registrations
4. Noisy logs showing the same session performing discovery multiple times

**Evidence from production logs (lazy discovery):**
```json
{"ts":1769146122.549,"msg":"Performing lazy tool discovery","session_id":"mcp-session-17ba...","request_id":"f59b5b4bdd..."}
{"ts":1769146122.550,"msg":"Performing lazy tool discovery","session_id":"mcp-session-17ba...","request_id":"677c1dd026..."}
{"ts":1769146122.551,"msg":"Performing lazy tool discovery","session_id":"mcp-session-17ba...","request_id":"6e4d82810d..."}
```

**Evidence from production logs (discover_tools after stale session):**
```json
{"ts":1769147335.121,"msg":"Processing discover_tools request","request_id":"a5d6aedc..."}
{"ts":1769147335.121,"msg":"Session not found, creating new session for discovery","request_id":"a5d6aedc..."}
{"ts":1769147335.295,"msg":"Processing discover_tools request","request_id":"fdfc6639..."}
{"ts":1769147335.306,"msg":"Processing discover_tools request","request_id":"f8806cc2..."}
{"ts":1769147335.792,"msg":"Initialized MCP client","request_id":"a5d6aedc...","tools_count":40}
{"ts":1769147335.805,"msg":"Initialized MCP client","request_id":"f8806cc2...","tools_count":40}
{"ts":1769147335.852,"msg":"Initialized MCP client","request_id":"fdfc6639...","tools_count":40}
```

All requests have the same `session_id` but different `request_id` values, indicating a race condition. In the second example, three separate MCP client connections were created when only one was needed.

## Solution

Use `singleflight.Group` from `golang.org/x/sync/singleflight` to deduplicate concurrent discovery requests. Two separate singleflight groups are used for the two discovery paths to maintain clear separation:

```go
type Gateway struct {
    // ... other fields ...

    // lazyDiscoveryGroup deduplicates concurrent tools/list lazy discovery
    lazyDiscoveryGroup singleflight.Group

    // discoverToolsGroup deduplicates concurrent maybedont__discover_tools calls
    discoverToolsGroup singleflight.Group
}
```

### Path 1: Lazy Discovery (tools/list)

```go
func (g *Gateway) ensurePassThroughToolsDiscovered(ctx context.Context, sessionID string) []mcp.Tool {
    // Fast path: check if clients already exist
    if len(g.clientManager.GetSessionClientNames(sessionID)) > 0 {
        return g.getToolsFromExistingClients(ctx, sessionID)
    }

    // Use singleflight to deduplicate concurrent requests
    result, err, shared := g.lazyDiscoveryGroup.Do(sessionID, func() (interface{}, error) {
        // Only one goroutine executes this per session
        // Others wait and receive the same result
        return g.doActualDiscovery(ctx, sessionID)
    })

    if shared {
        g.logger.Debug(ctx, "Discovery result shared from concurrent request", ...)
    }
    // ...
}
```

### Path 2: Explicit Discovery (maybedont__discover_tools)

```go
func (g *Gateway) DiscoverPassThroughTools(ctx context.Context, sessionID string, clientName string) (*DiscoveryResult, error) {
    // Build singleflight key: sessionID/clientName (clientName may be empty for "all clients")
    singleflightKey := sessionID + "/" + clientName

    result, err, shared := g.discoverToolsGroup.Do(singleflightKey, func() (interface{}, error) {
        return g.doDiscoverPassThroughTools(ctx, sessionID, clientName)
    })

    if shared {
        g.logger.Debug(ctx, "Discover tools result shared from concurrent request", ...)
    }
    // ...
}
```

The `clientName` parameter is included in the key because `discover_tools` can target a specific client or all clients (empty string). Requests for different clients should not be deduplicated against each other.

## Alternatives Considered

| Approach | Retry on Failure | Complexity | Drawbacks | Best For |
|----------|------------------|------------|-----------|----------|
| **`singleflight.Group`** (chosen) | Yes | Low | First caller's context used for all | Request deduplication |
| `sync.Once` | No | Low | No retry after failure; needs per-session map | One-time initialization that cannot fail |
| `sync.Mutex` + state flag | Yes | Medium | More boilerplate; manual state management | Simple cases needing explicit retry control |
| Atomic CAS + state | Yes | High | Complex; error-prone | Lock-free requirements |
| Channel-based sync | Yes | High | Difficult to implement correctly | Complex coordination |

### Why Not `sync.Once`?

`sync.Once` guarantees execution exactly once, but:
1. **No retry on failure**: If discovery fails (network error, auth issue), subsequent requests cannot retry
2. **Per-session management**: Would need `map[sessionID]*sync.Once` with cleanup concerns
3. **Panic behavior**: If function panics, `sync.Once` still marks as "done"

### Why `singleflight.Group`?

1. **Designed for this pattern**: Deduplicates concurrent work with shared results
2. **Allows retry on failure**: Next call after failure can attempt again
3. **Natural keying**: Uses session ID as key
4. **Battle-tested**: Used in Go's standard library (e.g., DNS lookups in `net` package)
5. **Simple API**: Single `Do()` call handles all synchronization

### Context Handling Note

All concurrent callers share the first caller's context. If the first caller's context is canceled, all waiters receive the cancellation error. For lazy discovery (which typically completes quickly), this is acceptable.

## Testing

The implementation includes tests verifying:

### Lazy Discovery (tools/list)
1. **Deduplication**: 10 concurrent requests result in only 1 actual discovery
2. **Different sessions**: Requests for different sessions are NOT deduplicated
3. **Error propagation**: All waiters receive the same error on failure
4. **Retry after error**: Failed discovery can be retried (unlike `sync.Once`)

### Explicit Discovery (discover_tools)
1. **Deduplication**: Concurrent discover_tools calls for the same session/client result in only 1 connection
2. **Different clients**: Requests for different clients within the same session are NOT deduplicated
3. **Error propagation**: All waiters receive the same error on failure

See `internal/gateway/lazy_discovery_test.go` for test implementations.

## Files Modified

- `internal/gateway/gateway.go`: Added `lazyDiscoveryGroup` and `discoverToolsGroup` singleflight.Group fields
- `internal/gateway/server.go`: Wrapped `ensurePassThroughToolsDiscovered` with singleflight
- `internal/gateway/gateway.go`: Wrapped `DiscoverPassThroughTools` with singleflight
- `internal/gateway/lazy_discovery_test.go`: Added concurrent discovery tests for both paths
- `go.mod`: Added `golang.org/x/sync` dependency
