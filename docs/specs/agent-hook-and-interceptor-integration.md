# Agent Hook and MCP Interceptor Integration

## Status
**Implemented** (intercept endpoint shipped; hook scripts and MCP interceptor integration are future work)

## Overview

This spec documents the landscape of agent-side interception mechanisms — both current (agent-specific hooks) and emerging (MCP interceptors via SEP-1763) — and how Maybe Don't can integrate with them as alternatives or complements to the MCP gateway proxy and `maybe-dont cli` skill approach.

Today, Maybe Don't offers two primary integration paths:

1. **MCP Gateway Proxy** — sits between the MCP client and downstream servers, intercepting all tool calls
2. **CLI Proxy** (`maybe-dont cli -- <command>`) — wraps CLI commands and validates them against the gateway's REST API before execution, typically invoked via an agent skill/instruction

Both require the agent to route traffic through Maybe Don't infrastructure. This spec explores a third path: **agent-native hooks that call Maybe Don't for policy decisions**, and a future fourth path where **MCP interceptors eliminate the need for a proxy entirely**.

## Goals

1. Document the current state of agent hook systems across major AI coding tools
2. Document the MCP Interceptors proposal (SEP-1763) and its implications for Maybe Don't
3. Define an architecture where Maybe Don't can operate as a hook target, interceptor, or gateway depending on client capabilities
4. Identify what can be built today (hooks) vs. what requires spec adoption (interceptors)

## Non-Goals

1. Replacing the MCP gateway proxy — it remains the primary integration for MCP tool calls
2. Building agent-specific plugins (VS Code extensions, etc.) — hooks are script-based
3. Defining a new protocol — we use existing hook APIs and the proposed interceptor spec

## Background: Current CLI Integration

The current approach uses an agent skill (`internal/skills/cli.claude.md`) that instructs the AI agent to route all CLI commands through `maybe-dont cli -s <server-url> -- <command>`. This works but has limitations:

- **Relies on LLM compliance** — the agent must choose to use the skill; it can ignore it
- **Agent-specific** — each agent platform needs its own skill variant
- **No response validation** — CLI commands execute via `syscall.Exec`, so output cannot be inspected

Agent hooks provide **deterministic enforcement** — they fire automatically regardless of what the LLM decides, and some support response inspection.

## Agent Hook Systems (Available Now)

### Comparison Matrix

| Feature | Claude Code | Cursor | Gemini CLI | Cline | Copilot CLI | VS Code Copilot |
|---------|------------|--------|------------|-------|-------------|-----------------|
| Pre-tool hook | `PreToolUse` | `preToolUse`, `beforeShellExecution`, `beforeMCPExecution` | `BeforeTool` | `PreToolUse` | `preToolUse` | `PreToolUse` |
| Post-tool hook | `PostToolUse` | `postToolUse`, `afterShellExecution`, `afterMCPExecution` | `AfterTool` | `PostToolUse` | `postToolUse` | `PostToolUse` |
| Block execution | Exit code 2 or JSON `deny` | Exit code 2 or JSON `deny` | Exit code 2 or JSON `decision: "deny"` | JSON `cancel: true` | Limited | JSON `permissionDecision: deny` |
| Modify input | JSON `updatedInput` (Cursor) | `updated_input`, `updated_mcp_tool_output` | `systemMessage` | `contextModification` | No | `updatedInput` |
| MCP tool matching | `mcp__<server>__<tool>` | Dedicated `beforeMCPExecution` | `mcp_context` in input | Tool name match | Tool name match | Tool name match |
| CLI/Bash matching | `Bash` matcher | Dedicated `beforeShellExecution` | `tool_name` matcher | `execute_command` tool | `bash` tool | `Bash` matcher |
| Hook types | `command`, `prompt`, `agent` | `command` | `command` | `command` (shell script) | `command` | `command` |
| Config location | `.claude/settings.json` | `.cursor/hooks/` | `settings.json` | `.clinerules/hooks/` | `.github/hooks/*.json` | `.github/hooks/*.json` |
| HTTP from hooks | Yes (any shell command) | Yes (fetch in Node/Bun) | Yes (any shell command) | Shell commands only | Shell commands only | Shell commands only |
| Fail-closed | No (non-zero other than 2 = proceed) | Yes (`beforeMCPExecution`, `beforeReadFile`) | No | No | No | No |
| Platform | macOS, Linux, Windows | macOS, Linux, Windows | macOS, Linux, Windows | macOS, Linux | macOS, Linux, Windows | macOS, Linux, Windows |

### Hook Architecture (All Platforms)

All hook systems follow the same basic pattern:

```
Agent decides to use a tool
        │
        ▼
┌─────────────────┐     JSON stdin      ┌──────────────────────┐
│  Pre-tool hook   │ ─────────────────▶ │  Hook script          │
│  (agent runtime) │                    │  (user-provided)      │
│                  │ ◀───────────────── │                       │
└────────┬────────┘  exit code + JSON   └──────────────────────┘
         │                                        │
    allow/deny                              Can call HTTP
         │                                   endpoints
         ▼
   Tool executes (or is blocked)
         │
         ▼
┌─────────────────┐     JSON stdin      ┌──────────────────────┐
│  Post-tool hook  │ ─────────────────▶ │  Hook script          │
│  (agent runtime) │                    │  (user-provided)      │
│                  │ ◀───────────────── │                       │
└─────────────────┘  exit code + JSON   └──────────────────────┘
```

### Claude Code Hooks

**Events:** `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, plus lifecycle events (`SessionStart`, `Stop`, etc.)

**Input (PreToolUse for Bash):**
```json
{
  "session_id": "abc123",
  "cwd": "/path/to/project",
  "hook_event_name": "PreToolUse",
  "tool_name": "Bash",
  "tool_input": {
    "command": "gh repo delete my-repo"
  }
}
```

**Blocking:** Exit code 2 blocks with stderr as feedback. Or JSON:
```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Destructive GitHub operation blocked by policy"
  }
}
```

**Config:** `.claude/settings.json` (project) or `~/.claude/settings.json` (global)

**Reference:** https://code.claude.com/docs/en/hooks-guide

### Cursor Hooks

**Events:** `beforeShellExecution`, `afterShellExecution`, `beforeMCPExecution`, `afterMCPExecution`, `preToolUse`, `postToolUse`, `beforeReadFile`, `afterFileEdit`, `beforeSubmitPrompt`, `stop`, plus lifecycle events

Cursor has **dedicated events** for shell and MCP execution, separate from the generic `preToolUse`. This is the most granular hook system available.

**Blocking:** Exit code 2 or JSON `"permission": "deny"`. `beforeMCPExecution` and `beforeReadFile` are **fail-closed** — script failures automatically block the operation.

**Config:** `.cursor/hooks/` directory

**Reference:** https://cursor.com/docs/agent/hooks

### Gemini CLI Hooks

**Events:** `BeforeTool`, `AfterTool`, `BeforeAgent`, `AfterAgent`, `BeforeModel`, `AfterModel`, `BeforeToolSelection`, `SessionStart`, `SessionEnd`, `Notification`, `PreCompress`

Gemini CLI shipped hooks in v0.26.0 (January 2026). It has dedicated model-layer hooks (`BeforeModel`, `AfterModel`, `BeforeToolSelection`) that other agents don't expose.

**Input (BeforeTool):**
```json
{
  "session_id": "abc123",
  "cwd": "/path/to/project",
  "hook_event_name": "BeforeTool",
  "timestamp": "2026-02-23T14:30:00Z",
  "tool_name": "Bash",
  "tool_input": {
    "command": "gh repo delete my-repo"
  },
  "mcp_context": {}
}
```

**Blocking:** Exit code 2 blocks with stderr as reason. Or JSON:
```json
{
  "decision": "deny",
  "reason": "Destructive GitHub operation blocked by policy"
}
```

**Config:** `settings.json` with `hooks` object, same structure as Claude Code (matcher + hooks array)

**Reference:** https://geminicli.com/docs/hooks/reference/

### Cline Hooks

**Events:** `PreToolUse`, `PostToolUse`, plus lifecycle events (`TaskStart`, `TaskComplete`, etc.)

**Input (PreToolUse):**
```json
{
  "taskId": "abc123",
  "clineVersion": "3.17.0",
  "timestamp": 1736654400000,
  "workspacePath": "/path/to/project",
  "preToolUse": {
    "tool": "execute_command",
    "parameters": { "command": "gh repo delete my-repo" }
  }
}
```

**Blocking:** Return JSON `{"cancel": true, "errorMessage": "Blocked by policy"}`

**Config:** `.clinerules/hooks/` (project) or `~/Documents/Cline/Rules/Hooks/` (global)

**Limitation:** macOS and Linux only. No Windows support.

**Reference:** https://docs.cline.bot/features/hooks/hook-reference

### GitHub Copilot CLI / VS Code

**Events:** `PreToolUse`, `PostToolUse`, `SessionStart`, `SessionEnd`, `UserPromptSubmit`, plus `SubagentStart`/`SubagentStop` (VS Code), `errorOccurred` (CLI)

**Blocking (VS Code):** JSON `permissionDecision: "deny"` with reason

**Config:** `.github/hooks/*.json` (shared format for CLI and VS Code)

**Reference:**
- CLI: https://docs.github.com/en/copilot/how-tos/copilot-cli/use-hooks
- VS Code: https://code.visualstudio.com/docs/copilot/customization/hooks

### OpenAI Codex CLI

Codex uses an approval-based system rather than user-defined hooks. It supports approval modes (`always`, `on-request`, `never`) and explicit approval prompts for MCP tool calls, but does not expose a pluggable pre-execution hook API for custom validation scripts.

**Workaround:** Codex supports MCP servers. A Maybe Don't MCP gateway in front of downstream servers provides equivalent interception for MCP tool calls. For raw CLI commands, there is no hook mechanism — the `maybe-dont cli` skill approach remains the only option.

**Reference:** https://developers.openai.com/codex/cli/

## MCP Interceptors (SEP-1763 — Draft)

### Overview

SEP-1763 (opened November 2025, status: Draft) proposes adding interceptors as first-class resources to the MCP specification. Unlike agent-specific hooks, interceptors would be **agent-agnostic** — any MCP client or server that implements the spec would support them.

**Reference:** https://github.com/modelcontextprotocol/modelcontextprotocol/issues/1763

### Key Design

Interceptors are typed resources with three categories:

| Type | Purpose | Execution | Blocking |
|------|---------|-----------|----------|
| **Validation** | Verify inputs/outputs | Parallel | `severity: error` blocks |
| **Mutation** | Transform content | Sequential (by priority) | Failure halts chain |
| **Observability** | Audit, logging, metrics | Fire-and-forget | Never blocks |

### Client-Side and Server-Side Placement

The spec explicitly supports interceptors on **both sides of the trust boundary**:

```
CLIENT (Sending):
  Original request
    → Mutate (sequential, by priority)
    → Validate & Observe (parallel)
    → Send across trust boundary

SERVER (Receiving):
  Receive
    → Validate & Observe (parallel)
    → Mutate (sequential, by priority)
    → Process
```

**This is the critical detail for Maybe Don't.** Client-side interceptors execute before the request leaves the MCP client — meaning policy validation can happen without a proxy.

### Supported Events

**Server features:** `tools/list`, `tools/call`, `prompts/list`, `prompts/get`, `resources/list`, `resources/read`, `resources/subscribe`

**Client features:** `sampling/createMessage`, `elicitation/create`, `roots/list`

**LLM interactions:** `llm/completion`

**Wildcards:** `*/request`, `*/response`, `*`

### API

- `interceptors/list` — discover available interceptors (supports event filtering)
- `interceptor/invoke` — invoke a single interceptor with payload
- `interceptor/executeChain` — execute a full interceptor chain

### Deployment Models

The spec supports multiple deployment models:

1. **First-party (in-process):** Interceptor code runs within the MCP server or client process
2. **Third-party (remote service):** Interceptors are separate services called over HTTP/gRPC
3. **Sidecar:** A universal interceptor runtime that acts as an MCP proxy, loading interceptors from configuration

### Implications for Maybe Don't

If SEP-1763 is adopted, Maybe Don't can operate as:

| Deployment | Description | Gateway Required? |
|------------|-------------|-------------------|
| **Client-side interceptor** | Validation + Mutation + Observability interceptors registered in the MCP client | No |
| **Server-side interceptor** | Same interceptors registered at the MCP server | No |
| **Remote interceptor service** | Maybe Don't gateway exposed as a third-party interceptor endpoint | Optional (lightweight) |
| **Sidecar runtime** | Maybe Don't ships as an interceptor sidecar proxy | Optional (lightweight) |
| **Full gateway proxy** | Current architecture, for clients that don't support interceptors | Yes |

### Mapping to Maybe Don't Validation Chain

| Maybe Don't Component | Interceptor Type | Event | Side |
|-----------------------|-----------------|-------|------|
| CEL request rules | Validation | `tools/call` request | Client |
| AI request rules | Validation | `tools/call` request | Client |
| CEL response rules | Validation | `tools/call` response | Client |
| AI response rules (redact) | Mutation | `tools/call` response | Client |
| Audit logging | Observability | `*` | Both |

### Current Status and Timeline Risk

- **Status:** Draft (November 2025)
- **SDK support:** None yet — no official SDK has shipped interceptor support
- **Reference implementation:** Multi-language reference exists but is experimental
- **Adoption risk:** Even after spec finalization, individual clients (Claude Code, Cursor, etc.) must implement it. Historically this takes 6-12+ months.

## Intercept Endpoint (`POST /api/v1/intercept`)

### Why a New Endpoint

Neither existing endpoint is suitable for agent hooks:

- **`/api/v1/cli/validate`** — only accepts pre-parsed `command` + `arguments`; cannot represent MCP tool calls or file operations; evaluates only `cli_expression` in CEL rules
- **`/api/v1/action/validate`** — closer (accepts `target` + `parameters`), but always evaluates as a tool call (`mcp_expression` only); designed for OpenHands with OpenHands-specific semantics

Hooks intercept **all tool types** — Bash commands, MCP tool calls, file edits — in a single event stream. The endpoint must handle all of them and route to the correct evaluation path internally (CLI expressions for shell commands, MCP expressions for tool calls).

### Design Principle

The endpoint schema aligns with [SEP-1763 (MCP Interceptors)](https://github.com/modelcontextprotocol/modelcontextprotocol/issues/1763) as closely as possible. When the interceptor spec is adopted, the gateway can expose this same endpoint as a **third-party remote interceptor** with minimal changes. The naming, field names, and response structure follow SEP-1763 conventions.

### Request Schema

Modeled on `InterceptorInvocationParams` from SEP-1763:

```json
{
  "event": "tools/call",
  "phase": "request",
  "payload": {
    "name": "Bash",
    "arguments": {
      "command": "gh repo delete my-repo"
    }
  },
  "context": {
    "principal": {
      "type": "service",
      "id": "claude-code"
    },
    "traceId": "hook-abc123",
    "timestamp": "2026-02-23T14:30:00Z",
    "sessionId": "session-xyz"
  },
  "config": {
    "working_directory": "/path/to/project"
  }
}
```

#### Field Reference

| Field | Type | Required | Description | SEP-1763 Alignment |
|-------|------|----------|-------------|---------------------|
| `event` | string | Yes | The intercepted event type. Uses MCP event naming: `tools/call`, `tools/list`, etc. | Direct match to `InterceptorEvent` |
| `phase` | `"request"` \| `"response"` | Yes | Whether this is pre-execution (request) or post-execution (response) | Direct match |
| `payload` | object | Yes | The event payload. Structure depends on event type — see [Payload Schemas](#payload-schemas) | Direct match (`payload: unknown`) |
| `context` | object | No | Execution context for tracing, attribution, and policy routing | Matches `context` in SEP-1763 |
| `context.principal` | object | No | Who is performing the action | Matches `principal` in SEP-1763 |
| `context.principal.type` | `"user"` \| `"service"` \| `"anonymous"` | No | Principal type | Direct match |
| `context.principal.id` | string | No | Principal identifier (agent name, user email, etc.) | Direct match |
| `context.traceId` | string | No | Distributed trace ID for correlating across systems | Direct match |
| `context.spanId` | string | No | Span ID within the trace | Direct match |
| `context.timestamp` | string (ISO 8601) | No | When the event occurred | Direct match |
| `context.sessionId` | string | No | Agent session ID | Direct match |
| `config` | object | No | Hook-specific configuration (not in SEP-1763 interceptor config sense) | Loosely maps to `config` |
| `config.working_directory` | string | No | CWD for CLI command evaluation | Maybe Don't extension |

#### Payload Schemas

**`tools/call` request phase** (pre-tool):
```json
{
  "name": "mcp__github__create_issue",
  "arguments": {
    "title": "Bug report",
    "body": "Steps to reproduce..."
  }
}
```

For shell/CLI tools, the hook script sends the tool as invoked by the agent:
```json
{
  "name": "Bash",
  "arguments": {
    "command": "gh repo delete my-repo"
  }
}
```

The gateway detects shell tools (configurable set of tool names: `Bash`, `execute_command`, `shell`, etc.) and internally parses the command string to evaluate both `cli_expression` and `mcp_expression` in CEL rules.

**`tools/call` response phase** (post-tool):
```json
{
  "name": "mcp__github__search_code",
  "arguments": {
    "query": "password"
  },
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Found 3 matches: credentials.json contains API_KEY=sk-..."
      }
    ]
  }
}
```

The response phase includes the tool's output in `result` for response validation (PII scanning, credential detection, redaction).

### Response Schema

Modeled on `ValidationResult` from SEP-1763:

```json
{
  "interceptor": "maybe-dont",
  "type": "validation",
  "phase": "request",
  "valid": false,
  "severity": "error",
  "messages": [
    {
      "path": "arguments.command",
      "message": "Repository deletion is not permitted without explicit approval",
      "severity": "error"
    }
  ],
  "durationMs": 245,
  "info": {
    "request_id": "req-abc123",
    "server_version": "1.3.0",
    "results": [
      {
        "policy_name": "no-destructive-github-actions",
        "policy_type": "cel",
        "action": "deny",
        "message": "Repository deletion is not permitted"
      }
    ]
  }
}
```

For mutation responses (redaction):

```json
{
  "interceptor": "maybe-dont",
  "type": "mutation",
  "phase": "response",
  "modified": true,
  "payload": {
    "name": "mcp__github__search_code",
    "arguments": { "query": "password" },
    "result": {
      "content": [
        {
          "type": "text",
          "text": "Found 3 matches: credentials.json contains API_KEY=[REDACTED]"
        }
      ]
    }
  },
  "durationMs": 312,
  "info": {
    "request_id": "req-def456",
    "server_version": "1.3.0",
    "results": [
      {
        "policy_name": "redact-api-keys",
        "policy_type": "ai",
        "action": "redact",
        "message": "API key redacted from tool output"
      }
    ]
  }
}
```

#### Response Field Reference

| Field | Type | Description | SEP-1763 Alignment |
|-------|------|-------------|---------------------|
| `interceptor` | string | Always `"maybe-dont"` | Direct match |
| `type` | `"validation"` \| `"mutation"` \| `"observability"` | The interceptor type that produced this result | Direct match |
| `phase` | `"request"` \| `"response"` | Echoed from request | Direct match |
| `valid` | boolean | Whether the payload passed validation (validation type only) | Direct match |
| `severity` | `"info"` \| `"warn"` \| `"error"` | Highest severity across all policy results. `error` = blocked. | Direct match |
| `messages` | array | Per-policy validation messages with path, message, severity | Direct match to `messages` array |
| `modified` | boolean | Whether the payload was mutated (mutation type only) | Direct match |
| `payload` | object | The mutated payload (mutation type only; returned only when `modified: true`) | Direct match |
| `durationMs` | number | Total evaluation time in milliseconds | Direct match |
| `info` | object | Maybe Don't-specific metadata (request_id, server_version, per-policy results) | Maps to `info: Record<string, unknown>` |

#### Error Responses

The intercept endpoint returns structured error responses for malformed requests:

| HTTP Status | Error Code | Condition |
|-------------|-----------|-----------|
| 400 | `invalid_request` | Malformed JSON body or decoding failure |
| 400 | `missing_event` | Required `event` field is empty |
| 400 | `missing_phase` | Required `phase` field is empty |
| 400 | `invalid_phase` | `phase` is not `"request"` or `"response"` |
| 400 | `missing_payload` | Required `payload` field is null |
| 400 | `missing_payload_name` | Required `payload.name` field is empty |
| 400 | `response_phase_missing_result` | Response phase requires `payload.result` |
| 415 | `invalid_content_type` | `Content-Type` is not `application/json` |

> **Note:** Response validation engine errors fail open (HTTP 200 with `valid=true`),
> consistent with the gateway's fail-open philosophy. No 500 status is returned.

Error response body:
```json
{
  "error": "missing_event",
  "message": "Required field 'event' is empty"
}
```

#### Severity Mapping

| Scenario | `valid` | `severity` | Hook Action |
|----------|---------|------------|-------------|
| All policies allow | `true` | `"info"` | Proceed |
| Some policies in audit-only mode triggered | `true` | `"warn"` | Proceed (logged) |
| Any enforced policy denies | `false` | `"error"` | Block |

### Internal Routing

The gateway routes evaluation internally based on the payload:

1. **Shell tool detection:** If `payload.name` matches a configurable set of shell tool names (`Bash`, `execute_command`, `shell`, `run_terminal_command`, etc.), parse `payload.arguments.command` into command + arguments and evaluate with **both** `cli_expression` and `mcp_expression`
2. **MCP tool calls:** All other tool names evaluate with `mcp_expression` only
3. **Response phase:** Evaluate with response validation rules (CEL response + AI response)

This keeps the routing logic server-side — hook scripts send what the agent gives them without needing to know which evaluation path applies.

#### Shell Tool Dual-Evaluation Detail

When a shell tool is detected, the gateway uses `EvaluateShellCommand()` which runs the request through **two separate evaluation paths** and merges the results:

1. **CLI evaluation:** The command string is parsed into `command` and `arguments`, then evaluated against `cli_expression` rules via `EvaluateCLICommand()`
2. **MCP evaluation:** The same tool call is evaluated against `mcp_expression` rules via `EvaluateToolCall()`, with CLI-only policies (those with only `cli_expression` and no `mcp_expression`) skipped

The results from both paths are merged: if either path denies, the combined result is denied. Both paths' audit details (CEL rule results, AI results) are preserved in the merged result. This ensures that a rule with both `cli_expression` and `mcp_expression` evaluates correctly regardless of how the tool call arrives.

### Configuration

New configuration section:

```yaml
intercept:
  enabled: true
  shell_tool_names:
    - "Bash"
    - "execute_command"
    - "shell"
    - "run_terminal_command"
    - "run_command"
```

The `shell_tool_names` list determines which tool names trigger CLI expression evaluation. This is extensible as new agents introduce new shell tool names.

## Proposed Architecture

### Hook Integration (Available Now)

For each supported agent, provide a hook script that calls `POST /api/v1/intercept` for policy decisions. This provides **deterministic enforcement** — the hook fires automatically, unlike the skill-based approach that relies on LLM compliance.

```
┌─────────────────────────────────────────────────────┐
│  AI Agent (Claude Code, Cursor, Cline, Copilot)     │
│                                                     │
│  Agent decides to run a tool or CLI command          │
│         │                                           │
│         ▼                                           │
│  ┌───────────────────────────┐                      │
│  │  PreToolUse hook          │                      │
│  │  (maybe-dont-hook.sh)     │──── HTTP POST ────┐  │
│  └───────────────────────────┘                   │  │
│         │                                        │  │
│    valid/invalid                                 │  │
│         │                                        │  │
│         ▼                                        │  │
│  Tool executes (or blocked)                      │  │
│         │                                        │  │
│         ▼                                        │  │
│  ┌───────────────────────────┐                   │  │
│  │  PostToolUse hook         │──── HTTP POST ──┐ │  │
│  │  (maybe-dont-hook.sh)     │                 │ │  │
│  └───────────────────────────┘                 │ │  │
│                                                │ │  │
└────────────────────────────────────────────────┼─┼──┘
                                                 │ │
                              ┌───────────────────┘ │
                              │  ┌──────────────────┘
                              ▼  ▼
                    ┌────────────────────┐
                    │  Maybe Don't       │
                    │  Gateway           │
                    │                    │
                    │  POST /api/v1/     │
                    │    intercept       │
                    │                    │
                    │  phase: request    │
                    │  (pre-tool)        │
                    │                    │
                    │  phase: response   │
                    │  (post-tool)       │
                    └────────────────────┘
```

### Hook Script Design

A single hook script (`maybe-dont-hook.sh` or `maybe-dont-hook.py`) that:

1. Reads JSON from stdin (agent-specific format)
2. Extracts tool name and parameters
3. Translates to the `/api/v1/intercept` request schema (event, phase, payload, context)
4. Calls the gateway via HTTP
5. Reads the response (`valid`, `severity`, `messages`)
6. Returns allow/deny in the agent-specific output format

Agent-specific wrappers handle format translation:

```
maybe-dont-hook.sh          ← core logic: build intercept request, call gateway, interpret response
├── claude-pretooluse.sh    ← translates Claude Code stdin → intercept schema, response → Claude output
├── cursor-beforeshell.sh   ← translates Cursor stdin → intercept schema, response → Cursor output
├── cline-pretooluse.sh     ← translates Cline stdin → intercept schema, response → Cline output
└── copilot-pretooluse.sh   ← translates Copilot stdin → intercept schema, response → Copilot output
```

Or a single polyglot script that detects the agent from input JSON structure.

### Interceptor Integration (Future — When SEP-1763 Ships)

When MCP interceptors are adopted:

```
┌─────────────────────────────────────────────────────┐
│  MCP Client (any agent)                             │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │  Maybe Don't Interceptor (client-side)        │  │
│  │                                               │  │
│  │  Validation: CEL rules + AI rules             │  │
│  │  Mutation:   PII redaction on responses       │  │
│  │  Observability: Audit logging                 │  │
│  └───────────────────────────────────────────────┘  │
│         │                                           │
│    (validated, no proxy needed)                      │
│         │                                           │
└─────────┼───────────────────────────────────────────┘
          ▼
   ┌──────────────┐
   │  MCP Server   │  ← unmodified
   └──────────────┘
```

The gateway becomes optional — used only for:
- Clients that don't support interceptors
- Centralized policy management and distribution
- Centralized audit log aggregation
- Remote interceptor service (the gateway exposes `/api/v1/intercept` as a third-party interceptor endpoint — the schema already aligns)

### Transition Path

```
Phase 1 (Now):     Skill-based CLI proxy       + MCP gateway proxy
Phase 2 (Now):     Agent hooks → /api/v1/intercept + MCP gateway proxy
Phase 3 (Future):  MCP interceptors (native)   + Gateway as remote interceptor for non-compliant clients
```

Each phase is additive — earlier mechanisms continue to work alongside newer ones. The `/api/v1/intercept` endpoint serves as the bridge: hook scripts call it in Phase 2, and it becomes the remote interceptor endpoint in Phase 3.

## Agent-Specific Implementation Notes

### Claude Code

- Use `PreToolUse` with `Bash` matcher for CLI commands
- Use `PreToolUse` with `mcp__.*` matcher for MCP tool calls
- Hook script can use `curl` to call gateway
- Config in `.claude/settings.json` (project) or `~/.claude/settings.json` (user)
- Also supports `type: "prompt"` and `type: "agent"` hooks for LLM-based decisions

### Cursor

- Use `beforeShellExecution` for CLI commands (dedicated event, more specific than generic `preToolUse`)
- Use `beforeMCPExecution` for MCP tool calls (**fail-closed** — extra safety)
- Use `afterMCPExecution` to scan MCP responses (can modify output via `updated_mcp_tool_output`)
- Config in `.cursor/hooks/` directory

### Gemini CLI

- Use `BeforeTool` with matcher for CLI commands (tool name matching via regex)
- Use `BeforeTool` for MCP tool calls (`mcp_context` included in input)
- Use `AfterTool` to inspect tool responses
- Block with exit code 2 or JSON `{"decision": "deny", "reason": "..."}`
- Config in `settings.json` with `hooks` object
- Also has model-layer hooks (`BeforeModel`, `BeforeToolSelection`) not available in other agents

### Cline

- Use `PreToolUse` matching `execute_command` for CLI commands
- Use `PreToolUse` matching MCP tool names for MCP tool calls
- Return `{"cancel": true, "errorMessage": "..."}` to block
- Config in `.clinerules/hooks/` (project) or `~/Documents/Cline/Rules/Hooks/` (global)
- **Limitation:** macOS/Linux only

### GitHub Copilot (CLI and VS Code)

- Use `PreToolUse` matching `bash` or `Bash` for CLI commands
- Use `PreToolUse` matching tool names for MCP tool calls
- Config in `.github/hooks/*.json`
- Shared format across CLI and VS Code — single hook definition works for both

### OpenAI Codex CLI

- **No user-defined hooks available** — approval system is built-in and not extensible
- For MCP tool calls: use Maybe Don't as an MCP gateway proxy in front of downstream servers
- For CLI commands: the `maybe-dont cli` skill approach remains the only option
- Monitor for future hook API — Codex is actively adding agent capabilities

## Relationship to Existing Specs and Endpoints

- **[cli-proxy-for-ai-agents](cli-proxy-for-ai-agents.md)** — defines `/api/v1/cli/validate`. The new `/api/v1/intercept` endpoint supersedes this for hook-based integrations, but `/api/v1/cli/validate` remains for the `maybe-dont cli` binary which has its own request/response contract.
- **[openhands-integration](openhands-integration.md)** — defines `/api/v1/action/validate`. Similarly, this endpoint remains for OpenHands' specific integration. The `/api/v1/intercept` endpoint is the forward-looking, SEP-1763-aligned API for new integrations.
- **[runtime-action-interception-architecture](runtime-action-interception-architecture.md)** — defines the layered enforcement model. Hooks add a new enforcement point (Layer A, client-side) alongside the existing proxy enforcement points. The `/api/v1/intercept` endpoint becomes the unified API that all Layer A enforcement points call.

## Open Questions

1. **Hook distribution:** How should hook scripts be distributed to users? Options: (a) ship in the `maybe-dont` binary as an export command (e.g., `maybe-dont hooks export --agent claude-code`), (b) publish to a hooks registry if one emerges, (c) document in the installation guide with copy-paste snippets.

2. **Policy caching:** Hook scripts add latency to every tool call. Should the hook script cache recent policy decisions locally with a TTL to reduce gateway round-trips?

3. **Unified hook script vs. per-agent scripts:** A single script that auto-detects the agent format is simpler to maintain but harder to debug. Per-agent scripts are more explicit. Which approach?

4. **SEP-1763 timeline:** Should we build a prototype interceptor implementation now against the draft spec, or wait for SDK support? The risk of building early is spec changes; the risk of waiting is missing early adoption.

5. **Interceptor policy distribution:** MCP interceptors define execution but not policy management. How do interceptor instances receive policy updates? Options: embedded config, remote policy API, file watch.

6. **Shell tool name discovery:** The `shell_tool_names` config allows the gateway to detect CLI commands, but new agents may introduce new tool names. Should the hook script hint the tool type via `config`, or should the gateway maintain a comprehensive default list?

7. **Observability responses:** Should the `/api/v1/intercept` endpoint also return observability results (audit confirmation) alongside validation/mutation results? SEP-1763 treats observability as fire-and-forget, but hook callers may want confirmation that the event was logged.

## References

- [SEP-1763: Interceptors for MCP](https://github.com/modelcontextprotocol/modelcontextprotocol/issues/1763) — Draft proposal
- [Claude Code Hooks Guide](https://code.claude.com/docs/en/hooks-guide)
- [Cursor Hooks Documentation](https://cursor.com/docs/agent/hooks)
- [Gemini CLI Hooks Reference](https://geminicli.com/docs/hooks/reference/)
- [Cline Hook Reference](https://docs.cline.bot/features/hooks/hook-reference)
- [GitHub Copilot CLI Hooks](https://docs.github.com/en/copilot/how-tos/copilot-cli/use-hooks)
- [VS Code Agent Hooks](https://code.visualstudio.com/docs/copilot/customization/hooks)
- [OpenAI Codex CLI Reference](https://developers.openai.com/codex/cli/)
- [MCP Tool Approval Discussion #1203](https://github.com/modelcontextprotocol/modelcontextprotocol/discussions/1203)
