package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display version information",
	Long:  `Display the version, commit hash, and build time of Maybe Don't AI.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Maybe Don't AI\n")
		fmt.Printf("Version:    %s\n", version)
		fmt.Printf("Commit:     %s\n", commit)
		fmt.Printf("Date:       %s\n", date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
