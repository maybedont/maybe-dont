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

	ToolGetAuditLog           = "maybedont__get_audit_log"
	ToolGenerateAuditReport   = "maybedont__generate_audit_report"
	ToolListDownstreamServers = "maybedont__list_downstream_servers"
	ToolListSessions          = "maybedont__list_sessions"
)

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
	SessionID       string   `json:"session_id"`
	ClientIP        string   `json:"client_ip,omitempty"`
	DownstreamNames []string `json:"downstream_clients"`
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

// NativeToolsHandler handles native gateway tools
type NativeToolsHandler struct {
	config               *config.Config
	logger               *config.SessionLogger
	auditLogger          *config.SessionLogger
	clientConfigProvider ClientConfigProvider
	toolsProvider        RegisteredToolsProvider
	sessionProvider      SessionProvider
}

// NewNativeToolsHandler creates a new native tools handler
func NewNativeToolsHandler(cfg *config.Config, logger, auditLogger *config.SessionLogger) *NativeToolsHandler {
	return &NativeToolsHandler{
		config:      cfg,
		logger:      logger,
		auditLogger: auditLogger,
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
	default:
		return nil, fmt.Errorf("unknown native tool: %s", req.Params.Name)
	}
}

// getAuditLogToolDefinition returns the MCP tool definition for get_audit_log
func (h *NativeToolsHandler) getAuditLogToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name:        ToolGetAuditLog,
		Description: "Retrieve the gateway's audit log entries. Returns JSON-formatted log entries from the configured audit log file.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum number of log entries to return (default: 100, max: %d)", h.config.NativeTools.AuditLog.MaxEntries),
					"default":     100,
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Number of entries to skip from the end (0 = most recent)",
					"default":     0,
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
		Name:        ToolGenerateAuditReport,
		Description: "Generate an AI-powered analysis report of the gateway's audit log. Analyzes patterns, security concerns, and provides recommendations prioritized by business impact.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"time_range": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"1h", "6h", "24h", "7d", "30d", "all"},
					"description": "Time range of logs to analyze",
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
