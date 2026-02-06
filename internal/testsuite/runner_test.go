package testsuite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single tag",
			input:    "security",
			expected: []string{"security"},
		},
		{
			name:     "multiple tags",
			input:    "security,performance,api",
			expected: []string{"security", "performance", "api"},
		},
		{
			name:     "tags with whitespace",
			input:    " security , performance , api ",
			expected: []string{"security", "performance", "api"},
		},
		{
			name:     "empty tags filtered",
			input:    "security,,performance",
			expected: []string{"security", "performance"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitTags(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasAllTags(t *testing.T) {
	tests := []struct {
		name     string
		tcTags   []string
		required []string
		expected bool
	}{
		{
			name:     "empty required tags",
			tcTags:   []string{"a", "b"},
			required: []string{},
			expected: true,
		},
		{
			name:     "nil required tags",
			tcTags:   []string{"a", "b"},
			required: nil,
			expected: true,
		},
		{
			name:     "all tags present",
			tcTags:   []string{"a", "b", "c"},
			required: []string{"a", "b"},
			expected: true,
		},
		{
			name:     "missing one tag",
			tcTags:   []string{"a", "c"},
			required: []string{"a", "b"},
			expected: false,
		},
		{
			name:     "empty test case tags",
			tcTags:   []string{},
			required: []string{"a"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAllTags(tt.tcTags, tt.required)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasAnyTag(t *testing.T) {
	tests := []struct {
		name     string
		tcTags   []string
		exclude  []string
		expected bool
	}{
		{
			name:     "empty exclude tags",
			tcTags:   []string{"a", "b"},
			exclude:  []string{},
			expected: false,
		},
		{
			name:     "nil exclude tags",
			tcTags:   []string{"a", "b"},
			exclude:  nil,
			expected: false,
		},
		{
			name:     "has excluded tag",
			tcTags:   []string{"a", "b", "c"},
			exclude:  []string{"b"},
			expected: true,
		},
		{
			name:     "no excluded tags present",
			tcTags:   []string{"a", "c"},
			exclude:  []string{"b", "d"},
			expected: false,
		},
		{
			name:     "empty test case tags",
			tcTags:   []string{},
			exclude:  []string{"a"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAnyTag(tt.tcTags, tt.exclude)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCasePatternMatching(t *testing.T) {
	// Tests for filepath.Match patterns used in filterTestCases
	tests := []struct {
		name     string
		caseID   string
		pattern  string
		expected bool
	}{
		{
			name:     "star pattern matches all",
			caseID:   "test-001",
			pattern:  "*",
			expected: true,
		},
		{
			name:     "prefix pattern",
			caseID:   "cel-req-001",
			pattern:  "cel-*",
			expected: true,
		},
		{
			name:     "prefix pattern no match",
			caseID:   "ai-req-001",
			pattern:  "cel-*",
			expected: false,
		},
		{
			name:     "exact match",
			caseID:   "test-001",
			pattern:  "test-001",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := filepath.Match(tt.pattern, tt.caseID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolvePath(t *testing.T) {
	// Create a temp directory structure for testing
	tempDir := t.TempDir()
	suiteDir := filepath.Join(tempDir, "suite")
	require.NoError(t, os.MkdirAll(suiteDir, 0755))

	// Create a file in suite dir for relative path test
	suiteFile := filepath.Join(suiteDir, "rules.yaml")
	require.NoError(t, os.WriteFile(suiteFile, []byte("test"), 0644))

	tests := []struct {
		name     string
		path     string
		suiteDir string
		expected string
	}{
		{
			name:     "absolute path unchanged",
			path:     "/etc/config.yaml",
			suiteDir: suiteDir,
			expected: "/etc/config.yaml",
		},
		{
			name:     "relative with dot prefix",
			path:     "./rules.yaml",
			suiteDir: suiteDir,
			expected: filepath.Join(suiteDir, "rules.yaml"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolvePath(tt.path, tt.suiteDir)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseModelFlag(t *testing.T) {
	// Create a model matrix with API keys for lookup
	modelMatrix := []ModelConfig{
		{Provider: "openai", Model: "gpt-4o-mini", APIKey: "${OPENAI_API_KEY}"},
		{Provider: "anthropic", Model: "claude-haiku-4-5-20251001", APIKey: "${ANTHROPIC_API_KEY}"},
	}

	tests := []struct {
		name           string
		modelFlag      string
		expectNil      bool
		expectProvider string
		expectModel    string
	}{
		{
			name:           "valid openai model",
			modelFlag:      "openai:gpt-4o-mini",
			expectNil:      false,
			expectProvider: "openai",
			expectModel:    "gpt-4o-mini",
		},
		{
			name:           "valid anthropic model",
			modelFlag:      "anthropic:claude-haiku-4-5-20251001",
			expectNil:      false,
			expectProvider: "anthropic",
			expectModel:    "claude-haiku-4-5-20251001",
		},
		{
			name:      "missing colon returns nil",
			modelFlag: "openai-gpt-4o-mini",
			expectNil: true,
		},
		{
			name:      "empty string returns nil",
			modelFlag: "",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseModelFlag(tt.modelFlag, modelMatrix)
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expectProvider, result.Provider)
				assert.Equal(t, tt.expectModel, result.Model)
			}
		})
	}
}

func TestResolveEnvVar(t *testing.T) {
	// Set up test environment variable
	t.Setenv("TEST_API_KEY", "sk-test-123")

	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{
			name:     "no env var",
			value:    "plain-value",
			expected: "plain-value",
		},
		{
			name:     "env var present",
			value:    "${TEST_API_KEY}",
			expected: "sk-test-123",
		},
		{
			name:     "env var not set",
			value:    "${NONEXISTENT_VAR}",
			expected: "",
		},
		{
			name:     "partial env var syntax",
			value:    "$TEST_API_KEY",
			expected: "$TEST_API_KEY", // Not substituted - requires ${...} syntax
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveEnvVar(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFormatEngineInfo verifies the engine/model suffix formatting for test output.
// This suffix appears after the test case ID in streaming output.
// TestModelConfig_IsEnabled verifies that the enabled field defaults correctly
// and can be explicitly set to true or false.
func TestModelConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  *bool
		expected bool
	}{
		{
			name:     "nil enabled defaults to true",
			enabled:  nil,
			expected: true,
		},
		{
			name:     "explicit true is enabled",
			enabled:  boolPtr(true),
			expected: true,
		},
		{
			name:     "explicit false is disabled",
			enabled:  boolPtr(false),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := ModelConfig{
				Provider: "openai",
				Model:    "gpt-4o-mini",
				Enabled:  tt.enabled,
			}
			assert.Equal(t, tt.expected, model.IsEnabled())
		})
	}
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool {
	return &b
}

func TestFormatEngineInfo(t *testing.T) {
	tests := []struct {
		name     string
		engine   string
		model    string
		expected string
	}{
		{
			name:     "empty engine returns empty string",
			engine:   "",
			model:    "",
			expected: "",
		},
		{
			name:     "engine only returns bracketed engine",
			engine:   "cel",
			model:    "",
			expected: " [cel]",
		},
		{
			name:     "engine with model returns combined format",
			engine:   "ai",
			model:    "openai:gpt-4",
			expected: " [ai:openai:gpt-4]",
		},
		{
			name:     "engine with anthropic model",
			engine:   "ai",
			model:    "anthropic:claude-haiku",
			expected: " [ai:anthropic:claude-haiku]",
		},
		{
			name:     "model without engine is ignored",
			engine:   "",
			model:    "openai:gpt-4",
			expected: "", // Engine empty means no suffix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatEngineInfo(tt.engine, tt.model)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFormatSingleTestResult_EngineInfo verifies that test result output includes
// the engine/model metadata suffix, title, confidence suppression for CEL, and
// multi-line policy formatting.
func TestFormatSingleTestResult_EngineInfo(t *testing.T) {
	tests := []struct {
		name            string
		result          TestResult
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "CEL test shows [cel] suffix and suppresses confidence",
			result: TestResult{
				CaseID: "test-1",
				Title:  "Allow safe read operation",
				Engine: "cel",
				Status: "passed",
				Expected: ExpectedResult{
					Decision: "allow",
				},
				Actual: ActualResult{
					Decision:   "allow",
					Confidence: 1.0,
				},
			},
			wantContains:    []string{"test-1", "[cel]", "Allow safe read operation", "decision: expected: allow, actual: allow\n"},
			wantNotContains: []string{"confidence"},
		},
		{
			name: "AI test shows [ai:model] suffix and includes confidence",
			result: TestResult{
				CaseID: "test-2",
				Title:  "Deny command execution",
				Engine: "ai",
				Model:  "anthropic:claude-haiku",
				Status: "passed",
				Expected: ExpectedResult{
					Decision: "deny",
				},
				Actual: ActualResult{
					Decision:   "deny",
					Confidence: 0.9,
				},
			},
			wantContains: []string{"test-2", "[ai:anthropic:claude-haiku]", "Deny command execution", "confidence: 0.9"},
		},
		{
			name: "failed test with engine info and reasoning",
			result: TestResult{
				CaseID: "test-3",
				Engine: "ai",
				Model:  "openai:gpt-4o-mini",
				Status: "failed",
				Expected: ExpectedResult{
					Decision: "deny",
				},
				Actual: ActualResult{
					Decision:   "allow",
					Confidence: 0.8,
					Reasoning:  "AI determined request is safe",
				},
				Failures: []string{"expected deny, got allow"},
			},
			// Reasoning should be plain text, not %q quoted
			wantContains:    []string{"test-3", "[ai:openai:gpt-4o-mini]", "✗", "reasoning: AI determined request is safe"},
			wantNotContains: []string{`reasoning: "AI`},
		},
		{
			name: "skipped test with engine info",
			result: TestResult{
				CaseID: "test-4",
				Engine: "ai",
				Model:  "openai:gpt-4",
				Status: "skipped",
				Error: &TestError{
					Type:    "cached",
					Message: "cached passed",
				},
			},
			wantContains: []string{"test-4", "[ai:openai:gpt-4]", "○", "skipped", "cached passed"},
		},
		{
			name: "errored test with engine info and title",
			result: TestResult{
				CaseID: "test-5",
				Title:  "Test timeout scenario",
				Engine: "ai",
				Model:  "anthropic:claude",
				Status: "errored",
				Error: &TestError{
					Type:    "timeout",
					Message: "Test case timed out",
				},
			},
			wantContains: []string{"test-5", "[ai:anthropic:claude]", "⚠", "timeout", "Test timeout scenario"},
		},
		{
			name: "no engine shows no suffix",
			result: TestResult{
				CaseID: "test-6",
				Engine: "",
				Status: "passed",
				Expected: ExpectedResult{
					Decision: "allow",
				},
				Actual: ActualResult{
					Decision:   "allow",
					Confidence: 1.0,
				},
			},
			wantContains: []string{"test-6", "✓"},
		},
		{
			name: "empty title is not shown",
			result: TestResult{
				CaseID: "test-7",
				Title:  "",
				Engine: "cel",
				Status: "passed",
				Expected: ExpectedResult{
					Decision: "allow",
				},
				Actual: ActualResult{
					Decision:   "allow",
					Confidence: 1.0,
				},
			},
			// The line after header should be "decision:", not an empty title line
			wantContains: []string{"✓ test-7", "decision: expected: allow"},
		},
		{
			name: "multi-line policies with triggering marker",
			result: TestResult{
				CaseID: "test-8",
				Engine: "cel",
				Status: "passed",
				Expected: ExpectedResult{
					Decision: "deny",
				},
				Actual: ActualResult{
					Decision:   "deny",
					Confidence: 1.0,
					PoliciesExecuted: []PolicyResult{
						{PolicyName: "Check file access", Decision: "allow", ElapsedMs: 100},
						{PolicyName: "Check command exec", Decision: "deny", ElapsedMs: 200},
						{PolicyName: "Check network", Decision: "allow", ElapsedMs: 150},
					},
				},
			},
			wantContains: []string{
				"policies:\n",
				"► Check command exec",
				"  Check file access",
				"  Check network",
			},
			// Old single-line format should not appear
			wantNotContains: []string{"policies: Check"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := formatSingleTestResult(tt.result)
			for _, want := range tt.wantContains {
				assert.Contains(t, output, want, "output should contain %q", want)
			}
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, output, notWant, "output should NOT contain %q", notWant)
			}
		})
	}
}

// TestFormatSectionHeader verifies the visual separator generation.
func TestFormatSectionHeader(t *testing.T) {
	tests := []struct {
		name     string
		label    string
		contains string
	}{
		{
			name:     "CEL header",
			label:    "cel",
			contains: "── cel ",
		},
		{
			name:     "model header with colon",
			label:    "anthropic:claude-opus-4-6",
			contains: "── anthropic:claude-opus-4-6 ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSectionHeader(tt.label)
			assert.Contains(t, result, tt.contains)
			// Should end with dashes and newline
			assert.True(t, strings.HasSuffix(result, "─\n"), "should end with dashes and newline")
			// Total rune width should be sectionHeaderWidth + newline
			runeCount := utf8.RuneCountInString(result)
			assert.Equal(t, sectionHeaderWidth+1, runeCount, "header should be exactly %d runes + newline", sectionHeaderWidth)
		})
	}
}

// TestFormatPolicies verifies multi-line policy formatting with triggering sort,
// column alignment, and colored markers for expected/unexpected policies.
func TestFormatPolicies(t *testing.T) {
	tests := []struct {
		name             string
		policies         []PolicyResult
		actualDecision   string
		expectedPolicies map[string]bool
		wantContains     []string
		wantNotContains  []string
		wantEmpty        bool
	}{
		{
			name:      "empty policies returns empty string",
			policies:  nil,
			wantEmpty: true,
		},
		{
			name: "triggering policy sorted first with marker",
			policies: []PolicyResult{
				{PolicyName: "Beta policy", Decision: "allow", ElapsedMs: 100},
				{PolicyName: "Alpha policy", Decision: "deny", ElapsedMs: 200},
			},
			actualDecision: "deny",
			wantContains: []string{
				"policies:\n",
				"► Alpha policy",
				"  Beta policy",
			},
		},
		{
			name: "multiple triggering sorted alphabetically",
			policies: []PolicyResult{
				{PolicyName: "Zebra", Decision: "deny", ElapsedMs: 300},
				{PolicyName: "Alpha", Decision: "deny", ElapsedMs: 100},
				{PolicyName: "Middle", Decision: "allow", ElapsedMs: 200},
			},
			actualDecision: "deny",
			wantContains: []string{
				"► Alpha",
				"► Zebra",
				"  Middle",
			},
		},
		{
			name: "columns include decision and elapsed",
			policies: []PolicyResult{
				{PolicyName: "Check exec", Decision: "deny", ElapsedMs: 1872},
			},
			actualDecision: "deny",
			wantContains: []string{
				"deny",
				"1872ms",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatPolicies(tt.policies, tt.actualDecision, tt.expectedPolicies)
			if tt.wantEmpty {
				assert.Empty(t, result)
				return
			}
			for _, want := range tt.wantContains {
				assert.Contains(t, result, want, "output should contain %q", want)
			}
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, result, notWant, "output should NOT contain %q", notWant)
			}
		})
	}
}

// TestFormatReasoning verifies plain-text reasoning formatting with truncation.
func TestFormatReasoning(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty reasoning returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "single line reasoning",
			input:    "AI determined request is safe",
			expected: "    reasoning: AI determined request is safe\n",
		},
		{
			name:     "multi-line truncated at first newline",
			input:    "First line\nSecond line\nThird line",
			expected: "    reasoning: First line...\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatReasoning(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFormatRedacted verifies inline and multi-line redacted content formatting.
func TestFormatRedacted(t *testing.T) {
	tests := []struct {
		name     string
		label    string
		content  string
		contains []string
	}{
		{
			name:     "empty content shows empty inline",
			label:    "redacted expected",
			content:  "",
			contains: []string{"redacted expected: \n"},
		},
		{
			name:     "single line content stays inline",
			label:    "redacted expected",
			content:  "sanitized output",
			contains: []string{"redacted expected: sanitized output\n"},
		},
		{
			name:    "multi-line content is indented",
			label:   "redacted expected",
			content: "line1\nline2\nline3",
			contains: []string{
				"redacted expected:\n",
				"      line1\n",
				"      line2\n",
				"      line3\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatRedacted(tt.label, tt.content)
			for _, want := range tt.contains {
				assert.Contains(t, result, want, "output should contain %q", want)
			}
		})
	}
}

// TestAcceptanceConfig_IsStrictPolicyMatch verifies strict policy match default and override.
func TestAcceptanceConfig_IsStrictPolicyMatch(t *testing.T) {
	tests := []struct {
		name     string
		config   AcceptanceConfig
		expected bool
	}{
		{
			name:     "nil defaults to true",
			config:   AcceptanceConfig{},
			expected: true,
		},
		{
			name:     "explicit true",
			config:   AcceptanceConfig{StrictPolicyMatch: boolPtr(true)},
			expected: true,
		},
		{
			name:     "explicit false",
			config:   AcceptanceConfig{StrictPolicyMatch: boolPtr(false)},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.config.IsStrictPolicyMatch())
		})
	}
}

// TestFormatSingleTestResult_Warnings verifies that warnings are displayed in test output.
func TestFormatSingleTestResult_Warnings(t *testing.T) {
	t.Run("passed with warnings shows WARNING lines", func(t *testing.T) {
		result := TestResult{
			CaseID: "test-warn-1",
			Engine: "ai",
			Model:  "openai:gpt-4o-mini",
			Status: "passed",
			Expected: ExpectedResult{
				Decision: "deny",
			},
			Actual: ActualResult{
				Decision:   "deny",
				Confidence: 1.0,
			},
			Warnings: []string{
				`unexpected policy "block-network" triggered with "deny"`,
			},
		}

		output := formatSingleTestResult(result)
		assert.Contains(t, output, "WARNING:")
		assert.Contains(t, output, `unexpected policy "block-network"`)
	})

	t.Run("passed without warnings has no WARNING lines", func(t *testing.T) {
		result := TestResult{
			CaseID: "test-clean",
			Engine: "cel",
			Status: "passed",
			Expected: ExpectedResult{
				Decision: "allow",
			},
			Actual: ActualResult{
				Decision:   "allow",
				Confidence: 1.0,
			},
		}

		output := formatSingleTestResult(result)
		assert.NotContains(t, output, "WARNING:")
	})
}

// TestFormatSingleTestResult_PhaseDisplay verifies that the phase line appears
// when set and is omitted when empty.
func TestFormatSingleTestResult_PhaseDisplay(t *testing.T) {
	t.Run("phase shown on its own line when set", func(t *testing.T) {
		result := TestResult{
			CaseID: "test-phase-1",
			Engine: "ai",
			Model:  "openai:gpt-4o-mini",
			Status: "passed",
			Phase:  "request",
			Expected: ExpectedResult{
				Decision: "deny",
			},
			Actual: ActualResult{
				Decision:   "deny",
				Confidence: 1.0,
			},
		}

		output := formatSingleTestResult(result)
		assert.Contains(t, output, "    phase: request\n")
		assert.Contains(t, output, "    decision: expected: deny, actual: deny")
	})

	t.Run("phase omitted when empty", func(t *testing.T) {
		result := TestResult{
			CaseID: "test-phase-2",
			Engine: "cel",
			Status: "passed",
			Expected: ExpectedResult{
				Decision: "allow",
			},
			Actual: ActualResult{
				Decision: "allow",
			},
		}

		output := formatSingleTestResult(result)
		assert.NotContains(t, output, "phase:")
	})

	t.Run("decision line uses decision label consistently", func(t *testing.T) {
		result := TestResult{
			CaseID: "test-decision-label",
			Engine: "ai",
			Model:  "openai:gpt-4o-mini",
			Status: "failed",
			Phase:  "response",
			Expected: ExpectedResult{
				Decision: "deny",
			},
			Actual: ActualResult{
				Decision:   "allow",
				Confidence: 0.8,
			},
			Failures: []string{"expected deny, got allow"},
		}

		output := formatSingleTestResult(result)
		assert.Contains(t, output, "    phase: response\n")
		assert.Contains(t, output, "    decision: expected: deny, actual: allow")
		assert.Contains(t, output, "confidence: 0.8")
	})
}

// TestFormatTextSummary verifies the summary section formatting including
// retry hints and cached test status breakdown.
func TestFormatTextSummary(t *testing.T) {
	suite := &Suite{
		BundleID: "test-suite",
		Acceptance: AcceptanceConfig{
			MinMatchRate: 0.8,
		},
	}

	t.Run("retry hint shown when there are failures", func(t *testing.T) {
		summary := &RunResult{
			TotalCases:    10,
			Passed:        7,
			Failed:        2,
			Errored:       1,
			MatchRate:     0.7,
			ThresholdsMet: false,
		}

		output := formatTextSummary(suite, summary, nil, nil, nil)
		assert.Contains(t, output, "--retry-failed")
		assert.Contains(t, output, "7 passed, 2 failed, 1 errored")
	})

	t.Run("no retry hint when all pass", func(t *testing.T) {
		summary := &RunResult{
			TotalCases:    5,
			Passed:        5,
			MatchRate:     1.0,
			ThresholdsMet: true,
		}

		output := formatTextSummary(suite, summary, nil, nil, nil)
		assert.NotContains(t, output, "--retry-failed")
	})

	t.Run("cached breakdown shown when cached tests exist", func(t *testing.T) {
		// Build results with 8 cached-passed and 2 cached-failed
		var results []TestResult
		for i := 0; i < 8; i++ {
			results = append(results, TestResult{
				Status: "skipped",
				Error:  &TestError{Type: "cached", Message: "cached passed"},
			})
		}
		for i := 0; i < 2; i++ {
			results = append(results, TestResult{
				Status: "skipped",
				Error:  &TestError{Type: "cached", Message: "cached failed"},
			})
		}

		summary := &RunResult{
			TotalCases:    15,
			Passed:        3,
			Failed:        1,
			Skipped:       10,
			SkippedCached: 10,
			MatchRate:     0.75,
			ThresholdsMet: false,
		}

		output := formatTextSummary(suite, summary, results, nil, nil)
		assert.Contains(t, output, "Cached:  10 skipped (8 passed, 2 failed in last run)")
		assert.Contains(t, output, "--retry-failed")
	})

	t.Run("retry hint for cached failures only", func(t *testing.T) {
		// Build results with 5 cached-passed and 2 cached-failed
		var results []TestResult
		for i := 0; i < 5; i++ {
			results = append(results, TestResult{
				Status: "skipped",
				Error:  &TestError{Type: "cached", Message: "cached passed"},
			})
		}
		for i := 0; i < 2; i++ {
			results = append(results, TestResult{
				Status: "skipped",
				Error:  &TestError{Type: "cached", Message: "cached failed"},
			})
		}

		summary := &RunResult{
			TotalCases:    10,
			Passed:        3,
			Skipped:       7,
			SkippedCached: 7,
			MatchRate:     1.0,
			ThresholdsMet: true,
		}

		output := formatTextSummary(suite, summary, results, nil, nil)
		assert.Contains(t, output, "Cached:  7 skipped (5 passed, 2 failed in last run)")
		assert.Contains(t, output, "retry previously failed tests: --retry-failed")
	})
}

// TestFormatModelComparison verifies the cross-model comparison table rendering.
func TestFormatModelComparison(t *testing.T) {
	// Disable color for deterministic test output
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	t.Run("returns empty for nil entries", func(t *testing.T) {
		assert.Empty(t, formatModelComparison(nil))
	})

	t.Run("returns empty for empty entries", func(t *testing.T) {
		assert.Empty(t, formatModelComparison([]ModelComparisonEntry{}))
	})

	t.Run("renders table with multiple models", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "cel", Passed: 10, Failed: 0, Errored: 0, MatchRate: 1.0, AvgMs: 2, TotalMs: 20},
			{Model: "openai:gpt-5", Passed: 8, Failed: 2, Errored: 0, MatchRate: 0.8, AvgMs: 1200, TotalMs: 12000},
			{Model: "anthropic:claude", Passed: 7, Failed: 2, Errored: 1, MatchRate: 0.7, AvgMs: 900, TotalMs: 9000, FromCache: true},
		}

		output := formatModelComparison(entries)

		// Verify structure
		assert.Contains(t, output, "Model Comparison")
		assert.Contains(t, output, "Model")
		assert.Contains(t, output, "Pass")
		assert.Contains(t, output, "Fail")
		assert.Contains(t, output, "Match%")
		assert.Contains(t, output, "Total")

		// Verify data rows
		assert.Contains(t, output, "cel")
		assert.Contains(t, output, "openai:gpt-5")
		assert.Contains(t, output, "anthropic:claude")
		assert.Contains(t, output, "100.0%")
		assert.Contains(t, output, "80.0%")
		assert.Contains(t, output, "70.0%")

		// Verify cached footnote
		assert.Contains(t, output, "previous run")
	})

	t.Run("no cached footnote when all tested in current run", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "cel", Passed: 5, MatchRate: 1.0},
			{Model: "openai:gpt-5", Passed: 4, Failed: 1, MatchRate: 0.8},
		}

		output := formatModelComparison(entries)
		assert.NotContains(t, output, "previous run")
	})
}

// TestSuite_ResolveAPIKey verifies the deterministic API key lookup:
// 1. Per-model api_key takes precedence
// 2. Falls back to provider-level api_key
// 3. Returns empty if neither configured
func TestSuite_ResolveAPIKey(t *testing.T) {
	tests := []struct {
		name      string
		suite     Suite
		model     ModelConfig
		wantKey   string
	}{
		{
			name: "model-level api_key takes precedence",
			suite: Suite{
				Providers: map[string]ProviderConfig{
					"openai": {APIKey: "provider-key"},
				},
			},
			model:   ModelConfig{Provider: "openai", Model: "gpt-4", APIKey: "model-key"},
			wantKey: "model-key",
		},
		{
			name: "falls back to provider-level api_key",
			suite: Suite{
				Providers: map[string]ProviderConfig{
					"openai": {APIKey: "provider-key"},
				},
			},
			model:   ModelConfig{Provider: "openai", Model: "gpt-4"},
			wantKey: "provider-key",
		},
		{
			name: "returns empty when provider not configured",
			suite: Suite{
				Providers: map[string]ProviderConfig{
					"anthropic": {APIKey: "anthropic-key"},
				},
			},
			model:   ModelConfig{Provider: "openai", Model: "gpt-4"},
			wantKey: "",
		},
		{
			name:    "returns empty when no providers section",
			suite:   Suite{},
			model:   ModelConfig{Provider: "openai", Model: "gpt-4"},
			wantKey: "",
		},
		{
			name: "model-level empty string does not override provider",
			suite: Suite{
				Providers: map[string]ProviderConfig{
					"openai": {APIKey: "provider-key"},
				},
			},
			model:   ModelConfig{Provider: "openai", Model: "gpt-4", APIKey: ""},
			wantKey: "provider-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.suite.ResolveAPIKey(tt.model)
			assert.Equal(t, tt.wantKey, got)
		})
	}
}

// TestSuite_ResolveEndpoint verifies the deterministic endpoint lookup:
// 1. Per-model endpoint takes precedence
// 2. Falls back to provider-level endpoint
// 3. Returns empty if neither configured (caller uses provider defaults)
func TestSuite_ResolveEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		suite        Suite
		model        ModelConfig
		wantEndpoint string
	}{
		{
			name: "model-level endpoint takes precedence",
			suite: Suite{
				Providers: map[string]ProviderConfig{
					"openai": {Endpoint: "https://provider.example.com"},
				},
			},
			model:        ModelConfig{Provider: "openai", Model: "gpt-4", Endpoint: "https://model.example.com"},
			wantEndpoint: "https://model.example.com",
		},
		{
			name: "falls back to provider-level endpoint",
			suite: Suite{
				Providers: map[string]ProviderConfig{
					"openai": {Endpoint: "https://provider.example.com"},
				},
			},
			model:        ModelConfig{Provider: "openai", Model: "gpt-4"},
			wantEndpoint: "https://provider.example.com",
		},
		{
			name: "returns empty when no endpoint configured",
			suite: Suite{
				Providers: map[string]ProviderConfig{
					"openai": {APIKey: "key-only"},
				},
			},
			model:        ModelConfig{Provider: "openai", Model: "gpt-4"},
			wantEndpoint: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.suite.ResolveEndpoint(tt.model)
			assert.Equal(t, tt.wantEndpoint, got)
		})
	}
}
