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
The server will listen for MCP client connections and forward them to the configured downstream server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create context for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		Logger.Info(ctx, "Starting MCP security gateway")

		// Increment gateway starts metric and report immediately
		if MetricsCollector != nil {
			MetricsCollector.IncrementGatewayStarts()
			// Always send metrics on gateway start
			if err := MetricsCollector.Report(ctx); err != nil {
				Logger.Warn(ctx, "Failed to report metrics on gateway start", zap.Error(err))
			}
		}

		// Create gateway instance
		p, err := gateway.New(ctx, cfg, Logger)
		if err != nil {
			return fmt.Errorf("failed to create gateway: %w", err)
		}

		// Pass metrics collector to gateway
		if MetricsCollector != nil {
			p.SetMetricsCollector(MetricsCollector)
		}

		// Handle shutdown signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			// Create a new context for shutdown logging since the main ctx will be cancelled
			Logger.Info(context.Background(), "Received shutdown signal", zap.String("signal", sig.String()))
			cancel()
		}()

		// Start the gateway
		if err := p.Start(ctx); err != nil {
			return fmt.Errorf("failed to start gateway: %w", err)
		}

		// Wait for shutdown
		<-ctx.Done()
		// Create a new context for shutdown logging
		shutdownCtx := context.Background()
		Logger.Info(shutdownCtx, "Shutting down gateway")

		// Close metrics collector before shutdown (stops background flush and performs final flush)
		if MetricsCollector != nil {
			if err := MetricsCollector.Close(); err != nil {
				Logger.Warn(shutdownCtx, "Failed to close metrics collector on shutdown", zap.Error(err))
			}
		}

		// Stop the gateway
		if err := p.Stop(shutdownCtx); err != nil {
			return fmt.Errorf("failed to stop gateway: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
