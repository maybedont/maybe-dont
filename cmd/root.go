package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/maybedont/maybe-dont/internal/metrics"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	cfgDir           string
	logDir           string
	cfgFileName      string
	cfg              *config.Config
	Logger           *config.SessionLogger
	MetricsCollector *metrics.Collector
	version          string
	commit           string
	date             string
	metricsConfig    metrics.Config

	// ResolvedLogDir is the resolved log directory, exported for use by gateway
	ResolvedLogDir string
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

		// Resolve config directory from CLI flag or environment variable
		resolvedCfgDir := cfgDir
		if resolvedCfgDir == "" {
			resolvedCfgDir = os.Getenv("MAYBE_DONT_CONFIG_DIR")
		}
		// Apply fallback logic for config directory
		resolvedCfgDir, err = config.ResolveConfigDir(resolvedCfgDir)
		if err != nil {
			return fmt.Errorf("failed to resolve config directory: %w", err)
		}

		// Write default config files if they don't exist (first-run bootstrap)
		if _, err := config.WriteDefaultsIfMissing(resolvedCfgDir); err != nil {
			return fmt.Errorf("failed to write default config files: %w", err)
		}

		// Resolve log directory from CLI flag or environment variable
		// If not specified, derives from config directory (e.g., ./config → ./config/logs)
		ResolvedLogDir = logDir
		if ResolvedLogDir == "" {
			ResolvedLogDir = os.Getenv("MAYBE_DONT_LOG_DIR")
		}
		ResolvedLogDir = config.ResolveLogDir(ResolvedLogDir, resolvedCfgDir)

		// Resolve config file name from CLI flag or environment variable
		resolvedCfgFileName := cfgFileName
		if resolvedCfgFileName == "" {
			resolvedCfgFileName = os.Getenv("MAYBE_DONT_CONFIG_FILE_NAME")
		}

		cfg, err = config.LoadConfig(resolvedCfgDir, resolvedCfgFileName)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if err := config.ValidateConfig(cfg); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}

		Logger, err = config.GetLogger(cfg, ResolvedLogDir)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}
		Logger.Debug(context.Background(), "Logger created")

		// Initialize metrics collector if metrics are configured at build time
		if metricsConfig.Dataset != "" && metricsConfig.APIToken != "" {
			MetricsCollector, err = metrics.NewCollector(
				version,
				metricsConfig,
				Logger.GetZapLogger(),
			)
			if err != nil {
				Logger.Warn(context.Background(), "Failed to initialize metrics collector", zap.Error(err))
				// Don't fail startup if metrics initialization fails
			} else {
				// Set rule usage flags based on config (Enabled indicates if phase is active)
				MetricsCollector.SetRuleUsage(
					cfg.RequestValidation.AI.Enabled,
					cfg.RequestValidation.CEL.Enabled,
					cfg.ResponseValidation.AI.Enabled,
					cfg.ResponseValidation.CEL.Enabled,
				)
				// Set MCP server count
				MetricsCollector.SetMCPServerCount(len(cfg.DownstreamMCPServers))
			}
		} else {
			Logger.Info(context.Background(), "Metrics collection disabled: not configured at build time")
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Usage()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute(VERSION string, COMMIT string, DATE string, metricsCfg metrics.Config) {
	version = VERSION
	commit = COMMIT
	date = DATE
	metricsConfig = metricsCfg
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgDir, "config-dir", "",
		"Config directory (env: MAYBE_DONT_CONFIG_DIR, default: $XDG_CONFIG_HOME/maybe-dont or ~/.config/maybe-dont)")
	rootCmd.PersistentFlags().StringVar(&logDir, "log-dir", "",
		"Log directory (env: MAYBE_DONT_LOG_DIR, default: $XDG_STATE_HOME/maybe-dont or ~/.local/state/maybe-dont)")
	rootCmd.PersistentFlags().StringVar(&cfgFileName, "config-file-name", "", "Config file name (env: MAYBE_DONT_CONFIG_FILE_NAME, default: maybe-dont.yaml)")
}
