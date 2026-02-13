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

// DefaultHistoryDepth is the default number of recent run outcomes to retain per test/model.
const DefaultHistoryDepth = 20

// StateFile represents the persisted test execution state.
type StateFile struct {
	SchemaVersion  string                     `json:"schema_version"`
	ProductVersion string                     `json:"product_version"`
	SuiteID        string                     `json:"suite_id"`
	HistoryDepth   int                        `json:"history_depth"`
	LastUpdated    time.Time                  `json:"last_updated"`
	Results        map[string]*CachedTestCase `json:"results"`
}

// CachedTestCase stores the cached result for a single test case.
type CachedTestCase struct {
	CaseID       string                   `json:"case_id"`
	PolicyHashes []string                 `json:"policy_hashes"`
	Models       map[string]*CachedResult `json:"models"`
}

// CachedResult stores the cached result for a single model run.
type CachedResult struct {
	Status     string    `json:"status"`
	Confidence float64   `json:"confidence"`
	LastRun    time.Time `json:"last_run"`
	DurationMs int64     `json:"duration_ms"`

	// Rolling history of recent runs (most recent first, capped at history_depth).
	// History only records actual executions — skipped (cached) runs are not included.
	History []RunOutcome `json:"history,omitempty"`
}

// RunOutcome records the outcome of a single test execution.
type RunOutcome struct {
	Status       string    `json:"status"` // "passed", "failed", "errored"
	RunAt        time.Time `json:"run_at"`
	DurationMs   int64     `json:"duration_ms"`
	PolicyChange bool      `json:"policy_change,omitempty"` // true when policy hashes differ from previous run
}

// StateManager manages test execution state for incremental execution.
type StateManager struct {
	mu           sync.Mutex
	filePath     string
	state        *StateFile
	dirty        bool
	historyDepth int // max history entries per test/model
}

// NewStateManager creates a new state manager for the given file path.
// If the file exists, it loads the existing state. historyDepth controls
// how many recent run outcomes are retained per test/model (0 uses DefaultHistoryDepth).
func NewStateManager(filePath string, suiteID string, productVersion string, historyDepth int) (*StateManager, error) {
	if historyDepth <= 0 {
		historyDepth = DefaultHistoryDepth
	}

	sm := &StateManager{
		filePath:     filePath,
		historyDepth: historyDepth,
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
			sm.state = newStateFile(suiteID, productVersion, historyDepth)
		}
	} else {
		// No state file - create in-memory only
		sm.state = newStateFile(suiteID, productVersion, historyDepth)
	}

	// Upgrade v1 state files: update schema version and persist history_depth.
	// Existing cached results are preserved — History fields start nil and
	// begin accumulating on next actual execution.
	if sm.state.SchemaVersion == "v1" {
		sm.state.SchemaVersion = "v2"
		sm.state.HistoryDepth = historyDepth
		sm.dirty = true
	}

	return sm, nil
}

func newStateFile(suiteID, productVersion string, historyDepth int) *StateFile {
	return &StateFile{
		SchemaVersion:  "v2",
		ProductVersion: productVersion,
		SuiteID:        suiteID,
		HistoryDepth:   historyDepth,
		LastUpdated:    time.Now(),
		Results:        make(map[string]*CachedTestCase),
	}
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

// RecordResult records a test result in the state, appending to the rolling
// history for pass rate tracking. Policy changes are detected by comparing
// incoming hashes against stored hashes and marked in the history entry.
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

	// Detect policy change by comparing incoming hashes against stored hashes.
	// A change is only flagged when the entry already existed with non-nil hashes
	// (first recording or legacy nil→computed is not a "change").
	policyChanged := ok && cached.PolicyHashes != nil && !hashesMatch(cached.PolicyHashes, policyHashes)

	existing := cached.Models[modelKey]

	// Build history entry from this execution
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

	// Trim to configured depth. If history_depth was reduced since last run,
	// skipped tests retain their old-depth histories until they naturally re-run;
	// this is harmless since pass rate calculations use len(History).
	if len(history) > sm.historyDepth {
		history = history[:sm.historyDepth]
	}

	result.History = history
	cached.PolicyHashes = policyHashes
	cached.Models[modelKey] = result
	sm.dirty = true
}

// PassRate computes the pass rate from a CachedResult's history.
// Returns (rate, runCount). Rate is 0.0-1.0. runCount is the number
// of history entries (may be less than history_depth if the test
// hasn't run that many times yet). Returns (0, 0) if no history.
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

// PassRateSincePolicyChange computes the pass rate using only runs in the
// current policy window. The window starts at the most recent PolicyChange
// marker and includes all newer entries. If PolicyChange is on the newest
// entry (index 0), there are no post-change runs to isolate, so the full
// history rate is returned — callers should compare run counts to detect
// this case and suppress redundant output (see formatPassRate in runner.go).
func PassRateSincePolicyChange(cr *CachedResult) (float64, int) {
	if len(cr.History) == 0 {
		return 0, 0
	}
	passed := 0
	count := 0
	for i, h := range cr.History {
		count++
		if h.Status == "passed" {
			passed++
		}
		// Stop after processing the policy change entry (include it in the window).
		// Skip the break when PolicyChange is on the very first entry (most recent run)
		// because there's nothing newer to form a "since change" window — use full history.
		if h.PolicyChange && i > 0 {
			break
		}
	}
	return float64(passed) / float64(count), count
}

// CachedTestPassRate holds pass rate data for a single (test case, model) combination,
// derived from cached history. Used to build stability reporting in summary-only mode.
type CachedTestPassRate struct {
	CaseID                  string
	Model                   string
	PassRate                float64
	PassRateRuns            int
	PassRateSinceChange     float64
	PassRateSinceChangeRuns int
}

// GetCachedPassRates returns pass rate data for all (test case, model) combinations
// in cached state that match the given policy hashes. Only includes entries with
// 2+ history entries (sufficient data for meaningful rates).
func (sm *StateManager) GetCachedPassRates(policyHashes []string) []CachedTestPassRate {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var results []CachedTestPassRate
	for _, cached := range sm.state.Results {
		if !hashesMatch(cached.PolicyHashes, policyHashes) {
			continue
		}
		for modelKey, result := range cached.Models {
			if len(result.History) < 2 {
				continue
			}
			rate, runs := PassRate(result)
			sinceRate, sinceRuns := PassRateSincePolicyChange(result)
			results = append(results, CachedTestPassRate{
				CaseID:                  cached.CaseID,
				Model:                   modelKey,
				PassRate:                rate,
				PassRateRuns:            runs,
				PassRateSinceChange:     sinceRate,
				PassRateSinceChangeRuns: sinceRuns,
			})
		}
	}
	return results
}

// PruneStaleHashes removes test case entries whose content hashes no longer exist
// in the current suite. Model results within surviving test cases are preserved —
// historical model data is valuable for cross-model comparison and costs little to keep.
func (sm *StateManager) PruneStaleHashes(currentHashes map[string]bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for hash := range sm.state.Results {
		if !currentHashes[hash] {
			delete(sm.state.Results, hash)
			sm.dirty = true
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

// GetLastUpdated returns the last updated timestamp from the state file.
func (sm *StateManager) GetLastUpdated() time.Time {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.state.LastUpdated
}

// HasResults returns true if the state file contains any cached test results,
// regardless of whether their policy hashes match current policies.
func (sm *StateManager) HasResults() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.state.Results) > 0
}

// CachedModelSummary aggregates test results for a single model from cached state.
type CachedModelSummary struct {
	Passed    int
	Failed    int
	Errored   int
	TotalMs   int64
	TestCount int

	// Stability is the mean pass rate across tests with 3+ history entries (0.0-1.0)
	Stability float64

	// StabilityTests is the count of tests with 3+ history entries used to compute Stability
	StabilityTests int
}

// GetModelSummaries aggregates test results per model from cached state.
// Only includes entries whose policy hashes match the provided hashes,
// ensuring results reflect the current policy versions.
func (sm *StateManager) GetModelSummaries(policyHashes []string) map[string]*CachedModelSummary {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	summaries := make(map[string]*CachedModelSummary)

	// Track per-model pass rates for stability calculation
	type passRateAccum struct {
		sumRates float64
		count    int
	}
	stabilityAccum := make(map[string]*passRateAccum)

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

			// Accumulate pass rate for stability (only tests with 3+ history entries)
			if len(result.History) >= 3 {
				rate, _ := PassRate(result)
				acc, ok := stabilityAccum[modelKey]
				if !ok {
					acc = &passRateAccum{}
					stabilityAccum[modelKey] = acc
				}
				acc.sumRates += rate
				acc.count++
			}
		}
	}

	// Compute stability as mean pass rate
	for modelKey, acc := range stabilityAccum {
		if s, ok := summaries[modelKey]; ok && acc.count > 0 {
			s.Stability = acc.sumRates / float64(acc.count)
			s.StabilityTests = acc.count
		}
	}

	return summaries
}
