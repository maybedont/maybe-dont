package cmd

import (
	"fmt"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/spf13/cobra"
)

var defaultsCmd = &cobra.Command{
	Use:   "defaults",
	Short: "Manage default configuration files",
	Long:  `Commands for working with embedded default configuration files.`,
	// Skip gateway's PersistentPreRunE - these commands don't need config loaded
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

var exportOutputDir string

var defaultsExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Extract embedded default configuration files",
	Long: `Extract all embedded default configuration files to a directory.

This command writes all default config and rule files to the specified
output directory. Unlike the automatic first-run behavior, this command
WILL overwrite existing files.

Use this to:
- Get fresh defaults after upgrading to compare with your customized files
- Recover defaults if you want to start over
- Inspect what the current version ships with

Example upgrade workflow:
  maybe-dont gateway defaults export --output-dir ./new-defaults
  diff ./new-defaults/cel_request_rules.yaml ~/.config/maybe-dont/cel_request_rules.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if exportOutputDir == "" {
			return fmt.Errorf("--output-dir is required")
		}

		fmt.Printf("Extracting default configuration files to %s\n", exportOutputDir)
		if err := config.DumpDefaults(exportOutputDir); err != nil {
			return fmt.Errorf("failed to export defaults: %w", err)
		}

		fmt.Println("Done.")
		return nil
	},
}

func init() {
	defaultsExportCmd.Flags().StringVarP(&exportOutputDir, "output-dir", "o", "",
		"Directory to write default files to (required)")
	_ = defaultsExportCmd.MarkFlagRequired("output-dir")

	defaultsCmd.AddCommand(defaultsExportCmd)
	gatewayCmd.AddCommand(defaultsCmd)
}
