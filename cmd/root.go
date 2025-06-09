package cmd

import (
	"fmt"
	"os"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	cfgFile  string
	cfg      *config.Config
	Logger   *zap.Logger
	aiRules  []byte
	celRules []byte
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:           "maybe-dont",
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "Maybe Don't, an MCP Security Proxy - Enterprise-grade security controls for MCP communications",
	Long: `MCP Security Proxy is a Go-based middleware service that provides enterprise-grade 
security controls for Model Context Protocol (MCP) communications. It acts as a transparent 
proxy between MCP clients and servers, enforcing security policies, validating requests, 
and providing comprehensive audit logging.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.LoadConfig(cfgFile, aiRules, celRules)
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
		Logger.Debug("Logger created", zap.Any("config", cfg))

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Usage()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute(VERSION string, COMMIT string, AIRules []byte, CELRules []byte) {
	aiRules = AIRules
	celRules = CELRules
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./config.yaml)")
	rootCmd.PersistentFlags().String("log-level", "debug", "log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().String("log-format", "json", "log format (json or text)")
}
