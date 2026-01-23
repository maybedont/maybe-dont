# Audit Action Reason Specification

## Overview

This specification adds clarity to audit log entries by distinguishing between what validation recommended (`recommended_action`) and what actually happened (`action`), along with a new `action_reason` field that explains why the action was taken. This enables easier analysis of audit logs, particularly for identifying requests that passed due to `audit_only` mode or fail-open behavior.

## Problem Statement

Currently, `recommended_action` and `action` in audit log entries are always set to the same value. There's no top-level indicator to explain:
- Why a request was allowed when policies would have denied it (audit_only mode)
- Why a request was allowed when evaluation couldn't complete (fail-open)
- Which validation phase caused a deny

To understand these scenarios, consumers must examine individual rule results, checking for `mode: "audit_only"` on each result and inferring the overall outcome.

## Goals

1. **Clear action semantics**: `recommended_action` reflects what validation determined; `action` reflects what actually happened
2. **Explicit reasoning**: New `action_reason` field explains why the action was taken
3. **Simplified analysis**: Audit report tool and humans can quickly identify audit_only bypasses and fail-open scenarios
4. **Consistent fail-open behavior**: CEL errors should fail-open like AI errors for consistency

## Non-Goals

1. Per-validation-phase action reasons (top-level summary is sufficient)
2. Detailed error categorization in `action_reason` (error details remain in individual rule results)

## Design

### Field Definitions

| Field | Type | Description |
|-------|------|-------------|
| `recommended_action` | `string` | What validation would decide if fully evaluated. **Omitted** when fail-open prevents complete evaluation. Values: `"allow"`, `"deny"`, `"redact"` |
| `action` | `string` | What actually happened. Values: `"allow"`, `"deny"`, `"redact"` |
| `action_reason` | `string` | Why the action was taken. **Omitted** when `action == recommended_action` with no special circumstances. |

### `action_reason` Values

| Value | Meaning |
|-------|---------|
| `request_policy` | A request validation policy caused the deny |
| `response_policy` | A response validation policy caused the deny or redact |
| `audit_mode` | Would have denied/redacted but mode is `audit_only` |
| `fail_open` | Couldn't complete evaluation (budget exhausted, timeout, errors) |

### Case Matrix

| Scenario | `recommended_action` | `action` | `action_reason` |
|----------|---------------------|----------|-----------------|
| All rules pass | `"allow"` | `"allow"` | _(omitted)_ |
| Request validation denies | `"deny"` | `"deny"` | `"request_policy"` |
| Response validation denies | `"deny"` | `"deny"` | `"response_policy"` |
| Response validation redacts | `"redact"` | `"redact"` | `"response_policy"` |
| Would deny but `audit_only` | `"deny"` | `"allow"` | `"audit_mode"` |
| Would redact but `audit_only` | `"redact"` | `"allow"` | `"audit_mode"` |
| Budget exhausted | _(omitted)_ | `"allow"` | `"fail_open"` |
| Timeout waiting for rules | _(omitted)_ | `"allow"` | `"fail_open"` |
| AI API errors | _(omitted)_ | `"allow"` | `"fail_open"` |
| CEL evaluation errors | _(omitted)_ | `"allow"` | `"fail_open"` |

### JSON Examples

**Normal allow (all rules pass):**
```json
{
  "recommended_action": "allow",
  "action": "allow"
}
```

**Request validation denies:**
```json
{
  "recommended_action": "deny",
  "action": "deny",
  "action_reason": "request_policy"
}
```

**Would deny but audit_only mode:**
```json
{
  "recommended_action": "deny",
  "action": "allow",
  "action_reason": "audit_mode"
}
```

**Fail-open due to timeout/budget/errors:**
```json
{
  "action": "allow",
  "action_reason": "fail_open"
}
```
Note: `recommended_action` is omitted because we couldn't determine what the recommendation would be.

## Behavior Changes

### 1. CEL Errors Now Fail-Open

**Current behavior**: CEL compilation, program, and evaluation errors cause `deny` for enabled rules.

**New behavior**: CEL errors cause `allow` (fail-open) with:
- `recommended_action`: omitted
- `action`: `"allow"`
- `action_reason`: `"fail_open"`

This makes CEL consistent with AI error handling and follows the principle that validation failures should not block requests.

### 2. `recommended_action` and `action` Can Differ

**Current behavior**: Both are always set to the same value.

**New behavior**: They differ when:
- `audit_only` mode prevents enforcement: `recommended_action` is the would-be decision, `action` is `"allow"`
- Fail-open occurs: `recommended_action` is omitted, `action` is `"allow"`

### 3. New `action_reason` Field

**Current behavior**: No field explaining why the action was taken.

**New behavior**: `action_reason` is present when:
- Any deny occurs: explains which phase (`request_policy` or `response_policy`)
- Any redact occurs: `response_policy`
- Audit mode bypass: `audit_mode`
- Fail-open: `fail_open`

## Implementation

### Files to Modify

1. **`internal/gateway/audit_entry.go`**
   - Add `ActionReason` field to `AuditEntry` struct

2. **`internal/gateway/gateway.go`**
   - Update `SetActions` calls to include action reason
   - Track recommended vs actual action separately
   - Determine action reason based on validation results

3. **`internal/gateway/cel_engine.go`**
   - Change error handling from fail-closed to fail-open
   - Return error information for action reason determination

4. **`internal/gateway/cel_response_engine.go`**
   - Change error handling from fail-closed to fail-open
   - Return error information for action reason determination

5. **`internal/gateway/ai_engine.go`**
   - Return recommended action separately from actual action
   - Support audit_mode detection at engine level

6. **`internal/gateway/ai_response_engine.go`**
   - Return recommended action separately from actual action
   - Support audit_mode detection at engine level

7. **`internal/gateway/tool_validation.go`**
   - Update `ValidationResults` to track recommended vs actual
   - Add action reason to results

8. **`internal/gateway/response_validation.go`**
   - Update `ResponseValidationResults` similarly

9. **`internal/gateway/audit_report_tool.go`**
   - Simplify audit_only detection using `action_reason`

10. **`internal/gateway/audit_log_tool.go`**
    - Add `ActionReason` to parsed audit entry struct

### Implementation Checklist

#### Phase 1: Audit Entry Updates
- [x] Add `ActionReason` field to `AuditEntry` struct
- [x] Update `SetActions` to accept action reason parameter
- [x] Update audit log tool to parse new field

#### Phase 2: CEL Engine Fail-Open
- [x] Change CEL compilation errors to fail-open
- [x] Change CEL program errors to fail-open
- [x] Change CEL evaluation errors to fail-open
- [x] Return fail-open indicator from CEL engine
- [x] Update CEL response engine similarly
- [x] Add tests for CEL fail-open behavior

#### Phase 3: Validation Result Tracking
- [x] Add `RecommendedAction` to `ValidationResults`
- [x] Add `ActionReason` to `ValidationResults`
- [x] Track audit_mode bypass in validation chain
- [x] Track fail_open in validation chain
- [x] Update response validation similarly

#### Phase 4: Gateway Integration
- [x] Update request validation to populate recommended/actual/reason
- [x] Update response validation to populate recommended/actual/reason
- [x] Update `SetActions` calls with correct values
- [x] Handle mixed scenarios (some rules audit_only, some enabled)

#### Phase 5: Audit Report Simplification
- [x] Update audit report tool to use `action_reason` field
- [x] Simplify audit_only detection logic
- [x] Update any filtering logic

#### Phase 6: Testing
- [x] Unit tests for all action_reason values
- [x] Integration tests for audit_only scenarios
- [x] Integration tests for fail_open scenarios
- [x] Test CEL error fail-open behavior
- [x] Run `make test` and `make lint`

## Testing

### Unit Tests

1. **Test normal allow**: All rules pass, verify `action_reason` is omitted
2. **Test request deny**: Request policy denies, verify `action_reason: "request_policy"`
3. **Test response deny**: Response policy denies, verify `action_reason: "response_policy"`
4. **Test response redact**: Response policy redacts, verify `action_reason: "response_policy"`
5. **Test audit_mode bypass**: audit_only rule denies, verify `recommended_action: "deny"`, `action: "allow"`, `action_reason: "audit_mode"`
6. **Test fail_open timeout**: Budget exhausted, verify `recommended_action` omitted, `action_reason: "fail_open"`
7. **Test fail_open CEL error**: CEL evaluation fails, verify fail-open behavior
8. **Test fail_open AI error**: AI API fails, verify fail-open behavior

### Manual Verification

1. Configure policies in `audit_only` mode, trigger deny condition, verify audit log shows `audit_mode`
2. Set very short blocking budget, trigger timeout, verify audit log shows `fail_open`
3. Configure invalid CEL expression, verify fail-open instead of deny

## Backwards Compatibility

This change affects:

- **Audit log schema**: Adds new `action_reason` field (additive)
- **Audit log semantics**: `recommended_action` may now differ from `action` or be omitted
- **CEL error behavior**: Changes from fail-closed (deny) to fail-open (allow)

### Migration Notes

1. **Audit log consumers**: Update parsing to handle:
   - Missing `recommended_action` field (fail-open case)
   - New `action_reason` field
   - `recommended_action != action` scenarios

2. **CEL error handling**: If you relied on CEL errors causing denies, this behavior changes. Consider adding explicit validation rules for edge cases.

## Future Considerations

1. **Configuration option**: Add config to revert CEL errors to fail-closed if needed
2. **Per-phase action reasons**: Could add action_reason to each validation phase for more granular tracking
3. **Error categorization in action_reason**: Could distinguish `fail_open_timeout`, `fail_open_error`, `fail_open_budget` if needed
