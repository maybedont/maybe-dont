# CLI Proxy for AI Agent Tool Validation

## Status
**Draft** - Pending Review

## Overview

Extend the Maybe Don't CLI to act as a proxy for CLI commands executed by AI agents. This allows the same policy validation (CEL rules, AI validation) applied to MCP tool calls to be applied to traditional CLI tool invocations.

## Goals

1. Provide a CLI wrapper that intercepts commands and validates them against the central Maybe Don't gateway
2. Reuse existing MCP gateway validation infrastructure (CEL engine, AI engine, audit logging)
3. Simple invocation: `maybe-dont cli -- <command> [args...]`
4. Server-controlled command filtering (no client-side allowlists)
5. Transparent execution - after validation, behave identically to direct command execution
6. Support AI agent workflows via skill/instruction definitions

## Non-Goals

1. Replacing MCP as the primary agent-to-tool protocol
2. Validating commands executed outside AI agent contexts
3. Preventing determined users from bypassing validation
4. Protecting against malicious users (threat model is AI agent mistakes, not adversaries)

## Architecture

### System Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        User Workstation                             │
│  ┌─────────────┐    ┌──────────────────┐    ┌────────────────────┐  │
│  │  AI Agent   │───▶│  maybe-dont cli  │───▶│  Target CLI (gh)   │  │
│  │  (Claude)   │    │                  │    │                    │  │
│  └─────────────┘    └────────┬─────────┘    └────────────────────┘  │
│                              │                        ▲             │
│                              │ HTTP/HTTPS             │ syscall.Exec│
│                              │                        │             │
└──────────────────────────────┼────────────────────────┼─────────────┘
                               │                        │
                               ▼                        │
┌──────────────────────────────────────────────────────────────────────┐
│                     Central Infrastructure                           │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │                    Maybe Don't Gateway                         │  │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌────────────────┐  │  │
│  │  │  Native Tool:   │  │   CEL Engine    │  │   AI Engine    │  │  │
│  │  │  validate_cli   │─▶│   (policies)    │─▶│   (OpenAI)     │  │  │
│  │  └─────────────────┘  └─────────────────┘  └────────────────┘  │  │
│  │                                │                               │  │
│  │                                ▼                               │  │
│  │                       ┌─────────────────┐                      │  │
│  │                       │   Audit Log     │                      │  │
│  │                       └─────────────────┘                      │  │
│  └────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
```

### Request Flow

1. AI agent executes: `maybe-dont cli -- gh pr comment 123 --body "LGTM"`
2. CLI wrapper parses arguments, extracts command and args
3. CLI wrapper sends HTTP POST to gateway's native tool endpoint
4. Gateway validates via CEL rules and/or AI policies
5. Gateway returns: allowed/denied + validation_required flag
6. If allowed: CLI wrapper uses `syscall.Exec` to replace itself with target command
7. If denied: CLI wrapper outputs error to stderr, exits non-zero

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Transport | HTTP/HTTPS only | Gateway is centrally managed; stdio/SSE not applicable |
| Authentication | None (V1) | Defer to future; network isolation assumed initially |
| Execution | `syscall.Exec` (Unix) | Transparent replacement; identical behavior to direct execution |
| Fallback | `exec.Command` (Windows) | Windows lacks direct exec replacement |
| Fail mode | Fail-open (V1) | Allow command if gateway unreachable; configurable later |

## CLI Interface

### Invocation Syntax

```bash
maybe-dont cli [flags] -- <command> [args...]
```

The `--` separator is **required**. It explicitly delineates flags for `maybe-dont cli` from the proxied command.

### Examples

```bash
# GitHub CLI
maybe-dont cli -- gh pr comment 123 --body "This looks good!"

# AWS CLI
maybe-dont cli -- aws s3 cp s3://bucket/file.txt ./local.txt

# Kubernetes
maybe-dont cli -- kubectl delete pod my-pod

# With flags
maybe-dont cli --server https://gateway.internal:8443 -- gh pr merge 123
maybe-dont cli -s https://gateway.internal:8443 -- gh pr merge 123
maybe-dont cli --timeout 30s -- aws s3 sync s3://bucket ./local
maybe-dont cli --dry-run -- rm -rf /tmp/data
```

### Error Handling

**Missing separator:**
```
$ maybe-dont cli gh pr comment 123
Error: missing command separator
Usage: maybe-dont cli [flags] -- <command> [args...]
```

**No command provided:**
```
$ maybe-dont cli --
Error: no command specified
Usage: maybe-dont cli [flags] -- <command> [args...]
```

**Command denied:**
```
$ maybe-dont cli -- gh repo delete my-repo
Error: Command blocked by security policy
Policy: no-destructive-github-actions
Reason: Repository deletion is not permitted without explicit approval
```

**Gateway unreachable (fail-open):**
```
$ maybe-dont cli -- gh pr comment 123 --body "LGTM"
Warning: Unable to reach validation gateway, proceeding without validation
# Command executes
```

### V1 Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--server` | `-s` | string | `http://localhost:8080` | Gateway endpoint URL |
| `--timeout` | | duration | `30s` | Validation request timeout |
| `--dry-run` | | bool | `false` | Validate only, don't execute |

### Future Flags (not in V1)

| Flag | Description |
|------|-------------|
| `--fail-closed` | Deny command if gateway unreachable |
| `--output-format` | `text` or `json` for error output |
| `--session-id` | Session identifier for cache scoping |

## Native Tool Design

The gateway exposes a native MCP tool that the CLI wrapper calls to validate commands.

### Tool Definition

```json
{
  "name": "maybedont__validate_cli",
  "description": "Validates a CLI command against security policies before execution",
  "inputSchema": {
    "type": "object",
    "properties": {
      "command": {
        "type": "string",
        "description": "The CLI executable (e.g., 'gh', 'aws', 'kubectl')"
      },
      "arguments": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Command arguments"
      },
      "working_directory": {
        "type": "string",
        "description": "Current working directory where command will execute"
      },
      "client_info": {
        "type": "object",
        "properties": {
          "hostname": { "type": "string" },
          "os": { "type": "string" },
          "os_version": { "type": "string" },
          "arch": { "type": "string" },
          "shell": { "type": "string" },
          "cli_version": { "type": "string" }
        }
      }
    },
    "required": ["command", "arguments"]
  }
}
```

### Request Example

```json
{
  "command": "gh",
  "arguments": ["pr", "comment", "123", "--body", "This looks good!"],
  "working_directory": "/home/user/project",
  "client_info": {
    "hostname": "dev-workstation-1",
    "os": "darwin",
    "os_version": "24.1.0",
    "arch": "arm64",
    "shell": "/bin/zsh",
    "cli_version": "1.2.0"
  }
}
```

### Response Structure

Mirrors existing `ValidationResults` structure with addition of `server_version`:

```json
{
  "allowed": true,
  "validation_required": true,
  "message": "Command approved by policy",
  "action_reason": "",
  "server_version": "1.3.0",
  "results": [
    {
      "policy_name": "github-cli-policy",
      "policy_type": "cel",
      "action": "allow",
      "message": "PR comments are permitted"
    }
  ]
}
```

**Denied response:**

```json
{
  "allowed": false,
  "validation_required": true,
  "message": "Policy 'no-destructive-github-actions' denied: Repository deletion is not permitted",
  "action_reason": "request_policy",
  "server_version": "1.3.0",
  "results": [
    {
      "policy_name": "no-destructive-github-actions",
      "policy_type": "ai",
      "action": "deny",
      "message": "Repository deletion could cause irreversible data loss"
    }
  ]
}
```

**No validation required (command not in server allowlist):**

```json
{
  "allowed": true,
  "validation_required": false,
  "message": "Command does not require validation",
  "server_version": "1.3.0",
  "results": []
}
```

### HTTP Endpoint

The CLI wrapper calls the gateway via HTTP, invoking the native tool:

```
POST /mcp/tools/call HTTP/1.1
Host: gateway.internal:8443
Content-Type: application/json

{
  "method": "tools/call",
  "params": {
    "name": "maybedont__validate_cli",
    "arguments": {
      "command": "gh",
      "arguments": ["pr", "comment", "123", "--body", "LGTM"],
      "working_directory": "/home/user/project",
      "client_info": { ... }
    }
  }
}
```

## Server-Side Configuration

The gateway controls which CLI commands require validation. No client-side allowlists.

### Configuration Structure

```yaml
# maybe-dont.yaml

cli_validation:
  enabled: true

  # Commands that require validation (allowlist)
  # If empty, ALL commands require validation
  validate_commands:
    - gh
    - aws
    - kubectl
    - gcloud
    - az
    - terraform
    - curl
    - docker
```

### Unified Policy Rules

CEL and AI rules support both MCP tool calls and CLI commands in a single rule definition.

#### CEL Rules

Use `expression` for MCP tool calls and `cli_expression` for CLI commands. At least one must be defined.

```yaml
# cel_request_rules.yaml

rules:
  # Both MCP and CLI
  - name: no-destructive-github-actions
    description: "Block destructive GitHub operations"

    # MCP tool expression
    expression: |
      tool.name == "github__delete_repo" ||
      (tool.name == "github__update_repo" && tool.arguments.delete == true)

    # CLI expression
    cli_expression: |
      cli.command == "gh" &&
      cli.arguments.size() >= 2 &&
      cli.arguments[0] == "repo" &&
      cli.arguments[1] == "delete"

    action: deny
    message: "Destructive GitHub operations are not permitted"

  # MCP only (no CLI equivalent)
  - name: no-bulk-email
    expression: |
      tool.name == "email__send_bulk"
    action: deny
    message: "Bulk email not permitted"

  # CLI only (no MCP equivalent)
  - name: no-sudo
    cli_expression: |
      cli.command == "sudo"
    action: deny
    message: "sudo not permitted for AI agents"
```

**Evaluation Logic:**

| `expression` | `cli_expression` | MCP Tool Call | CLI Command |
|--------------|------------------|---------------|-------------|
| ✓ | ✓ | Evaluate `expression` | Evaluate `cli_expression` |
| ✓ | ✗ | Evaluate `expression` | Skip rule |
| ✗ | ✓ | Skip rule | Evaluate `cli_expression` |
| ✗ | ✗ | Invalid (config error) | Invalid (config error) |

#### AI Rules

AI rules use a unified `operation` structure that works for both MCP and CLI:

```yaml
# ai_request_rules.yaml

rules:
  - name: general-safety-check
    description: "AI evaluation of operation safety"
    prompt: |
      Evaluate this operation for safety:

      Type: {{ operation.type }}          # "mcp_tool" or "cli"
      Name: {{ operation.name }}          # "github__create_issue" or "gh"
      Arguments: {{ operation.arguments }} # JSON representation

      Consider:
      1. Could this operation cause data loss?
      2. Could this operation affect production systems?
      3. Could this operation expose sensitive information?
      4. Is this appropriate for an AI agent to execute autonomously?

      Respond in JSON format:
      {
        "action": "allow" or "deny",
        "reasoning": "brief explanation",
        "confidence": 0.0-1.0
      }
    action_on_concern: deny
```

**Gateway Normalization:**

For MCP tool call:
```json
{"type": "mcp_tool", "name": "github__create_issue", "arguments": {"repo": "foo", "title": "bar"}}
```

For CLI command:
```json
{"type": "cli", "name": "gh", "arguments": ["issue", "create", "-R", "foo", "-t", "bar"]}
```

### CEL Context Variables

The CEL engine receives a `cli` object for CLI commands:

| Variable | Type | Description |
|----------|------|-------------|
| `cli.command` | string | The CLI executable name |
| `cli.arguments` | list(string) | Command arguments |
| `cli.working_directory` | string | Working directory path |
| `cli.client_info.hostname` | string | Client hostname |
| `cli.client_info.os` | string | Operating system |
| `cli.client_info.arch` | string | CPU architecture |
| `cli.client_info.shell` | string | User's shell |
| `cli.client_info.cli_version` | string | CLI wrapper version |

## Binary Distribution

### V1: Full Binary

The CLI proxy is included in the main `maybe-dont` binary. Users run `maybe-dont cli -- <command>`.

**Rationale:**
- Single build/release artifact per platform
- Users can run local gateway for testing if needed
- Go linker excludes unused code paths
- Simplifies versioning (one version number)

### Code Organization

Structure code to enable future split if needed:

```
internal/
  cliproxy/              # CLI proxy logic (isolated package)
    proxy.go             # Main proxy logic
    client.go            # HTTP client for gateway
    exec.go              # Command execution (syscall.Exec)
    exec_windows.go      # Windows fallback (exec.Command)
  gateway/               # Existing gateway logic
    ...
cmd/
  root.go
  start.go               # `maybe-dont start` - imports gateway
  version.go
  cli.go                 # `maybe-dont cli` - imports cliproxy only
  skill.go               # `maybe-dont skill` - skill management
```

### Future: Slim Binary (if needed)

If binary size becomes a concern, create separate entry point:

```
cmd/
  maybe-dont-cli/        # Separate main package
    main.go              # Only imports cliproxy
```

Build with:
```bash
go build -o maybe-dont-cli ./cmd/maybe-dont-cli
```

This produces a smaller binary (~5-10MB vs ~15-30MB) that only includes:
- Cobra (arg parsing)
- HTTP client
- syscall/exec

### Decision Criteria for Split

Consider splitting if:
- Full binary exceeds 50MB
- Users complain about download size
- Deployment constraints require minimal footprint

For now: **full binary, organized for easy split later**.

## Response Caching

### V1: No Caching (Always Call Server)

Every CLI invocation calls the gateway. Server decides validation behavior.

**Flow:**
1. CLI wrapper calls gateway for every command
2. Server returns `validation_required: true/false` based on allowlist
3. Server returns `allowed: true/false` based on policy evaluation
4. CLI wrapper executes or denies

**Pros:**
- Simplest implementation
- Central control is absolute
- No cache invalidation complexity

**Cons:**
- Network latency on every command (10-50ms typical)
- Requires connectivity for all commands

### V2 Optimization: TTL-Based Response Caching

Cache validation responses locally with time-based expiration.

**Flow:**
1. CLI wrapper checks local cache for command
2. If cached and not expired → use cached response
3. If not cached or expired → call server, cache response
4. Execute or deny based on response

**Cache Structure:**

```json
// $XDG_CACHE_HOME/maybe-dont/cli-cache.json
// Fallback: ~/.cache/maybe-dont/cli-cache.json
// Windows: %LOCALAPPDATA%\maybe-dont\cli-cache.json
{
  "version": 1,
  "entries": {
    "cat": {
      "allowed": true,
      "validation_required": false,
      "cached_at": "2024-01-15T10:00:00Z",
      "ttl_seconds": 3600
    },
    "gh": {
      "allowed": true,
      "validation_required": true,
      "cached_at": "2024-01-15T10:00:00Z",
      "ttl_seconds": 300
    }
  }
}
```

**TTL Strategy:**
- Commands with `validation_required: false` → longer TTL (60 minutes)
- Commands with `validation_required: true` → shorter TTL (5 minutes) or no caching
- Server can specify TTL in response: `"cache_ttl_seconds": 3600`

### V3 Optimization: Server-Managed Allowlist

Server provides full allowlist via `Last-Modified` header pattern.

**Flow:**
1. CLI wrapper stores last sync timestamp
2. On each call, server returns `X-MaybeDont-Config-Modified: <timestamp>`
3. If timestamp newer than local → fetch updated allowlist
4. Use local allowlist to skip server calls for non-validated commands

**Deferred:** More complex, implement only if V2 caching is insufficient.

### Session ID Enhancement (V2/V3)

If AI agent provides session ID, scope cache by session:

```bash
maybe-dont cli --session-id abc123 -- gh pr comment 123
```

Cache key becomes `{command}:{session_id}` - new session invalidates cache.

## Skill Management Subcommand

### Command Structure

```
maybe-dont skill <command> [args] [flags]
```

### Commands

**`maybe-dont skill list`**

Lists all available embedded skills.

```bash
$ maybe-dont skill list
Available skills:
  cli    Claude Code skill for CLI command validation

Use 'maybe-dont skill view <name>' to output a skill definition.
```

**`maybe-dont skill view <name> [--format <format>]`**

Outputs the specified skill definition to stdout.

```bash
# Default (Claude Code format)
$ maybe-dont skill view cli
# <outputs markdown skill definition>

# Deploy to project
$ maybe-dont skill view cli > .claude/skills/maybe-dont-cli.md

# Future: other formats
$ maybe-dont skill view cli --format cursor > .cursorrules
$ maybe-dont skill view cli --format copilot > .github/copilot-instructions.md
```

### V1 Scope

| Feature | V1 | Future |
|---------|-----|--------|
| `skill list` | ✓ | |
| `skill view cli` | ✓ | |
| `--format claude` (default) | ✓ | |
| `--format cursor` | | ✓ |
| `--format copilot` | | ✓ |
| Additional skills | | ✓ |

### Implementation

```go
//go:embed skills/cli.md
var cliSkillClaude string

// Future: embed multiple formats
//go:embed skills/cli.cursorrules
var cliSkillCursor string
```

## AI Agent Integration

### Claude Code Skill Definition

```markdown
# maybe-dont-cli

## Description
Routes all CLI commands through the Maybe Don't security gateway for validation before execution.

## Instructions

All shell/CLI commands must be executed through the Maybe Don't CLI proxy:

\`\`\`bash
maybe-dont cli -- <command> [args...]
\`\`\`

### Examples

\`\`\`bash
# GitHub CLI
maybe-dont cli -- gh pr comment 123 --body "Looks good!"

# AWS CLI
maybe-dont cli -- aws s3 ls s3://my-bucket

# Kubernetes
maybe-dont cli -- kubectl get pods -n production

# Git operations
maybe-dont cli -- git push origin main

# File operations
maybe-dont cli -- cat README.md
\`\`\`

### Important

1. **Always use the \`--\` separator** between \`maybe-dont cli\` and your command
2. The server determines which commands require validation
3. If a command is denied, read the error message for guidance on how to proceed
4. Do not attempt to bypass validation by running commands directly

### Error Handling

If a command is denied, you will see an error like:
\`\`\`
Error: Command blocked by security policy
Policy: no-destructive-github-actions
Reason: Repository deletion is not permitted without explicit approval
\`\`\`

When this happens:
- Explain to the user why the command was blocked
- Suggest alternatives if available
- Ask the user if they want to proceed manually
```

### Skill Deployment (Claude Code)

Place the skill file in one of:
- Project: `.claude/skills/maybe-dont-cli.md`
- User: `~/.claude/skills/maybe-dont-cli.md`
- Plugin: Via Claude Code plugin system

### Generic Guidance for Other AI Agents

The core instruction for any AI agent:

> All CLI commands that interact with external services must be prefixed with `maybe-dont cli --`. This routes commands through a security validation gateway. If a command is denied, explain the denial reason to the user and suggest alternatives.

**Adaptation patterns by agent:**

| Agent | Configuration Location |
|-------|----------------------|
| Cursor | `.cursorrules` in project root |
| GitHub Copilot | `.github/copilot-instructions.md` |
| Aider | `.aider.conf.yml` or conventions file |
| Windsurf | Workspace rules/context files |
| Generic LLM | System prompt or custom instructions |

**Key elements to include:**
1. The exact command syntax: `maybe-dont cli -- <command>`
2. The `--` separator requirement
3. Error handling guidance (explain denials, suggest alternatives)
4. Instruction not to bypass validation

## Risks and Limitations

### Known Limitations

| Limitation | Description | Mitigation |
|------------|-------------|------------|
| **Sensitive data in arguments** | CLI arguments (e.g., `--password xyz`) are sent to gateway for validation | Document as known limitation. If secrets are in CLI args, they're already exposed to the AI agent. Users should use environment variables or credential managers instead. |
| **No environment variable validation** | Commands may behave differently based on env vars the gateway can't see | Accept limitation. Policies validate explicit args only. |
| **Bypass by determined users** | Users can run commands directly without the wrapper | Not the threat model. We protect against AI agent mistakes, not malicious users. |
| **Network dependency** | Requires connectivity to gateway | Fail-open (V1) allows execution if unreachable. Configurable in future. |

### Technical Considerations

| Consideration | Handling |
|---------------|----------|
| **Shell quoting/escaping** | Not our problem. Shell parses args before we receive them. We pass through as-is. |
| **Binary output** | Transparent. `syscall.Exec` replaces our process; stdout is untouched. |
| **Interactive commands** | Transparent. stdin/stdout/stderr inherited directly. |
| **Long-running commands** | Transparent. After exec, we're gone; no timeout applies. |
| **Piping** | Transparent. `maybe-dont cli -- aws s3 cp ... - \| tar xz` works normally. |
| **Exit codes** | Direct from target command (via `syscall.Exec`). |

### Security Considerations

| Risk | Assessment |
|------|------------|
| **Command injection in wrapper** | Low. We use `syscall.Exec` with argument array, not shell string construction. |
| **MITM on gateway communication** | Use HTTPS in production. Noted as deployment consideration. |
| **Audit log exposure** | Command + args logged server-side. Same exposure model as MCP tool calls. |

### Operational Considerations

| Consideration | Recommendation |
|---------------|----------------|
| **Gateway availability** | Single point of dependency. Deploy with high availability if critical. |
| **Latency impact** | 10-50ms per validation. Acceptable for most workflows. V2 caching reduces further. |
| **Versioning** | `server_version` in response enables compatibility checks if needed. |

### Out of Scope

The following are explicitly not addressed:
- Preventing malicious users from bypassing validation
- Validating commands executed outside AI agent context
- Complete audit of all system calls (this is CLI-level, not syscall-level)
- Protecting against shell aliases or PATH manipulation

## Implementation Checklist

### Phase 1: Core CLI Proxy

- [ ] Create `internal/cliproxy/` package
- [ ] Implement HTTP client for gateway communication
- [ ] Implement `syscall.Exec` execution (Unix)
- [ ] Implement `exec.Command` fallback (Windows)
- [ ] Add `cmd/cli.go` subcommand with Cobra
- [ ] Support `--server` / `-s` flag
- [ ] Support `--timeout` flag
- [ ] Support `--dry-run` flag
- [ ] Implement `--` separator parsing
- [ ] Implement human-readable error output to stderr
- [ ] Implement fail-open behavior when gateway unreachable
- [ ] Add unit tests for argument parsing
- [ ] Add unit tests for HTTP client
- [ ] Add integration tests for end-to-end flow

### Phase 2: Gateway Native Tool

- [ ] Create `maybedont__validate_cli` native tool
- [ ] Define input schema (command, arguments, working_directory, client_info)
- [ ] Define output schema (allowed, validation_required, message, server_version, results)
- [ ] Implement command allowlist configuration (`cli_validation.validate_commands`)
- [ ] Wire to existing CEL engine with `cli` context variables
- [ ] Wire to existing AI engine with unified `operation` structure
- [ ] Add audit logging for CLI validations
- [ ] Add unit tests for native tool
- [ ] Add integration tests for validation flow

### Phase 3: Unified Policy Rules

- [ ] Add `cli_expression` field to CEL rule schema
- [ ] Update CEL engine to evaluate `cli_expression` for CLI context
- [ ] Normalize MCP tool calls and CLI commands for AI rules
- [ ] Add `operation.type` ("mcp_tool" or "cli") to AI rule context
- [ ] Update config validation (require at least one of `expression` or `cli_expression`)
- [ ] Add unit tests for mixed rules
- [ ] Add example rules to `config/` directory
- [ ] Update documentation

### Phase 4: Configuration

- [ ] Add `cli_validation` section to config schema
- [ ] Add `cli_validation.enabled` flag
- [ ] Add `cli_validation.validate_commands` allowlist
- [ ] Add environment variable support (`MAYBE_DONT_CLI_VALIDATION_*`)
- [ ] Update `config/maybe-dont.yaml` with CLI examples
- [ ] Add config validation tests

### Phase 5: Skill Management

- [ ] Add `cmd/skill.go` subcommand
- [ ] Implement `skill list` command
- [ ] Implement `skill view <name>` command
- [ ] Embed `skills/cli.md` in binary using `//go:embed`
- [ ] Add `--format` flag (default: `claude`, others deferred)
- [ ] Add help text and examples
- [ ] Add unit tests for skill commands

### Phase 6: Documentation

- [ ] Create Claude Code skill file (`skills/cli.md`)
- [ ] Add generic guidance for other AI agents
- [ ] Update CLAUDE.md with CLI proxy documentation
- [ ] Update README with CLI proxy usage
- [ ] Add examples to documentation

### Future Enhancements (Not in V1)

- [ ] V2: TTL-based response caching
- [ ] V2: Session ID support for cache scoping
- [ ] V3: Server-managed allowlist with `Last-Modified`
- [ ] Configurable fail mode (`fail_mode: open | closed`)
- [ ] `--output-format json` flag
- [ ] Slim binary build target (`maybe-dont-cli`)
- [ ] Authentication between CLI and gateway
- [ ] Additional skill formats (Cursor, Copilot, etc.)
