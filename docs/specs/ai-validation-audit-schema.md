# AI Validation Audit Schema Specification

## Overview

This specification defines the audit log schema for AI policy validation, including early termination behavior and timing metrics.

## Goals

1. Reduce latency by not blocking requests when the outcome is already determined
2. Capture comprehensive audit information for all policy evaluations
3. Provide clear timing metrics to understand blocking vs total evaluation time
4. Support both blocking (`enabled`) and non-blocking (`audit_only`) policy modes

## Configuration

### New Configuration Options

```yaml
ai_validation:
  enabled: true
  max_blocking_ms: 5000         # Max time to block request waiting for decision (default: 5000ms)
  max_rule_evaluation_ms: 10000 # Max time for any single rule to complete (default: 10000ms)
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_blocking_ms` | int | 5000 | Maximum time in milliseconds to block a request waiting for a policy decision. If exceeded, request is unblocked and evaluation continues in background. |
| `max_rule_evaluation_ms` | int | 10000 | Maximum time in milliseconds for any single rule to complete. Rules exceeding this timeout return `result: "error"` with `error: "timeout"`. |

## Audit Log Schema

### AI Validation Block

```json
{
  "request_validation": {
    "ai": {
      "action": "deny",
      "blocked_ms": 847,
      "evaluation_ms": 2341,
      "deciding_rule": "block_destructive_ops",
      "reason": "This operation would delete all user data",
      "results": [
        {
          "rule": "block_destructive_ops",
          "action": "deny",
          "result": "deny",
          "evaluation_ms": 847
        },
        {
          "rule": "require_valid_repo",
          "action": "allow",
          "result": "allow",
          "evaluation_ms": 1203
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
  }
}
```

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | `"allow"` \| `"deny"` | Yes | Gateway's final decision for the request |
| `blocked_ms` | int | Yes | Time in milliseconds the request was blocked waiting for a decision. Will be 0 if all rules are `audit_only`. |
| `evaluation_ms` | int | Yes | Total wall-clock time in milliseconds for all rules to complete evaluation |
| `deciding_rule` | string | No | Name of the rule that caused the decision. Omitted (not null) when no rule triggered a deny. |
| `reason` | string | No | Message from the AI response of the deciding rule. Omitted when `deciding_rule` is omitted. |
| `results` | array | Yes | Array of per-rule evaluation results, ordered by completion time |

### Result Entry Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `rule` | string | Yes | Rule name from the rule definition |
| `action` | `"allow"` \| `"deny"` | Yes | Rule's configured action from the rule definition |
| `mode` | `"audit_only"` | No | Only present when rule mode is `audit_only`. Omitted when `enabled`. |
| `result` | `"allow"` \| `"deny"` \| `"error"` | Yes | What this rule contributed to the decision |
| `evaluation_ms` | int | Yes | Time in milliseconds for this rule to complete |
| `error` | string | No | Only present when `result: "error"`. Brief description (e.g., "timeout", "api_error"). |

## Result Ordering

The `results` array is ordered by completion time (ascending). Results are appended as each goroutine returns:

1. The first entry completed first (lowest `evaluation_ms` relative to start)
2. The last entry completed last (highest `evaluation_ms`)
3. The first entry with `result: "deny"` from an `enabled` rule is the `deciding_rule`

Invariants:
- `results[0].evaluation_ms` ≤ `results[1].evaluation_ms` ≤ ... ≤ `results[n].evaluation_ms`
- `results[n].evaluation_ms` equals the top-level `evaluation_ms` (the last to complete)
- When `deciding_rule` is present, its `evaluation_ms` equals `blocked_ms`

## Policy Evaluation Logic

### Rule Action and Result Mapping

The combination of rule `action` (from config) and AI response determines the `result`:

| Rule Action | AI Response (`allowed:`) | Result | Effect (if `enabled`) |
|-------------|--------------------------|--------|----------------------|
| `deny` | `false` | `"deny"` | **DENY** request |
| `deny` | `true` | `"allow"` | No action (AI didn't find issue) |
| `allow` | `true` | `"allow"` | **ALLOW** confirmed |
| `allow` | `false` | `"deny"` | **DENY** request (required gate failed) |
| any | error/timeout | `"error"` | Treated as DENY (fail-closed) |

### Allow Policy Semantics

An `allow` policy acts as a **required gate**. If the AI returns `allowed: false`, the rule produces `result: "deny"`. This differs from `deny` policies which look for bad things; `allow` policies require confirmation of good things.

### Early Termination Behavior

| Scenario | Can Terminate Early? | Behavior |
|----------|---------------------|----------|
| `deny` policy with `mode: enabled` returns `result: "deny"` | ✅ Yes | Immediately unblock, cancel remaining goroutines |
| `allow` policy with `mode: enabled` returns `result: "deny"` | ✅ Yes | Immediately unblock (gate failed), cancel remaining |
| Any policy with `mode: audit_only` returns any result | ❌ No | Log result, continue (doesn't affect decision) |
| All policies are `mode: audit_only` | ✅ Yes (non-blocking) | Don't block request at all, collect results async |

### Background Completion

When early termination occurs:
1. Request is unblocked immediately
2. Remaining goroutines continue executing in the background
3. All results are collected for the audit log
4. Audit log is written only after all rules complete (or timeout)

This ensures the audit log captures complete information even when the request was unblocked early.

## Examples

### Example 1: Early Termination on Deny

First enabled deny rule triggers, request unblocked at 847ms, evaluation completes at 2341ms.

```json
{
  "request_validation": {
    "ai": {
      "action": "deny",
      "blocked_ms": 847,
      "evaluation_ms": 2341,
      "deciding_rule": "block_destructive_ops",
      "reason": "This operation would delete all user data",
      "results": [
        {
          "rule": "block_destructive_ops",
          "action": "deny",
          "result": "deny",
          "evaluation_ms": 847
        },
        {
          "rule": "require_valid_repo",
          "action": "allow",
          "result": "allow",
          "evaluation_ms": 1203
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
  }
}
```

### Example 2: All Rules Pass (Allowed)

No deny triggered, `deciding_rule` and `reason` omitted.

```json
{
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 1847,
      "evaluation_ms": 1847,
      "results": [
        {
          "rule": "block_destructive_ops",
          "action": "deny",
          "result": "allow",
          "evaluation_ms": 847
        },
        {
          "rule": "require_valid_repo",
          "action": "allow",
          "result": "allow",
          "evaluation_ms": 1203
        },
        {
          "rule": "check_permissions",
          "action": "deny",
          "result": "allow",
          "evaluation_ms": 1847
        }
      ]
    }
  }
}
```

### Example 3: All Audit-Only (Non-Blocking)

All rules are `audit_only`, request not blocked (`blocked_ms: 0`).

```json
{
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 0,
      "evaluation_ms": 1847,
      "results": [
        {
          "rule": "log_destructive_ops",
          "action": "deny",
          "mode": "audit_only",
          "result": "deny",
          "evaluation_ms": 1200
        },
        {
          "rule": "log_permissions",
          "action": "deny",
          "mode": "audit_only",
          "result": "allow",
          "evaluation_ms": 1847
        }
      ]
    }
  }
}
```

### Example 4: Timeout Error

One rule times out, but it's `audit_only` so request is still allowed.

```json
{
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 1203,
      "evaluation_ms": 10000,
      "results": [
        {
          "rule": "require_valid_repo",
          "action": "allow",
          "result": "allow",
          "evaluation_ms": 1203
        },
        {
          "rule": "check_slow_api",
          "action": "deny",
          "mode": "audit_only",
          "result": "error",
          "evaluation_ms": 10000,
          "error": "timeout"
        }
      ]
    }
  }
}
```

### Example 5: Allow Gate Failure

An `allow` rule fails (AI returns `allowed: false`), causing implicit deny.

```json
{
  "request_validation": {
    "ai": {
      "action": "deny",
      "blocked_ms": 1203,
      "evaluation_ms": 2100,
      "deciding_rule": "require_authorized_user",
      "reason": "User does not have permission to access this resource",
      "results": [
        {
          "rule": "block_destructive_ops",
          "action": "deny",
          "result": "allow",
          "evaluation_ms": 847
        },
        {
          "rule": "require_authorized_user",
          "action": "allow",
          "result": "deny",
          "evaluation_ms": 1203
        },
        {
          "rule": "check_rate_limits",
          "action": "deny",
          "result": "allow",
          "evaluation_ms": 2100
        }
      ]
    }
  }
}
```

## Implementation Notes

### Struct Changes

The `AuditValidationInfo` struct needs to be updated to support the new AI-specific schema while maintaining CEL compatibility.

### Native Tool Updates

The following native tools must be updated to parse and display the new schema:

1. **`get_audit_log`** - Parse new AI validation structure, handle missing fields gracefully
2. **`generate_audit_report`** - Update report generation to analyze new timing metrics and rule results

### Test Coverage Requirements

1. **Schema parsing tests** - Verify new struct can be marshaled/unmarshaled correctly
2. **Early termination tests** - Verify request is unblocked when first enabled deny arrives
3. **Timeout tests** - Verify rules timeout correctly and produce `result: "error"`
4. **Audit-only tests** - Verify `blocked_ms: 0` when all rules are `audit_only`
5. **Result ordering tests** - Verify results are ordered by completion time
6. **Allow gate tests** - Verify `allow` rules with `allowed: false` produce `result: "deny"`

### Backwards Compatibility

This schema change is **not backwards compatible**. Existing audit logs will not match the new schema. This is acceptable per requirements.
