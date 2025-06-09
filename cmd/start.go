package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/maybedont/maybe-dont/internal/proxy"
	"github.com/spf13/cobra"
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
		defer func() {
			cancel()
			_ = Logger.Sync()
		}()

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
}
