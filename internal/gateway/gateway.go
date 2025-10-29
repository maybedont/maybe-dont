package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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
	logger                  *config.SessionLogger
	auditLogger             *config.SessionLogger
	config                  *config.Config
	server                  *server.MCPServer
	clientManager           *ClientManager
	mu                      sync.RWMutex
	stopChan                chan struct{}
	metricsCollector        *metrics.Collector
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
}

// New creates a new gateway instance
func New(ctx context.Context, cfg *config.Config, logger *config.SessionLogger) (*Gateway, error) {
	// Create audit logger with its own configuration
	auditLogger, err := config.GetAuditLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// Initialize policy engines
	var policyEngine *CELPolicyEngine
	var aiPolicyEngine *AIPolicyEngine

	// Initialize CEL policy engine only if enabled
	if cfg.PolicyValidation.Enabled {
		logger.Info(ctx, "Initializing CEL policy engine")
		policyEngine, err = NewCELPolicyEngine(ctx, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL policy engine: %w", err)
		}

		// Load policies from configuration
		if err := policyEngine.LoadPolicies(cfg.PolicyValidation.Rules); err != nil {
			return nil, fmt.Errorf("failed to load CEL policies: %w", err)
		}
	} else {
		logger.Info(ctx, "CEL policy validation is disabled")
	}

	// Initialize AI policy engine only if enabled
	if cfg.AIPolicyValidation.Enabled {
		logger.Info(ctx, "Initializing AI policy engine")
		aiPolicyEngine = &AIPolicyEngine{
			endpoint: cfg.AIPolicyValidation.Endpoint,
			model:    cfg.AIPolicyValidation.Model,
			apiKey:   cfg.AIPolicyValidation.APIKey,
		}

		// Create AI policy engine
		err = InitAIPolicyEngine(ctx, logger, aiPolicyEngine)
		if err != nil {
			return nil, fmt.Errorf("failed to init AI policy engine: %w", err)
		}

		// Load policies from configuration
		if err := aiPolicyEngine.LoadPolicies(cfg.AIPolicyValidation.Rules); err != nil {
			return nil, fmt.Errorf("failed to load AI policies: %w", err)
		}
	} else {
		logger.Info(ctx, "AI policy validation is disabled")
	}

	// Initialize response validation engines
	var responsePolicyEngine *CELResponsePolicyEngine
	var aiResponsePolicyEngine *AIResponsePolicyEngine

	// Initialize CEL response policy engine only if enabled
	if cfg.ResponseValidation.Enabled {
		logger.Info(ctx, "Initializing CEL response policy engine")
		responsePolicyEngine, err = NewCELResponsePolicyEngine(ctx, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL response policy engine: %w", err)
		}

		// Load policies from configuration
		if err := responsePolicyEngine.LoadPolicies(cfg.ResponseValidation.Rules); err != nil {
			return nil, fmt.Errorf("failed to load CEL response policies: %w", err)
		}
	} else {
		logger.Info(ctx, "CEL response validation is disabled")
	}

	// Initialize AI response policy engine only if enabled
	if cfg.AIResponseValidation.Enabled {
		logger.Info(ctx, "Initializing AI response policy engine")
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

		// Load policies from configuration
		if err := aiResponsePolicyEngine.LoadPolicies(cfg.AIResponseValidation.Rules); err != nil {
			return nil, fmt.Errorf("failed to load AI response policies: %w", err)
		}
	} else {
		logger.Info(ctx, "AI response validation is disabled")
	}

	// Create client manager
	clientManager := NewClientManager(ctx, logger)

	return &Gateway{
		logger:                 logger,
		auditLogger:            auditLogger,
		config:                 cfg,
		stopChan:               make(chan struct{}),
		clientManager:          clientManager,
		policyEngine:           policyEngine,
		aiPolicyEngine:         aiPolicyEngine,
		responsePolicyEngine:   responsePolicyEngine,
		aiResponsePolicyEngine: aiResponsePolicyEngine,
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

	// Initialize validation chain with required handlers
	handlers := []ToolValidationHandler{
		NewToolLoggingHandler(g.auditLogger),
	}

	// Add CEL validation handler if enabled
	if g.config.PolicyValidation.Enabled && g.policyEngine != nil {
		g.logger.Info(ctx, "Adding CEL validation handler to chain")
		handlers = append(handlers, NewToolCELValidationHandler(g.logger, g.policyEngine))
	}

	// Add AI validation handler if enabled
	if g.config.AIPolicyValidation.Enabled && g.aiPolicyEngine != nil {
		g.logger.Info(ctx, "Adding AI validation handler to chain")
		handlers = append(handlers, NewToolAIValidationHandler(g.logger, g.aiPolicyEngine))
	}

	g.validationChain = NewToolValidationChain(handlers...)

	// Initialize response validation chain
	responseHandlers := []ResponseValidationHandler{
		NewResponseLoggingHandler(g.auditLogger),
	}

	// Add CEL response validation handler if enabled
	if g.config.ResponseValidation.Enabled && g.responsePolicyEngine != nil {
		g.logger.Info(ctx, "Adding CEL response validation handler to chain")
		responseHandlers = append(responseHandlers, NewResponseCELValidationHandler(g.logger, g.responsePolicyEngine))
	}

	// Add AI response validation handler if enabled
	if g.config.AIResponseValidation.Enabled && g.aiResponsePolicyEngine != nil {
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

	return nil
}

// SetMetricsCollector sets the metrics collector for tracking usage
func (g *Gateway) SetMetricsCollector(collector *metrics.Collector) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.metricsCollector = collector
}

// Stop gracefully shuts down the gateway
func (g *Gateway) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.logger.Info(ctx, "Stopping gateway")

	// Close the stop channel to signal shutdown
	close(g.stopChan)

	// Close all clients
	if g.clientManager != nil {
		if err := g.clientManager.Close(); err != nil {
			g.logger.Error(ctx, "Error closing clients", zap.Error(err))
		}
	}

	return nil
}

// Tool handler function
func (g *Gateway) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Track tool invocation in metrics
	if g.metricsCollector != nil {
		g.metricsCollector.IncrementToolInvocations()
	}

	// Create audit log entry
	auditLog := map[string]interface{}{
		"request": req,
	}

	// Ensure audit log is always written, even on panic
	defer func() {
		if r := recover(); r != nil {
			auditLog["error"] = fmt.Sprintf("panic: %v", r)
			auditLog["status"] = "panic"
			g.auditLogger.Error(ctx, "Tool call audit (panic)", zap.Any("audit", auditLog))
			panic(r) // Re-raise the panic
		}
	}()

	// Validate request through the chain
	validationResults, err := g.ValidateToolCall(ctx, req)
	if err != nil {
		auditLog["error"] = err.Error()
		auditLog["status"] = "validation_error"
		g.auditLogger.Error(ctx, "Tool call audit", zap.Any("audit", auditLog))
		return nil, fmt.Errorf("request validation failed: %v", err)
	}
	g.logger.Debug(ctx, "Validation results", zap.Any("validationResults", validationResults))

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

	// Add validation results to audit log
	auditLog["validation"] = validationResults

	// If validation failed, return structured error with user-friendly message
	if !validationResults.Allowed {
		errorMessage, errorData := g.buildPolicyDeniedError(&validationResults, req.Params.Name)

		auditLog["status"] = "denied"
		g.auditLogger.Warn(ctx, "Tool call audit", zap.Any("audit", auditLog))

		// Return error with proper MCP error code (-32600 for Invalid Request)
		// Note: We'll need to handle this error code in the server layer
		return nil, &PolicyDeniedError{
			Message: errorMessage,
			Data:    errorData,
		}
	}

	// Parse the prefixed tool name to get client name and original tool name
	clientName, originalToolName, err := ParsePrefixedName(req.Params.Name)
	if err != nil {
		auditLog["error"] = err.Error()
		auditLog["status"] = "invalid_tool_name"
		g.auditLogger.Error(ctx, "Tool call audit", zap.Any("audit", auditLog))
		return nil, fmt.Errorf("invalid tool name format: %w", err)
	}

	// Get the appropriate client
	clientInfo, err := g.clientManager.GetClient(clientName)
	if err != nil {
		auditLog["error"] = err.Error()
		auditLog["status"] = "client_not_found"
		g.auditLogger.Error(ctx, "Tool call audit", zap.Any("audit", auditLog))
		return nil, fmt.Errorf("client not found: %w", err)
	}

	// Create a new request with the original tool name
	originalReq := req
	originalReq.Params.Name = originalToolName

	// Call the tool on the appropriate client
	result, err := clientInfo.Client.CallTool(ctx, originalReq)
	if err != nil {
		auditLog["error"] = err.Error()
		auditLog["status"] = "execution_error"
		auditLog["client"] = clientName
		auditLog["original_tool_name"] = originalToolName
		g.auditLogger.Error(ctx, "Tool call audit", zap.Any("audit", auditLog))
		return nil, fmt.Errorf("tool call failed: %w", err)
	}

	// Validate response through the response validation chain
	if g.responseValidationChain != nil {
		responseValidationResults, err := g.responseValidationChain.Handle(ctx, req, result)
		if err != nil {
			g.logger.Error(ctx, "Response validation error", zap.Error(err))
			// Continue even if response validation has errors
		}

		// Add response validation results to audit log
		auditLog["response_validation"] = responseValidationResults

		// If response validation denied the response, return error
		if !responseValidationResults.Allowed {
			auditLog["status"] = "response_denied"
			g.auditLogger.Warn(ctx, "Tool call audit - response denied", zap.Any("audit", auditLog))

			return nil, fmt.Errorf("response denied by policy: %s", responseValidationResults.Message)
		}

		// If response was redacted, update the result
		if responseValidationResults.RedactedContent != nil {
			// Update the first text content item with redacted content
			if len(result.Content) > 0 {
				for i := range result.Content {
					if textContent, ok := result.Content[i].(mcp.TextContent); ok {
						textContent.Text = *responseValidationResults.RedactedContent
						result.Content[i] = textContent
						break
					}
				}
			}
			auditLog["redacted"] = true
		}

		// Add response validation summary to metadata
		if result.Meta == nil {
			result.Meta = &mcp.Meta{}
		}
		if result.Meta.AdditionalFields == nil {
			result.Meta.AdditionalFields = make(map[string]interface{})
		}

		// Create a summary of response validations
		responseValidationSummary := make(map[string]string)
		for _, v := range responseValidationResults.Results {
			if v.PolicyType != "audit" {
				if v.Allowed {
					responseValidationSummary[v.PolicyName] = "passed"
				} else {
					responseValidationSummary[v.PolicyName] = "failed"
				}
			}
		}
		result.Meta.AdditionalFields["response_validation_summary"] = responseValidationSummary
	}

	// Add execution result to audit log
	auditLog["result"] = result
	auditLog["status"] = "success"

	// Add validation summary to the result metadata
	if result.Meta == nil {
		result.Meta = &mcp.Meta{}
	}
	if result.Meta.AdditionalFields == nil {
		result.Meta.AdditionalFields = make(map[string]interface{})
	}

	// Create a summary of all validations
	validationSummary := make(map[string]string)
	for _, v := range validationResults.Results {
		if v.PolicyType != "audit" {
			if v.Allowed {
				validationSummary[v.PolicyName] = "passed"
			} else {
				validationSummary[v.PolicyName] = "failed"
			}
		}
	}
	result.Meta.AdditionalFields["validation_summary"] = validationSummary

	// Log the complete audit entry
	g.auditLogger.Info(ctx, "Tool call audit", zap.Any("audit", auditLog))

	return result, nil
}

// buildPolicyDeniedError creates a user-friendly error message and structured data for policy failures
func (g *Gateway) buildPolicyDeniedError(validationResults *ValidationResults, toolName string) (string, map[string]interface{}) {
	// Create a user-friendly error message
	var deniedPolicies []string
	var deniedMessages []string

	for _, result := range validationResults.Results {
		if !result.Allowed && result.PolicyType != "audit" {
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

	// Get the appropriate client
	clientInfo, err := g.clientManager.GetClient(clientName)
	if err != nil {
		return nil, fmt.Errorf("client not found: %w", err)
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

	// Get the appropriate client
	clientInfo, err := g.clientManager.GetClient(clientName)
	if err != nil {
		return nil, fmt.Errorf("client not found: %w", err)
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
