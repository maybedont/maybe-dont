package testsuite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// createTestLogger creates a no-op logger for test execution.
// Test output already captures all meaningful data (expected vs actual, policy decisions, AI reasoning).
// Internal engine logging is not useful for policy debugging.
func createTestLogger() *config.SessionLogger {
	return config.NewSessionLogger(zap.NewNop())
}

// Runner executes a test suite against policies.
type Runner struct {
	opts            RunnerOptions
	suite           *Suite
	testCases       []TestCase
	testCaseHashes  map[string]string // case_id -> content hash
	testCaseFiles   map[string]string // case_id -> file path
	policyHashes    []string          // SHA256 hashes of all loaded policies
	suiteDir        string
	logger          *config.SessionLogger
	rateLimiter     *RateLimiter
	stateManager    *StateManager
}

// NewRunner creates a new test suite runner with the given options.
func NewRunner(opts RunnerOptions) (*Runner, error) {
	// Resolve suite directory to absolute path
	absDir, err := filepath.Abs(opts.SuiteDir)
	if err != nil {
		return nil, &PathResolutionError{
			Path:    opts.SuiteDir,
			Message: fmt.Sprintf("failed to resolve absolute path: %v", err),
		}
	}

	// Check suite directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		return nil, &PathResolutionError{
			Path:    opts.SuiteDir,
			Message: fmt.Sprintf("directory does not exist: %v", err),
		}
	}
	if !info.IsDir() {
		return nil, &PathResolutionError{
			Path:    opts.SuiteDir,
			Message: "path is not a directory",
		}
	}

	// Create logger for the runner (respects MAYBE_DONT_LOGGER_LEVEL, defaults to info)
	sessionLogger := createTestLogger()

	return &Runner{
		opts:     opts,
		suiteDir: absDir,
		logger:   sessionLogger,
	}, nil
}

// Run executes the test suite and returns the results.
func (r *Runner) Run(ctx context.Context) (*RunResult, error) {
	// Phase 1: Load and validate suite configuration
	if err := r.loadSuite(); err != nil {
		return nil, err
	}

	// Phase 2: Discover and validate test cases
	if err := r.discoverTestCases(); err != nil {
		return nil, err
	}

	// Phase 3: Validate policy integrity
	if err := r.validatePolicyIntegrity(); err != nil {
		return nil, err
	}

	// Phase 3b: Compute policy hashes for cache invalidation
	if err := r.computePolicyHashes(); err != nil {
		return nil, err
	}

	// If validate-only mode, return success
	if r.opts.ValidateOnly {
		fmt.Printf("Suite validation passed: %d test cases discovered\n", len(r.testCases))
		return &RunResult{
			ThresholdsMet: true,
			TotalCases:    len(r.testCases),
		}, nil
	}

	// Initialize rate limiter with suite config and CLI overrides
	r.rateLimiter = NewRateLimiter(RateLimiterConfig{
		ProviderLimits:         r.suite.Execution.RateLimits,
		DefaultLimit:           DefaultRequestsPerMinute,
		DelayBetweenRequestsMs: r.suite.Execution.DelayBetweenRequestsMs,
		RateLimitBufferMs:      r.suite.Execution.RateLimitBufferMs,
		OverrideRPM:            r.opts.RequestsPerMinute,
		WaitOnLimit:            r.opts.Wait,
	})

	// Initialize state manager for incremental execution
	if r.opts.StateFile != "" {
		var err error
		r.stateManager, err = NewStateManager(r.opts.StateFile, r.suite.BundleID, "dev")
		if err != nil {
			return nil, fmt.Errorf("failed to initialize state manager: %w", err)
		}
		defer func() {
			if err := r.stateManager.Close(); err != nil {
				fmt.Printf("Warning: failed to close state file: %v\n", err)
			}
		}()
	}

	// Phase 4: Execute tests
	result, err := r.executeTests(ctx)
	if err != nil {
		return nil, err
	}

	// Prune stale hashes and save final state
	if r.stateManager != nil {
		// Build set of current content hashes
		currentHashes := make(map[string]bool)
		for _, hash := range r.testCaseHashes {
			currentHashes[hash] = true
		}

		// Build list of current model keys
		var currentModels []string
		models, _ := r.getModelsToTest() // Ignore error here - pruning is best-effort
		for _, model := range models {
			currentModels = append(currentModels, ModelKey(model.Provider, model.Model))
		}

		// Prune stale entries
		r.stateManager.PruneStaleHashes(currentHashes, currentModels)
	}

	return result, nil
}

// loadSuite loads and validates suite.yaml
func (r *Runner) loadSuite() error {
	suitePath := filepath.Join(r.suiteDir, "suite.yaml")

	data, err := os.ReadFile(suitePath)
	if err != nil {
		return &PathResolutionError{
			Path:    suitePath,
			Message: fmt.Sprintf("failed to read suite.yaml: %v", err),
		}
	}

	var suite Suite
	if err := yaml.Unmarshal(data, &suite); err != nil {
		return &SchemaValidationError{
			Message: fmt.Sprintf("failed to parse suite.yaml: %v", err),
		}
	}

	// Validate required fields
	if err := r.validateSuiteSchema(&suite); err != nil {
		return err
	}

	r.suite = &suite
	return nil
}

// validateSuiteSchema validates the suite configuration schema
func (r *Runner) validateSuiteSchema(suite *Suite) error {
	var errors []string

	if suite.Version == "" {
		errors = append(errors, "version is required")
	} else if suite.Version != "v1" {
		errors = append(errors, fmt.Sprintf("unsupported version %q, expected \"v1\"", suite.Version))
	}

	if suite.BundleID == "" {
		errors = append(errors, "bundle_id is required")
	}

	// At least one policy source must be configured
	if suite.Policies.CELRequestRules == "" &&
		suite.Policies.AIRequestRules == "" &&
		suite.Policies.CELResponseRules == "" &&
		suite.Policies.AIResponseRules == "" {
		errors = append(errors, "at least one policy source must be configured in policies")
	}

	// Validate acceptance thresholds
	if suite.Acceptance.MinMatchRate < 0 || suite.Acceptance.MinMatchRate > 1 {
		errors = append(errors, fmt.Sprintf("acceptance.min_match_rate must be between 0.0 and 1.0, got %f", suite.Acceptance.MinMatchRate))
	}

	// If AI engine is enabled, model_matrix must have at least one entry
	if suite.Engines.AI.Enabled && len(suite.Engines.AI.ModelMatrix) == 0 {
		errors = append(errors, "engines.ai.model_matrix must have at least one entry when AI engine is enabled")
	}

	// Validate model matrix entries
	for i, model := range suite.Engines.AI.ModelMatrix {
		if model.Provider == "" {
			errors = append(errors, fmt.Sprintf("engines.ai.model_matrix[%d].provider is required", i))
		} else if model.Provider != "openai" && model.Provider != "anthropic" && model.Provider != "openai_compatible" {
			errors = append(errors, fmt.Sprintf("engines.ai.model_matrix[%d].provider must be openai, anthropic, or openai_compatible", i))
		}
		if model.Model == "" {
			errors = append(errors, fmt.Sprintf("engines.ai.model_matrix[%d].model is required", i))
		}
	}

	if len(errors) > 0 {
		return &SchemaValidationError{
			Message: "suite.yaml validation failed",
			Details: errors,
		}
	}

	return nil
}

// discoverTestCases finds and parses all test case YAML files
func (r *Runner) discoverTestCases() error {
	casesDir := filepath.Join(r.suiteDir, "cases")

	// Check if cases directory exists
	info, err := os.Stat(casesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &PathResolutionError{
				Path:    casesDir,
				Message: "cases directory does not exist",
			}
		}
		return &PathResolutionError{
			Path:    casesDir,
			Message: fmt.Sprintf("failed to stat cases directory: %v", err),
		}
	}
	if !info.IsDir() {
		return &PathResolutionError{
			Path:    casesDir,
			Message: "cases path is not a directory",
		}
	}

	// Recursively find all .yaml files
	var caseFiles []string
	err = filepath.WalkDir(casesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml") {
			caseFiles = append(caseFiles, path)
		}
		return nil
	})
	if err != nil {
		return &PathResolutionError{
			Path:    casesDir,
			Message: fmt.Sprintf("failed to walk cases directory: %v", err),
		}
	}

	// Parse each test case file
	seenIDs := make(map[string]string) // case_id -> file path
	testCaseHashes := make(map[string]string) // case_id -> content hash
	testCaseFiles := make(map[string]string)  // case_id -> file path
	var testCases []TestCase

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

	r.testCases = testCases
	r.testCaseHashes = testCaseHashes
	r.testCaseFiles = testCaseFiles
	return nil
}

// parseTestCases parses a test case file which may contain one or multiple test cases.
// Supports both single test case (map) and multiple test cases (list) YAML formats.
func (r *Runner) parseTestCases(path string) ([]TestCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &PathResolutionError{
			Path:    path,
			Message: fmt.Sprintf("failed to read test case: %v", err),
		}
	}

	// Try parsing as a list of test cases first (multi-case format)
	var cases []TestCase
	if err := yaml.Unmarshal(data, &cases); err == nil && len(cases) > 0 {
		// Successfully parsed as list
		for i := range cases {
			// Apply defaults
			if cases[i].Phase == "" {
				cases[i].Phase = "request"
			}
			if cases[i].Engine == "" {
				cases[i].Engine = "both"
			}

			// Validate required fields
			if err := r.validateTestCaseSchema(&cases[i], path); err != nil {
				return nil, err
			}
		}
		return cases, nil
	}

	// Try parsing as a single test case (map format)
	var tc TestCase
	if err := yaml.Unmarshal(data, &tc); err != nil {
		return nil, &SchemaValidationError{
			Message: fmt.Sprintf("failed to parse test case %s: %v", path, err),
		}
	}

	// Apply defaults
	if tc.Phase == "" {
		tc.Phase = "request"
	}
	if tc.Engine == "" {
		tc.Engine = "both"
	}

	// Validate required fields
	if err := r.validateTestCaseSchema(&tc, path); err != nil {
		return nil, err
	}

	return []TestCase{tc}, nil
}

// validateTestCaseSchema validates a test case schema
func (r *Runner) validateTestCaseSchema(tc *TestCase, path string) error {
	var errors []string

	if tc.CaseID == "" {
		errors = append(errors, "case_id is required")
	}
	if tc.Title == "" {
		errors = append(errors, "title is required")
	}
	if tc.Phase != "request" && tc.Phase != "response" && tc.Phase != "both" {
		errors = append(errors, fmt.Sprintf("phase must be request, response, or both, got %q", tc.Phase))
	}
	if tc.Engine != "cel" && tc.Engine != "ai" && tc.Engine != "both" {
		errors = append(errors, fmt.Sprintf("engine must be cel, ai, or both, got %q", tc.Engine))
	}
	if tc.Request.ToolName == "" {
		errors = append(errors, "request.tool_name is required")
	}
	if tc.Expectations.Decision == "" {
		errors = append(errors, "expectations.decision is required")
	} else if tc.Expectations.Decision != "allow" && tc.Expectations.Decision != "deny" && tc.Expectations.Decision != "redact" {
		errors = append(errors, fmt.Sprintf("expectations.decision must be allow, deny, or redact, got %q", tc.Expectations.Decision))
	}

	// Response is required when phase includes response
	if (tc.Phase == "response" || tc.Phase == "both") && tc.Response == nil {
		errors = append(errors, "response is required when phase is response or both")
	}

	// Validate policy expectations
	for i, pe := range tc.Expectations.Policies {
		if pe.PolicyName == "" {
			errors = append(errors, fmt.Sprintf("expectations.policies[%d].policy_name is required", i))
		}
		if pe.Decision != "allow" && pe.Decision != "deny" && pe.Decision != "redact" {
			errors = append(errors, fmt.Sprintf("expectations.policies[%d].decision must be allow, deny, or redact", i))
		}
	}

	if len(errors) > 0 {
		return &SchemaValidationError{
			Message: fmt.Sprintf("test case validation failed: %s", path),
			Details: errors,
		}
	}

	return nil
}

// policyNameSets holds the names of loaded policies by category.
type policyNameSets struct {
	celRequest  map[string]bool
	aiRequest   map[string]bool
	celResponse map[string]bool
	aiResponse  map[string]bool
}

// validatePolicyIntegrity verifies that referenced policies exist.
// This prevents tests from silently passing when a referenced policy is deleted/renamed.
func (r *Runner) validatePolicyIntegrity() error {
	// Load all policy names from configured paths
	policyNames, err := r.loadPolicyNames()
	if err != nil {
		return err
	}

	// Cross-reference test cases against loaded policy names
	var errors []string
	for _, tc := range r.testCases {
		for _, pe := range tc.Expectations.Policies {
			if pe.PolicyName == "" {
				continue
			}

			// Determine which policy sets to check based on test case engine and phase
			found := false
			var checkedSets []string

			// Check against appropriate policy sets based on phase and engine
			if tc.Phase == "request" || tc.Phase == "both" || tc.Phase == "" {
				if tc.Engine == "cel" || tc.Engine == "both" || tc.Engine == "" {
					if policyNames.celRequest[pe.PolicyName] {
						found = true
					}
					checkedSets = append(checkedSets, "CEL request")
				}
				if tc.Engine == "ai" || tc.Engine == "both" || tc.Engine == "" {
					if policyNames.aiRequest[pe.PolicyName] {
						found = true
					}
					checkedSets = append(checkedSets, "AI request")
				}
			}
			if tc.Phase == "response" || tc.Phase == "both" {
				if tc.Engine == "cel" || tc.Engine == "both" || tc.Engine == "" {
					if policyNames.celResponse[pe.PolicyName] {
						found = true
					}
					checkedSets = append(checkedSets, "CEL response")
				}
				if tc.Engine == "ai" || tc.Engine == "both" || tc.Engine == "" {
					if policyNames.aiResponse[pe.PolicyName] {
						found = true
					}
					checkedSets = append(checkedSets, "AI response")
				}
			}

			if !found {
				errors = append(errors, fmt.Sprintf(
					"test case %q references policy %q but no such policy exists (checked: %s)",
					tc.CaseID, pe.PolicyName, strings.Join(checkedSets, ", "),
				))
			}
		}
	}

	if len(errors) > 0 {
		// Build helpful error message with loaded policy names
		var details strings.Builder
		details.WriteString("Policy integrity check failed:\n\n")
		for _, e := range errors {
			details.WriteString("  - ")
			details.WriteString(e)
			details.WriteString("\n")
		}
		details.WriteString("\nLoaded policies:\n")
		if len(policyNames.celRequest) > 0 {
			details.WriteString("  CEL request: ")
			details.WriteString(formatPolicyNames(policyNames.celRequest))
			details.WriteString("\n")
		}
		if len(policyNames.aiRequest) > 0 {
			details.WriteString("  AI request: ")
			details.WriteString(formatPolicyNames(policyNames.aiRequest))
			details.WriteString("\n")
		}
		if len(policyNames.celResponse) > 0 {
			details.WriteString("  CEL response: ")
			details.WriteString(formatPolicyNames(policyNames.celResponse))
			details.WriteString("\n")
		}
		if len(policyNames.aiResponse) > 0 {
			details.WriteString("  AI response: ")
			details.WriteString(formatPolicyNames(policyNames.aiResponse))
			details.WriteString("\n")
		}

		return &PolicyIntegrityError{
			Message: fmt.Sprintf("%d policy reference(s) not found", len(errors)),
			Details: details.String(),
		}
	}

	return nil
}

// loadPolicyNames loads all policies and extracts their names into sets.
func (r *Runner) loadPolicyNames() (*policyNameSets, error) {
	names := &policyNameSets{
		celRequest:  make(map[string]bool),
		aiRequest:   make(map[string]bool),
		celResponse: make(map[string]bool),
		aiResponse:  make(map[string]bool),
	}

	// Load CEL request policy names
	if r.suite.Policies.CELRequestRules != "" {
		policies, err := loadPoliciesFromPath(r.suite.Policies.CELRequestRules, r.suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load CEL request rules for integrity check: %w", err)
		}
		for _, p := range policies {
			names.celRequest[p.Name] = true
		}
	}

	// Load AI request policy names
	if r.suite.Policies.AIRequestRules != "" {
		policies, err := loadAIPoliciesFromPath(r.suite.Policies.AIRequestRules, r.suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load AI request rules for integrity check: %w", err)
		}
		for _, p := range policies {
			names.aiRequest[p.Name] = true
		}
	}

	// Load CEL response policy names
	if r.suite.Policies.CELResponseRules != "" {
		policies, err := loadResponsePoliciesFromPath(r.suite.Policies.CELResponseRules, r.suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load CEL response rules for integrity check: %w", err)
		}
		for _, p := range policies {
			names.celResponse[p.Name] = true
		}
	}

	// Load AI response policy names
	if r.suite.Policies.AIResponseRules != "" {
		policies, err := loadAIResponsePoliciesFromPath(r.suite.Policies.AIResponseRules, r.suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load AI response rules for integrity check: %w", err)
		}
		for _, p := range policies {
			names.aiResponse[p.Name] = true
		}
	}

	return names, nil
}

// computePolicyHashes computes SHA256 hashes of all policy files for cache invalidation.
// When policies change, cached test results must be invalidated even if test cases
// haven't changed. Without this, policyHashes is nil and hashesMatch(nil, nil) == true,
// so policy-only changes would never invalidate the cache.
func (r *Runner) computePolicyHashes() error {
	var hashes []string

	for _, path := range []string{
		r.suite.Policies.CELRequestRules,
		r.suite.Policies.AIRequestRules,
		r.suite.Policies.CELResponseRules,
		r.suite.Policies.AIResponseRules,
	} {
		if path == "" {
			continue
		}

		resolvedPath := resolvePath(path, r.suiteDir)
		fileHashes, err := hashPolicyPath(resolvedPath)
		if err != nil {
			return fmt.Errorf("failed to hash policy path %s: %w", path, err)
		}
		hashes = append(hashes, fileHashes...)
	}

	sort.Strings(hashes)
	r.policyHashes = hashes
	return nil
}

// hashPolicyPath computes hashes for a policy path (file or directory).
func hashPolicyPath(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", path, err)
	}

	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}
		return []string{ComputePolicyHash(data)}, nil
	}

	var hashes []string
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(p) != ".yaml" && filepath.Ext(p) != ".yml" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", p, err)
		}
		hashes = append(hashes, ComputePolicyHash(data))
		return nil
	})

	return hashes, err
}

// formatPolicyNames formats a set of policy names for display.
func formatPolicyNames(names map[string]bool) string {
	var list []string
	for name := range names {
		list = append(list, name)
	}
	if len(list) == 0 {
		return "(none)"
	}
	return strings.Join(list, ", ")
}

// policyInfo holds information about a loaded policy.
type policyInfo struct {
	name    string
	engine  string // "cel_request", "ai_request", "cel_response", "ai_response"
	enabled bool
}

// generateCoverageReport creates a coverage report comparing loaded policies against test cases.
func (r *Runner) generateCoverageReport() (*CoverageReport, error) {
	// Load all policies with their enabled status
	allPolicies, err := r.loadAllPoliciesWithStatus()
	if err != nil {
		return nil, err
	}

	// Build set of policies referenced by test cases
	referencedPolicies := make(map[string]bool)
	for _, tc := range r.testCases {
		for _, pe := range tc.Expectations.Policies {
			if pe.PolicyName != "" {
				referencedPolicies[pe.PolicyName] = true
			}
		}
	}

	// Build coverage report
	report := &CoverageReport{}
	var enabledPolicies []policyInfo

	for _, p := range allPolicies {
		// When --include-disabled is set, treat disabled policies as enabled for coverage
		effectivelyEnabled := p.enabled || r.opts.IncludeDisabled
		if effectivelyEnabled {
			enabledPolicies = append(enabledPolicies, p)
			report.TotalPolicies++
		} else {
			// Track disabled policies as skipped (only when not using --include-disabled)
			report.DisabledSkipped = append(report.DisabledSkipped, PolicyCoverageItem{
				Name:   p.name,
				Engine: p.engine,
			})
		}
	}

	// Find enabled policies without test coverage
	for _, p := range enabledPolicies {
		if referencedPolicies[p.name] {
			report.PoliciesWithTests++
		} else {
			report.PoliciesWithoutTests = append(report.PoliciesWithoutTests, PolicyCoverageItem{
				Name:   p.name,
				Engine: p.engine,
			})
		}
	}

	return report, nil
}

// loadAllPoliciesWithStatus loads all policies and returns them with enabled status.
func (r *Runner) loadAllPoliciesWithStatus() ([]policyInfo, error) {
	var allPolicies []policyInfo

	// Load CEL request policies
	if r.suite.Policies.CELRequestRules != "" {
		policies, err := loadPoliciesFromPath(r.suite.Policies.CELRequestRules, r.suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load CEL request rules: %w", err)
		}
		for _, p := range policies {
			allPolicies = append(allPolicies, policyInfo{
				name:    p.Name,
				engine:  "cel_request",
				enabled: p.IsEnabled(),
			})
		}
	}

	// Load AI request policies
	if r.suite.Policies.AIRequestRules != "" {
		policies, err := loadAIPoliciesFromPath(r.suite.Policies.AIRequestRules, r.suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load AI request rules: %w", err)
		}
		for _, p := range policies {
			allPolicies = append(allPolicies, policyInfo{
				name:    p.Name,
				engine:  "ai_request",
				enabled: p.IsEnabled(),
			})
		}
	}

	// Load CEL response policies
	if r.suite.Policies.CELResponseRules != "" {
		policies, err := loadResponsePoliciesFromPath(r.suite.Policies.CELResponseRules, r.suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load CEL response rules: %w", err)
		}
		for _, p := range policies {
			allPolicies = append(allPolicies, policyInfo{
				name:    p.Name,
				engine:  "cel_response",
				enabled: p.IsEnabled(),
			})
		}
	}

	// Load AI response policies
	if r.suite.Policies.AIResponseRules != "" {
		policies, err := loadAIResponsePoliciesFromPath(r.suite.Policies.AIResponseRules, r.suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load AI response rules: %w", err)
		}
		for _, p := range policies {
			allPolicies = append(allPolicies, policyInfo{
				name:    p.Name,
				engine:  "ai_response",
				enabled: p.IsEnabled(),
			})
		}
	}

	return allPolicies, nil
}

// executeTests runs the test cases and returns results
func (r *Runner) executeTests(ctx context.Context) (*RunResult, error) {
	// Create executor with loaded policies
	executor, err := NewExecutor(r.suite, r.testCases, r.suiteDir, r.opts.IncludeDisabled)
	if err != nil {
		return nil, err
	}

	// Filter test cases based on options
	cases := r.filterTestCases()

	// Determine which engines to run
	runCEL := r.shouldRunEngine("cel")
	runAI := r.shouldRunEngine("ai")

	// Set up streaming output to stdout (unless --quiet)
	var onProgress ProgressCallback
	isStreaming := !r.opts.Quiet

	if isStreaming {
		// Print header at start
		fmt.Printf("Policy Test Suite: %s\n", r.suite.BundleID)
		// Show how many test cases matched filters (especially useful when using --case-pattern)
		if r.opts.CasePattern != "" && r.opts.CasePattern != "*" {
			fmt.Printf("%d test case(s) matched pattern %q\n", len(cases), r.opts.CasePattern)
		}
		fmt.Printf("──────────────────────────────────────────────────\n\n")

		// Set up callback to print each result as it completes
		onProgress = func(result TestResult) {
			fmt.Print(formatSingleTestResult(result))
		}
	}

	var allResults []TestResult

	// Execute CEL tests - always run them since they're fast and deterministic
	if runCEL && r.suite.Engines.CEL.Enabled {
		celCases := filterCasesForEngine(cases, "cel")
		if isStreaming && len(celCases) > 0 {
			fmt.Print(formatSectionHeader("cel"))
		}
		celResults := executor.ExecuteCELTests(ctx, celCases, onProgress)
		allResults = append(allResults, celResults...)
	}

	// Execute AI tests
	if runAI && r.suite.Engines.AI.Enabled {
		aiCases := filterCasesForEngine(cases, "ai")

		// Filter out cases already processed by CEL (for engine: "both" cases)
		var aiOnlyCases []TestCase
		for _, tc := range aiCases {
			alreadyProcessed := false
			for _, result := range allResults {
				if result.CaseID == tc.CaseID {
					alreadyProcessed = true
					break
				}
			}
			if !alreadyProcessed {
				aiOnlyCases = append(aiOnlyCases, tc)
			}
		}

		if len(aiOnlyCases) > 0 {
			aiResults, err := r.executeAITests(ctx, aiOnlyCases, onProgress)
			if err != nil {
				return nil, err
			}
			allResults = append(allResults, aiResults...)
		}
	}

	// Calculate summary statistics
	result := r.calculateResults(allResults)

	// Output results based on format
	if err := r.outputResults(allResults, result, isStreaming); err != nil {
		return nil, err
	}

	return result, nil
}

// shouldRunEngine determines if a specific engine should be run.
func (r *Runner) shouldRunEngine(engine string) bool {
	if r.opts.Engine == "" || r.opts.Engine == "all" {
		return true
	}
	return r.opts.Engine == engine
}

// executeAITests runs AI tests against configured models.
func (r *Runner) executeAITests(ctx context.Context, cases []TestCase, onProgress ProgressCallback) ([]TestResult, error) {
	// Determine which models to test against
	models, err := r.getModelsToTest()
	if err != nil {
		return nil, err
	}

	if len(models) == 0 {
		// No models configured, skip all AI tests
		var results []TestResult
		for _, tc := range cases {
			result := TestResult{
				CaseID: tc.CaseID,
				Title:  tc.Title,
				Status: "skipped",
				Error: &TestError{
					Type:    "config_error",
					Message: "No AI models configured in model_matrix",
				},
			}
			results = append(results, result)
			if onProgress != nil {
				onProgress(result)
			}
		}
		return results, nil
	}

	var allResults []TestResult

	// Run tests against each model (or just the selected one)
	for _, model := range models {
		modelKey := ModelKey(model.Provider, model.Model)

		fmt.Printf("\n%s", formatSectionHeader(modelKey))

		// Create AI runner for this model
		runner, err := NewAITestRunner(model, r.suite, r.suiteDir, r.logger, r.rateLimiter)
		if err != nil {
			// If we can't create the runner, mark all cases as errored for this model
			for _, tc := range cases {
				result := TestResult{
					CaseID: tc.CaseID,
					Title:  tc.Title,
					Status: "errored",
					Error: &TestError{
						Type:    "config_error",
						Message: fmt.Sprintf("Failed to create AI runner for %s/%s: %v", model.Provider, model.Model, err),
					},
				}
				allResults = append(allResults, result)
				if onProgress != nil {
					onProgress(result)
				}
			}
			continue
		}

		// Filter cases based on state (skip cached passing tests unless --force)
		var casesToRun []TestCase
		var skippedCached []TestResult

		for _, tc := range cases {
			// Skip if test case doesn't target AI engine
			if tc.Engine != "ai" && tc.Engine != "both" {
				continue
			}

			contentHash := r.testCaseHashes[tc.CaseID]

			// Check if we should skip this test (valid cached result)
			if r.stateManager != nil && !r.opts.Force {
				if r.stateManager.ShouldSkip(contentHash, r.policyHashes, modelKey, r.opts.RetryFailed) {
					// Report as skipped (cached), including the previous status
					cachedStatus := r.stateManager.GetCachedStatus(contentHash, r.policyHashes, modelKey)
					result := TestResult{
						CaseID: tc.CaseID,
						Title:  tc.Title,
						Engine: "ai",
						Model:  modelKey,
						Status: "skipped",
						Error: &TestError{
							Type:    "cached",
							Message: fmt.Sprintf("cached %s", cachedStatus),
						},
					}
					skippedCached = append(skippedCached, result)
					if onProgress != nil {
						onProgress(result)
					}
					continue
				}
			}

			casesToRun = append(casesToRun, tc)
		}

		allResults = append(allResults, skippedCached...)

		// Apply max tests limit
		testsToRun := len(casesToRun)
		if r.opts.MaxTests > 0 && testsToRun > r.opts.MaxTests {
			testsToRun = r.opts.MaxTests
		}

		// Execute limited number of tests
		casesToRun = casesToRun[:testsToRun]

		// Create progress indicator for this model's tests (TTY-only animation)
		progressIndicator := NewTestProgressIndicator(os.Stdout, colorEnabled)

		// onStart fires before each test — starts the progress bar animation
		onStart := func(tc TestCase) {
			if onProgress == nil {
				return // no streaming output, skip animation
			}
			engineInfo := formatEngineInfo("ai", modelKey)
			var estimatedMs int64
			if r.stateManager != nil {
				contentHash := r.testCaseHashes[tc.CaseID]
				estimatedMs = r.stateManager.GetCachedDuration(contentHash, r.policyHashes, modelKey)
			}
			progressIndicator.Start(tc.CaseID, engineInfo, estimatedMs)
		}

		// Wrap progress callback to stop animation, record state, then print result
		wrappedProgress := func(result TestResult) {
			// Stop progress animation before printing result
			progressIndicator.Stop()

			// Record to state manager
			if r.stateManager != nil {
				contentHash := r.testCaseHashes[result.CaseID]
				cachedResult := &CachedResult{
					Status:     result.Status,
					Confidence: result.Actual.Confidence,
					LastRun:    time.Now(),
					DurationMs: result.ElapsedMs,
				}
				r.stateManager.RecordResult(contentHash, result.CaseID, r.policyHashes, modelKey, cachedResult)

				// Save state after each test (incremental)
				if err := r.stateManager.Save(); err != nil {
					// Log but don't fail - state persistence is best-effort
					fmt.Printf("Warning: failed to save state: %v\n", err)
				}
			}

			// Call original progress callback
			if onProgress != nil {
				onProgress(result)
			}
		}

		// Execute tests with this model
		modelResults := runner.ExecuteTests(ctx, casesToRun, onStart, wrappedProgress)
		allResults = append(allResults, modelResults...)
	}

	return allResults, nil
}

// getModelsToTest returns the models to test against based on CLI options.
// All returned models have their API keys and endpoints resolved from the
// provider config (if not set at the model level).
// Returns an error if --model flag specifies a provider not configured.
func (r *Runner) getModelsToTest() ([]ModelConfig, error) {
	// If --model flag is set, parse it and use only that model
	if r.opts.Model != "" {
		model := parseModelFlag(r.opts.Model, r.suite.Engines.AI.ModelMatrix)
		if model == nil {
			// Parse the provider from the flag for the error message
			parts := strings.SplitN(r.opts.Model, ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid --model format %q: expected provider:model", r.opts.Model)
			}
			provider := parts[0]
			// Check if provider exists in providers section
			if r.suite.Providers != nil {
				if _, ok := r.suite.Providers[provider]; ok {
					// Provider exists but model not in matrix - create config from provider
					model = &ModelConfig{
						Provider: provider,
						Model:    parts[1],
					}
				}
			}
			if model == nil {
				return nil, fmt.Errorf("provider %q not found in providers or model_matrix; add it to suite.yaml", provider)
			}
		}
		resolved := r.resolveModelConfig(*model)
		return []ModelConfig{resolved}, nil
	}

	// Filter to only enabled models and resolve their configs
	var enabledModels []ModelConfig
	for _, m := range r.suite.Engines.AI.ModelMatrix {
		if m.IsEnabled() {
			enabledModels = append(enabledModels, r.resolveModelConfig(m))
		}
	}

	// If --matrix flag is set, use all enabled models
	if r.opts.RunMatrix {
		return enabledModels, nil
	}

	// Default: use the first enabled model in the matrix
	if len(enabledModels) > 0 {
		return []ModelConfig{enabledModels[0]}, nil
	}

	return nil, nil
}

// resolveModelConfig returns a copy of the model with API key and endpoint
// resolved from the provider config if not set at the model level.
func (r *Runner) resolveModelConfig(m ModelConfig) ModelConfig {
	resolved := m
	// Resolve API key: model-level takes precedence over provider-level
	if resolved.APIKey == "" {
		resolved.APIKey = r.suite.ResolveAPIKey(m)
	}
	// Resolve endpoint: model-level takes precedence over provider-level
	if resolved.Endpoint == "" {
		resolved.Endpoint = r.suite.ResolveEndpoint(m)
	}
	return resolved
}

// parseModelFlag parses a model flag in the format "provider:model".
// It looks up settings from the model matrix:
//  1. First, look for an exact match (provider + model) - use that config entirely
//  2. If no exact match, look for any provider match to get endpoint, parameters, etc.
//  3. If no provider match, return nil (caller will check providers section)
//
// Note: API key resolution happens separately in resolveModelConfig using the
// providers section as fallback.
func parseModelFlag(flag string, modelMatrix []ModelConfig) *ModelConfig {
	parts := strings.SplitN(flag, ":", 2)
	if len(parts) != 2 {
		return nil
	}

	provider := parts[0]
	model := parts[1]

	// First pass: look for exact match (provider + model)
	for _, m := range modelMatrix {
		if m.Provider == provider && m.Model == model {
			// Return a copy with enabled forced to true (CLI override means use it)
			enabled := true
			return &ModelConfig{
				Provider:    m.Provider,
				Model:       m.Model,
				Endpoint:    m.Endpoint,
				APIKey:      m.APIKey,
				Parameters:  m.Parameters,
				QueryParams: m.QueryParams,
				Headers:     m.Headers,
				Enabled:     &enabled,
			}
		}
	}

	// Second pass: look for any provider match to get API key and settings
	for _, m := range modelMatrix {
		if m.Provider == provider {
			return &ModelConfig{
				Provider:    provider,
				Model:       model,
				Endpoint:    m.Endpoint,
				APIKey:      m.APIKey,
				Parameters:  m.Parameters,
				QueryParams: m.QueryParams,
				Headers:     m.Headers,
			}
		}
	}

	// No matching provider found - return nil so caller can error
	return nil
}

// filterTestCases filters test cases based on CLI options.
func (r *Runner) filterTestCases() []TestCase {
	var filtered []TestCase

	for _, tc := range r.testCases {
		// Apply tag filtering
		if r.opts.Tags != "" {
			tags := splitTags(r.opts.Tags)
			if !hasAllTags(tc.Tags, tags) {
				continue
			}
		}

		// Apply exclude tag filtering
		if r.opts.ExcludeTags != "" {
			excludeTags := splitTags(r.opts.ExcludeTags)
			if hasAnyTag(tc.Tags, excludeTags) {
				continue
			}
		}

		// Apply case pattern filtering (supports comma-separated patterns)
		if r.opts.CasePattern != "" && r.opts.CasePattern != "*" {
			patterns := strings.Split(r.opts.CasePattern, ",")
			matched := false
			for _, pattern := range patterns {
				pattern = strings.TrimSpace(pattern)
				if m, _ := filepath.Match(pattern, tc.CaseID); m {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		filtered = append(filtered, tc)
	}

	return filtered
}

// filterCasesForEngine returns cases that should run for a specific engine.
func filterCasesForEngine(cases []TestCase, engine string) []TestCase {
	var filtered []TestCase
	for _, tc := range cases {
		if tc.Engine == engine || tc.Engine == "both" {
			filtered = append(filtered, tc)
		}
	}
	return filtered
}

// splitTags splits a comma-separated tag string into a slice.
func splitTags(tags string) []string {
	if tags == "" {
		return nil
	}
	var result []string
	for _, tag := range strings.Split(tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			result = append(result, tag)
		}
	}
	return result
}

// hasAllTags returns true if tc has all the required tags.
func hasAllTags(tcTags, required []string) bool {
	tagSet := make(map[string]bool)
	for _, t := range tcTags {
		tagSet[t] = true
	}
	for _, r := range required {
		if !tagSet[r] {
			return false
		}
	}
	return true
}

// hasAnyTag returns true if tc has any of the excluded tags.
func hasAnyTag(tcTags, excluded []string) bool {
	tagSet := make(map[string]bool)
	for _, t := range tcTags {
		tagSet[t] = true
	}
	for _, e := range excluded {
		if tagSet[e] {
			return true
		}
	}
	return false
}

// calculateResults calculates summary statistics from test results.
func (r *Runner) calculateResults(results []TestResult) *RunResult {
	result := &RunResult{
		TotalCases: len(results),
	}

	for _, tr := range results {
		switch tr.Status {
		case "passed":
			result.Passed++
		case "failed":
			result.Failed++
		case "errored":
			result.Errored++
		case "skipped":
			result.Skipped++
			// Check if this was cached or rate limited
			if tr.Error != nil {
				switch tr.Error.Type {
				case "cached":
					result.SkippedCached++
				case "rate_limited":
					result.RateLimited++
				}
			}
		}
	}

	// Calculate match rate (passed / (total - skipped))
	evaluated := result.TotalCases - result.Skipped
	if evaluated > 0 {
		result.MatchRate = float64(result.Passed) / float64(evaluated)
	}

	// Check threshold
	minMatchRate := r.suite.Acceptance.MinMatchRate
	if minMatchRate == 0 {
		minMatchRate = 1.0 // Default to strict matching
	}
	result.ThresholdsMet = result.MatchRate >= minMatchRate

	// Calculate remaining tests (for --max-tests)
	if r.opts.MaxTests > 0 {
		totalFilteredCases := len(r.filterTestCases())
		// All outcomes are already categorized in TotalCases (passed + failed + errored + skipped).
		// Remaining = cases not yet accounted for in any outcome.
		result.Remaining = totalFilteredCases - result.TotalCases
		if result.Remaining < 0 {
			result.Remaining = 0
		}
		result.MoreTestsRemain = result.Remaining > 0
	}

	return result
}

// outputResults outputs the test results.
// If alreadyStreamed is true, only the summary is printed to stdout (results were already printed).
// If OutputFile is set, structured output (JSON/JUnit) is written to the file.
func (r *Runner) outputResults(results []TestResult, summary *RunResult, alreadyStreamed bool) error {
	// Generate coverage report from loaded policies and test cases
	coverage, err := r.generateCoverageReport()
	if err != nil {
		// Don't fail - coverage is informational only
		coverage = nil
	}

	// Build cross-model comparison table from results and cached state
	comparison := r.buildModelComparison(results)

	// Write to stdout (unless --quiet)
	if !r.opts.Quiet {
		var stdoutOutput string
		if alreadyStreamed {
			// Only print summary - header and results were already streamed
			stdoutOutput = formatTextSummary(r.suite, summary, results, coverage, comparison)
		} else {
			stdoutOutput = formatTextOutput(r.suite, results, summary, coverage, comparison)
		}
		fmt.Print(stdoutOutput)
	}

	// Write to file if specified
	if r.opts.OutputFile != "" {
		var fileOutput string
		var err error

		switch r.opts.OutputFormat {
		case "junit":
			fileOutput, err = formatJUnitOutput(r.suite, results, summary)
			if err != nil {
				return fmt.Errorf("failed to format JUnit output: %w", err)
			}
		default: // json is default for file output
			fileOutput, err = formatJSONOutput(r.suite, results, summary, coverage)
			if err != nil {
				return fmt.Errorf("failed to format JSON output: %w", err)
			}
		}

		if err := os.WriteFile(r.opts.OutputFile, []byte(fileOutput), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
	}

	return nil
}

// formatEngineInfo returns a formatted string showing engine and model info.
// Returns empty string if engine is not set.
func formatEngineInfo(engine, model string) string {
	if engine == "" {
		return ""
	}
	if model != "" {
		return fmt.Sprintf(" [%s:%s]", engine, model)
	}
	return fmt.Sprintf(" [%s]", engine)
}

// ANSI color/style codes for terminal output.
const (
	ansiReset     = "\033[0m"
	ansiBoldRed   = "\033[1;31m"
	ansiBoldGreen = "\033[1;32m"
	ansiBoldYellow = "\033[1;33m"
	ansiDim       = "\033[2m"
)

// colorEnabled controls whether ANSI color codes are emitted in text output.
// Automatically set based on whether stdout is a terminal and NO_COLOR is not set.
var colorEnabled = detectTerminal()

// detectTerminal returns true if stdout is an interactive terminal and color
// has not been disabled via the NO_COLOR environment variable.
func detectTerminal() bool {
	// Respect NO_COLOR convention (https://no-color.org/)
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// colorize wraps text with an ANSI color code, returning plain text when
// color output is disabled.
func colorize(code, text string) string {
	if !colorEnabled {
		return text
	}
	return code + text + ansiReset
}

// sectionHeaderWidth is the total character width for section header lines.
const sectionHeaderWidth = 50

// formatSectionHeader creates a visual separator like "── label ──────────────".
func formatSectionHeader(label string) string {
	// "── " + label + " " + remaining dashes + "\n"
	prefix := "── " + label + " "
	remaining := sectionHeaderWidth - utf8.RuneCountInString(prefix)
	if remaining < 2 {
		remaining = 2
	}
	return prefix + strings.Repeat("─", remaining) + "\n"
}

// formatPolicies formats policy results as a multi-line, column-aligned block.
// Triggering policies (whose decision matches the overall actual decision) are
// sorted first and marked with ►. The marker is green for expected triggering
// policies and yellow for unexpected ones. expectedPolicies is the set of policy
// names listed in the test case expectations (may be nil/empty).
func formatPolicies(policies []PolicyResult, actualDecision string, expectedPolicies map[string]bool) string {
	if len(policies) == 0 {
		return ""
	}

	// Determine max name length for column padding
	maxNameLen := 0
	for _, p := range policies {
		if len(p.PolicyName) > maxNameLen {
			maxNameLen = len(p.PolicyName)
		}
	}

	var sb strings.Builder
	sb.WriteString("    policies:\n")

	// For "allow" decisions, triggering markers are not meaningful — every policy
	// returns "allow" by default (it's the absence of deny, not an active action).
	// Just list all policies uniformly sorted alphabetically.
	if actualDecision == "allow" {
		sorted := make([]PolicyResult, len(policies))
		copy(sorted, policies)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].PolicyName < sorted[j].PolicyName
		})
		for _, p := range sorted {
			sb.WriteString(fmt.Sprintf("        %-*s  %-5s  %dms\n", maxNameLen, p.PolicyName, p.Decision, p.ElapsedMs))
		}
		return sb.String()
	}

	// For deny/redact: separate into triggering and non-triggering.
	// Triggering policies (whose decision matches actual) are sorted first with ► marker.
	var triggering, other []PolicyResult
	for _, p := range policies {
		if p.Decision == actualDecision {
			triggering = append(triggering, p)
		} else {
			other = append(other, p)
		}
	}

	sort.Slice(triggering, func(i, j int) bool {
		return triggering[i].PolicyName < triggering[j].PolicyName
	})
	sort.Slice(other, func(i, j int) bool {
		return other[i].PolicyName < other[j].PolicyName
	})

	// Write triggering policies first with colored ► marker
	for _, p := range triggering {
		marker := "►"
		if len(expectedPolicies) > 0 {
			if expectedPolicies[p.PolicyName] {
				marker = colorize(ansiBoldGreen, "►")
			} else {
				marker = colorize(ansiBoldYellow, "►")
			}
		}
		sb.WriteString(fmt.Sprintf("      %s %-*s  %-5s  %dms\n", marker, maxNameLen, p.PolicyName, p.Decision, p.ElapsedMs))
	}
	// Write remaining policies
	for _, p := range other {
		sb.WriteString(fmt.Sprintf("        %-*s  %-5s  %dms\n", maxNameLen, p.PolicyName, p.Decision, p.ElapsedMs))
	}

	return sb.String()
}

// formatReasoning formats a reasoning string for human output.
// Single-line reasoning is printed inline. Multi-line reasoning is truncated
// at the first newline with a "..." indicator.
func formatReasoning(reasoning string) string {
	if reasoning == "" {
		return ""
	}
	if idx := strings.IndexByte(reasoning, '\n'); idx >= 0 {
		return fmt.Sprintf("    reasoning: %s...\n", reasoning[:idx])
	}
	return fmt.Sprintf("    reasoning: %s\n", reasoning)
}

// formatRedacted formats redacted content for human output.
// Single-line content is printed inline. Multi-line content is indented below.
func formatRedacted(label, content string) string {
	if content == "" {
		return fmt.Sprintf("    %s: \n", label)
	}
	if !strings.Contains(content, "\n") {
		return fmt.Sprintf("    %s: %s\n", label, content)
	}
	// Multi-line: indent each line
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("    %s:\n", label))
	for _, line := range strings.Split(content, "\n") {
		sb.WriteString(fmt.Sprintf("      %s\n", line))
	}
	return sb.String()
}

// formatIndented formats a labeled block for human output.
// Single-line content is printed inline. Multi-line content has each line
// indented to align under the label.
func formatIndented(label, content string) string {
	if !strings.Contains(content, "\n") {
		return fmt.Sprintf("    %s: %s\n", label, content)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("    %s:\n", label))
	for _, line := range strings.Split(content, "\n") {
		sb.WriteString(fmt.Sprintf("      %s\n", line))
	}
	return sb.String()
}

// expectedPolicyNames builds a set of policy names from the test's expectations.
func expectedPolicyNames(expected ExpectedResult) map[string]bool {
	if len(expected.Policies) == 0 {
		return nil
	}
	names := make(map[string]bool, len(expected.Policies))
	for _, pe := range expected.Policies {
		names[pe.PolicyName] = true
	}
	return names
}

// formatSingleTestResult formats a single test result for streaming output.
func formatSingleTestResult(tr TestResult) string {
	var sb strings.Builder

	// Build engine/model suffix for display
	engineInfo := formatEngineInfo(tr.Engine, tr.Model)
	isCEL := tr.Engine == "cel"
	expectedNames := expectedPolicyNames(tr.Expected)

	// Helper: format the phase line (empty string if no phase set)
	formatPhaseLine := func() string {
		if tr.Phase == "" {
			return ""
		}
		return fmt.Sprintf("    phase: %s\n", tr.Phase)
	}

	// Helper: format the decision line, suppressing confidence for CEL
	formatDecisionLine := func() string {
		if isCEL {
			return fmt.Sprintf("    decision: expected: %s, actual: %s\n",
				tr.Expected.Decision, tr.Actual.Decision)
		}
		return fmt.Sprintf("    decision: expected: %s, actual: %s, confidence: %.1f\n",
			tr.Expected.Decision, tr.Actual.Decision, tr.Actual.Confidence)
	}

	// Helper: format warnings
	formatWarnings := func() {
		for _, w := range tr.Warnings {
			sb.WriteString(fmt.Sprintf("    %s %s\n", colorize(ansiBoldYellow, "WARNING:"), w))
		}
	}

	switch tr.Status {
	case "passed":
		icon := colorize(ansiBoldGreen, "✓")
		sb.WriteString(fmt.Sprintf("%s %s%s\n", icon, tr.CaseID, engineInfo))
		if tr.Title != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", tr.Title))
		}
		sb.WriteString(fmt.Sprintf("    elapsed: %dms\n", tr.ElapsedMs))
		sb.WriteString(formatPhaseLine())
		sb.WriteString(formatDecisionLine())
		sb.WriteString(formatPolicies(tr.Actual.PoliciesExecuted, tr.Actual.Decision, expectedNames))
		formatWarnings()
		// Show redacted content on success for redaction tests
		if tr.Expected.RedactedContent != "" || tr.Actual.RedactedContent != "" {
			sb.WriteString(formatRedacted("redacted expected", tr.Expected.RedactedContent))
			sb.WriteString(formatRedacted("redacted actual  ", tr.Actual.RedactedContent))
		}
	case "failed":
		icon := colorize(ansiBoldRed, "✗")
		sb.WriteString(fmt.Sprintf("%s %s%s\n", icon, tr.CaseID, engineInfo))
		if tr.Title != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", tr.Title))
		}
		sb.WriteString(fmt.Sprintf("    elapsed: %dms\n", tr.ElapsedMs))
		sb.WriteString(formatPhaseLine())
		sb.WriteString(formatDecisionLine())
		sb.WriteString(formatPolicies(tr.Actual.PoliciesExecuted, tr.Actual.Decision, expectedNames))
		// Show redacted content on failure only when meaningful:
		// - actual action was "redact" (so there's actual redacted content)
		// - OR both expected and actual have content (content mismatch case)
		showRedacted := tr.Actual.Decision == "redact" ||
			(tr.Expected.RedactedContent != "" && tr.Actual.RedactedContent != "")
		if showRedacted {
			sb.WriteString(formatRedacted("redacted expected", tr.Expected.RedactedContent))
			sb.WriteString(formatRedacted("redacted actual  ", tr.Actual.RedactedContent))
		}
		sb.WriteString(formatReasoning(tr.Actual.Reasoning))
		for _, f := range tr.Failures {
			// Colorize ► in failure messages to match the yellow policy markers above
			display := strings.ReplaceAll(f, "►", colorize(ansiBoldYellow, "►"))
			sb.WriteString(fmt.Sprintf("    FAILED: %s\n", display))
		}
	case "errored":
		icon := colorize(ansiBoldYellow, "⚠")
		sb.WriteString(fmt.Sprintf("%s %s%s\n", icon, tr.CaseID, engineInfo))
		if tr.Title != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", tr.Title))
		}
		sb.WriteString(fmt.Sprintf("    elapsed: %dms\n", tr.ElapsedMs))
		if tr.Error != nil {
			if strings.Contains(tr.Error.Message, "\n") {
				// Multi-line message: show type on its own line, then the best-formatted
				// body content. Details (from the underlying cause) typically has proper
				// JSON indentation; the message often doesn't.
				sb.WriteString(fmt.Sprintf("    ERROR: %s\n", tr.Error.Type))
				body := tr.Error.Message
				if tr.Error.Details != "" {
					body = tr.Error.Details
				}
				for _, line := range strings.Split(body, "\n") {
					sb.WriteString(fmt.Sprintf("      %s\n", line))
				}
			} else {
				sb.WriteString(fmt.Sprintf("    ERROR: %s - %s\n", tr.Error.Type, tr.Error.Message))
				// Show details only when they add info beyond what the message already contains
				if tr.Error.Details != "" && !strings.Contains(tr.Error.Message, tr.Error.Details) {
					sb.WriteString(formatIndented("details", tr.Error.Details))
				}
			}
		}
	case "skipped":
		icon := colorize(ansiDim, "○")
		sb.WriteString(fmt.Sprintf("%s %s%s (skipped)\n", icon, tr.CaseID, engineInfo))
		if tr.Error != nil {
			sb.WriteString(fmt.Sprintf("    reason: %s\n", tr.Error.Message))
		}
	}
	sb.WriteString("\n")

	return sb.String()
}

// formatTextSummary formats the summary section for text output.
// Includes results, thresholds, coverage, and slowest policies in one cohesive block.
func formatTextSummary(suite *Suite, summary *RunResult, results []TestResult, coverage *CoverageReport, comparison []ModelComparisonEntry) string {
	var sb strings.Builder

	sb.WriteString(formatSectionHeader("Summary"))
	sb.WriteString(fmt.Sprintf("Results: %d passed, %d failed, %d errored, %d skipped (%d total)\n",
		summary.Passed, summary.Failed, summary.Errored, summary.Skipped, summary.TotalCases))

	// Count cached pass/fail from individual results (derived at render time,
	// not stored in RunResult, to avoid denormalization that could drift).
	var cachedPassed, cachedFailed int
	if summary.SkippedCached > 0 {
		for _, tr := range results {
			if tr.Status == "skipped" && tr.Error != nil && tr.Error.Type == "cached" {
				if tr.Error.Message == "cached passed" {
					cachedPassed++
				} else {
					cachedFailed++
				}
			}
		}
		sb.WriteString(fmt.Sprintf("Cached:  %d skipped (%d passed, %d failed in last run)\n",
			summary.SkippedCached, cachedPassed, cachedFailed))
	}

	sb.WriteString(fmt.Sprintf("Match rate: %.1f%%\n", summary.MatchRate*100))

	if summary.ThresholdsMet {
		sb.WriteString("Thresholds: PASSED\n")
	} else {
		sb.WriteString(fmt.Sprintf("Thresholds: FAILED (min_match_rate: %.1f%% required, got %.1f%%)\n",
			suite.Acceptance.MinMatchRate*100, summary.MatchRate*100))
	}

	// Retry hint when there are failures or cached failures
	if summary.Failed > 0 || summary.Errored > 0 {
		sb.WriteString("\nTo retry failed/errored tests: --retry-failed\n")
	} else if cachedFailed > 0 {
		sb.WriteString("\nTo retry previously failed tests: --retry-failed\n")
	}

	// Policy coverage section
	if coverage != nil {
		coveragePercent := 0.0
		if coverage.TotalPolicies > 0 {
			coveragePercent = float64(coverage.PoliciesWithTests) / float64(coverage.TotalPolicies) * 100
		}
		sb.WriteString(fmt.Sprintf("\nPolicy coverage: %d/%d (%.0f%%)\n",
			coverage.PoliciesWithTests, coverage.TotalPolicies, coveragePercent))

		if len(coverage.PoliciesWithoutTests) > 0 {
			sb.WriteString(fmt.Sprintf("  Missing tests (%d):\n", len(coverage.PoliciesWithoutTests)))
			for _, p := range coverage.PoliciesWithoutTests {
				sb.WriteString(fmt.Sprintf("    - %s: %s\n", p.Engine, p.Name))
			}
		}

		if len(coverage.DisabledSkipped) > 0 {
			sb.WriteString(fmt.Sprintf("  Disabled (not tested): %d — use --include-disabled to include\n",
				len(coverage.DisabledSkipped)))
			for _, p := range coverage.DisabledSkipped {
				sb.WriteString(fmt.Sprintf("    - %s: %s\n", p.Engine, p.Name))
			}
		}
	}

	// Slowest policies section
	sb.WriteString(formatSlowestPolicies(results))

	// Model comparison table (shown when 2+ models have results)
	sb.WriteString(formatModelComparison(comparison))

	return sb.String()
}

// policyTiming aggregates timing data for a policy across all test runs.
type policyTiming struct {
	name    string
	totalMs int64
	maxMs   int64
	count   int
}

// formatSlowestPolicies returns a formatted string showing the top 5 slowest policies.
func formatSlowestPolicies(results []TestResult) string {
	// Aggregate timing data by policy name
	timings := make(map[string]*policyTiming)

	for _, tr := range results {
		for _, pr := range tr.Actual.PoliciesExecuted {
			if pr.PolicyName == "" {
				continue
			}
			t, exists := timings[pr.PolicyName]
			if !exists {
				t = &policyTiming{name: pr.PolicyName}
				timings[pr.PolicyName] = t
			}
			t.totalMs += pr.ElapsedMs
			t.count++
			if pr.ElapsedMs > t.maxMs {
				t.maxMs = pr.ElapsedMs
			}
		}
	}

	if len(timings) == 0 {
		return ""
	}

	// Convert to slice and sort by average time (descending)
	var sorted []policyTiming
	for _, t := range timings {
		sorted = append(sorted, *t)
	}

	// Sort by average time descending
	sort.Slice(sorted, func(i, j int) bool {
		avgI := sorted[i].totalMs / int64(sorted[i].count)
		avgJ := sorted[j].totalMs / int64(sorted[j].count)
		return avgJ < avgI
	})

	// Take top 5
	if len(sorted) > 5 {
		sorted = sorted[:5]
	}

	var sb strings.Builder
	sb.WriteString("\nSlowest policies (top 5):\n")

	const slowThresholdMs = 3000 // Mark policies over 3s as slow

	for _, t := range sorted {
		avgMs := t.totalMs / int64(t.count)
		slowMarker := ""
		if avgMs > slowThresholdMs {
			slowMarker = " ⚠"
		}
		sb.WriteString(fmt.Sprintf("  - %s: avg %dms, max %dms (%d runs)%s\n",
			t.name, avgMs, t.maxMs, t.count, slowMarker))
	}

	return sb.String()
}

// formatTextOutput formats results as human-readable text.
func formatTextOutput(suite *Suite, results []TestResult, summary *RunResult, coverage *CoverageReport, comparison []ModelComparisonEntry) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Policy Test Suite: %s\n", suite.BundleID))
	sb.WriteString("──────────────────────────────────────────────────\n\n")

	for _, tr := range results {
		sb.WriteString(formatSingleTestResult(tr))
	}

	sb.WriteString(formatTextSummary(suite, summary, results, coverage, comparison))

	return sb.String()
}

// buildModelComparison builds per-model comparison entries from current results
// and cached state. When a state manager is available, AI model data comes from
// the state file (which includes both current and previous run results). CEL data
// always comes from the current run since CEL results are not cached.
// Returns nil if fewer than 2 models have data (nothing to compare).
func (r *Runner) buildModelComparison(results []TestResult) []ModelComparisonEntry {
	// Track which models were tested in this run
	modelsInRun := make(map[string]bool)
	for _, tr := range results {
		if tr.Model != "" {
			modelsInRun[tr.Model] = true
		}
	}

	var entries []ModelComparisonEntry

	// CEL entry from current run results (CEL results are not cached in state)
	var celPassed, celFailed, celErrored int
	var celTotalMs int64
	hasCEL := false

	for _, tr := range results {
		if tr.Engine != "cel" {
			continue
		}
		hasCEL = true
		switch tr.Status {
		case "passed":
			celPassed++
		case "failed":
			celFailed++
		case "errored":
			celErrored++
		}
		celTotalMs += tr.ElapsedMs
	}

	if hasCEL {
		evaluated := celPassed + celFailed + celErrored
		entry := ModelComparisonEntry{
			Model:   "cel",
			Passed:  celPassed,
			Failed:  celFailed,
			Errored: celErrored,
			TotalMs: celTotalMs,
		}
		if evaluated > 0 {
			entry.MatchRate = float64(celPassed) / float64(evaluated)
			entry.AvgMs = celTotalMs / int64(evaluated)
		}
		entries = append(entries, entry)
	}

	// AI model entries: prefer state file (cumulative across runs) over current results
	if r.stateManager != nil {
		cachedSummaries := r.stateManager.GetModelSummaries(r.policyHashes)
		for modelKey, summary := range cachedSummaries {
			evaluated := summary.Passed + summary.Failed + summary.Errored
			entry := ModelComparisonEntry{
				Model:     modelKey,
				Passed:    summary.Passed,
				Failed:    summary.Failed,
				Errored:   summary.Errored,
				TotalMs:   summary.TotalMs,
				FromCache: !modelsInRun[modelKey],
			}
			if evaluated > 0 {
				entry.MatchRate = float64(summary.Passed) / float64(evaluated)
				entry.AvgMs = summary.TotalMs / int64(evaluated)
			}
			entries = append(entries, entry)
		}
	} else {
		// No state file — derive AI model data from current run results
		aiStats := make(map[string]*ModelComparisonEntry)
		for _, tr := range results {
			if tr.Engine != "ai" || tr.Model == "" {
				continue
			}

			entry, ok := aiStats[tr.Model]
			if !ok {
				entry = &ModelComparisonEntry{Model: tr.Model}
				aiStats[tr.Model] = entry
			}

			// For cached-skipped tests, count by their original status
			status := tr.Status
			if status == "skipped" && tr.Error != nil && tr.Error.Type == "cached" {
				status = strings.TrimPrefix(tr.Error.Message, "cached ")
			}

			switch status {
			case "passed":
				entry.Passed++
			case "failed":
				entry.Failed++
			case "errored":
				entry.Errored++
			}
			entry.TotalMs += tr.ElapsedMs
		}

		for _, entry := range aiStats {
			evaluated := entry.Passed + entry.Failed + entry.Errored
			if evaluated > 0 {
				entry.MatchRate = float64(entry.Passed) / float64(evaluated)
				entry.AvgMs = entry.TotalMs / int64(evaluated)
			}
			entries = append(entries, *entry)
		}
	}

	// Only show comparison when there are 2+ models to compare
	if len(entries) < 2 {
		return nil
	}

	// Sort: CEL first (baseline), then by match rate descending, then by name
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Model == "cel" {
			return true
		}
		if entries[j].Model == "cel" {
			return false
		}
		if entries[i].MatchRate != entries[j].MatchRate {
			return entries[i].MatchRate > entries[j].MatchRate
		}
		return entries[i].Model < entries[j].Model
	})

	return entries
}

// formatModelComparison renders the cross-model comparison table.
// Returns empty string if comparison is nil or empty.
func formatModelComparison(entries []ModelComparisonEntry) string {
	if len(entries) == 0 {
		return ""
	}

	// Calculate max model name width for column alignment
	maxModelLen := len("Model")
	for _, e := range entries {
		if len(e.Model) > maxModelLen {
			maxModelLen = len(e.Model)
		}
	}

	// Fixed column widths: "  Pass  Fail  Err  Match%   Avg ms    Total"
	// Build header to measure total width
	header := fmt.Sprintf("%-*s  %4s  %4s  %3s  %6s  %7s  %6s",
		maxModelLen, "Model", "Pass", "Fail", "Err", "Match%", "Avg ms", "Total")
	tableWidth := len(header)

	var sb strings.Builder

	// Top separator with label
	label := "Model Comparison"
	prefix := "\n── " + label + " "
	remaining := tableWidth - utf8.RuneCountInString(prefix) + 1 // +1 for leading \n
	if remaining < 2 {
		remaining = 2
	}
	sb.WriteString(prefix + strings.Repeat("─", remaining) + "\n")

	// Header row
	sb.WriteString(header + "\n")

	// Data rows
	hasFromCache := false
	for _, e := range entries {
		avgStr := formatDurationMs(e.AvgMs)
		totalStr := formatDurationSec(e.TotalMs)

		row := fmt.Sprintf("%-*s  %4d  %4d  %3d  %5.1f%%  %7s  %6s",
			maxModelLen, e.Model,
			e.Passed, e.Failed, e.Errored,
			e.MatchRate*100,
			avgStr, totalStr)

		if e.FromCache {
			hasFromCache = true
			sb.WriteString(colorize(ansiDim, row) + "\n")
		} else {
			sb.WriteString(row + "\n")
		}
	}

	// Bottom separator (full width)
	sb.WriteString(strings.Repeat("─", tableWidth) + "\n")

	// Footnote for cached entries
	if hasFromCache {
		sb.WriteString(colorize(ansiDim, "  (dimmed rows from previous run)") + "\n")
	}

	return sb.String()
}

// formatDurationMs formats milliseconds for the Avg ms column.
func formatDurationMs(ms int64) string {
	return fmt.Sprintf("%dms", ms)
}

// formatDurationSec formats milliseconds as seconds for the Total column.
func formatDurationSec(ms int64) string {
	return fmt.Sprintf("%.1fs", float64(ms)/1000.0)
}
