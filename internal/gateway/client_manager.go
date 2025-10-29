package gateway

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ClientInfo holds a client instance and its metadata
type ClientInfo struct {
	Name         string
	Client       *client.Client
	Config       config.ClientConfig
	Capabilities *mcp.ServerCapabilities
	// RequiresLazyInit indicates if capabilities should be checked per-session
	// This is true for clients with pass-through auth enabled
	RequiresLazyInit bool
	// Detailed capability information
	Tools     []mcp.Tool
	Prompts   []mcp.Prompt
	Resources []mcp.Resource
	Templates []mcp.ResourceTemplate
}

// ClientManager manages multiple MCP client instances
type ClientManager struct {
	clients        map[string]*ClientInfo
	sessionManager *SessionManager
	mu             sync.RWMutex
	logger         *config.SessionLogger
}

// NewClientManager creates a new client manager
func NewClientManager(ctx context.Context, logger *config.SessionLogger) *ClientManager {
	return &ClientManager{
		clients:        make(map[string]*ClientInfo),
		sessionManager: NewSessionManager(),
		logger:         logger,
	}
}

// InitializeClients initializes all configured clients from a map and collects any errors
func (cm *ClientManager) InitializeClients(ctx context.Context, configs map[string]config.ClientConfig) error {
	var errors []string

	for name, cfg := range configs {
		if err := cm.InitializeClient(ctx, name, cfg); err != nil {
			errMsg := fmt.Sprintf("failed to initialize client %s: %v", name, err)
			errors = append(errors, errMsg)
			cm.logger.Error(ctx, "Client initialization failed",
				zap.String("client", name),
				zap.Error(err))
		}
	}

	// Return collected errors if any
	if len(errors) > 0 {
		errMsg := fmt.Sprintf("failed to initialize %d client(s):\n", len(errors))
		for i, err := range errors {
			errMsg += fmt.Sprintf("  %d. %s\n", i+1, err)
		}
		return fmt.Errorf("%s", errMsg)
	}

	return nil
}

// InitializeClient initializes a single client
func (cm *ClientManager) InitializeClient(ctx context.Context, name string, cfg config.ClientConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if client already exists
	if _, exists := cm.clients[name]; exists {
		return fmt.Errorf("client %s already initialized", name)
	}

	clientInfo := &ClientInfo{
		Name:   name,
		Config: cfg,
	}

	// Initialize client based on type
	var cl *client.Client
	var err error

	switch cfg.Type {
	case "stdio":
		cm.logger.Debug(ctx, "Initializing STDIO MCP client",
			zap.String("name", name),
			zap.String("command", cfg.Command),
			zap.Any("command_args", cfg.CommandArgs))
		cl, err = client.NewStdioMCPClient(cfg.Command, nil, cfg.CommandArgs...)
	case "sse":
		cm.logger.Debug(ctx, "Initializing SSE MCP client",
			zap.String("name", name),
			zap.String("downstream_url", cfg.DownstreamURL))

		// Build SSE client options
		sseOpts := []transport.ClientOption{}

		// Add static headers if configured
		if len(cfg.SSEConfig.Headers) > 0 {
			sseOpts = append(sseOpts, client.WithHeaders(cfg.SSEConfig.Headers))
		}

		// Add dynamic auth headers if pass-through is enabled
		if cfg.Auth.PassThrough.Enabled {
			headerFunc := cm.createAuthHeaderFunc(name, cfg)
			sseOpts = append(sseOpts, client.WithHeaderFunc(headerFunc))
			cm.logger.Info(ctx, "Enabled pass-through auth for SSE client",
				zap.String("client", name),
				zap.Int("header_mappings", len(cfg.Auth.PassThrough.Headers)))
		}

		cl, err = client.NewSSEMCPClient(cfg.DownstreamURL, sseOpts...)
	case "http":
		cm.logger.Debug(ctx, "Initializing HTTP MCP client",
			zap.String("name", name),
			zap.String("downstream_url", cfg.DownstreamURL))

		// Build HTTP client options
		httpOpts := []transport.StreamableHTTPCOption{}

		// Add static headers if configured
		if len(cfg.HTTPConfig.Headers) > 0 {
			httpOpts = append(httpOpts, transport.WithHTTPHeaders(cfg.HTTPConfig.Headers))
		}

		// Add dynamic auth headers if pass-through is enabled
		if cfg.Auth.PassThrough.Enabled {
			headerFunc := cm.createAuthHeaderFunc(name, cfg)
			httpOpts = append(httpOpts, transport.WithHTTPHeaderFunc(headerFunc))
			cm.logger.Info(ctx, "Enabled pass-through auth for HTTP client",
				zap.String("client", name),
				zap.Int("header_mappings", len(cfg.Auth.PassThrough.Headers)))
		}

		cl, err = client.NewStreamableHttpClient(cfg.DownstreamURL, httpOpts...)
	default:
		return fmt.Errorf("unsupported transport type: %s", cfg.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	clientInfo.Client = cl

	// For pass-through auth clients, defer capability check until first session
	if cfg.Auth.PassThrough.Enabled {
		clientInfo.RequiresLazyInit = true
		cm.clients[name] = clientInfo
		cm.logger.Info(ctx, "Initialized MCP client with lazy initialization",
			zap.String("name", name),
			zap.Bool("pass_through_auth", true))
		return nil
	}

	// Check capabilities immediately for non-pass-through clients
	if err := cm.checkCapabilities(ctx, clientInfo); err != nil {
		if closeErr := cl.Close(); closeErr != nil {
			cm.logger.Error(ctx, "failed to close client after capability check error",
				zap.String("name", name),
				zap.Error(closeErr))
		}
		return fmt.Errorf("failed to check capabilities: %w", err)
	}

	cm.clients[name] = clientInfo
	cm.logger.Info(ctx, "Initialized MCP client", zap.String("name", name))

	return nil
}

// checkCapabilities checks and stores client capabilities
func (cm *ClientManager) checkCapabilities(ctx context.Context, clientInfo *ClientInfo) error {
	cm.logger.Debug(ctx, "Checking MCP server capabilities", zap.String("client", clientInfo.Name))

	req := &mcp.InitializeRequest{
		Request: mcp.Request{
			Method: "initialize",
		},
		Params: mcp.InitializeParams{
			ProtocolVersion: "1.0",
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    "maybe-dont",
				Version: "0.0.1",
			},
		},
	}

	// Attempt initialization with retry logic for stdio clients
	var resp *mcp.InitializeResult
	var err error

	if clientInfo.Config.Type == "stdio" {
		// Use retry logic for stdio clients to handle startup race conditions
		resp, err = cm.initializeWithRetry(ctx, clientInfo, req)
		if err != nil {
			return fmt.Errorf("failed to initialize MCP server after retries: %w", err)
		}
	} else {
		// For non-stdio clients, use direct initialization
		resp, err = clientInfo.Client.Initialize(ctx, *req)
		if err != nil {
			return fmt.Errorf("failed to initialize MCP server: %w", err)
		}
	}

	clientInfo.Capabilities = &resp.Capabilities
	cm.logger.Debug(ctx, "MCP server capabilities",
		zap.String("client", clientInfo.Name),
		zap.Any("capabilities", resp.Capabilities),
	)

	// Apply capability discovery delay if configured
	if clientInfo.Config.CapabilityDiscoveryDelayMs > 0 {
		delay := time.Duration(clientInfo.Config.CapabilityDiscoveryDelayMs) * time.Millisecond
		cm.logger.Debug(ctx, "Waiting before capability discovery",
			zap.String("client", clientInfo.Name),
			zap.Duration("delay", delay),
		)
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during capability discovery delay: %w", ctx.Err())
		case <-time.After(delay):
			// Continue after delay
		}
	}

	// Discover capabilities with retry logic
	if err := cm.discoverCapabilities(ctx, clientInfo); err != nil {
		return fmt.Errorf("failed to discover capabilities: %w", err)
	}

	return nil
}

// CheckCapabilitiesForSession checks capabilities for a client in a specific session
// This is used for lazy initialization of pass-through auth clients
// Returns the ClientInfo with loaded capabilities
func (cm *ClientManager) CheckCapabilitiesForSession(ctx context.Context, clientName, requestID string) (*ClientInfo, error) {
	cm.mu.RLock()
	clientInfo, exists := cm.clients[clientName]
	cm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("client %s not found", clientName)
	}

	// Check if capabilities already loaded for this session
	if cm.sessionManager.HasClientCapabilities(requestID, clientName) {
		cm.logger.Debug(ctx, "Capabilities already loaded for session",
			zap.String("client", clientName))
		// Return nil to indicate already loaded
		return nil, nil
	}

	cm.logger.Info(ctx, "Checking capabilities for session",
		zap.String("request_id", requestID),
		zap.String("client", clientName))

	// Use a temporary ClientInfo to avoid modifying the shared instance
	sessionClientInfo := &ClientInfo{
		Name:   clientInfo.Name,
		Client: clientInfo.Client,
		Config: clientInfo.Config,
	}

	// Check capabilities with the session context (which contains credentials)
	if err := cm.checkCapabilities(ctx, sessionClientInfo); err != nil {
		return nil, fmt.Errorf("failed to check capabilities for session: %w", err)
	}

	// Store capabilities in session manager
	cm.sessionManager.SetClientCapabilities(requestID, clientName, sessionClientInfo.Capabilities)

	cm.logger.Info(ctx, "Successfully loaded capabilities for session",
		zap.String("request_id", requestID),
		zap.String("client", clientName),
		zap.Int("tools", len(sessionClientInfo.Tools)),
		zap.Int("prompts", len(sessionClientInfo.Prompts)),
		zap.Int("resources", len(sessionClientInfo.Resources)))

	return sessionClientInfo, nil
}

// GetLazyInitClients returns a list of client names that require lazy initialization
func (cm *ClientManager) GetLazyInitClients() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var lazyClients []string
	for name, info := range cm.clients {
		if info.RequiresLazyInit {
			lazyClients = append(lazyClients, name)
		}
	}
	return lazyClients
}

// discoverCapabilities discovers tools, prompts, and resources with retry logic
func (cm *ClientManager) discoverCapabilities(ctx context.Context, clientInfo *ClientInfo) error {
	// Get tools if available (try regardless of ListChanged for stdio clients)
	if clientInfo.Capabilities.Tools != nil {
		tools, err := cm.discoverToolsWithRetry(ctx, clientInfo)
		if err != nil {
			return fmt.Errorf("failed to discover tools: %w", err)
		}
		clientInfo.Tools = tools
		cm.logger.Debug(ctx, "Tool discovery completed",
			zap.String("client", clientInfo.Name),
			zap.Int("tools_count", len(tools)),
		)
	}

	// Get prompts if available
	if clientInfo.Capabilities.Prompts != nil && clientInfo.Capabilities.Prompts.ListChanged {
		prompts, err := cm.discoverPromptsWithRetry(ctx, clientInfo)
		if err != nil {
			return fmt.Errorf("failed to discover prompts: %w", err)
		}
		clientInfo.Prompts = prompts
	}

	// Get resources if available
	if clientInfo.Capabilities.Resources != nil && clientInfo.Capabilities.Resources.ListChanged {
		resources, err := cm.discoverResourcesWithRetry(ctx, clientInfo)
		if err != nil {
			return fmt.Errorf("failed to discover resources: %w", err)
		}
		clientInfo.Resources = resources

		// Also list resource templates if available
		templates, err := cm.discoverResourceTemplatesWithRetry(ctx, clientInfo)
		if err != nil {
			return fmt.Errorf("failed to discover resource templates: %w", err)
		}
		clientInfo.Templates = templates
	}

	return nil
}

// retryWithDelay executes a function with retry logic
func retryWithDelay[T any](
	ctx context.Context,
	logger *config.SessionLogger,
	clientInfo *ClientInfo,
	operation string,
	fn func() (T, error),
	validateResult func(T, int) bool,
) (T, error) {
	cfg := clientInfo.Config
	maxRetries := cfg.CapabilityDiscoveryRetries
	retryDelay := time.Duration(cfg.CapabilityRetryDelayMs) * time.Millisecond

	// Log the start of the operation
	logger.Debug(ctx, "Starting "+operation+" with retry logic",
		zap.String("client", clientInfo.Name),
		zap.Int("max_retries", maxRetries),
		zap.Duration("retry_delay", retryDelay),
	)

	var lastErr error
	var zeroValue T
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := fn()
		if err != nil {
			lastErr = err
			if operation == "tool discovery" {
				titleCaser := cases.Title(language.English)
				logger.Debug(ctx, titleCaser.String(operation)+" attempt failed",
					zap.String("client", clientInfo.Name),
					zap.Int("attempt", attempt+1),
					zap.Error(err),
				)
			}
		} else {
			// If we have a validator, use it
			if validateResult != nil && !validateResult(result, attempt) {
				// Special case for tools: log and potentially retry
				if operation == "tool discovery" {
					logger.Debug(ctx, "Tool discovery returned empty list, retrying",
						zap.String("client", clientInfo.Name),
						zap.Int("attempt", attempt+1),
					)
				}
			} else {
				// Success
				if operation == "tool discovery" {
					// Use title case for operation name
					titleCaser := cases.Title(language.English)
					logger.Debug(ctx, titleCaser.String(operation)+" successful",
						zap.String("client", clientInfo.Name),
						zap.Int("attempt", attempt+1),
					)
				}
				return result, nil
			}
		}

		// Wait before retry (except on last attempt)
		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return zeroValue, fmt.Errorf("context cancelled during %s retry: %w", operation, ctx.Err())
			case <-time.After(retryDelay):
				// Continue to next attempt
			}
		}
	}

	if lastErr != nil {
		return zeroValue, lastErr
	}
	return zeroValue, nil // Return zero value if no error but no valid result
}

// discoverToolsWithRetry discovers tools with retry logic for stdio clients
func (cm *ClientManager) discoverToolsWithRetry(ctx context.Context, clientInfo *ClientInfo) ([]mcp.Tool, error) {
	return retryWithDelay(ctx, cm.logger, clientInfo, "tool discovery",
		func() ([]mcp.Tool, error) {
			toolsReq := &mcp.ListToolsRequest{
				PaginatedRequest: mcp.PaginatedRequest{
					Request: mcp.Request{
						Method: "tools/list",
					},
					Params: mcp.PaginatedParams{},
				},
			}
			toolsResp, err := clientInfo.Client.ListTools(ctx, *toolsReq)
			if err != nil {
				return nil, err
			}
			return toolsResp.Tools, nil
		},
		func(tools []mcp.Tool, attempt int) bool {
			// Validate: continue if we have tools OR if this is the last attempt
			if len(tools) > 0 || attempt == clientInfo.Config.CapabilityDiscoveryRetries {
				// Log success with tool count
				cm.logger.Debug(ctx, "Tool discovery successful",
					zap.String("client", clientInfo.Name),
					zap.Int("attempt", attempt+1),
					zap.Int("tools_count", len(tools)),
				)
				return true
			}
			return false
		},
	)
}

// discoverPromptsWithRetry discovers prompts with retry logic for stdio clients
func (cm *ClientManager) discoverPromptsWithRetry(ctx context.Context, clientInfo *ClientInfo) ([]mcp.Prompt, error) {
	return retryWithDelay(ctx, cm.logger, clientInfo, "prompt discovery",
		func() ([]mcp.Prompt, error) {
			promptsReq := &mcp.ListPromptsRequest{
				PaginatedRequest: mcp.PaginatedRequest{
					Request: mcp.Request{
						Method: "prompts/list",
					},
					Params: mcp.PaginatedParams{},
				},
			}
			promptsResp, err := clientInfo.Client.ListPrompts(ctx, *promptsReq)
			if err != nil {
				return nil, err
			}
			return promptsResp.Prompts, nil
		},
		nil, // No special validation needed for prompts
	)
}

// discoverResourcesWithRetry discovers resources with retry logic for stdio clients
func (cm *ClientManager) discoverResourcesWithRetry(ctx context.Context, clientInfo *ClientInfo) ([]mcp.Resource, error) {
	return retryWithDelay(ctx, cm.logger, clientInfo, "resource discovery",
		func() ([]mcp.Resource, error) {
			resourcesReq := &mcp.ListResourcesRequest{
				PaginatedRequest: mcp.PaginatedRequest{
					Request: mcp.Request{
						Method: "resources/list",
					},
					Params: mcp.PaginatedParams{},
				},
			}
			resourcesResp, err := clientInfo.Client.ListResources(ctx, *resourcesReq)
			if err != nil {
				return nil, err
			}
			return resourcesResp.Resources, nil
		},
		nil, // No special validation needed for resources
	)
}

// discoverResourceTemplatesWithRetry discovers resource templates with retry logic for stdio clients
func (cm *ClientManager) discoverResourceTemplatesWithRetry(ctx context.Context, clientInfo *ClientInfo) ([]mcp.ResourceTemplate, error) {
	return retryWithDelay(ctx, cm.logger, clientInfo, "resource template discovery",
		func() ([]mcp.ResourceTemplate, error) {
			templatesReq := &mcp.ListResourceTemplatesRequest{
				PaginatedRequest: mcp.PaginatedRequest{
					Request: mcp.Request{
						Method: "resources/templates/list",
					},
					Params: mcp.PaginatedParams{},
				},
			}
			templatesResp, err := clientInfo.Client.ListResourceTemplates(ctx, *templatesReq)
			if err != nil {
				return nil, err
			}
			return templatesResp.ResourceTemplates, nil
		},
		nil, // No special validation needed for resource templates
	)
}

// initializeWithRetry attempts to initialize an MCP client with exponential backoff retry logic
func (cm *ClientManager) initializeWithRetry(ctx context.Context, clientInfo *ClientInfo, req *mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	cfg := clientInfo.Config
	maxRetries := cfg.InitializationRetries
	baseDelay := time.Duration(cfg.RetryDelayMs) * time.Millisecond
	timeout := time.Duration(cfg.StartupTimeoutMs) * time.Millisecond

	cm.logger.Debug(ctx, "Starting MCP initialization with retry logic",
		zap.String("client", clientInfo.Name),
		zap.Int("max_retries", maxRetries),
		zap.Duration("base_delay", baseDelay),
		zap.Duration("timeout", timeout),
	)

	// Create a context with timeout for the entire operation
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Log attempt
		if attempt == 0 {
			cm.logger.Debug(timeoutCtx, "Attempting MCP initialization",
				zap.String("client", clientInfo.Name),
				zap.Int("attempt", attempt+1),
			)
		} else {
			cm.logger.Debug(timeoutCtx, "Retrying MCP initialization",
				zap.String("client", clientInfo.Name),
				zap.Int("attempt", attempt+1),
				zap.Int("max_attempts", maxRetries+1),
			)
		}

		// Attempt initialization
		resp, err := clientInfo.Client.Initialize(timeoutCtx, *req)
		if err == nil {
			cm.logger.Debug(timeoutCtx, "MCP initialization successful",
				zap.String("client", clientInfo.Name),
				zap.Int("attempt", attempt+1),
			)
			return resp, nil
		}

		lastErr = err

		// Check if we've exhausted all retries
		if attempt == maxRetries {
			cm.logger.Error(timeoutCtx, "MCP initialization failed after all retries",
				zap.String("client", clientInfo.Name),
				zap.Int("attempts", attempt+1),
				zap.Error(err),
			)
			break
		}

		// Check if context was cancelled (timeout)
		if timeoutCtx.Err() != nil {
			cm.logger.Error(timeoutCtx, "MCP initialization timed out",
				zap.String("client", clientInfo.Name),
				zap.Duration("timeout", timeout),
				zap.Error(timeoutCtx.Err()),
			)
			return nil, fmt.Errorf("initialization timed out after %v: %w", timeout, timeoutCtx.Err())
		}

		// Calculate exponential backoff delay (with max cap of 2 seconds)
		delay := min(baseDelay*time.Duration(1<<uint(attempt)), 2*time.Second)

		cm.logger.Debug(timeoutCtx, "Waiting before retry",
			zap.String("client", clientInfo.Name),
			zap.Duration("delay", delay),
			zap.Error(err),
		)

		// Wait before next attempt
		select {
		case <-timeoutCtx.Done():
			return nil, fmt.Errorf("initialization timed out during retry delay: %w", timeoutCtx.Err())
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return nil, fmt.Errorf("initialization failed after %d attempts: %w", maxRetries+1, lastErr)
}

// GetClient returns a client by name
func (cm *ClientManager) GetClient(name string) (*ClientInfo, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	client, exists := cm.clients[name]
	if !exists {
		return nil, fmt.Errorf("client not found: %s", name)
	}

	return client, nil
}

// GetAllClients returns all client information
func (cm *ClientManager) GetAllClients() map[string]*ClientInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]*ClientInfo)
	maps.Copy(result, cm.clients)
	return result
}

// Close closes all managed clients
func (cm *ClientManager) Close() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var errs []error
	for name, clientInfo := range cm.clients {
		if err := clientInfo.Client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close client %s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// ParsePrefixedName parses a prefixed name (e.g., "aws__list_files") into client name and original name
func ParsePrefixedName(prefixedName string) (clientName, originalName string, err error) {
	parts := strings.SplitN(prefixedName, "__", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid prefixed name format: %s", prefixedName)
	}
	return parts[0], parts[1], nil
}

// PrefixName creates a prefixed name from client name and original name
func PrefixName(clientName, originalName string) string {
	return fmt.Sprintf("%s__%s", clientName, originalName)
}

// createAuthHeaderFunc creates a header function for pass-through authentication
// This function will be called on each HTTP/SSE request to inject user credentials from context
func (cm *ClientManager) createAuthHeaderFunc(clientName string, cfg config.ClientConfig) transport.HTTPHeaderFunc {
	return func(ctx context.Context) map[string]string {
		headers := make(map[string]string)

		// Get client credentials from context
		clientCreds, ok := GetServiceCredentials(ctx, clientName)
		if !ok {
			cm.logger.Debug(ctx, "No credentials found for client in context",
				zap.String("client", clientName))
			return headers
		}

		// Apply each header mapping
		for _, mapping := range cfg.Auth.PassThrough.Headers {
			// Get credential value from context (keyed by target_header)
			value, ok := clientCreds.GetHeader(mapping.TargetHeader)
			if !ok {
				cm.logger.Debug(ctx, "Missing credential for header mapping",
					zap.String("client", clientName),
					zap.String("target_header", mapping.TargetHeader))
				continue
			}

			// Format value using template
			headerValue := formatCredentialValue(mapping.Format, value)
			headers[mapping.TargetHeader] = headerValue

			cm.logger.Debug(ctx, "Injected auth header for downstream",
				zap.String("client", clientName),
				zap.String("target_header", mapping.TargetHeader))
		}

		return headers
	}
}

// formatCredentialValue formats a credential value using a template
// Template can use {value} as a placeholder.
// If template is empty, returns raw value (default behavior for simple passthrough).
// Examples: "Bearer {value}", "sha256={value}", "" (raw value)
func formatCredentialValue(template string, value string) string {
	if template == "" {
		return value // Default: raw value passthrough
	}
	return strings.ReplaceAll(template, "{value}", value)
}
