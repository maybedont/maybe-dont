package testsuite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/maybedont/maybe-dont/internal/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests for the full Runner.Run() → state accumulation → model summary pipeline.
// These tests use temp directories with minimal suite fixtures and mock AI providers
// to verify end-to-end behavior without making real API calls.

// --- Test helpers ---

// setupTestSuite creates a minimal single-model test suite directory.
func setupTestSuite(t *testing.T, dir string, policyContent string, testCases map[string]string) {
	t.Helper()
	setupTestSuiteWithYAML(t, dir, singleModelSuiteYAML, policyContent, testCases)
}

// setupTestSuiteWithYAML creates a test suite directory with custom suite.yaml content.
func setupTestSuiteWithYAML(t *testing.T, dir string, suiteYAML string, policyContent string, testCases map[string]string) {
	t.Helper()

	casesDir := filepath.Join(dir, "cases")
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	policyPath := filepath.Join(dir, "ai_request_rules.yaml")
	require.NoError(t, os.WriteFile(policyPath, []byte(policyContent), 0644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.yaml"), []byte(suiteYAML), 0644))

	for name, content := range testCases {
		require.NoError(t, os.WriteFile(filepath.Join(casesDir, name), []byte(content), 0644))
	}
}

// mockProviderFactory returns a ProviderClientFactory that always returns the given mock.
func mockProviderFactory(mock *gateway.MockAIProviderClient) func(ModelConfig) (gateway.AIProviderClient, error) {
	return func(_ ModelConfig) (gateway.AIProviderClient, error) {
		return mock, nil
	}
}

// perModelFactory returns a ProviderClientFactory that creates a separate mock per model,
// using createMock to build each one. The returned map can be inspected to verify
// which models were called.
func perModelFactory(createMock func(model string) *gateway.MockAIProviderClient) (func(ModelConfig) (gateway.AIProviderClient, error), map[string]*gateway.MockAIProviderClient) {
	mocks := make(map[string]*gateway.MockAIProviderClient)
	factory := func(m ModelConfig) (gateway.AIProviderClient, error) {
		key := ModelKey(m.Provider, m.Model)
		mock := createMock(m.Model)
		mocks[key] = mock
		return mock, nil
	}
	return factory, mocks
}

func newDenyMock() *gateway.MockAIProviderClient {
	mock := gateway.NewMockAIProviderClient()
	mock.SetResponse(gateway.AICompletionResult{
		RawText:    `{"allowed":false,"message":"Test denied"}`,
		ParsedJSON: json.RawMessage(`{"allowed":false,"message":"Test denied"}`),
	})
	return mock
}

func newAllowMock() *gateway.MockAIProviderClient {
	mock := gateway.NewMockAIProviderClient()
	mock.SetResponse(gateway.AICompletionResult{
		RawText:    `{"allowed":true,"message":"Test allowed"}`,
		ParsedJSON: json.RawMessage(`{"allowed":true,"message":"Test allowed"}`),
	})
	return mock
}

// readPolicyHashes computes policy hashes the same way the runner does.
func readPolicyHashes(t *testing.T, dir string) []string {
	t.Helper()
	policyData, err := os.ReadFile(filepath.Join(dir, "ai_request_rules.yaml"))
	require.NoError(t, err)
	return []string{ComputePolicyHash(policyData)}
}

// parseJSONOutput reads and parses a JSON output file.
func parseJSONOutput(t *testing.T, path string) JSONOutput {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var output JSONOutput
	require.NoError(t, json.Unmarshal(data, &output))
	return output
}

// modelComparisonByName indexes model_comparison entries by model key.
func modelComparisonByName(entries []JSONModelComparisonEntry) map[string]JSONModelComparisonEntry {
	m := make(map[string]JSONModelComparisonEntry, len(entries))
	for _, e := range entries {
		m[e.Model] = e
	}
	return m
}

// --- Suite YAML templates ---

const singleModelSuiteYAML = `version: "v1"
bundle_id: integration-test
description: "Integration test suite"

providers:
  openai:
    api_key: "test-key"

policies:
  ai_request_rules: "./ai_request_rules.yaml"

acceptance:
  min_match_rate: 0.0

execution:
  timeout_ms: 5000
  retries: 0

engines:
  cel:
    enabled: false
  ai:
    enabled: true
    model_matrix:
      - provider: openai
        model: test-model-a
        api_key: "test-key"
`

const twoModelSuiteYAML = `version: "v1"
bundle_id: integration-test
description: "Integration test suite"

providers:
  openai:
    api_key: "test-key"

policies:
  ai_request_rules: "./ai_request_rules.yaml"

acceptance:
  min_match_rate: 0.0

execution:
  timeout_ms: 5000
  retries: 0

engines:
  cel:
    enabled: false
  ai:
    enabled: true
    model_matrix:
      - provider: openai
        model: test-model-a
        api_key: "test-key"
      - provider: openai
        model: test-model-b
        api_key: "test-key"
`

const threeModelSuiteYAML = `version: "v1"
bundle_id: integration-test
description: "Integration test suite"

providers:
  openai:
    api_key: "test-key"

policies:
  ai_request_rules: "./ai_request_rules.yaml"

acceptance:
  min_match_rate: 0.0

execution:
  timeout_ms: 5000
  retries: 0

engines:
  cel:
    enabled: false
  ai:
    enabled: true
    model_matrix:
      - provider: openai
        model: test-model-a
        api_key: "test-key"
      - provider: openai
        model: test-model-b
        api_key: "test-key"
      - provider: openai
        model: test-model-c
        api_key: "test-key"
`

// --- Test fixtures ---

const testPolicy = `rules:
  - name: "test-policy"
    prompt: "Is this request safe?"
    action: deny
`

const testCaseDeny = `case_id: tc-001
title: "Test deny case"
tags: [ai, request]
phase: request
engine: ai
request:
  tool_name: "test__dangerous_action"
  arguments:
    path: "/etc/shadow"
expectations:
  decision: deny
  policies:
    - policy_name: "test-policy"
      decision: deny
`

const testCaseAllow = `case_id: tc-002
title: "Test allow case"
tags: [ai, request]
phase: request
engine: ai
request:
  tool_name: "test__safe_action"
  arguments:
    path: "/tmp/readme.txt"
expectations:
  decision: allow
  policies:
    - policy_name: "test-policy"
      decision: allow
`

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

	// Run 2: same model, incremental — all 3 cached (2 passed + 1 failed)
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
	assert.Equal(t, 3, result2.CachedCount, "all 3 cases should be cached")
	assert.Equal(t, 2, result2.Passed, "2 cases should show as passed (cached)")
	assert.Equal(t, 1, result2.Failed, "1 case should still show as failed (cached)")
	assert.Empty(t, mock2.GetRecordedRequests(), "no tests should re-run in default incremental (all cached)")
}

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

// --- Original tests (state accumulation, historical models, cache invalidation) ---

// TestIntegration_StateAccumulation_AcrossModels verifies that running with model A,
// saving state, then running with model B results in state containing both models.
func TestIntegration_StateAccumulation_AcrossModels(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	ctx := context.Background()

	// Run 1: model A (deny mock → policy triggers → test passes since expectation is deny)
	runner1, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	runResult1, err := runner1.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, runResult1.TotalCases)
	assert.Equal(t, 1, runResult1.Passed)

	// Run 2: model B
	runner2, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-b",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	runResult2, err := runner2.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, runResult2.TotalCases)
	assert.Equal(t, 1, runResult2.Passed)

	// Verify state file contains both models
	sm, err := NewStateManager(stateFile, "integration-test", "dev", 0)
	require.NoError(t, err)

	policyHashes := readPolicyHashes(t, dir)
	summaries := sm.GetModelSummaries(policyHashes)
	assert.Contains(t, summaries, "openai:test-model-a", "state should contain model A results")
	assert.Contains(t, summaries, "openai:test-model-b", "state should contain model B results")
	assert.Equal(t, 1, summaries["openai:test-model-a"].Passed)
	assert.Equal(t, 1, summaries["openai:test-model-b"].Passed)
}

// TestIntegration_StateAccumulation_SameModel verifies that running the same model
// twice with unchanged test cases results in all tests being skipped (cached) on
// the second run.
func TestIntegration_StateAccumulation_SameModel(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	ctx := context.Background()

	// Run 1: execute tests
	runner1, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	runResult1, err := runner1.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, runResult1.Passed)
	assert.Equal(t, 0, runResult1.Skipped)

	// Run 2: same model, incremental — all tests should be cached
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
	runResult2, err := runner2.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, runResult2.CachedCount, "all tests should come from cache on second run")
	assert.Empty(t, mock2.GetRecordedRequests(), "cached run should not call AI provider")
}

// TestIntegration_ModelSummaryIncludesHistoricalModels verifies that after multi-model
// accumulation, model_comparison includes both the current and historical models.
func TestIntegration_ModelSummaryIncludesHistoricalModels(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
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
	_, err = runner1.Run(ctx)
	require.NoError(t, err)

	// Run 2: model B — model A results are only in state
	outputFile := filepath.Join(dir, "results.json")
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
	_, err = runner2.Run(ctx)
	require.NoError(t, err)

	output := parseJSONOutput(t, outputFile)

	assert.GreaterOrEqual(t, len(output.ModelComparison), 2,
		"model_comparison should include both current and historical models")

	byName := modelComparisonByName(output.ModelComparison)
	assert.Contains(t, byName, "openai:test-model-a", "should include historical model A")
	assert.Contains(t, byName, "openai:test-model-b", "should include current model B")
	assert.True(t, byName["openai:test-model-a"].FromCache, "historical model A should be from_cache")
	assert.False(t, byName["openai:test-model-b"].FromCache, "current model B should not be from_cache")
}

// TestIntegration_JSONOutputIncludesHistoricalModels verifies that results_by_model
// has only the current model while model_comparison includes historical ones.
func TestIntegration_JSONOutputIncludesHistoricalModels(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"deny.yaml":  testCaseDeny,
		"allow.yaml": testCaseAllow,
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
	_, err = runner1.Run(ctx)
	require.NoError(t, err)

	// Run 2: model B with JSON output
	outputFile := filepath.Join(dir, "results.json")
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
	_, err = runner2.Run(ctx)
	require.NoError(t, err)

	output := parseJSONOutput(t, outputFile)

	assert.Len(t, output.ResultsByModel, 1, "results_by_model should have only the current model")
	assert.Equal(t, "test-model-b", output.ResultsByModel[0].Model.Model)
	assert.GreaterOrEqual(t, len(output.ModelComparison), 2,
		"model_comparison should include historical models from state")
}

// TestIntegration_PolicyHashChange_InvalidatesCache verifies that modifying a policy
// file causes all tests to re-run on the next invocation.
func TestIntegration_PolicyHashChange_InvalidatesCache(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	ctx := context.Background()

	// Run 1: execute tests
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

	// Modify the policy file — changes the hash
	modifiedPolicy := `rules:
  - name: "test-policy"
    prompt: "Is this request REALLY safe? Check carefully."
    action: deny
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ai_request_rules.yaml"), []byte(modifiedPolicy), 0644))

	// Run 2: policy hash changed → no cache hit
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
	runResult2, err := runner2.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, runResult2.CachedCount, "no tests should be cached after policy change")
	assert.Equal(t, 1, runResult2.Passed, "test should pass after re-run")
	assert.NotEmpty(t, mock2.GetRecordedRequests(), "AI provider should have been called")
}

// --- New tests: matrix mode, mixed results, JSON contract, force, retry-failed, three-run ---

// TestIntegration_MatrixMode_BothModelsInSingleRun verifies that --matrix runs both
// models in a single invocation, with both appearing in results_by_model and
// model_comparison. This is the primary CI path.
func TestIntegration_MatrixMode_BothModelsInSingleRun(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	outputFile := filepath.Join(dir, "results.json")

	setupTestSuiteWithYAML(t, dir, twoModelSuiteYAML, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	ctx := context.Background()

	runner, err := NewRunner(RunnerOptions{
		SuiteDir:  dir,
		Engine:    "ai",
		RunMatrix: true,
		StateFile: stateFile,
		Quiet:     true,
		OutputFormat: "json",
		OutputFile:   outputFile,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	result, err := runner.Run(ctx)
	require.NoError(t, err)

	// Both models ran, each with 1 test case
	assert.Equal(t, 2, result.TotalCases, "matrix mode should run each test against each model")
	assert.Equal(t, 2, result.Passed)

	output := parseJSONOutput(t, outputFile)

	// results_by_model should have entries for both models (both ran in this invocation)
	assert.Len(t, output.ResultsByModel, 2, "results_by_model should have both models")
	resultModels := make(map[string]bool)
	for _, r := range output.ResultsByModel {
		resultModels[r.Model.Model] = true
	}
	assert.True(t, resultModels["test-model-a"], "results_by_model should include model A")
	assert.True(t, resultModels["test-model-b"], "results_by_model should include model B")

	// model_comparison should also have both (from current run, not cache)
	assert.Len(t, output.ModelComparison, 2, "model_comparison should have both models")
	byName := modelComparisonByName(output.ModelComparison)
	assert.False(t, byName["openai:test-model-a"].FromCache, "model A ran in this invocation")
	assert.False(t, byName["openai:test-model-b"].FromCache, "model B ran in this invocation")

	// State should contain both models
	sm, err := NewStateManager(stateFile, "integration-test", "dev", 0)
	require.NoError(t, err)
	summaries := sm.GetModelSummaries(readPolicyHashes(t, dir))
	assert.Len(t, summaries, 2)
	assert.Equal(t, 1, summaries["openai:test-model-a"].Passed)
	assert.Equal(t, 1, summaries["openai:test-model-b"].Passed)
}

// TestIntegration_MatrixMode_MixedPassFail verifies correct per-model stats when
// model A passes a test but model B fails the same test.
func TestIntegration_MatrixMode_MixedPassFail(t *testing.T) {
	dir := t.TempDir()
	outputFile := filepath.Join(dir, "results.json")

	setupTestSuiteWithYAML(t, dir, twoModelSuiteYAML, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	// Model A: deny mock → test expects deny → pass
	// Model B: allow mock → test expects deny → fail
	factory := func(m ModelConfig) (gateway.AIProviderClient, error) {
		if m.Model == "test-model-a" {
			return newDenyMock(), nil
		}
		return newAllowMock(), nil
	}

	ctx := context.Background()
	runner, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		RunMatrix:             true,
		Quiet:                 true,
		OutputFormat:          "json",
		OutputFile:            outputFile,
		ProviderClientFactory: factory,
	})
	require.NoError(t, err)
	result, err := runner.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, result.TotalCases)
	assert.Equal(t, 1, result.Passed, "model A should pass")
	assert.Equal(t, 1, result.Failed, "model B should fail")

	output := parseJSONOutput(t, outputFile)
	require.Len(t, output.ResultsByModel, 2)

	// Find each model's summary and verify independent stats
	for _, mr := range output.ResultsByModel {
		switch mr.Model.Model {
		case "test-model-a":
			assert.Equal(t, 1, mr.Summary.Passed, "model A: 1 passed")
			assert.Equal(t, 0, mr.Summary.Failed, "model A: 0 failed")
			assert.Equal(t, 1.0, mr.Summary.MatchRate, "model A: 100% match rate")
		case "test-model-b":
			assert.Equal(t, 0, mr.Summary.Passed, "model B: 0 passed")
			assert.Equal(t, 1, mr.Summary.Failed, "model B: 1 failed")
			assert.Equal(t, 0.0, mr.Summary.MatchRate, "model B: 0% match rate")
		default:
			t.Errorf("unexpected model: %s", mr.Model.Model)
		}
	}

	// model_comparison should reflect the same per-model divergence
	byName := modelComparisonByName(output.ModelComparison)
	assert.Equal(t, 1.0, byName["openai:test-model-a"].MatchRate)
	assert.Equal(t, 0.0, byName["openai:test-model-b"].MatchRate)

	// Overall summary should show the worst match rate
	assert.Equal(t, 0.0, output.OverallSummary.WorstMatchRate,
		"worst match rate should reflect the failing model")
}

// TestIntegration_JSONStructureMatchesCIContract verifies that the JSON output contains
// every field and nesting path that the CI summary job's jq queries depend on.
// If a refactor changes a field name or nesting, this test breaks before the CI does.
func TestIntegration_JSONStructureMatchesCIContract(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	outputFile := filepath.Join(dir, "results.json")

	setupTestSuiteWithYAML(t, dir, twoModelSuiteYAML, testPolicy, map[string]string{
		"deny.yaml":  testCaseDeny,
		"allow.yaml": testCaseAllow,
	})

	ctx := context.Background()

	// Run 1: model A only (to create historical state)
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

	// Run 2: model B with JSON output (model A is now historical)
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
	_, err = runner2.Run(ctx)
	require.NoError(t, err)

	// Parse as raw JSON to verify exact field presence (catches renames, not just Go struct changes)
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	// --- Top-level fields the CI reads ---
	assert.Contains(t, raw, "overall_summary", "CI reads .overall_summary")
	assert.Contains(t, raw, "results_by_model", "CI reads .results_by_model")
	assert.Contains(t, raw, "model_comparison", "CI reads .model_comparison")

	// --- overall_summary fields (used in Results by Engine table and Thresholds section) ---
	output := parseJSONOutput(t, outputFile)
	// These are read by: jq '.overall_summary.total_cases', etc.
	assert.Greater(t, output.OverallSummary.TotalCases, 0, ".overall_summary.total_cases")
	// Passed/failed/errored are ints (even if 0)
	_ = output.OverallSummary.Passed
	_ = output.OverallSummary.Failed
	_ = output.OverallSummary.Errored
	// CI reads: jq '.overall_summary.match_rate * 100'
	assert.GreaterOrEqual(t, output.OverallSummary.MatchRate, 0.0)
	assert.LessOrEqual(t, output.OverallSummary.MatchRate, 1.0)
	// CI reads: jq '.overall_summary.thresholds_met'
	_ = output.OverallSummary.ThresholdsMet
	// CI reads: jq '.overall_summary.min_match_rate_required * 100'
	assert.GreaterOrEqual(t, output.OverallSummary.MinMatchRateRequired, 0.0)
	// CI reads: jq '.overall_summary.worst_match_rate * 100'
	assert.GreaterOrEqual(t, output.OverallSummary.WorstMatchRate, 0.0)

	// --- results_by_model[] fields (used in Model Comparison fallback and Slowest Policies) ---
	require.NotEmpty(t, output.ResultsByModel, "results_by_model should not be empty")
	for _, mr := range output.ResultsByModel {
		// CI reads: .model.provider, .model.model
		assert.NotEmpty(t, mr.Model.Provider, ".results_by_model[].model.provider")
		assert.NotEmpty(t, mr.Model.Model, ".results_by_model[].model.model")
		// CI reads: .summary.passed, .summary.failed, .summary.errored, .summary.match_rate
		_ = mr.Summary.Passed
		_ = mr.Summary.Failed
		_ = mr.Summary.Errored
		assert.GreaterOrEqual(t, mr.Summary.MatchRate, 0.0)
		// CI reads: .summary.total_elapsed_ms, .summary.total_cases
		_ = mr.Summary.TotalElapsedMs
		assert.Greater(t, mr.Summary.TotalCases, 0, ".summary.total_cases should be > 0")

		// CI reads: .results[].actual.policies_executed[].policy_name, .elapsed_ms
		for _, tr := range mr.Results {
			assert.NotEmpty(t, tr.CaseID, ".results[].case_id")
			assert.NotEmpty(t, tr.Status, ".results[].status")
			if tr.Actual != nil {
				for _, pe := range tr.Actual.PoliciesExecuted {
					assert.NotEmpty(t, pe.PolicyName, ".policies_executed[].policy_name")
				}
			}
		}
	}

	// --- model_comparison[] fields (used in the Model Comparison table) ---
	require.GreaterOrEqual(t, len(output.ModelComparison), 2,
		"model_comparison should include current + historical models")
	for _, mc := range output.ModelComparison {
		// CI reads: .model, .passed, .failed, .errored, .match_rate, .avg_ms, .total_ms, .from_cache
		assert.NotEmpty(t, mc.Model, ".model_comparison[].model")
		assert.GreaterOrEqual(t, mc.MatchRate, 0.0, ".model_comparison[].match_rate")
		// from_cache is a bool — just verify it exists (it's always serialized)
	}

	// Verify the historical model has from_cache=true
	byName := modelComparisonByName(output.ModelComparison)
	assert.True(t, byName["openai:test-model-a"].FromCache,
		"historical model should have from_cache=true for CI 'Source' column")
	assert.False(t, byName["openai:test-model-b"].FromCache,
		"current model should have from_cache=false")
}

// TestIntegration_ForceMode_BypassesCache verifies that --full re-runs all tests
// even when the state file has valid cached results.
func TestIntegration_ForceMode_BypassesCache(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	ctx := context.Background()

	// Run 1: populate cache
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
	_, err = runner1.Run(ctx)
	require.NoError(t, err)
	run1Calls := len(mock1.GetRecordedRequests())
	assert.Greater(t, run1Calls, 0, "run 1 should call the AI provider")

	// Run 2: incremental (should use cache, no AI calls)
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
	assert.Equal(t, 1, result2.CachedCount, "run 2 incremental should use cache")
	assert.Empty(t, mock2.GetRecordedRequests(), "run 2 incremental should not call AI provider")

	// Run 3: force mode (should re-run everything despite cache)
	mock3 := newDenyMock()
	runner3, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		StateFile:             stateFile,
		Force:                 true,
		Quiet:                 true,
		ProviderClientFactory: mockProviderFactory(mock3),
	})
	require.NoError(t, err)
	result3, err := runner3.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result3.CachedCount, "force mode should not use cache")
	assert.Equal(t, 1, result3.Passed)
	assert.NotEmpty(t, mock3.GetRecordedRequests(), "force mode should call AI provider")
}

// TestIntegration_RetryFailed_RerunsFailed_SkipsPassing verifies that --retry-failed
// re-executes previously failed tests while keeping passing tests cached.
func TestIntegration_RetryFailed_RerunsFailed_SkipsPassing(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"deny.yaml":  testCaseDeny,
		"allow.yaml": testCaseAllow,
	})

	ctx := context.Background()

	// Run 1: deny mock.
	//   tc-001 expects deny → pass
	//   tc-002 expects allow → fail (got deny, expected allow)
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
	assert.Equal(t, 1, result1.Passed, "run 1: tc-001 should pass")
	assert.Equal(t, 1, result1.Failed, "run 1: tc-002 should fail")

	// Run 2: retry-failed with allow mock.
	//   tc-001 was passed → stays cached (not re-run)
	//   tc-002 was failed → re-runs with allow mock → passes
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

	// tc-001 cached as passed (counts as passed via effective status)
	// tc-002 re-ran and now passes
	assert.Equal(t, 1, result2.CachedCount, "tc-001 should come from cache")
	assert.Equal(t, 2, result2.Passed, "both tests should now show as passed")
	assert.Equal(t, 0, result2.Failed, "no tests should be failed after retry")

	// Verify the mock was called exactly once (only tc-002 re-ran, not tc-001)
	requests := mock2.GetRecordedRequests()
	assert.Len(t, requests, 1, "only the failed test should be re-executed")
}

// TestIntegration_ThreeRunAccumulation mirrors the real CI pattern: three separate
// workflow runs each with a different model, reading/writing the same state file.
// After all three, the state and JSON output should reflect all three models.
func TestIntegration_ThreeRunAccumulation(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuiteWithYAML(t, dir, threeModelSuiteYAML, testPolicy, map[string]string{
		"deny.yaml":  testCaseDeny,
		"allow.yaml": testCaseAllow,
	})

	ctx := context.Background()
	models := []string{"openai:test-model-a", "openai:test-model-b", "openai:test-model-c"}

	// Three sequential runs, one model each (simulating separate CI dispatches)
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
		_, err = runner.Run(ctx)
		require.NoError(t, err)
	}

	// Verify state has all 3 models
	sm, err := NewStateManager(stateFile, "integration-test", "dev", 0)
	require.NoError(t, err)
	summaries := sm.GetModelSummaries(readPolicyHashes(t, dir))
	assert.Len(t, summaries, 3, "state should have all 3 models")
	for _, model := range models {
		assert.Contains(t, summaries, model, "state should contain %s", model)
	}

	// Run 4: model C again with JSON output — model_comparison should show all 3
	outputFile := filepath.Join(dir, "results.json")
	runner4, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-c",
		StateFile:             stateFile,
		Quiet:                 true,
		OutputFormat:          "json",
		OutputFile:            outputFile,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	_, err = runner4.Run(ctx)
	require.NoError(t, err)

	output := parseJSONOutput(t, outputFile)

	// results_by_model has only model C (current run)
	assert.Len(t, output.ResultsByModel, 1)
	assert.Equal(t, "test-model-c", output.ResultsByModel[0].Model.Model)

	// model_comparison has all 3 models
	assert.Len(t, output.ModelComparison, 3, "model_comparison should have all 3 models")
	byName := modelComparisonByName(output.ModelComparison)
	assert.True(t, byName["openai:test-model-a"].FromCache, "model A is historical")
	assert.True(t, byName["openai:test-model-b"].FromCache, "model B is historical")
	assert.False(t, byName["openai:test-model-c"].FromCache, "model C is current")
}

// TestIntegration_MatrixMode_IncrementalSecondRun verifies that a second matrix run
// uses cached results from the first run. This catches issues where matrix mode
// interacts badly with state persistence.
func TestIntegration_MatrixMode_IncrementalSecondRun(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	setupTestSuiteWithYAML(t, dir, twoModelSuiteYAML, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	ctx := context.Background()

	// Run 1: matrix mode — both models execute
	factory1, mocks1 := perModelFactory(func(_ string) *gateway.MockAIProviderClient {
		return newDenyMock()
	})
	runner1, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		RunMatrix:             true,
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: factory1,
	})
	require.NoError(t, err)
	result1, err := runner1.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, result1.Passed)
	assert.Equal(t, 0, result1.CachedCount)
	// Both models should have been called
	assert.NotEmpty(t, mocks1["openai:test-model-a"].GetRecordedRequests(), "model A should be called in run 1")
	assert.NotEmpty(t, mocks1["openai:test-model-b"].GetRecordedRequests(), "model B should be called in run 1")

	// Run 2: matrix mode again — both models should be cached
	factory2, mocks2 := perModelFactory(func(_ string) *gateway.MockAIProviderClient {
		return newDenyMock()
	})
	runner2, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		RunMatrix:             true,
		StateFile:             stateFile,
		Quiet:                 true,
		ProviderClientFactory: factory2,
	})
	require.NoError(t, err)
	result2, err := runner2.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, result2.CachedCount, "both models should use cache on second run")
	// Neither model should have been called
	for key, mock := range mocks2 {
		assert.Empty(t, mock.GetRecordedRequests(), "%s should not be called in run 2", key)
	}
}

// --- Edge case tests ---

// TestIntegration_EmptyTestSuite verifies that a suite with a valid cases/ directory
// but no YAML files produces valid output with zero test cases and no errors.
func TestIntegration_EmptyTestSuite(t *testing.T) {
	dir := t.TempDir()
	outputFile := filepath.Join(dir, "results.json")

	// Create suite with empty cases directory (no test case YAML files)
	setupTestSuite(t, dir, testPolicy, map[string]string{})

	ctx := context.Background()

	runner, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:test-model-a",
		Quiet:                 true,
		OutputFormat:          "json",
		OutputFile:            outputFile,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	result, err := runner.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, result.TotalCases, "empty suite should have zero test cases")
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 0, result.Failed)
	assert.True(t, result.ThresholdsMet, "zero test cases should vacuously meet thresholds")

	// JSON output should be valid and parseable with empty arrays
	output := parseJSONOutput(t, outputFile)
	assert.Equal(t, 0, output.OverallSummary.TotalCases)
	assert.Empty(t, output.ResultsByModel, "results_by_model should be empty or have an entry with no results")
}

// TestIntegration_FirstRun_CreatesStateFile verifies that the first run with a
// state file path creates the file on disk, even when starting from nothing.
func TestIntegration_FirstRun_CreatesStateFile(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "brand-new-state.json")

	setupTestSuite(t, dir, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	// State file should not exist before the run
	_, err := os.Stat(stateFile)
	require.True(t, os.IsNotExist(err), "state file should not exist before first run")

	ctx := context.Background()
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
	assert.Equal(t, 1, result.Passed)

	// State file should now exist and be valid JSON
	info, err := os.Stat(stateFile)
	require.NoError(t, err, "state file should exist after first run")
	assert.Greater(t, info.Size(), int64(0), "state file should not be empty")

	data, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	require.True(t, json.Valid(data), "state file should contain valid JSON")

	// Verify state contains the model's results
	sm, err := NewStateManager(stateFile, "integration-test", "dev", 0)
	require.NoError(t, err)
	summaries := sm.GetModelSummaries(readPolicyHashes(t, dir))
	assert.Contains(t, summaries, "openai:test-model-a", "state should contain the model's results")
}

// TestIntegration_ModelKeyWithColons verifies that model names containing colons
// (e.g., "gpt-4:turbo") round-trip correctly through state persistence and
// model comparison output. The model flag parser uses SplitN(":", 2) so the
// colon in the model name should be preserved.
func TestIntegration_ModelKeyWithColons(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	outputFile := filepath.Join(dir, "results.json")

	// Suite YAML with a model name that contains a colon
	colonModelSuiteYAML := `version: "v1"
bundle_id: integration-test
description: "Integration test suite"

providers:
  openai:
    api_key: "test-key"

policies:
  ai_request_rules: "./ai_request_rules.yaml"

acceptance:
  min_match_rate: 0.0

execution:
  timeout_ms: 5000
  retries: 0

engines:
  cel:
    enabled: false
  ai:
    enabled: true
    model_matrix:
      - provider: openai
        model: "gpt-4:turbo"
        api_key: "test-key"
`
	setupTestSuiteWithYAML(t, dir, colonModelSuiteYAML, testPolicy, map[string]string{
		"deny.yaml": testCaseDeny,
	})

	ctx := context.Background()

	// Run with the colon-containing model name
	runner, err := NewRunner(RunnerOptions{
		SuiteDir:              dir,
		Engine:                "ai",
		Model:                 "openai:gpt-4:turbo",
		StateFile:             stateFile,
		Quiet:                 true,
		OutputFormat:          "json",
		OutputFile:            outputFile,
		ProviderClientFactory: mockProviderFactory(newDenyMock()),
	})
	require.NoError(t, err)
	result, err := runner.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Passed)

	// Verify the model key round-trips through state
	sm, err := NewStateManager(stateFile, "integration-test", "dev", 0)
	require.NoError(t, err)
	summaries := sm.GetModelSummaries(readPolicyHashes(t, dir))
	assert.Contains(t, summaries, "openai:gpt-4:turbo",
		"state should store model key with colons intact")
	assert.Equal(t, 1, summaries["openai:gpt-4:turbo"].Passed)

	// Verify JSON output model_comparison uses the full key
	output := parseJSONOutput(t, outputFile)
	require.NotEmpty(t, output.ModelComparison)
	byName := modelComparisonByName(output.ModelComparison)
	assert.Contains(t, byName, "openai:gpt-4:turbo",
		"model_comparison should use the full model key including colons")
}
