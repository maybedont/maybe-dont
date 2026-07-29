# OpenHands Integration

> **Status**: See [README.md](README.md)

## Context

GitHub issue [#22](https://github.com/maybedont/maybe-dont/issues/22) requests integration with OpenHands. The OpenHands agent-sdk has a pluggable security analyzer framework (GraySwan is the reference). We're building:

1. **PR 1 (maybe-dont)**: A dedicated `POST /api/v1/action/validate` REST endpoint on the gateway
2. **PR 2 (OpenHands SDK)**: A `MaybeDontAnalyzer` security analyzer that calls the endpoint

PR 1 ships first (merge, tag, release). Then PR 2 opens against `OpenHands/software-agent-sdk` from a `maybedont` org fork.

**How this fits with existing integrations**:

| Layer | What | Covers |
|---|---|---|
| **Security Analyzer** (PR 2) | Pre-execution validation via REST endpoint | ALL actions: shell commands, file ops, browser, tool calls |
| **MCP Proxy** (existing docs) | Execution-time validation + proxying | Only MCP tool calls routed through the gateway |

The MCP proxy integration already exists (maybedont.ai/docs) and requires no code changes to OpenHands. This PR adds the security analyzer layer for non-MCP actions.

---

## PR 1: Maybe Don't Gateway — Action Validation Endpoint

### Approach

Add `POST /api/v1/action/validate` — a purpose-built REST endpoint for validating agent actions before execution.

**Design derived from OpenHands' `SecurityAnalyzerBase` contract**: The analyzer receives an `ActionEvent` (tool name, structured arguments, agent reasoning) and must return a `SecurityRisk` (HIGH/MEDIUM/LOW/UNKNOWN). The endpoint accepts what an `ActionEvent` carries and returns what maps to `SecurityRisk`. The wire format aligns with our Action Envelope spec where it naturally fits.

**Key implementation insight**: The gateway already has `EvaluateToolCall()` on both CEL and AI engines that accept `mcp.CallToolRequest`. The new handler converts the request into an `mcp.CallToolRequest` and delegates to these existing methods. Zero new evaluation logic.

### Request/Response Contract

**Request** — maps from OpenHands `ActionEvent` fields:
```json
{
  "action_type": "tool_call",
  "target": "execute_bash",
  "parameters": { "command": "rm -rf /tmp/important-data" },
  "actor": "openhands-agent",
  "context": {
    "thought": "I need to clean up temporary files",
    "summary": "removing temporary data"
  }
}
```

| Request field | Derived from (OpenHands) | Purpose |
|---|---|---|
| `action_type` | Always `"tool_call"` for v1 | Identifies the action category |
| `target` | `ActionEvent.tool_name` | What's being called |
| `parameters` | `ActionEvent.tool_call.function.arguments` (parsed from JSON) | Structured arguments for policy evaluation |
| `actor` | Configurable client_id (default `"openhands"`) | Audit attribution |
| `context.thought` | `ActionEvent.thought` (joined text) | Agent reasoning — enriches AI rule evaluation |
| `context.summary` | `ActionEvent.summary` | Concise action description |

**Response** — maps to OpenHands `SecurityRisk`:
```json
{
  "request_id": "abc123...",
  "allowed": false,
  "risk_level": "high",
  "message": "Action denied by policy",
  "results": [
    { "policy_name": "no-destructive-ops", "policy_type": "ai", "action": "deny", "message": "..." }
  ],
  "server_version": "v1.x.x"
}
```

| Response field | Maps to (OpenHands) | Purpose |
|---|---|---|
| `risk_level` | `SecurityRisk` enum directly | The primary value the analyzer needs |
| `allowed` | Informs HIGH vs other levels | Binary policy decision |
| `results` | Not directly used by analyzer | Transparency — which policies fired |
| `message` | Logging/debugging | Human-readable explanation |

**Risk level derivation**:

| Condition | `risk_level` |
|---|---|
| `allowed=false` | `"high"` |
| `allowed=true`, AuditModeBypass (deny result in audit_only mode) | `"medium"` |
| `allowed=true`, clean pass (at least one policy evaluated) | `"low"` |
| No policies evaluated (no engines, no rules, or engine errors) | `"unknown"` |

**Edge case behavior** (all return HTTP 200, consistent with CLI handler fail-open):

| Scenario | `allowed` | `risk_level` | Rationale |
|---|---|---|---|
| No engines configured | `true` | `"unknown"` | Valid request, can't evaluate. OpenHands' default `ConfirmRisky` blocks on UNKNOWN (safe). |
| Engines exist, no policies loaded | `true` | `"unknown"` | `EvaluateToolCall()` returns empty results |
| Engine errors (CEL or AI fails) | `true` | `"unknown"` | Fail-open, same as CLI handler. Partial results from healthy engine still included in `results`. |
| audit_only + rule denies (`AuditModeBypass=true`) | `true` | `"medium"` | Deny logged but not enforced. Consistent with operator's choice to run audit_only. |
| audit_only + rule allows (or no match) | `true` | `"low"` | Clean pass — audit_only mode doesn't affect allow outcomes. |
| Bad request (missing target, bad JSON, wrong content-type) | HTTP 400 | — | JSON error response (caller error, not a policy result) |

### Gateway Files to Create

**`internal/gateway/action_validation.go`** — Request/response types + HTTP handler:
- `ActionValidationRequest` — `action_type`, `target`, `parameters` (map[string]interface{}), `actor`, `context`
- `ActionContext` — `thought`, `summary`
- `ActionValidationResponse` — `request_id`, `allowed`, `risk_level`, `message`, `results`, `server_version`
- `ActionValidationHandler` — HTTP handler struct (same pattern as `CLIValidationHandler`)
  - `ServeHTTP()`: Parse request → convert to `mcp.CallToolRequest` → call `evaluatePolicies()` → derive risk_level → return response
  - `evaluatePolicies()`: Reuse same pattern as CLI handler — calls `CELEngine.EvaluateToolCall()` and `AIEngine.EvaluateToolCall()` with blocking budget
  - `deriveRiskLevel()`: Maps ValidationResults → "high"/"medium"/"low"/"unknown"
- Reuse `CLIPolicyResult` for the `results` array

**`internal/gateway/action_validation_test.go`** — Tests

### Gateway Files to Modify

**`internal/gateway/server.go`** — Register the new route in both initSSEServer and initHTTPServer

### Conversion Logic

```go
func (h *ActionValidationHandler) toCallToolRequest(req *ActionValidationRequest) mcp.CallToolRequest {
    return mcp.CallToolRequest{
        Request: mcp.Request{Method: "tools/call"},
        Params: struct {
            Name      string                 `json:"name"`
            Arguments map[string]interface{} `json:"arguments,omitempty"`
        }{
            Name:      req.Target,
            Arguments: req.Parameters,
        },
    }
}
```

### No New Config Needed

The endpoint is available whenever request validation engines (CEL/AI) are configured. No additional flag — it reuses the existing engine configuration.

---

## PR 2: OpenHands SDK — MaybeDontAnalyzer

See full plan in the implementation tracking document.

## Workflow

1. ~~Implement PR 1 (gateway endpoint)~~ — **Done** → [PR #127](https://github.com/maybedont/maybe-dont/pull/127)
2. ~~Merge PR 1, tag, release gateway~~ — **Done** → v1.3.0
3. ~~Fork `OpenHands/software-agent-sdk` to `maybedont` org~~ — **Done**
4. ~~Implement PR 2 (MaybeDontAnalyzer) on the fork~~ — **Done**
5. ~~Open PR against `OpenHands/software-agent-sdk`~~ — **Done** → [software-agent-sdk#2142](https://github.com/OpenHands/software-agent-sdk/pull/2142)
6. ~~PR 3: OpenHands docs — MCP settings page + security guide~~ — **Done** → [docs#350](https://github.com/OpenHands/docs/pull/350)
7. ~~Update maybedont.ai/docs~~ — **Done** → [maybedont-site#82](https://github.com/maybedont/maybedont-site/pull/82)

**Pending**: PRs 2 and 3 awaiting merge by OpenHands maintainers.

## Deferred

- **OpenHands skill** (`skills/maybe-dont/SKILL.md` in `OpenHands/extensions`) — deferred; existing docs coverage is sufficient

## Out of Scope (v2)

- Response validation (requires execution, not just pre-execution validation)
- Conversation history forwarding beyond `thought`
- Gateway-side routing by `action_type`
