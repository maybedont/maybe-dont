package cmd

import (
	"github.com/spf13/cobra"
)

// testCmd represents the test command
var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run tests for policies and configurations",
	Long: `The test command provides subcommands for validating policies and configurations.

Available subcommands:
  policies    Run policy tests against a test suite`,
	// Override PersistentPreRunE to skip gateway config loading.
	// Test commands get their configuration from suite.yaml, not the gateway config.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}
