package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/maybedont/maybe-dont/internal/metrics"
	"go.uber.org/zap"
)

// PolicyDeniedError represents a policy denial error with structured data
type PolicyDeniedError struct {
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

// Error implements the error interface
func (e *PolicyDeniedError) Error() string {
	return e.Message
}

// Gateway represents an MCP security gateway instance
type Gateway struct {
	logger           *config.SessionLogger
	auditLogger      *config.SessionLogger
	config           *config.Config
	version          string
	server           *server.MCPServer
	clientManager    *ClientManager
	mu               sync.RWMutex
	stopChan         chan struct{}
	metricsCollector *metrics.Collector
	// Validation chain for request processing
	validationChain *ToolValidationChain
	// CEL policy engine
	policyEngine *CELPolicyEngine
	// AI policy engine
	aiPolicyEngine *AIPolicyEngine
	// Response validation chain
	responseValidationChain *ResponseValidationChain
	// CEL response policy engine
	responsePolicyEngine *CELResponsePolicyEngine
	// AI response policy engine
	aiResponsePolicyEngine *AIResponsePolicyEngine
	// Native tools handler
	nativeToolsHandler *NativeToolsHandler
	// Trusted proxy checker for extracting client IPs
	trustedProxyChecker *TrustedProxyChecker
}

// New creates a new gateway instance.
// logDir: the resolved log directory for audit log file output.
func New(ctx context.Context, cfg *config.Config, logger *config.SessionLogger, version string, logDir string) (*Gateway, error) {
	// Create audit logger with its own configuration
	auditLogger, err := config.GetAuditLogger(cfg, logDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// Initialize policy engines
	var policyEngine *CELPolicyEngine
	var aiPolicyEngine *AIPolicyEngine

	// Mode is already resolved during config loading, use it directly
	celPolicyMode := cfg.PolicyValidation.Mode
	aiPolicyMode := cfg.AIPolicyValidation.Mode

	// Initialize CEL policy engine only if not disabled
	if celPolicyMode != config.PolicyModeDisabled {
		logger.Info(ctx, "Initializing CEL policy engine", zap.String("mode", string(celPolicyMode)))
		policyEngine, err = NewCELPolicyEngine(ctx, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL policy engine: %w", err)
		}

		// Load policies from configuration with the resolved mode
		if err := policyEngine.LoadPolicies(cfg.PolicyValidation.Rules, celPolicyMode); err != nil {
			return nil, fmt.Errorf("failed to load CEL policies: %w", err)
		}
	} else {
		logger.Info(ctx, "CEL policy validation is disabled")
	}

	// Initialize AI policy engine only if not disabled
	if aiPolicyMode != config.PolicyModeDisabled {
		logger.Info(ctx, "Initializing AI policy engine", zap.String("mode", string(aiPolicyMode)))
		aiPolicyEngine = &AIPolicyEngine{
			endpoint: cfg.AIPolicyValidation.Endpoint,
			model:    cfg.AIPolicyValidation.Model,
			apiKey:   cfg.AIPolicyValidation.APIKey,
		}

		// Create AI policy engine
		err = InitAIPolicyEngine(logger, aiPolicyEngine)
		if err != nil {
			return nil, fmt.Errorf("failed to init AI policy engine: %w", err)
		}

		// Load policies from configuration with the resolved mode
		if err := aiPolicyEngine.LoadPolicies(cfg.AIPolicyValidation.Rules, aiPolicyMode); err != nil {
			return nil, fmt.Errorf("failed to load AI policies: %w", err)
		}
	} else {
		logger.Info(ctx, "AI policy validation is disabled")
	}

	// Initialize response validation engines
	var responsePolicyEngine *CELResponsePolicyEngine
	var aiResponsePolicyEngine *AIResponsePolicyEngine

	// Mode is already resolved during config loading, use it directly
	celResponseMode := cfg.ResponseValidation.Mode
	aiResponseMode := cfg.AIResponseValidation.Mode

	// Initialize CEL response policy engine only if not disabled
	if celResponseMode != config.PolicyModeDisabled {
		logger.Info(ctx, "Initializing CEL response policy engine", zap.String("mode", string(celResponseMode)))
		responsePolicyEngine, err = NewCELResponsePolicyEngine(ctx, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL response policy engine: %w", err)
		}

		// Load policies from configuration with the resolved mode
		if err := responsePolicyEngine.LoadPolicies(cfg.ResponseValidation.Rules, celResponseMode); err != nil {
			return nil, fmt.Errorf("failed to load CEL response policies: %w", err)
		}
	} else {
		logger.Info(ctx, "CEL response validation is disabled")
	}

	// Initialize AI response policy engine only if not disabled
	if aiResponseMode != config.PolicyModeDisabled {
		logger.Info(ctx, "Initializing AI response policy engine", zap.String("mode", string(aiResponseMode)))
		aiResponsePolicyEngine = &AIResponsePolicyEngine{
			endpoint: cfg.AIPolicyValidation.Endpoint,
			model:    cfg.AIPolicyValidation.Model,
			apiKey:   cfg.AIPolicyValidation.APIKey,
		}

		// Create AI response policy engine
		err = InitAIResponsePolicyEngine(ctx, logger, aiResponsePolicyEngine)
		if err != nil {
			return nil, fmt.Errorf("failed to init AI response policy engine: %w", err)
		}

		// Load policies from configuration with the resolved mode
		if err := aiResponsePolicyEngine.LoadPolicies(cfg.AIResponseValidation.Rules, aiResponseMode); err != nil {
			return nil, fmt.Errorf("failed to load AI response policies: %w", err)
		}
	} else {
		logger.Info(ctx, "AI response validation is disabled")
	}

	// Create client manager
	clientManager := NewClientManager(ctx, logger)

	// Create native tools handler with the resolved audit log path
	auditLogPath := config.ResolveAuditLogPath(cfg, logDir)
	nativeToolsHandler := NewNativeToolsHandler(cfg, logger, auditLogger, auditLogPath)

	// Wire up the client config provider for native tools
	nativeToolsHandler.SetClientConfigProvider(clientManager)

	// Create trusted proxy checker for client IP extraction
	trustedProxyChecker := NewTrustedProxyChecker(cfg.Server.TrustedProxies)
	if trustedProxyChecker.TrustAll() {
		logger.Debug(ctx, "Trusted proxy checker configured to trust all proxies (default)")
	} else {
		logger.Info(ctx, "Trusted proxy checker configured with specific trusted proxies",
			zap.Int("count", len(cfg.Server.TrustedProxies)))
	}

	return &Gateway{
		logger:                 logger,
		auditLogger:            auditLogger,
		config:                 cfg,
		version:                version,
		stopChan:               make(chan struct{}),
		clientManager:          clientManager,
		policyEngine:           policyEngine,
		aiPolicyEngine:         aiPolicyEngine,
		responsePolicyEngine:   responsePolicyEngine,
		aiResponsePolicyEngine: aiResponsePolicyEngine,
		nativeToolsHandler:     nativeToolsHandler,
		trustedProxyChecker:    trustedProxyChecker,
	}, nil
}

// Start initializes and starts the gateway
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Debug print the loaded config
	if _, err := json.MarshalIndent(g.config, "", "  "); err == nil {
		g.logger.Debug(ctx, "Loaded gateway config")
	} else {
		g.logger.Warn(ctx, "Failed to marshal config for debug print", zap.Error(err))
	}

	// Initialize validation chain with policy handlers
	// Note: Audit logging is now handled centrally in HandleToolCall
	var handlers []ToolValidationHandler

	// Add CEL validation handler if engine was created (mode is enabled or audit_only)
	if g.policyEngine != nil {
		g.logger.Info(ctx, "Adding CEL validation handler to chain")
		handlers = append(handlers, NewToolCELValidationHandler(g.logger, g.policyEngine))
	}

	// Add AI validation handler if engine was created (mode is enabled or audit_only)
	if g.aiPolicyEngine != nil {
		g.logger.Info(ctx, "Adding AI validation handler to chain")
		handlers = append(handlers, NewToolAIValidationHandler(g.logger, g.aiPolicyEngine))
	}

	g.validationChain = NewToolValidationChain(handlers...)

	// Initialize response validation chain
	// Note: Audit logging is now handled centrally in HandleToolCall
	var responseHandlers []ResponseValidationHandler

	// Add CEL response validation handler if engine was created (mode is enabled or audit_only)
	if g.responsePolicyEngine != nil {
		g.logger.Info(ctx, "Adding CEL response validation handler to chain")
		responseHandlers = append(responseHandlers, NewResponseCELValidationHandler(g.logger, g.responsePolicyEngine))
	}

	// Add AI response validation handler if engine was created (mode is enabled or audit_only)
	if g.aiResponsePolicyEngine != nil {
		g.logger.Info(ctx, "Adding AI response validation handler to chain")
		responseHandlers = append(responseHandlers, NewResponseAIValidationHandler(g.logger, g.aiResponsePolicyEngine))
	}

	g.responseValidationChain = NewResponseValidationChain(g.logger, responseHandlers...)

	// Initialize all configured clients
	if err := g.clientManager.InitializeClients(ctx, g.config.DownstreamMCPServers); err != nil {
		g.logger.Error(ctx, "Some clients failed to initialize", zap.Error(err))
		// Continue startup even if some clients failed to initialize
	}

	// Initialize server based on server type
	if err := g.initServer(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}

	// Wire up the tools provider now that the server is initialized
	g.nativeToolsHandler.SetToolsProvider(g)

	// Wire up the session provider for native tools
	g.nativeToolsHandler.SetSessionProvider(g.clientManager)

	// Wire up the discovery provider for native tools (pass-through discovery)
	g.nativeToolsHandler.SetDiscoveryProvider(g)

	return nil
}

// SetMetricsCollector sets the metrics collector for tracking usage
func (g *Gateway) SetMetricsCollector(collector *metrics.Collector) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.metricsCollector = collector
}

// ListRegisteredTools returns the names of all registered tools on the MCP server
func (g *Gateway) ListRegisteredTools() []string {
	if g.server == nil {
		return nil
	}
	tools := g.server.ListTools()
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	return names
}

// Stop gracefully shuts down the gateway
func (g *Gateway) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.logger.Info(ctx, "Stopping gateway")

	// Close the stop channel to signal shutdown
	close(g.stopChan)

	// Close all session clients
	if g.clientManager != nil {
		if err := g.clientManager.Close(ctx); err != nil {
			g.logger.Error(ctx, "Error closing clients", zap.Error(err))
		}
	}

	return nil
}

// HandleToolCall tool handler function
func (g *Gateway) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Track tool invocation in metrics
	if g.metricsCollector != nil {
		g.metricsCollector.IncrementToolInvocations()
	}

	// Check if this is a native gateway tool (not audited)
	if IsNativeTool(req.Params.Name) {
		g.logger.Debug(ctx, "Routing to native tool handler", zap.String("tool", req.Params.Name))
		return g.nativeToolsHandler.HandleToolCall(ctx, req)
	}

	// Get session ID, client IP, and request ID for audit logging
	sessionID, hasSession := GetSessionIDFromContext(ctx)
	clientIP, _ := GetClientIP(ctx)
	requestID, _ := GetRequestID(ctx)

	// Parse the prefixed tool name early to populate audit context
	clientName, originalToolName, parseErr := ParsePrefixedName(req.Params.Name)
	if parseErr != nil {
		// Can't create proper audit context without tool info, skip audit
		return nil, fmt.Errorf("invalid tool name format: %w", parseErr)
	}

	// Create audit context
	audit := NewAuditContext(req.Params.Name, clientName, originalToolName, sessionID, clientIP, requestID)

	// Set request params (convert to map for audit)
	if params, ok := req.Params.Arguments.(map[string]interface{}); ok {
		audit.SetRequestParams(params)
	}

	// Helper to write audit log and return
	writeAuditLog := func() {
		entry := audit.Finalize()
		g.auditLogger.Info(ctx, "Tool call audit",
			zap.Any("audit", entry))
	}

	// Validate request through the chain (timing is captured per-policy)
	validationResults, err := g.ValidateToolCall(ctx, req)

	if err != nil {
		// Validation error - don't write audit log (infrastructure error)
		return nil, fmt.Errorf("request validation failed: %v", err)
	}
	g.logger.Debug(ctx, "Validation results", zap.Any("validationResults", validationResults))

	// Extract validation results by policy type and populate audit context
	g.populateRequestValidationAudit(audit, validationResults)

	// Determine if validation passed
	if validationResults.DenyCount > 0 {
		validationResults.Allowed = false
		if validationResults.DenyCount == 1 {
			validationResults.Message = "Maybe Don't, a policy failed."
		} else {
			validationResults.Message = fmt.Sprintf("Maybe Don't, %d policies failed.", validationResults.DenyCount)
		}
	} else {
		validationResults.Allowed = true
		validationResults.Message = "All policies passed, maybe do."
	}

	// If validation failed, set audit actions and write
	if !validationResults.Allowed {
		audit.SetActions(string(config.PolicyActionDeny), string(config.PolicyActionDeny))
		writeAuditLog()

		errorMessage, errorData := g.buildPolicyDeniedError(&validationResults, req.Params.Name)
		return nil, &PolicyDeniedError{
			Message: errorMessage,
			Data:    errorData,
		}
	}

	// Check if we have a valid session
	if !hasSession {
		// No session - infrastructure error, skip audit
		return nil, fmt.Errorf("no session ID in context")
	}

	// Get the appropriate client for this session
	clientInfo, err := g.clientManager.GetSessionClient(sessionID, clientName)
	if err != nil {
		// Client not found - infrastructure error, skip audit
		return nil, fmt.Errorf("client not found for session: %w", err)
	}

	// Create a new request with the original tool name
	originalReq := req
	originalReq.Params.Name = originalToolName

	// Call the tool on the appropriate client with timing
	toolCallStart := time.Now()
	result, err := clientInfo.Client.CallTool(ctx, originalReq)
	toolCallDuration := time.Since(toolCallStart).Milliseconds()

	if err != nil {
		// Tool execution error - infrastructure error, skip audit
		return nil, fmt.Errorf("tool call failed: %w", err)
	}

	// Record tool call duration
	audit.SetRequestDuration(toolCallDuration)

	// Record response info
	audit.SetResponse(len(result.Content), result.IsError)

	// Validate response through the response validation chain (timing is captured per-policy)
	if g.responseValidationChain != nil {
		responseValidationResults, respErr := g.responseValidationChain.Handle(ctx, req, result)

		if respErr != nil {
			g.logger.Error(ctx, "Response validation error", zap.Error(respErr))
			// Continue even if response validation has errors
		}

		// Populate response validation audit
		g.populateResponseValidationAudit(audit, responseValidationResults)

		// If response validation denied the response
		if !responseValidationResults.Allowed {
			audit.SetActions(string(config.PolicyActionDeny), string(config.PolicyActionDeny))
			writeAuditLog()
			return nil, fmt.Errorf("response denied by policy: %s", responseValidationResults.Message)
		}

		// If response was redacted, update the result
		if responseValidationResults.RedactedContent != nil {
			if len(result.Content) > 0 {
				for i := range result.Content {
					if textContent, ok := result.Content[i].(mcp.TextContent); ok {
						textContent.Text = *responseValidationResults.RedactedContent
						result.Content[i] = textContent
						break
					}
				}
			}
		}

		// Add response validation summary to metadata
		if result.Meta == nil {
			result.Meta = &mcp.Meta{}
		}
		if result.Meta.AdditionalFields == nil {
			result.Meta.AdditionalFields = make(map[string]interface{})
		}
		responseValidationSummary := make(map[string]string)
		for _, v := range responseValidationResults.Results {
			if v.PolicyType != "audit" {
				if v.Action != config.PolicyActionDeny {
					responseValidationSummary[v.PolicyName] = "passed"
				} else {
					responseValidationSummary[v.PolicyName] = "failed"
				}
			}
		}
		result.Meta.AdditionalFields["response_validation_summary"] = responseValidationSummary
	}

	// Add validation summary to the result metadata
	if result.Meta == nil {
		result.Meta = &mcp.Meta{}
	}
	if result.Meta.AdditionalFields == nil {
		result.Meta.AdditionalFields = make(map[string]interface{})
	}
	validationSummary := make(map[string]string)
	for _, v := range validationResults.Results {
		if v.PolicyType != "audit" {
			if v.Action == config.PolicyActionAllow {
				validationSummary[v.PolicyName] = "passed"
			} else {
				validationSummary[v.PolicyName] = "failed"
			}
		}
	}
	result.Meta.AdditionalFields["validation_summary"] = validationSummary

	// Success - set actions and write audit log
	audit.SetActions(string(config.PolicyActionAllow), string(config.PolicyActionAllow))
	writeAuditLog()

	return result, nil
}

// populateRequestValidationAudit extracts validation results by policy type and populates audit context
func (g *Gateway) populateRequestValidationAudit(audit *AuditContext, results ValidationResults) {
	if len(results.Results) == 0 {
		return
	}

	// Extract CEL and AI results
	for _, r := range results.Results {
		if r.PolicyType == "audit" {
			continue // Skip audit-only policies
		}

		switch r.PolicyType {
		case "cel":
			audit.SetRequestValidationCEL(string(r.Action), r.PolicyName, r.DurationMs)
		case "ai":
			audit.SetRequestValidationAI(string(r.Action), r.Message, r.DurationMs)
		}
	}
}

// populateResponseValidationAudit extracts response validation results by policy type
func (g *Gateway) populateResponseValidationAudit(audit *AuditContext, results ResponseValidationResults) {
	if len(results.Results) == 0 {
		return
	}

	for _, r := range results.Results {
		if r.PolicyType == "audit" {
			continue
		}

		switch r.PolicyType {
		case "cel":
			audit.SetResponseValidationCEL(string(r.Action), r.PolicyName, r.DurationMs)
		case "ai":
			audit.SetResponseValidationAI(string(r.Action), r.Message, r.DurationMs)
		}
	}
}

// buildPolicyDeniedError creates a user-friendly error message and structured data for policy failures
func (g *Gateway) buildPolicyDeniedError(validationResults *ValidationResults, toolName string) (string, map[string]interface{}) {
	// Create a user-friendly error message
	var deniedPolicies []string
	var deniedMessages []string

	for _, result := range validationResults.Results {
		if result.Action == config.PolicyActionDeny && result.PolicyType != "audit" {
			deniedPolicies = append(deniedPolicies, result.PolicyName)
			if result.Message != "" {
				deniedMessages = append(deniedMessages, result.Message)
			}
		}
	}

	// Build user-friendly error message
	var errorMessage string
	if len(deniedPolicies) > 0 {
		if len(deniedPolicies) == 1 {
			// Single policy failure
			if len(deniedMessages) > 0 {
				errorMessage = fmt.Sprintf("Request denied by policy '%s': %s", deniedPolicies[0], deniedMessages[0])
			} else {
				errorMessage = fmt.Sprintf("Request denied by policy '%s'", deniedPolicies[0])
			}
		} else {
			// Multiple policy failures
			errorMessage = fmt.Sprintf("Request denied by %d policies:", len(deniedPolicies))
			for i, policyName := range deniedPolicies {
				if i < len(deniedMessages) && deniedMessages[i] != "" {
					errorMessage += fmt.Sprintf("\n- '%s': %s", policyName, deniedMessages[i])
				} else {
					errorMessage += fmt.Sprintf("\n- '%s'", policyName)
				}
			}
		}
	} else {
		errorMessage = fmt.Sprintf("Request denied by %d policy(ies)", len(deniedPolicies))
	}

	// Create structured error data
	errorData := map[string]interface{}{
		"denied_policies": deniedPolicies,
		"denied_count":    validationResults.DenyCount,
		"tool_name":       toolName,
	}

	return errorMessage, errorData
}

// Prompt handler function
func (g *Gateway) HandlePromptCall(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	// Parse the prefixed prompt name to get client name and original prompt name
	clientName, originalPromptName, err := ParsePrefixedName(req.Params.Name)
	if err != nil {
		return nil, fmt.Errorf("invalid prompt name format: %w", err)
	}

	// Get session ID from context
	sessionID, hasSession := GetSessionIDFromContext(ctx)
	if !hasSession {
		return nil, fmt.Errorf("no session ID in context")
	}

	// Get the appropriate client for this session
	clientInfo, err := g.clientManager.GetSessionClient(sessionID, clientName)
	if err != nil {
		return nil, fmt.Errorf("client not found for session: %w", err)
	}

	// Create a new request with the original prompt name
	originalReq := req
	originalReq.Params.Name = originalPromptName

	return clientInfo.Client.GetPrompt(ctx, originalReq)
}

// handleResourceRequest is a helper function that handles both resource and resource template requests
func (g *Gateway) handleResourceRequest(ctx context.Context, req mcp.ReadResourceRequest, resourceType string) ([]mcp.ResourceContents, error) {
	// Parse the prefixed URI to get client name and original URI
	clientName, originalURI, err := ParsePrefixedName(req.Params.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid %s URI format: %w", resourceType, err)
	}

	// Get session ID from context
	sessionID, hasSession := GetSessionIDFromContext(ctx)
	if !hasSession {
		return nil, fmt.Errorf("no session ID in context")
	}

	// Get the appropriate client for this session
	clientInfo, err := g.clientManager.GetSessionClient(sessionID, clientName)
	if err != nil {
		return nil, fmt.Errorf("client not found for session: %w", err)
	}

	// Create a new request with the original URI
	originalReq := req
	originalReq.Params.URI = originalURI

	result, err := clientInfo.Client.ReadResource(ctx, originalReq)
	if err != nil {
		return nil, err
	}
	return result.Contents, nil
}

// Resource handler function
func (g *Gateway) HandleResourceCall(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return g.handleResourceRequest(ctx, req, "resource")
}

// Resource template handler function
func (g *Gateway) HandleResourceTemplateCall(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return g.handleResourceRequest(ctx, req, "resource template")
}

// DiscoverPassThroughTools implements PassThroughDiscoveryProvider interface.
// It triggers discovery for pass-through clients that weren't discovered at startup,
// using credentials from the current request context.
func (g *Gateway) DiscoverPassThroughTools(ctx context.Context, sessionID string, clientName string) (*DiscoveryResult, error) {
	g.logger.Info(ctx, "Triggering pass-through tool discovery",
		zap.String("session_id", sessionID),
		zap.String("client_filter", clientName))

	result := &DiscoveryResult{
		DiscoveredClients: []DiscoveredClientInfo{},
		AlreadyConnected:  []string{},
		Errors:            []DiscoveryError{},
	}

	// Get all client configs
	configs := g.clientManager.GetClientConfigs()

	// Filter to only pass-through clients
	var passThroughClients []string
	for name, cfg := range configs {
		if !cfg.Auth.PassThrough.Enabled {
			continue
		}
		// If a specific client was requested, filter to that one
		if clientName != "" && name != clientName {
			continue
		}
		passThroughClients = append(passThroughClients, name)
	}

	if len(passThroughClients) == 0 {
		if clientName != "" {
			return nil, fmt.Errorf("client '%s' is not configured or is not a pass-through client", clientName)
		}
		g.logger.Info(ctx, "No pass-through clients configured")
		return result, nil
	}

	// Check which clients are already connected for this session
	for _, name := range passThroughClients {
		existingClient, err := g.clientManager.GetSessionClient(sessionID, name)
		if err == nil && existingClient != nil && existingClient.Client != nil {
			// Client already connected
			result.AlreadyConnected = append(result.AlreadyConnected, name)
			g.logger.Debug(ctx, "Client already connected for session",
				zap.String("session_id", sessionID),
				zap.String("client", name))
			continue
		}

		// Attempt to create/connect to this client
		cfg := configs[name]
		clientInfo, err := g.clientManager.CreateSingleSessionClient(ctx, sessionID, name, cfg)
		if err != nil {
			result.Errors = append(result.Errors, DiscoveryError{
				ClientName: name,
				Error:      err.Error(),
			})
			g.logger.Warn(ctx, "Failed to connect to pass-through client",
				zap.String("session_id", sessionID),
				zap.String("client", name),
				zap.Error(err))
			continue
		}

		// Register discovered tools for this session
		if len(clientInfo.Tools) > 0 {
			toolNames := make([]string, 0, len(clientInfo.Tools))
			for _, tool := range clientInfo.Tools {
				prefixedTool := tool
				prefixedTool.Name = PrefixName(name, tool.Name)

				err := g.server.AddSessionTool(sessionID, prefixedTool, g.handleToolCallWithErrorHandling)
				if err != nil {
					g.logger.Warn(ctx, "Failed to register session tool",
						zap.String("session_id", sessionID),
						zap.String("client", name),
						zap.String("tool", prefixedTool.Name),
						zap.Error(err))
					continue
				}
				toolNames = append(toolNames, tool.Name)
			}

			result.DiscoveredClients = append(result.DiscoveredClients, DiscoveredClientInfo{
				ClientName: name,
				ToolCount:  len(toolNames),
				Tools:      toolNames,
			})

			g.logger.Info(ctx, "Discovered and registered tools from pass-through client",
				zap.String("session_id", sessionID),
				zap.String("client", name),
				zap.Int("tools_count", len(toolNames)))
		}
	}

	return result, nil
}
