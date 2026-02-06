package testsuite

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// JSONOutput represents the full JSON output structure.
type JSONOutput struct {
	Suite          JSONSuiteInfo       `json:"suite"`
	PoliciesLoaded JSONPoliciesLoaded  `json:"policies_loaded"`
	ResultsByModel []JSONModelResults  `json:"results_by_model"`
	OverallSummary JSONOverallSummary  `json:"overall_summary"`
	Coverage       *JSONCoverage       `json:"coverage,omitempty"`
}

// JSONSuiteInfo contains suite metadata.
type JSONSuiteInfo struct {
	BundleID     string `json:"bundle_id"`
	Version      string `json:"version"`
	RunTimestamp string `json:"run_timestamp"`
}

// JSONPoliciesLoaded lists policies by engine type.
type JSONPoliciesLoaded struct {
	CELRequest  []string `json:"cel_request"`
	AIRequest   []string `json:"ai_request"`
	CELResponse []string `json:"cel_response"`
	AIResponse  []string `json:"ai_response"`
}

// JSONModelResults contains results for a single model.
type JSONModelResults struct {
	Model   JSONModelInfo    `json:"model"`
	Results []JSONTestResult `json:"results"`
	Summary JSONModelSummary `json:"summary"`
}

// JSONModelInfo contains model metadata.
type JSONModelInfo struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Tier     string `json:"tier,omitempty"`
}

// JSONTestResult contains a single test case result.
type JSONTestResult struct {
	CaseID    string              `json:"case_id"`
	Title     string              `json:"title"`
	Status    string              `json:"status"` // passed, failed, errored, skipped
	ElapsedMs int64               `json:"elapsed_ms"`
	Expected  *JSONExpected       `json:"expected,omitempty"`
	Actual    *JSONActual         `json:"actual,omitempty"`
	Failures  []string            `json:"failures,omitempty"`
	Error     *JSONError          `json:"error,omitempty"`
}

// JSONExpected contains expected outcomes.
type JSONExpected struct {
	Decision string              `json:"decision"`
	Policies []JSONPolicyExpect  `json:"policies,omitempty"`
}

// JSONPolicyExpect contains expected policy outcome.
type JSONPolicyExpect struct {
	PolicyName string `json:"policy_name"`
	Decision   string `json:"decision"`
}

// JSONActual contains actual outcomes.
type JSONActual struct {
	Decision         string             `json:"decision"`
	Confidence       float64            `json:"confidence"`
	Reasoning        string             `json:"reasoning,omitempty"`
	PoliciesExecuted []JSONPolicyResult `json:"policies_executed,omitempty"`
}

// JSONPolicyResult contains a single policy evaluation result.
type JSONPolicyResult struct {
	PolicyName string `json:"policy_name"`
	Decision   string `json:"decision"`
	Reasoning  string `json:"reasoning,omitempty"`
	ElapsedMs  int64  `json:"elapsed_ms"`
}

// JSONError contains error details.
type JSONError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// JSONModelSummary contains summary stats for a model.
type JSONModelSummary struct {
	TotalCases     int     `json:"total_cases"`
	Passed         int     `json:"passed"`
	Failed         int     `json:"failed"`
	Errored        int     `json:"errored"`
	Skipped        int     `json:"skipped"`
	MatchRate      float64 `json:"match_rate"`
	TotalElapsedMs int64   `json:"total_elapsed_ms"`
}

// JSONOverallSummary contains overall run summary.
type JSONOverallSummary struct {
	ModelsTested         int     `json:"models_tested"`
	ThresholdsMet        bool    `json:"thresholds_met"`
	MinMatchRateRequired float64 `json:"min_match_rate_required"`
	WorstMatchRate       float64 `json:"worst_match_rate"`
}

// JSONCoverage contains coverage information.
type JSONCoverage struct {
	TotalPolicies        int                    `json:"total_policies"`
	PoliciesWithTests    int                    `json:"policies_with_tests"`
	PoliciesWithoutTests []JSONPolicyCoverage   `json:"policies_without_tests,omitempty"`
	DisabledSkipped      []JSONPolicyCoverage   `json:"disabled_policies_skipped,omitempty"`
}

// JSONPolicyCoverage represents a policy in coverage report.
type JSONPolicyCoverage struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
}

// formatJSONOutput formats results as JSON.
func formatJSONOutput(suite *Suite, results []TestResult, summary *RunResult, coverage *CoverageReport) (string, error) {
	output := JSONOutput{
		Suite: JSONSuiteInfo{
			BundleID:     suite.BundleID,
			Version:      suite.Version,
			RunTimestamp: time.Now().UTC().Format(time.RFC3339),
		},
		PoliciesLoaded: JSONPoliciesLoaded{
			CELRequest:  []string{}, // TODO: populate from loaded policies
			AIRequest:   []string{},
			CELResponse: []string{},
			AIResponse:  []string{},
		},
	}

	// Group results by model (for now, single model)
	modelResults := JSONModelResults{
		Model: JSONModelInfo{
			Provider: "cel", // Default for CEL-only runs
			Model:    "deterministic",
		},
		Results: make([]JSONTestResult, 0, len(results)),
	}

	var totalElapsed int64
	for _, r := range results {
		jr := JSONTestResult{
			CaseID:    r.CaseID,
			Title:     r.Title,
			Status:    r.Status,
			ElapsedMs: r.ElapsedMs,
			Failures:  r.Failures,
		}

		if r.Expected.Decision != "" {
			jr.Expected = &JSONExpected{
				Decision: r.Expected.Decision,
			}
			for _, pe := range r.Expected.Policies {
				jr.Expected.Policies = append(jr.Expected.Policies, JSONPolicyExpect(pe))
			}
		}

		if r.Actual.Decision != "" {
			jr.Actual = &JSONActual{
				Decision:   r.Actual.Decision,
				Confidence: r.Actual.Confidence,
				Reasoning:  r.Actual.Reasoning,
			}
			for _, pr := range r.Actual.PoliciesExecuted {
				jr.Actual.PoliciesExecuted = append(jr.Actual.PoliciesExecuted, JSONPolicyResult{
					PolicyName: pr.PolicyName,
					Decision:   pr.Decision,
					Reasoning:  pr.Reasoning,
					ElapsedMs:  pr.ElapsedMs,
				})
			}
		}

		if r.Error != nil {
			jr.Error = &JSONError{
				Type:    r.Error.Type,
				Message: r.Error.Message,
				Details: r.Error.Details,
			}
		}

		modelResults.Results = append(modelResults.Results, jr)
		totalElapsed += r.ElapsedMs
	}

	modelResults.Summary = JSONModelSummary{
		TotalCases:     summary.TotalCases,
		Passed:         summary.Passed,
		Failed:         summary.Failed,
		Errored:        summary.Errored,
		Skipped:        summary.Skipped,
		MatchRate:      summary.MatchRate,
		TotalElapsedMs: totalElapsed,
	}

	output.ResultsByModel = []JSONModelResults{modelResults}

	minMatchRate := suite.Acceptance.MinMatchRate
	if minMatchRate == 0 {
		minMatchRate = 1.0
	}

	output.OverallSummary = JSONOverallSummary{
		ModelsTested:         1,
		ThresholdsMet:        summary.ThresholdsMet,
		MinMatchRateRequired: minMatchRate,
		WorstMatchRate:       summary.MatchRate,
	}

	// Add coverage if available
	if coverage != nil {
		output.Coverage = &JSONCoverage{
			TotalPolicies:     coverage.TotalPolicies,
			PoliciesWithTests: coverage.PoliciesWithTests,
		}
		for _, p := range coverage.PoliciesWithoutTests {
			output.Coverage.PoliciesWithoutTests = append(output.Coverage.PoliciesWithoutTests, JSONPolicyCoverage(p))
		}
		for _, p := range coverage.DisabledSkipped {
			output.Coverage.DisabledSkipped = append(output.Coverage.DisabledSkipped, JSONPolicyCoverage(p))
		}
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return string(data), nil
}

// JUnit XML structures

// JUnitTestSuites is the root element for JUnit XML.
type JUnitTestSuites struct {
	XMLName   xml.Name         `xml:"testsuites"`
	Name      string           `xml:"name,attr"`
	Tests     int              `xml:"tests,attr"`
	Failures  int              `xml:"failures,attr"`
	Errors    int              `xml:"errors,attr"`
	Time      float64          `xml:"time,attr"`
	TestSuite []JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite represents a single test suite (one per model).
type JUnitTestSuite struct {
	Name       string           `xml:"name,attr"`
	Tests      int              `xml:"tests,attr"`
	Failures   int              `xml:"failures,attr"`
	Errors     int              `xml:"errors,attr"`
	Skipped    int              `xml:"skipped,attr"`
	Time       float64          `xml:"time,attr"`
	Properties []JUnitProperty  `xml:"properties>property,omitempty"`
	TestCases  []JUnitTestCase  `xml:"testcase"`
}

// JUnitProperty represents a property in the test suite.
type JUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// JUnitTestCase represents a single test case.
type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Error     *JUnitError   `xml:"error,omitempty"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
}

// JUnitFailure represents a test failure.
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr,omitempty"`
	Content string `xml:",chardata"`
}

// JUnitError represents a test error.
type JUnitError struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr,omitempty"`
	Content string `xml:",chardata"`
}

// JUnitSkipped represents a skipped test.
type JUnitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// formatJUnitOutput formats results as JUnit XML.
func formatJUnitOutput(suite *Suite, results []TestResult, summary *RunResult) (string, error) {
	var totalTime float64
	for _, r := range results {
		totalTime += float64(r.ElapsedMs) / 1000.0
	}

	testSuites := JUnitTestSuites{
		Name:     "policy-test-suite",
		Tests:    summary.TotalCases,
		Failures: summary.Failed,
		Errors:   summary.Errored,
		Time:     totalTime,
	}

	// Create a single test suite (for now - will expand for model matrix)
	ts := JUnitTestSuite{
		Name:     fmt.Sprintf("policies/%s", suite.BundleID),
		Tests:    summary.TotalCases,
		Failures: summary.Failed,
		Errors:   summary.Errored,
		Skipped:  summary.Skipped,
		Time:     totalTime,
		Properties: []JUnitProperty{
			{Name: "bundle_id", Value: suite.BundleID},
			{Name: "version", Value: suite.Version},
		},
	}

	for _, r := range results {
		tc := JUnitTestCase{
			Name:      r.CaseID,
			ClassName: fmt.Sprintf("policies.%s", r.Expected.Decision),
			Time:      float64(r.ElapsedMs) / 1000.0,
		}

		switch r.Status {
		case "failed":
			var failureContent strings.Builder
			failureContent.WriteString(fmt.Sprintf("Expected: %s\n", r.Expected.Decision))
			failureContent.WriteString(fmt.Sprintf("Actual: %s\n", r.Actual.Decision))
			if len(r.Actual.PoliciesExecuted) > 0 {
				failureContent.WriteString("\nPolicy results:\n")
				for _, p := range r.Actual.PoliciesExecuted {
					failureContent.WriteString(fmt.Sprintf("  - %s: %s (%dms)\n", p.PolicyName, p.Decision, p.ElapsedMs))
				}
			}
			for _, f := range r.Failures {
				failureContent.WriteString(fmt.Sprintf("\n%s", f))
			}

			tc.Failure = &JUnitFailure{
				Message: fmt.Sprintf("Expected '%s', actual '%s'", r.Expected.Decision, r.Actual.Decision),
				Type:    "AssertionError",
				Content: failureContent.String(),
			}

		case "errored":
			var errorContent strings.Builder
			if r.Error != nil {
				errorContent.WriteString(fmt.Sprintf("Type: %s\n", r.Error.Type))
				errorContent.WriteString(fmt.Sprintf("Message: %s\n", r.Error.Message))
				if r.Error.Details != "" {
					errorContent.WriteString(fmt.Sprintf("Details: %s\n", r.Error.Details))
				}
			}

			tc.Error = &JUnitError{
				Message: r.Error.Message,
				Type:    r.Error.Type,
				Content: errorContent.String(),
			}

		case "skipped":
			message := ""
			if r.Error != nil {
				message = r.Error.Message
			}
			tc.Skipped = &JUnitSkipped{
				Message: message,
			}
		}

		ts.TestCases = append(ts.TestCases, tc)
	}

	testSuites.TestSuite = []JUnitTestSuite{ts}

	// Marshal with XML header
	data, err := xml.MarshalIndent(testSuites, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JUnit XML: %w", err)
	}

	return xml.Header + string(data), nil
}

// CoverageReport contains coverage analysis data.
type CoverageReport struct {
	TotalPolicies        int
	PoliciesWithTests    int
	PoliciesWithoutTests []PolicyCoverageItem
	DisabledSkipped      []PolicyCoverageItem
}

// PolicyCoverageItem represents a policy in the coverage report.
type PolicyCoverageItem struct {
	Name   string
	Engine string
}

// formatCoverageText formats coverage report as text.
func formatCoverageText(coverage *CoverageReport) string {
	if coverage == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\nCoverage Report\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	if len(coverage.PoliciesWithoutTests) > 0 {
		sb.WriteString(fmt.Sprintf("Policies without test coverage (%d):\n", len(coverage.PoliciesWithoutTests)))
		for _, p := range coverage.PoliciesWithoutTests {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", p.Engine, p.Name))
		}
		sb.WriteString("\n")
	}

	if len(coverage.DisabledSkipped) > 0 {
		sb.WriteString(fmt.Sprintf("Disabled policies (skipped): %d\n", len(coverage.DisabledSkipped)))
		for _, p := range coverage.DisabledSkipped {
			sb.WriteString(fmt.Sprintf("  - %s: %s (enabled: false)\n", p.Engine, p.Name))
		}
		sb.WriteString("\nUse --include-disabled to test these policies.\n")
	}

	coveragePercent := 0.0
	if coverage.TotalPolicies > 0 {
		coveragePercent = float64(coverage.PoliciesWithTests) / float64(coverage.TotalPolicies) * 100
	}
	sb.WriteString(fmt.Sprintf("\nPolicies with test coverage: %d/%d (%.0f%%)\n",
		coverage.PoliciesWithTests, coverage.TotalPolicies, coveragePercent))

	return sb.String()
}
