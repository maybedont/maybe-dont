package testsuite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/maybedont/maybe-dont/internal/gateway"
)

// TestResult represents the result of a single test case execution.
type TestResult struct {
	CaseID      string
	Title       string
	Phase       string // "request", "response", or "both"
	Engine      string // "cel" or "ai"
	Model       string // Model key for AI tests (e.g., "anthropic:claude-haiku"), empty for CEL
	Status      string // passed, failed, errored, skipped
	ElapsedMs   int64
	Expected    ExpectedResult
	Actual      ActualResult
	Failures    []string
	Warnings    []string // Non-fatal issues (e.g., unexpected triggering policies in non-strict mode)
	Error       *TestError
}

// ExpectedResult contains the expected outcomes from the test case.
type ExpectedResult struct {
	Decision        string
	Policies        []PolicyExpectation
	RedactedContent string // Expected content after redaction (for redact decision tests)
}

// ActualResult contains the actual outcomes from test execution.
type ActualResult struct {
	Decision         string
	Confidence       float64
	Reasoning        string
	PoliciesExecuted []PolicyResult
	RedactedContent  string // Actual content after redaction
}

// PolicyResult contains the result from a single policy evaluation.
type PolicyResult struct {
	PolicyName string
	Decision   string
	ElapsedMs  int64
	Reasoning  string
}

// TestError contains error details when a test errors.
type TestError struct {
	Type    string // timeout, api_error, etc.
	Message string
	Details string
}

// Executor runs test cases against loaded policies.
type Executor struct {
	suite           *Suite
	testCases       []TestCase
	celEngine       *gateway.CELPolicyEngine
	celResponseEngine *gateway.CELResponsePolicyEngine
	logger          *config.SessionLogger
	includeDisabled bool
}

// NewExecutor creates an executor for running test cases.
func NewExecutor(suite *Suite, testCases []TestCase, suiteDir string, includeDisabled bool) (*Executor, error) {
	// Create logger for the executor (respects MAYBE_DONT_LOGGER_LEVEL, defaults to info)
	sessionLogger := createTestLogger()

	executor := &Executor{
		suite:           suite,
		testCases:       testCases,
		logger:          sessionLogger,
		includeDisabled: includeDisabled,
	}

	// Load CEL request policies if configured
	if suite.Policies.CELRequestRules != "" {
		celEngine, err := gateway.NewCELPolicyEngine(context.Background(), sessionLogger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL engine: %w", err)
		}

		policies, err := loadPoliciesFromPath(suite.Policies.CELRequestRules, suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load CEL request rules: %w", err)
		}

		// Filter disabled policies unless includeDisabled is set
		if !includeDisabled {
			policies = filterEnabledPolicies(policies)
		}

		if err := celEngine.LoadPolicies(policies, "", includeDisabled); err != nil {
			return nil, fmt.Errorf("failed to load CEL policies into engine: %w", err)
		}

		executor.celEngine = celEngine
	}

	// Load CEL response policies if configured
	if suite.Policies.CELResponseRules != "" {
		celResponseEngine, err := gateway.NewCELResponsePolicyEngine(context.Background(), sessionLogger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL response engine: %w", err)
		}

		policies, err := loadResponsePoliciesFromPath(suite.Policies.CELResponseRules, suiteDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load CEL response rules: %w", err)
		}

		// Filter disabled policies unless includeDisabled is set
		if !includeDisabled {
			policies = filterEnabledResponsePolicies(policies)
		}

		if err := celResponseEngine.LoadPolicies(policies, "", includeDisabled); err != nil {
			return nil, fmt.Errorf("failed to load CEL response policies into engine: %w", err)
		}

		executor.celResponseEngine = celResponseEngine
	}

	return executor, nil
}

// loadPoliciesFromPath loads policies from a file or directory path.
func loadPoliciesFromPath(path, suiteDir string) ([]config.Policy, error) {
	resolvedPath := resolvePath(path, suiteDir)

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path %s: %w", resolvedPath, err)
	}

	if info.IsDir() {
		return loadPoliciesFromDirectory(resolvedPath)
	}

	return config.LoadPoliciesFromFile(resolvedPath)
}

// loadPoliciesFromDirectory loads all policy files from a directory recursively.
func loadPoliciesFromDirectory(dir string) ([]config.Policy, error) {
	var allPolicies []config.Policy

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}

		policies, err := config.LoadPoliciesFromFile(path)
		if err != nil {
			return fmt.Errorf("failed to load policies from %s: %w", path, err)
		}
		allPolicies = append(allPolicies, policies...)
		return nil
	})

	return allPolicies, err
}

// loadResponsePoliciesFromPath loads response policies from a file or directory path.
func loadResponsePoliciesFromPath(path, suiteDir string) ([]config.ResponsePolicy, error) {
	resolvedPath := resolvePath(path, suiteDir)

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path %s: %w", resolvedPath, err)
	}

	if info.IsDir() {
		return loadResponsePoliciesFromDirectory(resolvedPath)
	}

	return config.LoadResponsePoliciesFromFile(resolvedPath)
}

// loadResponsePoliciesFromDirectory loads all response policy files from a directory recursively.
func loadResponsePoliciesFromDirectory(dir string) ([]config.ResponsePolicy, error) {
	var allPolicies []config.ResponsePolicy

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}

		policies, err := config.LoadResponsePoliciesFromFile(path)
		if err != nil {
			return fmt.Errorf("failed to load response policies from %s: %w", path, err)
		}
		allPolicies = append(allPolicies, policies...)
		return nil
	})

	return allPolicies, err
}

// resolvePath resolves a path relative to the suite directory or repository root.
func resolvePath(path, suiteDir string) string {
	if filepath.IsAbs(path) {
		return path
	}

	// Try relative to suite directory first
	if len(path) >= 2 && path[:2] == "./" {
		return filepath.Join(suiteDir, path[2:])
	}

	// Try relative to repository root (current working directory)
	// This allows paths like "internal/config/defaults/cel_request_rules.yaml"
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Join(suiteDir, path)
	}

	// Check if path exists relative to cwd
	cwdPath := filepath.Join(cwd, path)
	if _, err := os.Stat(cwdPath); err == nil {
		return cwdPath
	}

	// Fall back to suite directory
	return filepath.Join(suiteDir, path)
}

// filterEnabledPolicies filters out disabled policies.
func filterEnabledPolicies(policies []config.Policy) []config.Policy {
	var enabled []config.Policy
	for _, p := range policies {
		if p.IsEnabled() {
			enabled = append(enabled, p)
		}
	}
	return enabled
}

// filterEnabledResponsePolicies filters out disabled response policies.
func filterEnabledResponsePolicies(policies []config.ResponsePolicy) []config.ResponsePolicy {
	var enabled []config.ResponsePolicy
	for _, p := range policies {
		if p.IsEnabled() {
			enabled = append(enabled, p)
		}
	}
	return enabled
}

// ExecuteCELTests runs test cases that target the CEL engine.
// If onProgress is provided, it's called after each test completes for streaming output.
func (e *Executor) ExecuteCELTests(ctx context.Context, cases []TestCase, onProgress ProgressCallback) []TestResult {
	var results []TestResult

	for _, tc := range cases {
		// Skip if test case doesn't target CEL engine
		if tc.Engine != "cel" && tc.Engine != "both" {
			continue
		}

		result := e.executeCELTest(ctx, tc)
		results = append(results, result)

		// Call progress callback for streaming output
		if onProgress != nil {
			onProgress(result)
		}
	}

	return results
}

// executeCELTest runs a single test case against the CEL engine.
func (e *Executor) executeCELTest(ctx context.Context, tc TestCase) TestResult {
	start := time.Now()

	result := TestResult{
		CaseID: tc.CaseID,
		Title:  tc.Title,
		Phase:  tc.Phase,
		Engine: "cel",
		Expected: ExpectedResult{
			Decision:        tc.Expectations.Decision,
			Policies:        tc.Expectations.Policies,
			RedactedContent: extractExpectedRedactedContent(tc.Expectations.RedactedContent),
		},
	}

	// Handle request validation
	if tc.Phase == "request" || tc.Phase == "both" {
		if e.celEngine == nil {
			result.Status = "errored"
			result.Error = &TestError{
				Type:    "config_error",
				Message: "CEL request engine not configured",
			}
			result.ElapsedMs = time.Since(start).Milliseconds()
			return result
		}

		// Build MCP request from test case
		req := buildCallToolRequest(tc.Request)

		// Evaluate against CEL engine
		validationResult, err := e.celEngine.EvaluateToolCall(ctx, req, nil)
		if err != nil {
			result.Status = "errored"
			result.Error = &TestError{
				Type:    "eval_error",
				Message: err.Error(),
			}
			result.ElapsedMs = time.Since(start).Milliseconds()
			return result
		}

		// Map validation result to actual result
		result.Actual = mapValidationResult(validationResult)
	}

	// Handle response validation
	if tc.Phase == "response" || tc.Phase == "both" {
		if e.celResponseEngine == nil {
			result.Status = "errored"
			result.Error = &TestError{
				Type:    "config_error",
				Message: "CEL response engine not configured",
			}
			result.ElapsedMs = time.Since(start).Milliseconds()
			return result
		}

		// Build MCP request and response from test case
		req := buildCallToolRequest(tc.Request)
		toolResult := buildCallToolResult(tc.Response)

		// Evaluate against CEL response engine
		validationResult, err := e.celResponseEngine.EvaluateResponse(ctx, req, &toolResult, nil)
		if err != nil {
			result.Status = "errored"
			result.Error = &TestError{
				Type:    "eval_error",
				Message: err.Error(),
			}
			result.ElapsedMs = time.Since(start).Milliseconds()
			return result
		}

		// For response validation, use the response result
		result.Actual = mapResponseValidationResult(validationResult)
	}

	result.ElapsedMs = time.Since(start).Milliseconds()

	// Compare expected vs actual
	cmp := compareResults(result.Expected, result.Actual, e.suite.Acceptance.IsStrictPolicyMatch())
	result.Failures = cmp.failures
	result.Warnings = cmp.warnings
	if len(result.Failures) == 0 {
		result.Status = "passed"
	} else {
		result.Status = "failed"
	}

	return result
}

// extractExpectedRedactedContent extracts the expected redacted content as a string.
// For now, we only support text content items and concatenate them.
func extractExpectedRedactedContent(items []ContentItem) string {
	if len(items) == 0 {
		return ""
	}
	var texts []string
	for _, item := range items {
		if item.Type == "text" && item.Text != "" {
			texts = append(texts, item.Text)
		}
	}
	if len(texts) == 0 {
		return ""
	}
	// For a single text item, return it directly
	if len(texts) == 1 {
		return texts[0]
	}
	// For multiple items, join with newlines
	return strings.Join(texts, "\n")
}

// buildCallToolRequest constructs an MCP CallToolRequest from test case request config.
func buildCallToolRequest(req RequestConfig) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params: mcp.CallToolParams{
			Name:      req.ToolName,
			Arguments: req.Arguments,
		},
	}
}

// buildCallToolResult constructs an MCP CallToolResult from test case response config.
func buildCallToolResult(resp *ResponseConfig) mcp.CallToolResult {
	if resp == nil {
		return mcp.CallToolResult{}
	}

	var content []mcp.Content
	for _, item := range resp.Content {
		switch item.Type {
		case "text":
			content = append(content, mcp.NewTextContent(item.Text))
		}
	}

	return mcp.CallToolResult{
		Content: content,
		IsError: resp.IsError,
	}
}

// mapValidationResult maps gateway ValidationResults to our ActualResult.
func mapValidationResult(vr gateway.ValidationResults) ActualResult {
	actual := ActualResult{
		Confidence: 1.0, // CEL is deterministic, always 100% confidence
	}

	// Determine decision from validation result
	if vr.Allowed {
		actual.Decision = "allow"
	} else {
		actual.Decision = "deny"
	}

	// Map per-policy results
	for _, pr := range vr.Results {
		actual.PoliciesExecuted = append(actual.PoliciesExecuted, PolicyResult{
			PolicyName: pr.PolicyName,
			Decision:   string(pr.Action),
			ElapsedMs:  pr.DurationMs,
		})
	}

	return actual
}

// mapResponseValidationResult maps gateway ResponseValidationResults to our ActualResult.
func mapResponseValidationResult(vr gateway.ResponseValidationResults) ActualResult {
	actual := ActualResult{
		Confidence: 1.0,
	}

	// Determine decision from RecommendedAction or Allowed
	if vr.RecommendedAction != "" {
		actual.Decision = string(vr.RecommendedAction)
	} else if vr.Allowed {
		actual.Decision = "allow"
	} else {
		actual.Decision = "deny"
	}

	// Capture redacted content if available
	if vr.RedactedContent != nil {
		actual.RedactedContent = *vr.RedactedContent
	}

	// Map per-policy results
	for _, pr := range vr.Results {
		actual.PoliciesExecuted = append(actual.PoliciesExecuted, PolicyResult{
			PolicyName: pr.PolicyName,
			Decision:   string(pr.Action),
			ElapsedMs:  pr.DurationMs,
		})
	}

	return actual
}

// compareResultsOutput holds both failures and warnings from result comparison.
type compareResultsOutput struct {
	failures []string
	warnings []string
}

// compareResults compares expected vs actual and returns failures and warnings.
// When strictPolicyMatch is true and policies are specified in expectations,
// any policy that triggers (matches the actual decision) but is NOT in the
// expected list causes a failure. When false, such cases produce warnings instead.
func compareResults(expected ExpectedResult, actual ActualResult, strictPolicyMatch bool) compareResultsOutput {
	var out compareResultsOutput

	// Compare overall action (allow/deny/redact)
	if expected.Decision != actual.Decision {
		out.failures = append(out.failures, fmt.Sprintf("expected %q, actual %q", expected.Decision, actual.Decision))
	}

	// Compare redacted content if expected (for redact decision tests)
	if expected.RedactedContent != "" {
		if actual.RedactedContent == "" {
			out.failures = append(out.failures, "expected redacted content but none was returned")
		} else if expected.RedactedContent != actual.RedactedContent {
			out.failures = append(out.failures, fmt.Sprintf("redacted content mismatch:\n  expected: %q\n  actual:   %q", expected.RedactedContent, actual.RedactedContent))
		}
	}

	// Compare per-policy expectations if specified
	expectedPolicies := make(map[string]string) // name -> expected decision
	for _, pe := range expected.Policies {
		expectedPolicies[pe.PolicyName] = pe.Decision

		found := false
		for _, pr := range actual.PoliciesExecuted {
			if pr.PolicyName == pe.PolicyName {
				found = true
				if pr.Decision != pe.Decision {
					out.failures = append(out.failures, fmt.Sprintf("policy %q: expected %q, actual %q", pe.PolicyName, pe.Decision, pr.Decision))
				}
				break
			}
		}
		if !found {
			out.failures = append(out.failures, fmt.Sprintf("policy %q not executed (check if enabled or conditions match)", pe.PolicyName))
		}
	}

	// Check for unexpected triggering policies.
	// Only meaningful for active actions (deny/redact) where specific policies drove the
	// decision. For "allow" tests, every policy returns "allow" by default — that's the
	// absence of any block, not a meaningful trigger.
	if len(expected.Policies) > 0 && actual.Decision != "allow" {
		var unexpected []string
		for _, pr := range actual.PoliciesExecuted {
			// A policy "triggers" if its decision matches the overall actual decision
			if pr.Decision != actual.Decision {
				continue // Non-triggering policies are fine
			}
			if _, expected := expectedPolicies[pr.PolicyName]; expected {
				continue // Expected policy
			}
			unexpected = append(unexpected, pr.PolicyName)
		}

		if len(unexpected) > 0 {
			msg := fmt.Sprintf("%d unexpected policy match(es) — see highlighted ► above", len(unexpected))
			if strictPolicyMatch {
				out.failures = append(out.failures, msg)
			} else {
				out.warnings = append(out.warnings, msg)
			}
		}
	}

	return out
}
