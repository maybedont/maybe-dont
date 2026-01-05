package gateway

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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

// onSessionRegister handles new upstream client sessions by creating downstream clients
func (g *Gateway) onSessionRegister(ctx context.Context, session server.ClientSession) {
	sessionID := session.SessionID()
	g.logger.Info(ctx, "New upstream session registered", zap.String("session_id", sessionID))

	// Create downstream clients for this session
	result, err := g.clientManager.CreateSessionClients(ctx, sessionID)
	if err != nil {
		g.logger.Error(ctx, "Failed to create downstream clients for session",
			zap.String("session_id", sessionID),
			zap.Error(err))
		// Note: We can't return an error from this hook, but tools will fail when used
		// Continue to register any tools that were successfully discovered
	}

	// Store client IP in session (extracted from HTTP request context)
	if clientIP, ok := GetClientIP(ctx); ok && clientIP != "" {
		g.clientManager.SetSessionClientIP(sessionID, clientIP)
		g.logger.Debug(ctx, "Stored client IP for session",
			zap.String("session_id", sessionID),
			zap.String("client_ip", clientIP))
	}

	// Register session-specific tools for pass-through clients
	if result != nil && len(result.PassThroughClients) > 0 {
		g.registerSessionTools(ctx, sessionID, result.PassThroughClients)
	}

	g.logger.Info(ctx, "Downstream clients created for session", zap.String("session_id", sessionID))
}

// registerSessionTools registers tools from pass-through clients as session-specific tools
func (g *Gateway) registerSessionTools(ctx context.Context, sessionID string, clients map[string]*SessionClientInfo) {
	for clientName, clientInfo := range clients {
		for _, tool := range clientInfo.Tools {
			prefixedTool := tool
			prefixedTool.Name = PrefixName(clientName, tool.Name)

			err := g.server.AddSessionTool(sessionID, prefixedTool, g.handleToolCallWithErrorHandling)
			if err != nil {
				g.logger.Error(ctx, "Failed to register session tool",
					zap.String("session_id", sessionID),
					zap.String("client", clientName),
					zap.String("tool", prefixedTool.Name),
					zap.Error(err))
				continue
			}

			g.logger.Debug(ctx, "Registered session-specific tool",
				zap.String("session_id", sessionID),
				zap.String("client", clientName),
				zap.String("tool", prefixedTool.Name))
		}

		g.logger.Info(ctx, "Registered session-specific tools from pass-through client",
			zap.String("session_id", sessionID),
			zap.String("client", clientName),
			zap.Int("tools_count", len(clientInfo.Tools)))
	}
}

// onSessionUnregister handles upstream client session cleanup
func (g *Gateway) onSessionUnregister(ctx context.Context, session server.ClientSession) {
	sessionID := session.SessionID()
	g.logger.Info(ctx, "Upstream session unregistered", zap.String("session_id", sessionID))

	// Close downstream clients for this session
	if err := g.clientManager.CloseSessionClients(ctx, sessionID); err != nil {
		g.logger.Error(ctx, "Failed to close downstream clients for session",
			zap.String("session_id", sessionID),
			zap.Error(err))
	}

	g.logger.Info(ctx, "Downstream clients closed for session", zap.String("session_id", sessionID))
}


// initMCPServer initializes the MCP server with common configuration and registers tools
func (g *Gateway) initMCPServer() (*server.MCPServer, error) {
	ctx := context.Background()

	// Create hooks for session lifecycle management
	hooks := &server.Hooks{}
	hooks.AddOnRegisterSession(g.onSessionRegister)
	hooks.AddOnUnregisterSession(g.onSessionUnregister)

	opts := []server.ServerOption{
		server.WithLogging(),
		server.WithRecovery(),
		server.WithHooks(hooks),
	}

	// Discover capabilities from all downstream clients using temporary probe connections
	discovered, err := g.clientManager.DiscoverAllCapabilities(ctx)
	if err != nil {
		g.logger.Warn(ctx, "Failed to discover capabilities from some clients", zap.Error(err))
		// Continue - we'll work with what we discovered
	}

	// Determine capabilities based on what was discovered
	hasTools := false
	hasPrompts := false
	hasResources := false
	hasSubscribe := false
	hasListChanged := true // Support dynamic updates

	// Check native tools
	if g.config.NativeTools.Enabled {
		nativeTools := g.nativeToolsHandler.GetTools()
		if len(nativeTools) > 0 {
			hasTools = true
		}
	}

	// Check discovered capabilities
	if discovered != nil {
		for _, tools := range discovered.Tools {
			if len(tools) > 0 {
				hasTools = true
				break
			}
		}
		for _, prompts := range discovered.Prompts {
			if len(prompts) > 0 {
				hasPrompts = true
				break
			}
		}
		for _, resources := range discovered.Resources {
			if len(resources) > 0 {
				hasResources = true
				break
			}
		}
	}

	// Add capabilities
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

	// Register native gateway tools
	if g.config.NativeTools.Enabled {
		nativeTools := g.nativeToolsHandler.GetTools()
		for _, tool := range nativeTools {
			g.server.AddTool(tool, g.handleToolCallWithErrorHandling)
			g.logger.Info(ctx, "Registered native tool", zap.String("name", tool.Name))
		}
	}

	// Register discovered tools/prompts/resources from downstream clients
	if discovered != nil {
		g.registerDiscoveredCapabilities(ctx, discovered)
	}

	return srv, nil
}

// registerDiscoveredCapabilities registers all discovered tools, prompts, and resources
func (g *Gateway) registerDiscoveredCapabilities(ctx context.Context, discovered *DiscoveredCapabilities) {
	// Register tools
	for clientName, tools := range discovered.Tools {
		for _, tool := range tools {
			prefixedTool := tool
			prefixedTool.Name = PrefixName(clientName, tool.Name)
			g.server.AddTool(prefixedTool, g.handleToolCallWithErrorHandling)
			g.logger.Debug(ctx, "Registered tool",
				zap.String("client", clientName),
				zap.String("original_name", tool.Name),
				zap.String("prefixed_name", prefixedTool.Name))
		}
	}

	// Register prompts
	for clientName, prompts := range discovered.Prompts {
		for _, prompt := range prompts {
			prefixedPrompt := prompt
			prefixedPrompt.Name = PrefixName(clientName, prompt.Name)
			g.server.AddPrompt(prefixedPrompt, g.HandlePromptCall)
			g.logger.Debug(ctx, "Registered prompt",
				zap.String("client", clientName),
				zap.String("original_name", prompt.Name),
				zap.String("prefixed_name", prefixedPrompt.Name))
		}
	}

	// Register resources
	for clientName, resources := range discovered.Resources {
		for _, resource := range resources {
			prefixedResource := resource
			prefixedResource.URI = PrefixName(clientName, resource.URI)
			g.server.AddResource(prefixedResource, g.HandleResourceCall)
			g.logger.Debug(ctx, "Registered resource",
				zap.String("client", clientName),
				zap.String("original_uri", resource.URI),
				zap.String("prefixed_uri", prefixedResource.URI))
		}
	}

	// Register resource templates
	for clientName, templates := range discovered.Templates {
		for _, template := range templates {
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
// for tracking capabilities per session and extracts the client IP address.
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

	// Extract and store client IP address
	clientIP := extractClientIP(r)
	ctx = WithClientIP(ctx, clientIP)

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

// extractClientIP extracts the client IP address from an HTTP request.
// It checks X-Forwarded-For and X-Real-IP headers first (for proxied requests),
// then falls back to RemoteAddr.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (may contain multiple IPs, take the first)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can be a comma-separated list: client, proxy1, proxy2
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr (host:port format)
	// Remove the port if present
	addr := r.RemoteAddr
	if colonIdx := strings.LastIndex(addr, ":"); colonIdx != -1 {
		// Check if this is an IPv6 address with brackets
		if bracketIdx := strings.LastIndex(addr, "]"); bracketIdx != -1 && bracketIdx > colonIdx {
			// IPv6 with port: [::1]:8080 -> [::1]
			return addr[:colonIdx]
		}
		// IPv4 or IPv6 without brackets
		return addr[:colonIdx]
	}

	return addr
}
