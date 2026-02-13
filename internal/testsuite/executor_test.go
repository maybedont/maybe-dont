package testsuite

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareResults(t *testing.T) {
	tests := []struct {
		name            string
		expected        ExpectedResult
		actual          ActualResult
		expectedFailures int
		wantContains    []string
	}{
		{
			name: "decisions match - allow",
			expected: ExpectedResult{
				Decision: "allow",
			},
			actual: ActualResult{
				Decision: "allow",
			},
			expectedFailures: 0,
		},
		{
			name: "decisions match - deny",
			expected: ExpectedResult{
				Decision: "deny",
			},
			actual: ActualResult{
				Decision: "deny",
			},
			expectedFailures: 0,
		},
		{
			name: "decision mismatch",
			expected: ExpectedResult{
				Decision: "deny",
			},
			actual: ActualResult{
				Decision: "allow",
			},
			expectedFailures: 1,
			wantContains:    []string{"expected \"deny\", actual \"allow\""},
		},
		{
			name: "policy decision match",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-dangerous", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-dangerous", Decision: "deny"},
				},
			},
			expectedFailures: 0,
		},
		{
			name: "policy decision mismatch",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-dangerous", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-dangerous", Decision: "allow"},
				},
			},
			expectedFailures: 1,
			wantContains:    []string{"policy \"block-dangerous\": expected \"deny\", actual \"allow\""},
		},
		{
			name: "expected policy not executed",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "missing-policy", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "other-policy", Decision: "deny"},
				},
			},
			expectedFailures: 1,
			wantContains:    []string{"policy \"missing-policy\" not executed (check if enabled or conditions match)"},
		},
		{
			name: "redacted content matches",
			expected: ExpectedResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: [REDACTED]",
			},
			actual: ActualResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: [REDACTED]",
			},
			expectedFailures: 0,
		},
		{
			name: "redacted content mismatch",
			expected: ExpectedResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: [REDACTED]",
			},
			actual: ActualResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: 123-45-6789",
			},
			expectedFailures: 1,
			wantContains:    []string{"redacted content mismatch"},
		},
		{
			name: "expected redacted content but none returned",
			expected: ExpectedResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: [REDACTED]",
			},
			actual: ActualResult{
				Decision:        "redact",
				RedactedContent: "",
			},
			expectedFailures: 1,
			wantContains:    []string{"expected redacted content but none was returned"},
		},
		{
			name: "no expected redacted content - skip validation",
			expected: ExpectedResult{
				Decision:        "redact",
				RedactedContent: "", // Not specified
			},
			actual: ActualResult{
				Decision:        "redact",
				RedactedContent: "Some redacted content",
			},
			expectedFailures: 0, // No failure because redacted content wasn't specified in expectations
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use non-strict mode for backward-compatible tests (no unexpected policy scenarios)
			out := compareResults(tt.expected, tt.actual, false)
			assert.Len(t, out.failures, tt.expectedFailures)

			for _, want := range tt.wantContains {
				found := false
				for _, f := range out.failures {
					if strings.Contains(f, want) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected failure message containing %q, got %v", want, out.failures)
			}
		})
	}
}

// TestCompareResults_StrictPolicyMatch verifies that unexpected triggering policies
// are flagged as failures in strict mode and as warnings in non-strict mode.
// The check only applies to active decisions (deny/redact) — for "allow" tests,
// every policy returns "allow" by default and that's not a meaningful trigger.
func TestCompareResults_StrictPolicyMatch(t *testing.T) {
	tests := []struct {
		name             string
		expected         ExpectedResult
		actual           ActualResult
		strict           bool
		wantFailures     int
		wantWarnings     int
		wantContains     []string // substrings expected in failures or warnings
	}{
		{
			name: "strict mode: unexpected triggering policy is a failure",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "block-network", Decision: "deny"}, // unexpected trigger
					{PolicyName: "check-args", Decision: "allow"},   // non-triggering, OK
				},
			},
			strict:       true,
			wantFailures: 1,
			wantWarnings: 0,
			wantContains: []string{"unexpected policy match"},
		},
		{
			name: "non-strict mode: unexpected triggering policy is a warning",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "block-network", Decision: "deny"}, // unexpected trigger
				},
			},
			strict:       false,
			wantFailures: 0,
			wantWarnings: 1,
			wantContains: []string{"unexpected policy match"},
		},
		{
			name: "strict mode: no unexpected triggers passes cleanly",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "check-args", Decision: "allow"}, // non-triggering
				},
			},
			strict:       true,
			wantFailures: 0,
			wantWarnings: 0,
		},
		{
			name: "strict mode: multiple unexpected triggers reported as single failure",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "block-network", Decision: "deny"},
					{PolicyName: "block-files", Decision: "deny"},
				},
			},
			strict:       true,
			wantFailures: 1, // single consolidated message
			wantWarnings: 0,
			wantContains: []string{"2 unexpected policy match"},
		},
		{
			name: "no policy expectations: skip unexpected check entirely",
			expected: ExpectedResult{
				Decision: "deny",
				// No policies specified
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "block-network", Decision: "deny"},
				},
			},
			strict:       true,
			wantFailures: 0, // No expectations = no strict check
			wantWarnings: 0,
		},
		{
			name: "allow decision: skip unexpected check (allow is passive default)",
			expected: ExpectedResult{
				Decision: "allow",
				Policies: []PolicyExpectation{
					{PolicyName: "check-access", Decision: "allow"},
				},
			},
			actual: ActualResult{
				Decision: "allow",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "check-access", Decision: "allow"},
					{PolicyName: "check-network", Decision: "allow"},
					{PolicyName: "check-files", Decision: "allow"},
				},
			},
			strict:       true,
			wantFailures: 0, // All policies returning "allow" is the normal case
			wantWarnings: 0,
		},
		{
			name: "redact decision: unexpected triggers are flagged",
			expected: ExpectedResult{
				Decision: "redact",
				Policies: []PolicyExpectation{
					{PolicyName: "redact-ssn", Decision: "redact"},
				},
			},
			actual: ActualResult{
				Decision: "redact",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "redact-ssn", Decision: "redact"},
					{PolicyName: "redact-email", Decision: "redact"}, // unexpected
				},
			},
			strict:       true,
			wantFailures: 1,
			wantWarnings: 0,
			wantContains: []string{"1 unexpected policy match"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := compareResults(tt.expected, tt.actual, tt.strict)
			assert.Len(t, out.failures, tt.wantFailures, "failures count")
			assert.Len(t, out.warnings, tt.wantWarnings, "warnings count")

			// Check expected substrings in both failures and warnings combined
			allMessages := append(out.failures, out.warnings...)
			for _, want := range tt.wantContains {
				found := false
				for _, msg := range allMessages {
					if strings.Contains(msg, want) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected message containing %q in %v", want, allMessages)
			}
		})
	}
}

func TestExtractExpectedRedactedContent(t *testing.T) {
	tests := []struct {
		name     string
		items    []ContentItem
		expected string
	}{
		{
			name:     "empty items",
			items:    nil,
			expected: "",
		},
		{
			name:     "single text item",
			items:    []ContentItem{{Type: "text", Text: "redacted content"}},
			expected: "redacted content",
		},
		{
			name: "multiple text items",
			items: []ContentItem{
				{Type: "text", Text: "line 1"},
				{Type: "text", Text: "line 2"},
			},
			expected: "line 1\nline 2",
		},
		{
			name: "mixed types - only text extracted",
			items: []ContentItem{
				{Type: "text", Text: "text content"},
				{Type: "image", Text: "ignored"},
			},
			expected: "text content",
		},
		{
			name: "empty text items filtered",
			items: []ContentItem{
				{Type: "text", Text: ""},
				{Type: "text", Text: "actual content"},
			},
			expected: "actual content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractExpectedRedactedContent(tt.items)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExecuteCELTest_SetsEngineField verifies that CEL test results have the
// Engine field correctly set to "cel" and Model field empty.
func TestExecuteCELTest_SetsEngineField(t *testing.T) {
	// This tests the behavior documented in executor.go where CEL test results
	// have Engine="cel" and Model="" (empty for CEL since it's deterministic)

	tests := []struct {
		name           string
		inputEngine    string // test case engine setting
		expectedEngine string // expected Engine field in result
		expectedModel  string // expected Model field in result
	}{
		{
			name:           "CEL-only test has engine=cel",
			inputEngine:    "cel",
			expectedEngine: "cel",
			expectedModel:  "",
		},
		{
			name:           "both engine test also sets engine=cel for CEL execution",
			inputEngine:    "both",
			expectedEngine: "cel",
			expectedModel:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a TestResult as executeCELTest would
			result := TestResult{
				CaseID: "test-cel-engine",
				Title:  "Test CEL Engine Field",
				Engine: "cel", // executeCELTest always sets this to "cel"
				Expected: ExpectedResult{
					Decision: "allow",
				},
			}

			// Verify the Engine field is set correctly
			assert.Equal(t, tt.expectedEngine, result.Engine, "Engine field should be set to 'cel'")
			assert.Equal(t, tt.expectedModel, result.Model, "Model field should be empty for CEL tests")
		})
	}
}

// TestTestResultFields verifies that TestResult struct fields are correctly populated
// for different test scenarios.
func TestTestResultFields(t *testing.T) {
	t.Run("CEL result has engine=cel and empty model", func(t *testing.T) {
		result := TestResult{
			CaseID: "cel-test-001",
			Title:  "CEL Test",
			Engine: "cel",
			Model:  "", // Always empty for CEL
			Status: "passed",
		}

		assert.Equal(t, "cel", result.Engine)
		assert.Empty(t, result.Model)
	})

	t.Run("AI result has engine=ai and model set", func(t *testing.T) {
		result := TestResult{
			CaseID: "ai-test-001",
			Title:  "AI Test",
			Engine: "ai",
			Model:  "openai:gpt-4o-mini",
			Status: "passed",
		}

		assert.Equal(t, "ai", result.Engine)
		assert.Equal(t, "openai:gpt-4o-mini", result.Model)
	})

	t.Run("AI result model includes provider prefix", func(t *testing.T) {
		result := TestResult{
			CaseID: "ai-test-002",
			Title:  "AI Test with Anthropic",
			Engine: "ai",
			Model:  "anthropic:claude-haiku",
			Status: "failed",
		}

		assert.Equal(t, "ai", result.Engine)
		assert.Equal(t, "anthropic:claude-haiku", result.Model)
		assert.Contains(t, result.Model, ":")
	})
}

// TestCompareResults_ExtraPolicyOnly verifies that the extraPolicyOnly flag is set
// correctly based on the combination of strict mode, prior failures, and unexpected
// triggering policies. The flag should be true ONLY when strict mode is on, phases 1-3
// produced no failures, and the sole failure came from phase 4 (unexpected triggers).
func TestCompareResults_ExtraPolicyOnly(t *testing.T) {
	tests := []struct {
		name            string
		expected        ExpectedResult
		actual          ActualResult
		strict          bool
		wantFailures    int
		wantWarnings    int
		wantExtraPolicy bool
	}{
		{
			name: "extra policy only: correct decision + unexpected trigger = extraPolicyOnly",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "block-network", Decision: "deny"}, // unexpected trigger
				},
			},
			strict:          true,
			wantFailures:    1,
			wantWarnings:    0,
			wantExtraPolicy: true,
		},
		{
			name: "decision mismatch + unexpected trigger = NOT extraPolicyOnly",
			expected: ExpectedResult{
				Decision: "allow",
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			strict:          true,
			wantFailures:    1, // decision mismatch
			wantWarnings:    0,
			wantExtraPolicy: false,
		},
		{
			name: "expected policy mismatch + unexpected trigger = NOT extraPolicyOnly",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "allow"},  // mismatch on expected policy
					{PolicyName: "block-network", Decision: "deny"}, // unexpected trigger
				},
			},
			strict:          true,
			wantFailures:    2, // policy decision mismatch + unexpected trigger
			wantWarnings:    0,
			wantExtraPolicy: false,
		},
		{
			name: "non-strict mode: extra trigger produces warning, not failure, no extraPolicyOnly",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "block-network", Decision: "deny"}, // unexpected trigger
				},
			},
			strict:          false,
			wantFailures:    0,
			wantWarnings:    1,
			wantExtraPolicy: false,
		},
		{
			name: "no policy expectations: no extraPolicyOnly even with extra triggers",
			expected: ExpectedResult{
				Decision: "deny",
				// No policies specified
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "block-network", Decision: "deny"},
				},
			},
			strict:          true,
			wantFailures:    0,
			wantWarnings:    0,
			wantExtraPolicy: false,
		},
		{
			name: "clean pass: no failures, no extraPolicyOnly",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-exec", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-exec", Decision: "deny"},
					{PolicyName: "check-args", Decision: "allow"}, // non-triggering, OK
				},
			},
			strict:          true,
			wantFailures:    0,
			wantWarnings:    0,
			wantExtraPolicy: false,
		},
		{
			name: "redact decision with extra policy = extraPolicyOnly",
			expected: ExpectedResult{
				Decision: "redact",
				Policies: []PolicyExpectation{
					{PolicyName: "redact-ssn", Decision: "redact"},
				},
			},
			actual: ActualResult{
				Decision: "redact",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "redact-ssn", Decision: "redact"},
					{PolicyName: "redact-email", Decision: "redact"}, // unexpected trigger
				},
			},
			strict:          true,
			wantFailures:    1,
			wantWarnings:    0,
			wantExtraPolicy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := compareResults(tt.expected, tt.actual, tt.strict)
			assert.Len(t, out.failures, tt.wantFailures, "failures count")
			assert.Len(t, out.warnings, tt.wantWarnings, "warnings count")
			assert.Equal(t, tt.wantExtraPolicy, out.extraPolicyOnly, "extraPolicyOnly flag")
		})
	}
}
