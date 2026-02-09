package testsuite

import "strings"

// Default rate limiting values
const (
	DefaultRequestsPerMinute      = 30   // Default requests per minute for unlisted providers
	DefaultDelayBetweenRequestsMs = 100  // Minimum delay between consecutive requests
	DefaultRateLimitBufferMs      = 5000 // Extra buffer when hitting rate limit window
)

// Auto-scaling max_tokens constants.
// Anthropic counts max_tokens (reserved capacity) against rate limits, not actual output.
// Starting small and scaling up on truncation minimizes wasted rate limit budget, but each
// truncation+retry costs more budget than a single larger request would. 128 balances these:
// most responses fit (no retry cost), while not over-reserving when they're short.
// OpenAI and compatible providers only count actual output tokens, so they start higher
// to avoid unnecessary retries.
//
// REVISIT: If Anthropic changes to count only actual output tokens (like OpenAI does),
// AnthropicInitialMaxTokens can be raised to DefaultInitialMaxTokens and auto-scaling
// can be disabled for all providers.
const (
	AnthropicInitialMaxTokens = 128  // Balances rate limit budget vs retry cost for Anthropic
	DefaultInitialMaxTokens   = 1024 // Generous default for providers that count actual output
	MaxMaxTokens              = 1024 // Cap to prevent runaway in case of unexpected responses
	MaxTokensScaleFactor      = 2.0  // Double on truncation
	MaxScalingAttempts        = 4    // Max retries: 128 -> 256 -> 512 -> 1024
)

// RunnerOptions configures the test suite runner from CLI flags.
type RunnerOptions struct {
	// SuiteDir is the directory containing suite.yaml and cases/
	SuiteDir string

	// Engine selects which engine to test: "cel", "ai", or "all"
	// Empty string means use the suite.yaml configuration
	Engine string

	// Model overrides the model for AI tests (format: "provider:model")
	Model string

	// RunMatrix runs the full model matrix from suite.yaml
	RunMatrix bool

	// OutputFormat selects the output format: "text", "junit", or "json"
	OutputFormat string

	// OutputFile writes structured output (JSON/JUnit) to a file
	OutputFile string

	// Quiet suppresses stdout output (streaming progress)
	Quiet bool

	// Tags filters test cases to only those with these tags (comma-separated)
	Tags string

	// ExcludeTags skips test cases with these tags (comma-separated)
	ExcludeTags string

	// CasePattern is a glob pattern for filtering case IDs
	CasePattern string

	// ValidateOnly runs validation without executing tests
	ValidateOnly bool

	// IncludeDisabled includes policies with enabled: false
	IncludeDisabled bool

	// TimeoutMs overrides the per-test-case timeout in milliseconds
	TimeoutMs int

	// RequestsPerMinute overrides all provider rate limits (CLI flag: --rpm)
	RequestsPerMinute int

	// MaxTests limits tests per model per invocation (exit code 5 if more remain)
	MaxTests int

	// Wait runs continuously until all tests complete, respecting rate limits
	Wait bool

	// StateFile path for incremental execution state persistence
	StateFile string

	// Force ignores state file and re-runs all tests
	Force bool

	// RetryFailed re-runs failed/errored tests even if cached (for checking transient issues)
	RetryFailed bool

	// SummaryOnly shows summary from cached state without running tests
	SummaryOnly bool
}

// RunResult contains the overall result of a test suite run.
type RunResult struct {
	// ThresholdsMet indicates whether acceptance thresholds were met
	ThresholdsMet bool

	// TotalCases is the total number of test cases
	TotalCases int

	// Passed is the count of passing test cases
	Passed int

	// Failed is the count of failing test cases
	Failed int

	// Errored is the count of errored test cases (timeouts, API errors)
	Errored int

	// Skipped is the count of skipped test cases
	Skipped int

	// CachedCount is the number of results sourced from cache (counted by original status above)
	CachedCount int

	// RateLimited is the count of tests skipped due to rate limiting
	RateLimited int

	// Remaining is the count of tests not yet run (for incremental execution)
	Remaining int

	// MatchRate is the percentage of tests that passed (0.0-1.0)
	MatchRate float64

	// MoreTestsRemain indicates --max-tests was used and more tests remain
	MoreTestsRemain bool
}

// effectiveStatus returns the policy-quality status for a test result.
// For cached results, this returns the original status from the prior run
// (e.g., "passed" instead of "skipped") so that summary statistics reflect
// cumulative policy quality rather than just what was evaluated in this run.
func effectiveStatus(tr TestResult) string {
	if tr.Status == "skipped" && tr.Error != nil && tr.Error.Type == "cached" {
		return strings.TrimPrefix(tr.Error.Message, "cached ")
	}
	return tr.Status
}

// ModelComparisonEntry holds per-model summary stats for the cross-model comparison table.
type ModelComparisonEntry struct {
	// Model is the model key (e.g., "openai:gpt-5", "cel")
	Model string

	// Passed is the number of tests that passed for this model
	Passed int

	// Failed is the number of tests that failed for this model
	Failed int

	// Errored is the number of tests that errored for this model
	Errored int

	// MatchRate is the pass rate (0.0-1.0) for this model
	MatchRate float64

	// AvgMs is the average test duration in milliseconds
	AvgMs int64

	// TotalMs is the total test duration in milliseconds
	TotalMs int64

	// FromCache is true if data is entirely from cached state (not tested in current run)
	FromCache bool
}

// Suite represents a parsed suite.yaml configuration.
type Suite struct {
	Version     string                    `yaml:"version"`
	BundleID    string                    `yaml:"bundle_id"`
	Description string                    `yaml:"description"`
	Providers   map[string]ProviderConfig `yaml:"providers,omitempty"`
	Policies    PoliciesConfig            `yaml:"policies"`
	Acceptance  AcceptanceConfig          `yaml:"acceptance"`
	Execution   ExecutionConfig           `yaml:"execution"`
	Engines     EnginesConfig             `yaml:"engines"`
	Filters     FiltersConfig             `yaml:"filters"`
}

// ProviderConfig defines provider-level settings shared across all models for that provider.
type ProviderConfig struct {
	APIKey   string `yaml:"api_key"`
	Endpoint string `yaml:"endpoint,omitempty"`
}

// ResolveAPIKey returns the API key for a model using deterministic lookup:
// 1. Per-model api_key (override)
// 2. Provider-level api_key from providers section
// Returns empty string if neither is configured.
func (s *Suite) ResolveAPIKey(m ModelConfig) string {
	// Per-model override takes precedence
	if m.APIKey != "" {
		return m.APIKey
	}
	// Fall back to provider-level config
	if s.Providers != nil {
		if pc, ok := s.Providers[m.Provider]; ok {
			return pc.APIKey
		}
	}
	return ""
}

// ResolveEndpoint returns the endpoint for a model using deterministic lookup:
// 1. Per-model endpoint (override)
// 2. Provider-level endpoint from providers section
// Returns empty string if neither is configured (caller should use provider defaults).
func (s *Suite) ResolveEndpoint(m ModelConfig) string {
	// Per-model override takes precedence
	if m.Endpoint != "" {
		return m.Endpoint
	}
	// Fall back to provider-level config
	if s.Providers != nil {
		if pc, ok := s.Providers[m.Provider]; ok {
			return pc.Endpoint
		}
	}
	return ""
}

// PoliciesConfig specifies the paths to policy files or directories.
type PoliciesConfig struct {
	CELRequestRules  string `yaml:"cel_request_rules"`
	AIRequestRules   string `yaml:"ai_request_rules"`
	CELResponseRules string `yaml:"cel_response_rules"`
	AIResponseRules  string `yaml:"ai_response_rules"`
}

// AcceptanceConfig defines pass/fail thresholds.
type AcceptanceConfig struct {
	MinMatchRate     float64 `yaml:"min_match_rate"`
	StrictPolicyMatch *bool  `yaml:"strict_policy_match,omitempty"` // Defaults to true if not specified
}

// IsStrictPolicyMatch returns true if unexpected triggering policies should cause test failure.
// Defaults to true when not explicitly set, enforcing focused test cases that only trigger
// the expected policies.
func (a AcceptanceConfig) IsStrictPolicyMatch() bool {
	if a.StrictPolicyMatch == nil {
		return true
	}
	return *a.StrictPolicyMatch
}

// ExecutionConfig defines test execution parameters.
type ExecutionConfig struct {
	TimeoutMs          int `yaml:"timeout_ms"`
	MaxTestDurationMs  int `yaml:"max_test_duration_ms"`
	Retries            int `yaml:"retries"`
	RetryDelayMs       int `yaml:"retry_delay_ms"`

	// Rate limiting configuration
	RateLimits              map[string]ProviderRateLimit `yaml:"rate_limits,omitempty"`
	DelayBetweenRequestsMs  int                          `yaml:"delay_between_requests_ms,omitempty"`
	RateLimitBufferMs       int                          `yaml:"rate_limit_buffer_ms,omitempty"`
}

// ProviderRateLimit defines rate limiting for a specific provider.
type ProviderRateLimit struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
}

// EnginesConfig configures which engines to test.
type EnginesConfig struct {
	CEL CELEngineConfig `yaml:"cel"`
	AI  AIEngineConfig  `yaml:"ai"`
}

// CELEngineConfig configures the CEL engine for testing.
type CELEngineConfig struct {
	Enabled bool `yaml:"enabled"`
}

// AIEngineConfig configures the AI engine for testing.
type AIEngineConfig struct {
	Enabled     bool          `yaml:"enabled"`
	ModelMatrix []ModelConfig `yaml:"model_matrix"`
}

// ModelConfig represents a single model in the test matrix.
type ModelConfig struct {
	Provider    string         `yaml:"provider"`
	Endpoint    string         `yaml:"endpoint,omitempty"`
	Model       string         `yaml:"model"`
	APIKey      string         `yaml:"api_key,omitempty"`
	Parameters  map[string]any `yaml:"parameters,omitempty"`
	QueryParams map[string]any `yaml:"query_params,omitempty"`
	Headers     map[string]any `yaml:"headers,omitempty"`
	Enabled     *bool          `yaml:"enabled,omitempty"` // Defaults to true if not specified
}

// IsEnabled returns true if this model should be included in test runs.
// Models are enabled by default if the field is not specified.
func (m ModelConfig) IsEnabled() bool {
	if m.Enabled == nil {
		return true
	}
	return *m.Enabled
}

// FiltersConfig defines default test case filters.
type FiltersConfig struct {
	Tags        []string `yaml:"tags"`
	ExcludeTags []string `yaml:"exclude_tags"`
	CasePattern string   `yaml:"case_pattern"`
}

// TestCase represents a single test case from the cases/ directory.
type TestCase struct {
	CaseID       string              `yaml:"case_id"`
	Title        string              `yaml:"title"`
	Tags         []string            `yaml:"tags,omitempty"`
	Notes        []string            `yaml:"notes,omitempty"`
	Phase        string              `yaml:"phase,omitempty"`   // request, response, or both (default: request)
	Engine       string              `yaml:"engine,omitempty"`  // cel, ai, or both (default: both)
	Request      RequestConfig       `yaml:"request"`
	Response     *ResponseConfig     `yaml:"response,omitempty"`
	Expectations ExpectationsConfig  `yaml:"expectations"`
}

// RequestConfig defines the request being validated.
type RequestConfig struct {
	ToolName   string         `yaml:"tool_name"`
	Arguments  map[string]any `yaml:"arguments"`
	// Future: CLICmd, WorkingDir for CLI command testing
}

// ResponseConfig defines the response for response validation tests.
type ResponseConfig struct {
	Content []ContentItem `yaml:"content"`
	IsError bool          `yaml:"is_error"`
}

// ContentItem represents a content item in a response.
type ContentItem struct {
	Type string `yaml:"type"` // text, image, resource
	Text string `yaml:"text,omitempty"`
}

// ExpectationsConfig defines the expected outcomes.
type ExpectationsConfig struct {
	Decision        string             `yaml:"decision"` // allow, deny, or redact
	Policies        []PolicyExpectation `yaml:"policies,omitempty"`
	RedactedContent []ContentItem      `yaml:"redacted_content,omitempty"`
}

// PolicyExpectation defines the expected decision for a specific policy.
type PolicyExpectation struct {
	PolicyName string `yaml:"policy_name"`
	Decision   string `yaml:"decision"`
}

// ProgressCallback is called after each test completes during execution.
// This enables streaming output for text format.
type ProgressCallback func(result TestResult)
