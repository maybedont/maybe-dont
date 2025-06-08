package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sudermanjr/maybe-dont/internal/config"
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
	// Create audit logger with a specific field to identify it
	auditLogger := logger.With(zap.String("logger", "audit"))

	// Create CEL policy engine
	policyEngine, err := NewCELPolicyEngine(logger, cfg.PolicyValidation.Default)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL policy engine: %w", err)
	}

	aiPolicyEngine := &AIPolicyEngine{
		defaultPolicy: cfg.AIPolicyValidation.Default,
		endpoint:      cfg.AIPolicyValidation.Endpoint,
		model:         cfg.AIPolicyValidation.Model,
		timeout:       cfg.AIPolicyValidation.Timeout,
		maxTokens:     cfg.AIPolicyValidation.MaxTokens,
		apiKey:        cfg.AIPolicyValidation.APIKey,
	}

	// Create AI policy engine
	err = InitAIPolicyEngine(logger, aiPolicyEngine)
	if err != nil {
		return nil, fmt.Errorf("failed to init AI policy engine: %w", err)
	}

	// Load policies from configuration
	if err := policyEngine.LoadPolicies(cfg.PolicyValidation.Rules); err != nil {
		return nil, fmt.Errorf("failed to load policies: %w", err)
	}

	// Load policies from configuration
	if err := aiPolicyEngine.LoadPolicies(cfg.AIPolicyValidation.Rules); err != nil {
		return nil, fmt.Errorf("failed to load policies: %w", err)
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

	// Initialize validation chain
	p.validationChain = NewToolValidationChain(
		NewToolLoggingHandler(p.auditLogger),
		NewToolCELValidationHandler(p.logger, p.policyEngine),
		NewToolAIValidationHandler(p.logger, p.aiPolicyEngine),
	)

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
	// Log the full request including params before validation
	p.logger.Info("Handling tool call request",
		zap.Any("request", req),
	)

	// Validate request through the chain
	validationResults, err := p.ValidateToolCall(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("request validation failed: %w", err)
	}

	// Call the tool
	result, err := p.client.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("tool call failed: %w", err)
	}

	// If validation failed, return error with all validation results
	if !validationResults.Allowed {
		// Convert validation results to JSON for error message
		resultsJSON, err := json.Marshal(validationResults)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal validation results: %w", err)
		}
		return nil, fmt.Errorf("policy validation failed: %s", string(resultsJSON))
	}

	// Add validation summary to the result metadata
	if result.Meta == nil {
		result.Meta = make(map[string]interface{})
	}

	// Create a summary of passed validations
	validationSummary := make(map[string]string)
	for _, v := range validationResults.Results {
		if v.Allowed && v.PolicyType != "audit" {
			validationSummary[v.PolicyName] = "passed"
		}
	}
	result.Meta["validation_summary"] = validationSummary

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
