package cmd

import (
	"fmt"
	"os"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgDir      string
	logDir      string
	cfgFileName string
	cfg         *config.Config
	Logger      *config.SessionLogger
	version     string
	commit      string
	date        string

	// ResolvedLogDir is the resolved log directory, exported for use by gateway
	ResolvedLogDir string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:           "maybe-dont",
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "Maybe Don't AI adds guardrails for agentic AI",
	Long: `Maybe Don't AI adds guardrails for agentic AI. This binary contains an MCP
gateway server that validates tool calls against configurable policies, and a CLI
proxy that routes shell commands through the gateway for validation before execution.`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Usage()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute(VERSION string, COMMIT string, DATE string) {
	version = VERSION
	commit = COMMIT
	date = DATE
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
