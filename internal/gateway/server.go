package gateway

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/yosida95/uritemplate/v3"
	"go.uber.org/zap"
)

// zapLogWriter is a writer that adapts log output to zap logger
type zapLogWriter struct {
	logger *zap.Logger
}

func (w *zapLogWriter) Write(p []byte) (n int, err error) {
	w.logger.Error(string(p))
	return len(p), nil
}

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

	// Get all clients to determine combined capabilities
	allClients := p.clientManager.GetAllClients()

	// Determine combined capabilities from all clients
	hasTools := false
	hasPrompts := false
	hasResources := false
	hasSubscribe := false
	hasListChanged := false

	for _, clientInfo := range allClients {
		if clientInfo.Capabilities != nil {
			if clientInfo.Capabilities.Tools != nil {
				hasTools = true
				if clientInfo.Capabilities.Tools.ListChanged {
					hasListChanged = true
				}
			}
			if clientInfo.Capabilities.Prompts != nil {
				hasPrompts = true
				if clientInfo.Capabilities.Prompts.ListChanged {
					hasListChanged = true
				}
			}
			if clientInfo.Capabilities.Resources != nil {
				hasResources = true
				if clientInfo.Capabilities.Resources.Subscribe {
					hasSubscribe = true
				}
				if clientInfo.Capabilities.Resources.ListChanged {
					hasListChanged = true
				}
			}
		}
	}

	// Add capabilities based on combined client capabilities
	if hasTools {
		opts = append(opts, server.WithToolCapabilities(hasListChanged))
	}
	if hasPrompts {
		opts = append(opts, server.WithPromptCapabilities(hasListChanged))
	}
	if hasResources {
		opts = append(opts, server.WithResourceCapabilities(hasSubscribe, hasListChanged))
	}

	srv := server.NewMCPServer("maybe-dont", "0.0.1", opts...)

	// Register tools/prompts/resources from all clients with prefixed names
	for clientName, clientInfo := range allClients {
		// Register tools
		for _, tool := range clientInfo.Tools {
			prefixedTool := tool
			prefixedTool.Name = PrefixName(clientName, tool.Name)
			srv.AddTool(prefixedTool, p.handleToolCallWithErrorHandling)
			p.logger.Debug("Registered tool",
				zap.String("client", clientName),
				zap.String("original_name", tool.Name),
				zap.String("prefixed_name", prefixedTool.Name))
		}

		// Register prompts
		for _, prompt := range clientInfo.Prompts {
			prefixedPrompt := prompt
			prefixedPrompt.Name = PrefixName(clientName, prompt.Name)
			srv.AddPrompt(prefixedPrompt, p.HandlePromptCall)
			p.logger.Debug("Registered prompt",
				zap.String("client", clientName),
				zap.String("original_name", prompt.Name),
				zap.String("prefixed_name", prefixedPrompt.Name))
		}

		// Register resources
		for _, resource := range clientInfo.Resources {
			prefixedResource := resource
			prefixedResource.URI = PrefixName(clientName, resource.URI)
			srv.AddResource(prefixedResource, p.HandleResourceCall)
			p.logger.Debug("Registered resource",
				zap.String("client", clientName),
				zap.String("original_uri", resource.URI),
				zap.String("prefixed_uri", prefixedResource.URI))
		}

		// Register resource templates
		for _, template := range clientInfo.Templates {
			prefixedTemplate := template
			if template.URITemplate != nil {
				originalRaw := template.URITemplate.Raw()
				prefixedRaw := PrefixName(clientName, originalRaw)

				// Create new URITemplate from prefixed raw string
				newTemplate, err := uritemplate.New(prefixedRaw)
				if err != nil {
					p.logger.Error("Failed to create prefixed URI template",
						zap.String("client", clientName),
						zap.String("original", originalRaw),
						zap.String("prefixed", prefixedRaw),
						zap.Error(err))
					continue
				}
				prefixedTemplate.URITemplate = &mcp.URITemplate{Template: newTemplate}
			}
			srv.AddResourceTemplate(prefixedTemplate, p.HandleResourceTemplateCall)
			p.logger.Debug("Registered resource template",
				zap.String("client", clientName),
				zap.String("original_template", func() string {
					if template.URITemplate != nil {
						return template.URITemplate.Raw()
					}
					return ""
				}()),
				zap.String("prefixed_template", func() string {
					if prefixedTemplate.URITemplate != nil {
						return prefixedTemplate.URITemplate.Raw()
					}
					return ""
				}()))
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

	// Create zap logger adapter for stdio server
	zapWriter := &zapLogWriter{logger: p.logger}
	stdioSrv.SetErrorLogger(log.New(zapWriter, "", 0))

	p.server = srv

	// Create error channel for startup confirmation
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		defer close(errChan)
		if err := stdioSrv.Listen(ctx, os.Stdin, os.Stdout); err != nil {
			p.logger.Error("Failed to start STDIO server", zap.Error(err))
			errChan <- err
		}
	}()

	// Check for startup errors (with timeout)
	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("STDIO server startup failed: %w", err)
		}
	case <-time.After(100 * time.Millisecond):
		// No immediate error, assume successful startup
	}

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

	// Create error channel for startup confirmation
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		defer close(errChan)
		if err := sseSrv.Start(p.config.Server.ListenAddr); err != nil {
			p.logger.Error("Failed to start SSE server", zap.Error(err))
			errChan <- err
		}
	}()

	// Monitor context for cancellation
	go func() {
		<-ctx.Done()
		// Use timeout context for shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := sseSrv.Shutdown(shutdownCtx); err != nil {
			p.logger.Error("Error shutting down SSE server", zap.Error(err))
		}
	}()

	// Check for startup errors (with timeout)
	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("SSE server startup failed: %w", err)
		}
	case <-time.After(100 * time.Millisecond):
		// No immediate error, assume successful startup
	}

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

	p.server = srv

	// Create error channel for startup confirmation
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		defer close(errChan)
		if err := httpSrv.Start(p.config.Server.ListenAddr); err != nil {
			p.logger.Error("Failed to start HTTP server", zap.Error(err))
			errChan <- err
		}
	}()

	// Monitor context for cancellation
	go func() {
		<-ctx.Done()
		// Use timeout context for shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			p.logger.Error("Error shutting down HTTP server", zap.Error(err))
		}
	}()

	// Check for startup errors (with timeout)
	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("HTTP server startup failed: %w", err)
		}
	case <-time.After(100 * time.Millisecond):
		// No immediate error, assume successful startup
	}

	p.logger.Info("HTTP server started", zap.String("listen_addr", p.config.Server.ListenAddr))

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
