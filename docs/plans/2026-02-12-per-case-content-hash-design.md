# Per-Test-Case Content Hashing

## Problem

The test suite state management uses per-file content hashing to track cached results.
Each YAML test file can contain multiple test cases (e.g., `ai_request_command_execution.yaml`
has 5 cases), but `ComputeContentHash()` hashes the entire file. All test cases in the same
file share one state entry, causing:

1. **State stores only one result per file** - `RecordResult()` overwrites previous cases'
   results. Only the last case's result survives.
2. **Cache decisions apply to all cases in a file** - `ShouldSkip()` returns the same
   answer for every case sharing a content hash.
3. **Model comparison from cache is wrong** - `GetModelSummaries()` returns 9 entries
   (one per file) instead of 50 (one per case), producing incorrect cached model stats.
4. **History tracking is corrupt** - Rolling history mixes outcomes from different test
   cases within the same file.

Current-run model comparison is always correct (built from in-memory results), which is
why the discrepancy only appears when viewing cached models from prior runs.

## Approach

Replace per-file hashing with per-test-case hashing using JSON serialization of each
case's behavioral fields. Go's `json.Marshal` sorts map keys alphabetically, providing
deterministic output without custom normalization.

### Hash Input

Only fields that affect test behavior are included:

- `Phase` - affects which engine evaluates
- `Engine` - affects which engine evaluates
- `Request` - the test input (tool_name, arguments)
- `Response` - for response validation tests
- `Expectations` - what we're testing for

Excluded (metadata that doesn't affect test outcomes):

- `CaseID` - label only; rename doesn't invalidate cache
- `Title` - display text
- `Tags` - organizational filtering
- `Notes` - documentation

### Trade-offs

- Editing one case in a multi-case file only invalidates that case (siblings stay cached)
- Renaming a case_id has no cache impact
- Adding a comment or whitespace to a YAML file has no cache impact (comments aren't
  part of the parsed struct - acceptable since they don't affect behavior)
- Two test cases with identical behavioral content share a cache entry (correct behavior)

## Changes

### Production Code

**`internal/testsuite/state.go`** - Add `ComputeTestCaseHash()`:

```go
func ComputeTestCaseHash(tc TestCase) string {
    input := struct {
        Phase        string             `json:"phase,omitempty"`
        Engine       string             `json:"engine,omitempty"`
        Request      RequestConfig      `json:"request"`
        Response     *ResponseConfig    `json:"response,omitempty"`
        Expectations ExpectationsConfig `json:"expectations"`
    }{
        Phase:        tc.Phase,
        Engine:       tc.Engine,
        Request:      tc.Request,
        Response:     tc.Response,
        Expectations: tc.Expectations,
    }
    data, _ := json.Marshal(input)
    return ComputeContentHash(data)
}
```

**`internal/testsuite/runner.go`** - Replace hash in `loadTestCases()`:

```go
// Before:
contentHash := ComputeContentHash(data)
for _, tc := range parsed {
    testCaseHashes[tc.CaseID] = contentHash
}

// After:
for _, tc := range parsed {
    testCaseHashes[tc.CaseID] = ComputeTestCaseHash(tc)
}
```

### Test Code

**Unit tests:**

1. `TestComputeTestCaseHash` - table-driven: determinism, field sensitivity, metadata
   exclusion, map key ordering stability, nested map stability
2. `TestLoadTestCases_MultiCaseFileUniqueHashes` - multi-case file produces distinct hashes

**Integration tests (all use multi-case YAML fixture with 3 cases):**

3. `TestIntegration_MultiCaseFile_PerCaseStateTracking` - state has 3 entries not 1
4. `TestIntegration_MultiCaseFile_CrossModelAccumulation` - two models accumulate correctly
5. `TestIntegration_MultiCaseFile_IncrementalCaching` - per-case skip decisions
6. `TestIntegration_MultiCaseFile_RetryFailed` - only failed cases re-run
7. `TestIntegration_MultiCaseFile_ForceMode` - all cases re-execute
8. `TestIntegration_MultiCaseFile_EditOneCasePreservesSiblings` - sibling cache preserved
9. `TestIntegration_MultiCaseFile_ThreeModelCIPattern` - full 3-model CI workflow

### Files NOT Modified

- State structs, `RecordResult()`, `ShouldSkip()`, `GetModelSummaries()`, `PruneStaleHashes()`
- Output formatting, types, workflow file, config, CLI

## State Migration

None. First run after deployment will start fresh - old file-level hashes won't match
new per-case hashes, so all tests re-run. Accepted as the simplest path.

## Risk

Low. The only production change is the hash computation in `loadTestCases()`. All
downstream code operates on content hashes opaquely.
