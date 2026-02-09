package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/maybedont/maybe-dont/internal/metrics"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// gatewayCmd is the parent command for gateway operations.
var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Gateway server commands",
	Long: `Commands for running and managing the gateway server.

The gateway acts as a transparent proxy between MCP clients and downstream
MCP servers, validating requests against configurable policies and providing
comprehensive audit logging.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error

		// Resolve config directory from CLI flag or environment variable
		resolvedCfgDir := cfgDir
		if resolvedCfgDir == "" {
			resolvedCfgDir = os.Getenv("MAYBE_DONT_CONFIG_DIR")
		}
		resolvedCfgDir, err = config.ResolveConfigDir(resolvedCfgDir)
		if err != nil {
			return fmt.Errorf("failed to resolve config directory: %w", err)
		}

		// Resolve config file name from CLI flag or environment variable
		resolvedCfgFileName := cfgFileName
		if resolvedCfgFileName == "" {
			resolvedCfgFileName = os.Getenv("MAYBE_DONT_CONFIG_FILE_NAME")
		}
		if resolvedCfgFileName == "" {
			resolvedCfgFileName = "maybe-dont.yaml"
		}

		// Write default config files if they don't exist (first-run bootstrap)
		createdFiles, err := config.WriteDefaultsIfMissing(resolvedCfgDir)
		if err != nil {
			return fmt.Errorf("failed to write default config files: %w", err)
		}
		if len(createdFiles) > 0 {
			fmt.Printf("Configuration initialized at %s\n", resolvedCfgDir)
		} else {
			fmt.Printf("Using configuration %s\n", filepath.Join(resolvedCfgDir, resolvedCfgFileName))
		}

		// Resolve log directory from CLI flag or environment variable
		ResolvedLogDir = logDir
		if ResolvedLogDir == "" {
			ResolvedLogDir = os.Getenv("MAYBE_DONT_LOG_DIR")
		}
		ResolvedLogDir, err = config.ResolveLogDir(ResolvedLogDir)
		if err != nil {
			return fmt.Errorf("failed to resolve log directory: %w", err)
		}

		cfg, err = config.LoadConfig(resolvedCfgDir, resolvedCfgFileName)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Print log directory if file-based logging is configured
		if cfg.Logger.Path != "" && cfg.Logger.Path != "stdout" && cfg.Logger.Path != "stderr" {
			fmt.Printf("Logging to %s\n", ResolvedLogDir)
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
			} else {
				MetricsCollector.SetRuleUsage(
					cfg.RequestValidation.AI.Enabled,
					cfg.RequestValidation.CEL.Enabled,
					cfg.ResponseValidation.AI.Enabled,
					cfg.ResponseValidation.CEL.Enabled,
				)
				MetricsCollector.SetMCPServerCount(len(cfg.DownstreamMCPServers))
			}
		} else {
			Logger.Info(context.Background(), "Metrics collection disabled: not configured at build time")
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

func init() {
	// Gateway-specific persistent flags
	gatewayCmd.PersistentFlags().StringVar(&cfgDir, "config-dir", "",
		"Config directory (env: MAYBE_DONT_CONFIG_DIR, default: $XDG_CONFIG_HOME/maybe-dont or ~/.config/maybe-dont)")
	gatewayCmd.PersistentFlags().StringVar(&logDir, "log-dir", "",
		"Log directory (env: MAYBE_DONT_LOG_DIR, default: $XDG_STATE_HOME/maybe-dont or ~/.local/state/maybe-dont)")
	gatewayCmd.PersistentFlags().StringVar(&cfgFileName, "config-file-name", "", "Config file name (env: MAYBE_DONT_CONFIG_FILE_NAME, default: maybe-dont.yaml)")

	rootCmd.AddCommand(gatewayCmd)
}
