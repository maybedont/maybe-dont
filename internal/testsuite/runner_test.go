package testsuite

import (
	"os"
	"path/filepath"
	"testing"

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
