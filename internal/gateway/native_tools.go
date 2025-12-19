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

	// Tool names
	ToolGetAuditLog         = "maybedont__get_audit_log"
	ToolGenerateAuditReport = "maybedont__generate_audit_report"
)

// NativeToolsHandler handles native gateway tools
type NativeToolsHandler struct {
	config      *config.Config
	logger      *config.SessionLogger
	auditLogger *config.SessionLogger
}

// NewNativeToolsHandler creates a new native tools handler
func NewNativeToolsHandler(cfg *config.Config, logger, auditLogger *config.SessionLogger) *NativeToolsHandler {
	return &NativeToolsHandler{
		config:      cfg,
		logger:      logger,
		auditLogger: auditLogger,
	}
}

// IsNativeTool checks if a tool name is a native gateway tool
func IsNativeTool(toolName string) bool {
	return strings.HasPrefix(toolName, NativeToolPrefix)
}

// GetTools returns the list of native tools to register
func (h *NativeToolsHandler) GetTools() []mcp.Tool {
	var tools []mcp.Tool

	if !h.config.NativeTools.Enabled {
		return tools
	}

	if h.config.NativeTools.AuditLog.Enabled {
		tools = append(tools, h.getAuditLogToolDefinition())
	}

	if h.config.NativeTools.AuditReport.Enabled {
		tools = append(tools, h.getAuditReportToolDefinition())
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
