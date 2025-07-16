package gateway

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

func (p *Gateway) initServer(ctx context.Context) error {
	switch p.config.Server.Type {
	case "stdio":
		return p.initStdioServer(ctx)
	case "sse":
		return p.initSSEServer(ctx)
	case "http":
		return p.initHTTPServer(ctx)
	default:
		return fmt.Errorf("unsupported server type: %s", p.config.Server.Type)
	}
}

// initMCPServer initializes the MCP server with common configuration and registers tools
func (p *Gateway) initMCPServer() (*server.MCPServer, error) {
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
			srv.AddTool(tool, p.handleToolCallWithErrorHandling)
			p.logger.Debug("Registered tool", zap.Any("tool", tool))
		}
	}

	if len(p.capabilityDetails.Prompts) > 0 {
		for _, prompt := range p.capabilityDetails.Prompts {
			srv.AddPrompt(prompt, p.HandlePromptCall)
			p.logger.Debug("Registered prompt", zap.Any("prompt", prompt))
		}
	}

	if len(p.capabilityDetails.Resources) > 0 {
		for _, resource := range p.capabilityDetails.Resources {
			srv.AddResource(resource, p.HandleResourceCall)
			p.logger.Debug("Registered resource", zap.Any("resource", resource))
		}
	}

	if len(p.capabilityDetails.Templates) > 0 {
		for _, template := range p.capabilityDetails.Templates {
			srv.AddResourceTemplate(template, p.HandleResourceTemplateCall)
			p.logger.Debug("Registered resource template", zap.Any("template", template))
		}
	}

	return srv, nil
}

func (p *Gateway) initStdioServer(ctx context.Context) error {
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

	p.logger.Info("STDIO server started")

	return nil
}

func (p *Gateway) initSSEServer(ctx context.Context) error {
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

	p.logger.Info("SSE server started", zap.String("listen_addr", p.config.Server.ListenAddr))

	return nil
}

func (p *Gateway) initHTTPServer(ctx context.Context) error {
	srv, err := p.initMCPServer()
	if err != nil {
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	// Create HTTP server
	httpSrv := server.NewStreamableHTTPServer(srv,
		server.WithEndpointPath("/mcp"),
	)

	// Add OAuth endpoints if authentication is enabled
	if p.authManager != nil && p.authManager.GetHandlers() != nil {
		handlers := p.authManager.GetHandlers()
		
		// Create a custom HTTP server to add OAuth endpoints
		mux := http.NewServeMux()
		
		// Add OAuth endpoints
		mux.HandleFunc("/oauth/authorize", handlers.AuthorizeHandler)
		mux.HandleFunc("/oauth/callback", handlers.CallbackHandler)
		mux.HandleFunc("/oauth/token", handlers.TokenHandler)
		mux.HandleFunc("/oauth/revoke", handlers.RevokeHandler)
		mux.HandleFunc("/oauth/userinfo", handlers.UserInfoHandler)
		
		// Add MCP endpoint with optional authentication middleware
		var mcpHandler http.Handler = httpSrv
		if p.config.Auth.Type == "oauth2" {
			mcpHandler = handlers.AuthMiddleware(mcpHandler)
		}
		mux.Handle("/mcp", mcpHandler)
		
		// Create custom HTTP server
		customSrv := &http.Server{
			Addr:    p.config.Server.ListenAddr,
			Handler: mux,
		}
		
		p.server = srv

		// Start server in a goroutine
		go func() {
			p.logger.Info("HTTP server with OAuth endpoints started", zap.String("listen_addr", p.config.Server.ListenAddr))
			if err := customSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				p.logger.Error("Failed to start HTTP server", zap.Error(err))
			}
		}()

		// Monitor context for cancellation
		go func() {
			<-ctx.Done()
			if err := customSrv.Shutdown(context.Background()); err != nil {
				p.logger.Error("Error shutting down HTTP server", zap.Error(err))
			}
		}()
		
		return nil
	}

	p.server = srv

	// Start server in a goroutine
	go func() {
		if err := httpSrv.Start(p.config.Server.ListenAddr); err != nil {
			p.logger.Error("Failed to start HTTP server", zap.Error(err))
		}
	}()

	p.logger.Info("HTTP server started", zap.String("listen_addr", p.config.Server.ListenAddr))

	// Monitor context for cancellation
	go func() {
		<-ctx.Done()
		if err := httpSrv.Shutdown(context.Background()); err != nil {
			p.logger.Error("Error shutting down HTTP server", zap.Error(err))
		}
	}()

	return nil
}

// Custom tool handler that handles PolicyDeniedError and returns proper MCP error responses
func (p *Gateway) handleToolCallWithErrorHandling(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, err := p.HandleToolCall(ctx, req)
	if err != nil {
		// Check if it's a PolicyDeniedError
		if policyErr, ok := err.(*PolicyDeniedError); ok {
			// Create error result with user-friendly message
			errorResult := mcp.NewToolResultError(policyErr.Message)

			// Add structured error data to the result
			if errorResult.Meta == nil {
				errorResult.Meta = make(map[string]interface{})
			}
			errorResult.Meta["error_code"] = -32600 // Invalid Request
			errorResult.Meta["error_data"] = policyErr.Data

			return errorResult, nil
		}
		// For other errors, return them as-is
		return nil, err
	}
	return result, nil
}
