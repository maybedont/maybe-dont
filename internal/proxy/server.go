package proxy

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

func (p *Proxy) initServer(ctx context.Context) error {
	switch p.config.Server.Type {
	case "stdio":
		return p.initStdioServer(ctx)
	case "sse":
		return p.initSSEServer(ctx)
	default:
		return fmt.Errorf("unsupported server type: %s", p.config.Server.Type)
	}
}

// initMCPServer initializes the MCP server with common configuration and registers tools
func (p *Proxy) initMCPServer() (*server.MCPServer, error) {
	opts := []server.ServerOption{
		server.WithLogging(),
		server.WithRecovery(),
	}

	// Add capabilities if available
	if p.capabilities != nil {
		// Enable capabilities based on server response
		if p.capabilities.Prompts != nil {
			opts = append(opts, server.WithPromptCapabilities(
				p.capabilities.Prompts.ListChanged,
			))
		}
		if p.capabilities.Resources != nil {
			opts = append(opts, server.WithResourceCapabilities(
				p.capabilities.Resources.Subscribe,
				p.capabilities.Resources.ListChanged,
			))
		}
		if p.capabilities.Tools != nil {
			opts = append(opts, server.WithToolCapabilities(
				p.capabilities.Tools.ListChanged,
			))
		}
	}

	srv := server.NewMCPServer("maybe-dont", "0.0.1", opts...)

	// Register detailed capability information
	if len(p.capabilityDetails.Tools) > 0 {
		for _, tool := range p.capabilityDetails.Tools {
			srv.AddTool(tool, p.HandleToolCall)
			p.logger.Info("Registered tool", zap.Any("tool", tool))
		}
	}

	if len(p.capabilityDetails.Prompts) > 0 {
		for _, prompt := range p.capabilityDetails.Prompts {
			srv.AddPrompt(prompt, p.HandlePromptCall)
			p.logger.Info("Registered prompt", zap.Any("prompt", prompt))
		}
	}

	if len(p.capabilityDetails.Resources) > 0 {
		for _, resource := range p.capabilityDetails.Resources {
			srv.AddResource(resource, p.HandleResourceCall)
			p.logger.Info("Registered resource", zap.Any("resource", resource))
		}
	}

	if len(p.capabilityDetails.Templates) > 0 {
		for _, template := range p.capabilityDetails.Templates {
			srv.AddResourceTemplate(template, p.HandleResourceTemplateCall)
			p.logger.Info("Registered resource template", zap.Any("template", template))
		}
	}

	return srv, nil
}

func (p *Proxy) initStdioServer(ctx context.Context) error {
	srv, err := p.initMCPServer()
	if err != nil {
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	// Create STDIO server
	stdioSrv := server.NewStdioServer(srv)

	// Set error logger
	stdioSrv.SetErrorLogger(log.New(os.Stderr, "", log.LstdFlags))

	p.server = srv

	// Start server in a goroutine
	go func() {
		if err := stdioSrv.Listen(ctx, os.Stdin, os.Stdout); err != nil {
			p.logger.Error("Failed to start STDIO server", zap.Error(err))
		}
	}()

	return nil
}

func (p *Proxy) initSSEServer(ctx context.Context) error {
	srv, err := p.initMCPServer()
	if err != nil {
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	// Create SSE server
	sseSrv := server.NewSSEServer(srv,
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
	)

	p.server = srv

	// Start server in a goroutine
	go func() {
		if err := sseSrv.Start(p.config.Server.ListenAddr); err != nil {
			p.logger.Error("Failed to start SSE server", zap.Error(err))
		}
	}()

	// Monitor context for cancellation
	go func() {
		<-ctx.Done()
		if err := sseSrv.Shutdown(context.Background()); err != nil {
			p.logger.Error("Error shutting down SSE server", zap.Error(err))
		}
	}()

	return nil
}
