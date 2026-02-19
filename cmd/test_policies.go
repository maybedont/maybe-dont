package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maybedont/maybe-dont/internal/testsuite"
	"github.com/spf13/cobra"
)

// Exit codes as defined in the spec
const (
	ExitSuccess          = 0
	ExitTestsFailed      = 1
	ExitSchemaValidation = 2
	ExitPolicyIntegrity  = 3
	ExitPathResolution   = 4
	ExitMoreTestsRemain  = 5
)

// CLI flags for test policies command
var (
	suiteDir          string
	engine            string
	model             string
	runMatrix         bool
	outputFormat      string
	outputFile        string
	quiet             bool
	tags              string
	excludeTags       string
	casePattern       string
	validateOnly      bool
	includeDisabled   bool
	timeout           int
	requestsPerMinute int
	maxTests          int
	wait              bool
	stateFile         string
	incremental       bool
	full              bool
	retryFailed       bool
	summaryOnly       bool
	historyDepth      int
)

// defaultStateFilePath returns the default path for the policy test state file.
// Uses XDG_STATE_HOME/maybe-dont/policy-test-state.json or falls back to
// ~/.local/state/maybe-dont/policy-test-state.json
func defaultStateFilePath() (string, error) {
	var stateDir string

	// Try XDG_STATE_HOME first
	if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
		stateDir = filepath.Join(xdgState, "maybe-dont")
	} else {
		// Fall back to XDG default
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		stateDir = filepath.Join(homeDir, ".local", "state", "maybe-dont")
	}

	return filepath.Join(stateDir, "policy-test-state.json"), nil
}

// testPoliciesCmd represents the policies subcommand under test
var testPoliciesCmd = &cobra.Command{
	Use:   "policies",
	Short: "Run policy tests against a test suite",
	Long: `Run policy tests to validate CEL and AI policies against a suite of test cases.

The test suite is defined in a directory containing:
  - suite.yaml: Suite configuration (required)
  - cases/: Directory containing test case YAML files (auto-discovered recursively)

Example usage:
  # Run with defaults from suite.yaml (no state persistence)
  maybe-dont test policies --suite-dir ./suite

  # Run only CEL engine tests
  maybe-dont test policies --suite-dir ./suite --engine cel

  # Run AI tests with a specific model
  maybe-dont test policies --suite-dir ./suite --engine ai --model openai:gpt-5-mini

  # Run full model matrix from suite.yaml
  maybe-dont test policies --suite-dir ./suite --matrix

  # Incremental mode: skip unchanged tests, persist results to state file
  maybe-dont test policies --suite-dir ./suite --incremental

  # Full mode: run all tests, persist results to state file
  maybe-dont test policies --suite-dir ./suite --full

  # Use custom state file location
  maybe-dont test policies --suite-dir ./suite --incremental --state-file ./my-state.json

  # Run incrementally until complete, respecting rate limits
  maybe-dont test policies --suite-dir ./suite --incremental --wait

  # Re-run only failed tests (to check for transient issues)
  maybe-dont test policies --suite-dir ./suite --incremental --retry-failed

  # Stream progress to stdout AND save JSON results to file
  maybe-dont test policies --suite-dir ./suite --output results.json

  # Stream progress AND save JUnit XML for CI
  maybe-dont test policies --suite-dir ./suite --format junit --output results.xml

  # Quiet mode: only write to file, no stdout
  maybe-dont test policies --suite-dir ./suite --quiet --output results.json

  # Validate suite configuration without running tests
  maybe-dont test policies --suite-dir ./suite --validate-only`,
	RunE: runTestPolicies,
}

func init() {
	testCmd.AddCommand(testPoliciesCmd)

	// Required flags
	testPoliciesCmd.Flags().StringVar(&suiteDir, "suite-dir", "", "Directory containing suite.yaml and cases/ (required)")
	_ = testPoliciesCmd.MarkFlagRequired("suite-dir")

	// Engine and model selection
	testPoliciesCmd.Flags().StringVar(&engine, "engine", "", "Engine to test: cel, ai, or all (default: from suite.yaml)")
	testPoliciesCmd.Flags().StringVar(&model, "model", "", "Override model(s) for AI tests: provider:model (comma-separated, e.g., openai:gpt-5-mini,openai:gpt-5)")
	testPoliciesCmd.Flags().BoolVar(&runMatrix, "matrix", false, "Run full model matrix from suite.yaml")

	// Output options
	testPoliciesCmd.Flags().StringVar(&outputFormat, "format", "json", "Output format for --output file: json or junit (default: json)")
	testPoliciesCmd.Flags().StringVar(&outputFile, "output", "", "Write structured output (JSON/JUnit) to file in addition to stdout")
	testPoliciesCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress stdout output (use with --output for file-only output)")

	// Filtering options
	testPoliciesCmd.Flags().StringVar(&tags, "tags", "", "Only run cases with these tags (comma-separated)")
	testPoliciesCmd.Flags().StringVar(&excludeTags, "exclude-tags", "", "Skip cases with these tags (comma-separated)")
	testPoliciesCmd.Flags().StringVar(&casePattern, "case-pattern", "", "Glob pattern for case IDs (default: *)")

	// Execution options
	testPoliciesCmd.Flags().BoolVar(&validateOnly, "validate-only", false, "Run suite validation without executing tests")
	testPoliciesCmd.Flags().BoolVar(&includeDisabled, "include-disabled", false, "Include policies with enabled: false in test execution")
	testPoliciesCmd.Flags().IntVar(&timeout, "timeout", 0, "Timeout per test case in milliseconds (default: from suite.yaml)")

	// Rate limiting options
	testPoliciesCmd.Flags().IntVar(&requestsPerMinute, "requests-per-minute", 0, "Override requests per minute for all providers")

	// Incremental execution options
	testPoliciesCmd.Flags().IntVar(&maxTests, "max-tests", 0, "Maximum tests per model per invocation (exit code 5 if more remain)")
	testPoliciesCmd.Flags().BoolVar(&wait, "wait", false, "Run continuously until all tests complete (requires --incremental or --full)")
	testPoliciesCmd.Flags().BoolVar(&incremental, "incremental", false, "Skip unchanged tests, persist results to state file")
	testPoliciesCmd.Flags().BoolVar(&full, "full", false, "Run all tests, persist results to state file")
	testPoliciesCmd.Flags().StringVar(&stateFile, "state-file", "", "Override state file location (use with --incremental or --full)")
	testPoliciesCmd.Flags().BoolVar(&retryFailed, "retry-failed", false, "Re-run failed/errored tests even if cached (for checking transient issues)")
	testPoliciesCmd.Flags().BoolVar(&summaryOnly, "summary-only", false, "Show summary from cached state without running tests")
	testPoliciesCmd.Flags().IntVar(&historyDepth, "history-depth", 0, "Override history depth for pass rate tracking (default: from suite.yaml or 20)")
}

func runTestPolicies(cmd *cobra.Command, args []string) error {
	// Validate flag combinations
	if incremental && full {
		return fmt.Errorf("cannot use both --incremental and --full")
	}

	if model != "" && runMatrix {
		return fmt.Errorf("cannot use both --model and --matrix; --model selects specific models, --matrix runs all enabled models")
	}

	useState := incremental || full || summaryOnly

	if stateFile != "" && !useState {
		return fmt.Errorf("--state-file requires --incremental or --full")
	}

	if wait && !useState {
		return fmt.Errorf("--wait requires --incremental or --full")
	}

	if summaryOnly && validateOnly {
		return fmt.Errorf("cannot use both --summary-only and --validate-only")
	}

	// Resolve state file path
	resolvedStateFile := stateFile
	if useState && resolvedStateFile == "" {
		defaultPath, err := defaultStateFilePath()
		if err != nil {
			return fmt.Errorf("failed to determine default state file path: %w", err)
		}
		resolvedStateFile = defaultPath
	}

	// Create state file directory if it doesn't exist
	if resolvedStateFile != "" {
		stateDir := filepath.Dir(resolvedStateFile)
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return fmt.Errorf("failed to create state directory %s: %w", stateDir, err)
		}
	}

	// Build runner options from CLI flags
	opts := testsuite.RunnerOptions{
		SuiteDir:          suiteDir,
		Engine:            engine,
		Model:             model,
		RunMatrix:         runMatrix,
		OutputFormat:      outputFormat,
		OutputFile:        outputFile,
		Quiet:             quiet,
		Tags:              tags,
		ExcludeTags:       excludeTags,
		CasePattern:       casePattern,
		ValidateOnly:      validateOnly,
		IncludeDisabled:   includeDisabled,
		TimeoutMs:         timeout,
		RequestsPerMinute: requestsPerMinute,
		MaxTests:          maxTests,
		Wait:              wait,
		StateFile:         resolvedStateFile,
		Force:             full,        // --full means "force" re-run all tests
		RetryFailed:       retryFailed, // --retry-failed re-runs failed/errored tests
		SummaryOnly:       summaryOnly,
		HistoryDepth:      historyDepth,
	}

	// Create and run the test suite runner
	runner, err := testsuite.NewRunner(opts)
	if err != nil {
		return handleRunnerError(err)
	}

	result, err := runner.Run(cmd.Context())
	if err != nil {
		return handleRunnerError(err)
	}

	// Exit with appropriate code based on result
	if result.MoreTestsRemain {
		os.Exit(ExitMoreTestsRemain)
	}
	if !result.ThresholdsMet {
		os.Exit(ExitTestsFailed)
	}

	return nil
}

// handleRunnerError maps testsuite errors to appropriate exit codes
func handleRunnerError(err error) error {
	if err == nil {
		return nil
	}

	// Check for specific error types and exit with appropriate codes
	var schemaErr *testsuite.SchemaValidationError
	if errors.As(err, &schemaErr) {
		fmt.Fprintf(os.Stderr, "Schema validation failed: %v\n", schemaErr)
		os.Exit(ExitSchemaValidation)
	}

	var policyErr *testsuite.PolicyIntegrityError
	if errors.As(err, &policyErr) {
		fmt.Fprintf(os.Stderr, "Policy integrity check failed: %v\n", policyErr)
		os.Exit(ExitPolicyIntegrity)
	}

	var pathErr *testsuite.PathResolutionError
	if errors.As(err, &pathErr) {
		fmt.Fprintf(os.Stderr, "Path resolution failed: %v\n", pathErr)
		os.Exit(ExitPathResolution)
	}

	// Generic error - return to cobra for standard error handling
	return err
}
