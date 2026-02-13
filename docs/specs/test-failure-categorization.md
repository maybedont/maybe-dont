# Test Failure Categorization

> **Status**: See [README.md](README.md)

## Problem

The model comparison table currently shows a single `Fail` count and `Match%` per model. When a test case gets the correct decision and matches all expected policies, but also triggers additional policies not listed in the expectations, it is counted as a failure (when `strict_policy_match` is true, the default). This inflates the failure count and deflates the match rate, making it impossible to distinguish between:

1. **Decision failures** — the engine got the wrong answer (e.g., expected deny, got allow)
2. **Extra policy failures** — the engine got the right answer via the right policies, but additional policies also triggered

These are fundamentally different problems. A decision failure means the engine is wrong. An extra policy failure means the engine is right but the test case or policies need tuning. When reviewing AI model accuracy, the user needs to see both numbers separately.

### Current table

```
── Model Comparison ──────────────────────────────────────────────
Model                 Pass   Fail   Err  Match%     Avg ms      Total
openai:gpt-5            42      8     0   84.0%    1234 ms     52.3s
anthropic:claude-3.7     40     10     0   80.0%    1456 ms     58.2s
──────────────────────────────────────────────────────────────────
```

The user cannot tell: are those 8 failures wrong decisions, or cases where extra policies triggered?

## Goals

1. Add an `Extra` column to the model comparison table showing failures caused solely by unexpected policy matches
2. Add an `Adj%` (adjusted match rate) column: `(Pass + Extra) / (Pass + Fail + Extra)` — the accuracy rate when extra policy matches are excluded
3. Surface the same breakdown in JSON output for programmatic consumption
4. Persist failure categorization in cached state so cached model rows also show the breakdown

## Non-Goals

- Changing pass/fail semantics (extra policy matches remain failures when `strict_policy_match` is true)
- Adding per-policy analytics (which policies trigger most, etc.)
- Changing the `strict_policy_match` default behavior
- UI changes to per-test-case output (the visual `►` markers already distinguish expected vs unexpected)

## Design

### Failure Classification

A test result is classified as `extra_policy_only` when **all** of these are true:
1. The overall decision matches expectations (`expected.Decision == actual.Decision`)
2. All expected policies executed with correct decisions (no missing or wrong-decision policies)
3. No redacted content mismatch
4. The only failure is unexpected triggering policies

In code terms: the `compareResults()` function in `executor.go` already detects all four failure categories separately. The change is to track _which_ failures are present rather than flattening them into a `[]string`.

### Data Flow

```
compareResults()          →  compareResultsOutput (add ExtraPolicyOnly bool)
  ↓
buildModelComparison()    →  ModelComparisonEntry (add ExtraPolicyOnly int, AdjMatchRate float64)
  ↓
formatModelComparison()   →  table rendering (add Extra, Adj% columns)
  ↓
CachedModelSummary        →  state persistence (add ExtraPolicyOnly int)
  ↓
JSONModelComparisonEntry  →  JSON output (add extra_policy_only int, adj_match_rate float64)
```

### Changes by File

#### `executor.go`

Add a boolean to `compareResultsOutput`:

```go
type compareResultsOutput struct {
    failures        []string
    warnings        []string
    extraPolicyOnly bool // true when decision + expected policies correct, only unexpected extras
}
```

Set `extraPolicyOnly = true` when:
- `out.failures` is empty after phases 1-3 (decision, redacted content, per-policy checks)
- Phase 4 adds at least one failure (unexpected triggering policies in strict mode)

This is a simple check: if `len(out.failures) == 0` before the phase 4 append, and phase 4 appends a failure, then `extraPolicyOnly = true`.

Update the caller (`executeTest`) to propagate `extraPolicyOnly` into `TestResult`.

#### `executor.go` → `TestResult`

Add field:

```go
type TestResult struct {
    // ... existing fields ...
    ExtraPolicyOnly bool // true when only failure is unexpected policy matches
}
```

#### `types.go` → `ModelComparisonEntry`

Add fields:

```go
type ModelComparisonEntry struct {
    // ... existing fields ...

    // ExtraPolicyOnly is the count of tests that failed solely due to unexpected
    // triggering policies (decision was correct, all expected policies matched)
    ExtraPolicyOnly int

    // AdjMatchRate is the match rate excluding extra-policy-only failures:
    // (Passed + ExtraPolicyOnly) / (Passed + Failed + ExtraPolicyOnly)
    AdjMatchRate float64
}
```

#### `types.go` → `RunResult`

Add field:

```go
type RunResult struct {
    // ... existing fields ...

    // ExtraPolicyOnly is the count of tests that failed only due to unexpected policy matches
    ExtraPolicyOnly int
}
```

#### `runner.go` → `buildModelComparison()`

When iterating results, count `ExtraPolicyOnly` separately from `Failed`:

```go
case "failed":
    if tr.ExtraPolicyOnly {
        entry.ExtraPolicyOnly++
    } else {
        entry.Failed++
    }
```

Compute `AdjMatchRate`:

```go
adjDecided := entry.Passed + entry.Failed + entry.ExtraPolicyOnly
if adjDecided > 0 {
    entry.AdjMatchRate = float64(entry.Passed + entry.ExtraPolicyOnly) / float64(adjDecided)
}
```

#### `runner.go` → `formatModelComparison()`

Add `Extra` and `Adj%` columns. The `Extra` column only appears when at least one entry has `ExtraPolicyOnly > 0` (like `Stab%` appearing only when history exists):

```
── Model Comparison ──────────────────────────────────────────────────────────
Model                 Pass   Fail  Extra   Err  Match%   Adj%     Avg ms      Total
openai:gpt-5            42      2      6     0   84.0%  96.0%    1234 ms     52.3s
anthropic:claude-3.7     40      4      6     0   80.0%  92.0%    1456 ms     58.2s
──────────────────────────────────────────────────────────────────────────────
```

When no entries have extra policy failures, the table renders without the `Extra` and `Adj%` columns (unchanged from today).

#### `runner.go` → `calculateResults()`

Count `ExtraPolicyOnly` in `RunResult`:

```go
if effectiveStatus(tr) == "failed" && tr.ExtraPolicyOnly {
    result.ExtraPolicyOnly++
}
```

#### `state.go` → `CachedModelSummary`

Add field so cached rows also show the breakdown:

```go
type CachedModelSummary struct {
    // ... existing fields ...
    ExtraPolicyOnly int
}
```

Update `GetModelSummaries()` to aggregate `ExtraPolicyOnly` from `CachedResult`. This requires storing the classification in `CachedResult` as well:

```go
type CachedResult struct {
    // ... existing fields ...
    ExtraPolicyOnly bool `json:"extra_policy_only,omitempty"`
}
```

Update `RecordResult()` to accept and store `ExtraPolicyOnly`.

#### `output.go` → JSON structures

Add to `JSONModelComparisonEntry`:

```go
type JSONModelComparisonEntry struct {
    // ... existing fields ...
    ExtraPolicyOnly int     `json:"extra_policy_only"`
    AdjMatchRate    float64 `json:"adj_match_rate"`
}
```

Add to `JSONTestResult`:

```go
type JSONTestResult struct {
    // ... existing fields ...
    ExtraPolicyOnly bool `json:"extra_policy_only,omitempty"`
}
```

### State Migration

No explicit migration needed. The new `extra_policy_only` field in `CachedResult` uses `omitempty` — existing state files without the field will deserialize as `false`, which is correct (we don't know the failure category for historical results). First run after deployment will re-run tests (due to the per-case hashing change in PR #117), populating the new field.

For cached model rows from older state files, `ExtraPolicyOnly` will be 0 and `Adj%` will equal `Match%`. This is conservative and correct — we don't claim accuracy we can't verify.

### Interaction with `strict_policy_match`

When `strict_policy_match: false`, unexpected policy matches produce warnings (not failures), so `ExtraPolicyOnly` will always be 0 — the test passes. The `Extra` and `Adj%` columns are only meaningful when `strict_policy_match: true` (the default).

## Test Plan

1. **Unit test `compareResults`**: Verify `extraPolicyOnly` is true when decision matches, expected policies match, but extra policies trigger
2. **Unit test `compareResults`**: Verify `extraPolicyOnly` is false when decision mismatches (even if extra policies also trigger)
3. **Unit test `compareResults`**: Verify `extraPolicyOnly` is false when an expected policy has wrong decision
4. **Unit test `compareResults`**: Verify `extraPolicyOnly` is false when `strict_policy_match` is false (warnings, not failures)
5. **Unit test `buildModelComparison`**: Verify `ExtraPolicyOnly` count and `AdjMatchRate` calculation
6. **Unit test `formatModelComparison`**: Verify `Extra` and `Adj%` columns appear when data exists, absent when all zeros
7. **Integration test**: Multi-case file with mixed failure types, verify table breakdown
8. **JSON output test**: Verify `extra_policy_only` and `adj_match_rate` fields in JSON
9. **State persistence test**: Record result with `ExtraPolicyOnly`, reload state, verify field preserved
10. **Cached model row test**: Verify cached rows show `ExtraPolicyOnly` from state

## Implementation Notes

- The `Extra` column splits out from `Fail` — i.e., `Fail` count should **decrease** by `ExtraPolicyOnly`. The total `Fail + Extra` equals the old `Fail` count. This is important: `Fail` now means "real failures" and `Extra` means "needs tuning."
- `Adj%` is computed as `(Pass + Extra) / (Pass + Fail + Extra)`. This excludes errored tests (same as current `Match%`).
- The `RecordResult()` function signature changes to accept `ExtraPolicyOnly`. All callers need updating.
