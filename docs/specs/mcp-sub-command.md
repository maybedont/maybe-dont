# Gateway Sub-Command Restructure

> **Status**: See [README.md](README.md)

## Overview

Restructure the `maybe-dont` CLI so that the gateway server functionality lives under a `gateway` sub-command rather than at the top level. This creates a cleaner hierarchy where the binary's two main capabilities (gateway server and CLI proxy) are peer sub-commands.

### Why "gateway" and not "mcp" or "server"

The sub-command was named `gateway` after researching terminology across the MCP ecosystem and traditional API infrastructure:

- **MCP ecosystem consensus**: Docker, IBM, Microsoft, Lasso, and most other MCP intermediary products call themselves "gateways." Products using "proxy" tend to be simpler transport bridges without policy/security layers.
- **Product identity**: The product is "Maybe Don't **Gateway**" — the sub-command matches the brand.
- **Specificity over generality**: "server" is too generic (everything is a server). "gateway" communicates the core value: it sits between agents and tools, intercepts requests, evaluates policies, and decides whether to allow them through. This applies to both MCP tool calls and CLI command validation.
- **Future-proof enough**: If the product evolves beyond gateway semantics, a rename would be warranted anyway since the mental model would have fundamentally changed.

## Goals

- Make the CLI hierarchy reflect the binary's dual nature: gateway server + CLI proxy
- Move gateway-specific flags (`--config-dir`, `--log-dir`, `--config-file-name`) under `gateway` so they don't pollute the top-level namespace
- Update help text to position Maybe Don't AI as "guardrails for agentic AI"
- Keep `test`, `cli`, `skill`, and `version` as top-level commands

## Non-Goals

- Splitting into separate binaries (future consideration, this restructure makes it easier)
- Backward compatibility shims for `maybe-dont start` (clean break)
- Changing any runtime behavior of the gateway or CLI proxy

## Current Command Hierarchy

```
maybe-dont                          # Prints help (loads config via PersistentPreRunE)
  --config-dir                      # Persistent flag (gateway only)
  --log-dir                         # Persistent flag (gateway only)
  --config-file-name                # Persistent flag (gateway only)
  ├── start                         # Start gateway
  ├── cli                           # CLI proxy (overrides PersistentPreRunE)
  ├── skill                         # Skill management (overrides PersistentPreRunE)
  │   ├── list
  │   └── view
  ├── test                          # Policy testing (overrides PersistentPreRunE)
  │   └── policies
  ├── config                        # Config management
  │   └── info                      # (overrides PersistentPreRunE)
  ├── defaults                      # Default config (overrides PersistentPreRunE)
  │   └── export
  └── version                       # Version info (overrides PersistentPreRunE)
```

**Problem**: 5 out of 7 sub-commands override `PersistentPreRunE` to skip config loading they don't need. The persistent flags are only used by `start`. The help text doesn't reflect the binary's dual purpose.

## Proposed Command Hierarchy

```
maybe-dont                          # Prints help (no PersistentPreRunE, no persistent flags)
  ├── gateway                       # Gateway server commands
  │   --config-dir                  # Persistent flag (scoped to gateway)
  │   --log-dir                     # Persistent flag (scoped to gateway)
  │   --config-file-name            # Persistent flag (scoped to gateway)
  │   ├── start                     # Start gateway
  │   ├── config                    # Config management
  │   │   └── info                  # Show resolved paths (overrides PersistentPreRunE)
  │   └── defaults                  # Default config (overrides PersistentPreRunE)
  │       └── export
  ├── cli                           # CLI proxy (unchanged)
  ├── test                          # Policy testing (unchanged)
  │   └── policies
  ├── skill                         # Skill management (unchanged)
  │   ├── list
  │   └── view
  └── version                       # Version info (unchanged)
```

**Improvements**:
- Only 2 commands need `PersistentPreRunE` overrides (down from 5)
- Gateway-specific flags scoped to where they're used
- Root command is lightweight — no config loading, no flags
- Top-level hierarchy reflects the binary's capabilities

## Implementation Details

### New file: `cmd/gateway.go`

New parent command that owns the gateway lifecycle:
- Defines `gatewayCmd` with `Use: "gateway"`
- Moves `PersistentPreRunE` from root (config loading, logger init, metrics init)
- Moves persistent flags: `--config-dir`, `--log-dir`, `--config-file-name`
- Registers `startCmd`, `configCmd`, `defaultsCmd` as children

### Modified: `cmd/root.go`

- Updated `Short` and `Long` descriptions (guardrails messaging)
- Removed `PersistentPreRunE` (moved to gateway.go)
- Removed persistent flag registration (moved to gateway.go)
- Package-level vars (`cfg`, `Logger`, etc.) remain here since they're shared across the `cmd` package

### Modified: `cmd/start.go`

- `init()`: Register under `gatewayCmd` instead of `rootCmd`

### Modified: `cmd/config.go`

- `init()`: Register under `gatewayCmd` instead of `rootCmd`
- `configInfoCmd.PersistentPreRunE`: Retained — still needs to skip gateway's config loading

### Modified: `cmd/defaults.go`

- `init()`: Register under `gatewayCmd` instead of `rootCmd`
- `defaultsCmd.PersistentPreRunE`: Retained — still needs to skip gateway's config loading

### Modified: `cmd/cli.go`, `cmd/skill.go`, `cmd/version.go`, `cmd/test.go`

- Remove `PersistentPreRunE` overrides (no longer needed since root has no `PersistentPreRunE`)

### cobra PersistentPreRunE inheritance

Only the most-specific `PersistentPreRunE` in the command chain runs:
- `gateway start` → `gatewayCmd.PersistentPreRunE` runs (loads config)
- `gateway config info` → `configInfoCmd.PersistentPreRunE` runs (no-op, skips config)
- `gateway defaults export` → `defaultsCmd.PersistentPreRunE` runs (no-op, skips config)
- `cli`, `test`, `skill`, `version` → no `PersistentPreRunE` in chain, nothing runs

## Build & Infrastructure Changes

| File | Change |
|------|--------|
| `Makefile` | `run` target: `./$(BINARY_NAME) start` → `./$(BINARY_NAME) gateway start` |
| `Dockerfile` | `CMD ["start"]` → `CMD ["gateway", "start"]` |
| `CLAUDE.md` | Update command examples |
| `.goreleaser.yaml` | No change (single binary) |
| `developer/scripts/runLocalDockerImage.sh` | No change (uses Docker CMD) |
| Homebrew formula | No change (generated by goreleaser) |

## Future Consideration: Binary Split

This restructure makes a future binary split straightforward:

- **`maybe-dont`** (full binary): All commands as described above
- **`maybe-dont-cli`** (lightweight ~5-8 MB): Only `cli`, `skill`, `version` commands

The `cli` sub-command + `internal/cliproxy` has zero external dependencies (stdlib only). A CLI-only binary would drop ~75% of the current 31 MB by excluding MCP, CEL, OpenAI, and viper dependencies.

The sub-command restructure is a prerequisite — it cleanly separates gateway concerns under `gateway` so the split is a matter of choosing which `cmd/*.go` files to include in each binary's build.
