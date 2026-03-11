# Agent Hook and MCP Interceptor Integration

## Status
**Draft** — intercept endpoint shipped (#133); hook scripts shipped (#131, Phases 1–4.1); Cursor mutation path, shell tests, and documentation remain

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

### Per-Agent Details

**Claude Code** — Events: `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, plus lifecycle. Blocking: exit code 2 or JSON `permissionDecision: deny`. Config: `.claude/settings.json`. Ref: https://code.claude.com/docs/en/hooks-guide

**Cursor** — Events: `beforeShellExecution`, `afterShellExecution`, `beforeMCPExecution`, `afterMCPExecution`, `preToolUse`, `postToolUse`, plus lifecycle. Dedicated events for shell and MCP execution (most granular hook system). `beforeMCPExecution` and `beforeReadFile` are **fail-closed**. Blocking: exit code 2 or JSON `permission: deny`. Config: `.cursor/hooks/`. Ref: https://cursor.com/docs/agent/hooks

**Gemini CLI** — Events: `BeforeTool`, `AfterTool`, plus model-layer hooks (`BeforeModel`, `AfterModel`, `BeforeToolSelection`) unique to Gemini. Blocking: exit code 2 or JSON `decision: deny`. Config: `settings.json`. Ref: https://geminicli.com/docs/hooks/reference/

**Cline** — Events: `PreToolUse`, `PostToolUse`, plus lifecycle. Blocking: JSON `cancel: true`. Config: `.clinerules/hooks/` (project) or `~/Documents/Cline/Rules/Hooks/` (global). **macOS/Linux only.** Ref: https://docs.cline.bot/features/hooks/hook-reference

**GitHub Copilot (CLI + VS Code)** — Events: `PreToolUse`, `PostToolUse`, `SessionStart`, `SessionEnd`, `UserPromptSubmit`. Blocking: JSON `permissionDecision: deny`. Config: `.github/hooks/*.json` (shared format). Ref: https://docs.github.com/en/copilot/how-tos/copilot-cli/use-hooks, https://code.visualstudio.com/docs/copilot/customization/hooks

**OpenAI Codex CLI** — No user-defined hooks. Approval-based system only. Use MCP gateway proxy for MCP tools; `maybe-dont cli` skill for CLI commands. Ref: https://developers.openai.com/codex/cli/

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
| 400 | `intercept_disabled` | Intercept endpoint is not enabled |
| 400 | `invalid_request` | Malformed JSON body or decoding failure |
| 400 | `missing_event` | Required `event` field is empty |
| 400 | `unsupported_event` | `event` is not a supported type (only `tools/call` currently) |
| 400 | `missing_phase` | Required `phase` field is empty |
| 400 | `invalid_phase` | `phase` is not `"request"` or `"response"` |
| 400 | `missing_payload_name` | Required `payload.name` field is empty (also returned when `payload` is null or omitted) |
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

## Relationship to Existing Specs and Endpoints

- **[cli-proxy-for-ai-agents](cli-proxy-for-ai-agents.md)** — defines `/api/v1/cli/validate`. The new `/api/v1/intercept` endpoint supersedes this for hook-based integrations, but `/api/v1/cli/validate` remains for the `maybe-dont cli` binary which has its own request/response contract.
- **[openhands-integration](openhands-integration.md)** — defines `/api/v1/action/validate`. Similarly, this endpoint remains for OpenHands' specific integration. The `/api/v1/intercept` endpoint is the forward-looking, SEP-1763-aligned API for new integrations.
- **[runtime-action-interception-architecture](runtime-action-interception-architecture.md)** — defines the layered enforcement model. Hooks add a new enforcement point (Layer A, client-side) alongside the existing proxy enforcement points. The `/api/v1/intercept` endpoint becomes the unified API that all Layer A enforcement points call.

## Resolved Design Decisions

The following questions were resolved during implementation planning:

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| 1 | **Hook distribution** | `maybe-dont hooks export --agent <name>` CLI command + docs copy-paste snippets | Mirrors existing `maybe-dont skill view` pattern. Binary embeds hook scripts. |
| 2 | **Policy caching** | No caching in v1 | Gateway is typically local (sub-50ms). Adds complexity and stale cache risks. Revisit if latency becomes a real complaint. |
| 3 | **Unified vs per-agent scripts** | Per-agent self-contained scripts with core logic inlined | Agent I/O formats are genuinely different (different JSON field names, different deny output formats). A polyglot detector is fragile. Each exported script is one file with no external dependencies beyond bash/curl/jq. |
| 4 | **SEP-1763 prototype** | Wait for SDK adoption | The intercept endpoint already aligns with the SEP-1763 schema. Building a prototype interceptor now risks churn against a draft spec. Hooks are the pragmatic bridge. |
| 5 | **Interceptor policy distribution** | N/A for v1 | Hooks call the gateway which already has policies loaded. Not relevant until interceptor adoption. |
| 6 | **Shell tool name discovery** | Gateway-side default list (already implemented in `shell_tool_names` config) | Hook scripts don't hint tool type — the gateway detects shell tools from its configured list. Solved. |
| 7 | **Observability responses** | No in v1 | SEP-1763 treats observability as fire-and-forget. The gateway audit log is the source of truth. |
| 8 | **Hook script language** | Bash + curl + jq | Zero interpreter dependencies beyond standard tools. Fast startup (no runtime overhead). Hook scripts are reference implementations — users can rewrite in Python or any language. |
| 9 | **Fail-open behavior** | Hooks fail open (allow + stderr warning) when gateway is unreachable | Matches CLI gateway behavior. Exception: Cursor `beforeMCPExecution` is inherently fail-closed by the agent runtime — hook script can't override that. |
| 10 | **Gateway URL config** | `MAYBE_DONT_URL` environment variable, default `http://localhost:8080` | Simple, consistent. |
| 11 | **PostToolUse hooks** | Include in v1 | The intercept endpoint already supports `phase: "response"`. Only Cursor `afterMCPExecution` can modify output (redaction); all other agents' post-tool hooks are observability-only. |
| 12 | **Cody** | Exclude | Cody uses the same VS Code hook format as Copilot (`.github/hooks/`). The Copilot hook script works for Cody. Document as a note on the Copilot page. Consider removing the standalone Cody docs page. |
| 13 | **Cline** | Include | 5M+ installs, 58K GitHub stars, top-3 VS Code-based coding agent. |
| 14 | **Curl timeout** | 30 seconds default | Matches CLI gateway default. Fail open on timeout. |
| 15 | **Double validation (hooks + MCP gateway)** | Solved at config level via hook matchers, not in script | See [Recommended Deployment Patterns](#recommended-deployment-patterns). |

## Hook Script Implementation Plan

### Scope: 5 Agents, Pre + Post Tool Hooks

| Agent | Pre-tool event | Post-tool event | Config location |
|-------|---------------|----------------|-----------------|
| Claude Code | `PreToolUse` | `PostToolUse` | `.claude/settings.json` |
| Cursor | `beforeShellExecution` + `beforeMCPExecution` | `afterShellExecution` + `afterMCPExecution` | `.cursor/hooks/` |
| Gemini CLI | `BeforeTool` | `AfterTool` | `settings.json` |
| Cline | `PreToolUse` | `PostToolUse` | `.clinerules/hooks/` |
| GitHub Copilot | `PreToolUse` | `PostToolUse` | `.github/hooks/*.json` |

### Script Architecture

Each agent gets **one self-contained bash script** with core logic inlined. The script handles both pre-tool and post-tool events by detecting the phase from the input JSON. The same script file is referenced from multiple hook config entries.

```
maybe-dont-claude-code.sh       ← one file, handles PreToolUse + PostToolUse
maybe-dont-cursor.sh            ← one file, handles all 4 Cursor events
maybe-dont-gemini-cli.sh        ← one file, handles BeforeTool + AfterTool
maybe-dont-cline.sh             ← one file, handles PreToolUse + PostToolUse
maybe-dont-copilot.sh           ← one file, handles PreToolUse + PostToolUse
```

Internal structure of each script:

```
┌─────────────────────────────────────────────┐
│  #!/usr/bin/env bash                        │
│                                             │
│  ┌─ Core functions (inlined) ─────────────┐ │
│  │ md_call_gateway()   — POST + fail-open │ │
│  │ md_is_denied()      — check valid field│ │
│  │ md_get_reason()     — extract messages │ │
│  │ md_check_deps()     — verify jq, curl  │ │
│  └────────────────────────────────────────┘ │
│                                             │
│  ┌─ Agent-specific translation ───────────┐ │
│  │ Read stdin JSON                        │ │
│  │ Detect phase (pre vs post)             │ │
│  │ Extract tool_name, arguments, result   │ │
│  │ Extract context (session_id, cwd)      │ │
│  │ Build /api/v1/intercept request        │ │
│  │ Call md_call_gateway()                 │ │
│  │ Translate response to agent format     │ │
│  │ Exit with agent-specific code          │ │
│  └────────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
```

### Phase Detection Per Agent

Each agent signals the event type differently in its stdin JSON:

| Agent | Phase detection method |
|-------|----------------------|
| Claude Code | `hook_event_name` field: `"PreToolUse"` or `"PostToolUse"` |
| Cursor | Payload shape: pre events lack `output`/`result` fields; post events include them. Also inferable from the event-specific JSON structure. |
| Gemini CLI | `hook_event_name` field: `"BeforeTool"` or `"AfterTool"` |
| Cline | Top-level key: `preToolUse` vs `postToolUse` |
| Copilot | `hook_event_name` field: `"PreToolUse"` or `"PostToolUse"` |

### MCP vs CLI Detection

**The hook script does not need to distinguish MCP from CLI tools.** The gateway's intercept endpoint handles routing internally by checking `payload.name` against its configured `shell_tool_names` list.

The MCP-vs-CLI distinction matters only at the **hook configuration level** (matcher patterns), which controls when the hook fires:

| Agent | CLI matcher | MCP matcher |
|-------|-----------|------------|
| Claude Code | `"Bash"` | `"mcp__.*"` (regex) |
| Cursor | `beforeShellExecution` event | `beforeMCPExecution` event |
| Gemini CLI | Tool name regex for shell tools | Tool name regex for MCP tools |
| Cline | `"execute_command"` | MCP tool names |
| Copilot | `"Bash"` or `"bash"` | MCP tool names |

### Per-Agent I/O Translation

#### Claude Code

**Input (PreToolUse):**
```json
{
  "session_id": "abc123",
  "cwd": "/path/to/project",
  "hook_event_name": "PreToolUse",
  "tool_name": "Bash",
  "tool_input": {"command": "gh repo delete my-repo"}
}
```

**Deny output:**
```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Repository deletion blocked by policy"
  }
}
```

**Allow:** Exit 0, no output.

**Post-tool:** Same script, detects `PostToolUse` from `hook_event_name`. Sends `phase: "response"` with tool result. Logs warnings to stderr (cannot modify output).

#### Cursor

**Input (beforeShellExecution):**
```json
{"command": "gh repo delete my-repo"}
```

**Input (beforeMCPExecution):**
```json
{"serverName": "github", "toolName": "create_issue", "arguments": {"title": "..."}}
```

**Deny output:**
```json
{"permission": "deny"}
```

**Post-tool (afterMCPExecution) — unique:** Can return `updated_mcp_tool_output` with redacted content from the gateway's mutation response. This is the only agent that supports output modification from hooks.

#### Gemini CLI

**Input (BeforeTool):**
```json
{
  "session_id": "abc123",
  "cwd": "/path/to/project",
  "hook_event_name": "BeforeTool",
  "tool_name": "Bash",
  "tool_input": {"command": "gh repo delete my-repo"},
  "mcp_context": {}
}
```

**Deny output:**
```json
{"decision": "deny", "reason": "Repository deletion blocked by policy"}
```

**Allow:** Exit 0, no output.

#### Cline

**Input (PreToolUse):**
```json
{
  "taskId": "abc123",
  "clineVersion": "3.17.0",
  "timestamp": 1736654400000,
  "workspacePath": "/path/to/project",
  "preToolUse": {
    "tool": "execute_command",
    "parameters": {"command": "gh repo delete my-repo"}
  }
}
```

**Deny output:**
```json
{"cancel": true, "errorMessage": "Repository deletion blocked by policy"}
```

**Allow:** Exit 0, empty JSON `{}` or no output.

#### GitHub Copilot

**Input (PreToolUse):**
```json
{
  "hook_event_name": "PreToolUse",
  "tool_name": "Bash",
  "tool_input": {"command": "gh repo delete my-repo"}
}
```

**Deny output:**
```json
{
  "hookSpecificOutput": {
    "permissionDecision": "deny",
    "permissionDecisionReason": "Repository deletion blocked by policy"
  }
}
```

**Allow:** Exit 0, no output.

### Recommended Deployment Patterns

When hooks and the MCP gateway are used together, hook matchers should be scoped to CLI/shell tools only to avoid double-validating MCP tool calls:

| Setup | Hook matchers | MCP Gateway | Use case |
|-------|--------------|-------------|----------|
| **Both (recommended)** | CLI tools only (`Bash`, `execute_command`) | Yes | Deterministic CLI enforcement via hooks, full MCP interception via gateway. No double validation. |
| **Hooks only** | All tools (CLI + MCP) | Not used | Lightweight, no proxy needed. Post-tool response scanning limited to agents that support it. |
| **MCP gateway only** | Not used | Yes | Full MCP interception with response validation/redaction. No CLI enforcement unless using `maybe-dont cli` skill. |

The exported config snippets default to the **"Both"** pattern with CLI-only matchers. A commented-out "hooks only" variant with broader matchers is included.

### CLI Command: `maybe-dont hooks`

New subcommand mirroring the existing `maybe-dont skill` pattern:

```bash
# List available hook agents
maybe-dont hooks list

# Export the hook script to stdout
maybe-dont hooks export --agent claude-code > maybe-dont-hook.sh
chmod +x maybe-dont-hook.sh

# Export the agent config snippet to stdout
maybe-dont hooks export --agent claude-code --config
```

**`hooks list`** output:
```
Available hook agents:
  claude-code    Claude Code PreToolUse/PostToolUse hooks
  cursor         Cursor shell and MCP execution hooks
  gemini-cli     Gemini CLI BeforeTool/AfterTool hooks
  cline          Cline PreToolUse/PostToolUse hooks
  copilot        GitHub Copilot PreToolUse/PostToolUse hooks

Use 'maybe-dont hooks export --agent <name>' to output a hook script.
Use 'maybe-dont hooks export --agent <name> --config' to output a config snippet.
```

**`hooks export --agent <name>`** outputs a self-contained bash script to stdout.

**`hooks export --agent <name> --config`** outputs the ready-to-paste config snippet for that agent, showing how to wire the hook script into the agent's configuration. Defaults to the "Both" deployment pattern (CLI-only matchers). Includes commented-out "hooks only" variant.

### Hook Script Runtime Behavior

- **Dependencies:** `bash`, `curl`, `jq`. Script checks for `jq` and `curl` at startup and exits with a clear error message if missing.
- **Gateway URL:** `$MAYBE_DONT_URL` environment variable, defaults to `http://localhost:8080`.
- **Timeout:** 30-second curl timeout. On timeout, fail open (allow) with stderr warning.
- **Fail-open:** If the gateway is unreachable, the script allows the tool call and writes a warning to stderr. Exception: Cursor `beforeMCPExecution` is inherently fail-closed by the agent runtime — the script cannot override this.
- **Context passthrough:** Scripts forward `cwd`/`workspacePath`, `session_id`/`taskId`, and any trace context from the agent input into the intercept request's `context` and `config` fields.
- **Post-tool observability:** For agents other than Cursor, post-tool hooks are observability-only — they call the gateway with `phase: "response"` for audit logging and policy evaluation, but cannot modify output. Warnings/denials are logged to stderr.
- **Post-tool mutation (Cursor only):** Cursor's `afterMCPExecution` hook can return `updated_mcp_tool_output`. When the gateway returns a mutation response (`type: "mutation"`, `modified: true`), the Cursor post-tool hook returns the redacted payload, applying the gateway's redaction to the actual tool output.

### Embedding and Distribution

Hook scripts are embedded in the binary using Go's `embed` package, following the same pattern as `internal/skills/`:

```
internal/hooks/
├── hooks.go                      ← Go API: ListHooks(), GetHook(), embed directives
├── hooks_test.go
├── claude-code.sh
├── cursor.sh
├── gemini-cli.sh
├── cline.sh
├── copilot.sh
├── claude-code.config.json       ← config snippet for Claude Code
├── cursor.config.json            ← config snippet for Cursor
├── gemini-cli.config.json        ← config snippet for Gemini CLI
├── cline.config.json             ← config snippet for Cline
└── copilot.config.json           ← config snippet for Copilot
```

CLI command in `cmd/hooks.go` mirrors `cmd/skill.go`.

## Future Improvements

Potential improvements identified during code review of the v1 hook scripts (PR #139):

### 1. Cursor `beforeMCPExecution` fail-open hardening

All scripts use `set -euo pipefail`, which means an unexpected error (e.g., jq failing on genuinely malformed input) causes a non-zero exit. For most agents this is harmless — non-zero exit = allow. But Cursor's `beforeMCPExecution` is **inherently fail-closed by the agent runtime**: a non-zero exit blocks the tool call regardless of the script's intent. The script documents this (cursor.sh:365-367) but doesn't mitigate it. A targeted `trap 'exit 0' ERR` within the `beforeMCPExecution` branch would ensure fail-open even on unexpected bash/jq errors, without weakening error handling in the rest of the script.

### 2. Large request bodies via shell argument

PostToolUse hooks serialize the entire tool result into the curl request body via `-d "$body"` as a shell argument. For very large tool outputs (e.g., reading a large file, verbose command output), this could hit OS argument length limits (~2MB on macOS/Linux). A more robust approach would write the request body to a temp file and use `--data-binary @"$tmpfile"` instead. Not a practical concern for most tool results, but worth hardening for edge cases.

### 3. Configurable curl timeout

The 30-second `--max-time 30` on curl is hardcoded. For interactive coding agents, 30 seconds of blocking can feel sluggish if the gateway is slow or under load. A `MAYBE_DONT_TIMEOUT` environment variable (defaulting to 30) would let users tune this without modifying the script. This is especially relevant for post-tool hooks where the timeout adds latency to the tool response pipeline without providing blocking value (post-tool is observability-only for most agents).

### 4. Additional agent support

The v1 set covers the 5 major agents with mature hook systems. Agents evaluated and excluded:

| Agent | Status | Reason |
|-------|--------|--------|
| **Windsurf** | Monitor | Has Cascade Hooks but the format is automation-focused, not request/deny compatible |
| **Kiro** | Monitor | Too new; JSON hook format not yet stable or publicly documented |
| **Aider** | No hooks | No native hook system; use CLI proxy |
| **Continue** | No hooks | No native hook API; use CLI proxy or MCP gateway |
| **Replit Agent** | No hooks | Browser-only environment; no local hook support |
| **Cody** | Covered | Uses Copilot hook format (`.github/hooks/`); documented as a note on copilot.config.json |

Windsurf and Kiro are the most likely candidates for future additions as their hook formats mature.

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
