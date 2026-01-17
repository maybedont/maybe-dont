# Validation Chain Audit Schema Specification

## Overview

This specification defines the audit log schema for the complete validation chain, including both request and response validation with deterministic rules and AI policies. It introduces a cumulative blocking budget that spans the entire tool call lifecycle.

## Goals

1. **Predictable latency**: A single `max_blocking_ms` config controls the maximum added latency for any tool call
2. **Unified blocking budget**: All validation phases (rules request, AI request, rules response, AI response) share a common budget
3. **Fail-open on timeout**: When budget is exhausted, remaining validations continue async but don't block
4. **Comprehensive audit trail**: Capture complete timing and decision information regardless of blocking state
5. **Early termination**: Stop blocking as soon as a decision is made (deny or all enabled rules pass)

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
    "rules": {
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
  "response": {
    "content_items": 1,
    "is_error": false
  },
  "response_validation": {
    "rules": {
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
- `total_blocked_ms` (858ms) = rules.blocked (5) + ai.blocked (700) + tool.duration (150) + response rules.blocked (3)
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

Both `request_validation.rules`, `request_validation.ai`, `response_validation.rules`, and `response_validation.ai` share this structure:

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
| `result` | `"allow"` \| `"deny"` \| `"redact"` \| `"error"` | Yes | What this rule contributed |
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
    "rules": {
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
| First `enabled` rule returns `deny` | Yes | Stop blocking, cancel remaining, continue async for audit |
| All `enabled` rules return `allow` | Yes | Stop blocking, remaining `audit_only` rules continue async |
| All rules are `audit_only` | Yes | Phase is non-blocking from start |
| Error on `enabled` rule | Yes | Treat as deny (fail-closed), stop blocking |
| Budget exhausted | Yes | Stop blocking, continue async |

### Across Validation Phases

- Early deny in request validation: Skip tool execution, skip response validation
- Early deny in response validation: Response is blocked/modified
- Budget exhaustion: Continue to next phase but non-blocking

## Rules vs AI Validation Differences

### Rules Validation (Deterministic)

- **Synchronous**: Rules evaluated sequentially (no goroutines)
- **Fast**: Typically <10ms per rule
- **Deterministic**: No external API calls
- **Early termination**: First enabled deny stops evaluation

### AI Validation

- **Parallel**: Rules evaluated concurrently via goroutines
- **Slow**: Typically 500ms-5000ms per rule
- **Non-deterministic**: External LLM API calls
- **Early termination**: First enabled deny cancels remaining goroutines
- **Per-rule timeout**: `max_rule_evaluation_ms` applies to each rule

## Policy Action and Result Mapping

### Deny Policies

| AI Response (`allowed:`) | Result | Effect (if `enabled`) |
|--------------------------|--------|----------------------|
| `false` | `"deny"` | DENY request/response |
| `true` | `"allow"` | No action (AI didn't find issue) |
| error/timeout | `"error"` | DENY (fail-closed) |

### Allow Policies (Required Gates)

| AI Response (`allowed:`) | Result | Effect (if `enabled`) |
|--------------------------|--------|----------------------|
| `true` | `"allow"` | Gate passed |
| `false` | `"deny"` | DENY (gate failed) |
| error/timeout | `"error"` | DENY (fail-closed) |

### Redact Policies (Response Only)

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
    "rules": {
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
    "rules": {
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
  "response": {
    "content_items": 1,
    "is_error": false
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
    "rules": {
      "action": "allow",
      "blocked_ms": 5,
      "evaluation_ms": 5,
      "results": [
        {"rule": "allow_search", "action": "allow", "result": "allow", "evaluation_ms": 5}
      ]
    }
  },
  "response_validation": {
    "rules": {
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
- Validation blocks use `rules` instead of `cel`
- `AuditRulesResult` (formerly `AuditCELResult`) expanded from simple to detailed format
- `total_blocked_ms` now includes tool call duration
- New `blocked_ms` field in each validation result
- New `user_agent` field in `upstream_request`

Existing audit logs will not match the new schema.
