# CLI Proxy for AI Agent Tool Validation

## Status
**Implemented** - Ready for merge

## Overview

Extend the Maybe Don't CLI to act as a proxy for CLI commands executed by AI agents. This allows the same policy validation (CEL rules, AI validation) applied to MCP tool calls to be applied to traditional CLI tool invocations.

## Goals

1. Provide a CLI wrapper that intercepts commands and validates them against the central Maybe Don't gateway
2. Reuse existing MCP gateway validation infrastructure (CEL engine, AI engine, audit logging)
3. Simple invocation: `maybe-dont cli -- <command> [args...]`
4. Server-controlled command filtering (no client-side allowlists)
5. Transparent execution - after validation, behave identically to direct command execution
6. Support AI agent workflows via skill/instruction definitions
7. Support users who don't use MCP (REST API doesn't require MCP gateway configuration)

## Non-Goals

1. Replacing MCP as the primary agent-to-tool protocol
2. Validating commands executed outside AI agent contexts
3. Preventing determined users from bypassing validation
4. Protecting against malicious users (threat model is AI agent mistakes, not adversaries)
5. Response validation for CLI commands (see [Response Validation Limitations](#response-validation-limitation))

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
│                              │ REST API               │             │
│                              │                        │             │
└──────────────────────────────┼────────────────────────┼─────────────┘
                               │                        │
                               ▼                        │
┌──────────────────────────────────────────────────────────────────────┐
│                     Central Infrastructure                           │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │                    Maybe Don't Gateway                         │  │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌────────────────┐  │  │
│  │  │  REST Endpoint  │  │   CEL Engine    │  │   AI Engine    │  │  │
│  │  │ /api/v1/cli/    │─▶│   (policies)    │─▶│   (OpenAI)     │  │  │
│  │  │    validate     │  └─────────────────┘  └────────────────┘  │  │
│  │  └─────────────────┘           │                               │  │
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
3. CLI wrapper sends HTTP POST to gateway's REST endpoint (`/api/v1/cli/validate`)
4. Gateway validates via CEL rules and/or AI policies (request validation only)
5. Gateway returns: allowed/denied + validation_required flag
6. If allowed: CLI wrapper uses `syscall.Exec` to replace itself with target command
7. If denied: CLI wrapper outputs error to stderr, exits non-zero

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Transport | REST API (not MCP) | Simpler client; supports users without MCP; no session overhead |
| API Path | `/api/v1/cli/validate` | Resource-oriented REST; path versioning for clarity |
| Authentication | None (V1) | Defer to future; network isolation assumed initially |
| Execution | `syscall.Exec` (Unix) | Transparent replacement; identical behavior to direct execution |
| Fallback | `exec.Command` (Windows) | Windows lacks direct exec replacement |
| Fail mode | Fail-open (V1) | Allow command if gateway unreachable; configurable later |
| Validation scope | Request only | Response validation not possible with `syscall.Exec` |

**Implementation Note - Validation Abstraction:**

The existing validation engines (`CELPolicyEngine`, `AIPolicyEngine`) are currently coupled to `mcp.CallToolRequest`. To support CLI validation, consider one of these approaches:

1. **Adapter Pattern (recommended)**: Create a `ValidationRequest` interface that both MCP tool calls and CLI commands can implement, allowing the engines to accept either type.
2. **Separate Methods**: Add `EvaluateCLICommand()` methods alongside existing `EvaluateToolCall()` methods.
3. **Unified Request Struct**: Create a common struct with a `Type` field ("mcp_tool" or "cli") that wraps either request type.

The choice is an implementation detail; all approaches achieve the same external behavior.

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

All errors and warnings are written to **stderr** to avoid interfering with command output or piping.

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
# Command executes (warning written to stderr, does not affect stdout/piping)
```

**CLI validation disabled on gateway:**
```
$ maybe-dont cli -- gh pr comment 123
Error: CLI validation is not enabled on this gateway
Configure cli_request_validation.enabled: true to enable this feature
```

### V1 Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--server` | `-s` | string | `http://localhost:8080` | Gateway base URL |
| `--timeout` | | duration | `30s` | Validation request timeout |
| `--dry-run` | | bool | `false` | Validate only, don't execute |

**Note:** The `--server` flag specifies the gateway base URL only. The CLI appends `/api/v1/cli/validate` at runtime. For example, `--server https://gateway.internal:8443` results in requests to `https://gateway.internal:8443/api/v1/cli/validate`.

### Future Flags (not in V1)

| Flag | Description |
|------|-------------|
| `--fail-closed` | Deny command if gateway unreachable |
| `--output-format` | `text` or `json` for error output |
| `--client-id` | Client ID for audit attribution (overrides env var) |

## REST API Design

The gateway exposes a REST endpoint for CLI validation. This is separate from the MCP protocol, allowing users who don't use MCP to still validate CLI commands.

### Endpoint

```
POST /api/v1/cli/validate
```

### Endpoint Availability

The CLI validation endpoint is available on **both HTTP and SSE transport types**. It is registered on the same `http.ServeMux` that serves MCP traffic, requiring minimal additional code (one `mux.HandleFunc` call per transport).

**Independence from MCP:** The CLI endpoint is controlled by `cli_request_validation.enabled`, independent of downstream MCP server configuration. A gateway can be deployed with:
- Only CLI validation (no downstream MCP servers)
- Only MCP proxying (CLI validation disabled)
- Both features enabled

**STDIO transport:** The CLI REST endpoint is not available when the gateway runs in STDIO mode, as STDIO does not expose an HTTP server.

### Request Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | Must be `application/json` |
| `X-Maybe-Dont-Client-ID` | No | Identifies the caller for audit attribution (e.g., user email, service account). See [Client ID](#client-id-for-audit-attribution). |
| `X-Request-ID` | No | Per-request tracing ID. If not provided, the server generates a 32-character hex string (consistent with MCP request ID generation). |

**Request ID vs Client ID:**
- **Request ID**: Unique identifier for a single HTTP request, used for tracing/debugging a specific request through logs
- **Client ID**: Identifies the caller across multiple requests (e.g., all commands from one user or AI agent), used for audit attribution

### Request Body

```json
{
  "command": "gh",
  "arguments": ["pr", "comment", "123", "--body", "This looks good!"],
  "working_directory": "/home/user/project",
  "client_info": {
    "hostname": "dev-workstation-1",
    "username": "developer",
    "os": "darwin",
    "os_version": "24.1.0",
    "arch": "arm64",
    "shell": "/bin/zsh",
    "cli_version": "1.2.0"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | Yes | The CLI executable name (e.g., `gh`, `aws`, `kubectl`) |
| `arguments` | string[] | Yes | Command arguments (can be empty array) |
| `working_directory` | string | No | Current working directory where command will execute |
| `client_info` | object | No | Client environment information |
| `client_info.hostname` | string | No | Client machine hostname |
| `client_info.username` | string | No | Current user (from `$USER` or equivalent) |
| `client_info.os` | string | No | Operating system (e.g., `darwin`, `linux`, `windows`) |
| `client_info.os_version` | string | No | OS version string (best-effort in V1; requires platform-specific code) |
| `client_info.arch` | string | No | CPU architecture (e.g., `amd64`, `arm64`) |
| `client_info.shell` | string | No | User's shell (e.g., `/bin/zsh`) |
| `client_info.cli_version` | string | No | Version of the `maybe-dont` CLI |

### Response Structure

**Success (allowed):**

```json
{
  "allowed": true,
  "validation_required": true,
  "message": "Command approved by policy",
  "action_reason": "",
  "server_version": "1.3.0",
  "client_version": "1.2.0",
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

**Denied:**

```json
{
  "allowed": false,
  "validation_required": true,
  "message": "Policy 'no-destructive-github-actions' denied: Repository deletion is not permitted",
  "action_reason": "request_policy",
  "server_version": "1.3.0",
  "client_version": "1.2.0",
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

**No validation required (command not in allowlist):**

```json
{
  "allowed": true,
  "validation_required": false,
  "message": "Command does not require validation",
  "server_version": "1.3.0",
  "client_version": "1.2.0",
  "results": []
}
```

### Version Fields

The response includes `server_version` and `client_version` fields:

- **`server_version`**: Gateway version (from build info)
- **`client_version`**: Echoed from `client_info.cli_version` in the request (if provided)

**V1 Behavior:** These fields are informational only. No compatibility checks are performed. Future versions may add:
- Minimum supported client version enforcement
- Deprecation warnings for outdated clients
- Feature capability negotiation

### Error Responses

All error responses follow the same structure:

```json
{
  "error": "<error_code>",
  "message": "<human-readable description>"
}
```

**Error Code Taxonomy:**

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `cli_validation_disabled` | 400 | CLI validation feature not enabled on gateway |
| `invalid_request` | 400 | Malformed request body (missing fields, invalid JSON) |
| `missing_command` | 400 | Required `command` field is empty or missing |
| `invalid_content_type` | 400 | Content-Type header is not `application/json` |
| `policy_evaluation_error` | 500 | CEL or AI engine failed to evaluate policies |
| `internal_error` | 500 | Unexpected server error |

**Examples:**

**400 Bad Request - CLI validation disabled:**

```json
{
  "error": "cli_validation_disabled",
  "message": "CLI validation is not enabled on this gateway. Set cli_request_validation.enabled: true in configuration."
}
```

**400 Bad Request - Missing command:**

```json
{
  "error": "missing_command",
  "message": "Required field 'command' is empty"
}
```

**400 Bad Request - Invalid request:**

```json
{
  "error": "invalid_request",
  "message": "Failed to parse request body: unexpected EOF"
}
```

**500 Internal Server Error - Policy evaluation:**

```json
{
  "error": "policy_evaluation_error",
  "message": "CEL engine failed: expression compilation error"
}
```

**500 Internal Server Error - Unexpected:**

```json
{
  "error": "internal_error",
  "message": "An unexpected error occurred"
}
```

### Client ID for Audit Attribution

Client IDs identify the caller across multiple CLI validation requests for audit attribution (e.g., all commands from one user or AI agent).

**Sources (in priority order):**
1. `MAYBE_DONT_CLIENT_ID` environment variable (user configuration)
2. `X-Maybe-Dont-Client-ID` request header (AI agent/skill instruction)

**Rationale:** The environment variable is set by the human user (intentional, persistent configuration). The header is set by the AI agent following skill instructions. User's explicit configuration takes precedence.

**Example values:**
- Email: `developer@company.com`
- User ID: `user-12345`
- Service account: `ci-bot-prod`
- Session identifier: `session-abc123`

**Example:**

```bash
# User sets client ID in their environment
export MAYBE_DONT_CLIENT_ID="developer@company.com"
maybe-dont cli -- gh pr comment 123 --body "LGTM"
```

The CLI wrapper reads the environment variable and sends it as the `X-Maybe-Dont-Client-ID` header. The audit log entry includes this client ID, enabling later analysis to link CLI calls with MCP tool calls from the same caller.

**Note:** This is optional. Without it, audit logs still capture hostname, username, working directory, and timestamp for attribution.

## Server-Side Configuration

The gateway controls which CLI commands require validation. No client-side allowlists.

### Configuration Structure

```yaml
# maybe-dont.yaml

cli_request_validation:
  enabled: true

  # Commands that require validation
  # Use "*" to validate ALL commands
  # Empty list is a configuration error when enabled=true
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

### Configuration Validation

| Condition | Behavior |
|-----------|----------|
| `enabled: false` | CLI validation disabled; endpoint returns 400 |
| `enabled: true`, `validate_commands: []` | **Configuration error** at startup |
| `enabled: true`, `validate_commands: ["*"]` | All commands require validation |
| `enabled: true`, `validate_commands: ["gh", "aws"]` | Only listed commands require validation |

**Rationale for requiring explicit `*`:** An empty list could be accidental. Requiring `*` makes "validate everything" an intentional choice and surfaces the performance implications (every command hits the gateway).

### Environment Variable Support

```bash
# Enable CLI validation
export MAYBE_DONT_CLI_REQUEST_VALIDATION_ENABLED=true

# Set validate_commands (comma-separated)
export MAYBE_DONT_CLI_REQUEST_VALIDATION_VALIDATE_COMMANDS=gh,aws,kubectl

# Validate all commands
export MAYBE_DONT_CLI_REQUEST_VALIDATION_VALIDATE_COMMANDS=*
```

## Unified Policy Rules

CEL and AI rules support both MCP tool calls and CLI commands in a single rule definition.

### CEL Rules

Use `mcp_expression` for MCP tool calls and `cli_expression` for CLI commands. At least one must be defined.

**Backwards Compatibility:** The legacy `expression` field is supported as a fallback for `mcp_expression`. If both `expression` and `mcp_expression` are present, `mcp_expression` takes precedence. This fallback is resolved at policy load time, so at runtime only `mcp_expression` and `cli_expression` exist.

```yaml
# cel_request_rules.yaml

rules:
  # Both MCP and CLI
  - name: no-destructive-github-actions
    description: "Block destructive GitHub operations"

    # MCP tool expression
    mcp_expression: |
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

  # MCP only (no CLI equivalent) - using legacy `expression` field
  - name: no-bulk-email
    expression: |  # Treated as mcp_expression for backwards compat
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

**Field Resolution (at policy load time):**

| `mcp_expression` | `expression` | Result |
|------------------|--------------|--------|
| present | present | Use `mcp_expression` (ignore `expression`) |
| present | absent | Use `mcp_expression` |
| absent | present | Use `expression` as `mcp_expression` |
| absent | absent | No MCP expression (rule skipped for MCP calls) |

**Evaluation Logic (at runtime):**

| `mcp_expression` | `cli_expression` | MCP Tool Call | CLI Command |
|------------------|------------------|---------------|-------------|
| ✓ | ✓ | Evaluate `mcp_expression` | Evaluate `cli_expression` |
| ✓ | ✗ | Evaluate `mcp_expression` | Skip rule |
| ✗ | ✓ | Skip rule | Evaluate `cli_expression` |
| ✗ | ✗ | Invalid (config error) | Invalid (config error) |

**Config Validation:**

- Error at load time if all of `mcp_expression`, `expression`, and `cli_expression` are empty/missing
- Warning at load time if `cli_expression` exists but `cli_request_validation.enabled: false`

### AI Rules

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

**Note:** Review existing AI policies for CLI compatibility. The prompt should be written to handle both structured MCP arguments (objects) and positional CLI arguments (arrays).

### CEL Context Variables

The CEL engine receives a `cli` object for CLI commands:

| Variable | Type | Description |
|----------|------|-------------|
| `cli.command` | string | The CLI executable name |
| `cli.arguments` | list(string) | Command arguments |
| `cli.working_directory` | string | Working directory path |
| `cli.client_info.hostname` | string | Client hostname |
| `cli.client_info.username` | string | Current user |
| `cli.client_info.os` | string | Operating system |
| `cli.client_info.arch` | string | CPU architecture |
| `cli.client_info.shell` | string | User's shell |
| `cli.client_info.cli_version` | string | CLI wrapper version |

**Implementation Note - Client Info Collection:**

The CLI wrapper should collect client info using Go's cross-platform standard library:

| Field | Go Approach | Notes |
|-------|-------------|-------|
| `hostname` | `os.Hostname()` | Works cross-platform; in Docker returns container ID unless `--hostname` set |
| `username` | `user.Current().Username` | From `os/user` package; works in containers |
| `os` | `runtime.GOOS` | Built-in: "darwin", "linux", "windows" |
| `arch` | `runtime.GOARCH` | Built-in: "amd64", "arm64", etc. |
| `os_version` | Platform-specific | Optional in V1; requires `sw_vers` (macOS), `/etc/os-release` (Linux), registry (Windows) |
| `shell` | `os.Getenv("SHELL")` / `os.Getenv("COMSPEC")` | Unix vs Windows; may be empty in containers |
| `cli_version` | Build-time constant | Embedded via `-ldflags` |

### CEL Policy Loading

Both expressions are compiled and stored at load time. The `expression` → `mcp_expression` fallback is resolved during loading, so runtime code only deals with `MCPExpression` and `CLIExpression`:

```go
type CELPolicy struct {
    Name           string
    Description    string
    MCPExpression  *cel.Program  // compiled from `mcp_expression` (or `expression` fallback)
    CLIExpression  *cel.Program  // compiled from `cli_expression`
    Action         config.PolicyAction
    Message        string
    Mode           config.PolicyMode
}
```

**Load-time resolution:**
```go
// Pseudocode for field resolution
if config.MCPExpression != "" {
    policy.MCPExpression = compile(config.MCPExpression)
} else if config.Expression != "" {
    policy.MCPExpression = compile(config.Expression)  // fallback
}
if config.CLIExpression != "" {
    policy.CLIExpression = compile(config.CLIExpression)
}
```

**Runtime evaluation:**
- MCP tool call → use `MCPExpression` (skip rule if nil)
- CLI command → use `CLIExpression` (skip rule if nil)

## Audit Logging

CLI validations are logged to the same audit log as MCP tool calls, using a compatible but distinct entry structure.

### Audit Entry Structure

For CLI validations, the `AuditEntry` uses a new `CLI` field instead of `Tool`:

```go
type AuditEntry struct {
    // ... existing fields ...

    // Tool is populated for MCP tool calls (nil for CLI)
    Tool *AuditToolInfo `json:"tool,omitempty"`

    // CLI is populated for CLI command validations (nil for MCP)
    CLI *AuditCLIInfo `json:"cli,omitempty"`

    // ... validation results, timing, etc. ...
}

type AuditCLIInfo struct {
    Command          string            `json:"command"`           // e.g., "gh"
    Arguments        []string          `json:"arguments"`         // e.g., ["pr", "comment", "123"]
    WorkingDirectory string            `json:"working_directory,omitempty"`
    ClientInfo       *CLIClientInfo    `json:"client_info,omitempty"`
}

type CLIClientInfo struct {
    Hostname   string `json:"hostname,omitempty"`
    Username   string `json:"username,omitempty"`
    OS         string `json:"os,omitempty"`
    OSVersion  string `json:"os_version,omitempty"`
    Arch       string `json:"arch,omitempty"`
    Shell      string `json:"shell,omitempty"`
    CLIVersion string `json:"cli_version,omitempty"`
}
```

**Example CLI Audit Entry:**

```json
{
  "validation_started": "2024-01-15T10:30:00.123456789Z",
  "created_at": "2024-01-15T10:30:00.234567890Z",
  "cli": {
    "command": "gh",
    "arguments": ["pr", "comment", "123", "--body", "LGTM"],
    "working_directory": "/home/user/project",
    "client_info": {
      "hostname": "dev-workstation-1",
      "username": "developer",
      "os": "darwin",
      "cli_version": "1.2.0"
    }
  },
  "upstream_request": {
    "id": "a1b2c3d4e5f6...",
    "client_id": "developer@company.com",
    "client_ip": "192.168.1.100"
  },
  "request_validation": {
    "cel": {
      "action": "allow",
      "results": [...]
    }
  },
  "recommended_action": "allow",
  "action": "allow",
  "duration_ms": 45
}
```

**Key Differences from MCP Audit Entries:**

| Field | MCP Tool Call | CLI Command |
|-------|---------------|-------------|
| `tool` | Populated | `null` |
| `cli` | `null` | Populated |
| `tool.client` | MCP client name (e.g., "github") | N/A |
| `cli.command` | N/A | CLI executable name |
| Response validation | Supported | Not supported (always `null`) |

## Response Validation Limitation

**CLI commands support request validation only.** Response validation is not possible because:

1. After validation passes, the CLI uses `syscall.Exec` to replace itself with the target command
2. Once `syscall.Exec` runs, our process is gone - we cannot intercept the command's output
3. Even if we used `exec.Command` with output capture, the side effect has already occurred

This is an architectural constraint, not a missing feature. For operations that modify state (like `gh repo delete`), there's no meaningful response validation - the action already happened.

**See also:** [Response Validation for State-Changing Operations](./response-validation-state-changes.md) for a broader discussion of this issue in MCP tool validation.

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
    client.go            # HTTP client for gateway REST API
    exec.go              # Command execution (syscall.Exec)
    exec_windows.go      # Windows fallback (exec.Command)
  gateway/               # Existing gateway logic
    cli_validation.go    # CLI validation endpoint handler
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

### Client ID Cache Scoping (V2/V3)

If client ID is provided, scope cache by client:

```bash
export MAYBE_DONT_CLIENT_ID="developer@company.com"
maybe-dont cli -- gh pr comment 123
```

Cache key becomes `{command}:{client_id}` - different clients have isolated caches.

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

**`maybe-dont skill view <name> --format <format>`**

Outputs the specified skill definition to stdout. The `--format` flag is required.

```bash
# Claude Code format
$ maybe-dont skill view cli --format claude > .claude/skills/maybe-dont-cli.md

# Cursor format
$ maybe-dont skill view cli --format cursor > .cursorrules

# GitHub Copilot format
$ maybe-dont skill view cli --format copilot > .github/copilot-instructions.md

# Generic format (for any AI system prompt)
$ maybe-dont skill view cli --format generic > instructions.md
```

### V1 Scope

| Feature | V1 | Future |
|---------|-----|--------|
| `skill list` | ✓ | |
| `skill view cli --format <format>` | ✓ | |
| `--format claude` | ✓ | |
| `--format cursor` | ✓ | |
| `--format copilot` | ✓ | |
| `--format generic` | ✓ | |
| Additional skills | | ✓ |

### Implementation

```go
//go:embed cli.md
var cliSkillClaude string

//go:embed cli.cursorrules
var cliSkillCursor string

//go:embed cli.copilot.md
var cliSkillCopilot string

//go:embed cli.generic.md
var cliSkillGeneric string
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

### Client Identification (Optional)

To identify yourself in audit logs for attribution, set:
\`\`\`bash
export MAYBE_DONT_CLIENT_ID="your-email@company.com"
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

> All CLI commands that interact with external services must be prefixed with `maybe-dont cli --`. This routes commands through a validation gateway. If a command is denied, explain the denial reason to the user and suggest alternatives.

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
| **Working directory in logs** | Working directory paths may contain sensitive info (usernames, project names) | Accept as known limitation. Same exposure model as MCP tool calls. |
| **No environment variable validation** | Commands may behave differently based on env vars the gateway can't see | Accept limitation. Policies validate explicit args only. |
| **No response validation** | CLI uses `syscall.Exec`; command output cannot be intercepted or validated | Request-only validation. Response validation remains MCP-only. See [Response Validation Limitation](#response-validation-limitation). |
| **Bypass by determined users** | Users can run commands directly without the wrapper | Not the threat model. We protect against AI agent mistakes, not malicious users. |
| **Network dependency** | Requires connectivity to gateway | Fail-open (V1) allows execution if unreachable. Configurable in future. |

### Technical Considerations

| Consideration | Handling |
|---------------|----------|
| **Shell quoting/escaping** | Not our problem. Shell parses args before we receive them. We pass through as-is. |
| **Binary output** | Transparent. `syscall.Exec` replaces our process; stdout is untouched. |
| **Interactive commands** | Transparent. stdin/stdout/stderr inherited directly. |
| **Long-running commands** | Transparent. After exec, we're gone; no timeout applies. |
| **Piping** | Transparent. `maybe-dont cli -- aws s3 cp ... - \| tar xz` works normally. Warnings go to stderr. |
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
| **Versioning** | `server_version` and `client_version` in response enable compatibility checks if needed. |

### Out of Scope

The following are explicitly not addressed:
- Preventing malicious users from bypassing validation
- Validating commands executed outside AI agent context
- Complete audit of all system calls (this is CLI-level, not syscall-level)
- Protecting against shell aliases or PATH manipulation
- Response validation for CLI commands (architectural constraint)

## Implementation Checklist

### Phase 1: Configuration

- [ ] Add `cli_request_validation` section to config schema
- [ ] Add `cli_request_validation.enabled` flag (default: false)
- [ ] Add `cli_request_validation.validate_commands` allowlist
- [ ] Validate: error if enabled + empty list; require `*` for "all commands"
- [ ] Add environment variable support (`MAYBE_DONT_CLI_REQUEST_VALIDATION_*`)
- [ ] Update `config/maybe-dont.yaml` with CLI examples (commented out)
- [ ] Add config validation tests

### Phase 2: REST API Endpoint

- [ ] Add REST endpoint handler at `/api/v1/cli/validate`
- [ ] Register endpoint on both HTTP and SSE transport muxes
- [ ] Define request/response JSON schemas
- [ ] Implement error code taxonomy (see Error Responses section)
- [ ] Extract validation logic into shared function (reusable by MCP native tool)
- [ ] Return 400 with JSON error if CLI validation disabled
- [ ] Support `X-Maybe-Dont-Client-ID` header for audit attribution
- [ ] Support `MAYBE_DONT_CLIENT_ID` env var (takes precedence over header)
- [ ] Support `X-Request-ID` header (generate 32-char hex string if not provided)
- [ ] Add `AuditCLIInfo` and `CLIClientInfo` structs
- [ ] Add audit logging for CLI validations (populate `CLI` field, not `Tool`)
- [ ] Add unit tests for endpoint
- [ ] Add integration tests for validation flow

### Phase 3: Core CLI Proxy

- [ ] Create `internal/cliproxy/` package
- [ ] Implement HTTP client for gateway REST API
- [ ] Append `/api/v1/cli/validate` to `--server` base URL
- [ ] Read `MAYBE_DONT_CLIENT_ID` env var, send as `X-Maybe-Dont-Client-ID` header
- [ ] Collect client info (hostname, username, os, arch, shell, cli_version)
- [ ] Implement `syscall.Exec` execution (Unix)
- [ ] Implement `exec.Command` fallback (Windows)
- [ ] Add `cmd/cli.go` subcommand with Cobra
- [ ] Support `--server` / `-s` flag (base URL only)
- [ ] Support `--timeout` flag
- [ ] Support `--dry-run` flag
- [ ] Implement `--` separator parsing
- [ ] Implement human-readable error output to stderr
- [ ] Implement fail-open behavior when gateway unreachable (warning to stderr)
- [ ] Add unit tests for argument parsing
- [ ] Add unit tests for HTTP client
- [ ] Add unit tests for URL construction (base URL + path)
- [ ] Add integration tests for end-to-end flow

### Phase 4: Unified Policy Rules

- [ ] Add `mcp_expression` and `cli_expression` fields to CEL rule schema
- [ ] Implement `expression` → `mcp_expression` fallback at policy load time
- [ ] Update CEL engine to compile and store both expressions (`MCPExpression`, `CLIExpression`)
- [ ] Update CEL engine to evaluate `cli_expression` for CLI context
- [ ] Skip rules without applicable expression for context type
- [ ] Normalize MCP tool calls and CLI commands for AI rules (`operation` structure)
- [ ] Add `operation.type` ("mcp_tool" or "cli") to AI rule context
- [ ] Update config validation (require at least one of `mcp_expression`, `expression`, or `cli_expression`)
- [ ] Warn at load if `cli_expression` exists but CLI validation disabled
- [ ] Add unit tests for mixed rules
- [ ] Add unit tests for `expression` → `mcp_expression` fallback
- [ ] Add example rules to `config/` directory
- [ ] Review default AI policies for CLI compatibility
- [ ] Update documentation

### Phase 5: Skill Management

- [ ] Add `cmd/skill.go` subcommand
- [ ] Implement `skill list` command
- [ ] Implement `skill view <name>` command
- [ ] Embed `skills/cli.md` in binary using `//go:embed`
- [ ] Add `--format` flag (required)
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
- [ ] V2: Client ID support for cache scoping
- [ ] V3: Server-managed allowlist with `Last-Modified`
- [ ] Configurable fail mode (`fail_mode: open | closed`)
- [ ] `--output-format json` flag
- [ ] Slim binary build target (`maybe-dont-cli`)
- [ ] Authentication between CLI and gateway
- [ ] Additional skills beyond `cli` (e.g., MCP-specific skills)
- [ ] MCP native tool wrapper (for users who prefer MCP interface)
