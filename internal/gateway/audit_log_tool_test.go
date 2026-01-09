package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAuditLog_FileDoesNotExist(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a config pointing to a non-existent file
	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000
	auditPath := "/tmp/non_existent_audit_log_12345.log"

	// Ensure file doesn't exist
	_ = os.Remove(auditPath)

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGetAuditLog
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGetAuditLog(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "Should not return error for non-existent file")

	// Parse response
	var response AuditLogResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify empty results
	assert.Empty(t, response.Entries, "Should return empty entries for non-existent file")
	assert.Equal(t, 0, response.TotalCount, "Total count should be 0")
	assert.Equal(t, 0, response.ReturnedCount, "Returned count should be 0")
	assert.False(t, response.Truncated, "Truncated should be false")
}

func TestGetAuditLog_EmptyFile(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a temporary empty file
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "empty_audit.log")
	err := os.WriteFile(auditPath, []byte{}, 0644)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGetAuditLog
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGetAuditLog(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "Should not return error for empty file")

	// Parse response
	var response AuditLogResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify empty results
	assert.Empty(t, response.Entries, "Should return empty entries for empty file")
	assert.Equal(t, 0, response.TotalCount, "Total count should be 0")
	assert.Equal(t, 0, response.ReturnedCount, "Returned count should be 0")
	assert.False(t, response.Truncated, "Truncated should be false")
}

func TestGetAuditLog_FileWithOnlyWhitespace(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with only whitespace and empty lines
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "whitespace_audit.log")
	err := os.WriteFile(auditPath, []byte("\n\n   \n\t\n"), 0644)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGetAuditLog
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGetAuditLog(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "Should not return error for whitespace-only file")

	// Parse response
	var response AuditLogResponse
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify empty results (whitespace lines are skipped)
	assert.Empty(t, response.Entries, "Should return empty entries for whitespace-only file")
	assert.Equal(t, 0, response.TotalCount, "Total count should be 0")
}

func TestGetAuditLog_WithValidEntries(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with valid audit log entries using recent timestamps
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "valid_audit.log")

	now := float64(time.Now().Unix())
	entries := []string{
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"create_issue","client":"github","prefixed_name":"github__create_issue"},"action":"allow"}}`, now-2),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"delete_bucket","client":"aws","prefixed_name":"aws__delete_bucket"},"action":"deny"}}`, now-1),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"search_code","client":"github","prefixed_name":"github__search_code"},"action":"allow"}}`, now),
	}
	content := ""
	for _, entry := range entries {
		content += entry + "\n"
	}
	err := os.WriteFile(auditPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGetAuditLog
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGetAuditLog(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)

	// Parse response
	var response AuditLogResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Verify results (should be in reverse order - newest first)
	assert.Len(t, response.Entries, 3)
	assert.Equal(t, 3, response.TotalCount)
	assert.Equal(t, 3, response.ReturnedCount)
	assert.False(t, response.Truncated)
}

func TestGetAuditLog_WithMalformedEntries(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with mix of valid and malformed entries using recent timestamps
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "malformed_audit.log")

	now := float64(time.Now().Unix())
	content := fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"test","client":"test","prefixed_name":"test__test"},"action":"allow"}}
not valid json at all
{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"test2","client":"test","prefixed_name":"test__test2"},"action":"deny"}}
{incomplete json
{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"test3","client":"test","prefixed_name":"test__test3"},"action":"allow"}}
`, now-2, now-1, now)
	err := os.WriteFile(auditPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGetAuditLog
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGetAuditLog(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "Should not error on malformed entries, just skip them")

	// Parse response
	var response AuditLogResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Should only return valid entries (3 out of 5 lines)
	assert.Len(t, response.Entries, 3)
	assert.Equal(t, 3, response.TotalCount)
}

func TestGetAuditLog_WithOldFormatEntries(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with mix of new format and old format entries
	// Old format entries are valid JSON but use the old structure (status, request.params.name)
	// They should be gracefully skipped since they don't have the new tool structure
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "mixed_format_audit.log")

	now := float64(time.Now().Unix())
	content := fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"status":"success","request":{"params":{"name":"old_tool"}}}}
{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"new_tool","client":"test","prefixed_name":"test__new_tool"},"action":"allow"}}
{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"status":"denied","request":{"params":{"name":"another_old_tool"}}}}
{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"new_tool2","client":"test","prefixed_name":"test__new_tool2"},"action":"deny"}}
`, now-3, now-2, now-1, now)
	err := os.WriteFile(auditPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGetAuditLog
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGetAuditLog(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "Should not error on old format entries, just skip them")

	// Parse response
	var response AuditLogResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Should only return new format entries (2 out of 4 lines)
	// Old format entries have empty tool.prefixed_name and are filtered out by matchesFilter
	assert.Len(t, response.Entries, 2, "Should only return new format entries")
	assert.Equal(t, 2, response.TotalCount)

	// Verify the returned entries are the new format ones
	assert.Equal(t, "test__new_tool2", response.Entries[0].Audit.Tool.PrefixedName, "First entry should be newest new-format entry")
	assert.Equal(t, "test__new_tool", response.Entries[1].Audit.Tool.PrefixedName, "Second entry should be older new-format entry")
}

func TestGetAuditLog_StderrPath(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000
	auditPath := "stderr"

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGetAuditLog
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGetAuditLog(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsError, "Should return error when audit path is stderr")

	// Verify error message
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "cannot read audit logs when audit log path is set to stderr")
}

func TestGetAuditLog_StdoutPath(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000
	auditPath := "stdout"

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGetAuditLog
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGetAuditLog(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsError, "Should return error when audit path is stdout")

	// Verify error message
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "cannot read audit logs when audit log path is set to stdout")
}

func TestGetAuditLog_AllEntriesOlderThanTimeWindow(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with entries that are older than the default 7-day window
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "old_audit.log")

	// Create entries from 30 days ago
	oldTime := float64(time.Now().Add(-30 * 24 * time.Hour).Unix())
	entries := []string{
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"create_issue","client":"github","prefixed_name":"github__create_issue"},"action":"allow"}}`, oldTime),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"delete_bucket","client":"aws","prefixed_name":"aws__delete_bucket"},"action":"deny"}}`, oldTime+1),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"search_code","client":"github","prefixed_name":"github__search_code"},"action":"allow"}}`, oldTime+2),
	}
	content := ""
	for _, entry := range entries {
		content += entry + "\n"
	}
	err := os.WriteFile(auditPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	// Test with default 7-day window - should return empty results
	t.Run("DefaultTimeWindow", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = ToolGetAuditLog
		req.Params.Arguments = map[string]interface{}{}

		result, err := handler.handleGetAuditLog(ctx, req)
		require.NoError(t, err)
		require.False(t, result.IsError)

		var response AuditLogResponse
		textContent, _ := mcp.AsTextContent(result.Content[0])
		_ = json.Unmarshal([]byte(textContent.Text), &response)

		assert.Empty(t, response.Entries, "Should return no entries when all are older than 7 days")
		assert.Equal(t, 0, response.TotalCount)
		assert.False(t, response.Truncated)
	})

	// Test with explicit 1-day window - should return empty results
	t.Run("ExplicitOneDayWindow", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = ToolGetAuditLog
		req.Params.Arguments = map[string]interface{}{
			"time_range": "1d",
		}

		result, err := handler.handleGetAuditLog(ctx, req)
		require.NoError(t, err)
		require.False(t, result.IsError)

		var response AuditLogResponse
		textContent, _ := mcp.AsTextContent(result.Content[0])
		_ = json.Unmarshal([]byte(textContent.Text), &response)

		assert.Empty(t, response.Entries, "Should return no entries when all are older than 1 day")
		assert.Equal(t, 0, response.TotalCount)
	})

	// Test with large enough window - should return all entries
	t.Run("LargeTimeWindow", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = ToolGetAuditLog
		req.Params.Arguments = map[string]interface{}{
			"time_range": "60d", // 60 days should include 30-day-old entries
		}

		result, err := handler.handleGetAuditLog(ctx, req)
		require.NoError(t, err)
		require.False(t, result.IsError)

		var response AuditLogResponse
		textContent, _ := mcp.AsTextContent(result.Content[0])
		_ = json.Unmarshal([]byte(textContent.Text), &response)

		assert.Len(t, response.Entries, 3, "Should return all entries with 60-day window")
		assert.Equal(t, 3, response.TotalCount)
	})
}

func TestGetAuditLog_WithFiltering(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with various entries using recent timestamps
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "filterable_audit.log")

	now := float64(time.Now().Unix())
	entries := []string{
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"create_issue","client":"github","prefixed_name":"github__create_issue"},"action":"allow"}}`, now-3),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"delete_bucket","client":"aws","prefixed_name":"aws__delete_bucket"},"action":"deny"}}`, now-2),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"search_code","client":"github","prefixed_name":"github__search_code"},"action":"allow"}}`, now-1),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","audit":{"tool":{"name":"delete_repo","client":"github","prefixed_name":"github__delete_repo"},"action":"deny"}}`, now),
	}
	content := ""
	for _, entry := range entries {
		content += entry + "\n"
	}
	err := os.WriteFile(auditPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000

	handler := NewNativeToolsHandler(cfg, logger, logger, auditPath)

	// Test filtering by status
	t.Run("FilterByStatus", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = ToolGetAuditLog
		req.Params.Arguments = map[string]interface{}{
			"filter": map[string]interface{}{
				"status": "denied",
			},
		}

		result, err := handler.handleGetAuditLog(ctx, req)
		require.NoError(t, err)
		require.False(t, result.IsError)

		var response AuditLogResponse
		textContent, _ := mcp.AsTextContent(result.Content[0])
		_ = json.Unmarshal([]byte(textContent.Text), &response)

		assert.Len(t, response.Entries, 2, "Should return only denied entries")
	})

	// Test filtering by client
	t.Run("FilterByClient", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = ToolGetAuditLog
		req.Params.Arguments = map[string]interface{}{
			"filter": map[string]interface{}{
				"client": "github",
			},
		}

		result, err := handler.handleGetAuditLog(ctx, req)
		require.NoError(t, err)
		require.False(t, result.IsError)

		var response AuditLogResponse
		textContent, _ := mcp.AsTextContent(result.Content[0])
		_ = json.Unmarshal([]byte(textContent.Text), &response)

		assert.Len(t, response.Entries, 3, "Should return only github entries")
	})

	// Test limit and truncation - verify most recent entries are returned
	t.Run("LimitWithTruncation", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = ToolGetAuditLog
		req.Params.Arguments = map[string]interface{}{
			"limit": float64(2),
		}

		result, err := handler.handleGetAuditLog(ctx, req)
		require.NoError(t, err)
		require.False(t, result.IsError)

		var response AuditLogResponse
		textContent, _ := mcp.AsTextContent(result.Content[0])
		_ = json.Unmarshal([]byte(textContent.Text), &response)

		assert.Len(t, response.Entries, 2, "Should return 2 entries (limit)")
		assert.Equal(t, 4, response.TotalCount, "Total should be 4")
		assert.Equal(t, 2, response.ReturnedCount)
		assert.True(t, response.Truncated, "Should indicate truncation when more entries exist")

		// Verify that the most recent entries are returned (newest first)
		// Entry order in file: github__create_issue (oldest), aws__delete_bucket, github__search_code, github__delete_repo (newest)
		// Expected result: github__delete_repo (first/newest), github__search_code (second)
		require.NotNil(t, response.Entries[0].Audit)
		assert.Equal(t, "github__delete_repo", response.Entries[0].Audit.Tool.PrefixedName,
			"First entry should be the most recent (github__delete_repo)")

		require.NotNil(t, response.Entries[1].Audit)
		assert.Equal(t, "github__search_code", response.Entries[1].Audit.Tool.PrefixedName,
			"Second entry should be the second most recent (github__search_code)")
	})
}
