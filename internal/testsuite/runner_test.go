package testsuite

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEffectiveStatus verifies that effectiveStatus correctly resolves cached
// results to their original status while leaving non-cached results unchanged.
func TestEffectiveStatus(t *testing.T) {
	tests := []struct {
		name   string
		result TestResult
		want   string
	}{
		{
			name:   "fresh passed",
			result: TestResult{Status: "passed"},
			want:   "passed",
		},
		{
			name:   "fresh failed",
			result: TestResult{Status: "failed"},
			want:   "failed",
		},
		{
			name:   "fresh errored",
			result: TestResult{Status: "errored"},
			want:   "errored",
		},
		{
			name:   "fresh skipped (no error)",
			result: TestResult{Status: "skipped"},
			want:   "skipped",
		},
		{
			name:   "rate-limited skipped stays skipped",
			result: TestResult{Status: "skipped", Error: &TestError{Type: "rate_limited", Message: "rate limited"}},
			want:   "skipped",
		},
		{
			name:   "cached passed resolves to passed",
			result: TestResult{Status: "skipped", Error: &TestError{Type: "cached", Message: "cached passed"}},
			want:   "passed",
		},
		{
			name:   "cached failed resolves to failed",
			result: TestResult{Status: "skipped", Error: &TestError{Type: "cached", Message: "cached failed"}},
			want:   "failed",
		},
		{
			name:   "cached errored resolves to errored",
			result: TestResult{Status: "skipped", Error: &TestError{Type: "cached", Message: "cached errored"}},
			want:   "errored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, effectiveStatus(tt.result))
		})
	}
}

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
			name: "allow with expected policies highlights and sorts to top",
			policies: []PolicyResult{
				{PolicyName: "Check command execution", Decision: "allow", ElapsedMs: 100},
				{PolicyName: "Check credential access", Decision: "allow", ElapsedMs: 200},
				{PolicyName: "Check executable creation", Decision: "allow", ElapsedMs: 150},
			},
			actualDecision:   "allow",
			expectedPolicies: map[string]bool{"Check executable creation": true},
			wantContains: []string{
				"► Check executable creation",
				"  Check command execution",
				"  Check credential access",
			},
		},
		{
			name: "allow without expected policies has no markers",
			policies: []PolicyResult{
				{PolicyName: "Beta policy", Decision: "allow", ElapsedMs: 100},
				{PolicyName: "Alpha policy", Decision: "allow", ElapsedMs: 200},
			},
			actualDecision: "allow",
			wantContains: []string{
				"  Alpha policy",
				"  Beta policy",
			},
			wantNotContains: []string{"►"},
		},
		{
			name: "columns include decision and elapsed",
			policies: []PolicyResult{
				{PolicyName: "Check exec", Decision: "deny", ElapsedMs: 1872},
			},
			actualDecision: "deny",
			wantContains: []string{
				"deny",
				"1872 ms (1.9s)",
			},
		},
		{
			name: "unexpected triggering policy shows reasoning",
			policies: []PolicyResult{
				{PolicyName: "Check command execution", Decision: "deny", ElapsedMs: 986, Reasoning: "Dangerous command tool blocked"},
				{PolicyName: "Check system access", Decision: "deny", ElapsedMs: 1454, Reasoning: "The sudo command requests elevated privileges"},
				{PolicyName: "Check credential access", Decision: "allow", ElapsedMs: 800},
			},
			actualDecision:   "deny",
			expectedPolicies: map[string]bool{"Check system access": true},
			wantContains: []string{
				"► Check command execution",
				"Dangerous command tool blocked",
				"► Check system access",
			},
			// Expected policy reasoning should NOT be shown, only unexpected
			wantNotContains: []string{"sudo command requests elevated"},
		},
		{
			name: "expected triggering policy does not show reasoning",
			policies: []PolicyResult{
				{PolicyName: "Check system access", Decision: "deny", ElapsedMs: 1454, Reasoning: "Should not appear in output"},
			},
			actualDecision:   "deny",
			expectedPolicies: map[string]bool{"Check system access": true},
			wantContains: []string{
				"► Check system access",
			},
			wantNotContains: []string{"Should not appear in output"},
		},
		{
			name: "unexpected policy with empty reasoning shows no extra lines",
			policies: []PolicyResult{
				{PolicyName: "Check exec", Decision: "deny", ElapsedMs: 100, Reasoning: ""},
			},
			actualDecision:   "deny",
			expectedPolicies: map[string]bool{"Other policy": true},
			wantContains: []string{
				"► Check exec",
				"deny",
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

// TestWrapReasoning verifies word-wrapping of per-policy reasoning text.
func TestWrapReasoning(t *testing.T) {
	tests := []struct {
		name      string
		reasoning string
		indent    string
		maxWidth  int
		expected  string
	}{
		{
			name:     "empty reasoning returns empty",
			indent:   "    ",
			maxWidth: 40,
			expected: "",
		},
		{
			name:      "whitespace-only reasoning returns empty",
			reasoning: "   \n  \t  ",
			indent:    "    ",
			maxWidth:  40,
			expected:  "",
		},
		{
			name:      "short reasoning fits on one line",
			reasoning: "Tool is dangerous",
			indent:    "          ",
			maxWidth:  80,
			expected:  "          Tool is dangerous\n",
		},
		{
			name:      "long reasoning wraps at word boundary",
			reasoning: "Dangerous command tool blocked because shell__run_command contains shell and run_command which are both on the dangerous tools list",
			indent:    "          ",
			maxWidth:  60,
			// Content width = 60 - 10 = 50 chars
			expected: "          Dangerous command tool blocked because\n" +
				"          shell__run_command contains shell and run_command\n" +
				"          which are both on the dangerous tools list\n",
		},
		{
			name:      "very long single word exceeds width",
			reasoning: "superlongwordthatcannotbewrapped short",
			indent:    "          ",
			maxWidth:  40,
			// Content width = 30, but the word is longer — still placed on one line
			expected: "          superlongwordthatcannotbewrapped\n" +
				"          short\n",
		},
		{
			name:      "multiline reasoning collapses whitespace",
			reasoning: "First line\nSecond line\n  Third line",
			indent:    "    ",
			maxWidth:  50,
			expected:  "    First line Second line Third line\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapReasoning(tt.reasoning, tt.indent, tt.maxWidth)
			assert.Equal(t, tt.expected, result)
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

// TestFormatSingleTestResult_UnexpectedPolicyReasoning verifies that per-policy reasoning
// replaces the top-level reasoning line when the failure is from unexpected policy matches
// (decision matches but extra policies triggered).
func TestFormatSingleTestResult_UnexpectedPolicyReasoning(t *testing.T) {
	// Disable color for deterministic test output
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	t.Run("decision mismatch shows top-level reasoning", func(t *testing.T) {
		result := TestResult{
			CaseID: "test-mismatch",
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
			Failures: []string{`expected "deny", actual "allow"`},
		}

		output := formatSingleTestResult(result)
		assert.Contains(t, output, "reasoning: AI determined request is safe")
	})

	t.Run("unexpected policies suppress top-level reasoning, show per-policy", func(t *testing.T) {
		result := TestResult{
			CaseID: "test-unexpected",
			Engine: "ai",
			Model:  "anthropic:claude-haiku",
			Status: "failed",
			Expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "Check system access", Decision: "deny"},
				},
			},
			Actual: ActualResult{
				Decision:   "deny",
				Confidence: 1.0,
				Reasoning:  "Overall deny reasoning that should be hidden",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "Check command execution", Decision: "deny", ElapsedMs: 986,
						Reasoning: "Dangerous tool blocked because shell__run_command is on the blocklist"},
					{PolicyName: "Check system access", Decision: "deny", ElapsedMs: 1454,
						Reasoning: "Sudo command detected"},
					{PolicyName: "Check credential access", Decision: "allow", ElapsedMs: 800},
				},
			},
			Failures: []string{"1 unexpected policy match(es) — see highlighted ► above"},
		}

		output := formatSingleTestResult(result)
		// Top-level reasoning should NOT appear (decision matches)
		assert.NotContains(t, output, "reasoning: Overall deny reasoning")
		// Unexpected policy reasoning SHOULD appear (Check command execution is unexpected)
		assert.Contains(t, output, "Dangerous tool blocked because")
		// Expected policy reasoning should NOT appear (Check system access is expected)
		assert.NotContains(t, output, "Sudo command detected")
	})
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

		// In the policy quality view, cached results are included in passed/failed counts:
		// 3 fresh passed + 8 cached passed = 11 passed
		// 1 fresh failed + 2 cached failed = 3 failed
		// Skipped = 0 (cached tests are not "skipped" anymore)
		summary := &RunResult{
			TotalCases:    15,
			Passed:        11,
			Failed:        3,
			Errored:       1,
			CachedCount:   10,
			MatchRate:     float64(11) / float64(15),
			ThresholdsMet: false,
		}

		output := formatTextSummary(suite, summary, results, nil, nil)
		assert.Contains(t, output, "Cached:  10 from previous run (8 passed, 2 failed)")
		assert.Contains(t, output, "11 passed, 3 failed, 1 errored")
		assert.Contains(t, output, "--retry-failed")
	})

	t.Run("retry hint for cached failures in failed count", func(t *testing.T) {
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

		// Cached failures are now in Failed count, so retry hint comes from Failed > 0
		summary := &RunResult{
			TotalCases:    10,
			Passed:        8,
			Failed:        2,
			CachedCount:   7,
			MatchRate:     0.8,
			ThresholdsMet: true,
		}

		output := formatTextSummary(suite, summary, results, nil, nil)
		assert.Contains(t, output, "Cached:  7 from previous run (5 passed, 2 failed)")
		assert.Contains(t, output, "--retry-failed")
	})
}

// TestCalculateResults_CachedResultsCountAsOriginalStatus verifies that cached results
// are counted by their original status (passed/failed) rather than as "skipped",
// producing summary stats that reflect cumulative policy quality.
func TestCalculateResults_CachedResultsCountAsOriginalStatus(t *testing.T) {
	runner := &Runner{
		suite: &Suite{
			Acceptance: AcceptanceConfig{MinMatchRate: 0.8},
		},
	}

	results := []TestResult{
		{Status: "passed"},
		{Status: "passed"},
		{Status: "failed"},
		// Cached results — should count as their original status
		{Status: "skipped", Error: &TestError{Type: "cached", Message: "cached passed"}},
		{Status: "skipped", Error: &TestError{Type: "cached", Message: "cached passed"}},
		{Status: "skipped", Error: &TestError{Type: "cached", Message: "cached passed"}},
		{Status: "skipped", Error: &TestError{Type: "cached", Message: "cached failed"}},
		{Status: "skipped", Error: &TestError{Type: "cached", Message: "cached errored"}},
		// Rate-limited — genuinely skipped
		{Status: "skipped", Error: &TestError{Type: "rate_limited", Message: "rate limited"}},
	}

	summary := runner.calculateResults(results)

	assert.Equal(t, 9, summary.TotalCases)
	assert.Equal(t, 5, summary.Passed, "should include 2 fresh + 3 cached passed")
	assert.Equal(t, 2, summary.Failed, "should include 1 fresh + 1 cached failed")
	assert.Equal(t, 1, summary.Errored, "should include 1 cached errored")
	assert.Equal(t, 1, summary.Skipped, "only rate-limited should be skipped")
	assert.Equal(t, 5, summary.CachedCount, "track cached count for info display")
	assert.Equal(t, 1, summary.RateLimited)
	// Match rate = 5 / (5 + 2) = 5/7 ≈ 0.714 (errors excluded from denominator)
	assert.InDelta(t, float64(5)/float64(7), summary.MatchRate, 0.001)
	assert.False(t, summary.ThresholdsMet, "71.4% < 80% threshold")
}

// TestCalculateResults_ExtraPolicyOnlySplitsFromFailed verifies that extra-policy-only
// failures are counted in ExtraPolicyOnly and NOT in Failed. The spec requires that
// "Extra column splits out from Fail" — they are mutually exclusive categories.
// MatchRate should use decided = Passed + Failed + ExtraPolicyOnly.
func TestCalculateResults_ExtraPolicyOnlySplitsFromFailed(t *testing.T) {
	runner := &Runner{
		suite: &Suite{
			Acceptance: AcceptanceConfig{MinMatchRate: 0.5},
		},
	}

	results := []TestResult{
		{Status: "passed"},
		{Status: "passed"},
		{Status: "passed"},
		{Status: "failed"},                              // real failure
		{Status: "failed", ExtraPolicyOnly: true},        // extra-policy-only
		{Status: "failed", ExtraPolicyOnly: true},        // extra-policy-only
		{Status: "errored", Error: &TestError{Type: "timeout", Message: "timed out"}},
	}

	summary := runner.calculateResults(results)

	assert.Equal(t, 7, summary.TotalCases)
	assert.Equal(t, 3, summary.Passed)
	assert.Equal(t, 1, summary.Failed, "only real failures, not extra-policy-only")
	assert.Equal(t, 2, summary.ExtraPolicyOnly, "extra-policy-only counted separately")
	assert.Equal(t, 1, summary.Errored)

	// decided = Passed + Failed + ExtraPolicyOnly = 3 + 1 + 2 = 6
	// MatchRate (lenient) = (3+2)/6 = 5/6 ≈ 0.833
	assert.InDelta(t, float64(5)/float64(6), summary.MatchRate, 0.001,
		"MatchRate should be (Passed+ExtraPolicyOnly)/(Passed+Failed+ExtraPolicyOnly)")

	// Threshold check: 0.5 >= 0.5 → met
	assert.True(t, summary.ThresholdsMet)
}

// TestCalculateResults_ZeroDecidedThresholdsVacuouslyMet verifies that when no tests
// produce a pass/fail decision (e.g., all skipped, all errored, or engine had no
// matching cases), thresholds are vacuously met rather than failing on 0/0.
func TestCalculateResults_ZeroDecidedThresholdsVacuouslyMet(t *testing.T) {
	tests := []struct {
		name    string
		results []TestResult
	}{
		{
			name:    "empty results (no matching cases for engine)",
			results: []TestResult{},
		},
		{
			name: "all skipped (rate limited)",
			results: []TestResult{
				{Status: "skipped", Error: &TestError{Type: "rate_limited", Message: "rate limited"}},
				{Status: "skipped", Error: &TestError{Type: "rate_limited", Message: "rate limited"}},
			},
		},
		{
			name: "all errored",
			results: []TestResult{
				{Status: "errored"},
				{Status: "errored"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &Runner{
				suite: &Suite{
					Acceptance: AcceptanceConfig{MinMatchRate: 1.0},
				},
			}

			summary := runner.calculateResults(tt.results)

			assert.Equal(t, 0.0, summary.MatchRate)
			assert.True(t, summary.ThresholdsMet, "0/0 decided tests should vacuously meet thresholds")
		})
	}
}

// TestFormatJSONOutput_OverallSummaryAggregates verifies that the JSON output's
// overall_summary contains pre-computed aggregate totals that match the policy
// quality view (cached results counted by original status).
func TestFormatJSONOutput_OverallSummaryAggregates(t *testing.T) {
	suite := &Suite{
		BundleID: "test",
		Version:  "v1",
		Acceptance: AcceptanceConfig{
			MinMatchRate: 0.8,
		},
	}

	results := []TestResult{
		// Model A: 2 fresh passed, 1 failed, 2 cached passed, 1 cached errored
		{Engine: "ai", Model: "openai:gpt-5", Status: "passed"},
		{Engine: "ai", Model: "openai:gpt-5", Status: "passed"},
		{Engine: "ai", Model: "openai:gpt-5", Status: "failed"},
		{Engine: "ai", Model: "openai:gpt-5", Status: "skipped", Error: &TestError{Type: "cached", Message: "cached passed"}},
		{Engine: "ai", Model: "openai:gpt-5", Status: "skipped", Error: &TestError{Type: "cached", Message: "cached passed"}},
		{Engine: "ai", Model: "openai:gpt-5", Status: "skipped", Error: &TestError{Type: "cached", Message: "cached errored"}},
		// Model B: 1 fresh passed, 1 cached failed
		{Engine: "ai", Model: "anthropic:haiku", Status: "passed"},
		{Engine: "ai", Model: "anthropic:haiku", Status: "skipped", Error: &TestError{Type: "cached", Message: "cached failed"}},
	}

	summary := &RunResult{ThresholdsMet: false}
	jsonStr, err := formatJSONOutput(suite, results, summary, nil, nil)
	require.NoError(t, err)

	var output JSONOutput
	err = json.Unmarshal([]byte(jsonStr), &output)
	require.NoError(t, err)

	// Check overall summary aggregates
	assert.Equal(t, 2, output.OverallSummary.ModelsTested)
	assert.Equal(t, 8, output.OverallSummary.TotalCases)
	assert.Equal(t, 5, output.OverallSummary.Passed, "3 fresh + 2 cached passed")
	assert.Equal(t, 2, output.OverallSummary.Failed, "1 fresh + 1 cached failed")
	assert.Equal(t, 1, output.OverallSummary.Errored, "1 cached errored")
	assert.Equal(t, 0, output.OverallSummary.Skipped)
	// Match rate = 5 / (5 + 2) = 5/7 ≈ 0.714 (errors excluded from denominator)
	assert.InDelta(t, float64(5)/float64(7), output.OverallSummary.MatchRate, 0.001)

	// Check per-model summaries also use effective status
	require.Len(t, output.ResultsByModel, 2)
	modelA := output.ResultsByModel[0].Summary
	assert.Equal(t, 6, modelA.TotalCases)
	assert.Equal(t, 4, modelA.Passed, "2 fresh + 2 cached passed")
	assert.Equal(t, 1, modelA.Failed)
	assert.Equal(t, 1, modelA.Errored, "cached errored counts as errored")
	assert.Equal(t, 0, modelA.Skipped)
	// Match rate = 4 / (4 + 1) = 4/5 = 0.8 (errors excluded from denominator)
	assert.InDelta(t, float64(4)/float64(5), modelA.MatchRate, 0.001)

	modelB := output.ResultsByModel[1].Summary
	assert.Equal(t, 2, modelB.TotalCases)
	assert.Equal(t, 1, modelB.Passed)
	assert.Equal(t, 1, modelB.Failed, "cached failed counts as failed")
	assert.InDelta(t, 0.5, modelB.MatchRate, 0.001)
}

// TestFormatJUnitOutput_CachedResultsAndProperties verifies that JUnit output
// classifies cached results by their original status and includes aggregate
// summary properties matching the policy quality view.
func TestFormatJUnitOutput_CachedResultsAndProperties(t *testing.T) {
	suite := &Suite{
		BundleID: "test",
		Version:  "v1",
		Acceptance: AcceptanceConfig{
			MinMatchRate: 0.8,
		},
	}

	results := []TestResult{
		// Fresh results
		{CaseID: "fresh-pass", Status: "passed", Expected: ExpectedResult{Decision: "allow"}},
		{CaseID: "fresh-fail", Status: "failed", Expected: ExpectedResult{Decision: "deny"},
			Actual: ActualResult{Decision: "allow"}, Failures: []string{"wrong decision"}},
		// Cached results — should render as their original status, not <skipped>
		{CaseID: "cached-pass", Status: "skipped", Expected: ExpectedResult{Decision: "allow"},
			Error: &TestError{Type: "cached", Message: "cached passed"}},
		{CaseID: "cached-fail", Status: "skipped", Expected: ExpectedResult{Decision: "deny"},
			Error: &TestError{Type: "cached", Message: "cached failed"}},
		{CaseID: "cached-err", Status: "skipped", Expected: ExpectedResult{Decision: "allow"},
			Error: &TestError{Type: "cached", Message: "cached errored"}},
		// Rate-limited — genuinely skipped
		{CaseID: "rate-limited", Status: "skipped", Expected: ExpectedResult{Decision: "allow"},
			Error: &TestError{Type: "rate_limited", Message: "rate limited"}},
	}

	// Summary uses policy quality view: cached results counted by original status
	summary := &RunResult{
		TotalCases:  6,
		Passed:      2, // 1 fresh + 1 cached
		Failed:      2, // 1 fresh + 1 cached
		Errored:     1, // 1 cached
		Skipped:     1, // only rate-limited
		CachedCount: 3,
	}

	xmlStr, err := formatJUnitOutput(suite, results, summary)
	require.NoError(t, err)

	// Parse the XML to verify structure
	var testSuites JUnitTestSuites
	err = xml.Unmarshal([]byte(xmlStr), &testSuites)
	require.NoError(t, err)

	require.Len(t, testSuites.TestSuite, 1)
	ts := testSuites.TestSuite[0]

	// Suite-level counts match policy quality view
	assert.Equal(t, 6, ts.Tests)
	assert.Equal(t, 2, ts.Failures)
	assert.Equal(t, 1, ts.Errors)
	assert.Equal(t, 1, ts.Skipped)

	// Verify properties: bundle_id, version, match_rate (passed/failed/errored removed
	// because they duplicate the standard tests/failures/errors XML attributes)
	propMap := make(map[string]string)
	for _, p := range ts.Properties {
		propMap[p.Name] = p.Value
	}
	assert.Equal(t, "test", propMap["bundle_id"])
	assert.Contains(t, propMap["match_rate"], "0.4") // 2/5 = 0.4
	assert.NotContains(t, propMap, "passed", "passed property should not be in JUnit output (duplicates standard attribute)")
	assert.NotContains(t, propMap, "failed", "failed property should not be in JUnit output (duplicates standard attribute)")
	assert.NotContains(t, propMap, "errored", "errored property should not be in JUnit output (duplicates standard attribute)")

	// Verify individual test case elements match effective status
	require.Len(t, ts.TestCases, 6)
	caseMap := make(map[string]JUnitTestCase)
	for _, tc := range ts.TestCases {
		caseMap[tc.Name] = tc
	}

	// Fresh pass: no failure/error/skipped elements
	assert.Nil(t, caseMap["fresh-pass"].Failure)
	assert.Nil(t, caseMap["fresh-pass"].Error)
	assert.Nil(t, caseMap["fresh-pass"].Skipped)

	// Fresh fail: has failure element with actual details
	assert.NotNil(t, caseMap["fresh-fail"].Failure)
	assert.Contains(t, caseMap["fresh-fail"].Failure.Message, "Expected 'deny', actual 'allow'")

	// Cached pass: no elements (it's a pass)
	assert.Nil(t, caseMap["cached-pass"].Failure)
	assert.Nil(t, caseMap["cached-pass"].Error)
	assert.Nil(t, caseMap["cached-pass"].Skipped)

	// Cached fail: has failure element indicating cached origin
	assert.NotNil(t, caseMap["cached-fail"].Failure)
	assert.Equal(t, "CachedFailure", caseMap["cached-fail"].Failure.Type)
	assert.Contains(t, caseMap["cached-fail"].Failure.Message, "previous run")

	// Cached errored: has error element indicating cached origin
	assert.NotNil(t, caseMap["cached-err"].Error)
	assert.Equal(t, "CachedError", caseMap["cached-err"].Error.Type)
	assert.Contains(t, caseMap["cached-err"].Error.Message, "previous run")

	// Rate-limited: still shows as skipped
	assert.NotNil(t, caseMap["rate-limited"].Skipped)
	assert.Equal(t, "rate limited", caseMap["rate-limited"].Skipped.Message)
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

	t.Run("renders summary for single model", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "openai:gpt-5", Passed: 8, Failed: 2, Errored: 0, MatchRate: 0.8, AvgMs: 1200, TotalMs: 12000},
		}

		output := formatModelComparison(entries)

		assert.Contains(t, output, "Model Summary")
		assert.NotContains(t, output, "Model Comparison")
		assert.Contains(t, output, "openai:gpt-5")
		assert.Contains(t, output, "80.0%")
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

// TestFormatModelComparison_ColumnAlignment verifies that all rows in the
// comparison table have equal width, catching column overflow bugs where
// formatted values exceed their field width and push subsequent columns out.
func TestFormatModelComparison_ColumnAlignment(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	// dataRowWidths extracts the display width (rune count) of each data row.
	// Uses rune count rather than byte length because multibyte characters like
	// the em dash (—) occupy 1 display column but 3 bytes.
	dataRowWidths := func(output string) (headerWidth int, rowWidths []int) {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		// lines[0] = top separator ("── Model ...")
		// lines[1] = header row
		// lines[2..n-1] = data rows
		// lines[n] = bottom separator ("───...")
		// optional: footnote
		require.GreaterOrEqual(t, len(lines), 4, "table must have separator + header + at least 1 data row + separator")
		headerWidth = utf8.RuneCountInString(lines[1])
		for _, line := range lines[2:] {
			if strings.HasPrefix(line, "─") || strings.Contains(line, "previous run") {
				continue
			}
			rowWidths = append(rowWidths, utf8.RuneCountInString(line))
		}
		return headerWidth, rowWidths
	}

	t.Run("small values stay aligned", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "cel", Passed: 10, Failed: 0, Errored: 0, MatchRate: 1.0, AvgMs: 2, TotalMs: 20},
			{Model: "openai:gpt-5", Passed: 8, Failed: 2, Errored: 0, MatchRate: 0.8, AvgMs: 1200, TotalMs: 12000},
		}
		output := formatModelComparison(entries)
		headerWidth, rowWidths := dataRowWidths(output)
		for i, rw := range rowWidths {
			assert.Equal(t, headerWidth, rw, "row %d width %d != header width %d\n%s", i, rw, headerWidth, output)
		}
	})

	t.Run("large totals stay aligned", func(t *testing.T) {
		// TotalMs: 2349500 → "2349.5 s" (8 chars) — would have overflowed old %7s
		entries := []ModelComparisonEntry{
			{Model: "cel", Passed: 500, Failed: 0, Errored: 0, MatchRate: 1.0, AvgMs: 2, TotalMs: 1000},
			{Model: "openai:gpt-5", Passed: 400, Failed: 100, Errored: 0, MatchRate: 0.8, AvgMs: 4699, TotalMs: 2349500},
		}
		output := formatModelComparison(entries)
		headerWidth, rowWidths := dataRowWidths(output)
		for i, rw := range rowWidths {
			assert.Equal(t, headerWidth, rw, "row %d width %d != header width %d\n%s", i, rw, headerWidth, output)
		}
	})

	t.Run("large avg ms stays aligned", func(t *testing.T) {
		// AvgMs: 150000 → "150000 ms" (9 chars) — would have overflowed old %8s
		entries := []ModelComparisonEntry{
			{Model: "fast-model", Passed: 50, Failed: 0, Errored: 0, MatchRate: 1.0, AvgMs: 500, TotalMs: 25000},
			{Model: "slow-model", Passed: 40, Failed: 10, Errored: 0, MatchRate: 0.8, AvgMs: 150000, TotalMs: 7500000},
		}
		output := formatModelComparison(entries)
		headerWidth, rowWidths := dataRowWidths(output)
		for i, rw := range rowWidths {
			assert.Equal(t, headerWidth, rw, "row %d width %d != header width %d\n%s", i, rw, headerWidth, output)
		}
	})

	t.Run("large pass fail err counts stay aligned", func(t *testing.T) {
		// Passed: 50000, Failed: 12000, Errored: 1500 — would have overflowed old %4d/%3d
		entries := []ModelComparisonEntry{
			{Model: "cel", Passed: 50000, Failed: 0, Errored: 0, MatchRate: 1.0, AvgMs: 1, TotalMs: 50000},
			{Model: "openai:gpt-5", Passed: 50000, Failed: 12000, Errored: 1500, MatchRate: 0.787, AvgMs: 2000, TotalMs: 127000000},
		}
		output := formatModelComparison(entries)
		headerWidth, rowWidths := dataRowWidths(output)
		for i, rw := range rowWidths {
			assert.Equal(t, headerWidth, rw, "row %d width %d != header width %d\n%s", i, rw, headerWidth, output)
		}
	})

	t.Run("stability column stays aligned with large values", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "cel", Passed: 50000, Failed: 0, Errored: 0, MatchRate: 1.0, AvgMs: 1, TotalMs: 50000},
			{Model: "openai:gpt-5", Passed: 50000, Failed: 12000, Errored: 1500, MatchRate: 0.787, AvgMs: 150000, TotalMs: 2349500, Stability: 0.96, StabilityTests: 10},
		}
		output := formatModelComparison(entries)
		headerWidth, rowWidths := dataRowWidths(output)
		for i, rw := range rowWidths {
			assert.Equal(t, headerWidth, rw, "row %d width %d != header width %d\n%s", i, rw, headerWidth, output)
		}
	})

	t.Run("mixed cached and non-cached stay aligned", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "openai:gpt-5", Passed: 50000, Failed: 12000, Errored: 1500, MatchRate: 0.787, AvgMs: 150000, TotalMs: 2349500},
			{Model: "anthropic:claude", Passed: 30000, Failed: 5000, Errored: 200, MatchRate: 0.852, AvgMs: 120000, TotalMs: 1800000, FromCache: true},
		}
		output := formatModelComparison(entries)
		headerWidth, rowWidths := dataRowWidths(output)
		for i, rw := range rowWidths {
			assert.Equal(t, headerWidth, rw, "row %d width %d != header width %d\n%s", i, rw, headerWidth, output)
		}
	})
}

// TestBuildModelComparison verifies that the model summary table always reflects
// the current run's results, not stale state data. This is a regression test for
// the bug where the Summary showed 50 results but Model Summary showed only 9.
func TestBuildModelComparison(t *testing.T) {
	t.Run("derives AI stats from results not state", func(t *testing.T) {
		// Set up state with stale data (fewer results than current run)
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		policyHashes := []string{"sha256:policy1"}
		sm.RecordResult("sha256:test1", "case-1", policyHashes, "openai:gpt-5", &CachedResult{
			Status: "passed", DurationMs: 100,
		})

		runner := &Runner{
			stateManager: sm,
			policyHashes: policyHashes,
		}

		// Current run has 3 results for this model
		results := []TestResult{
			{Engine: "ai", Model: "openai:gpt-5", Status: "passed", ElapsedMs: 100},
			{Engine: "ai", Model: "openai:gpt-5", Status: "failed", ElapsedMs: 200},
			{Engine: "ai", Model: "openai:gpt-5", Status: "passed", ElapsedMs: 150},
		}

		entries := runner.buildModelComparison(results)
		require.Len(t, entries, 1)
		assert.Equal(t, "openai:gpt-5", entries[0].Model)
		assert.Equal(t, 2, entries[0].Passed, "should count from results, not state")
		assert.Equal(t, 1, entries[0].Failed, "should count from results, not state")
		assert.False(t, entries[0].FromCache)
	})

	t.Run("augments with historical models from state", func(t *testing.T) {
		sm, _ := NewStateManager("", "test-suite", "1.0.0", 0)
		policyHashes := []string{"sha256:policy1"}
		// Historical model not in current run
		sm.RecordResult("sha256:test1", "case-1", policyHashes, "anthropic:claude", &CachedResult{
			Status: "passed", DurationMs: 5000,
		})

		runner := &Runner{
			stateManager: sm,
			policyHashes: policyHashes,
		}

		// Current run only has openai
		results := []TestResult{
			{Engine: "ai", Model: "openai:gpt-5", Status: "passed", ElapsedMs: 100},
		}

		entries := runner.buildModelComparison(results)
		require.Len(t, entries, 2, "should include both current and historical models")

		// Find entries by model name (order may vary before sort)
		modelMap := make(map[string]ModelComparisonEntry)
		for _, e := range entries {
			modelMap[e.Model] = e
		}

		openai := modelMap["openai:gpt-5"]
		assert.Equal(t, 1, openai.Passed)
		assert.False(t, openai.FromCache)

		anthropic := modelMap["anthropic:claude"]
		assert.Equal(t, 1, anthropic.Passed)
		assert.True(t, anthropic.FromCache)
	})

	t.Run("error on init results appear in summary", func(t *testing.T) {
		runner := &Runner{}

		// Error-on-init results now have Engine and Model set
		results := []TestResult{
			{Engine: "ai", Model: "openai:gpt-5", Status: "passed", ElapsedMs: 100},
			{Engine: "ai", Model: "anthropic:claude", Status: "errored", ElapsedMs: 50},
			{Engine: "ai", Model: "anthropic:claude", Status: "errored", ElapsedMs: 30},
		}

		entries := runner.buildModelComparison(results)
		require.Len(t, entries, 2)

		modelMap := make(map[string]ModelComparisonEntry)
		for _, e := range entries {
			modelMap[e.Model] = e
		}

		assert.Equal(t, 1, modelMap["openai:gpt-5"].Passed)
		assert.Equal(t, 2, modelMap["anthropic:claude"].Errored)
	})

	t.Run("includes CEL and AI entries", func(t *testing.T) {
		runner := &Runner{}

		results := []TestResult{
			{Engine: "cel", Status: "passed", ElapsedMs: 5},
			{Engine: "cel", Status: "passed", ElapsedMs: 3},
			{Engine: "ai", Model: "openai:gpt-5", Status: "passed", ElapsedMs: 100},
		}

		entries := runner.buildModelComparison(results)
		require.Len(t, entries, 2)

		modelMap := make(map[string]ModelComparisonEntry)
		for _, e := range entries {
			modelMap[e.Model] = e
		}

		assert.Equal(t, 2, modelMap["cel"].Passed)
		assert.Equal(t, 1, modelMap["openai:gpt-5"].Passed)
	})

	t.Run("cached-skipped results counted by original status", func(t *testing.T) {
		runner := &Runner{}

		results := []TestResult{
			{Engine: "ai", Model: "openai:gpt-5", Status: "skipped", Error: &TestError{
				Type: "cached", Message: "cached passed",
			}},
			{Engine: "ai", Model: "openai:gpt-5", Status: "skipped", Error: &TestError{
				Type: "cached", Message: "cached failed",
			}},
		}

		entries := runner.buildModelComparison(results)
		require.Len(t, entries, 1)
		assert.Equal(t, 1, entries[0].Passed)
		assert.Equal(t, 1, entries[0].Failed)
	})

	t.Run("cached-skipped results include elapsed time in model comparison", func(t *testing.T) {
		runner := &Runner{}

		results := []TestResult{
			{Engine: "ai", Model: "openai:gpt-5", Status: "skipped", ElapsedMs: 3200, Error: &TestError{
				Type: "cached", Message: "cached passed",
			}},
			{Engine: "ai", Model: "openai:gpt-5", Status: "skipped", ElapsedMs: 1500, Error: &TestError{
				Type: "cached", Message: "cached passed",
			}},
		}

		entries := runner.buildModelComparison(results)
		require.Len(t, entries, 1)
		assert.Equal(t, int64(4700), entries[0].TotalMs, "TotalMs should sum cached durations")
		assert.Equal(t, int64(2350), entries[0].AvgMs, "AvgMs should average cached durations")
	})
}

// TestSuite_ResolveAPIKey verifies the deterministic API key lookup:
// 1. Per-model api_key takes precedence
// 2. Falls back to provider-level api_key
// 3. Returns empty if neither configured
func TestSuite_ResolveAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		suite   Suite
		model   ModelConfig
		wantKey string
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

// TestFilterTestCases_Stats verifies that filterTestCases returns accurate
// intermediate counts at each filter stage.
func TestFilterTestCases_Stats(t *testing.T) {
	tests := []struct {
		name      string
		cases     []TestCase
		opts      RunnerOptions
		wantCount int
		wantStats FilterStats
	}{
		{
			name: "no filters returns all cases",
			cases: []TestCase{
				{CaseID: "a", Tags: []string{"x"}},
				{CaseID: "b", Tags: []string{"y"}},
				{CaseID: "c", Tags: []string{"x", "y"}},
			},
			opts:      RunnerOptions{},
			wantCount: 3,
			wantStats: FilterStats{
				Total: 3, AfterPattern: 3, AfterTags: 3, AfterExclude: 3,
			},
		},
		{
			name: "case pattern filters first",
			cases: []TestCase{
				{CaseID: "ai-req-001"},
				{CaseID: "ai-req-002"},
				{CaseID: "cel-req-001"},
			},
			opts:      RunnerOptions{CasePattern: "ai-*"},
			wantCount: 2,
			wantStats: FilterStats{
				Total: 3, AfterPattern: 2, AfterTags: 2, AfterExclude: 2,
				CasePattern: "ai-*",
			},
		},
		{
			name: "tags filter after pattern",
			cases: []TestCase{
				{CaseID: "a", Tags: []string{"security"}},
				{CaseID: "b", Tags: []string{"perf"}},
				{CaseID: "c", Tags: []string{"security", "perf"}},
			},
			opts:      RunnerOptions{Tags: "security"},
			wantCount: 2,
			wantStats: FilterStats{
				Total: 3, AfterPattern: 3, AfterTags: 2, AfterExclude: 2,
				Tags: "security",
			},
		},
		{
			name: "exclude tags filter after tags",
			cases: []TestCase{
				{CaseID: "a", Tags: []string{"security"}},
				{CaseID: "b", Tags: []string{"security", "slow"}},
				{CaseID: "c", Tags: []string{"perf"}},
			},
			opts:      RunnerOptions{Tags: "security", ExcludeTags: "slow"},
			wantCount: 1,
			wantStats: FilterStats{
				Total: 3, AfterPattern: 3, AfterTags: 2, AfterExclude: 1,
				Tags: "security", ExcludeTags: "slow",
			},
		},
		{
			name: "all filters chained",
			cases: []TestCase{
				{CaseID: "ai-req-001", Tags: []string{"security"}},
				{CaseID: "ai-req-002", Tags: []string{"security", "slow"}},
				{CaseID: "cel-req-001", Tags: []string{"security"}},
				{CaseID: "ai-req-003", Tags: []string{"perf"}},
			},
			opts:      RunnerOptions{CasePattern: "ai-*", Tags: "security", ExcludeTags: "slow"},
			wantCount: 1,
			wantStats: FilterStats{
				Total: 4, AfterPattern: 3, AfterTags: 2, AfterExclude: 1,
				CasePattern: "ai-*", Tags: "security", ExcludeTags: "slow",
			},
		},
		{
			name: "zero match from pattern",
			cases: []TestCase{
				{CaseID: "a"},
				{CaseID: "b"},
			},
			opts:      RunnerOptions{CasePattern: "nonexistent-*"},
			wantCount: 0,
			wantStats: FilterStats{
				Total: 2, AfterPattern: 0, AfterTags: 0, AfterExclude: 0,
				CasePattern: "nonexistent-*",
			},
		},
		{
			name: "zero match from tags",
			cases: []TestCase{
				{CaseID: "a", Tags: []string{"x"}},
				{CaseID: "b", Tags: []string{"y"}},
			},
			opts:      RunnerOptions{Tags: "nonexistent"},
			wantCount: 0,
			wantStats: FilterStats{
				Total: 2, AfterPattern: 2, AfterTags: 0, AfterExclude: 0,
				Tags: "nonexistent",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Runner{
				testCases: tt.cases,
				opts:      tt.opts,
			}
			cases, stats := r.filterTestCases()
			assert.Equal(t, tt.wantCount, len(cases))
			assert.Equal(t, tt.wantStats, stats)
		})
	}
}

// TestFormatSingleTestResult_TestNumbering verifies that [N/M] prefix appears
// when TestNumber and TestTotal are set, and is omitted when they are zero.
func TestFormatSingleTestResult_TestNumbering(t *testing.T) {
	// Disable color for deterministic test output
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	t.Run("shows [N/M] when set", func(t *testing.T) {
		result := TestResult{
			CaseID:     "ai-req-030",
			Engine:     "ai",
			Model:      "openai:gpt-5-mini",
			Status:     "passed",
			TestNumber: 3,
			TestTotal:  17,
			Expected:   ExpectedResult{Decision: "deny"},
			Actual:     ActualResult{Decision: "deny", Confidence: 1.0},
		}
		output := formatSingleTestResult(result)
		assert.Contains(t, output, "[3/17]")
		assert.Contains(t, output, "ai-req-030")
	})

	t.Run("omits [N/M] when zero", func(t *testing.T) {
		result := TestResult{
			CaseID:     "ai-req-030",
			Engine:     "ai",
			Model:      "openai:gpt-5-mini",
			Status:     "passed",
			TestNumber: 0,
			TestTotal:  0,
			Expected:   ExpectedResult{Decision: "deny"},
			Actual:     ActualResult{Decision: "deny", Confidence: 1.0},
		}
		output := formatSingleTestResult(result)
		assert.NotContains(t, output, "[0/0]")
		assert.NotContains(t, output, "[/]")
	})

	t.Run("numbering on skipped tests", func(t *testing.T) {
		result := TestResult{
			CaseID:     "ai-req-005",
			Engine:     "ai",
			Model:      "openai:gpt-5",
			Status:     "skipped",
			TestNumber: 1,
			TestTotal:  10,
			Error:      &TestError{Type: "cached", Message: "cached passed"},
		}
		output := formatSingleTestResult(result)
		assert.Contains(t, output, "[1/10]")
		assert.Contains(t, output, "ai-req-005")
	})

	t.Run("numbering on failed tests", func(t *testing.T) {
		result := TestResult{
			CaseID:     "ai-req-010",
			Engine:     "ai",
			Model:      "openai:gpt-5",
			Status:     "failed",
			TestNumber: 5,
			TestTotal:  20,
			Expected:   ExpectedResult{Decision: "deny"},
			Actual:     ActualResult{Decision: "allow", Confidence: 0.8},
			Failures:   []string{"expected deny, got allow"},
		}
		output := formatSingleTestResult(result)
		assert.Contains(t, output, "[5/20]")
	})

	t.Run("numbering on errored tests", func(t *testing.T) {
		result := TestResult{
			CaseID:     "ai-req-015",
			Engine:     "ai",
			Model:      "openai:gpt-5",
			Status:     "errored",
			TestNumber: 7,
			TestTotal:  10,
			Error:      &TestError{Type: "timeout", Message: "timed out"},
		}
		output := formatSingleTestResult(result)
		assert.Contains(t, output, "[7/10]")
	})
}

// TestFormatTestNumberPrefix verifies the [N/M] prefix formatting helper.
func TestFormatTestNumberPrefix(t *testing.T) {
	// Disable color for deterministic test output
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	tests := []struct {
		name       string
		number     int
		total      int
		wantEmpty  bool
		wantPrefix string
	}{
		{name: "both zero", number: 0, total: 0, wantEmpty: true},
		{name: "number zero", number: 0, total: 5, wantEmpty: true},
		{name: "total zero", number: 3, total: 0, wantEmpty: true},
		{name: "valid values", number: 3, total: 17, wantPrefix: "[3/17] "},
		{name: "single test", number: 1, total: 1, wantPrefix: "[1/1] "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTestNumberPrefix(tt.number, tt.total)
			if tt.wantEmpty {
				assert.Empty(t, result)
			} else {
				assert.Equal(t, tt.wantPrefix, result)
			}
		})
	}
}

// TestFormatSingleTestResult_PassRate verifies that pass rate is shown in
// individual test output when history has 2+ entries.
func TestFormatSingleTestResult_PassRate(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	t.Run("no pass rate shown when runs < 2", func(t *testing.T) {
		tr := TestResult{
			CaseID:       "test-001",
			Engine:       "ai",
			Model:        "openai:gpt-5",
			Status:       "passed",
			PassRateRuns: 1,
			PassRate:     1.0,
			Expected:     ExpectedResult{Decision: "deny"},
			Actual:       ActualResult{Decision: "deny"},
		}
		output := formatSingleTestResult(tr)
		assert.NotContains(t, output, "pass rate")
	})

	t.Run("shows pass rate when runs >= 2", func(t *testing.T) {
		tr := TestResult{
			CaseID:       "test-001",
			Engine:       "ai",
			Model:        "openai:gpt-5",
			Status:       "passed",
			PassRate:     0.85,
			PassRateRuns: 20,
			Expected:     ExpectedResult{Decision: "deny"},
			Actual:       ActualResult{Decision: "deny"},
		}
		output := formatSingleTestResult(tr)
		assert.Contains(t, output, "pass rate: 85% (last 20 runs)")
	})

	t.Run("shows policy change window when it differs", func(t *testing.T) {
		tr := TestResult{
			CaseID:                  "test-001",
			Engine:                  "ai",
			Model:                   "openai:gpt-5",
			Status:                  "failed",
			PassRate:                0.74,
			PassRateRuns:            20,
			PassRateSinceChange:     0.80,
			PassRateSinceChangeRuns: 5,
			Expected:                ExpectedResult{Decision: "deny"},
			Actual:                  ActualResult{Decision: "allow"},
			Failures:                []string{"wrong decision"},
		}
		output := formatSingleTestResult(tr)
		assert.Contains(t, output, "pass rate: 74% (last 20 runs)")
		assert.Contains(t, output, "80% since last policy change (5 runs)")
	})

	t.Run("omits policy change window when runs equal total", func(t *testing.T) {
		tr := TestResult{
			CaseID:                  "test-001",
			Engine:                  "ai",
			Model:                   "openai:gpt-5",
			Status:                  "passed",
			PassRate:                0.90,
			PassRateRuns:            10,
			PassRateSinceChange:     0.90,
			PassRateSinceChangeRuns: 10,
			Expected:                ExpectedResult{Decision: "deny"},
			Actual:                  ActualResult{Decision: "deny"},
		}
		output := formatSingleTestResult(tr)
		assert.Contains(t, output, "pass rate: 90% (last 10 runs)")
		assert.NotContains(t, output, "since last policy change")
	})
}

// TestFormatStabilitySection verifies the stability summary output.
func TestFormatStabilitySection(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	t.Run("empty when no tests have history", func(t *testing.T) {
		results := []TestResult{
			{CaseID: "test-1", PassRateRuns: 1}, // insufficient history
		}
		output := formatStabilitySection(results, 0.90)
		assert.Empty(t, output)
	})

	t.Run("all stable", func(t *testing.T) {
		results := []TestResult{
			{CaseID: "test-1", PassRate: 0.95, PassRateRuns: 5, Engine: "ai", Model: "gpt-5"},
			{CaseID: "test-2", PassRate: 1.0, PassRateRuns: 10, Engine: "ai", Model: "gpt-5"},
		}
		output := formatStabilitySection(results, 0.90)
		assert.Contains(t, output, "All 2 tests stable")
		assert.Contains(t, output, ">90%")
	})

	t.Run("flaky tests listed", func(t *testing.T) {
		results := []TestResult{
			{CaseID: "test-stable", PassRate: 0.95, PassRateRuns: 5, Engine: "ai", Model: "gpt-5"},
			{CaseID: "test-flaky", PassRate: 0.74, PassRateRuns: 20, Engine: "ai", Model: "openai:gpt-5"},
			{CaseID: "test-flaky", PassRate: 0.60, PassRateRuns: 10, Engine: "ai", Model: "anthropic:claude"},
		}
		output := formatStabilitySection(results, 0.90)
		assert.Contains(t, output, "Flaky tests")
		assert.Contains(t, output, "test-flaky")
		assert.Contains(t, output, "74% (openai:gpt-5)")
		assert.Contains(t, output, "60% (anthropic:claude)")
		assert.Contains(t, output, "Stable tests: 1/3")
	})
}

// TestFormatModelComparison_StabilityColumn verifies that the Stab% column
// appears only when stability data is available.
func TestFormatModelComparison_StabilityColumn(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	t.Run("no stab column without stability data", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "openai:gpt-5", Passed: 8, Failed: 2, MatchRate: 0.8},
		}
		output := formatModelComparison(entries)
		assert.NotContains(t, output, "Stab%")
	})

	t.Run("stab column shown when stability data present", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "openai:gpt-5", Passed: 8, Failed: 2, MatchRate: 0.8, Stability: 0.96, StabilityTests: 10},
			{Model: "anthropic:claude", Passed: 7, Failed: 3, MatchRate: 0.7, Stability: 0.87, StabilityTests: 8},
		}
		output := formatModelComparison(entries)
		assert.Contains(t, output, "Stab%")
		assert.Contains(t, output, "96%")
		assert.Contains(t, output, "87%")
	})

	t.Run("dash for models without stability", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "openai:gpt-5", Passed: 8, MatchRate: 0.8, Stability: 0.96, StabilityTests: 10},
			{Model: "cel", Passed: 10, MatchRate: 1.0}, // no stability for CEL
		}
		output := formatModelComparison(entries)
		assert.Contains(t, output, "Stab%")
		assert.Contains(t, output, "—") // dash for CEL
	})
}

// TestJSONOutput_PassRateFields verifies that pass rate fields appear in JSON output.
func TestJSONOutput_PassRateFields(t *testing.T) {
	suite := &Suite{
		BundleID: "test",
		Version:  "v1",
		Acceptance: AcceptanceConfig{
			MinMatchRate: 0.8,
		},
	}

	results := []TestResult{
		{
			CaseID:                  "test-with-history",
			Engine:                  "ai",
			Model:                   "openai:gpt-5",
			Status:                  "passed",
			PassRate:                0.85,
			PassRateRuns:            20,
			PassRateSinceChange:     1.0,
			PassRateSinceChangeRuns: 5,
			Expected:                ExpectedResult{Decision: "deny"},
			Actual:                  ActualResult{Decision: "deny"},
		},
		{
			CaseID:       "test-no-history",
			Engine:       "ai",
			Model:        "openai:gpt-5",
			Status:       "passed",
			PassRateRuns: 0, // no history
			Expected:     ExpectedResult{Decision: "allow"},
			Actual:       ActualResult{Decision: "allow"},
		},
	}

	summary := &RunResult{TotalCases: 2, Passed: 2, ThresholdsMet: true}

	jsonStr, err := formatJSONOutput(suite, results, summary, nil, nil)
	require.NoError(t, err)

	var output JSONOutput
	err = json.Unmarshal([]byte(jsonStr), &output)
	require.NoError(t, err)

	require.Len(t, output.ResultsByModel, 1)
	modelResults := output.ResultsByModel[0].Results

	// Test with history should have pass_rate fields
	withHistory := modelResults[0]
	require.NotNil(t, withHistory.PassRate)
	assert.InDelta(t, 0.85, *withHistory.PassRate, 0.001)
	require.NotNil(t, withHistory.PassRateRuns)
	assert.Equal(t, 20, *withHistory.PassRateRuns)
	require.NotNil(t, withHistory.PassRateSincePolicyChange)
	assert.InDelta(t, 1.0, *withHistory.PassRateSincePolicyChange, 0.001)

	// Test without history should not have pass_rate fields
	noHistory := modelResults[1]
	assert.Nil(t, noHistory.PassRate)
	assert.Nil(t, noHistory.PassRateRuns)
}

// TestValidateSuiteSchema_StabilityThreshold verifies that the suite schema
// validation rejects out-of-range stability_threshold values.
func TestValidateSuiteSchema_StabilityThreshold(t *testing.T) {
	validSuite := func() *Suite {
		return &Suite{
			Version:  "v1",
			BundleID: "test",
			Policies: PoliciesConfig{CELRequestRules: "rules.yaml"},
			Acceptance: AcceptanceConfig{
				MinMatchRate: 0.85,
			},
		}
	}

	t.Run("nil threshold is valid (uses default)", func(t *testing.T) {
		r := &Runner{}
		suite := validSuite()
		assert.NoError(t, r.validateSuiteSchema(suite))
	})

	t.Run("valid threshold accepted", func(t *testing.T) {
		r := &Runner{}
		suite := validSuite()
		threshold := 0.75
		suite.Acceptance.StabilityThreshold = &threshold
		assert.NoError(t, r.validateSuiteSchema(suite))
	})

	t.Run("threshold above 1.0 rejected", func(t *testing.T) {
		r := &Runner{}
		suite := validSuite()
		threshold := 1.5
		suite.Acceptance.StabilityThreshold = &threshold
		err := r.validateSuiteSchema(suite)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stability_threshold must be between 0.0 and 1.0")
	})

	t.Run("negative threshold rejected", func(t *testing.T) {
		r := &Runner{}
		suite := validSuite()
		threshold := -0.1
		suite.Acceptance.StabilityThreshold = &threshold
		err := r.validateSuiteSchema(suite)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stability_threshold must be between 0.0 and 1.0")
	})

	t.Run("boundary values accepted", func(t *testing.T) {
		r := &Runner{}
		for _, val := range []float64{0.0, 1.0} {
			suite := validSuite()
			threshold := val
			suite.Acceptance.StabilityThreshold = &threshold
			assert.NoError(t, r.validateSuiteSchema(suite), "threshold=%f should be valid", val)
		}
	})
}

// TestFormatModelComparison_ExtraPolicyColumns verifies that the Extra and Strict%
// columns appear only when at least one model has ExtraPolicyOnly > 0, and that
// they are absent when no extra-policy-only failures exist.
func TestFormatModelComparison_ExtraPolicyColumns(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = origColor }()

	t.Run("no extra columns without extra-policy-only data", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "openai:gpt-5", Passed: 8, Failed: 2, MatchRate: 0.8},
		}
		output := formatModelComparison(entries)
		assert.NotContains(t, output, "Extra")
		assert.NotContains(t, output, "Strict%")
	})

	t.Run("extra columns shown when extra-policy-only data present", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "openai:gpt-5", Passed: 7, Failed: 1, ExtraPolicyOnly: 2, Errored: 0, MatchRate: 0.9, StrictMatchRate: 0.7, AvgMs: 1200, TotalMs: 12000},
			{Model: "anthropic:claude", Passed: 9, Failed: 1, ExtraPolicyOnly: 0, Errored: 0, MatchRate: 0.9, StrictMatchRate: 0.9, AvgMs: 800, TotalMs: 8000},
		}
		output := formatModelComparison(entries)
		assert.Contains(t, output, "Extra")
		assert.Contains(t, output, "Strict%")
		assert.Contains(t, output, "90.0%")  // MatchRate for gpt-5 (lenient)
		assert.Contains(t, output, "70.0%")  // StrictMatchRate for gpt-5 (strict)
	})

	t.Run("extra and stability columns together", func(t *testing.T) {
		entries := []ModelComparisonEntry{
			{Model: "openai:gpt-5", Passed: 7, Failed: 1, ExtraPolicyOnly: 2, MatchRate: 0.9, StrictMatchRate: 0.7, Stability: 0.95, StabilityTests: 5},
		}
		output := formatModelComparison(entries)
		assert.Contains(t, output, "Extra")
		assert.Contains(t, output, "Strict%")
		assert.Contains(t, output, "Stab%")
		assert.Contains(t, output, "95%")
	})

	t.Run("extra column alignment stays correct", func(t *testing.T) {
		// dataRowWidths from TestFormatModelComparison_ColumnAlignment
		dataRowWidths := func(output string) (headerWidth int, rowWidths []int) {
			lines := strings.Split(strings.TrimSpace(output), "\n")
			require.GreaterOrEqual(t, len(lines), 4, "table must have separator + header + at least 1 data row + separator")
			headerWidth = utf8.RuneCountInString(lines[1])
			for _, line := range lines[2:] {
				if strings.HasPrefix(line, "─") || strings.Contains(line, "previous run") {
					continue
				}
				rowWidths = append(rowWidths, utf8.RuneCountInString(line))
			}
			return headerWidth, rowWidths
		}

		entries := []ModelComparisonEntry{
			{Model: "cel", Passed: 50, Failed: 0, ExtraPolicyOnly: 0, Errored: 0, MatchRate: 1.0, StrictMatchRate: 1.0, AvgMs: 2, TotalMs: 100},
			{Model: "openai:gpt-5", Passed: 35, Failed: 5, ExtraPolicyOnly: 10, Errored: 0, MatchRate: 0.9, StrictMatchRate: 0.7, AvgMs: 1200, TotalMs: 60000},
		}
		output := formatModelComparison(entries)
		headerWidth, rowWidths := dataRowWidths(output)
		for i, rw := range rowWidths {
			assert.Equal(t, headerWidth, rw, "row %d width %d != header width %d\n%s", i, rw, headerWidth, output)
		}
	})
}

// TestJSONOutput_ExtraPolicyOnlyFields verifies that extra-policy-only fields
// appear in JSON output when present.
func TestJSONOutput_ExtraPolicyOnlyFields(t *testing.T) {
	suite := &Suite{
		BundleID: "test",
		Version:  "v1",
		Acceptance: AcceptanceConfig{
			MinMatchRate: 0.8,
		},
	}

	results := []TestResult{
		{
			CaseID:          "test-extra-policy",
			Engine:          "ai",
			Model:           "openai:gpt-5",
			Status:          "failed",
			ExtraPolicyOnly: true,
			Expected:        ExpectedResult{Decision: "deny"},
			Actual:          ActualResult{Decision: "deny"},
			Failures:        []string{"1 unexpected policy match(es)"},
			ElapsedMs:       1200,
		},
		{
			CaseID:    "test-normal-pass",
			Engine:    "ai",
			Model:     "openai:gpt-5",
			Status:    "passed",
			Expected:  ExpectedResult{Decision: "deny"},
			Actual:    ActualResult{Decision: "deny"},
			ElapsedMs: 800,
		},
		{
			CaseID:    "test-real-failure",
			Engine:    "ai",
			Model:     "openai:gpt-5",
			Status:    "failed",
			Expected:  ExpectedResult{Decision: "deny"},
			Actual:    ActualResult{Decision: "allow"},
			Failures:  []string{"expected \"deny\", actual \"allow\""},
			ElapsedMs: 900,
		},
	}

	summary := &RunResult{
		TotalCases:      3,
		Passed:          1,
		Failed:          1,
		ExtraPolicyOnly: 1,
	}

	comparison := []ModelComparisonEntry{
		{Model: "openai:gpt-5", Passed: 1, Failed: 1, ExtraPolicyOnly: 1, MatchRate: 2.0 / 3.0, StrictMatchRate: 1.0 / 3.0},
	}

	jsonStr, err := formatJSONOutput(suite, results, summary, nil, comparison)
	require.NoError(t, err)

	// Parse and verify
	var output JSONOutput
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &output))

	// Check model comparison entry
	require.Len(t, output.ModelComparison, 1)
	assert.Equal(t, 1, output.ModelComparison[0].ExtraPolicyOnly)
	assert.InDelta(t, 1.0/3.0, output.ModelComparison[0].StrictMatchRate, 0.001)

	// Check individual test result
	require.Len(t, output.ResultsByModel, 1)
	found := false
	for _, r := range output.ResultsByModel[0].Results {
		if r.CaseID == "test-extra-policy" {
			assert.True(t, r.ExtraPolicyOnly, "ExtraPolicyOnly should be true for extra-policy test")
			found = true
		}
	}
	assert.True(t, found, "test-extra-policy result should be in output")

	// Check model summary includes ExtraPolicyOnly
	modelSummary := output.ResultsByModel[0].Summary
	assert.Equal(t, 1, modelSummary.ExtraPolicyOnly)

	// Check overall summary includes ExtraPolicyOnly
	assert.Equal(t, 1, output.OverallSummary.ExtraPolicyOnly)
}

// TestParseTestCases_WindowsPathEscaping verifies that Windows paths with
// backslashes are parsed correctly across the three YAML string styles.
//
// Convention: use double-quoted strings with \\ for backslashes. This is
// consistent with all other string values in test cases and policy files.
//
// YAML rules for backslashes:
//   - Double-quoted:       backslashes are escape chars — "C:\\Windows" → C:\Windows
//                          WARNING: "C:\tmp" silently becomes C:<TAB>mp (\t = tab)
//   - Unquoted (plain):    backslashes are literal — C:\Windows → C:\Windows
//   - Single-quoted:       backslashes are literal — 'C:\Windows' → C:\Windows
func TestParseTestCases_WindowsPathEscaping(t *testing.T) {
	expectedPath := "C:\\Windows\\System32\\drivers\\etc\\hosts"

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "unquoted path",
			yaml: `
- case_id: yaml-escape-unquoted
  title: "Unquoted Windows path"
  phase: request
  engine: ai
  request:
    tool_name: "filesystem__write_file"
    arguments:
      path: C:\Windows\System32\drivers\etc\hosts
      content: "test"
  expectations:
    decision: deny
    policies:
      - policy_name: "test-policy"
        decision: deny
`,
		},
		{
			name: "single-quoted path",
			yaml: `
- case_id: yaml-escape-single
  title: "Single-quoted Windows path"
  phase: request
  engine: ai
  request:
    tool_name: "filesystem__write_file"
    arguments:
      path: 'C:\Windows\System32\drivers\etc\hosts'
      content: "test"
  expectations:
    decision: deny
    policies:
      - policy_name: "test-policy"
        decision: deny
`,
		},
		{
			name: "double-quoted path with escaped backslashes (convention)",
			yaml: `
- case_id: yaml-escape-double
  title: "Double-quoted Windows path"
  phase: request
  engine: ai
  request:
    tool_name: "filesystem__write_file"
    arguments:
      path: "C:\\Windows\\System32\\drivers\\etc\\hosts"
      content: "test"
  expectations:
    decision: deny
    policies:
      - policy_name: "test-policy"
        decision: deny
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "test.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o644))

			runner := &Runner{
				suite: &Suite{
					Acceptance: AcceptanceConfig{MinMatchRate: 0.8},
				},
			}

			cases, err := runner.parseTestCases(path)
			require.NoError(t, err)
			require.Len(t, cases, 1)

			gotPath, ok := cases[0].Request.Arguments["path"].(string)
			require.True(t, ok, "path argument should be a string")
			assert.Equal(t, expectedPath, gotPath,
				"all YAML quoting styles should produce the same Windows path")
		})
	}
}

// TestParseTestCases_DoubleQuotedBackslashTrap demonstrates why double-quoted
// YAML strings are dangerous for Windows paths without proper escaping.
// Characters like \t (tab), \n (newline), \r (carriage return) are silently
// interpreted as escape sequences, mangling the path.
func TestParseTestCases_DoubleQuotedBackslashTrap(t *testing.T) {
	// "C:\tmp\new" in double quotes: \t → tab, \n → newline
	yaml := `
- case_id: yaml-escape-trap
  title: "Backslash trap"
  phase: request
  engine: ai
  request:
    tool_name: "filesystem__write_file"
    arguments:
      path: "C:\tmp\new"
      content: "test"
  expectations:
    decision: deny
    policies:
      - policy_name: "test-policy"
        decision: deny
`

	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	runner := &Runner{
		suite: &Suite{
			Acceptance: AcceptanceConfig{MinMatchRate: 0.8},
		},
	}

	cases, err := runner.parseTestCases(path)
	require.NoError(t, err)
	require.Len(t, cases, 1)

	gotPath := cases[0].Request.Arguments["path"].(string)
	// This is the WRONG value — \t became tab, \n became newline.
	// This test documents the trap, not a desired behavior.
	assert.NotEqual(t, `C:\tmp\new`, gotPath,
		"double-quoted \\t and \\n are silently interpreted as escape sequences")
	assert.Contains(t, gotPath, "\t", "\\t in double quotes becomes a tab character")
	assert.Contains(t, gotPath, "\n", "\\n in double quotes becomes a newline character")
}
