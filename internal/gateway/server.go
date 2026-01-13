package gateway

import (
	"context"
	"errors"
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

// onSessionRegister handles new upstream client sessions by creating downstream clients.
// Tool discovery and registration is performed asynchronously to avoid blocking the
// session registration hook, which can cause race conditions if the client disconnects
// before discovery completes.
func (g *Gateway) onSessionRegister(ctx context.Context, session server.ClientSession) {
	sessionID := session.SessionID()

	// Extract values from context that we need to preserve for async work.
	// The request context (ctx) may be cancelled when the HTTP request ends,
	// so we need to capture these values and create a new context for async work.
	clientIP, hasClientIP := GetClientIP(ctx)
	serviceCreds, _ := ctx.Value(ServiceCredentialsKey).(*ServiceCredentials)

	// Determine if this session has credentials (helps identify initialization vs SSE sessions)
	hasCredentials := serviceCreds != nil && len(serviceCreds.clients) > 0
	g.logger.Info(ctx, "New upstream session registered",
		zap.String("session_id", sessionID),
		zap.Bool("has_credentials", hasCredentials))

	// Store client IP in session immediately (this is fast, no network I/O)
	if hasClientIP && clientIP != "" {
		g.clientManager.SetSessionClientIP(sessionID, clientIP)
		g.logger.Debug(ctx, "Stored client IP for session",
			zap.String("session_id", sessionID),
			zap.String("client_ip", clientIP))
	}

	// Only trigger tool discovery if credentials are present.
	// This ensures we only discover tools for the session that actually has auth credentials,
	// avoiding duplicate discovery for SSE connections that may not have credentials.
	if hasCredentials {
		// Run tool discovery asynchronously to avoid blocking the session registration hook.
		// This prevents race conditions where the session gets unregistered (via defer in
		// the HTTP handler) before tool discovery completes.
		go g.discoverAndRegisterSessionTools(sessionID, clientIP, serviceCreds)
	} else {
		g.logger.Debug(ctx, "Skipping tool discovery - no credentials in context",
			zap.String("session_id", sessionID))
	}
}

// discoverAndRegisterSessionTools performs async discovery of downstream client tools
// and registers them with the session. This runs in a goroutine to avoid blocking
// the session registration hook.
func (g *Gateway) discoverAndRegisterSessionTools(sessionID string, clientIP string, serviceCreds *ServiceCredentials) {
	// Create a background context with the preserved values.
	// We use Background() because the original request context may be cancelled.
	ctx := context.Background()
	if serviceCreds != nil {
		ctx = WithServiceCredentials(ctx, serviceCreds)
	}
	if clientIP != "" {
		ctx = WithClientIP(ctx, clientIP)
	}

	g.logger.Debug(ctx, "Starting async tool discovery for session",
		zap.String("session_id", sessionID))

	// Create downstream clients for this session
	result, err := g.clientManager.CreateSessionClients(ctx, sessionID)
	if err != nil {
		g.logger.Error(ctx, "Failed to create downstream clients for session",
			zap.String("session_id", sessionID),
			zap.Error(err))
		// Continue to register any tools that were successfully discovered
	}

	// Register session-specific tools for pass-through clients
	if result != nil && len(result.DownstreamClients) > 0 {
		g.registerSessionTools(ctx, sessionID, result.DownstreamClients)
	}

	clientCount := 0
	if result != nil {
		clientCount = len(result.DownstreamClients)
	}
	g.logger.Info(ctx, "Async tool discovery completed for session",
		zap.String("session_id", sessionID),
		zap.Int("client_count", clientCount))
}

// registerSessionTools registers tools from pass-through clients as session-specific tools.
// Only pass-through clients need session tools; non-pass-through clients have their
// tools registered globally at startup.
func (g *Gateway) registerSessionTools(ctx context.Context, sessionID string, clients map[string]*SessionClientInfo) {
	for clientName, clientInfo := range clients {
		// Only register session tools for pass-through clients
		// Non-pass-through clients have their tools registered globally at startup
		if !clientInfo.Config.Auth.PassThrough.Enabled {
			g.logger.Debug(ctx, "Skipping session tool registration for non-pass-through client",
				zap.String("session_id", sessionID),
				zap.String("client", clientName))
			continue
		}

		registeredCount := 0
		g.logger.Info(ctx, "Starting session tool registration",
			zap.String("session_id", sessionID),
			zap.String("client", clientName),
			zap.Int("tools_to_register", len(clientInfo.Tools)))

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
			registeredCount++
		}

		if registeredCount != len(clientInfo.Tools) {
			g.logger.Warn(ctx, "Completed session tool registration with errors",
				zap.String("session_id", sessionID),
				zap.String("client", clientName),
				zap.Int("registered", registeredCount),
				zap.Int("total", len(clientInfo.Tools)))
		} else {
			g.logger.Info(ctx, "Completed session tool registration",
				zap.String("session_id", sessionID),
				zap.String("client", clientName),
				zap.Int("registered", registeredCount))
		}
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

// ensurePassThroughToolsDiscovered ensures that pass-through tools have been discovered
// for this session. If not already discovered, it performs synchronous discovery.
// Returns the list of discovered tools (which may be empty if no pass-through clients).
func (g *Gateway) ensurePassThroughToolsDiscovered(ctx context.Context, sessionID string) []mcp.Tool {
	// Check if this session already has downstream clients connected
	existingClients := g.clientManager.GetSessionClientNames(sessionID)
	if len(existingClients) > 0 {
		// Tools should already be registered via async discovery
		g.logger.Debug(ctx, "Session already has downstream clients",
			zap.String("session_id", sessionID),
			zap.Strings("clients", existingClients))
		return nil
	}

	// Get credentials from context for pass-through auth
	serviceCreds, _ := ctx.Value(ServiceCredentialsKey).(*ServiceCredentials)
	if serviceCreds == nil || len(serviceCreds.clients) == 0 {
		g.logger.Debug(ctx, "No credentials in context for lazy discovery",
			zap.String("session_id", sessionID))
		return nil
	}

	g.logger.Info(ctx, "Performing lazy tool discovery for session",
		zap.String("session_id", sessionID))

	// Perform synchronous discovery for this session
	result, err := g.clientManager.CreateSessionClients(ctx, sessionID)
	if err != nil {
		g.logger.Error(ctx, "Failed lazy tool discovery",
			zap.String("session_id", sessionID),
			zap.Error(err))
		return nil
	}

	if result == nil || len(result.DownstreamClients) == 0 {
		g.logger.Debug(ctx, "No downstream clients discovered",
			zap.String("session_id", sessionID))
		return nil
	}

	// Collect all discovered tools and register them as session tools
	var allTools []mcp.Tool
	for clientName, clientInfo := range result.DownstreamClients {
		// Only process pass-through clients
		if !clientInfo.Config.Auth.PassThrough.Enabled {
			continue
		}

		for _, tool := range clientInfo.Tools {
			prefixedTool := tool
			prefixedTool.Name = PrefixName(clientName, tool.Name)

			// Register the tool with the session
			if err := g.server.AddSessionTool(sessionID, prefixedTool, g.handleToolCallWithErrorHandling); err != nil {
				g.logger.Warn(ctx, "Failed to register session tool during lazy discovery",
					zap.String("session_id", sessionID),
					zap.String("tool", prefixedTool.Name),
					zap.Error(err))
				// Still add to return list so it appears in tools/list
			}
			allTools = append(allTools, prefixedTool)
		}
	}

	g.logger.Info(ctx, "Lazy tool discovery completed",
		zap.String("session_id", sessionID),
		zap.Int("tools_discovered", len(allTools)))

	return allTools
}

// initMCPServer initializes the MCP server with common configuration and registers tools
func (g *Gateway) initMCPServer() (*server.MCPServer, error) {
	ctx := context.Background()

	// Create hooks for session lifecycle management
	hooks := &server.Hooks{}
	hooks.AddOnRegisterSession(g.onSessionRegister)
	hooks.AddOnUnregisterSession(g.onSessionUnregister)

	// Create a tool filter that performs lazy discovery of pass-through tools.
	// When tools/list is called, if the session doesn't have pass-through tools yet,
	// we discover them synchronously and add them to the result.
	toolListFilter := func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
		session := server.ClientSessionFromContext(ctx)
		if session == nil {
			g.logger.Debug(ctx, "tools/list called with no session context",
				zap.Int("tool_count", len(tools)))
			return tools
		}

		sessionID := session.SessionID()

		// Check if we need to discover pass-through tools for this session
		discoveredTools := g.ensurePassThroughToolsDiscovered(ctx, sessionID)

		// Merge discovered tools with the existing tools
		if len(discoveredTools) > 0 {
			// Create a map to avoid duplicates (session tools should already be in 'tools' if registered)
			toolMap := make(map[string]mcp.Tool)
			for _, tool := range tools {
				toolMap[tool.Name] = tool
			}
			for _, tool := range discoveredTools {
				if _, exists := toolMap[tool.Name]; !exists {
					toolMap[tool.Name] = tool
				}
			}
			// Convert back to slice
			tools = make([]mcp.Tool, 0, len(toolMap))
			for _, tool := range toolMap {
				tools = append(tools, tool)
			}
		}

		// Log the response for debugging
		prefixCounts := make(map[string]int)
		for _, tool := range tools {
			clientName, _, err := ParsePrefixedName(tool.Name)
			if err != nil {
				prefixCounts["native"]++
			} else {
				prefixCounts[clientName]++
			}
		}

		g.logger.Info(ctx, "tools/list response",
			zap.String("session_id", sessionID),
			zap.Int("total_tools", len(tools)),
			zap.Any("by_prefix", prefixCounts))

		return tools
	}

	opts := []server.ServerOption{
		server.WithLogging(),
		server.WithRecovery(),
		server.WithHooks(hooks),
		server.WithToolFilter(toolListFilter),
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

	// Check native tools
	nativeTools := g.nativeToolsHandler.GetTools()
	if len(nativeTools) > 0 {
		hasTools = true
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
		opts = append(opts, server.WithToolCapabilities(true))
	}
	if hasPrompts {
		opts = append(opts, server.WithPromptCapabilities(true))
	}
	if hasResources {
		opts = append(opts, server.WithResourceCapabilities(false, true))
	}

	srv := server.NewMCPServer("maybe-dont", g.version, opts...)
	g.server = srv

	// Register native gateway tools
	for _, tool := range nativeTools {
		g.server.AddTool(tool, g.handleToolCallWithErrorHandling)
		g.logger.Info(ctx, "Registered native tool", zap.String("name", tool.Name))
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
		var policyErr *PolicyDeniedError
		if errors.As(err, &policyErr) {
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

	// Extract and store client IP address using trusted proxy configuration
	clientIP := g.trustedProxyChecker.ExtractClientIP(
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-IP"),
		r.RemoteAddr,
	)
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
