package cmd

import (
	"fmt"
	"os"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management commands",
	Long:  `Commands for managing and inspecting Maybe Don't AI configuration.`,
}

var configInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show resolved configuration paths",
	Long:  `Display the resolved configuration and log directory paths based on current settings.`,
	// Skip parent's PersistentPreRunE - we only need to resolve paths, not load full config
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve config directory
		resolvedCfgDir := cfgDir
		if resolvedCfgDir == "" {
			resolvedCfgDir = os.Getenv("MAYBE_DONT_CONFIG_DIR")
		}
		resolvedCfgDir, err := config.ResolveConfigDir(resolvedCfgDir)
		if err != nil {
			return fmt.Errorf("failed to resolve config directory: %w", err)
		}

		// Resolve log directory
		resolvedLogDir := logDir
		if resolvedLogDir == "" {
			resolvedLogDir = os.Getenv("MAYBE_DONT_LOG_DIR")
		}
		resolvedLogDir, err = config.ResolveLogDir(resolvedLogDir)
		if err != nil {
			return fmt.Errorf("failed to resolve log directory: %w", err)
		}

		fmt.Printf("Config directory: %s\n", resolvedCfgDir)
		fmt.Printf("Log directory:    %s\n", resolvedLogDir)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInfoCmd)
}
