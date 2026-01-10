package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// AuditLogEntry represents a parsed audit log entry from the zap-formatted log file.
// Session/client info is in Audit.IncomingRequest (not duplicated at top level).
type AuditLogEntry struct {
	Level     string      `json:"level"`
	Timestamp float64     `json:"ts"`
	Caller    string      `json:"caller"`
	Message   string      `json:"msg"`
	Logger    string      `json:"logger"`
	Audit     *AuditEntry `json:"audit"`
}

// AuditLogFilter represents filters for audit log queries
type AuditLogFilter struct {
	Status   string `json:"status"`
	ToolName string `json:"tool_name"`
	Client   string `json:"client"`
}

// AuditLogResponse represents the response from get_audit_log
type AuditLogResponse struct {
	Entries       []AuditLogEntry `json:"entries"`
	TotalCount    int             `json:"total_count"`
	ReturnedCount int             `json:"returned_count"`
	Truncated     bool            `json:"truncated"`
}

// handleGetAuditLog handles the maybedont__get_audit_log tool call
func (h *NativeToolsHandler) handleGetAuditLog(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Info(ctx, "Processing get_audit_log request")

	// Parse parameters with defaults
	limit := 100
	timeRangeStr := "7d" // Default to last 7 days
	var filter AuditLogFilter

	if args, ok := req.Params.Arguments.(map[string]interface{}); ok && args != nil {
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		if tr, ok := args["time_range"].(string); ok {
			timeRangeStr = tr
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

	// Parse time range
	timeRange, err := ParseTimeRange(timeRangeStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid time_range: %v", err)), nil
	}

	// Read and parse audit log file
	entries, totalCount, err := h.readAuditLogEntries(ctx, limit, timeRange, filter)
	if err != nil {
		h.logger.Error(ctx, "Failed to read audit log", zap.Error(err))
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read audit log: %v", err)), nil
	}

	// Build response
	response := AuditLogResponse{
		Entries:       entries,
		TotalCount:    totalCount,
		ReturnedCount: len(entries),
		Truncated:     totalCount > len(entries),
	}

	// Serialize to JSON
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to serialize response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// readAuditLogEntries reads and filters audit log entries from the configured file.
// It reads backwards from the end of the file, collecting entries until either:
//   - The limit is reached, or
//   - An entry falls outside the time range (entries are assumed chronologically ordered)
//
// A zero duration means no time filtering (collect up to limit entries).
// Returns entries newest-first (up to limit), plus a count of matching entries found.
// Note: totalCount may be less than actual total if we stop early due to time range.
func (h *NativeToolsHandler) readAuditLogEntries(ctx context.Context, limit int, timeRange time.Duration, filter AuditLogFilter) ([]AuditLogEntry, int, error) {
	auditPath := h.auditLogPath

	// Check if audit path is stderr/stdout (can't read from those)
	if auditPath == "stderr" || auditPath == "stdout" {
		return nil, 0, fmt.Errorf("cannot read audit logs when audit log path is set to %s", auditPath)
	}

	// Check if file exists
	fileInfo, err := os.Stat(auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty results if file doesn't exist
			return []AuditLogEntry{}, 0, nil
		}
		return nil, 0, fmt.Errorf("failed to stat audit file: %w", err)
	}

	fileSize := fileInfo.Size()
	if fileSize == 0 {
		return []AuditLogEntry{}, 0, nil
	}

	// Open the file
	file, err := os.Open(auditPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open audit file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	// Calculate cutoff time if time range is specified
	var cutoffTime time.Time
	if timeRange > 0 {
		cutoffTime = time.Now().Add(-timeRange)
	}

	// Read backwards from end of file
	var entries []AuditLogEntry
	totalCount := 0

	const chunkSize = 64 * 1024 // 64KB chunks
	readPos := fileSize
	var leftover []byte // Partial line from previous chunk

	for readPos > 0 && len(entries) < limit {
		// Calculate chunk boundaries
		chunkStart := readPos - chunkSize
		if chunkStart < 0 {
			chunkStart = 0
		}
		chunkLen := readPos - chunkStart

		// Read the chunk
		chunk := make([]byte, chunkLen)
		if _, err := file.ReadAt(chunk, chunkStart); err != nil && err != io.EOF {
			return nil, 0, fmt.Errorf("failed to read chunk: %w", err)
		}

		// Append leftover from previous iteration (this is the end of a line that continued into next chunk)
		if len(leftover) > 0 {
			chunk = append(chunk, leftover...)
			leftover = nil
		}

		// Find all complete lines in this chunk (reading backwards)
		lines := h.extractLinesReverse(chunk, chunkStart > 0)
		if chunkStart > 0 && len(lines.partial) > 0 {
			// Save partial line at the start for next iteration
			leftover = lines.partial
		}

		// Process lines (they come out newest-first from extractLinesReverse)
		for _, line := range lines.complete {
			if len(line) == 0 {
				continue
			}

			var entry AuditLogEntry
			if err := json.Unmarshal(line, &entry); err != nil {
				h.logger.Debug(ctx, "Skipping unparseable audit log line (may be old format)", zap.Error(err))
				continue
			}

			// Check time range - if entry is too old, we can stop
			// (assuming log entries are chronologically ordered)
			if !cutoffTime.IsZero() {
				entryTime := time.Unix(int64(entry.Timestamp), 0)
				if entryTime.Before(cutoffTime) {
					// Entry is too old, and all earlier entries will be older too
					return entries, totalCount, nil
				}
			}

			// Apply filters
			if !h.matchesFilter(entry, filter) {
				continue
			}

			totalCount++
			if len(entries) < limit {
				entries = append(entries, entry)
			}
		}

		readPos = chunkStart
	}

	// Process any remaining leftover (first line of file)
	if len(leftover) > 0 && len(entries) < limit {
		var entry AuditLogEntry
		if err := json.Unmarshal(leftover, &entry); err == nil {
			if cutoffTime.IsZero() || !time.Unix(int64(entry.Timestamp), 0).Before(cutoffTime) {
				if h.matchesFilter(entry, filter) {
					totalCount++
					if len(entries) < limit {
						entries = append(entries, entry)
					}
				}
			}
		}
	}

	return entries, totalCount, nil
}

// linesResult holds the result of extracting lines from a chunk
type linesResult struct {
	complete [][]byte // Complete lines, newest first
	partial  []byte   // Partial line at the start of chunk (if not at file start)
}

// extractLinesReverse extracts complete lines from a chunk, returning them newest-first.
// If hasMoreBefore is true, the first line in the chunk is assumed to be partial
// (continuing from a previous chunk) and is returned separately.
func (h *NativeToolsHandler) extractLinesReverse(chunk []byte, hasMoreBefore bool) linesResult {
	var result linesResult

	// Find all newline positions
	var lineEnds []int
	for i, b := range chunk {
		if b == '\n' {
			lineEnds = append(lineEnds, i)
		}
	}

	if len(lineEnds) == 0 {
		// No newlines - entire chunk is one partial line
		if hasMoreBefore {
			result.partial = chunk
		} else if len(chunk) > 0 {
			result.complete = append(result.complete, chunk)
		}
		return result
	}

	// Extract lines from end to start (newest first)
	// Last segment: from last newline+1 to end of chunk
	if lineEnds[len(lineEnds)-1] < len(chunk)-1 {
		line := chunk[lineEnds[len(lineEnds)-1]+1:]
		if len(line) > 0 {
			result.complete = append(result.complete, line)
		}
	}

	// Middle segments: between newlines (iterate backwards)
	for i := len(lineEnds) - 1; i > 0; i-- {
		start := lineEnds[i-1] + 1
		end := lineEnds[i]
		if end > start {
			result.complete = append(result.complete, chunk[start:end])
		}
	}

	// First segment: from start of chunk to first newline
	firstNewline := lineEnds[0]
	if hasMoreBefore {
		// This is a partial line continuing from previous chunk
		if firstNewline > 0 {
			result.partial = chunk[:firstNewline]
		}
	} else {
		// This is a complete line (start of file)
		if firstNewline > 0 {
			result.complete = append(result.complete, chunk[:firstNewline])
		}
	}

	return result
}

// matchesFilter checks if an audit entry matches the specified filters
func (h *NativeToolsHandler) matchesFilter(entry AuditLogEntry, filter AuditLogFilter) bool {
	if entry.Audit == nil {
		return false
	}

	// Skip entries that don't have the new format structure
	// Old format entries will have empty Tool.PrefixedName
	if entry.Audit.Tool.PrefixedName == "" {
		return false
	}

	// Filter by status (action field: "allow" or "deny")
	if filter.Status != "" {
		// Map old status values to new action values for backward compatibility
		expectedAction := filter.Status
		switch filter.Status {
		case "success":
			expectedAction = string(config.PolicyActionAllow)
		case "denied", "response_denied":
			expectedAction = string(config.PolicyActionDeny)
		}
		if entry.Audit.Action != expectedAction {
			return false
		}
	}

	// Filter by tool name (prefix matching on prefixed_name)
	if filter.ToolName != "" {
		if !strings.HasPrefix(entry.Audit.Tool.PrefixedName, filter.ToolName) {
			return false
		}
	}

	// Filter by client name
	if filter.Client != "" {
		if entry.Audit.Tool.Client != filter.Client {
			return false
		}
	}

	return true
}
