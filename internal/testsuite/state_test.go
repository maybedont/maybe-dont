package testsuite

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStateManager(t *testing.T) {
	t.Run("creates new state without file", func(t *testing.T) {
		sm, err := NewStateManager("", "test-suite", "1.0.0")
		require.NoError(t, err)
		assert.NotNil(t, sm.state)
		assert.Equal(t, "v1", sm.state.SchemaVersion)
		assert.Equal(t, "1.0.0", sm.state.ProductVersion)
		assert.Equal(t, "test-suite", sm.state.SuiteID)
	})

	t.Run("creates new state when file doesn't exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")

		sm, err := NewStateManager(statePath, "test-suite", "1.0.0")
		require.NoError(t, err)
		assert.NotNil(t, sm.state)
		assert.Equal(t, "v1", sm.state.SchemaVersion)
	})

	t.Run("loads existing state file", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")

		// Create initial state
		sm1, err := NewStateManager(statePath, "test-suite", "1.0.0")
		require.NoError(t, err)

		// Record a result
		sm1.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "passed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})
		require.NoError(t, sm1.Save())

		// Load the state in a new manager
		sm2, err := NewStateManager(statePath, "test-suite", "1.0.0")
		require.NoError(t, err)

		// Verify the result was loaded
		assert.True(t, sm2.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", false))
	})
}

func TestStateManager_ShouldSkip(t *testing.T) {
	t.Run("returns false for uncached test", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		assert.False(t, sm.ShouldSkip("sha256:abc123", []string{}, "openai:gpt-4", false))
	})

	t.Run("returns true for cached passing test", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "passed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})

		assert.True(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", false))
	})

	t.Run("returns true for cached failing test by default", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "failed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})

		// By default, all cached results (including failed) are skipped
		assert.True(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", false))
	})

	t.Run("returns false for cached failing test when retryFailed=true", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "failed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})

		// With retryFailed=true, failed tests should be re-run
		assert.False(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", true))
	})

	t.Run("returns true for cached passing test when retryFailed=true", func(t *testing.T) {
		// Passed tests should still be skipped even when retryFailed=true
		// because retryFailed only re-runs failed/errored tests
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "passed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})

		// With retryFailed=true, passed tests should still be skipped
		assert.True(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", true))
	})

	t.Run("returns false for cached errored test when retryFailed=true", func(t *testing.T) {
		// Errored tests (timeouts, API errors) should be re-run when retryFailed=true
		// since they may have failed due to transient issues
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "errored",
			Confidence: 0,
			LastRun:    time.Now(),
			DurationMs: 30000,
		})

		// With retryFailed=true, errored tests should be re-run
		assert.False(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", true))
	})

	t.Run("returns true for cached errored test when retryFailed=false", func(t *testing.T) {
		// By default, all cached results (including errored) are skipped
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "errored",
			Confidence: 0,
			LastRun:    time.Now(),
			DurationMs: 30000,
		})

		// By default, errored tests are skipped (same behavior as failed)
		assert.True(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", false))
	})

	t.Run("returns false when policy hashes changed", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "passed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})

		// Different policy hash should not match
		assert.False(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol2"}, "openai:gpt-4", false))
	})

	t.Run("returns false for different model", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "passed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})

		// Different model should not match
		assert.False(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "anthropic:claude", false))
	})
}

func TestStateManager_RecordResult(t *testing.T) {
	t.Run("creates new entry", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "passed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})

		cached := sm.state.Results["sha256:abc123"]
		require.NotNil(t, cached)
		assert.Equal(t, "test-1", cached.CaseID)
		assert.Equal(t, []string{"sha256:pol1"}, cached.PolicyHashes)
		assert.Equal(t, "passed", cached.Models["openai:gpt-4"].Status)
	})

	t.Run("adds second model to existing entry", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")
		hashes := []string{"sha256:pol1"}

		sm.RecordResult("sha256:abc123", "test-1", hashes, "openai:gpt-4", &CachedResult{Status: "passed"})
		sm.RecordResult("sha256:abc123", "test-1", hashes, "anthropic:claude", &CachedResult{Status: "failed"})

		cached := sm.state.Results["sha256:abc123"]
		require.NotNil(t, cached)
		assert.Len(t, cached.Models, 2)
		assert.Equal(t, "passed", cached.Models["openai:gpt-4"].Status)
		assert.Equal(t, "failed", cached.Models["anthropic:claude"].Status)
	})

	t.Run("overwrites existing model result", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")
		hashes := []string{"sha256:pol1"}

		sm.RecordResult("sha256:abc123", "test-1", hashes, "openai:gpt-4", &CachedResult{
			Status: "failed", DurationMs: 100,
		})
		sm.RecordResult("sha256:abc123", "test-1", hashes, "openai:gpt-4", &CachedResult{
			Status: "passed", DurationMs: 200,
		})

		cached := sm.state.Results["sha256:abc123"]
		require.NotNil(t, cached)
		assert.Equal(t, "passed", cached.Models["openai:gpt-4"].Status)
		assert.Equal(t, int64(200), cached.Models["openai:gpt-4"].DurationMs)
	})

	// Regression test: RecordResult must update PolicyHashes on existing entries.
	// Without this, entries created with nil hashes (e.g., from before computePolicyHashes
	// was added) can never be skipped because ShouldSkip sees hashesMatch(nil, computed)
	// which always fails. The entry gets re-recorded on every run but PolicyHashes stays
	// nil, creating an infinite re-run loop.
	t.Run("updates policy hashes on existing entry", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		// Simulate a legacy entry recorded before policy hashing was implemented
		sm.RecordResult("sha256:abc123", "test-1", nil, "openai:gpt-4", &CachedResult{
			Status: "passed", DurationMs: 100,
		})

		// Verify initial state: nil hashes
		cached := sm.state.Results["sha256:abc123"]
		require.NotNil(t, cached)
		assert.Nil(t, cached.PolicyHashes)

		// Re-record the same test with computed policy hashes (simulates a re-run
		// after computePolicyHashes was added)
		newHashes := []string{"sha256:pol1", "sha256:pol2"}
		sm.RecordResult("sha256:abc123", "test-1", newHashes, "openai:gpt-4", &CachedResult{
			Status: "passed", DurationMs: 120,
		})

		// PolicyHashes must be updated so that future ShouldSkip calls succeed
		assert.Equal(t, newHashes, cached.PolicyHashes)

		// The critical assertion: ShouldSkip must now return true with the new hashes
		assert.True(t, sm.ShouldSkip("sha256:abc123", newHashes, "openai:gpt-4", false),
			"ShouldSkip must return true after RecordResult updates policy hashes")
	})
}

func TestStateManager_PruneStaleHashes(t *testing.T) {
	t.Run("removes stale test hashes", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		// Record results for two tests
		sm.RecordResult("sha256:abc123", "test-1", []string{}, "openai:gpt-4", &CachedResult{Status: "passed"})
		sm.RecordResult("sha256:def456", "test-2", []string{}, "openai:gpt-4", &CachedResult{Status: "passed"})

		// Prune with only one test remaining
		currentHashes := map[string]bool{"sha256:abc123": true}
		sm.PruneStaleHashes(currentHashes)

		// test-1 should remain, test-2 should be removed
		assert.Contains(t, sm.state.Results, "sha256:abc123")
		assert.NotContains(t, sm.state.Results, "sha256:def456")
	})

	t.Run("preserves all model results within surviving test cases", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		// Record results for two models on the same test case
		sm.RecordResult("sha256:abc123", "test-1", []string{}, "openai:gpt-4", &CachedResult{Status: "passed"})
		sm.RecordResult("sha256:abc123", "test-1", []string{}, "anthropic:claude", &CachedResult{Status: "passed"})

		// Prune test cases — model data within surviving entries is preserved
		currentHashes := map[string]bool{"sha256:abc123": true}
		sm.PruneStaleHashes(currentHashes)

		cached := sm.state.Results["sha256:abc123"]
		require.NotNil(t, cached)
		assert.Contains(t, cached.Models, "openai:gpt-4")
		assert.Contains(t, cached.Models, "anthropic:claude", "model results should be preserved across prune")
	})
}

func TestStateManager_Save(t *testing.T) {
	t.Run("saves state to file", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")

		sm, err := NewStateManager(statePath, "test-suite", "1.0.0")
		require.NoError(t, err)

		sm.RecordResult("sha256:abc123", "test-1", []string{}, "openai:gpt-4", &CachedResult{Status: "passed"})

		err = sm.Save()
		require.NoError(t, err)

		// Verify file exists
		_, err = os.Stat(statePath)
		assert.NoError(t, err)
	})

	t.Run("does nothing without file path", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")
		sm.RecordResult("sha256:abc123", "test-1", []string{}, "openai:gpt-4", &CachedResult{Status: "passed"})

		err := sm.Save()
		assert.NoError(t, err)
	})

	t.Run("does nothing when not dirty", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")

		sm, _ := NewStateManager(statePath, "test-suite", "1.0.0")

		// Don't record anything - state is not dirty
		err := sm.Save()
		assert.NoError(t, err)

		// File should not exist
		_, err = os.Stat(statePath)
		assert.True(t, os.IsNotExist(err))
	})
}

// TestStateManager_MultiModelPersistence verifies that multiple model results
// on the same test case survive a save/load round-trip. This reproduces the bug
// where running --model A then --model B would lose model A's cached results.
func TestStateManager_MultiModelPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	policyHashes := []string{"sha256:policy1"}

	// Simulate first run: record results for model A
	sm1, err := NewStateManager(statePath, "test-suite", "1.0.0")
	require.NoError(t, err)

	sm1.RecordResult("sha256:test1", "case-1", policyHashes, "anthropic:claude", &CachedResult{
		Status: "passed", DurationMs: 5000,
	})
	sm1.RecordResult("sha256:test2", "case-2", policyHashes, "anthropic:claude", &CachedResult{
		Status: "failed", DurationMs: 3000,
	})
	require.NoError(t, sm1.Save())

	// Simulate second run: load state, record results for model B on same test cases
	sm2, err := NewStateManager(statePath, "test-suite", "1.0.0")
	require.NoError(t, err)

	sm2.RecordResult("sha256:test1", "case-1", policyHashes, "openai:gpt-5", &CachedResult{
		Status: "passed", DurationMs: 2000,
	})
	sm2.RecordResult("sha256:test2", "case-2", policyHashes, "openai:gpt-5", &CachedResult{
		Status: "passed", DurationMs: 1500,
	})
	require.NoError(t, sm2.Save())

	// Load final state and verify both models are present
	sm3, err := NewStateManager(statePath, "test-suite", "1.0.0")
	require.NoError(t, err)

	summaries := sm3.GetModelSummaries(policyHashes)
	require.Len(t, summaries, 2, "expected both models to survive save/load round-trip")

	anthropic := summaries["anthropic:claude"]
	require.NotNil(t, anthropic, "anthropic model results should be preserved")
	assert.Equal(t, 1, anthropic.Passed)
	assert.Equal(t, 1, anthropic.Failed)

	openai := summaries["openai:gpt-5"]
	require.NotNil(t, openai, "openai model results should be preserved")
	assert.Equal(t, 2, openai.Passed)
	assert.Equal(t, 0, openai.Failed)
}

func TestComputeContentHash(t *testing.T) {
	// Test that same content produces same hash
	content1 := []byte("test content")
	content2 := []byte("test content")
	content3 := []byte("different content")

	hash1 := ComputeContentHash(content1)
	hash2 := ComputeContentHash(content2)
	hash3 := ComputeContentHash(content3)

	assert.Equal(t, hash1, hash2)
	assert.NotEqual(t, hash1, hash3)
	assert.True(t, len(hash1) > 10) // Should be a proper hash
	assert.Contains(t, hash1, "sha256:")
}

func TestModelKey(t *testing.T) {
	key := ModelKey("openai", "gpt-4")
	assert.Equal(t, "openai:gpt-4", key)
}

// TestGetCachedDuration verifies that GetCachedDuration returns the cached
// duration for a test case, or 0 when no matching cache entry exists.
func TestGetCachedDuration(t *testing.T) {
	sm, _ := NewStateManager("", "test-suite", "1.0.0")
	policyHashes := []string{"sha256:policy1"}

	sm.RecordResult("sha256:test1", "case-1", policyHashes, "openai:gpt-5", &CachedResult{
		Status: "passed", DurationMs: 12345,
	})

	t.Run("returns cached duration", func(t *testing.T) {
		d := sm.GetCachedDuration("sha256:test1", policyHashes, "openai:gpt-5")
		assert.Equal(t, int64(12345), d)
	})

	t.Run("returns 0 for unknown content hash", func(t *testing.T) {
		d := sm.GetCachedDuration("sha256:unknown", policyHashes, "openai:gpt-5")
		assert.Equal(t, int64(0), d)
	})

	t.Run("returns 0 for mismatched policy hashes", func(t *testing.T) {
		d := sm.GetCachedDuration("sha256:test1", []string{"sha256:other"}, "openai:gpt-5")
		assert.Equal(t, int64(0), d)
	})

	t.Run("returns 0 for unknown model", func(t *testing.T) {
		d := sm.GetCachedDuration("sha256:test1", policyHashes, "anthropic:claude")
		assert.Equal(t, int64(0), d)
	})
}

// TestGetModelSummaries verifies that GetModelSummaries aggregates per-model
// results from cached state, filtering by policy hash match.
func TestGetModelSummaries(t *testing.T) {
	t.Run("aggregates results by model with matching policy hashes", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")
		policyHashes := []string{"sha256:policy1"}

		sm.RecordResult("sha256:test1", "case-1", policyHashes, "openai:gpt-5", &CachedResult{
			Status: "passed", DurationMs: 100,
		})
		sm.RecordResult("sha256:test2", "case-2", policyHashes, "openai:gpt-5", &CachedResult{
			Status: "failed", DurationMs: 200,
		})
		sm.RecordResult("sha256:test1", "case-1", policyHashes, "anthropic:claude", &CachedResult{
			Status: "passed", DurationMs: 50,
		})

		summaries := sm.GetModelSummaries(policyHashes)

		require.Len(t, summaries, 2)

		openai := summaries["openai:gpt-5"]
		require.NotNil(t, openai)
		assert.Equal(t, 1, openai.Passed)
		assert.Equal(t, 1, openai.Failed)
		assert.Equal(t, 0, openai.Errored)
		assert.Equal(t, int64(300), openai.TotalMs)
		assert.Equal(t, 2, openai.TestCount)

		anthropic := summaries["anthropic:claude"]
		require.NotNil(t, anthropic)
		assert.Equal(t, 1, anthropic.Passed)
		assert.Equal(t, int64(50), anthropic.TotalMs)
	})

	t.Run("excludes results with mismatched policy hashes", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:test1", "case-1", []string{"sha256:old"}, "openai:gpt-5", &CachedResult{
			Status: "passed", DurationMs: 100,
		})
		sm.RecordResult("sha256:test2", "case-2", []string{"sha256:current"}, "openai:gpt-5", &CachedResult{
			Status: "passed", DurationMs: 200,
		})

		summaries := sm.GetModelSummaries([]string{"sha256:current"})

		require.Len(t, summaries, 1)
		assert.Equal(t, 1, summaries["openai:gpt-5"].TestCount)
		assert.Equal(t, int64(200), summaries["openai:gpt-5"].TotalMs)
	})

	t.Run("returns empty map when no results match", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0")

		sm.RecordResult("sha256:test1", "case-1", []string{"sha256:old"}, "openai:gpt-5", &CachedResult{
			Status: "passed",
		})

		summaries := sm.GetModelSummaries([]string{"sha256:different"})
		assert.Empty(t, summaries)
	})
}

// TestHashesMatch exercises the order-independent hash comparison, including
// nil vs non-nil edge cases that caused the incremental re-run bug.
func TestHashesMatch(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []string
		expected bool
	}{
		{name: "both nil", a: nil, b: nil, expected: true},
		{name: "both empty", a: []string{}, b: []string{}, expected: true},
		{name: "nil vs non-nil", a: nil, b: []string{"sha256:abc"}, expected: false},
		{name: "non-nil vs nil", a: []string{"sha256:abc"}, b: nil, expected: false},
		{name: "nil vs empty", a: nil, b: []string{}, expected: true},
		{name: "same single element", a: []string{"sha256:abc"}, b: []string{"sha256:abc"}, expected: true},
		{name: "different single element", a: []string{"sha256:abc"}, b: []string{"sha256:def"}, expected: false},
		{name: "same elements different order", a: []string{"sha256:b", "sha256:a"}, b: []string{"sha256:a", "sha256:b"}, expected: true},
		{name: "different lengths", a: []string{"sha256:a"}, b: []string{"sha256:a", "sha256:b"}, expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, hashesMatch(tc.a, tc.b))
		})
	}
}

// TestGetCachedStatus verifies that GetCachedStatus returns the cached status
// for a test case/model, or empty string when no matching entry exists.
func TestGetCachedStatus(t *testing.T) {
	sm, _ := NewStateManager("", "test-suite", "1.0.0")
	hashes := []string{"sha256:pol1"}

	sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{Status: "passed"})
	sm.RecordResult("sha256:test2", "case-2", hashes, "openai:gpt-5", &CachedResult{Status: "failed"})

	tests := []struct {
		name         string
		contentHash  string
		policyHashes []string
		modelKey     string
		expected     string
	}{
		{name: "returns passed status", contentHash: "sha256:test1", policyHashes: hashes, modelKey: "openai:gpt-5", expected: "passed"},
		{name: "returns failed status", contentHash: "sha256:test2", policyHashes: hashes, modelKey: "openai:gpt-5", expected: "failed"},
		{name: "empty for unknown hash", contentHash: "sha256:unknown", policyHashes: hashes, modelKey: "openai:gpt-5", expected: ""},
		{name: "empty for mismatched policies", contentHash: "sha256:test1", policyHashes: []string{"sha256:other"}, modelKey: "openai:gpt-5", expected: ""},
		{name: "empty for unknown model", contentHash: "sha256:test1", policyHashes: hashes, modelKey: "anthropic:claude", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, sm.GetCachedStatus(tc.contentHash, tc.policyHashes, tc.modelKey))
		})
	}
}

// TestPersistenceRoundTrip_PolicyHashUpdate verifies that updated policy hashes
// survive a save/load cycle. This covers the scenario where a legacy state file
// (with null policy_hashes) gets corrected and the correction persists to disk.
func TestPersistenceRoundTrip_PolicyHashUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Session 1: Record result with nil policy hashes (simulates legacy state)
	sm1, err := NewStateManager(statePath, "test-suite", "1.0.0")
	require.NoError(t, err)
	sm1.RecordResult("sha256:abc123", "test-1", nil, "openai:gpt-4", &CachedResult{
		Status: "passed", DurationMs: 100,
	})
	require.NoError(t, sm1.Save())

	// Session 2: Re-record with computed policy hashes (simulates fix)
	sm2, err := NewStateManager(statePath, "test-suite", "1.0.0")
	require.NoError(t, err)

	// Before fix: entry has nil hashes, ShouldSkip fails
	newHashes := []string{"sha256:pol1", "sha256:pol2"}
	assert.False(t, sm2.ShouldSkip("sha256:abc123", newHashes, "openai:gpt-4", false),
		"ShouldSkip should fail before re-recording because hashes don't match")

	// Re-record updates the hashes
	sm2.RecordResult("sha256:abc123", "test-1", newHashes, "openai:gpt-4", &CachedResult{
		Status: "passed", DurationMs: 120,
	})
	require.NoError(t, sm2.Save())

	// Session 3: Fresh load should now skip correctly
	sm3, err := NewStateManager(statePath, "test-suite", "1.0.0")
	require.NoError(t, err)

	assert.True(t, sm3.ShouldSkip("sha256:abc123", newHashes, "openai:gpt-4", false),
		"ShouldSkip should succeed after policy hashes were corrected and persisted")
}

// TestShouldSkip_NilCachedHashesVsComputedHashes directly tests the exact
// scenario that caused the incremental re-run bug: cached entry has nil
// policy hashes but the query provides non-nil computed hashes.
func TestShouldSkip_NilCachedHashesVsComputedHashes(t *testing.T) {
	sm, _ := NewStateManager("", "test-suite", "1.0.0")

	// Directly manipulate state to simulate a legacy entry with nil PolicyHashes
	sm.state.Results["sha256:abc123"] = &CachedTestCase{
		CaseID:       "test-1",
		PolicyHashes: nil,
		Models: map[string]*CachedResult{
			"openai:gpt-4": {Status: "passed", DurationMs: 100},
		},
	}

	// This is the exact scenario that caused the bug: nil cached hashes
	// vs non-nil computed hashes from computePolicyHashes()
	computedHashes := []string{"sha256:pol1", "sha256:pol2"}
	assert.False(t, sm.ShouldSkip("sha256:abc123", computedHashes, "openai:gpt-4", false),
		"ShouldSkip must return false when cached hashes are nil but query hashes are non-nil")

	// After RecordResult updates the hashes, it should now skip
	sm.RecordResult("sha256:abc123", "test-1", computedHashes, "openai:gpt-4", &CachedResult{
		Status: "passed", DurationMs: 100,
	})
	assert.True(t, sm.ShouldSkip("sha256:abc123", computedHashes, "openai:gpt-4", false),
		"ShouldSkip must return true after RecordResult corrects the policy hashes")
}
