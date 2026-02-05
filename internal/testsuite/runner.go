package testsuite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	opts      RunnerOptions
	suite     *Suite
	testCases []TestCase
	suiteDir  string
	logger    *config.SessionLogger
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

	// If validate-only mode, return success
	if r.opts.ValidateOnly {
		fmt.Printf("Suite validation passed: %d test cases discovered\n", len(r.testCases))
		return &RunResult{
			ThresholdsMet: true,
			TotalCases:    len(r.testCases),
		}, nil
	}

	// Phase 4: Execute tests
	result, err := r.executeTests(ctx)
	if err != nil {
		return nil, err
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
	var testCases []TestCase

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
			testCases = append(testCases, tc)
		}
	}

	r.testCases = testCases
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

// validatePolicyIntegrity verifies that referenced policies exist
func (r *Runner) validatePolicyIntegrity() error {
	// TODO: Implement policy loading and cross-reference validation
	// For now, skip integrity validation until policy loading is implemented
	return nil
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
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		// Set up callback to print each result as it completes
		onProgress = func(result TestResult) {
			fmt.Print(formatSingleTestResult(result))
		}
	}

	var allResults []TestResult

	// Execute CEL tests
	if runCEL && r.suite.Engines.CEL.Enabled {
		celCases := filterCasesForEngine(cases, "cel")
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
	models := r.getModelsToTest()

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
		if model.Tier != "" {
			fmt.Printf("\nTesting with model: %s/%s (tier: %s)\n", model.Provider, model.Model, model.Tier)
		} else {
			fmt.Printf("\nTesting with model: %s/%s\n", model.Provider, model.Model)
		}

		// Create AI runner for this model
		runner, err := NewAITestRunner(model, r.suite, r.suiteDir, r.logger)
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

		// Execute tests with this model
		modelResults := runner.ExecuteTests(ctx, cases, onProgress)
		allResults = append(allResults, modelResults...)
	}

	return allResults, nil
}

// getModelsToTest returns the models to test against based on CLI options.
func (r *Runner) getModelsToTest() []ModelConfig {
	// If --model flag is set, parse it and use only that model
	if r.opts.Model != "" {
		model := parseModelFlag(r.opts.Model, r.suite.Engines.AI.ModelMatrix)
		if model != nil {
			return []ModelConfig{*model}
		}
		// Invalid model format, fall back to matrix
	}

	// If --matrix flag is set, use all models
	if r.opts.RunMatrix {
		return r.suite.Engines.AI.ModelMatrix
	}

	// Default: use the first model in the matrix
	if len(r.suite.Engines.AI.ModelMatrix) > 0 {
		return []ModelConfig{r.suite.Engines.AI.ModelMatrix[0]}
	}

	return nil
}

// parseModelFlag parses a model flag in the format "provider:model".
// It looks up API key and other settings from the model matrix if a matching provider exists,
// or falls back to default environment variables based on provider.
func parseModelFlag(flag string, modelMatrix []ModelConfig) *ModelConfig {
	parts := strings.SplitN(flag, ":", 2)
	if len(parts) != 2 {
		return nil
	}

	provider := parts[0]
	model := parts[1]

	// Look for a matching provider in the model matrix to get API key and other settings
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

	// Fall back to default environment variable based on provider
	var apiKeyEnvVar string
	switch provider {
	case "openai":
		apiKeyEnvVar = "${OPENAI_API_KEY}"
	case "anthropic":
		apiKeyEnvVar = "${ANTHROPIC_API_KEY}"
	default:
		// For unknown providers, try a generic pattern
		apiKeyEnvVar = fmt.Sprintf("${%s_API_KEY}", strings.ToUpper(provider))
	}

	return &ModelConfig{
		Provider: provider,
		Model:    model,
		APIKey:   apiKeyEnvVar,
	}
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

	return result
}

// outputResults outputs the test results.
// If alreadyStreamed is true, only the summary is printed to stdout (results were already printed).
// If OutputFile is set, structured output (JSON/JUnit) is written to the file.
func (r *Runner) outputResults(results []TestResult, summary *RunResult, alreadyStreamed bool) error {
	// TODO: Generate coverage report from loaded policies and test cases
	var coverage *CoverageReport

	// Write to stdout (unless --quiet)
	if !r.opts.Quiet {
		var stdoutOutput string
		if alreadyStreamed {
			// Only print summary - header and results were already streamed
			stdoutOutput = formatTextSummary(r.suite, summary, results)
		} else {
			stdoutOutput = formatTextOutput(r.suite, results, summary)
		}
		if coverage != nil {
			stdoutOutput += formatCoverageText(coverage)
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

// formatSingleTestResult formats a single test result for streaming output.
func formatSingleTestResult(tr TestResult) string {
	var sb strings.Builder

	switch tr.Status {
	case "passed":
		sb.WriteString(fmt.Sprintf("✓ %s (%dms)\n", tr.CaseID, tr.ElapsedMs))
		sb.WriteString(fmt.Sprintf("    expected: %s, got: %s (confidence: %.1f)\n",
			tr.Expected.Decision, tr.Actual.Decision, tr.Actual.Confidence))
		if len(tr.Actual.PoliciesExecuted) > 0 {
			sb.WriteString("    policies: ")
			for i, p := range tr.Actual.PoliciesExecuted {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s (%s, %dms)", p.PolicyName, p.Decision, p.ElapsedMs))
			}
			sb.WriteString("\n")
		}
	case "failed":
		sb.WriteString(fmt.Sprintf("✗ %s (%dms)\n", tr.CaseID, tr.ElapsedMs))
		sb.WriteString(fmt.Sprintf("    expected: %s, got: %s (confidence: %.1f)\n",
			tr.Expected.Decision, tr.Actual.Decision, tr.Actual.Confidence))
		if len(tr.Actual.PoliciesExecuted) > 0 {
			sb.WriteString("    policies: ")
			for i, p := range tr.Actual.PoliciesExecuted {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s (%s, %dms)", p.PolicyName, p.Decision, p.ElapsedMs))
			}
			sb.WriteString("\n")
		}
		if tr.Actual.Reasoning != "" {
			sb.WriteString(fmt.Sprintf("    reasoning: %q\n", tr.Actual.Reasoning))
		}
		for _, f := range tr.Failures {
			sb.WriteString(fmt.Sprintf("    FAILED: %s\n", f))
		}
	case "errored":
		sb.WriteString(fmt.Sprintf("⚠ %s (%dms)\n", tr.CaseID, tr.ElapsedMs))
		if tr.Error != nil {
			sb.WriteString(fmt.Sprintf("    ERROR: %s - %s\n", tr.Error.Type, tr.Error.Message))
			if tr.Error.Details != "" {
				sb.WriteString(fmt.Sprintf("    details: %s\n", tr.Error.Details))
			}
		}
	case "skipped":
		sb.WriteString(fmt.Sprintf("○ %s (skipped)\n", tr.CaseID))
		if tr.Error != nil {
			sb.WriteString(fmt.Sprintf("    reason: %s\n", tr.Error.Message))
		}
	}
	sb.WriteString("\n")

	return sb.String()
}

// formatTextSummary formats only the summary section for text output (when results were streamed).
func formatTextSummary(suite *Suite, summary *RunResult, results []TestResult) string {
	var sb strings.Builder

	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("Results: %d passed, %d failed, %d errored, %d skipped (%d total)\n",
		summary.Passed, summary.Failed, summary.Errored, summary.Skipped, summary.TotalCases))
	sb.WriteString(fmt.Sprintf("Match rate: %.1f%%\n", summary.MatchRate*100))

	if summary.ThresholdsMet {
		sb.WriteString("Thresholds: PASSED\n")
	} else {
		sb.WriteString(fmt.Sprintf("Thresholds: FAILED (min_match_rate: %.1f%% required, got %.1f%%)\n",
			suite.Acceptance.MinMatchRate*100, summary.MatchRate*100))
	}

	// Add slowest policies section
	sb.WriteString(formatSlowestPolicies(results))

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
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			avgI := sorted[i].totalMs / int64(sorted[i].count)
			avgJ := sorted[j].totalMs / int64(sorted[j].count)
			if avgJ > avgI {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

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
func formatTextOutput(suite *Suite, results []TestResult, summary *RunResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Policy Test Suite: %s\n", suite.BundleID))
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	for _, tr := range results {
		sb.WriteString(formatSingleTestResult(tr))
	}

	sb.WriteString(formatTextSummary(suite, summary, results))

	return sb.String()
}
