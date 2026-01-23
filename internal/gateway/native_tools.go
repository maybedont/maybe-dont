package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

const (
	// NativeToolPrefix is the prefix for all native gateway tools
	NativeToolPrefix = "maybedont__"

	ToolDiscoverTools         = "maybedont__discover_tools"
	ToolGenerateAuditReport   = "maybedont__generate_audit_report"
	ToolGetAuditLog           = "maybedont__get_audit_log"
	ToolListDownstreamServers = "maybedont__list_downstream_servers"
	ToolListSessions          = "maybedont__list_sessions"
)

// boolPtr returns a pointer to a bool value (helper for annotations)
func boolPtr(v bool) *bool {
	return &v
}

// ClientConfigProvider provides access to downstream client configurations
type ClientConfigProvider interface {
	GetClientConfigs() map[string]config.ClientConfig
}

// RegisteredToolsProvider provides access to registered tools on the MCP server
type RegisteredToolsProvider interface {
	ListRegisteredTools() []string
}

// SessionInfo contains information about an active session
type SessionInfo struct {
	SessionID         string                 `json:"session_id"`
	ClientIP          string                 `json:"client_ip,omitempty"`
	UserAgent         string                 `json:"user_agent,omitempty"`
	DownstreamClients []DownstreamClientInfo `json:"downstream_clients"`
}

// DownstreamClientInfo contains information about a downstream MCP client
type DownstreamClientInfo struct {
	Name      string `json:"name"`
	ToolCount int    `json:"tool_count"`
}

// SessionClientTools contains tool names for a session's downstream clients
type SessionClientTools struct {
	ClientName string   `json:"client_name"`
	Tools      []string `json:"tools"`
}

// SessionProvider provides access to active session information
type SessionProvider interface {
	GetActiveSessions() []SessionInfo
	// GetSessionClientTools returns the tools discovered for each client in a session
	GetSessionClientTools(sessionID string) []SessionClientTools
}

// DiscoveryResult contains the result of pass-through tool discovery
type DiscoveryResult struct {
	DiscoveredClients []DiscoveredClientInfo `json:"discovered_clients"`
	AlreadyConnected  []string               `json:"already_connected,omitempty"`
	Errors            []DiscoveryError       `json:"errors,omitempty"`
	// Shared indicates this result was obtained from a concurrent request via singleflight.
	// This is an internal field, not included in JSON responses.
	Shared bool `json:"-"`
}

// DiscoveredClientInfo contains information about a discovered client
type DiscoveredClientInfo struct {
	ClientName string   `json:"client_name"`
	ToolCount  int      `json:"tool_count"`
	Tools      []string `json:"tools"`
}

// DiscoveryError contains information about a discovery error
type DiscoveryError struct {
	ClientName string `json:"client_name"`
	Error      string `json:"error"`
}

// PassThroughDiscoveryProvider provides the ability to discover tools from pass-through clients
type PassThroughDiscoveryProvider interface {
	// DiscoverPassThroughTools triggers discovery for pass-through clients
	// and registers discovered tools for the session.
	// If clientName is empty, discovers all pass-through clients.
	DiscoverPassThroughTools(ctx context.Context, sessionID string, clientName string) (*DiscoveryResult, error)
}

// NativeToolsHandler handles native gateway tools
type NativeToolsHandler struct {
	config               *config.Config
	logger               *config.SessionLogger
	auditLogger          *config.SessionLogger
	auditLogPath         string // Full path to the audit log file
	clientConfigProvider ClientConfigProvider
	toolsProvider        RegisteredToolsProvider
	sessionProvider      SessionProvider
	discoveryProvider    PassThroughDiscoveryProvider
}

// NewNativeToolsHandler creates a new native tools handler.
// auditLogPath is the full resolved path to the audit log file.
func NewNativeToolsHandler(cfg *config.Config, logger, auditLogger *config.SessionLogger, auditLogPath string) *NativeToolsHandler {
	return &NativeToolsHandler{
		config:       cfg,
		logger:       logger,
		auditLogger:  auditLogger,
		auditLogPath: auditLogPath,
	}
}

// SetClientConfigProvider sets the provider for client configurations
func (h *NativeToolsHandler) SetClientConfigProvider(provider ClientConfigProvider) {
	h.clientConfigProvider = provider
}

// SetToolsProvider sets the provider for registered tools
func (h *NativeToolsHandler) SetToolsProvider(provider RegisteredToolsProvider) {
	h.toolsProvider = provider
}

// SetSessionProvider sets the provider for session information
func (h *NativeToolsHandler) SetSessionProvider(provider SessionProvider) {
	h.sessionProvider = provider
}

// SetDiscoveryProvider sets the provider for pass-through tool discovery
func (h *NativeToolsHandler) SetDiscoveryProvider(provider PassThroughDiscoveryProvider) {
	h.discoveryProvider = provider
}

// IsNativeTool checks if a tool name is a native gateway tool
func IsNativeTool(toolName string) bool {
	return strings.HasPrefix(toolName, NativeToolPrefix)
}

// GetTools returns the list of native tools to register
func (h *NativeToolsHandler) GetTools() []mcp.Tool {
	var tools []mcp.Tool

	if h.config.NativeTools.AuditLog.Enabled {
		tools = append(tools, h.getAuditLogToolDefinition())
	}

	if h.config.NativeTools.AuditReport.Enabled {
		tools = append(tools, h.getAuditReportToolDefinition())
	}

	if h.config.NativeTools.ListServers.Enabled {
		tools = append(tools, h.getListDownstreamServersToolDefinition())
	}

	if h.config.NativeTools.ListSessions.Enabled {
		tools = append(tools, h.getListSessionsToolDefinition())
	}

	// discover_tools is always enabled - it's required for pass-through auth to work
	tools = append(tools, h.getDiscoverToolsDefinition())

	return tools
}

// HandleToolCall routes native tool calls to appropriate handlers
func (h *NativeToolsHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Debug(ctx, "Handling native tool call", zap.String("tool", req.Params.Name))

	switch req.Params.Name {
	case ToolGetAuditLog:
		return h.handleGetAuditLog(ctx, req)
	case ToolGenerateAuditReport:
		return h.handleGenerateAuditReport(ctx, req)
	case ToolListDownstreamServers:
		return h.handleListDownstreamServers(ctx, req)
	case ToolListSessions:
		return h.handleListSessions(ctx, req)
	case ToolDiscoverTools:
		return h.handleDiscoverTools(ctx, req)
	default:
		return nil, fmt.Errorf("unknown native tool: %s", req.Params.Name)
	}
}

// getAuditLogToolDefinition returns the MCP tool definition for get_audit_log
func (h *NativeToolsHandler) getAuditLogToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name:         ToolGetAuditLog,
		Description:  "[EXPERIMENTAL] Retrieve the gateway's audit log entries. Returns JSON-formatted log entries from the configured audit log file.",
		DeferLoading: true,
		Annotations: mcp.ToolAnnotation{
			ReadOnlyHint: boolPtr(true),
		},
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum number of log entries to return (default: 100, max: %d)", h.config.NativeTools.AuditLog.MaxEntries),
					"default":     100,
				},
				"time_range": map[string]interface{}{
					"type":        "string",
					"description": "Time range to look back for entries. Supports Go duration format (e.g., '1h30m', '45m'), days ('7d', '30d'), weeks ('2w'), or 'all' for no limit. Default: '7d'",
					"default":     "7d",
				},
				"filter": map[string]interface{}{
					"type":        "object",
					"description": "Optional filters to apply",
					"properties": map[string]interface{}{
						"status": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"success", "denied", "error", "validation_error", "execution_error", "client_not_found", "invalid_tool_name", "response_denied", "panic"},
							"description": "Filter by status",
						},
						"tool_name": map[string]interface{}{
							"type":        "string",
							"description": "Filter by tool name (supports prefix matching)",
						},
						"client": map[string]interface{}{
							"type":        "string",
							"description": "Filter by downstream client name",
						},
					},
				},
			},
		},
	}
}

// getAuditReportToolDefinition returns the MCP tool definition for generate_audit_report
func (h *NativeToolsHandler) getAuditReportToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name:         ToolGenerateAuditReport,
		Description:  "[EXPERIMENTAL] Generate an AI-powered analysis report of the gateway's audit log. Analyzes patterns, security concerns, and provides recommendations prioritized by business impact.",
		DeferLoading: true,
		Annotations: mcp.ToolAnnotation{
			ReadOnlyHint: boolPtr(true),
		},
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"time_range": map[string]interface{}{
					"type":        "string",
					"description": "Time range of logs to analyze. Supports Go duration format (e.g., '1h30m', '45m'), days ('7d', '30d'), weeks ('2w'), or 'all' for no limit. Default: '24h'",
					"default":     "24h",
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"security", "usage", "errors", "comprehensive"},
					"description": "Focus area for the report",
					"default":     "comprehensive",
				},
				"include_recommendations": map[string]interface{}{
					"type":        "boolean",
					"description": "Include policy improvement recommendations",
					"default":     true,
				},
				"include_impact_analysis": map[string]interface{}{
					"type":        "boolean",
					"description": "Sort and annotate concerns by potential monetary/reputational business impact",
					"default":     true,
				},
				"format": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"markdown", "json", "summary"},
					"description": "Output format for the report",
					"default":     "markdown",
				},
			},
		},
	}
}
