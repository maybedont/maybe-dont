package testsuite

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// StateFile represents the persisted test execution state.
type StateFile struct {
	SchemaVersion  string                     `json:"schema_version"`
	ProductVersion string                     `json:"product_version"`
	SuiteID        string                     `json:"suite_id"`
	LastUpdated    time.Time                  `json:"last_updated"`
	Results        map[string]*CachedTestCase `json:"results"`
}

// CachedTestCase stores the cached result for a single test case.
type CachedTestCase struct {
	CaseID       string                    `json:"case_id"`
	PolicyHashes []string                  `json:"policy_hashes"`
	Models       map[string]*CachedResult  `json:"models"`
}

// CachedResult stores the cached result for a single model run.
type CachedResult struct {
	Status     string    `json:"status"`
	Confidence float64   `json:"confidence"`
	LastRun    time.Time `json:"last_run"`
	DurationMs int64     `json:"duration_ms"`
}

// StateManager manages test execution state for incremental execution.
type StateManager struct {
	mu       sync.Mutex
	filePath string
	state    *StateFile
	dirty    bool
}

// NewStateManager creates a new state manager for the given file path.
// If the file exists, it loads the existing state.
func NewStateManager(filePath string, suiteID string, productVersion string) (*StateManager, error) {
	sm := &StateManager{
		filePath: filePath,
	}

	// Try to load existing state
	if filePath != "" {
		if err := sm.load(); err != nil {
			// Only silently start fresh for file-not-found. Other errors (permission
			// denied, corrupt JSON, etc.) should be surfaced so users aren't surprised
			// by silently lost cache state.
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("failed to load state file %s: %w", filePath, err)
			}
			sm.state = &StateFile{
				SchemaVersion:  "v1",
				ProductVersion: productVersion,
				SuiteID:        suiteID,
				LastUpdated:    time.Now(),
				Results:        make(map[string]*CachedTestCase),
			}
		}
	} else {
		// No state file - create in-memory only
		sm.state = &StateFile{
			SchemaVersion:  "v1",
			ProductVersion: productVersion,
			SuiteID:        suiteID,
			LastUpdated:    time.Now(),
			Results:        make(map[string]*CachedTestCase),
		}
	}

	return sm, nil
}

// load reads the state file from disk.
func (sm *StateManager) load() error {
	if sm.filePath == "" {
		return fmt.Errorf("no state file path configured")
	}

	data, err := os.ReadFile(sm.filePath)
	if err != nil {
		return err
	}

	var state StateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	sm.state = &state
	return nil
}

// Save writes the current state to disk with file locking.
func (sm *StateManager) Save() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.filePath == "" || !sm.dirty {
		return nil
	}

	sm.state.LastUpdated = time.Now()

	data, err := json.MarshalIndent(sm.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temp file first, then rename for atomicity
	tmpPath := sm.filePath + ".tmp"

	// Open with exclusive lock
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create temp state file: %w", err)
	}

	// Try to acquire exclusive lock with timeout
	if err := acquireLock(file, 5*time.Second); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to acquire lock on state file: %w", err)
	}

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write state file: %w", err)
	}

	// Flush to disk before rename to ensure data integrity
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync state file: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close state file: %w", err)
	}

	// Atomic rename. On Unix this is atomic even when the target exists.
	// On Windows, os.Rename fails if the target exists, so we remove it first
	// and retry. This makes the operation non-atomic on Windows, but the
	// file lock prevents concurrent writers.
	if err := os.Rename(tmpPath, sm.filePath); err != nil {
		// Retry after removing target (handles Windows limitation)
		if removeErr := os.Remove(sm.filePath); removeErr == nil {
			if err := os.Rename(tmpPath, sm.filePath); err != nil {
				_ = os.Remove(tmpPath)
				return fmt.Errorf("failed to rename state file after removing target: %w", err)
			}
		} else if !errors.Is(removeErr, os.ErrNotExist) {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to rename state file: %w", err)
		}
	}

	sm.dirty = false
	return nil
}

// acquireLock is implemented in platform-specific files:
// - state_unix.go for Unix systems (uses flock)
// - state_windows.go for Windows (uses LockFileEx)

// ShouldSkip returns true if the test case can be skipped (valid cached result).
// A test is skippable if:
// 1. The content hash matches
// 2. All policy hashes match
// 3. We have a cached result for this model
// 4. retryFailed is false, OR the cached status is "passed"
//
// By default, all cached results (passed, failed, errored) are skipped since
// re-running unchanged tests should produce the same result.
// Use retryFailed=true to re-run failed/errored tests (for checking transient issues).
func (sm *StateManager) ShouldSkip(contentHash string, policyHashes []string, modelKey string, retryFailed bool) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cached, ok := sm.state.Results[contentHash]
	if !ok {
		return false
	}

	// Check policy hashes match
	if !hashesMatch(cached.PolicyHashes, policyHashes) {
		return false
	}

	// Check if we have a result for this model
	modelResult, ok := cached.Models[modelKey]
	if !ok {
		return false
	}

	// If retryFailed is set, only skip passed tests
	if retryFailed {
		return modelResult.Status == "passed"
	}

	// Default: skip all cached results (passed, failed, errored)
	return true
}

// GetCachedStatus returns the cached status for a test case/model combination.
// Returns empty string if no cached result exists.
func (sm *StateManager) GetCachedStatus(contentHash string, policyHashes []string, modelKey string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cached, ok := sm.state.Results[contentHash]
	if !ok {
		return ""
	}
	if !hashesMatch(cached.PolicyHashes, policyHashes) {
		return ""
	}
	modelResult, ok := cached.Models[modelKey]
	if !ok {
		return ""
	}
	return modelResult.Status
}

// hashesMatch checks if two hash slices are equal (order-independent).
func hashesMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Sort copies to compare
	aCopy := make([]string, len(a))
	bCopy := make([]string, len(b))
	copy(aCopy, a)
	copy(bCopy, b)
	sort.Strings(aCopy)
	sort.Strings(bCopy)

	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}
	return true
}

// RecordResult records a test result in the state.
func (sm *StateManager) RecordResult(contentHash, caseID string, policyHashes []string, modelKey string, result *CachedResult) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cached, ok := sm.state.Results[contentHash]
	if !ok {
		cached = &CachedTestCase{
			CaseID:       caseID,
			PolicyHashes: policyHashes,
			Models:       make(map[string]*CachedResult),
		}
		sm.state.Results[contentHash] = cached
	}

	cached.Models[modelKey] = result
	sm.dirty = true
}

// PruneStaleHashes removes entries for hashes that no longer exist in the current test cases.
func (sm *StateManager) PruneStaleHashes(currentHashes map[string]bool, currentModels []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Build model key set
	modelSet := make(map[string]bool)
	for _, m := range currentModels {
		modelSet[m] = true
	}

	// Remove stale test case hashes
	for hash := range sm.state.Results {
		if !currentHashes[hash] {
			delete(sm.state.Results, hash)
			sm.dirty = true
		}
	}

	// Remove stale model results within remaining test cases
	for _, cached := range sm.state.Results {
		for modelKey := range cached.Models {
			if !modelSet[modelKey] {
				delete(cached.Models, modelKey)
				sm.dirty = true
			}
		}
	}
}

// Close saves any pending changes and closes the state file.
func (sm *StateManager) Close() error {
	return sm.Save()
}

// ComputeContentHash calculates SHA256 hash of raw file bytes.
func ComputeContentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// ComputePolicyHash calculates SHA256 hash of a policy's YAML representation.
func ComputePolicyHash(policyYAML []byte) string {
	hash := sha256.Sum256(policyYAML)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// ModelKey generates a cache key for a model configuration.
func ModelKey(provider, model string) string {
	return provider + ":" + model
}

// GetCachedDuration returns the cached duration in milliseconds for a test case/model
// combination. Returns 0 if no cached result exists or policy hashes don't match.
// Used by the progress indicator to estimate how long a test will take.
func (sm *StateManager) GetCachedDuration(contentHash string, policyHashes []string, modelKey string) int64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cached, ok := sm.state.Results[contentHash]
	if !ok {
		return 0
	}
	if !hashesMatch(cached.PolicyHashes, policyHashes) {
		return 0
	}
	modelResult, ok := cached.Models[modelKey]
	if !ok {
		return 0
	}
	return modelResult.DurationMs
}

// CachedModelSummary aggregates test results for a single model from cached state.
type CachedModelSummary struct {
	Passed    int
	Failed    int
	Errored   int
	TotalMs   int64
	TestCount int
}

// GetModelSummaries aggregates test results per model from cached state.
// Only includes entries whose policy hashes match the provided hashes,
// ensuring results reflect the current policy versions.
func (sm *StateManager) GetModelSummaries(policyHashes []string) map[string]*CachedModelSummary {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	summaries := make(map[string]*CachedModelSummary)

	for _, cached := range sm.state.Results {
		if !hashesMatch(cached.PolicyHashes, policyHashes) {
			continue
		}

		for modelKey, result := range cached.Models {
			s, ok := summaries[modelKey]
			if !ok {
				s = &CachedModelSummary{}
				summaries[modelKey] = s
			}

			s.TestCount++
			s.TotalMs += result.DurationMs

			switch result.Status {
			case "passed":
				s.Passed++
			case "failed":
				s.Failed++
			case "errored":
				s.Errored++
			}
		}
	}

	return summaries
}
