package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test CEL policies against sample requests",
	Long: `Test CEL policies against sample requests to validate their behavior.
This command allows you to test policies without running the proxy server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := zap.L().Named("test")
		logger.Info("Testing CEL policies")

		requestFile, _ := cmd.Flags().GetString("request")
		interactive, _ := cmd.Flags().GetBool("interactive")
		authContext, _ := cmd.Flags().GetString("auth-context")

		if requestFile != "" {
			// TODO: Load and test against request file
			logger.Info("Testing against request file", zap.String("file", requestFile))
		} else if interactive {
			// TODO: Start interactive testing mode
			logger.Info("Starting interactive testing mode")
		} else {
			return fmt.Errorf("either --request or --interactive must be specified")
		}

		if authContext != "" {
			// TODO: Parse and use auth context
			logger.Info("Using auth context", zap.String("context", authContext))
		}

		// TODO: Implement policy testing
		// 1. Load and compile CEL expressions
		// 2. Validate expression syntax and types
		// 3. Execute policies against test requests
		// 4. Show which rules match and their outcomes
		// 5. Measure policy evaluation performance

		return nil
	},
}

func init() {
	rootCmd.AddCommand(testCmd)

	// Test command specific flags
	testCmd.Flags().String("request", "", "Path to request file for testing")
	testCmd.Flags().Bool("interactive", false, "Start interactive testing mode")
	testCmd.Flags().String("auth-context", "", "JSON string containing auth context for testing")
	testCmd.MarkFlagsMutuallyExclusive("request", "interactive")
}
