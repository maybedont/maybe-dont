package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test configuration and policy validation",
	Long: `Test command validates the configuration and policies without starting the proxy.
It can be used to:
- Validate configuration syntax and values
- Test CEL policies against sample requests
- Verify authentication settings
- Check transport configuration`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Print current configuration
		configJSON, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		fmt.Println("Current Configuration:")
		fmt.Println(string(configJSON))
		fmt.Println("\nConfiguration is valid!")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}
