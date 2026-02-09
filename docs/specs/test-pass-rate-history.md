# Test Pass Rate History

> **Status**: See [docs/specs/README.md](README.md) for current status.

## Overview

Add rolling pass rate tracking to the policy test suite so that each test case + model combination reports its historical pass rate (e.g., "this test passes 85% of the time"). This enables identifying flaky tests, measuring the impact of policy/test changes, and surfacing stability metrics in reporting.

Today, the state file stores a single result per (test_case, model) — the latest pass/fail/error. There is no way to answer: "How reliably does this test pass?" Without this, a test that passes 50% of the time looks identical to one that passes 100% of the time on any given run.

## Goals

1. **Track pass rate over time** — Store the last N run outcomes per (test_case, model) in the state file, enabling pass rate calculation (e.g., "85% over last 20 runs").

2. **Preserve history across policy changes** — When a policy changes, don't discard existing history. Instead, mark a boundary in the history so reporting can show both the blended rate and the rate since the last change.

3. **Surface stability in reporting** — Add pass rate to individual test output, a "flaky tests" summary section, and a stability column in the model comparison table. The stability threshold is configurable.

## Non-Goals

1. **Unbounded history** — We use a fixed-size ring buffer, not an append-only log. Deep historical analytics (e.g., "pass rate trend over 6 months") would require a separate export pipeline and is out of scope.

2. **Per-run snapshots** — We don't store full run metadata (who triggered it, CI run ID, etc.). The history is a sequence of outcomes, not a run audit trail.

3. **Automatic flaky test remediation** — We surface the data; the user decides what to do about it.

4. **Semantic hashing** — Using canonical hashes of only behavior-affecting fields (to survive cosmetic edits like comment/title changes) was considered and deferred. The benefit is narrow (cosmetic edits to existing tests are rare), the ongoing maintenance burden is real (every new field requires behavioral vs. cosmetic classification), and the cost asymmetry favors the safe default: a false re-run costs one API call (~$0.01-0.10), while a false cache hit from a wrong field classification produces silently stale results. Raw-byte hashing catches all changes, which is always safe. If cosmetic-edit cache invalidation becomes a measured pain point, semantic hashing can be tackled as a focused follow-up spec.

5. **Per-rule policy hashing** — Currently, policy hashes are computed per-file. Changing one rule in a file with 10 rules invalidates all tests touching that file. Per-rule hashing would be more precise but adds significant complexity (mapping which rules a test exercises is not straightforward for AI rules). Defer unless "unnecessary re-runs after single-rule edits" becomes a measured problem.

## Future Enhancements

### Per-test-case `min_pass_rate`

Today, the stability threshold is global (`acceptance.stability_threshold`). All tests are held to the same bar. This doesn't account for test cases that intentionally push AI reasoning boundaries — a test designed to detect subtle credential exfiltration via base64 encoding may only match 70% of the time, and that's acceptable given the difficulty of the task.

A per-test-case `min_pass_rate` field in `expectations` would let authors declare the expected reliability floor:

```yaml
# cases/ai-req-045.yaml
case_id: ai-req-045
title: Detect subtle credential exfiltration via base64 encoding
tags: [credential-access, edge-case]
expectations:
  decision: deny
  min_pass_rate: 0.70  # This test pushes AI reasoning boundaries
```

The stability section would compare each test's pass rate against its own `min_pass_rate`, falling back to the global `stability_threshold` when not set.

This also enables a **ratchet workflow**: start with a permissive threshold for a boundary-pushing test, observe that a newer model achieves a higher rate, and tighten the threshold to lock in the improvement. For example, a test that required 70% starts achieving 85% with a new model — the author raises `min_pass_rate` to 0.80, ensuring the improvement is preserved.

Additionally, a consistently 0% pass rate for a specific model is valuable data — it indicates that model is not a good fit for policy validation at runtime, rather than indicating the test is flaky.

## Design

### 1. Run History Ring Buffer

#### New types

Add a `RunOutcome` struct and a `History` field to `CachedResult`:

```go
// RunOutcome records the outcome of a single test execution.
type RunOutcome struct {
    Status       string    `json:"status"`                    // "passed", "failed", "errored"
    RunAt        time.Time `json:"run_at"`
    DurationMs   int64     `json:"duration_ms"`
    PolicyChange bool      `json:"policy_change,omitempty"`   // true when policy hashes differ from previous run
}

// CachedResult stores the cached result for a single model run.
type CachedResult struct {
    // Current result (latest run) — existing fields, unchanged
    Status     string    `json:"status"`
    Confidence float64   `json:"confidence"`
    LastRun    time.Time `json:"last_run"`
    DurationMs int64     `json:"duration_ms"`

    // Rolling history of recent runs (most recent first, capped at history_depth)
    History []RunOutcome `json:"history,omitempty"`
}
```

#### RecordResult behavior change

When `RecordResult` is called:

1. Look up the existing `CachedResult` for this model (if any).
2. Determine if this is a policy change: compare the incoming `policyHashes` against the existing `cached.PolicyHashes`. If they differ, set `PolicyChange: true` on the new `RunOutcome`.
3. Prepend a new `RunOutcome` to `History`.
4. Trim `History` to `historyDepth` (default: 20).
5. Update the top-level `Status`, `Confidence`, `LastRun`, `DurationMs` fields as before.

```go
func (sm *StateManager) RecordResult(contentHash, caseID string, policyHashes []string, modelKey string, result *CachedResult) {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    cached, ok := sm.state.Results[contentHash]
    if !ok {
        cached = &CachedTestCase{
            CaseID: caseID,
            Models: make(map[string]*CachedResult),
        }
        sm.state.Results[contentHash] = cached
    }

    // Detect policy change by comparing incoming hashes against stored hashes
    policyChanged := ok && cached.PolicyHashes != nil && !hashesMatch(cached.PolicyHashes, policyHashes)

    existing := cached.Models[modelKey]

    // Build the new history entry
    outcome := RunOutcome{
        Status:       result.Status,
        RunAt:        result.LastRun,
        DurationMs:   result.DurationMs,
        PolicyChange: policyChanged,
    }

    // Preserve existing history, prepend new outcome
    var history []RunOutcome
    if existing != nil {
        history = existing.History
    }
    history = append([]RunOutcome{outcome}, history...)

    // Trim to configured depth
    if len(history) > sm.historyDepth {
        history = history[:sm.historyDepth]
    }

    // Update the cached result
    result.History = history
    cached.PolicyHashes = policyHashes
    cached.Models[modelKey] = result
    sm.dirty = true
}
```

#### History depth configuration

Add `history_depth` to `ExecutionConfig` in `suite.yaml`:

```yaml
execution:
  history_depth: 20  # default
```

Also support a `--history-depth` CLI flag for override. The `StateManager` receives this value at construction time.

**Note on depth changes**: If `history_depth` is reduced (e.g., from 20 to 10), newly-run tests are trimmed immediately. Skipped tests (cache hits) retain their old-depth histories until they naturally re-run. This is harmless — the extra entries are just ignored by pass rate calculations until they age out — but is worth a comment in the code.

#### Pass rate calculation

Add standalone utility functions:

```go
// PassRate computes the pass rate from a CachedResult's history.
// Returns (rate, runCount). Rate is 0.0-1.0. runCount is the number
// of history entries used (may be less than history_depth if the test
// hasn't run that many times yet).
func PassRate(cr *CachedResult) (float64, int) {
    if len(cr.History) == 0 {
        return 0, 0
    }
    passed := 0
    for _, h := range cr.History {
        if h.Status == "passed" {
            passed++
        }
    }
    return float64(passed) / float64(len(cr.History)), len(cr.History)
}

// PassRateSincePolicyChange computes the pass rate using only runs
// after the most recent policy change marker. Returns (rate, runCount).
// If no policy change marker exists, returns the full history rate.
func PassRateSincePolicyChange(cr *CachedResult) (float64, int) {
    if len(cr.History) == 0 {
        return 0, 0
    }
    passed := 0
    count := 0
    for _, h := range cr.History {
        if h.PolicyChange && count > 0 {
            break // stop at the policy change boundary
        }
        count++
        if h.Status == "passed" {
            passed++
        }
    }
    return float64(passed) / float64(count), count
}
```

### 2. State File Schema Migration

#### Schema version bump

The state file `schema_version` changes from `"v1"` to `"v2"`. New field `history_depth` is added at the top level.

```json
{
  "schema_version": "v2",
  "product_version": "dev",
  "suite_id": "default-policies",
  "history_depth": 20,
  "last_updated": "2026-02-09T...",
  "results": { ... }
}
```

#### Backward compatibility

When `NewStateManager` loads a `v1` file:
- It reads successfully (all v1 fields are a subset of v2).
- All existing entries have `History: nil` — they begin accumulating history from their next actual execution.
- **No cache invalidation occurs.** Since we retain raw-byte hashing, all existing content hash keys and policy hash keys remain valid. Existing cached results continue to be used for cache skip decisions. History simply starts empty and fills up over subsequent runs.
- The schema version is updated to `v2` on the next save.

This is a fully non-destructive upgrade — no re-runs required.

#### CI state artifact

The GitHub Actions workflow (`policy-tests.yaml`) needs no changes. The artifact is downloaded, the v1 state loads and works immediately with v2 code, history starts accumulating, and the updated v2 state is re-uploaded.

### 3. Reporting Enhancements

#### Individual test output (streaming)

When a test completes and has history with 2+ entries, append pass rate:

```
✓ [12/45] ai-req-020 (ai, openai:gpt-5)
  Block rm -rf command (2.3s, request)
  Decision: deny ✓ (confidence: 0.95)
  Pass rate: 85% (last 20 runs)
```

If a policy change marker exists in history:

```
✗ [13/45] ai-req-045 (ai, anthropic:claude-haiku)
  Block credential file access (1.8s, request)
  Decision: allow ✗ (expected: deny, confidence: 0.72)
  Pass rate: 74% (last 20 runs), 80% since last policy change (5 runs)
```

Pass rate is NOT shown for tests with only 1 history entry (first run — rate would be 100% or 0% which is not informative).

#### Stability summary section

Add a "Stability" section to `formatTextSummary`, shown after the existing summary and before policy coverage:

```
── Stability ──────────────────────────────────────
Flaky tests (pass rate < 90%):
  ⚠ ai-req-045: 74% (openai:gpt-5), 60% (anthropic:claude-haiku)
  ⚠ ai-req-033: 82% (anthropic:claude-haiku)
Stable tests: 43/45 (96%)
```

The stability threshold defaults to 90% and is configurable in `suite.yaml`:

```yaml
acceptance:
  min_match_rate: 0.85
  stability_threshold: 0.90  # pass rate below this flags a test as flaky
```

If all tests are above the threshold:

```
── Stability ──────────────────────────────────────
All 45 tests stable (≥90% pass rate)
```

Tests with fewer than 3 history entries are excluded from stability reporting (insufficient data).

#### Model comparison table

Add a `Stab%` column showing average pass rate across all tests for each model:

```
Model                          Pass  Fail  Err  Match%  Avg ms  Stab%
openai:gpt-5                     42     3    0   93.3%   2.1s    96%
anthropic:claude-haiku            38     7    0   84.4%   1.8s    87%
```

`Stab%` is the mean pass rate across all test cases that have 3+ history entries for that model.

#### JSON output

Add fields to the per-test result and per-model summary in the JSON output:

```json
{
  "results": [{
    "case_id": "ai-req-020",
    "status": "passed",
    "pass_rate": 0.85,
    "pass_rate_runs": 20,
    "pass_rate_since_policy_change": 1.0,
    "pass_rate_since_policy_change_runs": 5
  }],
  "model_summaries": {
    "openai:gpt-5": {
      "passed": 42,
      "failed": 3,
      "stability": 0.96
    }
  }
}
```

History entries are not included in JSON output — they are an internal state concern. Pass rate and run count are the derived metrics that consumers need.

#### JUnit XML

Pass rate is not included in JUnit XML output. JUnit consumers (CI dashboards) don't have standard support for custom metrics, and adding `<property>` elements provides little value to existing tooling.

### 4. ShouldSkip and History Interaction

The `ShouldSkip` logic remains unchanged. It checks whether the current content hash + policy hashes + model key match a cached entry. If they match and the cached status is "passed" (or any status when `retryFailed=false`), the test is skipped.

When a test IS skipped (cache hit), its existing history is not modified. History only grows when a test actually runs.

This means: if a test is cached as "passed" and gets skipped for 10 runs, then re-runs and fails, the history shows `[F, P]` — not `[F, P, P, P, P, P, P, P, P, P, P]` with phantom "passed" entries from skipped runs. The history reflects actual executions only.

### 5. Summary-Only Mode

The existing `--summary-only` flag shows summary from cached state without running tests. This mode will now also show:
- Pass rates for each test (from cached history)
- Stability summary section
- Model stability column

This makes `--summary-only` the primary way to check test stability without incurring API costs.

## Size Analysis

Current state file: ~50 test cases x ~3 models x ~200 bytes/entry = ~30KB

With 20-entry history: ~50 x 3 x (200 + 20 x 120) = ~50 x 3 x 2600 = ~390KB

This is well within reason for a JSON file read/written on each test. No separate storage mechanism is needed.

## Implementation Plan

### Phase 1: State file changes (core infrastructure)
1. Add `RunOutcome` struct and `History` field to `CachedResult`
2. Add `historyDepth` to `StateManager` and `StateFile`
3. Update `RecordResult` to append history with ring buffer trimming
4. Add policy change detection to `RecordResult`
5. Add `PassRate()` and `PassRateSincePolicyChange()` utility functions
6. Bump schema version to `v2`, handle `v1` loading
7. Add `history_depth` to `ExecutionConfig` and CLI flag
8. Add `stability_threshold` to `AcceptanceConfig`
9. Tests for all new state behavior

### Phase 2: Reporting enhancements
1. Add pass rate to individual test streaming output
2. Add stability summary section to `formatTextSummary`
3. Add stability column to `formatModelComparison`
4. Add pass rate fields to JSON output
5. Update `--summary-only` to include stability data
6. Tests for reporting output

### Phase 3: Documentation and cleanup
1. Update CLAUDE.md testing section with history/stability references
2. Update existing policy-test-suite spec to reference this spec
3. Verify CI workflow works with v2 state (non-destructive upgrade)
