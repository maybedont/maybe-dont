package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// Proxy represents an MCP security proxy instance
type Proxy struct {
	logger       *zap.Logger
	auditLogger  *zap.Logger
	config       *config.Config
	server       *server.MCPServer
	client       *client.Client
	mu           sync.RWMutex
	capabilities *mcp.ServerCapabilities
	stopChan     chan struct{}
	// Detailed capability information
	capabilityDetails struct {
		Tools     []mcp.Tool
		Prompts   []mcp.Prompt
		Resources []mcp.Resource
		Templates []mcp.ResourceTemplate
	}
	// Validation chain for request processing
	validationChain *ToolValidationChain
	// CEL policy engine
	policyEngine *CELPolicyEngine
	// AI policy engine
	aiPolicyEngine *AIPolicyEngine
}

// New creates a new proxy instance
func New(cfg *config.Config, logger *zap.Logger) (*Proxy, error) {
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
		logger.Info("Initializing CEL policy engine")
		policyEngine, err = NewCELPolicyEngine(logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL policy engine: %w", err)
		}

		// Load policies from configuration
		if err := policyEngine.LoadPolicies(cfg.PolicyValidation.Rules); err != nil {
			return nil, fmt.Errorf("failed to load CEL policies: %w", err)
		}
	} else {
		logger.Info("CEL policy validation is disabled")
	}

	// Initialize AI policy engine only if enabled
	if cfg.AIPolicyValidation.Enabled {
		logger.Info("Initializing AI policy engine")
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

		// Load policies from configuration
		if err := aiPolicyEngine.LoadPolicies(cfg.AIPolicyValidation.Rules); err != nil {
			return nil, fmt.Errorf("failed to load AI policies: %w", err)
		}
	} else {
		logger.Info("AI policy validation is disabled")
	}

	return &Proxy{
		logger:         logger,
		auditLogger:    auditLogger,
		config:         cfg,
		stopChan:       make(chan struct{}),
		policyEngine:   policyEngine,
		aiPolicyEngine: aiPolicyEngine,
	}, nil
}

// Start initializes and starts the proxy
func (p *Proxy) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Debug print the loaded config
	if cfgBytes, err := json.MarshalIndent(p.config, "", "  "); err == nil {
		p.logger.Debug("Loaded proxy config", zap.String("config", string(cfgBytes)))
	} else {
		p.logger.Warn("Failed to marshal config for debug print", zap.Error(err))
	}

	// Initialize validation chain with required handlers
	handlers := []ToolValidationHandler{
		NewToolLoggingHandler(p.auditLogger),
	}

	// Add CEL validation handler if enabled
	if p.config.PolicyValidation.Enabled && p.policyEngine != nil {
		p.logger.Info("Adding CEL validation handler to chain")
		handlers = append(handlers, NewToolCELValidationHandler(p.logger, p.policyEngine))
	}

	// Add AI validation handler if enabled
	if p.config.AIPolicyValidation.Enabled && p.aiPolicyEngine != nil {
		p.logger.Info("Adding AI validation handler to chain")
		handlers = append(handlers, NewToolAIValidationHandler(p.logger, p.aiPolicyEngine))
	}

	p.validationChain = NewToolValidationChain(handlers...)

	// Initialize downstream client based on transport type
	if err := p.initDownstreamClient(ctx); err != nil {
		return fmt.Errorf("failed to initialize downstream client: %w", err)
	}

	// Initialize server based on server type
	if err := p.initServer(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the proxy
func (p *Proxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger.Info("Stopping proxy")

	// Close the stop channel to signal shutdown
	close(p.stopChan)

	// Close the client if it exists
	if p.client != nil {
		if err := p.client.Close(); err != nil {
			p.logger.Error("Error closing client", zap.Error(err))
		}
	}

	return nil
}

// Tool handler function
func (p *Proxy) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Create audit log entry
	auditLog := map[string]interface{}{
		"request": req,
	}

	// Validate request through the chain
	validationResults, err := p.ValidateToolCall(ctx, req)
	if err != nil {
		auditLog["error"] = err.Error()
		auditLog["status"] = "validation_error"
		p.auditLogger.Error("Tool call audit", zap.Any("audit", auditLog))
		return nil, fmt.Errorf("request validation failed: %v", err)
	}
	p.logger.Debug("Validation results", zap.Any("validationResults", validationResults))

	if validationResults.DenyCount > 0 {
		validationResults.Allowed = false
		validationResults.Message = "Maybe Don't, A policy failed."
	} else {
		validationResults.Allowed = true
		validationResults.Message = "All policies passed, maybe do."
	}

	// Add validation results to audit log
	auditLog["validation"] = validationResults

	// If validation failed, return error with all validation results
	if !validationResults.Allowed {
		// Convert validation results to JSON for error message
		resultsJSON, err := json.Marshal(validationResults)
		if err != nil {
			auditLog["error"] = fmt.Sprintf("failed to marshal validation results: %v", err)
			auditLog["status"] = "marshal_error"
			p.auditLogger.Error("Tool call audit", zap.Any("audit", auditLog))
			return nil, fmt.Errorf("failed to marshal validation results: %w", err)
		}
		auditLog["status"] = "denied"
		p.auditLogger.Warn("Tool call audit", zap.Any("audit", auditLog))
		return nil, fmt.Errorf("Maybe Don't: %s", string(resultsJSON))
	}

	// Call the tool
	result, err := p.client.CallTool(ctx, req)
	if err != nil {
		auditLog["error"] = err.Error()
		auditLog["status"] = "execution_error"
		p.auditLogger.Error("Tool call audit", zap.Any("audit", auditLog))
		return nil, fmt.Errorf("tool call failed: %w", err)
	}

	// Add execution result to audit log
	auditLog["result"] = result
	auditLog["status"] = "success"

	// Add validation summary to the result metadata
	if result.Meta == nil {
		result.Meta = make(map[string]interface{})
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
	result.Meta["validation_summary"] = validationSummary

	// Log the complete audit entry
	p.auditLogger.Info("Tool call audit", zap.Any("audit", auditLog))

	return result, nil
}

// Prompt handler function
func (p *Proxy) HandlePromptCall(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return p.client.GetPrompt(ctx, req)
}

// Resource handler function
func (p *Proxy) HandleResourceCall(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	result, err := p.client.ReadResource(ctx, req)
	if err != nil {
		return nil, err
	}
	return result.Contents, nil
}

// Resource template handler function
func (p *Proxy) HandleResourceTemplateCall(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	result, err := p.client.ReadResource(ctx, req)
	if err != nil {
		return nil, err
	}
	return result.Contents, nil
}
