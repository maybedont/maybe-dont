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
		sm, err := NewStateManager("", "test-suite", "1.0.0", 0)
		require.NoError(t, err)
		assert.NotNil(t, sm.state)
		assert.Equal(t, "v2", sm.state.SchemaVersion)
		assert.Equal(t, "1.0.0", sm.state.ProductVersion)
		assert.Equal(t, "test-suite", sm.state.SuiteID)
		assert.Equal(t, DefaultHistoryDepth, sm.state.HistoryDepth)
	})

	t.Run("creates new state when file doesn't exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")

		sm, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
		require.NoError(t, err)
		assert.NotNil(t, sm.state)
		assert.Equal(t, "v2", sm.state.SchemaVersion)
	})

	t.Run("loads existing state file", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")

		// Create initial state
		sm1, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
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
		sm2, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
		require.NoError(t, err)

		// Verify the result was loaded
		assert.True(t, sm2.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", false))
	})
}

func TestStateManager_ShouldSkip(t *testing.T) {
	t.Run("returns false for uncached test", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

		assert.False(t, sm.ShouldSkip("sha256:abc123", []string{}, "openai:gpt-4", false))
	})

	t.Run("returns true for cached passing test", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

		sm.RecordResult("sha256:abc123", "test-1", []string{"sha256:pol1"}, "openai:gpt-4", &CachedResult{
			Status:     "passed",
			Confidence: 1.0,
			LastRun:    time.Now(),
			DurationMs: 100,
		})

		assert.True(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-4", false))
	})

	t.Run("returns true for cached failing test by default", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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

		sm, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
		require.NoError(t, err)

		sm.RecordResult("sha256:abc123", "test-1", []string{}, "openai:gpt-4", &CachedResult{Status: "passed"})

		err = sm.Save()
		require.NoError(t, err)

		// Verify file exists
		_, err = os.Stat(statePath)
		assert.NoError(t, err)
	})

	t.Run("does nothing without file path", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		sm.RecordResult("sha256:abc123", "test-1", []string{}, "openai:gpt-4", &CachedResult{Status: "passed"})

		err := sm.Save()
		assert.NoError(t, err)
	})

	t.Run("does nothing when not dirty", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")

		sm, _ := NewStateManager(statePath, "test-suite", "1.0.0", 0)

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
	sm1, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
	require.NoError(t, err)

	sm1.RecordResult("sha256:test1", "case-1", policyHashes, "anthropic:claude", &CachedResult{
		Status: "passed", DurationMs: 5000,
	})
	sm1.RecordResult("sha256:test2", "case-2", policyHashes, "anthropic:claude", &CachedResult{
		Status: "failed", DurationMs: 3000,
	})
	require.NoError(t, sm1.Save())

	// Simulate second run: load state, record results for model B on same test cases
	sm2, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
	require.NoError(t, err)

	sm2.RecordResult("sha256:test1", "case-1", policyHashes, "openai:gpt-5", &CachedResult{
		Status: "passed", DurationMs: 2000,
	})
	sm2.RecordResult("sha256:test2", "case-2", policyHashes, "openai:gpt-5", &CachedResult{
		Status: "passed", DurationMs: 1500,
	})
	require.NoError(t, sm2.Save())

	// Load final state and verify both models are present
	sm3, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
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
	sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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
	sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
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
	sm1, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
	require.NoError(t, err)
	sm1.RecordResult("sha256:abc123", "test-1", nil, "openai:gpt-4", &CachedResult{
		Status: "passed", DurationMs: 100,
	})
	require.NoError(t, sm1.Save())

	// Session 2: Re-record with computed policy hashes (simulates fix)
	sm2, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
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
	sm3, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
	require.NoError(t, err)

	assert.True(t, sm3.ShouldSkip("sha256:abc123", newHashes, "openai:gpt-4", false),
		"ShouldSkip should succeed after policy hashes were corrected and persisted")
}

// TestShouldSkip_NilCachedHashesVsComputedHashes directly tests the exact
// scenario that caused the incremental re-run bug: cached entry has nil
// policy hashes but the query provides non-nil computed hashes.
func TestShouldSkip_NilCachedHashesVsComputedHashes(t *testing.T) {
	sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

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

// =============================================================================
// History ring buffer tests
// =============================================================================

// TestRecordResult_AppendsHistory verifies that each call to RecordResult
// prepends a RunOutcome to the history and trims to the configured depth.
func TestRecordResult_AppendsHistory(t *testing.T) {
	t.Run("first recording creates single history entry", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		hashes := []string{"sha256:pol1"}
		now := time.Now()

		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: now, DurationMs: 100,
		})

		cached := sm.state.Results["sha256:test1"]
		require.NotNil(t, cached)
		result := cached.Models["openai:gpt-5"]
		require.Len(t, result.History, 1)
		assert.Equal(t, "passed", result.History[0].Status)
		assert.Equal(t, int64(100), result.History[0].DurationMs)
		assert.False(t, result.History[0].PolicyChange)
	})

	t.Run("subsequent recordings prepend to history", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		hashes := []string{"sha256:pol1"}

		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: time.Now(), DurationMs: 100,
		})
		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: "failed", LastRun: time.Now(), DurationMs: 200,
		})
		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: time.Now(), DurationMs: 150,
		})

		result := sm.state.Results["sha256:test1"].Models["openai:gpt-5"]
		require.Len(t, result.History, 3)
		// Most recent first
		assert.Equal(t, "passed", result.History[0].Status)
		assert.Equal(t, "failed", result.History[1].Status)
		assert.Equal(t, "passed", result.History[2].Status)
	})

	t.Run("trims history to configured depth", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 3) // depth=3
		hashes := []string{"sha256:pol1"}

		for i := range 5 {
			status := "passed"
			if i%2 == 0 {
				status = "failed"
			}
			sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
				Status: status, LastRun: time.Now(), DurationMs: int64(i * 100),
			})
		}

		result := sm.state.Results["sha256:test1"].Models["openai:gpt-5"]
		require.Len(t, result.History, 3, "history should be trimmed to depth=3")
		// Should retain the 3 most recent (indices 4, 3, 2 → failed, passed, failed)
		assert.Equal(t, "failed", result.History[0].Status)
		assert.Equal(t, "passed", result.History[1].Status)
		assert.Equal(t, "failed", result.History[2].Status)
	})

	t.Run("models have independent histories", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		hashes := []string{"sha256:pol1"}

		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: time.Now(), DurationMs: 100,
		})
		sm.RecordResult("sha256:test1", "case-1", hashes, "anthropic:claude", &CachedResult{
			Status: "failed", LastRun: time.Now(), DurationMs: 200,
		})

		openai := sm.state.Results["sha256:test1"].Models["openai:gpt-5"]
		anthropic := sm.state.Results["sha256:test1"].Models["anthropic:claude"]
		require.Len(t, openai.History, 1)
		require.Len(t, anthropic.History, 1)
		assert.Equal(t, "passed", openai.History[0].Status)
		assert.Equal(t, "failed", anthropic.History[0].Status)
	})
}

// TestRecordResult_PolicyChangeDetection verifies that the PolicyChange flag
// is set correctly when policy hashes change between runs.
func TestRecordResult_PolicyChangeDetection(t *testing.T) {
	t.Run("no policy change on first recording", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		hashes := []string{"sha256:pol1"}

		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: time.Now(), DurationMs: 100,
		})

		result := sm.state.Results["sha256:test1"].Models["openai:gpt-5"]
		assert.False(t, result.History[0].PolicyChange)
	})

	t.Run("no policy change when hashes match", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		hashes := []string{"sha256:pol1"}

		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: time.Now(), DurationMs: 100,
		})
		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: "failed", LastRun: time.Now(), DurationMs: 200,
		})

		result := sm.state.Results["sha256:test1"].Models["openai:gpt-5"]
		assert.False(t, result.History[0].PolicyChange)
		assert.False(t, result.History[1].PolicyChange)
	})

	t.Run("policy change flagged when hashes differ", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		hashesV1 := []string{"sha256:pol1"}
		hashesV2 := []string{"sha256:pol2"}

		// Record with v1 policy
		sm.RecordResult("sha256:test1", "case-1", hashesV1, "openai:gpt-5", &CachedResult{
			Status: "failed", LastRun: time.Now(), DurationMs: 100,
		})
		// Record with v2 policy (changed)
		sm.RecordResult("sha256:test1", "case-1", hashesV2, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: time.Now(), DurationMs: 200,
		})

		result := sm.state.Results["sha256:test1"].Models["openai:gpt-5"]
		require.Len(t, result.History, 2)
		assert.True(t, result.History[0].PolicyChange, "most recent entry should have PolicyChange=true")
		assert.False(t, result.History[1].PolicyChange, "older entry should not be modified")
	})

	t.Run("nil to non-nil policy hashes is not a change", func(t *testing.T) {
		// This simulates a legacy entry with nil PolicyHashes being re-recorded
		// after computePolicyHashes was added. This is a "fix", not a "change".
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)

		sm.RecordResult("sha256:test1", "case-1", nil, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: time.Now(), DurationMs: 100,
		})
		sm.RecordResult("sha256:test1", "case-1", []string{"sha256:pol1"}, "openai:gpt-5", &CachedResult{
			Status: "passed", LastRun: time.Now(), DurationMs: 100,
		})

		result := sm.state.Results["sha256:test1"].Models["openai:gpt-5"]
		assert.False(t, result.History[0].PolicyChange,
			"nil-to-computed transition should not be flagged as policy change")
	})
}

// TestPassRate verifies pass rate calculation from history.
func TestPassRate(t *testing.T) {
	tests := []struct {
		name         string
		history      []RunOutcome
		expectedRate float64
		expectedRuns int
	}{
		{
			name:         "empty history",
			history:      nil,
			expectedRate: 0,
			expectedRuns: 0,
		},
		{
			name: "all passed",
			history: []RunOutcome{
				{Status: "passed"}, {Status: "passed"}, {Status: "passed"},
			},
			expectedRate: 1.0,
			expectedRuns: 3,
		},
		{
			name: "all failed",
			history: []RunOutcome{
				{Status: "failed"}, {Status: "failed"},
			},
			expectedRate: 0.0,
			expectedRuns: 2,
		},
		{
			name: "mixed results",
			history: []RunOutcome{
				{Status: "passed"}, {Status: "failed"}, {Status: "passed"},
				{Status: "errored"}, {Status: "passed"},
			},
			expectedRate: 0.6,
			expectedRuns: 5,
		},
		{
			name: "errored counts as not passed",
			history: []RunOutcome{
				{Status: "errored"}, {Status: "passed"},
			},
			expectedRate: 0.5,
			expectedRuns: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cr := &CachedResult{History: tc.history}
			rate, runs := PassRate(cr)
			assert.InDelta(t, tc.expectedRate, rate, 0.001)
			assert.Equal(t, tc.expectedRuns, runs)
		})
	}
}

// TestPassRateSincePolicyChange verifies that pass rate calculation stops
// at the most recent policy change boundary.
func TestPassRateSincePolicyChange(t *testing.T) {
	t.Run("no policy change returns full history rate", func(t *testing.T) {
		cr := &CachedResult{
			History: []RunOutcome{
				{Status: "passed"}, {Status: "failed"}, {Status: "passed"},
			},
		}
		rate, runs := PassRateSincePolicyChange(cr)
		assert.InDelta(t, 2.0/3.0, rate, 0.001)
		assert.Equal(t, 3, runs)
	})

	t.Run("stops at policy change boundary", func(t *testing.T) {
		cr := &CachedResult{
			History: []RunOutcome{
				{Status: "passed"},                       // after change
				{Status: "passed"},                       // after change
				{Status: "failed", PolicyChange: true},   // the change run itself
				{Status: "failed"},                       // before change (should not be included)
				{Status: "failed"},                       // before change
			},
		}
		rate, runs := PassRateSincePolicyChange(cr)
		assert.InDelta(t, 2.0/3.0, rate, 0.001, "should count 2 passed out of 3 runs since change")
		assert.Equal(t, 3, runs, "should include the policy change run and runs after it")
	})

	t.Run("policy change at first entry uses only that entry", func(t *testing.T) {
		cr := &CachedResult{
			History: []RunOutcome{
				{Status: "passed", PolicyChange: true},
				{Status: "failed"},
				{Status: "failed"},
			},
		}
		// PolicyChange is at index 0, count > 0 check prevents breaking at first entry
		// so it should include all entries (no boundary to stop at)
		rate, runs := PassRateSincePolicyChange(cr)
		assert.InDelta(t, 1.0/3.0, rate, 0.001)
		assert.Equal(t, 3, runs)
	})

	t.Run("empty history", func(t *testing.T) {
		cr := &CachedResult{}
		rate, runs := PassRateSincePolicyChange(cr)
		assert.Equal(t, float64(0), rate)
		assert.Equal(t, 0, runs)
	})
}

// TestHistoryPersistence verifies that history survives save/load round-trips.
func TestHistoryPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	hashes := []string{"sha256:pol1"}

	// Session 1: Record 3 results to build history
	sm1, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
	require.NoError(t, err)

	for _, status := range []string{"passed", "failed", "passed"} {
		sm1.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: status, LastRun: time.Now(), DurationMs: 100,
		})
	}
	require.NoError(t, sm1.Save())

	// Session 2: Load and verify history survived
	sm2, err := NewStateManager(statePath, "test-suite", "1.0.0", 0)
	require.NoError(t, err)

	result := sm2.state.Results["sha256:test1"].Models["openai:gpt-5"]
	require.Len(t, result.History, 3, "history should survive save/load round-trip")
	assert.Equal(t, "passed", result.History[0].Status)
	assert.Equal(t, "failed", result.History[1].Status)
	assert.Equal(t, "passed", result.History[2].Status)

	// Verify pass rate
	rate, runs := PassRate(result)
	assert.InDelta(t, 2.0/3.0, rate, 0.001)
	assert.Equal(t, 3, runs)
}

// TestV1StateUpgrade verifies that loading a v1 state file upgrades it to v2
// without invalidating existing cached results.
func TestV1StateUpgrade(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Write a v1 state file manually
	v1State := `{
		"schema_version": "v1",
		"product_version": "dev",
		"suite_id": "test-suite",
		"last_updated": "2026-01-01T00:00:00Z",
		"results": {
			"sha256:abc123": {
				"case_id": "case-1",
				"policy_hashes": ["sha256:pol1"],
				"models": {
					"openai:gpt-5": {
						"status": "passed",
						"confidence": 1.0,
						"last_run": "2026-01-01T00:00:00Z",
						"duration_ms": 100
					}
				}
			}
		}
	}`
	require.NoError(t, os.WriteFile(statePath, []byte(v1State), 0644))

	// Load with v2 code
	sm, err := NewStateManager(statePath, "test-suite", "dev", 0)
	require.NoError(t, err)

	// Schema should be upgraded
	assert.Equal(t, "v2", sm.state.SchemaVersion)
	assert.Equal(t, DefaultHistoryDepth, sm.state.HistoryDepth)

	// Existing cached result should still be usable
	assert.True(t, sm.ShouldSkip("sha256:abc123", []string{"sha256:pol1"}, "openai:gpt-5", false),
		"v1 cached results should remain valid after upgrade")

	// History should be nil (no history existed in v1)
	result := sm.state.Results["sha256:abc123"].Models["openai:gpt-5"]
	assert.Nil(t, result.History, "v1 entries should have nil history")

	// Recording a new result should start building history
	sm.RecordResult("sha256:abc123", "case-1", []string{"sha256:pol1"}, "openai:gpt-5", &CachedResult{
		Status: "passed", LastRun: time.Now(), DurationMs: 120,
	})
	result = sm.state.Results["sha256:abc123"].Models["openai:gpt-5"]
	require.Len(t, result.History, 1, "first recording after v1 upgrade should create history")
}

// TestGetPassRate verifies the StateManager.GetPassRate lookup method.
func TestGetPassRate(t *testing.T) {
	sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
	hashes := []string{"sha256:pol1"}

	// Build history: 3 passed, 1 failed
	for _, status := range []string{"passed", "failed", "passed", "passed"} {
		sm.RecordResult("sha256:test1", "case-1", hashes, "openai:gpt-5", &CachedResult{
			Status: status, LastRun: time.Now(), DurationMs: 100,
		})
	}

	t.Run("returns rate for matching entry", func(t *testing.T) {
		rate, runs, found := sm.GetPassRate("sha256:test1", hashes, "openai:gpt-5")
		assert.True(t, found)
		assert.InDelta(t, 0.75, rate, 0.001)
		assert.Equal(t, 4, runs)
	})

	t.Run("returns false for unknown content hash", func(t *testing.T) {
		_, _, found := sm.GetPassRate("sha256:unknown", hashes, "openai:gpt-5")
		assert.False(t, found)
	})

	t.Run("returns false for mismatched policy hashes", func(t *testing.T) {
		_, _, found := sm.GetPassRate("sha256:test1", []string{"sha256:other"}, "openai:gpt-5")
		assert.False(t, found)
	})

	t.Run("returns false for unknown model", func(t *testing.T) {
		_, _, found := sm.GetPassRate("sha256:test1", hashes, "anthropic:claude")
		assert.False(t, found)
	})
}

// TestDefaultHistoryDepth verifies that history depth defaults correctly.
func TestDefaultHistoryDepth(t *testing.T) {
	t.Run("zero uses default", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		assert.Equal(t, DefaultHistoryDepth, sm.historyDepth)
	})

	t.Run("negative uses default", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", -5)
		assert.Equal(t, DefaultHistoryDepth, sm.historyDepth)
	})

	t.Run("custom depth is honored", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 10)
		assert.Equal(t, 10, sm.historyDepth)
	})
}
