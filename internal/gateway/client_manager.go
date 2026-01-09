package gateway

import (
	"context"
	"fmt"
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

// ClientManager manages multiple MCP client instances
type ClientManager struct {
	// clientConfigs stores the configuration for each downstream client
	// These are used as templates to create per-session clients
	clientConfigs map[string]config.ClientConfig
	// sessionManager manages per-session downstream client instances
	sessionManager *SessionManager
	mu             sync.RWMutex
	logger         *config.SessionLogger
}

// NewClientManager creates a new client manager
func NewClientManager(ctx context.Context, logger *config.SessionLogger) *ClientManager {
	return &ClientManager{
		clientConfigs:  make(map[string]config.ClientConfig),
		sessionManager: NewSessionManager(logger),
		logger:         logger,
	}
}

// InitializeClients stores client configurations for later per-session instantiation
func (cm *ClientManager) InitializeClients(ctx context.Context, configs map[string]config.ClientConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for name, cfg := range configs {
		cm.clientConfigs[name] = cfg
		cm.logger.Info(ctx, "Registered client configuration",
			zap.String("name", name),
			zap.String("type", cfg.Type))
	}

	return nil
}

// DiscoveredCapabilities holds the discovered tools, prompts, and resources from all clients
type DiscoveredCapabilities struct {
	Tools     map[string][]mcp.Tool             // clientName -> tools
	Prompts   map[string][]mcp.Prompt           // clientName -> prompts
	Resources map[string][]mcp.Resource         // clientName -> resources
	Templates map[string][]mcp.ResourceTemplate // clientName -> templates
}

// DiscoverAllCapabilities creates temporary probe connections to all configured clients,
// discovers their capabilities (tools, prompts, resources), and then closes the connections.
// This is used at startup to register tools globally on the MCP server.
func (cm *ClientManager) DiscoverAllCapabilities(ctx context.Context) (*DiscoveredCapabilities, error) {
	cm.mu.RLock()
	configs := make(map[string]config.ClientConfig)
	for k, v := range cm.clientConfigs {
		configs[k] = v
	}
	cm.mu.RUnlock()

	cm.logger.Info(ctx, "Discovering capabilities from all downstream clients",
		zap.Int("client_count", len(configs)))

	discovered := &DiscoveredCapabilities{
		Tools:     make(map[string][]mcp.Tool),
		Prompts:   make(map[string][]mcp.Prompt),
		Resources: make(map[string][]mcp.Resource),
		Templates: make(map[string][]mcp.ResourceTemplate),
	}

	var errs []string
	var skippedPassThrough []string
	for name, cfg := range configs {
		// Skip probing for clients with pass-through auth enabled
		// These clients require upstream credentials which aren't available at startup
		if cfg.Auth.PassThrough.Enabled {
			cm.logger.Info(ctx, "Skipping probe for pass-through auth client (requires upstream credentials)",
				zap.String("client", name))
			skippedPassThrough = append(skippedPassThrough, name)
			continue
		}

		// Create a temporary probe client
		clientInfo, err := cm.createClient(ctx, name, cfg)
		if err != nil {
			errMsg := fmt.Sprintf("failed to probe client %s: %v", name, err)
			errs = append(errs, errMsg)
			cm.logger.Error(ctx, "Probe client creation failed",
				zap.String("client", name),
				zap.Error(err))
			continue
		}

		// Store discovered capabilities
		discovered.Tools[name] = clientInfo.Tools
		discovered.Prompts[name] = clientInfo.Prompts
		discovered.Resources[name] = clientInfo.Resources
		discovered.Templates[name] = clientInfo.Templates

		cm.logger.Info(ctx, "Discovered capabilities from client",
			zap.String("client", name),
			zap.Int("tools", len(clientInfo.Tools)),
			zap.Int("prompts", len(clientInfo.Prompts)),
			zap.Int("resources", len(clientInfo.Resources)),
			zap.Int("templates", len(clientInfo.Templates)))

		// Close the probe client
		if clientInfo.Client != nil {
			if err := clientInfo.Client.Close(); err != nil {
				cm.logger.Warn(ctx, "Failed to close probe client",
					zap.String("client", name),
					zap.Error(err))
			}
		}
	}

	if len(errs) > 0 {
		cm.logger.Warn(ctx, "Some clients failed during capability discovery",
			zap.Int("failed_count", len(errs)),
			zap.Int("total_count", len(configs)))
		// Don't return error - continue with successfully discovered capabilities
	}

	cm.logger.Info(ctx, "Capability discovery complete",
		zap.Int("clients_discovered", len(discovered.Tools)),
		zap.Int("clients_skipped_passthrough", len(skippedPassThrough)))

	return discovered, nil
}

// SessionDiscoveryResult contains information about tools discovered during session creation
type SessionDiscoveryResult struct {
	// DownstreamClients contains newly discovered clients
	DownstreamClients map[string]*SessionClientInfo
}

// CreateSessionClients creates downstream client instances for a new upstream session
// Returns discovery results for pass-through clients that need session-specific tool registration
func (cm *ClientManager) CreateSessionClients(ctx context.Context, sessionID string) (*SessionDiscoveryResult, error) {
	cm.mu.RLock()
	configs := make(map[string]config.ClientConfig)
	for k, v := range cm.clientConfigs {
		configs[k] = v
	}
	cm.mu.RUnlock()

	cm.logger.Info(ctx, "Creating downstream clients for session",
		zap.String("session_id", sessionID),
		zap.Int("client_count", len(configs)))

	// Create a session first
	cm.sessionManager.CreateSession(sessionID)

	result := &SessionDiscoveryResult{
		DownstreamClients: make(map[string]*SessionClientInfo),
	}

	var errs []string
	for name, cfg := range configs {
		clientInfo, err := cm.createClient(ctx, name, cfg)
		if err != nil {
			errMsg := fmt.Sprintf("failed to create client %s: %v", name, err)
			errs = append(errs, errMsg)
			cm.logger.Error(ctx, "Client creation failed for session",
				zap.String("session_id", sessionID),
				zap.String("client", name),
				zap.Error(err))
			continue
		}

		// Store client in session
		cm.sessionManager.SetSessionClient(sessionID, name, clientInfo)
		cm.logger.Debug(ctx, "Created downstream client for session",
			zap.String("session_id", sessionID),
			zap.String("client", name))

		// Track clients
		result.DownstreamClients[name] = clientInfo

		if cfg.Auth.PassThrough.Enabled && len(clientInfo.Tools) > 0 {
			cm.logger.Debug(ctx, "Discovered tools from pass-through client for session",
				zap.String("session_id", sessionID),
				zap.String("client", name),
				zap.Int("tools_count", len(clientInfo.Tools)))
		}
	}

	if len(errs) > 0 {
		return result, fmt.Errorf("failed to create %d client(s) for session %s", len(errs), sessionID)
	}

	cm.logger.Info(ctx, "Successfully created all downstream clients for session",
		zap.String("session_id", sessionID))
	return result, nil
}

// CreateSingleSessionClient creates a single downstream client for an existing session.
// This is used for on-demand discovery of pass-through clients that weren't connected at session creation.
func (cm *ClientManager) CreateSingleSessionClient(ctx context.Context, sessionID, clientName string, cfg config.ClientConfig) (*SessionClientInfo, error) {
	cm.logger.Info(ctx, "Creating single downstream client for session",
		zap.String("session_id", sessionID),
		zap.String("client", clientName))

	// Ensure session exists
	session, exists := cm.sessionManager.GetSession(sessionID)
	if !exists {
		// Create session if it doesn't exist (shouldn't normally happen)
		cm.sessionManager.CreateSession(sessionID)
	} else {
		// Check if client already exists in session
		if existingClient, ok := session.GetClient(clientName); ok && existingClient != nil && existingClient.Client != nil {
			cm.logger.Debug(ctx, "Client already exists for session",
				zap.String("session_id", sessionID),
				zap.String("client", clientName))
			return existingClient, nil
		}
	}

	// Create the client
	clientInfo, err := cm.createClient(ctx, clientName, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client %s: %w", clientName, err)
	}

	// Store client in session
	cm.sessionManager.SetSessionClient(sessionID, clientName, clientInfo)
	cm.logger.Info(ctx, "Created downstream client for session",
		zap.String("session_id", sessionID),
		zap.String("client", clientName),
		zap.Int("tools_count", len(clientInfo.Tools)))

	return clientInfo, nil
}

// createClient creates a single downstream client instance
func (cm *ClientManager) createClient(ctx context.Context, name string, cfg config.ClientConfig) (*SessionClientInfo, error) {
	clientInfo := &SessionClientInfo{
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
		return nil, fmt.Errorf("unsupported transport type: %s", cfg.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create MCP client: %w", err)
	}

	clientInfo.Client = cl

	// Check capabilities
	if err := cm.checkSessionClientCapabilities(ctx, clientInfo); err != nil {
		if closeErr := cl.Close(); closeErr != nil {
			cm.logger.Error(ctx, "failed to close client after capability check error",
				zap.String("name", name),
				zap.Error(closeErr))
		}
		return nil, fmt.Errorf("failed to check capabilities: %w", err)
	}

	cm.logger.Info(ctx, "Initialized MCP client", zap.String("name", name))
	return clientInfo, nil
}

// CloseSessionClients closes all downstream clients for a session
func (cm *ClientManager) CloseSessionClients(ctx context.Context, sessionID string) error {
	return cm.sessionManager.DeleteSession(ctx, sessionID)
}

// checkSessionClientCapabilities checks and stores capabilities for a session client
func (cm *ClientManager) checkSessionClientCapabilities(ctx context.Context, clientInfo *SessionClientInfo) error {
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
		resp, err = cm.initializeSessionClientWithRetry(ctx, clientInfo, req)
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
	if err := cm.discoverSessionClientCapabilities(ctx, clientInfo); err != nil {
		return fmt.Errorf("failed to discover capabilities: %w", err)
	}

	return nil
}

// GetSessionClient retrieves a downstream client for a specific session
func (cm *ClientManager) GetSessionClient(sessionID, clientName string) (*SessionClientInfo, error) {
	clientInfo, ok := cm.sessionManager.GetSessionClient(sessionID, clientName)
	if !ok {
		return nil, fmt.Errorf("client %s not found for session %s", clientName, sessionID)
	}
	return clientInfo, nil
}

// GetAllSessionClients retrieves all downstream clients for a session
func (cm *ClientManager) GetAllSessionClients(sessionID string) (map[string]*SessionClientInfo, error) {
	session, ok := cm.sessionManager.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return session.GetAllClients(), nil
}

// GetClientConfigs returns all client configurations (for capability reporting)
func (cm *ClientManager) GetClientConfigs() map[string]config.ClientConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	result := make(map[string]config.ClientConfig)
	for k, v := range cm.clientConfigs {
		result[k] = v
	}
	return result
}

// SetSessionClientIP sets the client IP for a session
func (cm *ClientManager) SetSessionClientIP(sessionID, clientIP string) {
	session, ok := cm.sessionManager.GetSession(sessionID)
	if !ok {
		return
	}
	session.SetClientIP(clientIP)
}

// GetActiveSessions returns information about all active sessions
func (cm *ClientManager) GetActiveSessions() []SessionInfo {
	sessionIDs := cm.sessionManager.GetAllSessions()
	sessions := make([]SessionInfo, 0, len(sessionIDs))

	for _, sessionID := range sessionIDs {
		session, ok := cm.sessionManager.GetSession(sessionID)
		if !ok {
			continue
		}

		// Get downstream client names for this session
		clients := session.GetAllClients()
		clientNames := make([]string, 0, len(clients))
		for clientName := range clients {
			clientNames = append(clientNames, clientName)
		}

		sessions = append(sessions, SessionInfo{
			SessionID:       sessionID,
			ClientIP:        session.GetClientIP(),
			DownstreamNames: clientNames,
		})
	}

	return sessions
}

// GetSessionClientTools returns the tools discovered for each client in a session
func (cm *ClientManager) GetSessionClientTools(sessionID string) []SessionClientTools {
	session, ok := cm.sessionManager.GetSession(sessionID)
	if !ok {
		return nil
	}

	clients := session.GetAllClients()
	result := make([]SessionClientTools, 0, len(clients))

	for clientName, clientInfo := range clients {
		toolNames := make([]string, 0, len(clientInfo.Tools))
		for _, tool := range clientInfo.Tools {
			toolNames = append(toolNames, tool.Name)
		}

		result = append(result, SessionClientTools{
			ClientName: clientName,
			Tools:      toolNames,
		})
	}

	return result
}

// discoverSessionClientCapabilities discovers tools, prompts, and resources for a session client
func (cm *ClientManager) discoverSessionClientCapabilities(ctx context.Context, clientInfo *SessionClientInfo) error {
	// Get tools if available (try regardless of ListChanged for stdio clients)
	if clientInfo.Capabilities.Tools != nil {
		tools, err := cm.discoverSessionToolsWithRetry(ctx, clientInfo)
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
		prompts, err := cm.discoverSessionPromptsWithRetry(ctx, clientInfo)
		if err != nil {
			return fmt.Errorf("failed to discover prompts: %w", err)
		}
		clientInfo.Prompts = prompts
	}

	// Get resources if available
	if clientInfo.Capabilities.Resources != nil && clientInfo.Capabilities.Resources.ListChanged {
		resources, err := cm.discoverSessionResourcesWithRetry(ctx, clientInfo)
		if err != nil {
			return fmt.Errorf("failed to discover resources: %w", err)
		}
		clientInfo.Resources = resources

		// Also list resource templates if available
		templates, err := cm.discoverSessionResourceTemplatesWithRetry(ctx, clientInfo)
		if err != nil {
			return fmt.Errorf("failed to discover resource templates: %w", err)
		}
		clientInfo.Templates = templates
	}

	return nil
}

// sessionRetryWithDelay executes a function with retry logic for session clients
func sessionRetryWithDelay[T any](
	ctx context.Context,
	logger *config.SessionLogger,
	clientInfo *SessionClientInfo,
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

// discoverSessionToolsWithRetry discovers tools with retry logic for session clients
func (cm *ClientManager) discoverSessionToolsWithRetry(ctx context.Context, clientInfo *SessionClientInfo) ([]mcp.Tool, error) {
	return sessionRetryWithDelay(ctx, cm.logger, clientInfo, "tool discovery",
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

// discoverSessionPromptsWithRetry discovers prompts with retry logic for session clients
func (cm *ClientManager) discoverSessionPromptsWithRetry(ctx context.Context, clientInfo *SessionClientInfo) ([]mcp.Prompt, error) {
	return sessionRetryWithDelay(ctx, cm.logger, clientInfo, "prompt discovery",
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

// discoverSessionResourcesWithRetry discovers resources with retry logic for session clients
func (cm *ClientManager) discoverSessionResourcesWithRetry(ctx context.Context, clientInfo *SessionClientInfo) ([]mcp.Resource, error) {
	return sessionRetryWithDelay(ctx, cm.logger, clientInfo, "resource discovery",
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

// discoverSessionResourceTemplatesWithRetry discovers resource templates with retry logic for session clients
func (cm *ClientManager) discoverSessionResourceTemplatesWithRetry(ctx context.Context, clientInfo *SessionClientInfo) ([]mcp.ResourceTemplate, error) {
	return sessionRetryWithDelay(ctx, cm.logger, clientInfo, "resource template discovery",
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

// initializeSessionClientWithRetry attempts to initialize an MCP client with exponential backoff retry logic
func (cm *ClientManager) initializeSessionClientWithRetry(ctx context.Context, clientInfo *SessionClientInfo, req *mcp.InitializeRequest) (*mcp.InitializeResult, error) {
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

// Close closes all session clients
func (cm *ClientManager) Close(ctx context.Context) error {
	return cm.sessionManager.CloseAllSessions(ctx)
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
