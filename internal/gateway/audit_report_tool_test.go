package gateway

import (
	"context"
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

func TestGenerateAuditReport_EmptyFile(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create an empty audit log file
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "empty_audit.log")
	err := os.WriteFile(auditPath, []byte{}, 0644)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000
	cfg.NativeTools.AuditReport.Enabled = true
	cfg.NativeTools.AuditReport.MaxEntries = 1000
	cfg.Validation.AI.APIKey = "test-key" // Required to get past the API key check

	handler := NewNativeToolsHandler(cfg, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGenerateAuditReport
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGenerateAuditReport(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "Should not return error for empty file")

	// Verify the message indicates no entries found
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "No audit log entries found",
		"Should indicate no entries found for empty file")
}

func TestGenerateAuditReport_FileDoesNotExist(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	auditPath := "/tmp/non_existent_audit_log_report_test_12345.log"

	cfg := &config.Config{}
	cfg.NativeTools.AuditLog.Enabled = true
	cfg.NativeTools.AuditLog.MaxEntries = 1000
	cfg.NativeTools.AuditReport.Enabled = true
	cfg.NativeTools.AuditReport.MaxEntries = 1000
	cfg.Validation.AI.APIKey = "test-key"

	// Ensure file doesn't exist
	_ = os.Remove(auditPath)

	handler := NewNativeToolsHandler(cfg, logger, auditPath)

	req := mcp.CallToolRequest{}
	req.Params.Name = ToolGenerateAuditReport
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler.handleGenerateAuditReport(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "Should not return error for non-existent file")

	// Verify the message indicates no entries found
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "No audit log entries found",
		"Should indicate no entries found for non-existent file")
}

func TestGenerateAuditReport_AllEntriesOlderThanTimeWindow(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with entries from 30 days ago
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "old_audit.log")

	oldTime := float64(time.Now().Add(-30 * 24 * time.Hour).Unix())
	entries := []string{
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"create_issue","client":"github","prefixed_name":"github__create_issue"},"action":"allow"}}`, oldTime),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"Tool call audit","logger":"audit","audit":{"tool":{"name":"delete_bucket","client":"aws","prefixed_name":"aws__delete_bucket"},"action":"deny"}}`, oldTime+1),
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
	cfg.NativeTools.AuditReport.Enabled = true
	cfg.NativeTools.AuditReport.MaxEntries = 1000
	cfg.Validation.AI.APIKey = "test-key"

	handler := NewNativeToolsHandler(cfg, logger, auditPath)

	// Test with default 24h time range - should return no entries message
	t.Run("Default24hTimeRange", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = ToolGenerateAuditReport
		req.Params.Arguments = map[string]interface{}{}

		result, err := handler.handleGenerateAuditReport(ctx, req)
		require.NoError(t, err)
		require.False(t, result.IsError)

		textContent, ok := mcp.AsTextContent(result.Content[0])
		require.True(t, ok)
		assert.Contains(t, textContent.Text, "No audit log entries found",
			"Should indicate no entries found when all entries are older than 24h")
	})

	// Test with 7d time range - should return no entries message
	t.Run("SevenDayTimeRange", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = ToolGenerateAuditReport
		req.Params.Arguments = map[string]interface{}{
			"time_range": "7d",
		}

		result, err := handler.handleGenerateAuditReport(ctx, req)
		require.NoError(t, err)
		require.False(t, result.IsError)

		textContent, ok := mcp.AsTextContent(result.Content[0])
		require.True(t, ok)
		assert.Contains(t, textContent.Text, "No audit log entries found",
			"Should indicate no entries found when all entries are older than 7 days")
	})
}

func TestGetEntriesForReport_LimitReturnsMostRecentEntries(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with 5 entries, we'll limit to 2
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "limit_test_audit.log")

	now := float64(time.Now().Unix())
	entries := []string{
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"audit","audit":{"tool":{"name":"oldest","client":"test","prefixed_name":"tool_oldest"},"action":"allow"}}`, now-4),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"audit","audit":{"tool":{"name":"second_oldest","client":"test","prefixed_name":"tool_second_oldest"},"action":"allow"}}`, now-3),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"audit","audit":{"tool":{"name":"middle","client":"test","prefixed_name":"tool_middle"},"action":"allow"}}`, now-2),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"audit","audit":{"tool":{"name":"second_newest","client":"test","prefixed_name":"tool_second_newest"},"action":"deny"}}`, now-1),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"audit","audit":{"tool":{"name":"newest","client":"test","prefixed_name":"tool_newest"},"action":"allow"}}`, now),
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
	cfg.NativeTools.AuditReport.Enabled = true
	cfg.NativeTools.AuditReport.MaxEntries = 2 // Limit to 2 entries
	cfg.Validation.AI.APIKey = "test-key"

	handler := NewNativeToolsHandler(cfg, logger, auditPath)

	// Get entries for report with the limit of 2
	resultEntries, stats, err := handler.getEntriesForReport(ctx, "7d")
	require.NoError(t, err)

	// Should only have 2 entries
	assert.Len(t, resultEntries, 2, "Should return only 2 entries due to limit")

	// Verify statistics reflect only the 2 entries analyzed
	assert.Equal(t, 2, stats.TotalRequests, "Stats should reflect only the limited entries")

	// Verify the most recent entries are returned (newest first after reversal)
	// The entries should be: tool_newest (first), tool_second_newest (second)
	require.NotNil(t, resultEntries[0].Audit)
	assert.Equal(t, "tool_newest", resultEntries[0].Audit.Tool.PrefixedName,
		"First entry should be the most recent (tool_newest)")

	require.NotNil(t, resultEntries[1].Audit)
	assert.Equal(t, "tool_second_newest", resultEntries[1].Audit.Tool.PrefixedName,
		"Second entry should be the second most recent (tool_second_newest)")

	// Verify stats count the statuses of the returned entries
	// tool_newest = success, tool_second_newest = denied
	assert.Equal(t, 1, stats.SuccessCount, "Should have 1 success from the 2 most recent")
	assert.Equal(t, 1, stats.DeniedCount, "Should have 1 denied from the 2 most recent")
}

func TestGetEntriesForReport_AllTimeRange(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)

	// Create a file with old entries
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "all_time_audit.log")

	// Create entries from 60 days ago
	oldTime := float64(time.Now().Add(-60 * 24 * time.Hour).Unix())
	entries := []string{
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"audit","audit":{"tool":{"name":"old_tool_1","client":"test","prefixed_name":"old_tool_1"},"action":"allow"}}`, oldTime),
		fmt.Sprintf(`{"level":"info","ts":%f,"msg":"audit","audit":{"tool":{"name":"old_tool_2","client":"test","prefixed_name":"old_tool_2"},"action":"allow"}}`, oldTime+1),
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
	cfg.NativeTools.AuditReport.Enabled = true
	cfg.NativeTools.AuditReport.MaxEntries = 1000
	cfg.Validation.AI.APIKey = "test-key"

	handler := NewNativeToolsHandler(cfg, logger, auditPath)

	// Get entries with "all" time range - should include all entries regardless of age
	resultEntries, stats, err := handler.getEntriesForReport(ctx, "all")
	require.NoError(t, err)

	assert.Len(t, resultEntries, 2, "Should return all entries with 'all' time range")
	assert.Equal(t, 2, stats.TotalRequests)
}

func TestParseTimeRange(t *testing.T) {
	tests := []struct {
		input       string
		expected    time.Duration
		expectError bool
	}{
		// Go standard duration format
		{"1h", 1 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"1h30m", 90 * time.Minute, false},
		{"45s", 45 * time.Second, false},
		{"2h45m30s", 2*time.Hour + 45*time.Minute + 30*time.Second, false},

		// Extended formats - days
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},

		// Extended formats - weeks
		{"1w", 7 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},

		// Special values
		{"all", 0, false},
		{"", 0, false},

		// Invalid formats
		{"invalid", 0, true},
		{"7x", 0, true},
		{"abc123", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result, err := ParseTimeRange(tc.input)
			if tc.expectError {
				assert.Error(t, err, "ParseTimeRange(%q) should return error", tc.input)
			} else {
				require.NoError(t, err, "ParseTimeRange(%q) should not return error", tc.input)
				assert.Equal(t, tc.expected, result,
					"ParseTimeRange(%q) should return %v", tc.input, tc.expected)
			}
		})
	}
}
