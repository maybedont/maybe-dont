# Intercept Endpoint Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `POST /api/v1/intercept` endpoint with shared policy evaluation layer, supporting request and response phase validation for agent hook scripts.

**Architecture:** Extract duplicated policy evaluation logic from CLI and action handlers into a shared `PolicyEvaluator`. Build new intercept handler using that shared evaluator. Intercept handler routes shell tools to CLI evaluation and everything else to MCP tool evaluation, with response phase support via the existing `ResponseValidationChain`.

**Tech Stack:** Go, net/http, mcp-go, CEL, testify

---

### Task 1: Extract PolicyEvaluator from existing handlers

Extract the duplicated evaluation + async audit logic into a shared component. Both CLI and action handlers have identical logic for: blocking budget creation, calling CEL then AI engines, merging results, AuditModeBypass clearing, and async audit completion handling.

**Files:**
- Create: `internal/gateway/policy_evaluator.go`
- Test: `internal/gateway/policy_evaluator_test.go`

**Step 1: Write tests for PolicyEvaluator**

Test the shared evaluation logic in isolation. Use the existing `MockAIProviderClient` and CEL engine test patterns. Key test cases:

```go
// policy_evaluator_test.go

// TestPolicyEvaluator_EvaluateToolCall_NoEngines verifies that when no engines
// are configured, evaluation returns allowed=true with an informational message.
func TestPolicyEvaluator_EvaluateToolCall_NoEngines(t *testing.T)

// TestPolicyEvaluator_EvaluateToolCall_CELDeny verifies that a CEL deny
// result propagates through to the final ValidationResults.
func TestPolicyEvaluator_EvaluateToolCall_CELDeny(t *testing.T)

// TestPolicyEvaluator_EvaluateToolCall_AuditModeBypassClearedOnDeny verifies
// that AuditModeBypass is cleared when an enforced deny overrides it.
func TestPolicyEvaluator_EvaluateToolCall_AuditModeBypassClearedOnDeny(t *testing.T)

// TestPolicyEvaluator_EvaluateCLICommand_CELDeny verifies CLI command
// evaluation routes through correctly.
func TestPolicyEvaluator_EvaluateCLICommand_CELDeny(t *testing.T)

// TestPolicyEvaluator_EvaluateResponse_Allowed verifies response phase
// evaluation delegates to the ResponseValidationChain.
func TestPolicyEvaluator_EvaluateResponse_Allowed(t *testing.T)

// TestPolicyEvaluator_EvaluateResponse_Redacted verifies redaction results
// are returned from response evaluation.
func TestPolicyEvaluator_EvaluateResponse_Redacted(t *testing.T)

// TestWriteAsyncAuditCompletion verifies the async audit goroutine writes
// an entry when completion arrives.
func TestWriteAsyncAuditCompletion(t *testing.T)
```

Use `newTestCELEngineWithDenyRule()` pattern from `action_validation_test.go` to create real CEL engines. Use `config.NewSessionLogger(zaptest.NewLogger(t))` for loggers.

**Step 2: Run tests to verify they fail**

Run: `go test -run TestPolicyEvaluator -v ./internal/gateway/...`
Expected: FAIL — `PolicyEvaluator` type doesn't exist yet.

**Step 3: Implement PolicyEvaluator**

```go
// policy_evaluator.go

// PolicyEvaluator encapsulates the core request and response policy evaluation
// logic shared across all validation handlers (CLI, action, intercept).
type PolicyEvaluator struct {
    CELEngine       *CELPolicyEngine
    AIEngine        *AIPolicyEngine
    ResponseChain   *ResponseValidationChain
    MaxBlockingMs   int
    MaxRuleEvaluationMs int
    Logger          *config.SessionLogger
}

// EvaluateToolCall evaluates an MCP tool call against request policies.
// Creates a blocking budget, calls CEL then AI engines, merges results.
func (e *PolicyEvaluator) EvaluateToolCall(ctx context.Context, req mcp.CallToolRequest) ValidationResults

// EvaluateCLICommand evaluates a CLI command against request policies.
func (e *PolicyEvaluator) EvaluateCLICommand(ctx context.Context, req *CLIValidationRequest) ValidationResults

// EvaluateResponse evaluates a tool response through the response validation chain.
func (e *PolicyEvaluator) EvaluateResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error)

// WriteAsyncAuditCompletion handles the goroutine + select + timeout pattern
// for writing async AI completion audit entries. The buildEntry function creates
// the handler-specific audit entry from the async completion.
func WriteAsyncAuditCompletion(
    writer AuditWriter,
    logger *config.SessionLogger,
    requestID string,
    asyncCompletion <-chan AsyncCompletion,
    buildEntry func(AsyncCompletion) *AuditEntry,
)
```

The `EvaluateToolCall` and `EvaluateCLICommand` methods contain the logic currently in `cli_validation.go:385-484` and `action_validation.go:250-354` — blocking budget creation, engine calls, result merging, AuditModeBypass clearing, default messages. Extract verbatim, parameterizing only the engine method calls (tool vs CLI).

`WriteAsyncAuditCompletion` contains the goroutine pattern from `cli_validation.go:567-603` and `action_validation.go:485-517`.

**Step 4: Run tests to verify they pass**

Run: `go test -run TestPolicyEvaluator -v ./internal/gateway/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/gateway/policy_evaluator.go internal/gateway/policy_evaluator_test.go
git commit -m "feat: extract shared PolicyEvaluator from validation handlers"
```

---

### Task 2: Refactor CLI handler to use PolicyEvaluator

Replace the inline `evaluatePolicies` in CLI handler with delegation to `PolicyEvaluator`. Replace inline async audit goroutine with `WriteAsyncAuditCompletion`.

**Files:**
- Modify: `internal/gateway/cli_validation.go`
- Modify: `internal/gateway/server.go` (create and inject PolicyEvaluator)

**Step 1: Add `Evaluator` field to `CLIValidationHandlerConfig`**

```go
// In CLIValidationHandlerConfig, add:
Evaluator *PolicyEvaluator
```

**Step 2: Replace `evaluatePolicies` with delegation**

```go
func (h *CLIValidationHandler) evaluatePolicies(ctx context.Context, req *CLIValidationRequest) ValidationResults {
    return h.config.Evaluator.EvaluateCLICommand(ctx, req)
}
```

Remove the individual `CELEngine`, `AIEngine`, `MaxBlockingMs`, `MaxRuleEvaluationMs` fields from `CLIValidationHandlerConfig` — they now live in `PolicyEvaluator`.

**Step 3: Replace async audit goroutine with WriteAsyncAuditCompletion**

In `writeAuditEntryWithValidation`, replace the inline goroutine (lines 567-603) with:

```go
if results.AsyncCompletion != nil {
    WriteAsyncAuditCompletion(h.config.AuditWriter, h.config.Logger, ctx.RequestID,
        results.AsyncCompletion, func(completion AsyncCompletion) *AuditEntry {
            return &AuditEntry{
                Source:            "cli",
                // ... handler-specific fields
            }
        })
}
```

**Step 4: Update server.go to create and inject PolicyEvaluator**

In `initSSEServer()` and `initHTTPServer()`, create the shared evaluator and pass it:

```go
evaluator := &PolicyEvaluator{
    CELEngine:           g.policyEngine,
    AIEngine:            g.aiPolicyEngine,
    ResponseChain:       g.responseValidationChain,
    MaxBlockingMs:       g.config.Validation.MaxBlockingMs,
    MaxRuleEvaluationMs: g.config.Validation.MaxRuleEvaluationMs,
    Logger:              g.logger,
}

cliHandler := NewCLIValidationHandler(CLIValidationHandlerConfig{
    Enabled:               g.config.CLIRequestValidation.Enabled,
    ValidateCommands:      g.config.CLIRequestValidation.ValidateCommands,
    Logger:                g.logger,
    Version:               g.version,
    AuditWriter:           g.auditWriter,
    Evaluator:             evaluator,
    IncludeArgumentValues: g.config.Audit.ShouldIncludeArgumentValues(),
})
```

**Step 5: Run ALL existing CLI tests**

Run: `go test -run TestCLI -v ./internal/gateway/...`
Expected: ALL PASS — behavior is identical, only code organization changed.

**Step 6: Commit**

```bash
git add internal/gateway/cli_validation.go internal/gateway/server.go
git commit -m "refactor: CLI handler uses shared PolicyEvaluator"
```

---

### Task 3: Refactor action handler to use PolicyEvaluator

Same pattern as Task 2 but for the action handler.

**Files:**
- Modify: `internal/gateway/action_validation.go`
- Modify: `internal/gateway/server.go` (reuse same PolicyEvaluator instance)

**Step 1: Add `Evaluator` field to `ActionValidationHandlerConfig`**

Remove `CELEngine`, `AIEngine`, `MaxBlockingMs`, `MaxRuleEvaluationMs`. Add `Evaluator *PolicyEvaluator`.

**Step 2: Replace `evaluatePolicies` with delegation**

```go
func (h *ActionValidationHandler) evaluatePolicies(ctx context.Context, req *ActionValidationRequest) ValidationResults {
    mcpReq := h.toCallToolRequest(req)
    return h.config.Evaluator.EvaluateToolCall(ctx, mcpReq)
}
```

**Step 3: Replace async audit goroutine with WriteAsyncAuditCompletion**

Same pattern as CLI handler.

**Step 4: Update server.go**

Pass the same `evaluator` instance to `NewActionValidationHandler`.

**Step 5: Run ALL existing action tests**

Run: `go test -run TestAction -v ./internal/gateway/...`
Expected: ALL PASS

**Step 6: Run full test suite**

Run: `make test`
Expected: ALL PASS — no regressions.

**Step 7: Commit**

```bash
git add internal/gateway/action_validation.go internal/gateway/server.go
git commit -m "refactor: action handler uses shared PolicyEvaluator"
```

---

### Task 4: Add InterceptConfig to configuration

Add the `intercept` configuration section with `enabled` and `shell_tool_names`.

**Files:**
- Modify: `internal/config/config.go` — add `InterceptConfig` struct and field on `Config`
- Modify: `internal/config/defaults/maybe-dont.yaml` — add default values
- Test: `internal/config/config_test.go` — add config loading test

**Step 1: Write failing config test**

```go
// TestInterceptConfigDefaults verifies that intercept config loads with
// correct defaults: enabled=true, default shell tool names.
func TestInterceptConfigDefaults(t *testing.T)

// TestInterceptConfigEnvOverride verifies that intercept config can be
// overridden via MAYBE_DONT_INTERCEPT_ENABLED env var.
func TestInterceptConfigEnvOverride(t *testing.T)
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestInterceptConfig -v ./internal/config/...`
Expected: FAIL — `InterceptConfig` doesn't exist.

**Step 3: Add InterceptConfig**

In `config.go`, after `CLIRequestValidationConfig`:

```go
type InterceptConfig struct {
    Enabled        bool     `mapstructure:"enabled"`
    ShellToolNames []string `mapstructure:"shell_tool_names"`
}
```

In `Config` struct, after `CLIRequestValidation`:

```go
Intercept InterceptConfig `mapstructure:"intercept"`
```

In `defaults/maybe-dont.yaml`, after the `cli_request_validation` section:

```yaml
intercept:
  # Enable the /api/v1/intercept endpoint for agent hook scripts
  enabled: true

  # Tool names that represent shell/CLI execution.
  # When a tool with one of these names is intercepted, the gateway parses
  # the command string and evaluates both cli_expression and mcp_expression.
  shell_tool_names:
    - "Bash"
    - "execute_command"
    - "shell"
    - "run_terminal_command"
    - "run_command"
```

**Step 4: Run tests to verify they pass**

Run: `go test -run TestInterceptConfig -v ./internal/config/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/defaults/maybe-dont.yaml internal/config/config_test.go
git commit -m "feat: add intercept endpoint configuration"
```

---

### Task 5: Implement intercept handler — request types and validation

Build the handler with request/response types and input validation. No evaluation logic yet — just request parsing and error responses.

**Files:**
- Create: `internal/gateway/intercept_handler.go`
- Create: `internal/gateway/intercept_handler_test.go`

**Step 1: Write tests for request validation**

```go
// TestInterceptHandler_InvalidContentType verifies 400 on non-JSON content type.
func TestInterceptHandler_InvalidContentType(t *testing.T)

// TestInterceptHandler_InvalidJSON verifies 400 on malformed JSON.
func TestInterceptHandler_InvalidJSON(t *testing.T)

// TestInterceptHandler_MissingEvent verifies 400 when event field is empty.
func TestInterceptHandler_MissingEvent(t *testing.T)

// TestInterceptHandler_UnsupportedEvent verifies 400 when event is not "tools/call".
func TestInterceptHandler_UnsupportedEvent(t *testing.T)

// TestInterceptHandler_MissingPhase verifies 400 when phase field is empty.
func TestInterceptHandler_MissingPhase(t *testing.T)

// TestInterceptHandler_InvalidPhase verifies 400 when phase is not "request" or "response".
func TestInterceptHandler_InvalidPhase(t *testing.T)

// TestInterceptHandler_MissingPayloadName verifies 400 when payload.name is empty.
func TestInterceptHandler_MissingPayloadName(t *testing.T)

// TestInterceptHandler_ResponsePhaseMissingResult verifies 400 when phase is
// "response" but payload.result is not provided.
func TestInterceptHandler_ResponsePhaseMissingResult(t *testing.T)

// TestInterceptHandler_Disabled verifies 400 when intercept is not enabled.
func TestInterceptHandler_Disabled(t *testing.T)
```

Use `httptest.NewRecorder()` and `httptest.NewRequest()` patterns from existing tests.

**Step 2: Run tests to verify they fail**

Run: `go test -run TestInterceptHandler -v ./internal/gateway/...`
Expected: FAIL

**Step 3: Implement request/response types and handler skeleton**

All SEP-1763-aligned types from the design doc, plus:

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

type InterceptHandler struct {
    config InterceptHandlerConfig
}

func NewInterceptHandler(cfg InterceptHandlerConfig) *InterceptHandler

func (h *InterceptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

`ServeHTTP` validates the request and returns 400 errors. For valid requests, return a placeholder 200 (evaluation logic added in next task).

**Step 4: Run tests to verify they pass**

Run: `go test -run TestInterceptHandler -v ./internal/gateway/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/gateway/intercept_handler.go internal/gateway/intercept_handler_test.go
git commit -m "feat: intercept handler request types and validation"
```

---

### Task 6: Implement intercept handler — request phase evaluation

Add the routing logic: shell tool detection, CLI command parsing, and delegation to `PolicyEvaluator`.

**Files:**
- Modify: `internal/gateway/intercept_handler.go`
- Modify: `internal/gateway/intercept_handler_test.go`

**Step 1: Write tests for request phase evaluation**

```go
// TestInterceptHandler_RequestPhase_MCPTool_Allowed verifies that a non-shell
// MCP tool call is evaluated as a tool call and returns valid=true with severity="info".
func TestInterceptHandler_RequestPhase_MCPTool_Allowed(t *testing.T)

// TestInterceptHandler_RequestPhase_MCPTool_Denied verifies that a denied MCP
// tool call returns valid=false with severity="error" and messages.
func TestInterceptHandler_RequestPhase_MCPTool_Denied(t *testing.T)

// TestInterceptHandler_RequestPhase_ShellTool_CLIDeny verifies that a shell
// tool (e.g., "Bash") triggers CLI command parsing and cli_expression evaluation.
func TestInterceptHandler_RequestPhase_ShellTool_CLIDeny(t *testing.T)

// TestInterceptHandler_RequestPhase_ShellTool_MCPAllow verifies that a shell
// tool also evaluates mcp_expression (not just cli_expression).
func TestInterceptHandler_RequestPhase_ShellTool_MCPAllow(t *testing.T)

// TestInterceptHandler_RequestPhase_AuditModeBypass verifies that audit-only
// deny returns valid=true with severity="warn".
func TestInterceptHandler_RequestPhase_AuditModeBypass(t *testing.T)

// TestInterceptHandler_RequestPhase_NoEngines verifies that when no engines
// are configured, the response is valid=true.
func TestInterceptHandler_RequestPhase_NoEngines(t *testing.T)

// TestInterceptHandler_ResponseFormat verifies the SEP-1763 response structure:
// interceptor, type, phase, valid, severity, messages, durationMs, info fields.
func TestInterceptHandler_ResponseFormat(t *testing.T)
```

Use `newTestCELEngineWithDenyRule()` to create CEL engines with rules that use `cli_expression` and/or `mcp_expression`. Create `PolicyEvaluator` instances with those engines.

**Step 2: Run tests to verify they fail**

Run: `go test -run "TestInterceptHandler_RequestPhase" -v ./internal/gateway/...`
Expected: FAIL — evaluation not wired up yet.

**Step 3: Implement request phase routing**

In `ServeHTTP`, after validation passes:

```go
func (h *InterceptHandler) isShellTool(name string) bool {
    for _, shellName := range h.config.ShellToolNames {
        if shellName == name {
            return true
        }
    }
    return false
}

func (h *InterceptHandler) evaluateRequest(ctx context.Context, req *InterceptRequest) ValidationResults {
    if h.isShellTool(req.Payload.Name) {
        return h.evaluateShellCommand(ctx, req)
    }
    return h.evaluateToolCall(ctx, req)
}
```

`evaluateShellCommand` parses `arguments.command` into a `CLIValidationRequest` and calls `evaluator.EvaluateCLICommand()`.

`evaluateToolCall` converts to `mcp.CallToolRequest` and calls `evaluator.EvaluateToolCall()`.

Map `ValidationResults` to `InterceptResponse`:
- `Allowed=true, !AuditModeBypass` → `valid=true, severity="info"`
- `Allowed=true, AuditModeBypass` → `valid=true, severity="warn"`
- `Allowed=false` → `valid=false, severity="error"`

**Step 4: Run tests to verify they pass**

Run: `go test -run "TestInterceptHandler_RequestPhase" -v ./internal/gateway/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/gateway/intercept_handler.go internal/gateway/intercept_handler_test.go
git commit -m "feat: intercept handler request phase evaluation with shell routing"
```

---

### Task 7: Implement intercept handler — response phase evaluation

Add response phase support: convert payload to MCP types, call response validation chain, handle redaction as mutation response.

**Files:**
- Modify: `internal/gateway/intercept_handler.go`
- Modify: `internal/gateway/intercept_handler_test.go`

**Step 1: Write tests for response phase**

```go
// TestInterceptHandler_ResponsePhase_Allowed verifies that a response with
// no policy violations returns type="validation", valid=true.
func TestInterceptHandler_ResponsePhase_Allowed(t *testing.T)

// TestInterceptHandler_ResponsePhase_Denied verifies that a denied response
// returns type="validation", valid=false, severity="error".
func TestInterceptHandler_ResponsePhase_Denied(t *testing.T)

// TestInterceptHandler_ResponsePhase_Redacted verifies that a redacted response
// returns type="mutation", modified=true, with the redacted payload.
func TestInterceptHandler_ResponsePhase_Redacted(t *testing.T)

// TestInterceptHandler_ResponsePhase_NoChain verifies graceful handling when
// no response validation chain is configured.
func TestInterceptHandler_ResponsePhase_NoChain(t *testing.T)
```

**Step 2: Run tests to verify they fail**

Run: `go test -run "TestInterceptHandler_ResponsePhase" -v ./internal/gateway/...`
Expected: FAIL

**Step 3: Implement response phase evaluation**

```go
func (h *InterceptHandler) evaluateResponse(ctx context.Context, req *InterceptRequest) (*InterceptResponse, error) {
    // Convert payload to mcp.CallToolRequest + mcp.CallToolResult
    mcpReq := h.payloadToCallToolRequest(req)
    mcpResult := h.payloadToCallToolResult(req)

    // Evaluate through response chain
    results, err := h.config.Evaluator.EvaluateResponse(ctx, mcpReq, mcpResult)

    // If redacted, return mutation response with modified payload
    if results.RedactedContent != nil {
        return h.buildMutationResponse(req, results)
    }

    // Otherwise return validation response
    return h.buildResponseValidationResponse(req, results)
}
```

Mutation responses set `type: "mutation"`, `modified: true`, and include the modified `payload` with redacted content in `payload.result.content`.

**Step 4: Run tests to verify they pass**

Run: `go test -run "TestInterceptHandler_ResponsePhase" -v ./internal/gateway/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/gateway/intercept_handler.go internal/gateway/intercept_handler_test.go
git commit -m "feat: intercept handler response phase with redaction support"
```

---

### Task 8: Implement intercept handler — audit logging

Add audit entry creation with `source: "intercept"` and context mapping.

**Files:**
- Modify: `internal/gateway/intercept_handler.go`
- Modify: `internal/gateway/intercept_handler_test.go`

**Step 1: Write tests for audit logging**

```go
// TestInterceptHandler_AuditEntry_MCPTool verifies audit entry has source="intercept",
// Tool field populated with payload.name, and UpstreamRequest with mapped context fields.
func TestInterceptHandler_AuditEntry_MCPTool(t *testing.T)

// TestInterceptHandler_AuditEntry_ShellTool verifies audit entry has both
// Tool and CLI fields populated for shell tools.
func TestInterceptHandler_AuditEntry_ShellTool(t *testing.T)

// TestInterceptHandler_AuditEntry_ContextMapping verifies context.traceId maps
// to UpstreamRequest.ExternalID and context.sessionId maps to UpstreamRequest.SessionID.
func TestInterceptHandler_AuditEntry_ContextMapping(t *testing.T)

// TestInterceptHandler_AuditEntry_PrincipalAsClientID verifies context.principal.id
// maps to UpstreamRequest.ClientID when X-Maybe-Dont-Client-ID header is not set.
func TestInterceptHandler_AuditEntry_PrincipalAsClientID(t *testing.T)
```

Use a mock `AuditWriter` that captures entries for assertion.

**Step 2: Run tests to verify they fail**

Run: `go test -run "TestInterceptHandler_Audit" -v ./internal/gateway/...`
Expected: FAIL

**Step 3: Implement audit logging**

Add `writeAuditEntry` method to intercept handler. Key mappings:
- `Source: "intercept"`
- `context.traceId` → `UpstreamRequest.ExternalID`
- `context.sessionId` → `UpstreamRequest.SessionID`
- `context.principal.id` → fallback for `UpstreamRequest.ClientID` (after header check)
- Shell tools: populate both `Tool` (with shell tool name) and `CLI` (with parsed command)
- MCP tools: populate `Tool` only
- Use `WriteAsyncAuditCompletion` for async AI results

**Step 4: Run tests to verify they pass**

Run: `go test -run "TestInterceptHandler_Audit" -v ./internal/gateway/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/gateway/intercept_handler.go internal/gateway/intercept_handler_test.go
git commit -m "feat: intercept handler audit logging with context mapping"
```

---

### Task 9: Register handler and run full test suite

Wire the intercept handler into the HTTP mux in `server.go`, run the full suite, format and lint.

**Files:**
- Modify: `internal/gateway/server.go`

**Step 1: Register in both SSE and HTTP server init**

After the action handler registration in both `initSSEServer()` and `initHTTPServer()`:

```go
// Register intercept endpoint
interceptHandler := NewInterceptHandler(InterceptHandlerConfig{
    Enabled:               g.config.Intercept.Enabled,
    ShellToolNames:        g.config.Intercept.ShellToolNames,
    Logger:                g.logger,
    Version:               g.version,
    AuditWriter:           g.auditWriter,
    Evaluator:             evaluator,
    IncludeArgumentValues: g.config.Audit.ShouldIncludeArgumentValues(),
})
mux.Handle("/api/v1/intercept", interceptHandler)
if g.config.Intercept.Enabled {
    g.logger.Info(ctx, "Intercept endpoint enabled",
        zap.Int("shell_tool_names_count", len(g.config.Intercept.ShellToolNames)),
    )
}
```

**Step 2: Run full test suite**

Run: `make test`
Expected: ALL PASS

**Step 3: Format and lint**

Run: `make fmt && make lint`
Expected: Clean

**Step 4: Run `go mod tidy`**

Run: `go mod tidy`
Expected: No changes (no new dependencies)

**Step 5: Commit**

```bash
git add internal/gateway/server.go
git commit -m "feat: register intercept endpoint on HTTP mux"
```

---

### Task 10: Verification and review

Final verification: build, test, lint, and self-review.

**Step 1: Build**

Run: `make build`
Expected: Success

**Step 2: Full test suite**

Run: `make test`
Expected: ALL PASS

**Step 3: Lint**

Run: `make lint`
Expected: Clean

**Step 4: Review diff**

Run: `git diff main...HEAD --stat` and `git log --oneline main..HEAD`

Verify:
- No unintended changes to existing handler behavior
- All new files are in the correct locations
- Config defaults are in sync between `config.go` and `defaults/maybe-dont.yaml`
- No sensitive data logged (tool parameters gated by `IncludeArgumentValues`)
- Error responses use consistent JSON structure

**Step 5: Use `superpowers:verification-before-completion` skill**

Run the verification skill to confirm all claims of passing tests and clean builds.
