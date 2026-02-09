# Test Pass Rate History

> **Status**: See [docs/specs/README.md](README.md) for current status.

## Overview

Add rolling pass rate tracking to the policy test suite so that each test case + model combination reports its historical pass rate (e.g., "this test passes 85% of the time"). This enables identifying flaky tests, measuring the impact of policy/test changes, and surfacing stability metrics in reporting.

Today, the state file stores a single result per (test_case, model) — the latest pass/fail/error. There is no way to answer: "How reliably does this test pass?" Without this, a test that passes 50% of the time looks identical to one that passes 100% of the time on any given run.

## Goals

1. **Track pass rate over time** — Store the last N run outcomes per (test_case, model) in the state file, enabling pass rate calculation (e.g., "85% over last 20 runs").

2. **Survive cosmetic edits** — Use semantic hashing for test cases and policies so that comments, whitespace, titles, notes, and other non-behavioral fields don't invalidate cached history.

3. **Preserve history across policy changes** — When a policy changes meaningfully, don't discard existing history. Instead, mark a boundary in the history so reporting can show both the blended rate and the rate since the last change.

4. **Surface stability in reporting** — Add pass rate to individual test output, a "flaky tests" summary section, and a stability column in the model comparison table.

## Non-Goals

1. **Unbounded history** — We use a fixed-size ring buffer, not an append-only log. Deep historical analytics (e.g., "pass rate trend over 6 months") would require a separate export pipeline and is out of scope.

2. **Per-run snapshots** — We don't store full run metadata (who triggered it, CI run ID, etc.). The history is a sequence of outcomes, not a run audit trail.

3. **Automatic flaky test remediation** — We surface the data; the user decides what to do about it.

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

#### Pass rate calculation

Add a method to `StateManager` (or as a standalone utility):

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
            break // stop at the policy change boundary (but include the change run itself)
        }
        count++
        if h.Status == "passed" {
            passed++
        }
    }
    return float64(passed) / float64(count), count
}
```

### 2. Semantic Hashing for Test Cases

Replace raw-byte hashing with a canonical hash of only behavior-affecting fields.

#### Meaningful fields (included in hash)

| Field | Rationale |
|-------|-----------|
| `case_id` | Identity |
| `phase` | Determines which engine/phase runs |
| `engine` | Determines which engine runs |
| `request.tool_name` | The actual request being validated |
| `request.arguments` | The actual request data |
| `response` (if present) | For response validation tests |
| `expectations.decision` | What we're checking |
| `expectations.policies` | Which policies should trigger |
| `expectations.redacted_content` | For redact tests |

#### Non-meaningful fields (excluded from hash)

| Field | Rationale |
|-------|-----------|
| `title` | Display label, documentation |
| `tags` | Affects filtering, not test behavior |
| `notes` | Documentation |
| YAML comments | Documentation |
| Whitespace | Formatting |

#### Implementation

```go
// semanticTestCaseHash contains only the fields that affect test behavior.
type semanticTestCaseHash struct {
    CaseID       string              `json:"case_id"`
    Phase        string              `json:"phase"`
    Engine       string              `json:"engine"`
    Request      RequestConfig       `json:"request"`
    Response     *ResponseConfig     `json:"response,omitempty"`
    Expectations ExpectationsConfig  `json:"expectations"`
}

// ComputeSemanticHash computes a content hash from only the behavior-affecting
// fields of a test case. Changes to title, tags, notes, comments, and whitespace
// do not affect the hash.
func ComputeSemanticHash(tc *TestCase) string {
    canonical := semanticTestCaseHash{
        CaseID:       tc.CaseID,
        Phase:        tc.Phase,
        Engine:       tc.Engine,
        Request:      tc.Request,
        Response:     tc.Response,
        Expectations: tc.Expectations,
    }
    data, _ := json.Marshal(canonical) // Go's json.Marshal produces deterministic output for structs
    hash := sha256.Sum256(data)
    return "sha256:" + hex.EncodeToString(hash[:])
}
```

`ComputeContentHash(rawBytes)` is retained for backward compatibility but no longer used as the primary key for test case state. The call site in `discoverTestCases` switches to `ComputeSemanticHash(parsedTestCase)`.

### 3. Semantic Hashing for Policies

Replace raw-byte policy hashing with a canonical hash of only evaluation-affecting fields.

#### Policy types and their meaningful fields

**AI policies** (`gateway.AIPolicy`, `gateway.AIResponsePolicy`):

| Included | Excluded |
|----------|----------|
| `action` (deny/allow/redact) | `name` |
| `prompt` (trimmed leading/trailing whitespace) | `description` |
| | `message` |
| | `enabled` |
| | `mode` |

**CEL policies** (`gateway.CELPolicy`):

| Included | Excluded |
|----------|----------|
| `action` (deny/allow) | `name` |
| `mcp_expression` (trimmed) | `description` |
| `cli_expression` (trimmed) | `message` |
| | `mode` |

#### Implementation

Policy files contain a YAML list of rules under a `rules:` key. The hashing implementation:

1. Parse the YAML file into the appropriate policy struct list.
2. For each rule, extract only the meaningful fields into a canonical struct.
3. Sort by a deterministic key (the trimmed prompt/expression content, since `name` is excluded).
4. Marshal to JSON and hash.

```go
// semanticAIPolicyHash contains only fields that affect AI policy evaluation.
type semanticAIPolicyHash struct {
    Action string `json:"action"`
    Prompt string `json:"prompt"` // strings.TrimSpace applied
}

// semanticCELPolicyHash contains only fields that affect CEL policy evaluation.
type semanticCELPolicyHash struct {
    Action        string `json:"action"`
    MCPExpression string `json:"mcp_expression"` // strings.TrimSpace applied
    CLIExpression string `json:"cli_expression"` // strings.TrimSpace applied
}
```

The existing `hashPolicyPath` function is updated to parse YAML, extract canonical fields, and hash the canonical representation rather than raw bytes.

**Note on whitespace trimming**: Only leading and trailing whitespace on the `prompt`/expression string values is trimmed (`strings.TrimSpace`). Internal whitespace within prompts is preserved byte-for-byte since it can affect AI model interpretation.

### 4. State File Schema Migration

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
- All existing entries have `History: nil` which is fine — they just have no history yet.
- However, **all test case keys will miss** on the first run because the keys change from raw-byte hashes to semantic hashes. This means:
  - Every test re-runs on the first execution after upgrading (one-time cost).
  - `PruneStaleHashes` removes all v1-keyed entries at the end of the run.
  - History begins accumulating from this point forward.

This is an acceptable one-time migration cost. There is no existing history to preserve (that's the feature we're adding), and the v1 cache entries would only save one run of re-execution.

#### CI state artifact

The GitHub Actions workflow (`policy-tests.yaml`) needs no changes. The artifact is downloaded, the state file loads (v1 entries miss, tests re-run), new v2 state is saved, and the artifact is re-uploaded. After one run, CI is fully on v2.

### 5. Reporting Enhancements

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

The 90% threshold is a reasonable default. Tests below this threshold are flagged. If all tests are above 90%, the section shows:

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
    "pass_rate_since_policy_change_runs": 5,
    "history": [
      {"status": "passed", "run_at": "2026-02-09T...", "duration_ms": 2300},
      {"status": "failed", "run_at": "2026-02-08T...", "duration_ms": 1900, "policy_change": true}
    ]
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

### 6. ShouldSkip and History Interaction

The `ShouldSkip` logic remains unchanged. It checks whether the current content hash + policy hashes + model key match a cached entry. If they match and the cached status is "passed" (or any status when `retryFailed=false`), the test is skipped.

When a test IS skipped (cache hit), its existing history is not modified. History only grows when a test actually runs.

This means: if a test is cached as "passed" and gets skipped for 10 runs, then re-runs and fails, the history shows `[F, P]` — not `[F, P, P, P, P, P, P, P, P, P, P]` with phantom "passed" entries from skipped runs. The history reflects actual executions only.

### 7. Summary-Only Mode

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
8. Tests for all new state behavior

### Phase 2: Semantic hashing
1. Implement `ComputeSemanticHash` for test cases
2. Implement semantic policy hashing (`ComputeSemanticPolicyHash`)
3. Update `discoverTestCases` to use semantic hashing
4. Update `computePolicyHashes` / `hashPolicyPath` to use semantic hashing
5. Retain `ComputeContentHash` for any non-test-case uses
6. Tests for hash stability (same semantic content = same hash regardless of formatting)

### Phase 3: Reporting enhancements
1. Add pass rate to individual test streaming output
2. Add stability summary section to `formatTextSummary`
3. Add stability column to `formatModelComparison`
4. Add pass rate fields to JSON output
5. Update `--summary-only` to include stability data
6. Tests for reporting output

### Phase 4: Documentation and cleanup
1. Update CLAUDE.md testing section with history/stability references
2. Update existing policy-test-suite spec to reference this spec
3. Verify CI workflow works with v2 state (one-time migration run)

## Open Questions

1. **Stability threshold**: 90% is proposed as the default threshold for flagging flaky tests. Should this be configurable in `suite.yaml`?

2. **History in JUnit XML**: JUnit doesn't have a standard field for pass rate. Should we encode it as a property/attribute, or leave it out of JUnit output?

3. **Policy hash granularity**: Currently, policy hashes are computed per-file (one hash per YAML file). With semantic hashing, should we hash per-rule instead? Per-rule hashing would mean changing one rule in a file with 10 rules doesn't invalidate history for tests that only exercise the other 9 rules. This is more precise but adds complexity to the hash matching logic. Recommend deferring to a follow-up if needed.
