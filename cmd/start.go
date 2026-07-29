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
	Short: "Start the MCP gateway server",
	Long: `Start the MCP gateway server with the configured settings.
The server will listen for MCP client connections and forward them to the configured downstream servers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create context for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		Logger.Info(ctx, "Starting MCP gateway")

		// Create gateway instance
		p, err := gateway.New(ctx, cfg, Logger, version, ResolvedLogDir)
		if err != nil {
			return fmt.Errorf("failed to create gateway: %w", err)
		}

		// Handle shutdown signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		var shutdownSignal os.Signal
		go func() {
			shutdownSignal = <-sigChan
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
		Logger.Info(shutdownCtx, "Shutting down gateway", zap.String("signal", shutdownSignal.String()))

		// Stop the gateway
		if err := p.Stop(shutdownCtx); err != nil {
			return fmt.Errorf("failed to stop gateway: %w", err)
		}

		return nil
	},
}

func init() {
	gatewayCmd.AddCommand(startCmd)
}
