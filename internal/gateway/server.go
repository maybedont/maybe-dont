package gateway

import (
	"context"
	"encoding/json"
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

	"github.com/maybedont/maybe-dont/internal/config"
)

// zapLogWriter is a writer that adapts log output to zap logger
type zapLogWriter struct {
	logger *zap.Logger
}

// Write implements io.Writer, logging each write as a zap error-level entry.
// Used to route the MCP stdio server's *log.Logger error output through zap.
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

// onSessionRegister handles new upstream client sessions.
//
// Creates the session in SessionManager and stores client metadata (IP, User-Agent)
// immediately. Tool discovery is deferred until the client calls tools/list.
//
// If the session already exists (unexpected — the SDK should generate unique IDs),
// we log a warning and refresh metadata on the existing session rather than failing
// the registration, since this callback has no error return.
//
// Note: The SDK may fire this callback for transient GET connections that never lead
// to tool discovery. These "phantom" sessions are lightweight and cleaned up by idle
// timeout. They are filtered from list_sessions output by default.
func (g *Gateway) onSessionRegister(ctx context.Context, session server.ClientSession) {
	sessionID := session.SessionID()

	clientIP, hasClientIP := GetClientIP(ctx)
	userAgent, hasUserAgent := GetUserAgent(ctx)
	caller, hasCaller := GetCaller(ctx)
	hasCredentials := g.hasPassThroughCredentials(ctx)

	// Build log fields
	logFields := []zap.Field{
		zap.String("session_id", sessionID),
		zap.Bool("has_credentials", hasCredentials),
	}
	if hasCaller && caller != "" {
		logFields = append(logFields, zap.String("caller", caller))
	}

	g.logger.Info(ctx, "New upstream session registered", logFields...)

	s, err := g.clientManager.sessionManager.CreateSession(sessionID)
	if err != nil {
		// Session already exists. This is unexpected — the SDK should generate unique IDs.
		// Log a warning and refresh metadata on the existing session.
		g.logger.Warn(ctx, "Session already exists during registration, refreshing metadata",
			zap.String("session_id", sessionID),
			zap.Error(err))

		s, _ = g.clientManager.sessionManager.GetSession(sessionID)
		if s == nil {
			// Shouldn't happen — CreateSession said it exists but GetSession can't find it
			g.logger.Error(ctx, "Session reported as existing but not found",
				zap.String("session_id", sessionID))
			return
		}
	}

	// Store client metadata from the request context
	if hasClientIP && clientIP != "" {
		s.SetClientIP(clientIP)
	}
	if hasUserAgent && userAgent != "" {
		s.SetUserAgent(userAgent)
	}

	// Mark session as connected (has an active SSE connection)
	s.SetConnected(true)
}

// onSessionUnregister handles upstream client session cleanup.
//
// For Streamable HTTP transport, the SDK may unregister sessions when GET (listening)
// connections close, even though the session is still valid for POST requests.
// There is also a race condition in the SDK where a GET connection arriving immediately
// after initialize can arm cleanup defers that destroy the session prematurely.
//
// To avoid losing downstream client connections due to transient SDK unregister events,
// we intentionally do NOT close downstream clients here. The idle session timeout
// (SessionManager.cleanupExpiredSessions) handles resource cleanup when sessions are
// truly abandoned.
func (g *Gateway) onSessionUnregister(ctx context.Context, session server.ClientSession) {
	sessionID := session.SessionID()

	// Mark session as disconnected (SSE connection closed)
	s, ok := g.clientManager.sessionManager.GetSession(sessionID)
	if ok {
		s.SetConnected(false)
	}

	// Log session state for diagnostics
	clientNames := g.clientManager.GetSessionClientNames(sessionID)
	g.logger.Info(ctx, "Upstream session unregistered (downstream clients preserved for reconnection)",
		zap.String("session_id", sessionID),
		zap.Int("downstream_client_count", len(clientNames)),
		zap.Strings("downstream_clients", clientNames))
}

// jsonRPCRequest is used to parse just the method and declared protocol
// version from a JSON-RPC request
type jsonRPCRequest struct {
	Method string `json:"method"`
	Params struct {
		Meta map[string]any `json:"_meta"`
	} `json:"params"`
}

// declaredProtocolVersion returns the protocol version a request declares in
// its _meta, which only stateless (2026-07-28 and later) requests carry.
func (r jsonRPCRequest) declaredProtocolVersion() string {
	version, _ := r.Params.Meta[mcp.MetaKeyProtocolVersion].(string)
	return version
}

// errStatelessProtocolUnsupported rejects requests on the stateless MCP
// protocol (2026-07-28 and later). The gateway keeps per-session downstream
// connections and pass-through credentials, which that protocol has no
// session to hang them on. Rejecting server/discover makes clients fall back
// to the initialize handshake, the same as with a server that predates it.
var errStatelessProtocolUnsupported = fmt.Errorf(
	"MCP protocol %s and later is not supported by this gateway; connect with initialize using protocol version %s or earlier",
	mcp.ProtocolVersion20260728, mcp.LATEST_LEGACY_PROTOCOL_VERSION)

// isStatelessProtocolRequest reports whether a request uses the stateless
// protocol, from either its _meta or its Mcp-Protocol-Version header.
func isStatelessProtocolRequest(ctx context.Context, req jsonRPCRequest) bool {
	if req.Method == string(mcp.MethodServerDiscover) || mcp.IsModernProtocol(req.declaredProtocolVersion()) {
		return true
	}
	if headers, ok := GetRawRequestHeaders(ctx); ok {
		// Only a known version counts: IsModernProtocol compares strings, so
		// an unrecognized value like "latest" would sort as modern.
		version := headers.Get(server.HeaderKeyProtocolVersion)
		return mcp.IsValidProtocolVersion(version) && mcp.IsModernProtocol(version)
	}
	return false
}

// downstreamRequestParams is used to parse just the prefixed tool or prompt
// name, or resource URI, from a request addressed to a downstream server
type downstreamRequestParams struct {
	Params struct {
		Name string `json:"name"`
		URI  string `json:"uri"`
	} `json:"params"`
}

// downstreamTarget returns the prefixed name a downstream-bound request is
// addressed to: the tool name for tools/call, the prompt name for
// prompts/get and the resource URI for resources/read. ok is false for every
// other method.
func downstreamTarget(method string, msg []byte) (target string, ok bool) {
	switch mcp.MCPMethod(method) {
	case mcp.MethodToolsCall, mcp.MethodPromptsGet, mcp.MethodResourcesRead:
	default:
		return "", false
	}
	var params downstreamRequestParams
	if err := json.Unmarshal(msg, &params); err != nil {
		return "", false
	}
	if mcp.MCPMethod(method) == mcp.MethodResourcesRead {
		return params.Params.URI, params.Params.URI != ""
	}
	return params.Params.Name, params.Params.Name != ""
}

// onRequestInitialization is called before each request is processed.
// It detects stale sessions where the tool being called belongs to a downstream
// MCP server (based on the client prefix pattern), but the session doesn't exist
// in our SessionManager (e.g., after server restart).
//
// In this case, we return an error instructing the AI agent to call discover_tools
// to re-establish their session and refresh their tool list.
func (g *Gateway) onRequestInitialization(ctx context.Context, id any, message any) error {
	// Parse the raw JSON to get the method.
	// The message is passed as json.RawMessage (which is a named type for []byte).
	// We need to handle both json.RawMessage and []byte type assertions.
	var msgBytes []byte
	switch m := message.(type) {
	case json.RawMessage:
		msgBytes = m
	case []byte:
		msgBytes = m
	default:
		// Not raw bytes, can't parse - let it through
		return nil
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(msgBytes, &req); err != nil {
		// Can't parse method, let the normal handler deal with it
		return nil
	}

	// Log all JSON-RPC methods for request flow visibility
	g.logger.Debug(ctx, "Processing JSON-RPC request",
		zap.String("method", req.Method))

	if isStatelessProtocolRequest(ctx, req) {
		g.logger.Debug(ctx, "Rejecting stateless MCP protocol request",
			zap.String("method", req.Method),
			zap.String("protocol_version", req.declaredProtocolVersion()))
		return errStatelessProtocolUnsupported
	}

	// Only requests addressed to a downstream tool, prompt or resource need a
	// session and a downstream connection
	toolName, ok := downstreamTarget(req.Method, msgBytes)
	if !ok {
		// Unparseable or not downstream-bound, let the normal handler deal with it
		return nil
	}

	// Check if the name has a prefix that matches a configured downstream client
	clientName, _, err := ParsePrefixedName(toolName)
	if err != nil {
		// Not a prefixed name (e.g., native tool like "maybedont__discover_tools")
		// Let the normal handler deal with it
		return nil
	}

	// Check if this client prefix corresponds to a configured downstream client
	if !g.clientManager.IsClientConfigured(clientName) {
		// Not a known client prefix, let the normal handler deal with it
		// This could be a native tool or just an unknown tool
		return nil
	}

	// The tool belongs to a configured downstream client.
	// Now check if the session exists in our SessionManager.
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		// No session in context - shouldn't happen, but let it through
		return nil
	}

	sessionID := session.SessionID()

	// Check if we have this session in our SessionManager
	if g.clientManager.HasSession(sessionID) {
		// Session exists. A pass-through client has to be connected here,
		// before the SDK looks the tool up, because its tools are only
		// registered once some session has discovered them. Other clients
		// are connected by the handlers, after policy validation.
		return g.ensurePassThroughClient(ctx, sessionID, clientName)
	}

	// Session doesn't exist in our SessionManager, but the tool belongs to a
	// configured downstream client. This means the AI agent is using a stale
	// session (e.g., from before a server restart).
	g.logger.Debug(ctx, "Stale session detected for downstream tool call",
		zap.String("session_id", sessionID),
		zap.String("tool_name", toolName),
		zap.String("client_prefix", clientName))

	return &SessionExpiredError{
		SessionID: sessionID,
		Reason:    fmt.Sprintf("session not found for tool '%s' from downstream server '%s'", toolName, clientName),
	}
}

// ensurePassThroughToolsDiscovered ensures that pass-through tools are available
// for this session. It checks for existing downstream clients and returns their tools,
// or performs synchronous discovery if no clients exist yet.
// Uses singleflight to deduplicate concurrent discovery requests for the same session.
// Returns the list of tools (which may be empty if no pass-through clients).
func (g *Gateway) ensurePassThroughToolsDiscovered(ctx context.Context, sessionID string) []mcp.Tool {
	// Fast path: the session's pass-through clients are already connected.
	// Only pass-through clients count; a client connected on demand by a
	// tool call must not stop pass-through discovery.
	existingClients := g.connectedPassThroughClients(sessionID)
	if len(existingClients) > 0 {
		// Downstream clients exist - return their tools directly.
		// The tools may not be registered as session tools (due to race conditions),
		// so we return them here to be merged into the tools/list response.
		g.logger.Debug(ctx, "Session has downstream clients, returning their tools",
			zap.String("session_id", sessionID),
			zap.Strings("clients", existingClients))
		return g.getToolsFromExistingClients(ctx, sessionID)
	}

	// Check if pass-through credentials are available in raw headers
	if !g.hasPassThroughCredentials(ctx) {
		g.logger.Debug(ctx, "No pass-through credentials in context for lazy discovery",
			zap.String("session_id", sessionID))
		return nil
	}

	// Use singleflight to deduplicate concurrent discovery requests for the same session.
	// This prevents multiple concurrent tools/list requests from all triggering discovery.
	result, err, shared := g.lazyDiscoveryGroup.Do(sessionID, func() (interface{}, error) {
		// Double-check: another request might have completed discovery while we waited
		existingClients := g.connectedPassThroughClients(sessionID)
		if len(existingClients) > 0 {
			g.logger.Debug(ctx, "Discovery already completed by another request",
				zap.String("session_id", sessionID),
				zap.Strings("clients", existingClients))
			return g.getToolsFromExistingClients(ctx, sessionID), nil
		}

		g.logger.Info(ctx, "Performing lazy tool discovery for session",
			zap.String("session_id", sessionID))

		// Perform synchronous discovery for this session
		discoveryResult, err := g.clientManager.CreateSessionClients(ctx, sessionID)
		if err != nil {
			return nil, err
		}

		if discoveryResult == nil || len(discoveryResult.DownstreamClients) == 0 {
			g.logger.Debug(ctx, "No downstream clients discovered",
				zap.String("session_id", sessionID))
			return []mcp.Tool{}, nil
		}

		// Collect all discovered tools and register them as session tools
		var allTools []mcp.Tool
		for clientName, clientInfo := range discoveryResult.DownstreamClients {
			// Only process pass-through clients
			if !clientInfo.Config.Auth.PassThrough.Enabled {
				continue
			}

			for _, tool := range clientInfo.Tools {
				prefixedTool := tool
				prefixedTool.Name = PrefixName(clientName, tool.Name)
				prefixedTool.DeferLoading = true

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

		return allTools, nil
	})

	if shared {
		g.logger.Debug(ctx, "Lazy discovery result shared from concurrent request",
			zap.String("session_id", sessionID))
	}

	if err != nil {
		g.logger.Error(ctx, "Failed lazy tool discovery",
			zap.String("session_id", sessionID),
			zap.Error(err))
		return nil
	}

	if result == nil {
		return nil
	}
	return result.([]mcp.Tool)
}

// connectedPassThroughClients returns the names of the session's connected
// downstream clients that use pass-through auth.
func (g *Gateway) connectedPassThroughClients(sessionID string) []string {
	configs := g.clientManager.GetClientConfigs()
	var names []string
	for _, name := range g.clientManager.GetSessionClientNames(sessionID) {
		if configs[name].Auth.PassThrough.Enabled {
			names = append(names, name)
		}
	}
	return names
}

// getToolsFromExistingClients retrieves tools from downstream clients that are already
// connected for this session. This handles subsequent tools/list calls after the
// initial lazy discovery has already connected the downstream clients.
func (g *Gateway) getToolsFromExistingClients(ctx context.Context, sessionID string) []mcp.Tool {
	clients, err := g.clientManager.GetAllSessionClients(sessionID)
	if err != nil {
		g.logger.Debug(ctx, "Failed to get session clients",
			zap.String("session_id", sessionID),
			zap.Error(err))
		return nil
	}

	var allTools []mcp.Tool
	for clientName, clientInfo := range clients {
		// Only process pass-through clients
		if !clientInfo.Config.Auth.PassThrough.Enabled {
			continue
		}

		for _, tool := range clientInfo.Tools {
			prefixedTool := tool
			prefixedTool.Name = PrefixName(clientName, tool.Name)
			prefixedTool.DeferLoading = true
			allTools = append(allTools, prefixedTool)
		}
	}

	if len(allTools) > 0 {
		g.logger.Debug(ctx, "Retrieved tools from existing downstream clients",
			zap.String("session_id", sessionID),
			zap.Int("tool_count", len(allTools)))
	}

	return allTools
}

// initMCPServer initializes the MCP server with common configuration and registers tools
func (g *Gateway) initMCPServer() (*server.MCPServer, error) {
	ctx := context.Background()

	// Create hooks for session lifecycle management
	hooks := &server.Hooks{}
	hooks.AddOnRegisterSession(g.onSessionRegister)
	hooks.AddOnUnregisterSession(g.onSessionUnregister)

	// Add request initialization hook to detect stale sessions.
	// This hook runs before the SDK looks up the tool in its registry.
	// If the session doesn't exist in our SessionManager but the tool name
	// matches a configured downstream client prefix, we return an error
	// instructing the AI agent to re-establish their session.
	hooks.AddOnRequestInitialization(g.onRequestInitialization)

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

		// Merge discovered tools with the existing tools, preserving order
		if len(discoveredTools) > 0 {
			// Track existing tool names to avoid duplicates
			existingNames := make(map[string]bool)
			for _, tool := range tools {
				existingNames[tool.Name] = true
			}
			// Append only the tools that don't already exist
			for _, tool := range discoveredTools {
				if !existingNames[tool.Name] {
					tools = append(tools, tool)
				}
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

		g.logger.Debug(ctx, "tools/list response",
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
	nativeToolNames := make([]string, 0, len(nativeTools))
	for _, tool := range nativeTools {
		g.server.AddTool(tool, g.handleToolCallWithErrorHandling)
		nativeToolNames = append(nativeToolNames, tool.Name)
	}
	if len(nativeToolNames) > 0 {
		g.logger.Info(ctx, "Registered native tools", zap.Strings("names", nativeToolNames))
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
		toolNames := make([]string, 0, len(tools))
		for _, tool := range tools {
			prefixedTool := tool
			prefixedTool.Name = PrefixName(clientName, tool.Name)
			prefixedTool.DeferLoading = true
			g.server.AddTool(prefixedTool, g.handleToolCallWithErrorHandling)
			toolNames = append(toolNames, prefixedTool.Name)
		}
		if len(toolNames) > 0 {
			g.logger.Debug(ctx, "Registered tools",
				zap.String("client", clientName),
				zap.Strings("tools", toolNames))
		}
	}

	// Register prompts
	for clientName, prompts := range discovered.Prompts {
		promptNames := make([]string, 0, len(prompts))
		for _, prompt := range prompts {
			prefixedPrompt := prompt
			prefixedPrompt.Name = PrefixName(clientName, prompt.Name)
			g.server.AddPrompt(prefixedPrompt, g.HandlePromptCall)
			promptNames = append(promptNames, prefixedPrompt.Name)
		}
		if len(promptNames) > 0 {
			g.logger.Debug(ctx, "Registered prompts",
				zap.String("client", clientName),
				zap.Strings("prompts", promptNames))
		}
	}

	// Register resources
	for clientName, resources := range discovered.Resources {
		resourceURIs := make([]string, 0, len(resources))
		for _, resource := range resources {
			prefixedResource := resource
			prefixedResource.URI = PrefixName(clientName, resource.URI)
			g.server.AddResource(prefixedResource, g.HandleResourceCall)
			resourceURIs = append(resourceURIs, prefixedResource.URI)
		}
		if len(resourceURIs) > 0 {
			g.logger.Debug(ctx, "Registered resources",
				zap.String("client", clientName),
				zap.Strings("resources", resourceURIs))
		}
	}

	// Register resource templates
	for clientName, templates := range discovered.Templates {
		templateURIs := make([]string, 0, len(templates))
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
			if prefixedTemplate.URITemplate != nil {
				templateURIs = append(templateURIs, prefixedTemplate.URITemplate.Raw())
			}
		}
		if len(templateURIs) > 0 {
			g.logger.Debug(ctx, "Registered resource templates",
				zap.String("client", clientName),
				zap.Strings("templates", templateURIs))
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

	// Create SSE server — context enrichment is handled by mcpContextMiddleware below
	sseSrv := server.NewSSEServer(srv,
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
	)

	g.server = srv

	// Log if auth is enabled
	if g.callerAuthConfig != nil && g.callerAuthConfig.Enabled {
		g.logger.Info(ctx, "Required header authentication enabled",
			zap.String("header", g.callerAuthConfig.HeaderName),
			zap.Strings("allowed_values", g.callerAuthConfig.OriginalValues()))
	}

	// Create mux and mount both SSE endpoints, wrapped with context enrichment middleware.
	// This ensures both SSE and message handlers get enriched context.
	mux := http.NewServeMux()
	mux.Handle("/sse", g.mcpContextMiddleware(sseSrv.SSEHandler()))
	mux.Handle("/message", g.mcpContextMiddleware(sseSrv.MessageHandler()))

	// Add helpful 404 for HTTP endpoint when SSE transport is active
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found: HTTP transport not enabled. Server configured for SSE transport.", http.StatusNotFound)
	})

	// Create shared policy evaluator
	evaluator := &PolicyEvaluator{
		CELEngine:           g.policyEngine,
		AIEngine:            g.aiPolicyEngine,
		ResponseChain:       g.responseValidationChain,
		MaxBlockingMs:       g.config.Validation.MaxBlockingMs,
		MaxRuleEvaluationMs: g.config.Validation.MaxRuleEvaluationMs,
		Logger:              g.logger,
	}

	// Register CLI validation endpoint
	cliHandler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:               g.config.CLIRequestValidation.Enabled,
		ValidateCommands:      g.config.CLIRequestValidation.ValidateCommands,
		Logger:                g.logger,
		Version:               g.version,
		AuditWriter:           g.auditWriter,
		Evaluator:             evaluator,
		IncludeArgumentValues: g.config.Audit.ShouldIncludeArgumentValues(),
	})
	mux.Handle("/api/v1/cli/validate", cliHandler)
	if g.config.CLIRequestValidation.Enabled {
		g.logger.Info(ctx, "CLI validation endpoint enabled",
			zap.Int("validate_commands_count", len(g.config.CLIRequestValidation.ValidateCommands)),
		)
	}

	// Register action validation endpoint
	actionHandler := NewActionValidationHandler(ActionValidationHandlerConfig{
		Logger:                g.logger,
		Version:               g.version,
		AuditWriter:           g.auditWriter,
		Evaluator:             evaluator,
		IncludeArgumentValues: g.config.Audit.ShouldIncludeArgumentValues(),
	})
	mux.Handle("/api/v1/action/validate", actionHandler)
	g.logger.Info(ctx, "Action validation endpoint registered")

	// Register intercept endpoint
	interceptHandler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:               g.config.Intercept.Enabled,
		ShellToolNames:        g.config.Intercept.ShellToolNames,
		Logger:                g.logger,
		Version:               g.version,
		AuditWriter:           g.auditWriter,
		Evaluator:             evaluator,
		IncludeArgumentValues: g.config.Audit.ShouldIncludeArgumentValues(),
	})
	mux.Handle("/api/v1/intercept", interceptHandler)
	if g.config.Intercept.Enabled {
		g.logger.Info(ctx, "Intercept endpoint enabled",
			zap.Int("shell_tool_names_count", len(g.config.Intercept.ShellToolNames)),
		)
	}

	// Wrap with auth middleware
	handler := AuthMiddleware(g.callerAuthConfig, mux)

	// Create custom HTTP server
	httpServer := &http.Server{
		Addr:    g.config.Server.ListenAddr,
		Handler: handler,
	}

	// Create error channel for startup confirmation
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		defer close(errChan)
		if err := httpServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				// Expected during graceful shutdown
				return
			}
			g.logger.Error(context.Background(), "SSE server failed", zap.Error(err))
			errChan <- err
		}
	}()

	// Monitor context for cancellation
	go func() {
		<-ctx.Done()
		// Use timeout context for shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
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

	// Create streamable HTTP server as an http.Handler
	// Note: We set the endpoint path but will mount it ourselves on a mux
	mcpHandler := server.NewStreamableHTTPServer(srv,
		server.WithEndpointPath("/mcp"),
	)

	g.server = srv

	// Log if auth is enabled
	if g.callerAuthConfig != nil && g.callerAuthConfig.Enabled {
		g.logger.Info(ctx, "Required header authentication enabled",
			zap.String("header", g.callerAuthConfig.HeaderName),
			zap.Strings("allowed_values", g.callerAuthConfig.OriginalValues()))
	}

	// Create mux and mount the MCP handler, wrapped with context enrichment middleware.
	// This ensures both GET (SSE listen) and POST (JSON-RPC) requests get enriched context,
	// which is needed because the SDK's WithHTTPContextFunc only runs on POST, not GET.
	mux := http.NewServeMux()
	mux.Handle("/mcp", g.mcpContextMiddleware(mcpHandler))

	// Add helpful 404 for SSE endpoint when HTTP transport is active
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found: SSE transport not enabled. Server configured for HTTP transport.", http.StatusNotFound)
	})

	// Create shared policy evaluator
	evaluator := &PolicyEvaluator{
		CELEngine:           g.policyEngine,
		AIEngine:            g.aiPolicyEngine,
		ResponseChain:       g.responseValidationChain,
		MaxBlockingMs:       g.config.Validation.MaxBlockingMs,
		MaxRuleEvaluationMs: g.config.Validation.MaxRuleEvaluationMs,
		Logger:              g.logger,
	}

	// Register CLI validation endpoint
	cliHandler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:               g.config.CLIRequestValidation.Enabled,
		ValidateCommands:      g.config.CLIRequestValidation.ValidateCommands,
		Logger:                g.logger,
		Version:               g.version,
		AuditWriter:           g.auditWriter,
		Evaluator:             evaluator,
		IncludeArgumentValues: g.config.Audit.ShouldIncludeArgumentValues(),
	})
	mux.Handle("/api/v1/cli/validate", cliHandler)
	if g.config.CLIRequestValidation.Enabled {
		g.logger.Info(ctx, "CLI validation endpoint enabled",
			zap.Int("validate_commands_count", len(g.config.CLIRequestValidation.ValidateCommands)),
		)
	}

	// Register action validation endpoint
	actionHandler := NewActionValidationHandler(ActionValidationHandlerConfig{
		Logger:                g.logger,
		Version:               g.version,
		AuditWriter:           g.auditWriter,
		Evaluator:             evaluator,
		IncludeArgumentValues: g.config.Audit.ShouldIncludeArgumentValues(),
	})
	mux.Handle("/api/v1/action/validate", actionHandler)
	g.logger.Info(ctx, "Action validation endpoint registered")

	// Register intercept endpoint
	interceptHandler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:               g.config.Intercept.Enabled,
		ShellToolNames:        g.config.Intercept.ShellToolNames,
		Logger:                g.logger,
		Version:               g.version,
		AuditWriter:           g.auditWriter,
		Evaluator:             evaluator,
		IncludeArgumentValues: g.config.Audit.ShouldIncludeArgumentValues(),
	})
	mux.Handle("/api/v1/intercept", interceptHandler)
	if g.config.Intercept.Enabled {
		g.logger.Info(ctx, "Intercept endpoint enabled",
			zap.Int("shell_tool_names_count", len(g.config.Intercept.ShellToolNames)),
		)
	}

	// Wrap with auth middleware
	handler := AuthMiddleware(g.callerAuthConfig, mux)

	// Create custom HTTP server
	httpServer := &http.Server{
		Addr:    g.config.Server.ListenAddr,
		Handler: handler,
	}

	// Create error channel for startup confirmation
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		defer close(errChan)
		if err := httpServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				// Expected during graceful shutdown
				return
			}
			g.logger.Error(context.Background(), "HTTP server failed", zap.Error(err))
			errChan <- err
		}
	}()

	// Monitor context for cancellation
	go func() {
		<-ctx.Done()
		// Use timeout context for shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
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

// mcpContextMiddleware wraps an HTTP handler with context enrichment.
// This replaces the SDK's WithHTTPContextFunc/WithSSEContextFunc callbacks, which
// only run on POST requests. By using middleware, both GET (SSE listen) and POST
// (JSON-RPC) requests get enriched context, ensuring onSessionRegister has access
// to client IP, User-Agent, and other request metadata.
func (g *Gateway) mcpContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := g.extractAuthFromRequest(r.Context(), r)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractAuthFromRequest prepares the context with request metadata needed for downstream calls.
// It generates a request ID for tracking, extracts the client IP address, parses the JSON-RPC
// method, and stores raw request headers for lazy credential extraction when needed.
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

	// Store request ID in context first so it can be used in logging
	ctx = WithRequestID(ctx, requestID)

	// Extract and store client IP address using trusted proxy configuration
	clientIP := g.trustedProxyChecker.ExtractClientIP(
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-IP"),
		r.RemoteAddr,
	)
	ctx = WithClientIP(ctx, clientIP)

	// Extract and store User-Agent header
	userAgent := r.Header.Get("User-Agent")
	ctx = WithUserAgent(ctx, userAgent)

	if r.Header.Get("X-Request-ID") == "" {
		g.logger.Debug(ctx, "Generated new request ID")
	} else {
		g.logger.Debug(ctx, "Using existing request ID")
	}

	// Store raw request headers in context for lazy credential extraction.
	// Credentials will be extracted on-demand when making downstream requests,
	// avoiding unnecessary extraction for native tool calls.
	ctx = WithRawRequestHeaders(ctx, r.Header)

	// Extract caller identifier if auth is enabled.
	// The middleware already validated the header value; we just store it in context.
	ctx = extractCallerFromRequest(ctx, r, g.callerAuthConfig)

	return ctx
}

// hasPassThroughCredentials checks if any pass-through source headers are present
// in the raw request headers. This is used to determine if lazy discovery should
// be attempted without eagerly extracting all credentials.
func (g *Gateway) hasPassThroughCredentials(ctx context.Context) bool {
	for _, clientConfig := range g.config.DownstreamMCPServers {
		if hasPassThroughCredentialsFor(ctx, clientConfig) {
			return true
		}
	}
	return false
}

// hasPassThroughCredentialsFor reports whether the request carries at least
// one of the source headers of a pass-through client.
func hasPassThroughCredentialsFor(ctx context.Context, cfg config.ClientConfig) bool {
	if !cfg.Auth.PassThrough.Enabled {
		return false
	}
	rawHeaders, ok := GetRawRequestHeaders(ctx)
	if !ok {
		return false
	}
	for _, mapping := range cfg.Auth.PassThrough.Headers {
		if rawHeaders.Get(mapping.SourceHeader) != "" {
			return true
		}
	}
	return false
}

// ensurePassThroughClient connects a pass-through client for the session when
// the request carries its credentials and it isn't connected yet. Without
// credentials the request proceeds and the handler returns the error telling
// the caller to send them and call maybedont__discover_tools.
func (g *Gateway) ensurePassThroughClient(ctx context.Context, sessionID, clientName string) error {
	cfg, ok := g.clientManager.GetClientConfigs()[clientName]
	if !ok || !cfg.Auth.PassThrough.Enabled || !hasPassThroughCredentialsFor(ctx, cfg) {
		return nil
	}
	if _, err := g.clientManager.GetSessionClient(sessionID, clientName); err == nil {
		return nil
	}

	// Also registers the client's tools, which the SDK needs to route the
	// call when no session has discovered them yet.
	result, err := g.DiscoverPassThroughTools(ctx, sessionID, clientName)
	if err != nil {
		g.logger.Warn(ctx, "Failed to connect pass-through client for session",
			zap.String("session_id", sessionID),
			zap.String("client", clientName),
			zap.Error(err))
		return errDownstreamConnectFailed(clientName)
	}
	if len(result.Errors) > 0 {
		// Discovery already logged the raw error and made this message safe
		// for the caller with safeDownstreamConnectError.
		g.logger.Warn(ctx, "Failed to connect pass-through client for session",
			zap.String("session_id", sessionID),
			zap.String("client", clientName),
			zap.String("error", result.Errors[0].Error))
		return errors.New(result.Errors[0].Error)
	}
	return nil
}

// getOrConnectSessionClient returns the session's client for a downstream
// server, connecting it on first use. Handlers call it after policy
// validation, so a denied or invalid request never spawns a process or opens
// a connection. Pass-through clients are connected earlier, by
// ensurePassThroughClient, when credentials are present; one that still
// isn't connected returns the guidance error from GetSessionClient.
func (g *Gateway) getOrConnectSessionClient(ctx context.Context, sessionID, clientName string) (*SessionClientInfo, error) {
	clientInfo, err := g.clientManager.GetSessionClient(sessionID, clientName)
	if err == nil || IsSessionExpiredError(err) {
		return clientInfo, err
	}

	cfg, ok := g.clientManager.GetClientConfigs()[clientName]
	if !ok || cfg.Auth.PassThrough.Enabled {
		return nil, err
	}

	connected, connectErr, _ := g.sessionConnectGroup.Do(sessionID+"/"+clientName, func() (interface{}, error) {
		return g.clientManager.CreateSingleSessionClient(ctx, sessionID, clientName, cfg)
	})
	if connectErr != nil {
		g.logger.Warn(ctx, "Failed to connect downstream client for session",
			zap.String("session_id", sessionID),
			zap.String("client", clientName),
			zap.Error(connectErr))
		return nil, safeDownstreamConnectError(clientName, cfg, connectErr)
	}
	return connected.(*SessionClientInfo), nil
}
