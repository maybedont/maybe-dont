# AI Engine Error Logging Specification

## Overview

This specification addresses a gap in error logging for the AI validation engines. Currently, when AI policy evaluations fail, error information is either discarded and replaced with generic classifications (in `ai_engine.go`), or logged without consistent categorization (in `ai_response_engine.go`). This makes debugging difficult because HTTP status codes, OpenAI error messages, and network errors are lost or inconsistently formatted.

## Problem Statement

### Current Behavior in Request Engine

In `ai_engine.go:205-221`, errors are reduced to single-word strings:

```go
if err != nil {
    errMsg := "api_error"
    if policyCtx.Err() == context.DeadlineExceeded {
        errMsg = "timeout"
    } else if policyCtx.Err() == context.Canceled {
        errMsg = "canceled"
    }
    resultChan <- aiRuleResult{
        // ...
        err: errMsg,  // Only stores "api_error", "timeout", or "canceled"
    }
}
```

The `aiRuleResult.err` field is a `string`, so by the time errors are logged (line 534-538), the actual cause is lost:

```go
e.logger.Error(ctx, "AI policy evaluation error",
    zap.String("rule", result.rule),
    zap.String("error", result.err),  // Just "canceled" - no details
)
```

### Current Behavior in Response Engine

The response engine (`ai_response_engine.go`) preserves errors as `error` types, but:
- Does not classify errors into categories (timeout, canceled, api_error, etc.)
- Uses raw `err.Error()` for audit logs instead of a consistent format
- Does not filter expected cancellations from ERROR logs
- Lacks DEBUG level logging with concise summaries

## Goals

1. **ERROR log**: Log full error details including stack trace for unexpected errors (HTTP failures, network issues, API errors)
2. **DEBUG log**: Log a concise summary matching the audit log format (`"category: brief_message"`)
3. **Audit log**: Include a concise but informative error description (more than just "api_error")
4. **Consistency**: Both `ai_engine.go` and `ai_response_engine.go` should handle errors identically
5. **DRY**: Share error formatting logic between request and response engines

## Non-Goals

1. Changing the audit log schema structure
2. Adding new configuration options
3. Modifying CEL engines (CEL errors are primarily compile-time during rule loading)

## Design

### Shared Error Classification Helper

Add a helper function to classify context errors, shared by both engines:

```go
// classifyContextError returns the error category based on context state.
// Returns "timeout" for deadline exceeded, "canceled" for context cancellation,
// or the provided defaultCategory otherwise.
func classifyContextError(ctx context.Context, defaultCategory string) string {
    if ctx.Err() == context.DeadlineExceeded {
        return "timeout"
    } else if ctx.Err() == context.Canceled {
        return "canceled"
    }
    return defaultCategory
}
```

### Shared Error Formatting Helper

Add a helper function to format audit log errors (already implemented in `ai_engine.go`):

```go
// formatAuditError formats an error for the audit log as "category: message".
// The message is truncated to 100 characters to keep audit logs concise.
func formatAuditError(category string, err error) string {
    if err == nil {
        return category
    }

    msg := err.Error()

    // Truncate long messages for audit log (keep concise)
    const maxLen = 100
    if len(msg) > maxLen {
        msg = msg[:maxLen] + "..."
    }

    return fmt.Sprintf("%s: %s", category, msg)
}
```

### Update aiRuleResult (Request Engine)

Already implemented - stores both `err error` and `errCategory string`.

### Update aiResponseRuleResult (Response Engine)

Add `errCategory` field to match the request engine pattern:

```go
type aiResponseRuleResult struct {
    policy       AIResponsePolicy
    result       string // "allow", "deny", "redact", or "error"
    message      string
    redacted     string
    evaluationMs int64
    err          error
    errCategory  string // "api_error", "timeout", "canceled", "parse_error", "no_response"
}
```

### Error Handling at Source

Both engines should classify errors consistently at the point where they occur:

```go
if err != nil {
    errCategory := classifyContextError(policyCtx, "api_error")
    resultChan <- result{
        // ...
        err:         err,
        errCategory: errCategory,
    }
    return
}
```

Apply to all error paths:
- API call failures: `classifyContextError(ctx, "api_error")`
- Empty response: `"no_response"`
- Parse errors: `"parse_error"`

### Logging Strategy

#### ERROR Level: Unexpected Errors Only

Log at ERROR level for errors that indicate something unexpected happened:

```go
if result.err != nil {
    e.logger.Error(ctx, "AI policy evaluation failed",
        zap.String("rule", result.rule),
        zap.String("category", result.errCategory),
        zap.Int64("evaluation_ms", result.evaluationMs),
        zap.Error(result.err),
    )
}
```

**Note on cancellation errors**: With the current implementation, policies use detached contexts and are allowed to complete even after a decision is made. This ensures the audit log captures complete results from all policies. Cancellation errors (`canceled`) should only occur due to external factors like gateway shutdown, not from early termination logic.

#### DEBUG Level: Concise Summary

Log at DEBUG level for all error results with the same concise format used in the audit log:

```go
if result.err != nil {
    e.logger.Debug(ctx, "AI policy evaluation error",
        zap.String("rule", result.rule),
        zap.String("error", formatAuditError(result.errCategory, result.err)),
        zap.Int64("evaluation_ms", result.evaluationMs),
    )
}
```

### Audit Log Format

The audit log `Error` field should be more informative than a single word, but still concise. Format as `category: brief_message`:

| Error Category | Audit Log Error Field |
|----------------|----------------------|
| `timeout` | `"timeout: context deadline exceeded"` |
| `canceled` | `"canceled: context canceled"` |
| `api_error` | `"api_error: <first 100 chars of error message>"` |
| `parse_error` | `"parse_error: <first 100 chars of error message>"` |
| `no_response` | `"no_response: API returned empty choices"` |

## Changes Summary

### Files to Modify

1. **`internal/gateway/ai_engine.go`** (already implemented)
   - Changed `aiRuleResult.err` from `string` to `error`
   - Added `aiRuleResult.errCategory` field
   - Updated error handling with classification
   - Updated logging with ERROR/DEBUG split
   - Added `formatAuditError` helper function

2. **`internal/gateway/ai_response_engine.go`** (to be implemented)
   - Add `aiResponseRuleResult.errCategory` field
   - Update error handling to classify errors (timeout, canceled, api_error, etc.)
   - Update audit log entries to use `formatAuditError`
   - Add DEBUG logging with concise format

### DRY Considerations

The `formatAuditError` function is defined in `ai_engine.go` and accessible to `ai_response_engine.go` since both are in the same package. No code duplication is needed.

Consider extracting `classifyContextError` as a shared helper if the pattern is needed elsewhere.

## Implementation Checklist

### Request Engine (ai_engine.go) - COMPLETED
- [x] Add `errCategory` field to `aiRuleResult` struct
- [x] Change `err` field from `string` to `error` in `aiRuleResult`
- [x] Update error handling at API call site
- [x] Update error handling for empty response
- [x] Update error handling for parse errors
- [x] Add `formatAuditError` helper function
- [x] Update ERROR logging to include full error
- [x] Update DEBUG logging with error category
- [x] Update async path to use new error format
- [x] Update audit result creation to use `formatAuditError`
- [x] Add unit tests for error logging scenarios

### Response Engine (ai_response_engine.go) - TODO
- [ ] Add `errCategory` field to `aiResponseRuleResult` struct
- [ ] Update error handling at API call site to classify errors
- [ ] Update error handling for empty response to use category
- [ ] Update error handling for parse errors to use category
- [ ] Update ERROR logging with full error details
- [ ] Add DEBUG logging with concise format
- [ ] Update async path to use `formatAuditError`
- [ ] Update audit result creation to use `formatAuditError`
- [ ] Add unit tests for response engine error logging
- [ ] Run `make test` and `make lint`

## Testing

### Unit Tests

1. **Test API error logging**: Mock HTTP 401/500 responses, verify ERROR log contains status code
2. **Test timeout logging**: Trigger context deadline, verify ERROR log contains "context deadline exceeded"
3. **Test all policies complete**: Early deny doesn't cancel other policies, all policies run to completion for audit
4. **Test external cancellation**: Gateway shutdown cancels policies, verify ERROR log includes cancellation details
5. **Test audit log format**: Verify `error` field contains `"category: message"` format
6. **Test long error truncation**: Verify errors > 100 chars are truncated in audit log
7. **Test response engine parity**: Verify response engine produces identical error format to request engine

### Manual Verification

1. Configure an invalid API key, verify 401 error is logged with full details
2. Set very short timeout, verify timeout errors include duration context
3. Run with all audit_only policies, trigger errors, verify async path logs correctly
4. Verify both request and response validation produce consistent error formats

## Backwards Compatibility

This change affects:
- **Application logs**: Log format changes (adds more detail). This is purely additive.
- **Audit log `error` field**: Changes from single word to `"category: message"` format. This may affect log parsing tools that expect exact string matches like `"canceled"`.

Consumers of the audit log should update any exact-match filters to use prefix matching (e.g., `startsWith("canceled")` instead of `== "canceled"`).
