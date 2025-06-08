package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display version and build information",
	Long:  `Display the version, commit hash, and build time of the MCP security proxy.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("MCP Security Proxy\n")
		fmt.Printf("Version:    %s\n", version)
		fmt.Printf("Commit:     %s\n", commit)
		fmt.Printf("Build Time: %s\n", buildTime)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
