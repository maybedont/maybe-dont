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
	Suite           JSONSuiteInfo                `json:"suite"`
	PoliciesLoaded  JSONPoliciesLoaded           `json:"policies_loaded"`
	ResultsByModel  []JSONModelResults           `json:"results_by_model"`
	OverallSummary  JSONOverallSummary           `json:"overall_summary"`
	ModelComparison []JSONModelComparisonEntry   `json:"model_comparison,omitempty"`
	Coverage        *JSONCoverage                `json:"coverage,omitempty"`
}

// JSONModelComparisonEntry holds per-model summary stats including historical cached models.
type JSONModelComparisonEntry struct {
	Model           string  `json:"model"`
	Passed          int     `json:"passed"`
	Failed          int     `json:"failed"`
	ExtraPolicyOnly int     `json:"extra_policy_only,omitempty"`
	Errored         int     `json:"errored"`
	MatchRate       float64 `json:"match_rate"`
	StrictMatchRate float64 `json:"strict_match_rate,omitempty"`
	AvgMs           int64   `json:"avg_ms"`
	TotalMs         int64   `json:"total_ms"`
	FromCache       bool    `json:"from_cache"`
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
	Failures        []string `json:"failures,omitempty"`
	ExtraPolicyOnly bool     `json:"extra_policy_only,omitempty"`
	Error           *JSONError `json:"error,omitempty"`

	// Pass rate fields (present only when history has 2+ entries)
	PassRate                     *float64 `json:"pass_rate,omitempty"`
	PassRateRuns                 *int     `json:"pass_rate_runs,omitempty"`
	PassRateSincePolicyChange    *float64 `json:"pass_rate_since_policy_change,omitempty"`
	PassRateSinceChangeRuns      *int     `json:"pass_rate_since_policy_change_runs,omitempty"`
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
	TotalCases      int      `json:"total_cases"`
	Passed          int      `json:"passed"`
	Failed          int      `json:"failed"`
	ExtraPolicyOnly int      `json:"extra_policy_only,omitempty"`
	Errored         int      `json:"errored"`
	Skipped         int      `json:"skipped"`
	MatchRate       float64  `json:"match_rate"`
	StrictMatchRate float64  `json:"strict_match_rate,omitempty"`
	TotalElapsedMs  int64    `json:"total_elapsed_ms"`
	Stability       *float64 `json:"stability,omitempty"`       // Mean pass rate across tests with 3+ history entries
	StabilityTests  *int     `json:"stability_tests,omitempty"` // Number of tests used for stability calculation
}

// JSONOverallSummary contains overall run summary.
// Aggregate counts use the policy-quality view: cached results are counted
// by their original status (e.g., "cached passed" → "passed"), so these
// numbers reflect cumulative policy quality across all runs.
type JSONOverallSummary struct {
	ModelsTested         int     `json:"models_tested"`
	TotalCases           int     `json:"total_cases"`
	Passed               int     `json:"passed"`
	Failed               int     `json:"failed"`
	ExtraPolicyOnly      int     `json:"extra_policy_only,omitempty"`
	Errored              int     `json:"errored"`
	Skipped              int     `json:"skipped"`
	MatchRate            float64 `json:"match_rate"`
	StrictMatchRate      float64 `json:"strict_match_rate,omitempty"`
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
func formatJSONOutput(suite *Suite, results []TestResult, summary *RunResult, coverage *CoverageReport, comparison []ModelComparisonEntry) (string, error) {
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

	// Group results by model key (Engine:Model or "cel" for deterministic)
	type modelBucket struct {
		info            JSONModelInfo
		results         []JSONTestResult
		passed          int
		failed          int
		extraPolicyOnly int
		errored         int
		skipped         int
		totalMs         int64
	}

	buckets := make(map[string]*modelBucket)
	bucketOrder := []string{} // preserve insertion order

	for _, r := range results {
		// Determine model key for grouping
		key := "cel"
		info := JSONModelInfo{Provider: "cel", Model: "deterministic"}
		if r.Engine == "ai" && r.Model != "" {
			key = r.Model
			parts := strings.SplitN(r.Model, ":", 2)
			if len(parts) == 2 {
				info = JSONModelInfo{Provider: parts[0], Model: parts[1]}
			} else {
				info = JSONModelInfo{Provider: r.Model, Model: r.Model}
			}
		}

		b, ok := buckets[key]
		if !ok {
			b = &modelBucket{info: info}
			buckets[key] = b
			bucketOrder = append(bucketOrder, key)
		}

		jr := JSONTestResult{
			CaseID:          r.CaseID,
			Title:           r.Title,
			Status:          r.Status,
			ElapsedMs:       r.ElapsedMs,
			Failures:        r.Failures,
			ExtraPolicyOnly: r.ExtraPolicyOnly,
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

		// Populate pass rate fields when history data is available
		if r.PassRateRuns >= 2 {
			rate := r.PassRate
			runs := r.PassRateRuns
			jr.PassRate = &rate
			jr.PassRateRuns = &runs
			if r.PassRateSinceChangeRuns > 0 && r.PassRateSinceChangeRuns < r.PassRateRuns {
				sinceRate := r.PassRateSinceChange
				sinceRuns := r.PassRateSinceChangeRuns
				jr.PassRateSincePolicyChange = &sinceRate
				jr.PassRateSinceChangeRuns = &sinceRuns
			}
		}

		b.results = append(b.results, jr)
		b.totalMs += r.ElapsedMs

		// Use effective status so cached results count toward policy quality
		// (e.g., "cached passed" → "passed")
		switch effectiveStatus(r) {
		case "passed":
			b.passed++
		case "failed":
			if r.ExtraPolicyOnly {
				b.extraPolicyOnly++
			} else {
				b.failed++
			}
		case "errored":
			b.errored++
		case "skipped":
			b.skipped++
		}
	}

	// Build a lookup from comparison entries for stability data
	comparisonByModel := make(map[string]ModelComparisonEntry)
	for _, c := range comparison {
		comparisonByModel[c.Model] = c
	}

	// Build ResultsByModel in insertion order and accumulate aggregate totals
	worstMatchRate := 1.0
	var aggPassed, aggFailed, aggErrored, aggSkipped, aggExtraPolicyOnly int
	for _, key := range bucketOrder {
		b := buckets[key]
		total := b.passed + b.failed + b.extraPolicyOnly + b.errored + b.skipped
		// Match rate excludes errored tests — errors are infrastructure issues, not policy failures
		decided := b.passed + b.failed + b.extraPolicyOnly
		matchRate := 0.0
		strictMatchRate := 0.0
		if decided > 0 {
			matchRate = float64(b.passed+b.extraPolicyOnly) / float64(decided)
			strictMatchRate = float64(b.passed) / float64(decided)
		}
		if matchRate < worstMatchRate {
			worstMatchRate = matchRate
		}

		aggPassed += b.passed
		aggFailed += b.failed
		aggExtraPolicyOnly += b.extraPolicyOnly
		aggErrored += b.errored
		aggSkipped += b.skipped

		modelSummary := JSONModelSummary{
			TotalCases:      total,
			Passed:          b.passed,
			Failed:          b.failed,
			ExtraPolicyOnly: b.extraPolicyOnly,
			Errored:         b.errored,
			Skipped:         b.skipped,
			MatchRate:       matchRate,
			StrictMatchRate:    strictMatchRate,
			TotalElapsedMs:  b.totalMs,
		}

		// Add stability data from comparison entries
		if c, ok := comparisonByModel[key]; ok && c.StabilityTests > 0 {
			stab := c.Stability
			stabTests := c.StabilityTests
			modelSummary.Stability = &stab
			modelSummary.StabilityTests = &stabTests
		}

		output.ResultsByModel = append(output.ResultsByModel, JSONModelResults{
			Model:   b.info,
			Results: b.results,
			Summary: modelSummary,
		})
	}

	minMatchRate := suite.Acceptance.MinMatchRate
	if minMatchRate == 0 {
		minMatchRate = 1.0
	}

	aggTotal := aggPassed + aggFailed + aggExtraPolicyOnly + aggErrored + aggSkipped
	// Match rate excludes errored tests — errors are infrastructure issues, not policy failures
	aggDecided := aggPassed + aggFailed + aggExtraPolicyOnly
	aggMatchRate := 0.0
	aggStrictMatchRate := 0.0
	if aggDecided > 0 {
		aggMatchRate = float64(aggPassed+aggExtraPolicyOnly) / float64(aggDecided)
		aggStrictMatchRate = float64(aggPassed) / float64(aggDecided)
	}

	output.OverallSummary = JSONOverallSummary{
		ModelsTested:         len(bucketOrder),
		TotalCases:           aggTotal,
		Passed:               aggPassed,
		Failed:               aggFailed,
		ExtraPolicyOnly:      aggExtraPolicyOnly,
		Errored:              aggErrored,
		Skipped:              aggSkipped,
		MatchRate:            aggMatchRate,
		StrictMatchRate:         aggStrictMatchRate,
		ThresholdsMet:        summary.ThresholdsMet,
		MinMatchRateRequired: minMatchRate,
		WorstMatchRate:       worstMatchRate,
	}

	// Add model comparison entries (includes historical cached models)
	for _, c := range comparison {
		output.ModelComparison = append(output.ModelComparison, JSONModelComparisonEntry{
			Model:           c.Model,
			Passed:          c.Passed,
			Failed:          c.Failed,
			ExtraPolicyOnly: c.ExtraPolicyOnly,
			Errored:         c.Errored,
			MatchRate:       c.MatchRate,
			StrictMatchRate:    c.StrictMatchRate,
			AvgMs:           c.AvgMs,
			TotalMs:         c.TotalMs,
			FromCache:       c.FromCache,
		})
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

	// Compute aggregate match rate for properties
	evaluated := summary.TotalCases - summary.Skipped
	matchRate := 0.0
	if evaluated > 0 {
		matchRate = float64(summary.Passed) / float64(evaluated)
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
			{Name: "match_rate", Value: fmt.Sprintf("%.4f", matchRate)},
		},
	}

	for _, r := range results {
		tc := JUnitTestCase{
			Name:      r.CaseID,
			ClassName: fmt.Sprintf("policies.%s", r.Expected.Decision),
			Time:      float64(r.ElapsedMs) / 1000.0,
		}

		// Use effective status so cached results are classified by their
		// original outcome (e.g., "cached failed" → failure element).
		isCached := r.Status == "skipped" && r.Error != nil && r.Error.Type == "cached"

		switch effectiveStatus(r) {
		case "failed":
			if isCached {
				tc.Failure = &JUnitFailure{
					Message: "Failed in previous run (cached result)",
					Type:    "CachedFailure",
				}
			} else {
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
			}

		case "errored":
			if isCached {
				tc.Error = &JUnitError{
					Message: "Errored in previous run (cached result)",
					Type:    "CachedError",
				}
			} else {
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

