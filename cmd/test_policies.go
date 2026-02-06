package cmd

import (
	"fmt"
	"os"

	"github.com/maybedont/maybe-dont/internal/testsuite"
	"github.com/spf13/cobra"
)

// Exit codes as defined in the spec
const (
	ExitSuccess             = 0
	ExitTestsFailed         = 1
	ExitSchemaValidation    = 2
	ExitPolicyIntegrity     = 3
	ExitPathResolution      = 4
	ExitMoreTestsRemain     = 5
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
	force             bool
)

// testPoliciesCmd represents the policies subcommand under test
var testPoliciesCmd = &cobra.Command{
	Use:   "policies",
	Short: "Run policy tests against a test suite",
	Long: `Run policy tests to validate CEL and AI policies against a suite of test cases.

The test suite is defined in a directory containing:
  - suite.yaml: Suite configuration (required)
  - cases/: Directory containing test case YAML files (auto-discovered recursively)

Example usage:
  # Run with defaults from suite.yaml
  maybe-dont test policies --suite-dir ./suite

  # Run only CEL engine tests
  maybe-dont test policies --suite-dir ./suite --engine cel

  # Run AI tests with a specific model
  maybe-dont test policies --suite-dir ./suite --engine ai --model openai:gpt-4o-mini

  # Run full model matrix from suite.yaml
  maybe-dont test policies --suite-dir ./suite --matrix

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
	testPoliciesCmd.Flags().StringVar(&model, "model", "", "Override model for AI tests: provider:model (e.g., openai:gpt-4o-mini)")
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
	testPoliciesCmd.Flags().IntVar(&requestsPerMinute, "rpm", 0, "Shorthand for --requests-per-minute")

	// Incremental execution options
	testPoliciesCmd.Flags().IntVar(&maxTests, "max-tests", 0, "Maximum tests per model per invocation (exit code 5 if more remain)")
	testPoliciesCmd.Flags().BoolVar(&wait, "wait", false, "Run continuously until all tests complete (requires --state-file)")
	testPoliciesCmd.Flags().StringVar(&stateFile, "state-file", "", "Path to state file for incremental execution")
	testPoliciesCmd.Flags().BoolVar(&force, "force", false, "Ignore state file and re-run all tests")
}

func runTestPolicies(cmd *cobra.Command, args []string) error {
	// Validate flag combinations
	if wait && stateFile == "" {
		return fmt.Errorf("--wait requires --state-file to be specified")
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
		StateFile:         stateFile,
		Force:             force,
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
	switch e := err.(type) {
	case *testsuite.SchemaValidationError:
		fmt.Fprintf(os.Stderr, "Schema validation failed: %v\n", e)
		os.Exit(ExitSchemaValidation)
	case *testsuite.PolicyIntegrityError:
		fmt.Fprintf(os.Stderr, "Policy integrity check failed: %v\n", e)
		os.Exit(ExitPolicyIntegrity)
	case *testsuite.PathResolutionError:
		fmt.Fprintf(os.Stderr, "Path resolution failed: %v\n", e)
		os.Exit(ExitPathResolution)
	}

	// Generic error - return to cobra for standard error handling
	return err
}
