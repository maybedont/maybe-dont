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
// the engine/model metadata suffix in the formatted output.
func TestFormatSingleTestResult_EngineInfo(t *testing.T) {
	tests := []struct {
		name         string
		result       TestResult
		wantContains []string
	}{
		{
			name: "CEL test shows [cel] suffix",
			result: TestResult{
				CaseID: "test-1",
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
			wantContains: []string{"test-1", "[cel]"},
		},
		{
			name: "AI test shows [ai:model] suffix",
			result: TestResult{
				CaseID: "test-2",
				Engine: "ai",
				Model:  "anthropic:claude-haiku",
				Status: "passed",
				Expected: ExpectedResult{
					Decision: "deny",
				},
				Actual: ActualResult{
					Decision:   "deny",
					Confidence: 1.0,
				},
			},
			wantContains: []string{"test-2", "[ai:anthropic:claude-haiku]"},
		},
		{
			name: "failed test with engine info",
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
				},
				Failures: []string{"expected deny, got allow"},
			},
			wantContains: []string{"test-3", "[ai:openai:gpt-4o-mini]", "✗"},
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
					Message: "Skipped due to valid cached result",
				},
			},
			wantContains: []string{"test-4", "[ai:openai:gpt-4]", "○", "skipped"},
		},
		{
			name: "errored test with engine info",
			result: TestResult{
				CaseID: "test-5",
				Engine: "ai",
				Model:  "anthropic:claude",
				Status: "errored",
				Error: &TestError{
					Type:    "timeout",
					Message: "Test case timed out",
				},
			},
			wantContains: []string{"test-5", "[ai:anthropic:claude]", "⚠", "timeout"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := formatSingleTestResult(tt.result)
			for _, want := range tt.wantContains {
				assert.Contains(t, output, want, "output should contain %q", want)
			}
		})
	}
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
