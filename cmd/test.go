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
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}
