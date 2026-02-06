package testsuite

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
			// File doesn't exist or is invalid - start fresh
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

	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close state file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, sm.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	sm.dirty = false
	return nil
}

// acquireLock is implemented in platform-specific files:
// - state_unix.go for Unix systems (uses flock)
// - state_windows.go for Windows (uses LockFileEx)

// ShouldSkip returns true if the test case can be skipped (valid cached result).
// A test is skippable only if:
// 1. The content hash matches
// 2. All policy hashes match
// 3. The cached result status is "passed" (failed tests should be re-run)
func (sm *StateManager) ShouldSkip(contentHash string, policyHashes []string, modelKey string) bool {
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

	// Only skip if the previous run passed
	return modelResult.Status == "passed"
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
