# Response Validation for State-Changing Operations

## Status
**Draft** - Problem Statement Only

## Overview

This document describes a fundamental challenge with response validation for operations that modify state. When a tool call or CLI command changes state (creates, updates, or deletes resources), response validation happens *after* the side effect has occurred. This creates a mismatch between what we can meaningfully do with the validation result and what the AI agent perceives.

## The Problem

### Current Response Validation Behavior

Response validation can take two actions when a concern is detected:

1. **Deny** - Return an error to the AI agent
2. **Redact** - Modify the response to remove sensitive content

For read-only operations (listing files, querying data, fetching information), both actions are meaningful:
- **Deny**: Prevent the AI agent from seeing sensitive query results
- **Redact**: Filter specific fields or entries from the response

### The State-Change Problem

For operations that modify state, the timeline looks like this:

```
1. AI agent requests: "Delete repository foo/bar"
2. Request validation: ALLOW
3. MCP server executes: Repository deleted ← IRREVERSIBLE
4. Response received: {"status": "deleted", "repo": "foo/bar"}
5. Response validation: DENY (detects sensitive operation)
6. Return to AI agent: Error message
```

At step 6, we have a problem:
- The repository is **already deleted**
- We tell the AI agent the operation "failed"
- The AI agent's mental model is now wrong

### Consequences of Misleading the AI Agent

If we return an error for a successful state-changing operation:

1. **Retry loops**: The agent may retry the "failed" operation
2. **Incorrect reasoning**: The agent believes the state wasn't changed
3. **Cascading errors**: Subsequent operations based on wrong assumptions
4. **User confusion**: The agent reports failure, but the action succeeded

### MCP Tool Annotations Don't Help

MCP tools can declare `readOnlyHint: true`, but:
- It's a **hint**, not a guarantee
- Many tools don't set it
- A tool marked read-only might still have side effects (logging, caching)
- We can't reliably determine read-only status from the annotation alone

## Current Mitigation

### Policy Writer Responsibility

Currently, the responsibility falls on policy writers to:

1. Only write response validation rules for truly read-only operations
2. Use `redact` action instead of `deny` when possible
3. Understand that `deny` on a state-changing response is misleading

This is documented but not enforced.

## Potential Solutions

### Option 1: Skip Non-Redact Response Rules for State-Changing Operations

If we could reliably detect state-changing operations, we could:
- Run all response rules
- Only apply `redact` actions
- Log but ignore `deny` actions for state-changing operations

**Challenges:**
- Cannot reliably detect state-changing operations
- `readOnlyHint` is unreliable
- Heuristics (checking for "delete", "create", "update" in names) are fragile

### Option 2: Require Explicit Read-Only Declaration in Rules

Response rules could declare what operation types they apply to:

```yaml
rules:
  - name: redact-pii-from-listings
    applies_to: read_only  # Only run for read-only operations
    expression: ...
    action: redact
```

**Challenges:**
- Still need to classify operations as read-only or not
- Adds complexity to rule authoring
- Doesn't solve the classification problem, just moves it

### Option 3: Warn-Only Mode for Response Deny on Uncertain Operations

For operations we can't classify as read-only:
- Log the deny recommendation
- Return the original response unchanged
- Alert operators via audit log

**Trade-off:**
- Safer (doesn't mislead agent)
- Less protective (potential sensitive data exposure)

### Option 4: Pre-Execution Response Estimation (Complex)

Before executing, estimate what kind of response will be returned and whether response rules might deny it. If likely denial, deny at request phase instead.

**Challenges:**
- Highly complex
- Requires understanding tool semantics
- May have false positives blocking legitimate operations

### Option 5: Accept the Limitation

Document clearly that:
- Response validation is meaningful only for read-only operations
- `deny` action on state-changing responses will mislead the AI agent
- Policy writers must understand this limitation
- Future: explore "confirm before execute" patterns

## Recommendation

For V1, **accept the limitation** with clear documentation (Option 5):

1. Document in CLAUDE.md and user docs that response validation is best suited for read-only operations
2. Audit log entries should flag when response `deny` is issued for operations that might be state-changing
3. Consider a config option: `response_validation.warn_on_possible_state_change: true`
4. Future: Explore request-phase "pre-flight" checks that can prevent the operation before execution

## Related Work

- [CLI Proxy Spec](./cli-proxy-for-ai-agents.md) - CLI proxy supports request validation only due to `syscall.Exec`
- MCP specification discussions on operation semantics
- Research into tool classification heuristics

## Implementation Checklist

This is a **problem statement** document. Implementation items will be added when a solution approach is chosen.

- [ ] Add documentation about response validation limitations
- [ ] Add audit log field to flag potential state-changing response denials
- [ ] Consider `warn_on_possible_state_change` config option
- [ ] Research tool classification approaches for future improvement

## Questions for Discussion

1. Should we attempt heuristic classification of operations (risky but protective)?
2. Is the current "policy writer responsibility" approach acceptable for V1?
3. Should response `deny` rules be allowed at all, or should we only support `redact`?
4. Is there value in a "dry run" or "confirm" mode that asks before state-changing operations?
