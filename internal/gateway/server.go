package gateway

import (
	"context"
	"fmt"
	"log"
	"net/http"
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

func (g *Gateway) initServer(ctx context.Context) error {
	switch g.config.Server.Type {
	case "stdio":
		return g.initStdioServer(ctx)
	case "sse":
		return g.initSSEServer(ctx)
	case "http":
		return g.initHTTPServer(ctx)
	default:
		return fmt.Errorf("unsupported server type: %s", g.config.Server.Type)
	}
}

// handleInitializeRequest handles the initialize request and performs lazy capability loading
func (g *Gateway) handleInitializeRequest(ctx context.Context, id any, req *mcp.InitializeRequest) {
	// Extract request ID from context
	requestID, hasRequestID := GetRequestID(ctx)
	if !hasRequestID {
		g.logger.Warn(ctx, "No request ID found in context for initialize request")
		return
	}

	g.logger.Info(ctx, "Handling initialize request", zap.String("request_id", requestID))

	// Get list of clients that require lazy initialization
	lazyClients := g.clientManager.GetLazyInitClients()

	// If there are lazy init clients, check their capabilities synchronously
	if len(lazyClients) > 0 {
		g.logger.Info(ctx, "Checking capabilities for lazy init clients",
			zap.String("request_id", requestID),
			zap.Int("client_count", len(lazyClients)))

		for _, clientName := range lazyClients {
			clientInfo, err := g.clientManager.CheckCapabilitiesForSession(ctx, clientName, requestID)
			if err != nil {
				g.logger.Error(ctx, "Failed to check capabilities for client",
					zap.String("client", clientName),
					zap.String("request_id", requestID),
					zap.Error(err))
				// Note: We can't return an error from this hook, but the error will be
				// reflected when the client tries to use tools that failed to initialize
				continue
			}

			// clientInfo will be nil if capabilities were already loaded
			if clientInfo == nil {
				continue
			}

			// Dynamically register tools for this client
			g.registerClientTools(ctx, clientName, clientInfo)
			g.registerClientPrompts(ctx, clientName, clientInfo)
			g.registerClientResources(ctx, clientName, clientInfo)

			g.logger.Info(ctx, "Dynamically registered client capabilities",
				zap.String("client", clientName),
				zap.String("request_id", requestID))
		}
	}
}

// registerClientTools registers all tools from a client with prefixed names
func (g *Gateway) registerClientTools(ctx context.Context, clientName string, clientInfo *ClientInfo) {
	for _, tool := range clientInfo.Tools {
		prefixedTool := tool
		prefixedTool.Name = PrefixName(clientName, tool.Name)
		g.server.AddTool(prefixedTool, g.handleToolCallWithErrorHandling)
		g.logger.Debug(ctx, "Registered tool",
			zap.String("client", clientName),
			zap.String("original_name", tool.Name),
			zap.String("prefixed_name", prefixedTool.Name))
	}
}

// registerClientPrompts registers all prompts from a client with prefixed names
func (g *Gateway) registerClientPrompts(ctx context.Context, clientName string, clientInfo *ClientInfo) {
	for _, prompt := range clientInfo.Prompts {
		prefixedPrompt := prompt
		prefixedPrompt.Name = PrefixName(clientName, prompt.Name)
		g.server.AddPrompt(prefixedPrompt, g.HandlePromptCall)
		g.logger.Debug(ctx, "Registered prompt",
			zap.String("client", clientName),
			zap.String("original_name", prompt.Name),
			zap.String("prefixed_name", prefixedPrompt.Name))
	}
}

// registerClientResources registers all resources and templates from a client with prefixed names
func (g *Gateway) registerClientResources(ctx context.Context, clientName string, clientInfo *ClientInfo) {
	for _, resource := range clientInfo.Resources {
		prefixedResource := resource
		prefixedResource.URI = PrefixName(clientName, resource.URI)
		g.server.AddResource(prefixedResource, g.HandleResourceCall)
		g.logger.Debug(ctx, "Registered resource",
			zap.String("client", clientName),
			zap.String("original_uri", resource.URI),
			zap.String("prefixed_uri", prefixedResource.URI))
	}

	for _, template := range clientInfo.Templates {
		prefixedTemplate := template
		if template.URITemplate != nil {
			originalRaw := template.URITemplate.Raw()
			prefixedRaw := PrefixName(clientName, originalRaw)

			// Create new URITemplate from prefixed raw string
			newTemplate, err := uritemplate.New(prefixedRaw)
			if err != nil {
				g.logger.Error(ctx, "Failed to create prefixed URI template",
					zap.String("client", clientName),
					zap.String("original", originalRaw),
					zap.String("prefixed", prefixedRaw),
					zap.Error(err))
				continue
			}
			prefixedTemplate.URITemplate = &mcp.URITemplate{Template: newTemplate}
		}
		g.server.AddResourceTemplate(prefixedTemplate, g.HandleResourceTemplateCall)
		g.logger.Debug(ctx, "Registered resource template",
			zap.String("client", clientName))
	}
}

// initMCPServer initializes the MCP server with common configuration and registers tools
func (g *Gateway) initMCPServer() (*server.MCPServer, error) {
	// Create hooks for initialize request handling
	hooks := &server.Hooks{}
	hooks.AddBeforeInitialize(g.handleInitializeRequest)

	opts := []server.ServerOption{
		server.WithLogging(),
		server.WithRecovery(),
		server.WithHooks(hooks),
	}

	// Get all clients to determine combined capabilities
	allClients := g.clientManager.GetAllClients()

	// Determine combined capabilities from all clients
	hasTools := false
	hasPrompts := false
	hasResources := false
	hasSubscribe := false
	hasListChanged := false

	// Check if native tools are enabled (they contribute to hasTools)
	if g.config.NativeTools.Enabled {
		nativeTools := g.nativeToolsHandler.GetTools()
		if len(nativeTools) > 0 {
			hasTools = true
		}
	}

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

	srv := server.NewMCPServer("maybe-dont", g.version, opts...)
	g.server = srv

	// Register tools/prompts/resources from non-lazy clients
	// Lazy clients will be registered dynamically when a session initializes
	ctx := context.Background()
	for clientName, clientInfo := range allClients {
		// Skip lazy init clients - their capabilities will be loaded and registered per-session
		if clientInfo.RequiresLazyInit {
			g.logger.Debug(ctx, "Skipping registration for lazy init client",
				zap.String("client", clientName))
			continue
		}

		g.registerClientTools(ctx, clientName, clientInfo)
		g.registerClientPrompts(ctx, clientName, clientInfo)
		g.registerClientResources(ctx, clientName, clientInfo)
	}

	// Register native gateway tools
	if g.config.NativeTools.Enabled {
		nativeTools := g.nativeToolsHandler.GetTools()
		for _, tool := range nativeTools {
			g.server.AddTool(tool, g.handleToolCallWithErrorHandling)
			g.logger.Info(ctx, "Registered native tool", zap.String("name", tool.Name))
		}
	}

	return srv, nil
}

func (g *Gateway) initStdioServer(ctx context.Context) error {
	srv, err := g.initMCPServer()
	if err != nil {
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	// Create STDIO server
	stdioSrv := server.NewStdioServer(srv)

	// Create zap logger adapter for stdio server
	zapWriter := &zapLogWriter{logger: g.logger.Logger()}
	stdioSrv.SetErrorLogger(log.New(zapWriter, "", 0))

	g.server = srv

	// Create error channel for startup confirmation
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		defer close(errChan)
		if err := stdioSrv.Listen(ctx, os.Stdin, os.Stdout); err != nil {
			g.logger.Error(ctx, "Failed to start STDIO server", zap.Error(err))
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

	g.logger.Info(ctx, "STDIO server started")

	return nil
}

func (g *Gateway) initSSEServer(ctx context.Context) error {
	srv, err := g.initMCPServer()
	if err != nil {
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	// Create SSE server with auth extraction context function
	sseSrv := server.NewSSEServer(srv,
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
		server.WithSSEContextFunc(g.extractAuthFromRequest),
	)

	g.server = srv

	// Create error channel for startup confirmation
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		defer close(errChan)
		if err := sseSrv.Start(g.config.Server.ListenAddr); err != nil {
			g.logger.Error(context.Background(), "Failed to start SSE server", zap.Error(err))
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
			g.logger.Error(shutdownCtx, "Error shutting down SSE server", zap.Error(err))
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

	g.logger.Info(ctx, "SSE server started", zap.String("listen_addr", g.config.Server.ListenAddr))

	return nil
}

func (g *Gateway) initHTTPServer(ctx context.Context) error {
	srv, err := g.initMCPServer()
	if err != nil {
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	// Create HTTP server with auth extraction context function
	httpSrv := server.NewStreamableHTTPServer(srv,
		server.WithEndpointPath("/mcp"),
		server.WithHTTPContextFunc(g.extractAuthFromRequest),
	)

	g.server = srv

	// Create error channel for startup confirmation
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		defer close(errChan)
		if err := httpSrv.Start(g.config.Server.ListenAddr); err != nil {
			g.logger.Error(context.Background(), "Failed to start HTTP server", zap.Error(err))
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
			g.logger.Error(shutdownCtx, "Error shutting down HTTP server", zap.Error(err))
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

	g.logger.Info(ctx, "HTTP server started", zap.String("listen_addr", g.config.Server.ListenAddr))

	return nil
}

// Custom tool handler that handles PolicyDeniedError and returns proper MCP error responses
func (g *Gateway) handleToolCallWithErrorHandling(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, err := g.HandleToolCall(ctx, req)
	if err != nil {
		// Check if it's a PolicyDeniedError
		if policyErr, ok := err.(*PolicyDeniedError); ok {
			// Create error result with user-friendly message
			errorResult := mcp.NewToolResultError(policyErr.Message)

			// Add structured error data to the result
			if errorResult.Meta == nil {
				errorResult.Meta = &mcp.Meta{}
			}
			if errorResult.Meta.AdditionalFields == nil {
				errorResult.Meta.AdditionalFields = make(map[string]interface{})
			}
			errorResult.Meta.AdditionalFields["error_code"] = -32600 // Invalid Request
			errorResult.Meta.AdditionalFields["error_data"] = policyErr.Data

			return errorResult, nil
		}
		// For other errors, return them as-is
		return nil, err
	}
	return result, nil
}

// extractAuthFromRequest extracts authentication credentials from HTTP request headers
// and stores them in context for pass-through authentication. It also generates a request ID
// for tracking capabilities per session.
func (g *Gateway) extractAuthFromRequest(ctx context.Context, r *http.Request) context.Context {
	// Generate or extract request ID
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		// Generate new request ID if not provided
		var err error
		requestID, err = GenerateRequestID()
		if err != nil {
			g.logger.Error(context.Background(), "Failed to generate request ID", zap.Error(err))
			requestID = "unknown"
		}
	}

	// Record the session in metrics
	if g.metricsCollector != nil {
		g.metricsCollector.RecordRequest(requestID)
	}

	// Store request ID in context first so it can be used in logging
	ctx = WithRequestID(ctx, requestID)

	if r.Header.Get("X-Request-ID") == "" {
		g.logger.Debug(ctx, "Generated new request ID")
	} else {
		g.logger.Debug(ctx, "Using existing request ID")
	}

	// Create credentials storage
	serviceCreds := NewServiceCredentials()

	// Extract credentials from client pass-through configurations
	for clientName, clientConfig := range g.config.DownstreamMCPServers {
		// Skip if pass-through is not enabled
		if !clientConfig.Auth.PassThrough.Enabled {
			continue
		}

		var clientCreds *ClientCredentials

		// Extract credentials from configured header mappings
		for _, mapping := range clientConfig.Auth.PassThrough.Headers {
			headerValue := r.Header.Get(mapping.SourceHeader)
			if headerValue == "" {
				continue
			}

			// Initialize client credentials if needed
			if clientCreds == nil {
				clientCreds = NewClientCredentials()
			}

			// Store credential using target_header as key
			clientCreds.SetHeader(mapping.TargetHeader, headerValue)

			g.logger.Debug(ctx, "Extracted credential from header",
				zap.String("source_header", mapping.SourceHeader),
				zap.String("client", clientName),
				zap.String("target_header", mapping.TargetHeader))
		}

		// Only store client credentials if we extracted any
		if clientCreds != nil {
			serviceCreds.SetClient(clientName, clientCreds)
		}
	}

	// Store credentials in context
	return WithServiceCredentials(ctx, serviceCreds)
}
