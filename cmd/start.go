package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/maybedont/maybe-dont/internal/gateway"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Launch the MCP security gateway server",
	Long: `Start the MCP security gateway server with the configured settings.
The server will listen for MCP client connections and proxy them to the configured downstream server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		Logger.Info("Starting MCP security gateway")
		// Create gateway instance
		p, err := gateway.New(cfg, Logger)
		if err != nil {
			return fmt.Errorf("failed to create gateway: %w", err)
		}

		// Create context for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle shutdown signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			Logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
			cancel()
		}()

		// Start the gateway
		if err := p.Start(ctx); err != nil {
			return fmt.Errorf("failed to start gateway: %w", err)
		}

		// Wait for shutdown
		<-ctx.Done()
		Logger.Info("Shutting down gateway")

		// Stop the gateway
		if err := p.Stop(); err != nil {
			return fmt.Errorf("failed to stop gateway: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
