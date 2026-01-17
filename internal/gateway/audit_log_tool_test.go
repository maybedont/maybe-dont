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

func TestGetAuditLog_LineSpanningMultipleChunks(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with a line that exceeds the 64KB chunk size
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "large_line_audit.log")

	now := float64(time.Now().Unix())

	// Create a large params value that will make the JSON line exceed 64KB
	// 64KB = 65536 bytes, so we need a params string larger than that
	largeParams := make([]byte, 70000)
	for i := range largeParams {
		largeParams[i] = 'x'
	}

	// Create entries: one normal, one large (>64KB), one normal
	normalEntry1 := fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"small_tool","client":"test","prefixed_name":"test__small_tool"},"action":"allow"}}`, now-2)
	largeEntry := fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"large_tool","client":"test","prefixed_name":"test__large_tool"},"action":"allow","request":{"params":{"data":"%s"}}}}`, now-1, string(largeParams))
	normalEntry2 := fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"another_small_tool","client":"test","prefixed_name":"test__another_small_tool"},"action":"deny"}}`, now)

	content := normalEntry1 + "\n" + largeEntry + "\n" + normalEntry2 + "\n"
	err := os.WriteFile(auditPath, []byte(content), 0644)
	require.NoError(t, err)

	// Verify the file is larger than 64KB
	fileInfo, err := os.Stat(auditPath)
	require.NoError(t, err)
	require.Greater(t, fileInfo.Size(), int64(64*1024), "File should be larger than 64KB to test chunk spanning")

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
	require.False(t, result.IsError, "Should handle lines spanning multiple chunks")

	// Parse response
	var response AuditLogResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Should return all 3 entries (newest first)
	assert.Len(t, response.Entries, 3, "Should return all 3 entries including the large one")
	assert.Equal(t, 3, response.TotalCount)

	// Verify order (newest first)
	assert.Equal(t, "test__another_small_tool", response.Entries[0].Audit.Tool.PrefixedName)
	assert.Equal(t, "test__large_tool", response.Entries[1].Audit.Tool.PrefixedName)
	assert.Equal(t, "test__small_tool", response.Entries[2].Audit.Tool.PrefixedName)
}

func TestGetAuditLog_SingleLineLargerThan64KB(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with a single line that exceeds 64KB (no other lines)
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "single_large_line_audit.log")

	now := float64(time.Now().Unix())

	// Create a large params value that will make the JSON line exceed 64KB
	largeParams := make([]byte, 70000)
	for i := range largeParams {
		largeParams[i] = 'y'
	}

	largeEntry := fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"single_large_tool","client":"test","prefixed_name":"test__single_large_tool"},"action":"allow","request":{"params":{"data":"%s"}}}}`, now, string(largeParams))

	content := largeEntry + "\n"
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
	require.False(t, result.IsError, "Should handle single large line")

	// Parse response
	var response AuditLogResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Should return the single large entry
	assert.Len(t, response.Entries, 1, "Should return the single large entry")
	assert.Equal(t, "test__single_large_tool", response.Entries[0].Audit.Tool.PrefixedName)
}

func TestGetAuditLog_NoNewlineAtEOF(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file without a trailing newline
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "no_newline_audit.log")

	now := float64(time.Now().Unix())
	entries := []string{
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"tool1","client":"test","prefixed_name":"test__tool1"},"action":"allow"}}`, now-1),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"tool2","client":"test","prefixed_name":"test__tool2"},"action":"deny"}}`, now),
	}
	// No trailing newline after last entry
	content := entries[0] + "\n" + entries[1]
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
	require.False(t, result.IsError, "Should handle file without trailing newline")

	// Parse response
	var response AuditLogResponse
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Should return both entries
	assert.Len(t, response.Entries, 2, "Should return both entries even without trailing newline")
	assert.Equal(t, "test__tool2", response.Entries[0].Audit.Tool.PrefixedName)
	assert.Equal(t, "test__tool1", response.Entries[1].Audit.Tool.PrefixedName)
}

func TestTimestampToTime_Precision(t *testing.T) {
	// Test that fractional seconds are preserved
	tests := []struct {
		name      string
		timestamp float64
		wantSec   int64
		wantNsec  int64
	}{
		{
			name:      "whole seconds",
			timestamp: 1704067200.0, // 2024-01-01 00:00:00 UTC
			wantSec:   1704067200,
			wantNsec:  0,
		},
		{
			name:      "half second",
			timestamp: 1704067200.5,
			wantSec:   1704067200,
			wantNsec:  500000000,
		},
		{
			name:      "millisecond precision",
			timestamp: 1704067200.123,
			wantSec:   1704067200,
			wantNsec:  123000000,
		},
		{
			name:      "microsecond precision",
			timestamp: 1704067200.123456,
			wantSec:   1704067200,
			wantNsec:  123456000,
		},
		{
			name:      "nanosecond precision",
			timestamp: 1704067200.123456789,
			wantSec:   1704067200,
			wantNsec:  123456789,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := timestampToTime(tt.timestamp)
			assert.Equal(t, tt.wantSec, result.Unix(), "Seconds should match")
			// Allow small tolerance for floating point precision
			assert.InDelta(t, tt.wantNsec, result.Nanosecond(), 100, "Nanoseconds should be close")
		})
	}
}

func TestGetAuditLog_TimeRangeWithFractionalTimestamps(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "fractional_ts_audit.log")

	// Create entries with fractional timestamps
	// Use a time just slightly before and after the cutoff to test precision
	now := time.Now()
	cutoffTime := now.Add(-1 * time.Hour)

	// Entry 1: 1 second before cutoff (should be excluded)
	ts1 := float64(cutoffTime.Add(-1*time.Second).UnixNano()) / 1e9
	// Entry 2: 500ms after cutoff (should be included)
	ts2 := float64(cutoffTime.Add(500*time.Millisecond).UnixNano()) / 1e9
	// Entry 3: now (should be included)
	ts3 := float64(now.UnixNano()) / 1e9

	entries := []string{
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"old_tool","client":"test","prefixed_name":"test__old_tool"},"action":"allow"}}`, ts1),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"cutoff_tool","client":"test","prefixed_name":"test__cutoff_tool"},"action":"allow"}}`, ts2),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"new_tool","client":"test","prefixed_name":"test__new_tool"},"action":"allow"}}`, ts3),
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
	req.Params.Arguments = map[string]interface{}{
		"time_range": "1h",
	}

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

	// Should return 2 entries (the ones within the 1-hour window)
	assert.Len(t, response.Entries, 2, "Should return entries within the time range with fractional precision")
	assert.Equal(t, "test__new_tool", response.Entries[0].Audit.Tool.PrefixedName)
	assert.Equal(t, "test__cutoff_tool", response.Entries[1].Audit.Tool.PrefixedName)
}

func TestParseAuditLine_NewDirectFormat(t *testing.T) {
	// Tests parsing the new direct JSONL format (AuditEntry written directly)
	directJSON := `{"validation_started":"2024-01-15T12:00:00Z","created_at":"2024-01-15T12:00:01Z","tool":{"name":"test_tool","client":"test_client","prefixed_name":"test_client__test_tool"},"upstream_request":{"id":"req-123","session_id":"sess-456","client_ip":"192.168.1.1","user_agent":"TestAgent/1.0"},"action":"allow","recommended_action":"allow","duration_ms":100,"total_blocked_ms":50}`

	entry, entryTime, err := parseAuditLine([]byte(directJSON))
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.NotNil(t, entry.Audit)

	assert.Equal(t, "test_client__test_tool", entry.Audit.Tool.PrefixedName)
	assert.Equal(t, "test_client", entry.Audit.Tool.Client)
	assert.Equal(t, "test_tool", entry.Audit.Tool.Name)
	assert.Equal(t, "allow", entry.Audit.Action)
	assert.Equal(t, "2024-01-15T12:00:01Z", entry.Audit.CreatedAt)
	assert.Equal(t, "2024-01-15T12:00:00Z", entry.Audit.ValidationStarted)
	assert.Equal(t, "TestAgent/1.0", entry.Audit.UpstreamRequest.UserAgent)
	assert.False(t, entryTime.IsZero(), "Entry time should be parsed from created_at")
}

func TestParseAuditLine_LegacyZapFormat(t *testing.T) {
	// Tests parsing the legacy zap-formatted log entry with old schema
	legacyJSON := `{"level":"info","ts":1705320000.123,"caller":"gateway/gateway.go:100","msg":"Tool call audit","logger":"audit","audit":{"created_at":"2024-01-15T12:00:00Z","tool":{"name":"legacy_tool","client":"legacy_client","prefixed_name":"legacy_client__legacy_tool"},"request":{"params":{"arg1":"value1"},"called_at":"2024-01-15T12:00:00.001Z","duration_ms":50},"incoming_request":{"id":"req-old","session_id":"sess-old","client_ip":"10.0.0.1"},"request_validation":{"cel":{"action":"allow","evaluation_ms":10,"results":[]}},"action":"allow","recommended_action":"allow","duration_ms":100,"total_blocked_ms":20}}`

	entry, entryTime, err := parseAuditLine([]byte(legacyJSON))
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.NotNil(t, entry.Audit)

	// Verify the entry was converted to new format
	assert.Equal(t, "legacy_client__legacy_tool", entry.Audit.Tool.PrefixedName)
	assert.Equal(t, "legacy_client", entry.Audit.Tool.Client)
	assert.Equal(t, "legacy_tool", entry.Audit.Tool.Name)
	assert.Equal(t, "allow", entry.Audit.Action)

	// Verify legacy Request was merged into Tool
	assert.Equal(t, "value1", entry.Audit.Tool.Params["arg1"])
	assert.Equal(t, "2024-01-15T12:00:00.001Z", entry.Audit.Tool.CalledAt)

	// Verify legacy IncomingRequest was converted to UpstreamRequest
	assert.Equal(t, "req-old", entry.Audit.UpstreamRequest.RequestID)
	assert.Equal(t, "sess-old", entry.Audit.UpstreamRequest.SessionID)
	assert.Equal(t, "10.0.0.1", entry.Audit.UpstreamRequest.ClientIP)

	// Verify legacy CEL was converted to Rules
	require.NotNil(t, entry.Audit.RequestValidation)
	require.NotNil(t, entry.Audit.RequestValidation.Rules)
	assert.Equal(t, "allow", entry.Audit.RequestValidation.Rules.Action)

	// Verify zap metadata was preserved
	assert.Equal(t, "info", entry.Level)
	assert.InDelta(t, 1705320000.123, entry.Timestamp, 0.001)

	// Verify entry time was parsed from zap timestamp
	assert.False(t, entryTime.IsZero())
}

func TestParseAuditLine_InvalidJSON(t *testing.T) {
	invalidJSON := `not valid json at all`

	entry, entryTime, err := parseAuditLine([]byte(invalidJSON))
	require.Error(t, err)
	assert.Nil(t, entry)
	assert.True(t, entryTime.IsZero())
}

func TestParseAuditLine_EmptyToolPrefixedName(t *testing.T) {
	// Entry with validation_started but no prefixed_name should fail
	// because we require prefixed_name to be non-empty for valid entries
	emptyPrefixedNameJSON := `{"validation_started":"2024-01-15T12:00:00Z","created_at":"2024-01-15T12:00:01Z","tool":{"name":"","client":"","prefixed_name":""},"action":"allow"}`

	entry, _, err := parseAuditLine([]byte(emptyPrefixedNameJSON))
	// The entry should fail because prefixed_name is empty
	require.Error(t, err, "Entry without prefixed_name should fail to parse")
	assert.Nil(t, entry)
}

func TestParseAuditLine_ZapFormatWithNewSchema(t *testing.T) {
	// Tests parsing a zap-formatted entry that uses the new schema (has rules, not cel)
	zapNewJSON := `{"level":"info","ts":1705320000.5,"caller":"gateway/gateway.go:200","msg":"Tool call audit","logger":"audit","audit":{"validation_started":"2024-01-15T12:00:00Z","created_at":"2024-01-15T12:00:01Z","tool":{"name":"new_tool","client":"new_client","prefixed_name":"new_client__new_tool","params":{"key":"value"}},"upstream_request":{"id":"req-new","session_id":"sess-new","client_ip":"172.16.0.1","user_agent":"NewAgent/2.0"},"request_validation":{"rules":{"action":"allow","evaluation_ms":5}},"action":"allow","recommended_action":"allow","duration_ms":80,"total_blocked_ms":30}}`

	entry, entryTime, err := parseAuditLine([]byte(zapNewJSON))
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.NotNil(t, entry.Audit)

	assert.Equal(t, "new_client__new_tool", entry.Audit.Tool.PrefixedName)
	assert.Equal(t, "new_client", entry.Audit.Tool.Client)
	assert.Equal(t, "allow", entry.Audit.Action)
	assert.Equal(t, "NewAgent/2.0", entry.Audit.UpstreamRequest.UserAgent)
	assert.Equal(t, "req-new", entry.Audit.UpstreamRequest.RequestID)

	// Verify zap metadata was preserved
	assert.Equal(t, "info", entry.Level)
	assert.InDelta(t, 1705320000.5, entry.Timestamp, 0.001)

	// Entry time should come from zap timestamp
	assert.False(t, entryTime.IsZero())
}
