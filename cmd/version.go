package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display version information",
	Long:  `Display the version, commit hash, and build time of the MCP security gateway.`,
	// Skip parent's PersistentPreRunE - this command doesn't need config loaded
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("MCP Security Gateway\n")
		fmt.Printf("Version:    %s\n", version)
		fmt.Printf("Commit:     %s\n", commit)
		fmt.Printf("Date:       %s\n", date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
