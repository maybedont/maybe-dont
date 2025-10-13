package cmd

import (
	"fmt"
	"os"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	cfgPath             string
	cfg                 *config.Config
	Logger              *zap.Logger
	aiRules             []byte
	celRules            []byte
	aiResponseRules     []byte
	celResponseRules    []byte
	version             string
	commit              string
	date                string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:           "maybe-dont",
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "Maybe Don't, an MCP Security Gateway - Enterprise-grade security controls for MCP communications",
	Long: `MCP Security Gateway is a Go-based middleware service that provides enterprise-grade 
security controls for Model Context Protocol (MCP) communications. It acts as a transparent
gateway between MCP clients and servers, enforcing security policies, validating requests, 
and providing comprehensive audit logging.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.LoadConfig(cfgPath, aiRules, celRules, aiResponseRules, celResponseRules)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if err := config.ValidateConfig(cfg); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}

		Logger, err = config.GetLogger(cfg)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}
		Logger.Debug("Logger created")

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Usage()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute(VERSION string, COMMIT string, DATE string, AIRules []byte, CELRules []byte, AIResponseRules []byte, CELResponseRules []byte) {
	aiRules = AIRules
	celRules = CELRules
	aiResponseRules = AIResponseRules
	celResponseRules = CELResponseRules
	version = VERSION
	commit = COMMIT
	date = DATE
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config-path", "", "Override the config directory (default is ./ or $HOME/.maybe-dont)")
}
