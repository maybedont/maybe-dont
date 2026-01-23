# Validation Chain Audit Schema Specification

## Overview

This specification defines the audit log schema for the complete validation chain, including both request and response validation with deterministic rules and AI policies. It introduces a cumulative blocking budget that spans the entire tool call lifecycle.

## Goals

1. **Predictable latency**: A single `max_blocking_ms` config controls the maximum added latency for any tool call
2. **Unified blocking budget**: All validation phases (rules request, AI request, rules response, AI response) share a common budget
3. **Fail-open on timeout**: When budget is exhausted, remaining validations continue async but don't block
4. **Comprehensive audit trail**: Capture complete timing and decision information regardless of blocking state
5. **Early termination**: Stop blocking as soon as a decision is made (deny or all enabled rules pass)
6. **True async for audit-only**: Policies in `audit_only` mode must never block the caller; evaluation runs entirely in the background

## Configuration

### Timeout Configuration

```yaml
validation:
  max_blocking_ms: 90000         # Max cumulative time to block tool call (default: 90000ms)
  max_rule_evaluation_ms: 45000  # Max time for any single rule (default: 45000ms)
  ai:
    endpoint: "https://api.openai.com/v1/chat/completions"
    model: "gpt-4o-mini"
    api_key: "${OPENAI_API_KEY}"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_blocking_ms` | int | 90000 | Maximum cumulative time in milliseconds to block a tool call across ALL validation phases. Once exhausted, remaining validations run non-blocking. |
| `max_rule_evaluation_ms` | int | 45000 | Maximum time in milliseconds for any single rule to complete. Rules exceeding this return `result: "error"` with `error: "timeout"`. |

### Blocking Budget Flow

```
Tool call starts
├── blocking_budget = max_blocking_ms (e.g., 90000ms)
│
├── Rules request validation
│   ├── Consumes X ms from budget
│   └── Early terminate on first enabled deny
│
├── AI request validation
│   ├── Consumes Y ms from remaining budget
│   └── Early terminate on first enabled deny
│
├── [If budget exhausted: remaining validations run non-blocking]
│
├── Downstream tool call execution
│
├── Rules response validation
│   ├── Consumes Z ms from remaining budget
│   └── Early terminate on first enabled deny
│
└── AI response validation
    ├── Consumes W ms from remaining budget
    └── Early terminate on first enabled deny

Audit log written after all validations complete (blocking or not)
```

## Audit Log Schema

### Complete Audit Entry Structure

```json
{
  "validation_started": "2026-01-14T20:30:00.000000000Z",
  "created_at": "2026-01-14T20:30:02.650000000Z",
  "tool": {
    "name": "create_issue",
    "client": "github",
    "prefixed_name": "github__create_issue",
    "params": {"title": "New feature", "body": "Description"},
    "called_at": "2026-01-14T20:30:00.855000000Z",
    "duration_ms": 150
  },
  "upstream_request": {
    "id": "req-abc123",
    "session_id": "sess-xyz789",
    "client_ip": "127.0.0.1",
    "user_agent": "claude-code/1.0.0"
  },
  "request_validation": {
    "cel": {
      "action": "allow",
      "blocked_ms": 5,
      "evaluation_ms": 5,
      "results": [
        {
          "rule": "allow_github_tools",
          "action": "allow",
          "result": "allow",
          "evaluation_ms": 5
        }
      ]
    },
    "ai": {
      "action": "allow",
      "blocked_ms": 700,
      "evaluation_ms": 2341,
      "results": [
        {
          "rule": "block_destructive_ops",
          "action": "deny",
          "result": "allow",
          "evaluation_ms": 700
        },
        {
          "rule": "check_permissions",
          "action": "deny",
          "mode": "audit_only",
          "result": "deny",
          "evaluation_ms": 2341
        }
      ]
    }
  },
  "response_validation": {
    "cel": {
      "action": "allow",
      "blocked_ms": 3,
      "evaluation_ms": 3,
      "results": []
    },
    "ai": {
      "action": "allow",
      "blocked_ms": 0,
      "evaluation_ms": 1500,
      "results": [
        {
          "rule": "redact_secrets",
          "action": "redact",
          "mode": "audit_only",
          "result": "allow",
          "evaluation_ms": 1500
        }
      ]
    }
  },
  "recommended_action": "allow",
  "action": "allow",
  "duration_ms": 2650,
  "total_blocked_ms": 858
}
```

In this example:
- `total_blocked_ms` (858ms) = cel.blocked (5) + ai.blocked (700) + tool.duration (150) + response cel.blocked (3)
- Gateway overhead = 858 - 150 = 708ms

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `validation_started` | string | Yes | RFC3339Nano timestamp when the tool call was received and validation began |
| `created_at` | string | Yes | RFC3339Nano timestamp when the audit entry was finalized and written |
| `duration_ms` | int | Yes | Total wall-clock time from `validation_started` to `created_at` |
| `total_blocked_ms` | int | Yes | Time caller was blocked (validation blocking + tool call duration) |

### Tool Object Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Original tool name |
| `client` | string | Yes | Downstream client name |
| `prefixed_name` | string | Yes | Full prefixed tool name (`{client}__{name}`) |
| `params` | object | No | Tool call parameters |
| `called_at` | string | No | RFC3339Nano timestamp when downstream tool was invoked (omitted if denied) |
| `duration_ms` | int | No | Downstream call duration in milliseconds (omitted if denied) |

### Upstream Request Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | No | Request ID |
| `session_id` | string | No | Session ID |
| `client_ip` | string | No | Client IP address |
| `user_agent` | string | No | User-Agent header from incoming request (useful for identifying AI agents) |

### Validation Block Fields (Rules and AI)

Both `request_validation.cel`, `request_validation.ai`, `response_validation.cel`, and `response_validation.ai` share this structure:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | `"allow"` \| `"deny"` \| `"redact"` | Yes | Final decision from this validation phase |
| `blocked_ms` | int | Yes | Time this phase contributed to blocking. 0 if budget was already exhausted or all rules are `audit_only`. |
| `evaluation_ms` | int | Yes | Total wall-clock time for all rules in this phase to complete |
| `deciding_rule` | string | No | Rule that caused a deny decision. Omitted when action is `allow`. |
| `reason` | string | No | Message from the deciding rule. Omitted when `deciding_rule` is omitted. |
| `results` | array | Yes | Per-rule evaluation results, ordered by completion time |

### Per-Rule Result Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `rule` | string | Yes | Rule name from the rule definition |
| `action` | `"allow"` \| `"deny"` \| `"redact"` | Yes | Rule's configured action |
| `mode` | `"audit_only"` | No | Only present when rule mode is `audit_only` |
| `result` | `"allow"` \| `"deny"` \| `"redact"` \| `"error"` | Yes | Effective decision from this rule (see [Policy Action and Result Mapping](#policy-action-and-result-mapping)) |
| `evaluation_ms` | int | Yes | Time for this rule to complete |
| `error` | string | No | Present when `result: "error"` (e.g., "timeout", "api_error") |

## Blocking Budget Behavior

### Budget Consumption

1. **Rules validation**: Typically very fast (<10ms), consumes minimal budget
2. **AI validation**: Each rule runs in parallel, budget consumed until decision is made
3. **Response validation**: Uses remaining budget after request validation and tool execution

### Budget Exhaustion

When `blocked_ms` across all phases reaches `max_blocking_ms`:

1. Current validation phase stops blocking immediately
2. Remaining rules in current phase continue async (for audit)
3. Subsequent validation phases run entirely non-blocking
4. Tool call proceeds with `allow` decision (fail-open)
5. Audit log captures what would have happened given more time

### Example: Budget Exhausted Mid-Evaluation

```json
{
  "request_validation": {
    "cel": {
      "action": "allow",
      "blocked_ms": 5,
      "evaluation_ms": 5,
      "results": [...]
    },
    "ai": {
      "action": "allow",
      "blocked_ms": 4995,
      "evaluation_ms": 8000,
      "results": [
        {
          "rule": "fast_check",
          "action": "deny",
          "result": "allow",
          "evaluation_ms": 1000
        },
        {
          "rule": "slow_check",
          "action": "deny",
          "result": "deny",
          "evaluation_ms": 8000
        }
      ]
    }
  },
  "response_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 0,
      "evaluation_ms": 2000,
      "results": [
        {
          "rule": "check_response",
          "action": "deny",
          "result": "deny",
          "evaluation_ms": 2000
        }
      ]
    }
  },
  "action": "allow",
  "total_blocked_ms": 5000
}
```

In this example:
- Budget of 5000ms was exhausted during AI request validation
- `slow_check` completed at 8000ms but only contributed 4995ms to blocking (rest was non-blocking)
- `slow_check` returned `deny` but decision was already made (fail-open)
- Response validation ran entirely non-blocking (`blocked_ms: 0`)
- Audit shows `check_response` would have denied given time

## Early Termination Behavior

### Within a Validation Phase

| Scenario | Can Terminate Early? | Behavior |
|----------|---------------------|----------|
| First `enabled` rule returns `deny` | Yes | Stop blocking, all remaining rules continue async for complete audit |
| All `enabled` rules return `allow` | Yes | Stop blocking, remaining `audit_only` rules continue async |
| All rules are `audit_only` | Yes | Phase is non-blocking from start |
| Error on `enabled` rule | Yes | Treat as deny (fail-closed), stop blocking |
| Budget exhausted | Yes | Stop blocking, continue async |

### Across Validation Phases

- Early deny in request validation: Skip tool execution, skip response validation
- Early deny in response validation: Response is blocked/modified
- Budget exhaustion: Continue to next phase but non-blocking

## Policy Mode Behavior

This section defines the precise behavior for each policy mode combination. The key principle is that `audit_only` policies must **never** cause the caller to wait—they execute entirely asynchronously.

### Policy Modes

> **Note**: Policy mode configuration has been simplified. See [Rule Mode Simplification](rule-mode-simplification.md) for details. The configuration uses `enabled: true/false` at the validation level and `mode: audit_only` (optional) at the rule level. The runtime behaviors described below are unchanged.

| Mode | Executed? | Blocks Caller? | Affects Decision? | Appears in Audit Log? |
|------|-----------|----------------|-------------------|----------------------|
| enabled (can block) | Yes | Yes | Yes | Yes |
| `audit_only` | Yes | **No** | No | Yes |
| disabled | **No** | No | No | **No** |

### Mode Combinations and Expected Behavior

#### 1. All Policies Disabled

When validation is disabled (e.g., `request_validation.ai.enabled: false`):

- **Behavior**: The engine returns immediately without executing any policies
- **Blocking**: 0ms
- **Audit Log**: The validation block is omitted entirely (e.g., no `request_validation.ai` field)
- **Policies Executed**: 0

```json
{
  "request_validation": {
    "cel": {
      "action": "allow",
      "blocked_ms": 5,
      "evaluation_ms": 5,
      "results": [...]
    }
  },
  "action": "allow",
  "total_blocked_ms": 5
}
```

Note: `request_validation.ai` is absent because AI validation is disabled.

#### 2. All Policies Enabled (Can Block)

When all loaded policies are in enabled mode (no `mode: audit_only`):

- **Behavior**: Engine blocks until either:
  - An enabled policy returns `deny` (early termination), OR
  - All enabled policies complete evaluation
- **Blocking**: `blocked_ms` equals the time until decision is made (longest running policy if all allow)
- **Audit Log**: All policy results included with their evaluation times
- **Decision**: Based on policy results

```json
{
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 1500,
      "evaluation_ms": 1500,
      "results": [
        {"rule": "check_safe", "action": "deny", "result": "allow", "evaluation_ms": 800},
        {"rule": "verify_params", "action": "deny", "result": "allow", "evaluation_ms": 1500}
      ]
    }
  },
  "action": "allow",
  "total_blocked_ms": 1500
}
```

#### 3. All Policies Audit-Only

When all loaded policies have `mode: audit_only`:

- **Behavior**: Engine returns **immediately** with `allow` decision. Policy evaluation continues asynchronously in background goroutines.
- **Blocking**: 0ms (caller is not blocked at all)
- **Audit Log**: Written asynchronously after all background evaluations complete. Results include all policy outcomes.
- **Decision**: Always `allow` (audit-only policies cannot affect the decision)

**Critical Implementation Requirement**: The `EvaluateToolCall` function must return immediately when all policies are `audit_only`. The goroutines continue running in the background, and results are collected via a callback mechanism that writes to the audit log when complete.

```json
{
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 0,
      "evaluation_ms": 3500,
      "results": [
        {"rule": "log_access", "action": "deny", "mode": "audit_only", "result": "deny", "evaluation_ms": 2000},
        {"rule": "log_params", "action": "deny", "mode": "audit_only", "result": "allow", "evaluation_ms": 3500}
      ]
    }
  },
  "action": "allow",
  "total_blocked_ms": 0
}
```

Note: `evaluation_ms` (3500) reflects when results were collected asynchronously, but `blocked_ms` is 0 because the caller was never blocked.

#### 4. Mixed Modes (Enabled + Audit-Only)

When policies have a mix of `enabled` and `audit_only` modes:

- **Behavior**: Engine blocks only until all `enabled` policies complete (or one denies). Then returns immediately. Any `audit_only` policies still running continue in the background.
- **Blocking**: Time until all `enabled` policies complete
- **Audit Log**: Written after ALL policies (including async audit_only) complete
- **Decision**: Based only on `enabled` policy results

```json
{
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 800,
      "evaluation_ms": 3500,
      "results": [
        {"rule": "block_destructive", "action": "deny", "result": "allow", "evaluation_ms": 800},
        {"rule": "audit_access", "action": "deny", "mode": "audit_only", "result": "deny", "evaluation_ms": 3500}
      ]
    }
  },
  "action": "allow",
  "total_blocked_ms": 800
}
```

In this example:
- `block_destructive` (enabled) completed in 800ms → caller was blocked for 800ms
- `audit_access` (audit_only) completed in 3500ms → ran async, didn't block caller
- `evaluation_ms` (3500) reflects total time for all policies
- `blocked_ms` (800) reflects only the time waiting for enabled policies

#### 5. Mixed Modes with Early Deny

When an enabled policy denies early while other policies are still running:

- **Behavior**: Engine returns `deny` immediately when the enabled policy denies. All other policies (both enabled and audit_only) continue running in the background to provide complete audit information.
- **Blocking**: Time until the denying policy completed
- **Audit Log**: Includes the deny result plus all other policy results. Since all policies complete, the audit log provides a complete picture of what every policy would have decided.

```json
{
  "request_validation": {
    "ai": {
      "action": "deny",
      "blocked_ms": 500,
      "evaluation_ms": 2000,
      "deciding_rule": "block_destructive",
      "reason": "Destructive operation detected",
      "results": [
        {"rule": "block_destructive", "action": "deny", "result": "deny", "evaluation_ms": 500},
        {"rule": "slow_enabled_check", "action": "deny", "result": "allow", "evaluation_ms": 1800},
        {"rule": "audit_access", "action": "deny", "mode": "audit_only", "result": "allow", "evaluation_ms": 2000}
      ]
    }
  },
  "action": "deny",
  "total_blocked_ms": 500
}
```

### Disabled Policies Are Not Loaded

Policies with `enabled: false` are filtered out during `LoadPolicies()`:

- They are **not** added to the engine's policy list
- They are **never** executed
- They do **not** appear in audit log results
- They consume zero resources

This is distinct from `audit_only` policies which ARE loaded and executed, just asynchronously.

### Async Audit Log Writing

To support true async behavior for audit-only policies, the audit system must handle delayed result collection:

1. **Immediate Return Path**: When the decision is made (all enabled policies complete or early deny), the validation function returns immediately with:
   - The decision (`allow` or `deny`)
   - A handle/callback for receiving async results

2. **Background Completion**: Audit-only policies (and any other async work) continue running. When complete, results are sent via the callback.

3. **Audit Entry Finalization**: The audit entry is written only after ALL results are collected:
   - `blocked_ms` reflects actual blocking time (excludes async work)
   - `evaluation_ms` reflects total wall-clock time for all evaluations
   - `created_at` is set when the entry is written (after async completion)

### Implementation Requirements

#### AsyncValidationResult

The AI engine must return both immediate results and a mechanism for async completion:

```go
type AsyncValidationResult struct {
    // Immediate results (available when function returns)
    Results     ValidationResults

    // For async completion (nil if no async work pending)
    Completion  <-chan AsyncCompletion
}

type AsyncCompletion struct {
    AIDetails    *AuditAIResult  // Complete AI results including async policies
    EvaluationMs int64           // Total evaluation time
}
```

#### AuditContext Updates

The `AuditContext` must support deferred finalization:

```go
// SetAIResultsAsync registers a callback for async AI results
func (ac *AuditContext) SetAIResultsAsync(completion <-chan AsyncCompletion)

// FinalizeAsync waits for async results before writing audit entry
// Should be called in a goroutine to not block the response
func (ac *AuditContext) FinalizeAsync() *AuditEntry
```

#### Gateway Integration

The gateway must handle async audit writing:

```go
// After validation completes
if asyncResult.Completion != nil {
    // Audit-only policies still running - finalize async
    go func() {
        entry := auditCtx.FinalizeAsync()  // Waits for completion
        auditWriter.Write(entry)
    }()
} else {
    // All work complete - finalize immediately
    entry := auditCtx.Finalize()
    auditWriter.Write(entry)
}
```

## Rules vs AI Validation Differences

### Rules Validation (Deterministic)

- **Synchronous**: Rules evaluated sequentially (no goroutines)
- **Fast**: Typically <10ms per rule
- **Deterministic**: No external API calls
- **Early termination**: First enabled deny stops evaluation
- **No async for audit_only**: See "Async Behavior Scope" below

### AI Validation

- **Parallel**: Rules evaluated concurrently via goroutines
- **Slow**: Typically 500ms-5000ms per rule
- **Non-deterministic**: External LLM API calls
- **Early termination**: First enabled deny stops blocking; all goroutines continue to completion for audit
- **Per-rule timeout**: `max_rule_evaluation_ms` applies to each rule
- **True async for audit_only**: Returns immediately, evaluation continues in background

### Async Behavior Scope

**The true async behavior for `audit_only` policies applies ONLY to AI engines**, not to CEL/deterministic rules engines.

| Engine | Async for audit_only? | Rationale |
|--------|----------------------|-----------|
| AI Request (`ai_engine.go`) | **Yes** | Slow (500ms-5s), external API calls |
| AI Response (`ai_response_engine.go`) | **Yes** | Same as request |
| CEL Request (`cel_engine.go`) | No | Fast (<10ms), complexity not justified |
| CEL Response (`cel_response_engine.go`) | No | Same as request |

**Why CEL remains synchronous:**

1. CEL evaluation is deterministic and fast (sub-millisecond to ~10ms total)
2. No external API calls - purely in-memory expression evaluation
3. The "blocking" from `audit_only` CEL rules is negligible (<10ms)
4. Adding async infrastructure for <10ms operations adds code complexity without meaningful user benefit
5. Simpler synchronous code is easier to maintain, debug, and reason about

**Future Consideration:** If CEL rule execution time increases significantly (e.g., due to complex expressions, large datasets, or external data lookups), this decision should be revisited. The same async pattern used for AI engines could be applied to CEL engines if needed.

## Policy Action and Result Mapping

### CEL Rules (Deterministic)

For CEL rules, the `result` field represents the **effective decision** based on whether the expression matched and what action was configured. This makes CEL results consistent with AI results—both report what action the rule contributed.

| Action | Expression Matched? | Result | Effect (if `enabled`) |
|--------|---------------------|--------|----------------------|
| `deny` | yes | `"deny"` | DENY request/response |
| `deny` | no | `"allow"` | No action (pattern not found) |
| `allow` | yes | `"allow"` | Gate passed |
| `allow` | no | `"deny"` | DENY (gate failed) |
| `redact` | yes | `"redact"` | Apply redaction |
| `redact` | no | `"allow"` | No action (pattern not found) |
| any | error | `"error"` | DENY (fail-closed) |

**Key insight**: You can determine if the expression matched by comparing `action` and `result`:
- `action == result` → expression matched
- `action != result` → expression did not match

**Debugging**: The DEBUG log includes the raw `matched` boolean for developers who need to troubleshoot rule expressions.

### AI Policies

The following tables apply to AI-powered policies, where the result combines the AI's response with the rule's configured action.

#### Deny Policies

| AI Response (`allowed:`) | Result | Effect (if `enabled`) |
|--------------------------|--------|----------------------|
| `false` | `"deny"` | DENY request/response |
| `true` | `"allow"` | No action (AI didn't find issue) |
| error/timeout | `"error"` | DENY (fail-closed) |

#### Allow Policies (Required Gates)

| AI Response (`allowed:`) | Result | Effect (if `enabled`) |
|--------------------------|--------|----------------------|
| `true` | `"allow"` | Gate passed |
| `false` | `"deny"` | DENY (gate failed) |
| error/timeout | `"error"` | DENY (fail-closed) |

#### Redact Policies (Response Only)

| AI Response | Result | Effect (if `enabled`) |
|-------------|--------|----------------------|
| `allowed: true, redacted_content: "..."` | `"redact"` | Apply redaction |
| `allowed: true, redacted_content: ""` | `"allow"` | No redaction needed |
| `allowed: false` | `"deny"` | DENY response |
| error/timeout | `"error"` | DENY (fail-closed) |

## Examples

### Example 1: Fast Allow (All Pass)

```json
{
  "request_validation": {
    "cel": {
      "action": "allow",
      "blocked_ms": 3,
      "evaluation_ms": 3,
      "results": [
        {"rule": "allow_read_ops", "action": "allow", "result": "allow", "evaluation_ms": 3}
      ]
    },
    "ai": {
      "action": "allow",
      "blocked_ms": 1200,
      "evaluation_ms": 1200,
      "results": [
        {"rule": "check_safe", "action": "deny", "result": "allow", "evaluation_ms": 800},
        {"rule": "verify_user", "action": "allow", "result": "allow", "evaluation_ms": 1200}
      ]
    }
  },
  "action": "allow",
  "total_blocked_ms": 1203
}
```

### Example 2: Early Deny on Rules

```json
{
  "request_validation": {
    "cel": {
      "action": "deny",
      "blocked_ms": 2,
      "evaluation_ms": 2,
      "deciding_rule": "block_delete_ops",
      "reason": "Delete operations are not allowed",
      "results": [
        {"rule": "block_delete_ops", "action": "deny", "result": "deny", "evaluation_ms": 2}
      ]
    }
  },
  "action": "deny",
  "total_blocked_ms": 2
}
```

Note: AI request validation was skipped because rules validation denied.

### Example 3: All Audit-Only (Non-Blocking)

```json
{
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 0,
      "evaluation_ms": 3500,
      "results": [
        {"rule": "log_access", "action": "deny", "mode": "audit_only", "result": "deny", "evaluation_ms": 2000},
        {"rule": "log_params", "action": "deny", "mode": "audit_only", "result": "allow", "evaluation_ms": 3500}
      ]
    }
  },
  "action": "allow",
  "total_blocked_ms": 0
}
```

### Example 4: Response Validation Deny

```json
{
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 800,
      "evaluation_ms": 800,
      "results": [
        {"rule": "check_request", "action": "deny", "result": "allow", "evaluation_ms": 800}
      ]
    }
  },
  "response_validation": {
    "ai": {
      "action": "deny",
      "blocked_ms": 1500,
      "evaluation_ms": 1500,
      "deciding_rule": "block_secrets",
      "reason": "Response contains API keys",
      "results": [
        {"rule": "block_secrets", "action": "deny", "result": "deny", "evaluation_ms": 1500}
      ]
    }
  },
  "action": "deny",
  "total_blocked_ms": 2300
}
```

### Example 5: Mixed Rules and AI with Redaction

```json
{
  "request_validation": {
    "cel": {
      "action": "allow",
      "blocked_ms": 5,
      "evaluation_ms": 5,
      "results": [
        {"rule": "allow_search", "action": "allow", "result": "allow", "evaluation_ms": 5}
      ]
    }
  },
  "response_validation": {
    "cel": {
      "action": "redact",
      "blocked_ms": 10,
      "evaluation_ms": 10,
      "results": [
        {"rule": "redact_emails", "action": "redact", "result": "redact", "evaluation_ms": 10}
      ]
    }
  },
  "action": "allow",
  "total_blocked_ms": 15
}
```

## Implementation Notes

### BlockingBudget Struct

A `BlockingBudget` struct tracks cumulative blocking time across all phases:

```go
type BlockingBudget struct {
    maxBlockingMs     int64
    startTime         time.Time
    totalBlockedMs    int64
    mu                sync.Mutex
}

func (b *BlockingBudget) RemainingMs() int64
func (b *BlockingBudget) IsExhausted() bool
func (b *BlockingBudget) ConsumeBlocking(ms int64) int64  // Returns actual consumed
```

### Validation Chain Updates

1. Gateway creates `BlockingBudget` at tool call start
2. Budget passed to each validation handler
3. Each handler checks `RemainingMs()` before blocking
4. Results include `blocked_ms` (actual) and `evaluation_ms` (total)

### Audit Entry Updates

1. Add `total_blocked_ms` to top-level `AuditEntry`
2. Update `AuditRulesResult` to match `AuditAIResult` structure
3. Both include `blocked_ms`, `evaluation_ms`, `deciding_rule`, `reason`, `results[]`

### Native Tool Updates

1. **`get_audit_log`**: Parse unified validation structure
2. **`generate_audit_report`**: Analyze blocking budget usage patterns

## Backwards Compatibility

This schema change is **not backwards compatible**. Key changes:
- New `validation_started` field at top level
- `incoming_request` renamed to `upstream_request`
- `request` object merged into `tool` (params, called_at, duration_ms now under `tool`)
- `AuditRulesResult` (formerly `AuditCELResult`) expanded from simple to detailed format
- `total_blocked_ms` now includes tool call duration
- New `blocked_ms` field in each validation result
- New `user_agent` field in `upstream_request`

Existing audit logs will not match the new schema.

## Testing Requirements

The following test cases must be implemented to verify correct policy mode behavior. Tests should verify both the blocking behavior (timing) and audit log correctness.

### Unit Tests for AI Engine

#### Test: All Policies Disabled
- **Setup**: `request_validation.ai.mode: disabled`
- **Assertions**:
  - `EvaluateToolCall` returns immediately (< 10ms)
  - `ValidationResults.Allowed` is `true`
  - `AIDetails` is `nil` (no AI validation occurred)
  - No goroutines spawned for policy evaluation

#### Test: All Policies Enabled - All Allow
- **Setup**: Multiple policies with `mode: enabled`, mock AI to return `allowed: true`
- **Assertions**:
  - Function blocks until all policies complete
  - `blocked_ms` approximately equals `evaluation_ms`
  - `blocked_ms` approximately equals the slowest policy's evaluation time
  - All policy results appear in `AIDetails.Results`
  - `AIDetails.Action` is `"allow"`

#### Test: All Policies Enabled - Early Deny
- **Setup**: Multiple policies with `mode: enabled`, one fast policy returns deny
- **Assertions**:
  - Function returns after first deny (doesn't wait for slower policies to block)
  - `blocked_ms` approximately equals the denying policy's time
  - `AIDetails.DecidingRule` is set to the denying policy name
  - All policies complete and appear in results (no `error: "canceled"` - policies run to completion for audit)

#### Test: All Policies Audit-Only - True Async
- **Setup**: All policies with `mode: audit_only`, policies take 1-2 seconds each
- **Assertions**:
  - `EvaluateToolCall` returns immediately (< 50ms, well before policies complete)
  - `ValidationResults.Allowed` is `true`
  - `blocked_ms` is `0`
  - `Completion` channel is non-nil
  - After waiting on `Completion`, `AIDetails` contains all policy results
  - `evaluation_ms` reflects actual completion time (1-2 seconds)

#### Test: Mixed Modes - Enabled Completes First
- **Setup**: One `enabled` policy (fast), one `audit_only` policy (slow)
- **Assertions**:
  - Function returns after enabled policy completes
  - `blocked_ms` reflects only enabled policy time
  - `Completion` channel provides audit_only results later
  - Final `evaluation_ms` includes audit_only time

#### Test: Mixed Modes - Early Deny with Pending Audit-Only
- **Setup**: One `enabled` policy that denies quickly, one slow `audit_only` policy
- **Assertions**:
  - Function returns `deny` immediately
  - `blocked_ms` reflects only the denying policy time
  - Audit-only policy continues and completes in background
  - Final audit entry includes both results

#### Test: Disabled Policies Not In Audit Log
- **Setup**: Mix of `enabled`, `audit_only`, and `disabled` policies
- **Assertions**:
  - Disabled policies do not appear in `engine.policies` after `LoadPolicies()`
  - Disabled policies do not appear in `AIDetails.Results`
  - Only enabled and audit_only policies are executed and logged

#### Test: Audit-Only Never Causes Waiting
- **Setup**: One `enabled` policy (100ms), one `audit_only` policy (5000ms)
- **Assertions**:
  - Total blocking time is ~100ms (not 5000ms)
  - Function returns in ~100ms
  - `blocked_ms` is ~100ms
  - `evaluation_ms` (after async completion) is ~5000ms

### Integration Tests

#### Test: End-to-End Async Audit Writing
- **Setup**: Gateway with all `audit_only` AI policies, make a tool call
- **Assertions**:
  - Tool call response returns immediately
  - Audit log entry is written after AI policies complete (check file timestamps)
  - Audit entry contains complete AI results

#### Test: Gateway Handles Async Completion Correctly
- **Setup**: Gateway with mixed mode policies
- **Assertions**:
  - Response returned to caller after enabled policies complete
  - Audit entry written after all policies (including async) complete
  - No race conditions or missing results

#### Test: Multiple Concurrent Requests with Async Policies
- **Setup**: Multiple simultaneous tool calls with audit_only policies
- **Assertions**:
  - Each request gets independent async handling
  - Audit entries are correctly associated with their requests
  - No cross-contamination of results between requests

### Test Helpers

To support these tests, create mock AI clients that can:
- Return configurable responses (`allowed: true/false`)
- Simulate configurable delays
- Track whether they were called and when
- Support cancellation detection

```go
type MockAIPolicy struct {
    Name        string
    Response    AIResponse
    Delay       time.Duration
    Called      bool
    CalledAt    time.Time
    CompletedAt time.Time
    WasCanceled bool
}
```

## Implementation Checklist

The following tasks should be completed sequentially to implement the async audit-only behavior:

### Phase 1: Core Types and Infrastructure

- [x] **1.1** Create `AsyncValidationResult` and `AsyncCompletion` types in `tool_validation.go`
- [x] **1.2** Add mock AI client for testing with configurable delays and responses
- [x] **1.3** Add `SetAIResultsAsync` and `FinalizeAsync` methods to `AuditContext`

### Phase 2: AI Engine Refactoring

- [x] **2.1** Refactor `AIPolicyEngine.EvaluateToolCall` (`ai_engine.go`) to return immediately for audit_only policies
- [x] **2.2** Refactor `AIResponsePolicyEngine.EvaluateResponse` (`ai_response_engine.go`) with same async pattern
- [x] **2.3** Update `ToolAIValidationHandler` to handle async completion channel
- [x] **2.4** Update `ResponseAIValidationHandler` to handle async completion channel

### Phase 3: Gateway Integration

- [x] **3.1** Update Gateway tool call handler to support async audit writing
- [x] **3.2** Ensure proper cleanup of background goroutines on gateway shutdown

### Phase 4: Unit Tests

- [x] **4.1** Test: All Policies Disabled - no execution, immediate return
- [x] **4.2** Test: All Policies Enabled - blocks until completion
- [x] **4.3** Test: All Policies Audit-Only - true async, immediate return
- [x] **4.4** Test: Mixed Modes - enabled blocks, audit_only async
- [x] **4.5** Test: Disabled policies not in audit log
- [x] **4.6** Test: Audit-only never causes waiting (timing assertion)
- [x] **4.7** Test: Early deny with pending audit_only policies

### Phase 5: Integration Tests and Validation

- [x] **5.1** Integration test: End-to-end async audit writing
- [x] **5.2** Integration test: Multiple concurrent requests with async policies
- [x] **5.3** Run full test suite and fix any regressions
- [ ] **5.4** Manual testing with real AI policies in audit_only mode
