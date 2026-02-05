package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONLAuditWriter_WriteEntry(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "audit.log")

	writer, err := NewJSONLAuditWriter(auditPath, "", config.RotationConfig{
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Compress:   false,
	}, "all")
	require.NoError(t, err)
	defer func() { _ = writer.Close() }()

	// Create a test audit entry
	entry := &AuditEntry{
		CreatedAt: "2024-01-15T12:00:00Z",
		Tool: &AuditToolInfo{
			Name:         "test_tool",
			Client:       "test_client",
			PrefixedName: "test_client__test_tool",
		},
		Action:            "allow",
		RecommendedAction: "allow",
		DurationMs:        100,
		TotalBlockedMs:    50,
	}

	// Write the entry
	written, err := writer.Write(entry)
	require.NoError(t, err)
	assert.True(t, written, "Entry should be written")

	// Close and verify the file contents
	err = writer.Close()
	require.NoError(t, err)

	// Read the file and verify it contains valid JSON
	content, err := os.ReadFile(auditPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `"prefixed_name":"test_client__test_tool"`)
	assert.Contains(t, string(content), `"action":"allow"`)
}

func TestJSONLAuditWriter_FilterDenyOnly(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "audit.log")

	writer, err := NewJSONLAuditWriter(auditPath, "", config.RotationConfig{
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Compress:   false,
	}, "deny_only")
	require.NoError(t, err)
	defer func() { _ = writer.Close() }()

	// Test that allowed entries are filtered out
	allowEntry := &AuditEntry{
		CreatedAt: "2024-01-15T12:00:00Z",
		Tool: &AuditToolInfo{
			Name:         "test_tool",
			Client:       "test_client",
			PrefixedName: "test_client__test_tool",
		},
		Action: string(config.PolicyActionAllow),
	}

	written, err := writer.Write(allowEntry)
	require.NoError(t, err)
	assert.False(t, written, "Allow entry should be filtered out with deny_only filter")

	// Test that denied entries are written
	denyEntry := &AuditEntry{
		CreatedAt: "2024-01-15T12:00:01Z",
		Tool: &AuditToolInfo{
			Name:         "test_tool2",
			Client:       "test_client",
			PrefixedName: "test_client__test_tool2",
		},
		Action: string(config.PolicyActionDeny),
	}

	written, err = writer.Write(denyEntry)
	require.NoError(t, err)
	assert.True(t, written, "Deny entry should be written with deny_only filter")

	// Close and verify the file contains only the deny entry
	err = writer.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(auditPath)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "test_tool__test_client")
	assert.Contains(t, string(content), "test_client__test_tool2")
}

func TestJSONLAuditWriter_NilEntry(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "audit.log")

	writer, err := NewJSONLAuditWriter(auditPath, "", config.RotationConfig{
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Compress:   false,
	}, "all")
	require.NoError(t, err)
	defer func() { _ = writer.Close() }()

	// Writing nil should not error but should not write
	written, err := writer.Write(nil)
	require.NoError(t, err)
	assert.False(t, written, "Nil entry should not be written")
}

func TestJSONLAuditWriter_StdoutStderr(t *testing.T) {
	tests := []struct {
		name       string
		auditPath  string
		expectPath bool
	}{
		{"stdout", "stdout", false},
		{"stderr", "stderr", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer, err := NewJSONLAuditWriter(tt.auditPath, "", config.RotationConfig{}, "all")
			require.NoError(t, err)

			// Verify that writing works (doesn't actually write to stdout/stderr in tests,
			// but verifies the path doesn't error)
			entry := &AuditEntry{
				CreatedAt: "2024-01-15T12:00:00Z",
				Tool: &AuditToolInfo{
					Name:         "test_tool",
					Client:       "test_client",
					PrefixedName: "test_client__test_tool",
				},
				Action: "allow",
			}

			// This would write to stdout/stderr but won't cause test failure
			_, err = writer.Write(entry)
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)
		})
	}
}

func TestJSONLAuditWriter_RelativePathWithLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	auditPath := "audit.log" // Relative path

	writer, err := NewJSONLAuditWriter(auditPath, tmpDir, config.RotationConfig{
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Compress:   false,
	}, "all")
	require.NoError(t, err)
	defer func() { _ = writer.Close() }()

	entry := &AuditEntry{
		CreatedAt: "2024-01-15T12:00:00Z",
		Tool: &AuditToolInfo{
			Name:         "test_tool",
			Client:       "test_client",
			PrefixedName: "test_client__test_tool",
		},
		Action: "allow",
	}

	written, err := writer.Write(entry)
	require.NoError(t, err)
	assert.True(t, written)

	err = writer.Close()
	require.NoError(t, err)

	// Verify file was created in the log directory
	expectedPath := filepath.Join(tmpDir, "audit.log")
	_, err = os.Stat(expectedPath)
	require.NoError(t, err, "Audit file should be created in log directory")
}

func TestJSONLAuditWriter_DefaultFilterIsAll(t *testing.T) {
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "audit.log")

	// Create with empty filter - should default to "all"
	writer, err := NewJSONLAuditWriter(auditPath, "", config.RotationConfig{
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Compress:   false,
	}, "")
	require.NoError(t, err)
	defer func() { _ = writer.Close() }()

	// Both allow and deny entries should be written
	allowEntry := &AuditEntry{
		CreatedAt: "2024-01-15T12:00:00Z",
		Tool: &AuditToolInfo{
			PrefixedName: "client__tool1",
		},
		Action: string(config.PolicyActionAllow),
	}

	written, err := writer.Write(allowEntry)
	require.NoError(t, err)
	assert.True(t, written, "Allow entry should be written with default filter")

	denyEntry := &AuditEntry{
		CreatedAt: "2024-01-15T12:00:01Z",
		Tool: &AuditToolInfo{
			PrefixedName: "client__tool2",
		},
		Action: string(config.PolicyActionDeny),
	}

	written, err = writer.Write(denyEntry)
	require.NoError(t, err)
	assert.True(t, written, "Deny entry should be written with default filter")
}

func TestIsAbsolutePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/var/log/audit.log", true},
		{"/home/user/logs/audit.log", true},
		{"audit.log", false},
		{"logs/audit.log", false},
		{"C:\\logs\\audit.log", true},
		{"D:\\audit.log", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isAbsolutePath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestEnsureAuditFileWritable tests the helper function for audit file writability validation
func TestEnsureAuditFileWritable(t *testing.T) {
	t.Run("succeeds for writable path", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "audit.log")

		err := ensureAuditFileWritable(filePath)
		require.NoError(t, err)

		// Verify the file was created
		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr)
	})

	t.Run("creates parent directories if needed", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "nested", "dirs", "audit.log")

		err := ensureAuditFileWritable(filePath)
		require.NoError(t, err)

		// Verify the file was created
		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr)
	})

	t.Run("fails for unwritable directory", func(t *testing.T) {
		// Try to write to a system path that doesn't exist and can't be created
		err := ensureAuditFileWritable("/nonexistent/readonly/system/path/audit.log")
		require.Error(t, err)
	})
}

// TestJSONLAuditWriter_FailFast tests that the audit writer fails at startup
// when the log file cannot be written (e.g., unwritable directory)
func TestJSONLAuditWriter_FailFast(t *testing.T) {
	t.Run("succeeds with stdout", func(t *testing.T) {
		writer, err := NewJSONLAuditWriter("stdout", "", config.RotationConfig{}, "all")
		require.NoError(t, err)
		require.NotNil(t, writer)
		_ = writer.Close()
	})

	t.Run("succeeds with stderr", func(t *testing.T) {
		writer, err := NewJSONLAuditWriter("stderr", "", config.RotationConfig{}, "all")
		require.NoError(t, err)
		require.NotNil(t, writer)
		_ = writer.Close()
	})

	t.Run("succeeds with writable file path", func(t *testing.T) {
		tmpDir := t.TempDir()
		writer, err := NewJSONLAuditWriter("audit.log", tmpDir, config.RotationConfig{}, "all")
		require.NoError(t, err)
		require.NotNil(t, writer)
		_ = writer.Close()
	})

	t.Run("fails fast with unwritable directory", func(t *testing.T) {
		// Use a path that cannot be written to
		_, err := NewJSONLAuditWriter("audit.log", "/nonexistent/readonly/system/path", config.RotationConfig{}, "all")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot write to audit log file")
	})
}
