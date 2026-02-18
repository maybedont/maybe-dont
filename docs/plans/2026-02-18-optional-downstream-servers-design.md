# Design: Optional Downstream MCP Servers

**Issue:** [#122](https://github.com/maybedont/maybe-dont/issues/122)
**Date:** 2026-02-18

## Problem

The gateway unconditionally requires at least one downstream MCP server, even when used purely as a CLI proxy or with only native tools. This blocks users who don't need MCP proxying.

## Approach

Remove the hard validation requirement. Add an INFO log at startup when no downstream servers are configured so users aren't silently misconfigured.

## Changes

### 1. Remove validation gate (`internal/config/config.go`)

Delete lines 1437-1440 — the `len(cfg.DownstreamMCPServers) == 0` check in `validateConfigWithOptions()`.

### 2. Add startup INFO log (`internal/gateway/gateway.go`)

In `Gateway.Start()`, after the debug config print:

```go
if len(g.config.DownstreamMCPServers) == 0 {
    g.logger.Info(ctx, "No downstream MCP servers configured — gateway will serve native tools and CLI validation only")
}
```

### 3. Update config tests (`internal/config/config_test.go`)

- Remove assertion on "at least one downstream MCP server" in `TestLoadConfig_EmptyDirectory`
- Update `TestValidateConfigWithContext_NoConfigFileShowsGuidance` — no longer triggers downstream server error
- Add test: config with no downstream servers passes validation

### 4. Add gateway integration test

Verify gateway starts with no downstream servers configured — sessions initialize normally, native tools work, unknown tools get standard SDK "tool not found" handling.

## What doesn't change

- `ClientManager` / `DiscoverAllCapabilities()` — already handles empty maps
- `list_servers_tool.go` — already returns "No downstream MCP servers" message
- Native tools — registered unconditionally
- CLI validation handler — independent of downstream servers
- Session initialization — works as before; `onRequestInitialization` passes unknown prefixes through to SDK
- Default config YAML — no changes needed
