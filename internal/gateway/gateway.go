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
	"golang.org/x/sync/singleflight"
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

// SessionExpiredError indicates that the session is no longer valid and needs to be re-established.
// This error provides clear guidance to AI agents on how to recover.
type SessionExpiredError struct {
	SessionID string
	Reason    string
}

// Error implements the error interface
func (e *SessionExpiredError) Error() string {
	return fmt.Sprintf("Session expired: %s. To recover, call the 'maybedont__discover_tools' tool to re-establish your connection to downstream MCP servers. This will create a new session and rediscover available tools.", e.Reason)
}

// IsSessionExpiredError checks if an error is a SessionExpiredError
func IsSessionExpiredError(err error) bool {
	_, ok := err.(*SessionExpiredError)
	return ok
}

// Gateway represents an MCP security gateway instance
type Gateway struct {
	logger           *config.SessionLogger
	auditWriter      AuditWriter
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
	// AI provider info for audit logging (populated when AI validation is enabled)
	aiProviderInfo *AuditAIProvider
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
	// Caller authentication config (loaded once at startup)
	callerAuthConfig *CallerAuthConfig
	// WaitGroup for tracking pending async audit writes
	pendingAuditWrites sync.WaitGroup
	// Singleflight group for deduplicating concurrent lazy discovery requests per session
	lazyDiscoveryGroup singleflight.Group
	// Singleflight group for deduplicating concurrent maybedont__discover_tools calls
	discoverToolsGroup singleflight.Group
}

// New creates a new gateway instance.
// logDir: the resolved log directory for audit log file output.
func New(ctx context.Context, cfg *config.Config, logger *config.SessionLogger, version string, logDir string) (*Gateway, error) {
	// Create audit writer with rotation and filtering support
	auditPath := cfg.Audit.Path
	if auditPath == "" {
		auditPath = "maybedont-audit.log"
	}
	auditWriter, err := NewJSONLAuditWriter(auditPath, logDir, cfg.Audit.Rotation, cfg.Audit.Filter)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit writer: %w", err)
	}

	// Initialize policy engines
	var policyEngine *CELPolicyEngine
	var aiPolicyEngine *AIPolicyEngine
	var aiProviderInfo *AuditAIProvider

	// Initialize CEL request policy engine only if enabled
	if cfg.RequestValidation.CEL.Enabled {
		celMode := cfg.RequestValidation.CEL.Mode
		logger.Info(ctx, "Initializing CEL request policy engine", zap.String("mode", string(celMode)))
		policyEngine, err = NewCELPolicyEngine(ctx, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL request policy engine: %w", err)
		}

		// Load policies from configuration with the resolved mode
		if err := policyEngine.LoadPolicies(cfg.RequestValidation.CEL.Rules, celMode); err != nil {
			return nil, fmt.Errorf("failed to load CEL request policies: %w", err)
		}
	} else {
		logger.Info(ctx, "CEL request policy validation is disabled")
	}

	// Initialize AI request policy engine only if enabled
	if cfg.RequestValidation.AI.Enabled {
		aiMode := cfg.RequestValidation.AI.Mode
		logger.Info(ctx, "Initializing AI request policy engine", zap.String("mode", string(aiMode)))
		aiPolicyEngine = &AIPolicyEngine{
			cfg:                 cfg,
			maxRuleEvaluationMs: cfg.Validation.MaxRuleEvaluationMs,
		}

		// Create AI request policy engine
		err = InitAIPolicyEngine(logger, aiPolicyEngine)
		if err != nil {
			return nil, fmt.Errorf("failed to init AI request policy engine: %w", err)
		}

		// Capture AI provider info for audit logging (shared by both request and response validation)
		aiProviderInfo = NewAuditAIProvider(aiPolicyEngine.providerClient.ProviderInfo())

		// Load policies from configuration with the resolved mode
		if err := aiPolicyEngine.LoadPolicies(cfg.RequestValidation.AI.Rules, aiMode); err != nil {
			return nil, fmt.Errorf("failed to load AI request policies: %w", err)
		}
	} else {
		logger.Info(ctx, "AI request policy validation is disabled")
	}

	// Initialize response validation engines
	var responsePolicyEngine *CELResponsePolicyEngine
	var aiResponsePolicyEngine *AIResponsePolicyEngine

	// Initialize CEL response policy engine only if enabled
	if cfg.ResponseValidation.CEL.Enabled {
		celResponseMode := cfg.ResponseValidation.CEL.Mode
		logger.Info(ctx, "Initializing CEL response policy engine", zap.String("mode", string(celResponseMode)))
		responsePolicyEngine, err = NewCELResponsePolicyEngine(ctx, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL response policy engine: %w", err)
		}

		// Load policies from configuration with the resolved mode
		if err := responsePolicyEngine.LoadPolicies(cfg.ResponseValidation.CEL.Rules, celResponseMode); err != nil {
			return nil, fmt.Errorf("failed to load CEL response policies: %w", err)
		}
	} else {
		logger.Info(ctx, "CEL response validation is disabled")
	}

	// Initialize AI response policy engine only if enabled
	if cfg.ResponseValidation.AI.Enabled {
		aiResponseMode := cfg.ResponseValidation.AI.Mode
		logger.Info(ctx, "Initializing AI response policy engine", zap.String("mode", string(aiResponseMode)))
		aiResponsePolicyEngine = &AIResponsePolicyEngine{
			cfg:                 cfg,
			maxRuleEvaluationMs: cfg.Validation.MaxRuleEvaluationMs,
		}

		// Create AI response policy engine
		err = InitAIResponsePolicyEngine(ctx, logger, aiResponsePolicyEngine)
		if err != nil {
			return nil, fmt.Errorf("failed to init AI response policy engine: %w", err)
		}

		// Capture AI provider info for audit logging if not already captured by request validation
		// (request and response share the same AI config)
		if aiProviderInfo == nil {
			aiProviderInfo = NewAuditAIProvider(aiResponsePolicyEngine.providerClient.ProviderInfo())
		}

		// Load policies from configuration with the resolved mode
		if err := aiResponsePolicyEngine.LoadPolicies(cfg.ResponseValidation.AI.Rules, aiResponseMode); err != nil {
			return nil, fmt.Errorf("failed to load AI response policies: %w", err)
		}
	} else {
		logger.Info(ctx, "AI response validation is disabled")
	}

	// Create client manager with configured session timeout
	sessionTimeout := DefaultSessionTimeout
	if cfg.Server.SessionTimeoutMinutes > 0 {
		sessionTimeout = time.Duration(cfg.Server.SessionTimeoutMinutes) * time.Minute
	}
	logger.Info(ctx, "Session timeout configured", zap.Duration("timeout", sessionTimeout))
	clientManager := NewClientManagerWithTimeout(ctx, logger, sessionTimeout)

	// Create native tools handler with the resolved audit log path
	auditLogPath := config.ResolveAuditLogPath(cfg, logDir)
	nativeToolsHandler := NewNativeToolsHandler(cfg, logger, auditLogPath)

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

	// Load and validate caller auth config (fail fast on invalid patterns)
	callerAuthConfig, err := LoadCallerAuthConfig()
	if err != nil {
		return nil, fmt.Errorf("invalid caller auth configuration: %w", err)
	}

	return &Gateway{
		logger:                 logger,
		auditWriter:            auditWriter,
		config:                 cfg,
		version:                version,
		stopChan:               make(chan struct{}),
		clientManager:          clientManager,
		policyEngine:           policyEngine,
		aiPolicyEngine:         aiPolicyEngine,
		aiProviderInfo:         aiProviderInfo,
		responsePolicyEngine:   responsePolicyEngine,
		aiResponsePolicyEngine: aiResponsePolicyEngine,
		nativeToolsHandler:     nativeToolsHandler,
		trustedProxyChecker:    trustedProxyChecker,
		callerAuthConfig:       callerAuthConfig,
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

	// Close the stop channel to signal shutdown
	close(g.stopChan)

	// Close all session clients
	if g.clientManager != nil {
		if err := g.clientManager.Close(ctx); err != nil {
			g.logger.Error(ctx, "Error closing clients", zap.Error(err))
		}
	}

	// Wait for pending async audit writes to complete before closing the writer
	g.logger.Debug(ctx, "Waiting for pending async audit writes to complete")
	g.pendingAuditWrites.Wait()

	// Close the audit writer
	if g.auditWriter != nil {
		if err := g.auditWriter.Close(); err != nil {
			g.logger.Error(ctx, "Error closing audit writer", zap.Error(err))
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

	// Enrich context with session_id for logging (request_id is already set by middleware)
	// This must happen before any tool handling so all log messages have session context
	sessionID, hasSession := GetSessionIDFromContext(ctx)
	ctx = WithSessionID(ctx, sessionID)

	// Check if this is a native gateway tool (not audited)
	if IsNativeTool(req.Params.Name) {
		g.logger.Debug(ctx, "Routing to native tool handler", zap.String("tool", req.Params.Name))
		return g.nativeToolsHandler.HandleToolCall(ctx, req)
	}

	// Get client IP, user agent, and request ID for audit logging
	clientIP, _ := GetClientIP(ctx)
	userAgent, _ := GetUserAgent(ctx)
	requestID, _ := GetRequestID(ctx)

	// Parse the prefixed tool name early to populate audit context
	clientName, originalToolName, parseErr := ParsePrefixedName(req.Params.Name)
	if parseErr != nil {
		// Can't create proper audit context without tool info, skip audit
		return nil, fmt.Errorf("invalid tool name format: %w", parseErr)
	}

	// Create audit context
	audit := NewAuditContext(req.Params.Name, clientName, originalToolName, sessionID, clientIP, requestID)

	// Set User-Agent header
	audit.SetUserAgent(userAgent)

	// Set AI provider info if AI validation is enabled
	if g.aiProviderInfo != nil {
		audit.SetAIProvider(g.aiProviderInfo)
	}

	// Set request params (convert to map for audit)
	if params, ok := req.Params.Arguments.(map[string]interface{}); ok {
		audit.SetRequestParams(params)
	}

	// Create blocking budget for cumulative blocking time tracking
	// Uses max_blocking_ms from global validation config (applies to all validation phases)
	maxBlockingMs := int64(g.config.Validation.MaxBlockingMs)
	blockingBudget := NewBlockingBudget(maxBlockingMs)

	// Create context with blocking budget for validation handlers
	validationCtx := WithBlockingBudget(ctx, blockingBudget)

	// Helper to write audit log - handles both sync and async cases
	writeAuditLog := func() {
		// Calculate total blocked time = validation blocking + tool call duration
		toolDuration := int64(0)
		if audit.Entry().Tool.DurationMs != nil {
			toolDuration = *audit.Entry().Tool.DurationMs
		}
		audit.SetTotalBlockedMs(blockingBudget.TotalBlockedMs() + toolDuration)

		// Check if there's async work pending (audit_only policies still running)
		hasAsyncWork := audit.HasAsyncWork()
		g.logger.Debug(ctx, "Audit log write decision",
			zap.Bool("has_async_work", hasAsyncWork),
			zap.String("tool", audit.Entry().Tool.PrefixedName),
		)

		if hasAsyncWork {
			// Track this async write for graceful shutdown
			g.pendingAuditWrites.Add(1)
			// Create a context with request_id and session_id for async logging (original ctx may be cancelled)
			asyncCtx := context.WithValue(context.Background(), config.RequestIDKey, audit.Entry().UpstreamRequest.RequestID)
			asyncCtx = context.WithValue(asyncCtx, config.SessionIDKey, audit.Entry().UpstreamRequest.SessionID)
			toolName := audit.Entry().Tool.PrefixedName
			// Write audit log asynchronously after all background work completes
			go func() {
				defer g.pendingAuditWrites.Done()
				g.logger.Debug(asyncCtx, "Starting async audit log finalization",
					zap.String("tool", toolName),
				)
				entry := audit.FinalizeAsync()
				written, err := g.auditWriter.Write(entry)
				if err != nil {
					g.logger.Error(asyncCtx, "Failed to write audit log (async)",
						zap.Error(err),
					)
				} else {
					g.logger.Debug(asyncCtx, "Async audit log write completed",
						zap.Bool("written", written),
						zap.String("tool", toolName),
					)
				}
			}()
		} else {
			// Synchronous audit log write
			entry := audit.Finalize()
			if _, err := g.auditWriter.Write(entry); err != nil {
				g.logger.Error(ctx, "Failed to write audit log", zap.Error(err))
			}
		}
	}

	// Validate request through the chain (timing is captured per-policy)
	g.logger.Debug(ctx, "Starting tool call validation",
		zap.String("session_id", sessionID),
		zap.String("tool", req.Params.Name),
		zap.String("client", clientName),
	)
	validationResults, err := g.ValidateToolCall(validationCtx, req)

	if err != nil {
		// Validation error - don't write audit log (infrastructure error)
		return nil, fmt.Errorf("request validation failed: %v", err)
	}

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
		audit.SetActions(string(config.PolicyActionDeny), string(config.PolicyActionDeny), ActionReasonRequestPolicy)
		writeAuditLog()

		g.logger.Info(ctx, "Tool call denied by request validation",
			zap.String("session_id", sessionID),
			zap.String("tool", req.Params.Name),
			zap.String("client", clientName),
			zap.Int("deny_count", validationResults.DenyCount),
			zap.Strings("denying_rules", validationResults.DenyingRuleNames()),
			zap.String("message", validationResults.Message),
		)

		errorMessage, errorData := g.buildPolicyDeniedError(&validationResults, req.Params.Name)
		return nil, &PolicyDeniedError{
			Message: errorMessage,
			Data:    errorData,
		}
	}

	// Check if we have a valid session
	if !hasSession {
		// No session - return a helpful error for AI agents
		return nil, &SessionExpiredError{
			SessionID: "",
			Reason:    "no session established",
		}
	}

	// Get the appropriate client for this session
	clientInfo, err := g.clientManager.GetSessionClient(sessionID, clientName)
	if err != nil {
		// Check if this is a session expired error - pass it through directly
		if IsSessionExpiredError(err) {
			return nil, err
		}
		// Other client errors - wrap with context
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

	// Validate response through the response validation chain (timing is captured per-policy)
	// Use validationCtx to share the blocking budget with response validation
	// Skip validation for empty responses — there is nothing for the AI to evaluate.
	// Guard condition mirrored in TestResponseValidationSkipsEmptyContent (response_validation_test.go)
	if g.responseValidationChain != nil && len(result.Content) > 0 {
		g.logger.Debug(ctx, "Starting response validation",
			zap.String("session_id", sessionID),
			zap.String("tool", req.Params.Name),
			zap.String("client", clientName),
		)
		responseValidationResults, respErr := g.responseValidationChain.Handle(validationCtx, req, result)

		if respErr != nil {
			g.logger.Error(ctx, "Response validation error",
				zap.String("session_id", sessionID),
				zap.String("tool", req.Params.Name),
				zap.String("client", clientName),
				zap.Error(respErr))
			// Continue even if response validation has errors
		}

		// Populate response validation audit
		g.populateResponseValidationAudit(audit, responseValidationResults)

		// If response validation denied the response
		if !responseValidationResults.Allowed {
			audit.SetActions(string(config.PolicyActionDeny), string(config.PolicyActionDeny), ActionReasonResponsePolicy)
			writeAuditLog()

			g.logger.Info(ctx, "Tool call denied by response validation",
				zap.String("session_id", sessionID),
				zap.String("tool", req.Params.Name),
				zap.String("client", clientName),
				zap.Int("deny_count", responseValidationResults.DenyCount()),
				zap.Strings("denying_rules", responseValidationResults.DenyingRuleNames()),
				zap.String("message", responseValidationResults.Message),
			)

			errorMessage := g.buildResponseDeniedError(&responseValidationResults, req.Params.Name)
			return nil, &PolicyDeniedError{
				Message: errorMessage,
				Data: map[string]interface{}{
					"tool_name":    req.Params.Name,
					"denied_count": responseValidationResults.DenyCount(),
					"phase":        "response",
				},
			}
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
	} else if g.responseValidationChain != nil {
		g.logger.Debug(ctx, "Skipping response validation for empty response",
			zap.String("session_id", sessionID),
			zap.String("tool", req.Params.Name),
			zap.String("client", clientName),
		)
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
	// Determine action reason based on validation results
	var actionReason ActionReason
	var recommendedAction string
	if validationResults.FailedOpen {
		actionReason = ActionReasonFailOpen
		recommendedAction = "" // Unknown - omit from audit log
	} else if validationResults.AuditModeBypass {
		actionReason = ActionReasonAuditMode
		recommendedAction = string(validationResults.RecommendedAction)
	} else {
		recommendedAction = string(config.PolicyActionAllow)
	}
	audit.SetActions(recommendedAction, string(config.PolicyActionAllow), actionReason)
	writeAuditLog()

	return result, nil
}

// populateRequestValidationAudit extracts validation results by policy type and populates audit context
func (g *Gateway) populateRequestValidationAudit(audit *AuditContext, results ValidationResults) {
	if len(results.Results) == 0 && results.RulesDetails == nil && results.AIDetails == nil && results.AsyncCompletion == nil {
		return
	}

	// Set rules details if present (deterministic rule evaluation results)
	if results.RulesDetails != nil {
		audit.SetRequestValidationRules(results.RulesDetails)
	}

	// Set AI details if present (for synchronous completion)
	if results.AIDetails != nil {
		audit.SetRequestValidationAI(results.AIDetails)
	}

	// Register async completion channel if present (for audit_only policies still running)
	if results.AsyncCompletion != nil {
		// Create a context with request_id and session_id for logging (we don't have the original ctx here)
		logCtx := context.WithValue(context.Background(), config.RequestIDKey, audit.Entry().UpstreamRequest.RequestID)
		logCtx = context.WithValue(logCtx, config.SessionIDKey, audit.Entry().UpstreamRequest.SessionID)
		g.logger.Debug(logCtx, "Registering async request AI completion channel",
			zap.String("tool", audit.Entry().Tool.PrefixedName),
		)
		audit.SetRequestAIResultsAsync(results.AsyncCompletion)
	}
}

// populateResponseValidationAudit extracts response validation results by policy type
func (g *Gateway) populateResponseValidationAudit(audit *AuditContext, results ResponseValidationResults) {
	if len(results.Results) == 0 && results.RulesDetails == nil && results.AIDetails == nil && results.AsyncCompletion == nil {
		return
	}

	// Set rules details if present (deterministic rule evaluation results)
	if results.RulesDetails != nil {
		audit.SetResponseValidationRules(results.RulesDetails)
	}

	// Set AI details if present (for synchronous completion)
	if results.AIDetails != nil {
		audit.SetResponseValidationAI(results.AIDetails)
	}

	// Register async completion channel if present (for audit_only policies still running)
	if results.AsyncCompletion != nil {
		// Create a context with request_id and session_id for logging (we don't have the original ctx here)
		logCtx := context.WithValue(context.Background(), config.RequestIDKey, audit.Entry().UpstreamRequest.RequestID)
		logCtx = context.WithValue(logCtx, config.SessionIDKey, audit.Entry().UpstreamRequest.SessionID)
		g.logger.Debug(logCtx, "Registering async response AI completion channel",
			zap.String("tool", audit.Entry().Tool.PrefixedName),
		)
		audit.SetResponseAIResultsAsync(results.AsyncCompletion)
	}
}

// buildPolicyDeniedError creates an AI-friendly error message and structured data for request policy failures.
// The message is designed to help AI agents understand why the request was denied and suggest alternatives.
func (g *Gateway) buildPolicyDeniedError(validationResults *ValidationResults, toolName string) (string, map[string]interface{}) {
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

	// Build AI-friendly error message with guidance
	var errorMessage string
	if len(deniedPolicies) > 0 {
		if len(deniedPolicies) == 1 {
			if len(deniedMessages) > 0 {
				errorMessage = fmt.Sprintf("Request denied by policy '%s': %s", deniedPolicies[0], deniedMessages[0])
			} else {
				errorMessage = fmt.Sprintf("Request denied by policy '%s'", deniedPolicies[0])
			}
		} else {
			errorMessage = fmt.Sprintf("Request denied by %d policies:", len(deniedPolicies))
			for i, policyName := range deniedPolicies {
				if i < len(deniedMessages) && deniedMessages[i] != "" {
					errorMessage += fmt.Sprintf("\n- %s: %s", policyName, deniedMessages[i])
				} else {
					errorMessage += fmt.Sprintf("\n- %s", policyName)
				}
			}
		}
		// Add guidance for AI agents
		errorMessage += "\n\nTo proceed, consider: using a different tool, modifying parameters to avoid restricted operations, or asking the user for guidance on allowed alternatives."
	} else {
		errorMessage = "Request denied by policy. Please try a different approach or ask the user for guidance."
	}

	errorData := map[string]interface{}{
		"denied_policies": deniedPolicies,
		"denied_count":    validationResults.DenyCount,
		"tool_name":       toolName,
	}

	return errorMessage, errorData
}

// buildResponseDeniedError creates an AI-friendly error message for response policy failures.
// The message helps AI agents understand why the response was blocked and what to try next.
func (g *Gateway) buildResponseDeniedError(results *ResponseValidationResults, toolName string) string {
	var deniedPolicies []string
	var deniedMessages []string

	for _, result := range results.Results {
		if result.Action == config.PolicyActionDeny {
			deniedPolicies = append(deniedPolicies, result.PolicyName)
			if result.Message != "" {
				deniedMessages = append(deniedMessages, result.Message)
			}
		}
	}

	var errorMessage string

	// Check if this was a validation timeout/error vs explicit denial
	if results.Message != "" && (len(deniedPolicies) == 0 ||
		containsAny(results.Message, "timeout", "deadline exceeded", "Failed to evaluate")) {
		// Translate technical error messages to user-friendly ones
		friendlyMessage := humanizeErrorMessage(results.Message)
		errorMessage = fmt.Sprintf("Response validation failed: %s", friendlyMessage)
		errorMessage += "\n\nThis may be a temporary issue. You can retry the request, or try with simpler parameters that produce a smaller response."
	} else if len(deniedPolicies) > 0 {
		if len(deniedPolicies) == 1 {
			if len(deniedMessages) > 0 {
				errorMessage = fmt.Sprintf("Response denied by policy '%s': %s", deniedPolicies[0], deniedMessages[0])
			} else {
				errorMessage = fmt.Sprintf("Response denied by policy '%s'", deniedPolicies[0])
			}
		} else {
			errorMessage = fmt.Sprintf("Response denied by %d policies:", len(deniedPolicies))
			for i, policyName := range deniedPolicies {
				if i < len(deniedMessages) && deniedMessages[i] != "" {
					errorMessage += fmt.Sprintf("\n- %s: %s", policyName, deniedMessages[i])
				} else {
					errorMessage += fmt.Sprintf("\n- %s", policyName)
				}
			}
		}
		errorMessage += "\n\nThe tool executed but the response was filtered. Consider requesting less sensitive data, using more specific filters, or asking the user about alternative approaches."
	} else {
		errorMessage = fmt.Sprintf("Response blocked: %s", humanizeErrorMessage(results.Message))
		errorMessage += "\n\nConsider trying a different approach or asking the user for guidance."
	}

	return errorMessage
}

// humanizeErrorMessage translates technical Go error messages to user-friendly descriptions
func humanizeErrorMessage(msg string) string {
	if containsAny(msg, "context deadline exceeded", "deadline exceeded") {
		return "validation timed out while processing the response"
	}
	if containsAny(msg, "context canceled") {
		return "validation was cancelled"
	}
	if containsAny(msg, "connection refused", "connection reset") {
		return "could not connect to validation service"
	}
	if containsAny(msg, "Failed to evaluate response policy") {
		// Extract the underlying cause if present
		if containsAny(msg, "deadline exceeded") {
			return "policy evaluation timed out"
		}
		return "policy evaluation failed"
	}
	return msg
}

// containsAny checks if s contains any of the substrings
func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
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
		return nil, &SessionExpiredError{
			SessionID: "",
			Reason:    "no session established",
		}
	}

	// Get the appropriate client for this session
	clientInfo, err := g.clientManager.GetSessionClient(sessionID, clientName)
	if err != nil {
		// Check if this is a session expired error - pass it through directly
		if IsSessionExpiredError(err) {
			return nil, err
		}
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
		return nil, &SessionExpiredError{
			SessionID: "",
			Reason:    "no session established",
		}
	}

	// Get the appropriate client for this session
	clientInfo, err := g.clientManager.GetSessionClient(sessionID, clientName)
	if err != nil {
		// Check if this is a session expired error - pass it through directly
		if IsSessionExpiredError(err) {
			return nil, err
		}
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
// Uses singleflight to deduplicate concurrent requests for the same session/client.
func (g *Gateway) DiscoverPassThroughTools(ctx context.Context, sessionID string, clientName string) (*DiscoveryResult, error) {
	// Build singleflight key: sessionID/clientName (clientName may be empty for "all clients")
	singleflightKey := sessionID + "/" + clientName

	result, err, shared := g.discoverToolsGroup.Do(singleflightKey, func() (interface{}, error) {
		return g.doDiscoverPassThroughTools(ctx, sessionID, clientName)
	})

	if err != nil {
		return nil, err
	}

	discoveryResult := result.(*DiscoveryResult)

	// Mark the result as shared if this request waited for another's discovery
	if shared {
		// Copy the result to avoid mutating the shared instance
		sharedResult := *discoveryResult
		sharedResult.Shared = true
		return &sharedResult, nil
	}

	return discoveryResult, nil
}

// doDiscoverPassThroughTools performs the actual discovery logic.
// This is called within singleflight to deduplicate concurrent requests.
func (g *Gateway) doDiscoverPassThroughTools(ctx context.Context, sessionID string, clientName string) (*DiscoveryResult, error) {
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

		// Register discovered tools globally (not per-session)
		// Using AddTool instead of AddSessionTool ensures the tool always exists in the SDK's registry,
		// even after the session expires. This allows our handler to return a helpful error message
		// guiding the user to reconnect, rather than the SDK returning an unhelpful "tool not found" error.
		if len(clientInfo.Tools) > 0 {
			toolNames := make([]string, 0, len(clientInfo.Tools))
			newlyRegistered := make([]string, 0)
			for _, tool := range clientInfo.Tools {
				prefixedTool := tool
				prefixedTool.Name = PrefixName(name, tool.Name)
				prefixedTool.DeferLoading = true

				// Check if this tool is already registered globally (from a previous session)
				existingTool := g.server.GetTool(prefixedTool.Name)
				if existingTool == nil {
					// Register globally so it persists across session lifecycles
					g.server.AddTool(prefixedTool, g.handleToolCallWithErrorHandling)
					newlyRegistered = append(newlyRegistered, prefixedTool.Name)
				}
				toolNames = append(toolNames, tool.Name)
			}

			result.DiscoveredClients = append(result.DiscoveredClients, DiscoveredClientInfo{
				ClientName: name,
				ToolCount:  len(toolNames),
				Tools:      toolNames,
			})

			if len(newlyRegistered) > 0 {
				g.logger.Debug(ctx, "Registered pass-through tools globally",
					zap.String("session_id", sessionID),
					zap.String("client", name),
					zap.Strings("tools", newlyRegistered))
			}

			g.logger.Info(ctx, "Discovered and registered tools from pass-through client",
				zap.String("session_id", sessionID),
				zap.String("client", name),
				zap.Int("tools_count", len(toolNames)))
		}
	}

	return result, nil
}
