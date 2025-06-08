package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/sudermanjr/maybe-dont/internal/proxy"
	"go.uber.org/zap"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Launch the MCP security proxy server",
	Long: `Start the MCP security proxy server with the configured settings.
The server will begin listening for connections and enforcing security policies.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		Logger.Info("Starting MCP security proxy")
		// Create proxy instance
		p, err := proxy.New(cfg, Logger)
		if err != nil {
			return err
		}

		// Create context that will be canceled on interrupt
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle OS signals
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigCh
			Logger.Info("Received signal, shutting down", zap.String("signal", sig.String()))
			cancel()
		}()

		// Start the proxy
		if err := p.Start(ctx); err != nil {
			return err
		}

		// Wait for context cancellation
		<-ctx.Done()

		Logger.Info("Shutting down proxy")
		if err := p.Stop(); err != nil {
			Logger.Error("Error during shutdown", zap.Error(err))
			return err
		}

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
	if err := viper.BindPFlag("proxy.dry_run", startCmd.Flags().Lookup("dry-run")); err != nil {
		Logger.Fatal("Failed to bind dry-run flag", zap.Error(err))
	}
	if err := viper.BindPFlag("server.listen_addr", startCmd.Flags().Lookup("listen-addr")); err != nil {
		Logger.Fatal("Failed to bind listen-addr flag", zap.Error(err))
	}
	if err := viper.BindPFlag("transport.downstream_url", startCmd.Flags().Lookup("downstream-url")); err != nil {
		Logger.Fatal("Failed to bind downstream-url flag", zap.Error(err))
	}
	if err := viper.BindPFlag("security.auth.type", startCmd.Flags().Lookup("auth-type")); err != nil {
		Logger.Fatal("Failed to bind auth-type flag", zap.Error(err))
	}
}
