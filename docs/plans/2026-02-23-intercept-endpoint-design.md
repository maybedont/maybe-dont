# Intercept Endpoint Design

## Overview

Add a `POST /api/v1/intercept` endpoint aligned with SEP-1763 (MCP Interceptors) that provides a unified API for agent hook scripts to call the gateway for policy validation. This replaces the need for agent-specific endpoints and supports both request-phase (pre-tool) and response-phase (post-tool) validation.

## Motivation

Existing validation endpoints are caller-specific:

| Endpoint | Designed For | Limitation |
|----------|-------------|------------|
| `/api/v1/cli/validate` | `maybe-dont cli` binary | CLI-only; no MCP tool support |
| `/api/v1/action/validate` | OpenHands | MCP-only (`mcp_expression`); no CLI expression support |

Agent hooks intercept all tool types in a single event stream. They need one endpoint that accepts any tool type and routes to the correct evaluation path internally.

## Architecture

### Shared evaluation layer

Extract core validation logic currently duplicated across CLI and action handlers into a `PolicyEvaluator`:

```go
// policy_evaluator.go

type PolicyEvaluator struct {
    CELEngine           *CELPolicyEngine
    AIEngine            *AIPolicyEngine
    CELResponseEngine   *CELResponsePolicyEngine
    AIResponseEngine    *AIResponsePolicyEngine
    ResponseChain       *ResponseValidationChain
    MaxBlockingMs       int
    MaxRuleEvaluationMs int
    Logger              *config.SessionLogger
}

func (e *PolicyEvaluator) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest) ValidationResults
func (e *PolicyEvaluator) EvaluateCLICommand(ctx context.Context, req *CLIValidationRequest) ValidationResults
func (e *PolicyEvaluator) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error)
```

Each method encapsulates:
- Blocking budget creation from configured `MaxBlockingMs`
- Calling CEL engine then AI engine with budget
- Merging results (allowed/deny, `AuditModeBypass` clearing on enforced deny)
- Returning results with `AsyncCompletion` channel intact for async audit

### Async audit helper

Shared goroutine + select + timeout pattern:

```go
func WriteAsyncAuditCompletion(
    writer AuditWriter,
    logger *config.SessionLogger,
    asyncCompletion <-chan AsyncCompletion,
    buildEntry func(AsyncCompletion) *AuditEntry,
)
```

Each handler passes a closure that builds its handler-specific audit entry from the async result. The goroutine management, timeout, and error handling are shared.

### Existing handler refactoring

CLI and action handlers adopt `PolicyEvaluator` — their `evaluatePolicies` methods become thin wrappers:

```go
// CLI handler
func (h *CLIValidationHandler) evaluatePolicies(ctx context.Context, req *CLIValidationRequest) ValidationResults {
    return h.evaluator.EvaluateCLICommand(ctx, req)
}

// Action handler
func (h *ActionValidationHandler) evaluatePolicies(ctx context.Context, req *ActionValidationRequest) ValidationResults {
    mcpReq := h.toCallToolRequest(req)
    return h.evaluator.EvaluateToolCall(ctx, mcpReq)
}
```

Existing tests continue to pass unchanged — this is a refactor, not a behavior change.

### Intercept handler

New standalone handler with SEP-1763-aligned request/response types.

#### Request schema

```go
type InterceptRequest struct {
    Event   string            `json:"event"`
    Phase   string            `json:"phase"`
    Payload InterceptPayload  `json:"payload"`
    Context *InterceptContext `json:"context,omitempty"`
    Config  *InterceptConfig  `json:"config,omitempty"`
}

type InterceptPayload struct {
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments,omitempty"`
    Result    *InterceptResult       `json:"result,omitempty"`
}

type InterceptResult struct {
    Content []InterceptContent `json:"content"`
}

type InterceptContent struct {
    Type string `json:"type"`
    Text string `json:"text"`
}

type InterceptContext struct {
    Principal *InterceptPrincipal `json:"principal,omitempty"`
    TraceID   string              `json:"traceId,omitempty"`
    SpanID    string              `json:"spanId,omitempty"`
    Timestamp string              `json:"timestamp,omitempty"`
    SessionID string              `json:"sessionId,omitempty"`
}

type InterceptPrincipal struct {
    Type string `json:"type,omitempty"`
    ID   string `json:"id,omitempty"`
}

type InterceptConfig struct {
    WorkingDirectory string `json:"working_directory,omitempty"`
}
```

#### Response schema

```go
type InterceptResponse struct {
    Interceptor string              `json:"interceptor"`
    Type        string              `json:"type"`
    Phase       string              `json:"phase"`
    Valid       bool                `json:"valid"`
    Severity    string              `json:"severity"`
    Messages    []InterceptMessage  `json:"messages"`
    Modified    bool                `json:"modified,omitempty"`
    Payload     *InterceptPayload   `json:"payload,omitempty"`
    DurationMs  int64               `json:"durationMs"`
    Info        InterceptInfo       `json:"info"`
}

type InterceptMessage struct {
    Path     string `json:"path,omitempty"`
    Message  string `json:"message"`
    Severity string `json:"severity"`
}

type InterceptInfo struct {
    RequestID     string                  `json:"request_id"`
    ServerVersion string                  `json:"server_version"`
    Results       []InterceptPolicyResult `json:"results"`
}

type InterceptPolicyResult struct {
    PolicyName string `json:"policy_name"`
    PolicyType string `json:"policy_type"`
    Action     string `json:"action"`
    Message    string `json:"message,omitempty"`
}
```

#### Routing logic

**Request phase:**
1. If `payload.name` is in configured `ShellToolNames` → parse `arguments.command` into command + args → `evaluator.EvaluateCLICommand()`
2. Otherwise → convert to `mcp.CallToolRequest` → `evaluator.EvaluateToolCall()`

**Response phase:**
1. Convert to `mcp.CallToolRequest` + `mcp.CallToolResult` from payload
2. Call `evaluator.EvaluateResponse()`
3. If redacted → return `type: "mutation"` with modified payload
4. Otherwise → return `type: "validation"` with pass/fail

#### Validation rules

- `event` required, must be `"tools/call"` (return 400 for unsupported events)
- `phase` required, must be `"request"` or `"response"`
- `payload.name` required
- `payload.result` required when `phase: "response"`

#### Severity mapping

| Scenario | `valid` | `severity` |
|----------|---------|------------|
| All allow | `true` | `"info"` |
| Audit-only deny | `true` | `"warn"` |
| Enforced deny | `false` | `"error"` |

#### Audit logging

- `Source: "intercept"`
- Shell tools populate both `Tool` and `CLI` fields
- MCP tools populate `Tool` only
- `UpstreamRequest.ExternalID` mapped from `context.traceId`
- `UpstreamRequest.SessionID` mapped from `context.sessionId`
- Async completion uses shared `WriteAsyncAuditCompletion` helper

### Handler config

```go
type InterceptHandlerConfig struct {
    Enabled               bool
    ShellToolNames        []string
    Logger                *config.SessionLogger
    Version               string
    AuditWriter           AuditWriter
    Evaluator             *PolicyEvaluator
    IncludeArgumentValues bool
}
```

### Configuration

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

Added to `config.go` as `InterceptConfig`. Defaults embedded in `internal/config/defaults/maybe-dont.yaml`.

## Files changed

| File | Change |
|------|--------|
| `policy_evaluator.go` (new) | Shared `PolicyEvaluator` + `WriteAsyncAuditCompletion` |
| `policy_evaluator_test.go` (new) | Tests for shared evaluation logic |
| `intercept_handler.go` (new) | Handler, request/response types, routing |
| `intercept_handler_test.go` (new) | Handler-specific tests |
| `cli_validation.go` | Refactor to use `PolicyEvaluator` |
| `action_validation.go` | Refactor to use `PolicyEvaluator` |
| `server.go` | Register intercept handler, create shared `PolicyEvaluator` |
| `config/config.go` | Add `InterceptConfig` struct |
| `config/defaults/maybe-dont.yaml` | Add `intercept` defaults |

## References

- Issue: [#133](https://github.com/maybedont/maybe-dont/issues/133)
- Spec: `docs/specs/agent-hook-and-interceptor-integration.md`
- SEP-1763: https://github.com/modelcontextprotocol/modelcontextprotocol/issues/1763
