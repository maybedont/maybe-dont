# Per-Test-Case Content Hashing - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the test suite state management to track each test case individually instead of per-file, so model comparison data from cached state is correct.

**Architecture:** Replace the per-file `ComputeContentHash(fileBytes)` call in `loadTestCases()` with a new `ComputeTestCaseHash(tc)` function that JSON-serializes each test case's behavioral fields (request, response, expectations, phase, engine) and hashes the result. Go's `json.Marshal` sorts map keys alphabetically, giving deterministic hashes. Metadata fields (case_id, title, tags, notes) are excluded so renames don't invalidate cache.

**Tech Stack:** Go, `encoding/json`, `crypto/sha256`, testify

**Design doc:** `docs/plans/2026-02-12-per-case-content-hash-design.md`

**Worktree:** `.worktrees/degroff/matrix_state_fix`

---

## Task 1: Add `ComputeTestCaseHash` and write unit tests

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/state.go` (add function after `ComputePolicyHash` at line ~459)
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/state_test.go` (add test at end of file)

### Step 1: Write the failing test

Add `TestComputeTestCaseHash` to `state_test.go`. Table-driven test covering:

```go
func TestComputeTestCaseHash(t *testing.T) {
	baseCase := TestCase{
		CaseID: "test-001",
		Title:  "Test case",
		Tags:   []string{"ai", "request"},
		Notes:  []string{"A note"},
		Phase:  "request",
		Engine: "ai",
		Request: RequestConfig{
			ToolName:  "test__action",
			Arguments: map[string]any{"path": "/tmp/file.txt", "mode": "read"},
		},
		Expectations: ExpectationsConfig{
			Decision: "deny",
			Policies: []PolicyExpectation{
				{PolicyName: "test-policy", Decision: "deny"},
			},
		},
	}

	tests := []struct {
		name      string
		a         TestCase
		b         TestCase
		expectSame bool
	}{
		{
			name:       "same case produces same hash",
			a:          baseCase,
			b:          baseCase,
			expectSame: true,
		},
		{
			name: "different case_id same hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.CaseID = "different-id"
				return tc
			}(),
			expectSame: true,
		},
		{
			name: "different title same hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Title = "Different title"
				return tc
			}(),
			expectSame: true,
		},
		{
			name: "different tags same hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Tags = []string{"different", "tags"}
				return tc
			}(),
			expectSame: true,
		},
		{
			name: "different notes same hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Notes = []string{"different note"}
				return tc
			}(),
			expectSame: true,
		},
		{
			name: "different tool_name different hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Request.ToolName = "test__other_action"
				return tc
			}(),
			expectSame: false,
		},
		{
			name: "different arguments different hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Request.Arguments = map[string]any{"path": "/etc/passwd"}
				return tc
			}(),
			expectSame: false,
		},
		{
			name: "different decision different hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Expectations.Decision = "allow"
				return tc
			}(),
			expectSame: false,
		},
		{
			name: "different phase different hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Phase = "response"
				return tc
			}(),
			expectSame: false,
		},
		{
			name: "different engine different hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Engine = "cel"
				return tc
			}(),
			expectSame: false,
		},
		{
			name: "with response vs without different hash",
			a:    baseCase,
			b: func() TestCase {
				tc := baseCase
				tc.Response = &ResponseConfig{
					Content: []ContentItem{{Type: "text", Text: "secret data"}},
				}
				return tc
			}(),
			expectSame: false,
		},
		{
			name: "map key ordering does not affect hash",
			a: func() TestCase {
				tc := baseCase
				tc.Request.Arguments = map[string]any{"alpha": "1", "beta": "2", "gamma": "3"}
				return tc
			}(),
			b: func() TestCase {
				tc := baseCase
				// Different insertion order, same keys and values
				tc.Request.Arguments = map[string]any{"gamma": "3", "alpha": "1", "beta": "2"}
				return tc
			}(),
			expectSame: true,
		},
		{
			name: "nested map key ordering does not affect hash",
			a: func() TestCase {
				tc := baseCase
				tc.Request.Arguments = map[string]any{
					"config": map[string]any{"zebra": "z", "apple": "a"},
				}
				return tc
			}(),
			b: func() TestCase {
				tc := baseCase
				tc.Request.Arguments = map[string]any{
					"config": map[string]any{"apple": "a", "zebra": "z"},
				}
				return tc
			}(),
			expectSame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hashA := ComputeTestCaseHash(tt.a)
			hashB := ComputeTestCaseHash(tt.b)

			assert.NotEmpty(t, hashA, "hash should not be empty")
			assert.True(t, strings.HasPrefix(hashA, "sha256:"), "hash should have sha256 prefix")

			if tt.expectSame {
				assert.Equal(t, hashA, hashB, "hashes should be equal")
			} else {
				assert.NotEqual(t, hashA, hashB, "hashes should differ")
			}
		})
	}
}
```

Note: add `"strings"` to the imports in `state_test.go`.

### Step 2: Run test to verify it fails

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestComputeTestCaseHash -v ./internal/testsuite/`

Expected: FAIL - `ComputeTestCaseHash` undefined.

### Step 3: Write minimal implementation

Add to `state.go` after `ComputePolicyHash` (after line 459):

```go
// ComputeTestCaseHash produces a deterministic hash of a test case's behavioral
// fields. Metadata (CaseID, Title, Tags, Notes) is excluded so that renames and
// documentation changes don't invalidate the cache. Uses JSON marshaling which
// sorts map keys alphabetically, ensuring deterministic output for map[string]any
// fields like Arguments.
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

### Step 4: Run test to verify it passes

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestComputeTestCaseHash -v ./internal/testsuite/`

Expected: PASS - all 13 subtests pass.

### Step 5: Run full test suite to verify no regressions

Run: `cd .worktrees/degroff/matrix_state_fix && go test ./internal/testsuite/...`

Expected: All existing tests still pass (new function is additive, not yet wired in).

### Step 6: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/state.go internal/testsuite/state_test.go
git commit -m "feat: add ComputeTestCaseHash for per-case content hashing

Hashes only behavioral fields (phase, engine, request, response,
expectations) using JSON serialization. Metadata fields (case_id,
title, tags, notes) are excluded so renames don't invalidate cache.
json.Marshal sorts map keys alphabetically for deterministic output."
```

---

## Task 2: Wire in the new hash and fix `loadTestCases`

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/runner.go:421-452` (the `loadTestCases` loop)

### Step 1: Make the change

In `runner.go`, in the `loadTestCases()` function, replace the per-file hash loop. The current code (lines 421-452):

```go
for _, path := range caseFiles {
    // Read raw file content for hashing
    rawContent, err := os.ReadFile(path)
    if err != nil {
        return &PathResolutionError{
            Path:    path,
            Message: fmt.Sprintf("failed to read file for hashing: %v", err),
        }
    }
    contentHash := ComputeContentHash(rawContent)

    cases, err := r.parseTestCases(path)
    if err != nil {
        return err
    }

    for _, tc := range cases {
        // Check for duplicate case_id
        if existingPath, exists := seenIDs[tc.CaseID]; exists {
            return &SchemaValidationError{
                Message: fmt.Sprintf("duplicate case_id %q", tc.CaseID),
                Details: []string{
                    fmt.Sprintf("First occurrence: %s", existingPath),
                    fmt.Sprintf("Duplicate: %s", path),
                },
            }
        }
        seenIDs[tc.CaseID] = path
        testCaseHashes[tc.CaseID] = contentHash
        testCaseFiles[tc.CaseID] = path
        testCases = append(testCases, tc)
    }
}
```

Replace with:

```go
for _, path := range caseFiles {
    cases, err := r.parseTestCases(path)
    if err != nil {
        return err
    }

    for _, tc := range cases {
        // Check for duplicate case_id
        if existingPath, exists := seenIDs[tc.CaseID]; exists {
            return &SchemaValidationError{
                Message: fmt.Sprintf("duplicate case_id %q", tc.CaseID),
                Details: []string{
                    fmt.Sprintf("First occurrence: %s", existingPath),
                    fmt.Sprintf("Duplicate: %s", path),
                },
            }
        }
        seenIDs[tc.CaseID] = path
        testCaseHashes[tc.CaseID] = ComputeTestCaseHash(tc)
        testCaseFiles[tc.CaseID] = path
        testCases = append(testCases, tc)
    }
}
```

Key changes:
- Removed `rawContent` read and `ComputeContentHash(rawContent)` call
- Replaced `contentHash` with `ComputeTestCaseHash(tc)` per case
- Removed the now-unused `os.ReadFile` for hashing (note: `parseTestCases` reads the file internally)

### Step 2: Run all existing tests

Run: `cd .worktrees/degroff/matrix_state_fix && go test ./internal/testsuite/...`

Expected: All existing tests pass. The existing integration tests use single-case-per-file fixtures, so the hash values change but behavior is identical (each file still gets one unique hash).

### Step 3: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/runner.go
git commit -m "fix: use per-test-case content hashing instead of per-file

The content hash used as the state cache key was computed from the entire
YAML file, causing all test cases in a multi-case file to share one state
entry. This made RecordResult overwrite siblings, ShouldSkip return the
same answer for every case in a file, and GetModelSummaries return one
entry per file instead of per case.

Now each test case is hashed individually using ComputeTestCaseHash,
which serializes only behavioral fields via JSON for deterministic output."
```

---

## Task 3: Add multi-case test fixture and first integration test

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/runner_integration_test.go` (add fixture + test)

### Step 1: Add multi-case YAML fixture constant

Add after the existing `testCaseAllow` constant (around line 246):

```go
// multiCaseYAML contains 3 test cases in a single file (the multi-case format
// that triggered the per-file hashing bug). mc-001 and mc-003 expect deny,
// mc-002 expects allow.
const multiCaseYAML = `- case_id: mc-001
  title: "Multi-case deny 1"
  engine: ai
  phase: request
  request:
    tool_name: "test__dangerous_action"
    arguments:
      command: "delete-all-data"
  expectations:
    decision: deny
    policies:
      - policy_name: "test-policy"
        decision: deny

- case_id: mc-002
  title: "Multi-case allow"
  engine: ai
  phase: request
  request:
    tool_name: "test__safe_action"
    arguments:
      path: "/tmp/readme.txt"
  expectations:
    decision: allow
    policies:
      - policy_name: "test-policy"
        decision: allow

- case_id: mc-003
  title: "Multi-case deny 2"
  engine: ai
  phase: request
  request:
    tool_name: "test__another_dangerous"
    arguments:
      command: "drop-database"
  expectations:
    decision: deny
    policies:
      - policy_name: "test-policy"
        decision: deny
`
```

### Step 2: Write the core regression test

Add after the new fixture:

```go
// --- Multi-case file tests ---
// These tests verify that multiple test cases in a single YAML file are tracked
// individually in the state, not collapsed into one entry per file.

// TestIntegration_MultiCaseFile_PerCaseStateTracking verifies that a single YAML
// file with 3 test cases produces 3 separate state entries, not 1.
// This is the core regression test for the per-file hashing bug.
func TestIntegration_MultiCaseFile_PerCaseStateTracking(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	// Single file with 3 cases
	setupTestSuite(t, dir, testPolicy, map[string]string{
		"multi.yaml": multiCaseYAML,
	})

	ctx := context.Background()

	// Run with deny mock: mc-001 pass, mc-002 fail (expects allow), mc-003 pass
	runner, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	result, err := runner.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, result.TotalCases, "should run all 3 cases")
	assert.Equal(t, 2, result.Passed, "mc-001 and mc-003 should pass")
	assert.Equal(t, 1, result.Failed, "mc-002 should fail (expects allow, got deny)")

	// Verify state has 3 entries (not 1)
	sm, err := NewStateManager(stateFile, "integration-test", "dev", 0)
	require.NoError(t, err)

	policyHashes := readPolicyHashes(t, dir)
	summaries := sm.GetModelSummaries(policyHashes)
	require.Contains(t, summaries, "openai:test-model-a")

	summary := summaries["openai:test-model-a"]
	assert.Equal(t, 3, summary.TestCount, "state should have 3 entries, not 1")
	assert.Equal(t, 2, summary.Passed, "state should show 2 passed")
	assert.Equal(t, 1, summary.Failed, "state should show 1 failed")
}
```

### Step 3: Run the test

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestIntegration_MultiCaseFile_PerCaseStateTracking -v ./internal/testsuite/`

Expected: PASS - 3 state entries, 2 passed, 1 failed.

### Step 4: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/runner_integration_test.go
git commit -m "test: add multi-case file fixture and core state tracking test

Adds a 3-case YAML fixture and an integration test that verifies each
case gets its own state entry. This is the core regression test for
the per-file hashing bug."
```

---

## Task 4: Cross-model accumulation test

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/runner_integration_test.go`

### Step 1: Write the test

```go
// TestIntegration_MultiCaseFile_CrossModelAccumulation verifies that running
// model A then model B against a multi-case file preserves model A's per-case
// results in the cached model comparison.
func TestIntegration_MultiCaseFile_CrossModelAccumulation(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	outputFile := filepath.Join(dir, "results.json")

	setupTestSuiteWithYAML(t, dir, twoModelSuiteYAML, testPolicy, map[string]string{
		"multi.yaml": multiCaseYAML,
	})

	ctx := context.Background()

	// Run 1: model A
	runner1, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	result1, err := runner1.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, result1.Passed)
	assert.Equal(t, 1, result1.Failed)

	// Run 2: model B with JSON output
	runner2, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-b",
		StateFile:             stateFile,
		Quiet:                 true,
		OutputFormat:          "json",
		OutputFile:            outputFile,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	result2, err := runner2.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, result2.Passed)
	assert.Equal(t, 1, result2.Failed)

	// Verify state has both models with correct per-case counts
	sm, err := NewStateManager(stateFile, "integration-test", "dev", 0)
	require.NoError(t, err)
	policyHashes := readPolicyHashes(t, dir)
	summaries := sm.GetModelSummaries(policyHashes)

	require.Contains(t, summaries, "openai:test-model-a")
	require.Contains(t, summaries, "openai:test-model-b")
	assert.Equal(t, 3, summaries["openai:test-model-a"].TestCount, "model A should have 3 entries")
	assert.Equal(t, 2, summaries["openai:test-model-a"].Passed, "model A should have 2 passed")
	assert.Equal(t, 1, summaries["openai:test-model-a"].Failed, "model A should have 1 failed")
	assert.Equal(t, 3, summaries["openai:test-model-b"].TestCount, "model B should have 3 entries")

	// Verify JSON model_comparison shows model A from cache with correct counts
	output := parseJSONOutput(t, outputFile)
	byName := modelComparisonByName(output.ModelComparison)

	require.Contains(t, byName, "openai:test-model-a")
	assert.True(t, byName["openai:test-model-a"].FromCache, "model A should be from cache")
	assert.Equal(t, 2, byName["openai:test-model-a"].Passed, "cached model A should show 2 passed (not 1)")
	assert.Equal(t, 1, byName["openai:test-model-a"].Failed, "cached model A should show 1 failed")

	assert.False(t, byName["openai:test-model-b"].FromCache, "model B should be current")
	assert.Equal(t, 2, byName["openai:test-model-b"].Passed)
	assert.Equal(t, 1, byName["openai:test-model-b"].Failed)
}
```

### Step 2: Run

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestIntegration_MultiCaseFile_CrossModelAccumulation -v ./internal/testsuite/`

Expected: PASS.

### Step 3: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/runner_integration_test.go
git commit -m "test: add cross-model accumulation test for multi-case files

Verifies that running model A then model B preserves per-case results
for model A in the cached model comparison output."
```

---

## Task 5: Incremental caching test

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/runner_integration_test.go`

### Step 1: Write the test

```go
// TestIntegration_MultiCaseFile_IncrementalCaching verifies that incremental mode
// caches each case individually within a multi-case file. Passing cases are skipped,
// the failing case is re-run.
func TestIntegration_MultiCaseFile_IncrementalCaching(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"multi.yaml": multiCaseYAML,
	})

	ctx := context.Background()

	// Run 1: deny mock → mc-001 pass, mc-002 fail, mc-003 pass
	mock1 := newDenyMock()
	runner1, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(mock1),
	})
	require.NoError(t, err)
	result1, err := runner1.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, result1.TotalCases)
	assert.Equal(t, 0, result1.CachedCount, "first run should have no cached results")
	assert.Len(t, mock1.GetRecordedRequests(), 3, "first run should call AI provider 3 times")

	// Run 2: same model, incremental — 2 passing cached, 1 failing re-runs
	mock2 := newDenyMock()
	runner2, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(mock2),
	})
	require.NoError(t, err)
	result2, err := runner2.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, result2.TotalCases, "should still report 3 total cases")
	assert.Equal(t, 2, result2.CachedCount, "2 passing cases should be cached")
	assert.Equal(t, 2, result2.Passed, "2 cases should show as passed (cached)")
	assert.Equal(t, 1, result2.Failed, "1 case should still show as failed (cached)")
	assert.Len(t, mock2.GetRecordedRequests(), 0, "no tests should re-run in default incremental (all cached)")
}
```

### Step 2: Run

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestIntegration_MultiCaseFile_IncrementalCaching -v ./internal/testsuite/`

Expected: PASS.

### Step 3: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/runner_integration_test.go
git commit -m "test: add incremental caching test for multi-case files

Verifies per-case cache decisions: passing cases are skipped individually,
not as a group based on file-level hash."
```

---

## Task 6: Retry-failed test

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/runner_integration_test.go`

### Step 1: Write the test

```go
// TestIntegration_MultiCaseFile_RetryFailed verifies that --retry-failed with
// a multi-case file re-runs only the individually failed cases.
func TestIntegration_MultiCaseFile_RetryFailed(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"multi.yaml": multiCaseYAML,
	})

	ctx := context.Background()

	// Run 1: deny mock → mc-001 pass, mc-002 fail, mc-003 pass
	runner1, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	_, err = runner1.Run(ctx)
	require.NoError(t, err)

	// Run 2: retry-failed with allow mock — only mc-002 should re-run
	mock2 := newAllowMock()
	runner2, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		RetryFailed:           true,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(mock2),
	})
	require.NoError(t, err)
	result2, err := runner2.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, result2.CachedCount, "mc-001 and mc-003 should be cached (passed)")
	assert.Equal(t, 3, result2.Passed, "all 3 should now show as passed")
	assert.Equal(t, 0, result2.Failed, "no failures after retry with allow mock")
	assert.Len(t, mock2.GetRecordedRequests(), 1, "only mc-002 should be re-executed")
}
```

### Step 2: Run

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestIntegration_MultiCaseFile_RetryFailed -v ./internal/testsuite/`

Expected: PASS.

### Step 3: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/runner_integration_test.go
git commit -m "test: add retry-failed test for multi-case files

Verifies that only individually failed cases are re-run, not the
entire file."
```

---

## Task 7: Force mode test

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/runner_integration_test.go`

### Step 1: Write the test

```go
// TestIntegration_MultiCaseFile_ForceMode verifies that --full bypasses per-case
// cache and re-runs all cases even if they previously passed.
func TestIntegration_MultiCaseFile_ForceMode(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"multi.yaml": multiCaseYAML,
	})

	ctx := context.Background()

	// Run 1: populate state
	runner1, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	_, err = runner1.Run(ctx)
	require.NoError(t, err)

	// Run 2: force mode — all 3 cases should re-execute
	mock2 := newDenyMock()
	runner2, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Force:                 true,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(mock2),
	})
	require.NoError(t, err)
	result2, err := runner2.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, result2.CachedCount, "force mode should not use cache")
	assert.Equal(t, 3, result2.TotalCases, "all 3 cases should run")
	assert.Len(t, mock2.GetRecordedRequests(), 3, "AI provider should be called 3 times")
}
```

### Step 2: Run

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestIntegration_MultiCaseFile_ForceMode -v ./internal/testsuite/`

Expected: PASS.

### Step 3: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/runner_integration_test.go
git commit -m "test: add force mode test for multi-case files

Verifies --full bypasses per-case cache and re-runs everything."
```

---

## Task 8: Edit-one-case-preserves-siblings test

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/runner_integration_test.go`

### Step 1: Write the test

```go
// TestIntegration_MultiCaseFile_EditOneCasePreservesSiblings verifies that
// modifying one test case in a multi-case file only invalidates that case's
// cache, leaving siblings cached.
func TestIntegration_MultiCaseFile_EditOneCasePreservesSiblings(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"multi.yaml": multiCaseYAML,
	})

	ctx := context.Background()

	// Run 1: populate state with all 3 cases
	runner1, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	_, err = runner1.Run(ctx)
	require.NoError(t, err)

	// Modify mc-002 only (change tool_name) — siblings mc-001 and mc-003 unchanged
	modifiedMultiCase := `- case_id: mc-001
  title: "Multi-case deny 1"
  engine: ai
  phase: request
  request:
    tool_name: "test__dangerous_action"
    arguments:
      command: "delete-all-data"
  expectations:
    decision: deny
    policies:
      - policy_name: "test-policy"
        decision: deny

- case_id: mc-002
  title: "Multi-case allow"
  engine: ai
  phase: request
  request:
    tool_name: "test__modified_action"
    arguments:
      path: "/tmp/different.txt"
  expectations:
    decision: allow
    policies:
      - policy_name: "test-policy"
        decision: allow

- case_id: mc-003
  title: "Multi-case deny 2"
  engine: ai
  phase: request
  request:
    tool_name: "test__another_dangerous"
    arguments:
      command: "drop-database"
  expectations:
    decision: deny
    policies:
      - policy_name: "test-policy"
        decision: deny
`
	// Overwrite the file with modified mc-002
	casesDir := filepath.Join(dir, "cases")
	require.NoError(t, os.WriteFile(filepath.Join(casesDir, "multi.yaml"), []byte(modifiedMultiCase), 0644))

	// Run 2: incremental — mc-001 and mc-003 should be cached, mc-002 should re-run
	mock2 := newAllowMock()
	runner2, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(mock2),
	})
	require.NoError(t, err)
	result2, err := runner2.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, result2.CachedCount, "mc-001 and mc-003 should still be cached")
	assert.Len(t, mock2.GetRecordedRequests(), 1, "only modified mc-002 should re-run")
}
```

### Step 2: Run

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestIntegration_MultiCaseFile_EditOneCasePreservesSiblings -v ./internal/testsuite/`

Expected: PASS.

### Step 3: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/runner_integration_test.go
git commit -m "test: add edit-one-case-preserves-siblings test

Verifies Approach B's key advantage: modifying one case in a multi-case
file only invalidates that case's cache, not the entire file."
```

---

## Task 9: Three-model CI pattern end-to-end test

**Files:**
- Modify: `.worktrees/degroff/matrix_state_fix/internal/testsuite/runner_integration_test.go`

### Step 1: Write the test

```go
// TestIntegration_MultiCaseFile_ThreeModelCIPattern simulates the real CI workflow:
// three sequential single-model runs, then a fourth run that should show all three
// models in the comparison with correct per-case counts. This is the exact scenario
// that was broken before the per-case hashing fix.
func TestIntegration_MultiCaseFile_ThreeModelCIPattern(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuiteWithYAML(t, dir, threeModelSuiteYAML, testPolicy, map[string]string{
		"multi.yaml": multiCaseYAML,
	})

	ctx := context.Background()
	models := []string{"openai:test-model-a", "openai:test-model-b", "openai:test-model-c"}

	// Three sequential single-model runs (simulating separate CI dispatches)
	for _, model := range models {
		runner, err := NewRunner(RunnerOptions{
			SuiteDir:              dir,
			Engine:                "ai",
			Model:                 model,
			StateFile:             stateFile,
			Quiet:                 true,
			ProviderClientFactory: mockProviderFactory(newDenyMock()),
		})
		require.NoError(t, err)
		result, err := runner.Run(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, result.TotalCases, "%s should run 3 cases", model)
	}

	// Verify state has all 3 models with 3 entries each
	sm, err := NewStateManager(stateFile, "integration-test", "dev", 0)
	require.NoError(t, err)
	policyHashes := readPolicyHashes(t, dir)
	summaries := sm.GetModelSummaries(policyHashes)
	assert.Len(t, summaries, 3, "state should have all 3 models")

	for _, model := range models {
		require.Contains(t, summaries, model)
		assert.Equal(t, 3, summaries[model].TestCount, "%s should have 3 test entries", model)
		assert.Equal(t, 2, summaries[model].Passed, "%s should have 2 passed", model)
		assert.Equal(t, 1, summaries[model].Failed, "%s should have 1 failed", model)
	}

	// Run 4: model A again (incremental) with JSON output
	outputFile := filepath.Join(dir, "results.json")
	mock4 := newDenyMock()
	runner4, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		OutputFormat:          "json",
		OutputFile:            outputFile,
		ProviderClientFactory: mockProviderFactory(mock4),
	})
	require.NoError(t, err)
	result4, err := runner4.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, result4.CachedCount, "all 3 cases should be cached for model A")
	assert.Len(t, mock4.GetRecordedRequests(), 0, "no AI calls needed for fully cached run")

	// Verify JSON model_comparison shows all 3 models with correct counts
	output := parseJSONOutput(t, outputFile)
	assert.Len(t, output.ModelComparison, 3, "model_comparison should have all 3 models")

	byName := modelComparisonByName(output.ModelComparison)

	// Model A is current run (cached but counted as current)
	require.Contains(t, byName, "openai:test-model-a")
	assert.Equal(t, 2, byName["openai:test-model-a"].Passed, "model A should show 2 passed")
	assert.Equal(t, 1, byName["openai:test-model-a"].Failed, "model A should show 1 failed")

	// Models B and C are from cache — this is the exact assertion that was failing before the fix
	for _, model := range []string{"openai:test-model-b", "openai:test-model-c"} {
		require.Contains(t, byName, model)
		assert.True(t, byName[model].FromCache, "%s should be from cache", model)
		assert.Equal(t, 2, byName[model].Passed, "%s should show 2 passed (not 1)", model)
		assert.Equal(t, 1, byName[model].Failed, "%s should show 1 failed", model)
	}
}
```

### Step 2: Run

Run: `cd .worktrees/degroff/matrix_state_fix && go test -run TestIntegration_MultiCaseFile_ThreeModelCIPattern -v ./internal/testsuite/`

Expected: PASS.

### Step 3: Commit

```bash
cd .worktrees/degroff/matrix_state_fix
git add internal/testsuite/runner_integration_test.go
git commit -m "test: add three-model CI pattern test for multi-case files

Simulates the exact CI workflow that was broken: three sequential
single-model runs, then a fourth run that verifies all three models
appear in the comparison with correct per-case counts."
```

---

## Task 10: Final verification

### Step 1: Run full test suite

Run: `cd .worktrees/degroff/matrix_state_fix && go test ./internal/testsuite/... -v -count=1`

Expected: ALL tests pass (both new and existing).

### Step 2: Run linter

Run: `cd .worktrees/degroff/matrix_state_fix && make lint`

Expected: No lint errors.

### Step 3: Run full project tests

Run: `cd .worktrees/degroff/matrix_state_fix && make test`

Expected: All project tests pass.
