package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// AuditLogEntry represents a parsed audit log entry
type AuditLogEntry struct {
	Level     string                 `json:"level"`
	Timestamp float64                `json:"ts"`
	Caller    string                 `json:"caller"`
	Message   string                 `json:"msg"`
	Logger    string                 `json:"logger"`
	RequestID string                 `json:"request_id"`
	Audit     map[string]interface{} `json:"audit"`
}

// AuditLogFilter represents filters for audit log queries
type AuditLogFilter struct {
	Status   string `json:"status"`
	ToolName string `json:"tool_name"`
	Client   string `json:"client"`
}

// AuditLogResponse represents the response from get_audit_log
type AuditLogResponse struct {
	Entries       []map[string]interface{} `json:"entries"`
	TotalCount    int                      `json:"total_count"`
	ReturnedCount int                      `json:"returned_count"`
	HasMore       bool                     `json:"has_more"`
}

// handleGetAuditLog handles the maybedont__get_audit_log tool call
func (h *NativeToolsHandler) handleGetAuditLog(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Info(ctx, "Processing get_audit_log request")

	// Parse parameters
	limit := 100
	offset := 0
	var filter AuditLogFilter

	if args, ok := req.Params.Arguments.(map[string]interface{}); ok && args != nil {
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		if o, ok := args["offset"].(float64); ok {
			offset = int(o)
		}
		if f, ok := args["filter"].(map[string]interface{}); ok {
			if s, ok := f["status"].(string); ok {
				filter.Status = s
			}
			if t, ok := f["tool_name"].(string); ok {
				filter.ToolName = t
			}
			if c, ok := f["client"].(string); ok {
				filter.Client = c
			}
		}
	}

	// Enforce max entries limit
	if limit > h.config.NativeTools.AuditLog.MaxEntries {
		limit = h.config.NativeTools.AuditLog.MaxEntries
	}

	// Read and parse audit log file
	entries, totalCount, err := h.readAuditLogEntries(ctx, limit, offset, filter)
	if err != nil {
		h.logger.Error(ctx, "Failed to read audit log", zap.Error(err))
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read audit log: %v", err)), nil
	}

	// Build response
	response := AuditLogResponse{
		Entries:       entries,
		TotalCount:    totalCount,
		ReturnedCount: len(entries),
		HasMore:       totalCount > offset+len(entries),
	}

	// Serialize to JSON
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to serialize response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// readAuditLogEntries reads and filters audit log entries from the configured file
func (h *NativeToolsHandler) readAuditLogEntries(ctx context.Context, limit, offset int, filter AuditLogFilter) ([]map[string]interface{}, int, error) {
	auditPath := h.config.Audit.Path

	// Check if audit path is stderr/stdout (can't read from those)
	if auditPath == "stderr" || auditPath == "stdout" {
		return nil, 0, fmt.Errorf("cannot read audit logs when audit.path is set to %s", auditPath)
	}

	// Check file size before reading
	fileInfo, err := os.Stat(auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty results if file doesn't exist
			return []map[string]interface{}{}, 0, nil
		}
		return nil, 0, fmt.Errorf("failed to stat audit file: %w", err)
	}

	if fileInfo.Size() > h.config.NativeTools.AuditLog.MaxFileSizeBytes {
		return nil, 0, fmt.Errorf("audit log file exceeds maximum size limit of %d bytes", h.config.NativeTools.AuditLog.MaxFileSizeBytes)
	}

	// Open and read the file
	file, err := os.Open(auditPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open audit file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	// Read all entries (we need to read all to count and apply offset from end)
	var allEntries []map[string]interface{}
	scanner := bufio.NewScanner(file)

	// Increase buffer size for potentially large log lines
	const maxScanTokenSize = 1024 * 1024 // 1MB per line max
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			h.logger.Warn(ctx, "Failed to parse audit log line", zap.Error(err))
			continue
		}

		// Apply filters
		if !h.matchesFilter(entry, filter) {
			continue
		}

		allEntries = append(allEntries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("error reading audit file: %w", err)
	}

	totalCount := len(allEntries)

	// Reverse entries (newest first)
	for i, j := 0, len(allEntries)-1; i < j; i, j = i+1, j-1 {
		allEntries[i], allEntries[j] = allEntries[j], allEntries[i]
	}

	// Apply offset and limit
	start := offset
	if start >= len(allEntries) {
		return []map[string]interface{}{}, totalCount, nil
	}

	end := start + limit
	if end > len(allEntries) {
		end = len(allEntries)
	}

	return allEntries[start:end], totalCount, nil
}

// matchesFilter checks if an audit entry matches the specified filters
func (h *NativeToolsHandler) matchesFilter(entry map[string]interface{}, filter AuditLogFilter) bool {
	audit, ok := entry["audit"].(map[string]interface{})
	if !ok {
		// Skip entries without audit data
		return false
	}

	// Filter by status
	if filter.Status != "" {
		status, ok := audit["status"].(string)
		if !ok || status != filter.Status {
			return false
		}
	}

	// Filter by tool name (prefix matching)
	if filter.ToolName != "" {
		request, ok := audit["request"].(map[string]interface{})
		if !ok {
			return false
		}
		params, ok := request["params"].(map[string]interface{})
		if !ok {
			return false
		}
		toolName, ok := params["name"].(string)
		if !ok {
			return false
		}
		if !strings.HasPrefix(toolName, filter.ToolName) {
			return false
		}
	}

	// Filter by client
	if filter.Client != "" {
		// Client can be in the audit entry directly or parsed from tool name
		client, ok := audit["client"].(string)
		if ok && client == filter.Client {
			return true
		}

		// Also try to extract from tool name
		request, ok := audit["request"].(map[string]interface{})
		if !ok {
			return false
		}
		params, ok := request["params"].(map[string]interface{})
		if !ok {
			return false
		}
		toolName, ok := params["name"].(string)
		if !ok {
			return false
		}
		clientName, _, err := ParsePrefixedName(toolName)
		if err != nil {
			return false
		}
		if clientName != filter.Client {
			return false
		}
	}

	return true
}
