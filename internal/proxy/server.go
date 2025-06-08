package proxy

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

func (p *Proxy) initServer(ctx context.Context) error {
	switch p.config.Server.Type {
	case "sse":
		return p.initSSEServer(ctx)
	default:
		return fmt.Errorf("unsupported server type: %s", p.config.Server.Type)
	}
}

func (p *Proxy) initSSEServer(ctx context.Context) error {
	opts := []server.ServerOption{
		server.WithLogging(),
		server.WithRecovery(),
	}

	// Add capabilities if available
	if p.capabilities != nil {
		// Enable capabilities based on server response
		if p.capabilities.Prompts != nil {
			opts = append(opts, server.WithPromptCapabilities(p.capabilities.Prompts.ListChanged))
		}
		if p.capabilities.Resources != nil {
			opts = append(opts, server.WithResourceCapabilities(
				p.capabilities.Resources.Subscribe,
				p.capabilities.Resources.ListChanged,
			))
		}
		if p.capabilities.Tools != nil {
			opts = append(opts, server.WithToolCapabilities(p.capabilities.Tools.ListChanged))
		}
	}

	srv := server.NewMCPServer("maybe-dont", "0.0.1", opts...)

	// Create SSE server
	sseSrv := server.NewSSEServer(srv,
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
	)

	p.server = srv

	// Start server in a goroutine
	go func() {
		if err := sseSrv.Start(p.config.Server.ListenAddr); err != nil {
			p.logger.Error("SSE server error", zap.Error(err))
		}
	}()

	// Monitor context for cancellation
	go func() {
		<-ctx.Done()
		p.logger.Info("Context cancelled, shutting down SSE server")
		// The server will be terminated when the process exits
	}()

	return nil
}
