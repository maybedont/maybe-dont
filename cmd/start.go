package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Launch the MCP security proxy server",
	Long: `Start the MCP security proxy server with the configured settings.
The server will begin listening for connections and enforcing security policies.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := zap.L().Named("start")
		logger.Info("Starting MCP security proxy")

		// TODO: Implement proxy server startup
		// 1. Initialize proxy configuration
		// 2. Set up authentication
		// 3. Load and compile CEL policies
		// 4. Start transport listeners
		// 5. Begin accepting connections

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)

	// Start command specific flags
	startCmd.Flags().Bool("dry-run", false, "Validate configuration and exit")
	startCmd.Flags().String("listen-addr", "", "Override listen address")
	startCmd.Flags().String("downstream-url", "", "Override downstream server URL")
	startCmd.Flags().String("auth-type", "", "Override authentication type")

	// Bind flags to viper
	viper.BindPFlag("proxy.dry_run", startCmd.Flags().Lookup("dry-run"))
	viper.BindPFlag("proxy.listen.address", startCmd.Flags().Lookup("listen-addr"))
	viper.BindPFlag("server.url", startCmd.Flags().Lookup("downstream-url"))
	viper.BindPFlag("security.auth.type", startCmd.Flags().Lookup("auth-type"))
} 