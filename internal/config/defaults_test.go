package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetDefaultFiles(t *testing.T) {
	// Test that all expected default files are returned
	files := GetDefaultFiles()

	expectedFiles := []string{
		"maybe-dont.yaml",
		"cel_request_rules.yaml",
		"ai_request_rules.yaml",
		"cel_response_rules.yaml",
		"ai_response_rules.yaml",
	}

	require.Len(t, files, len(expectedFiles), "Should have %d default files", len(expectedFiles))

	// Check each expected file is present
	for _, expected := range expectedFiles {
		found := false
		for _, f := range files {
			if f.Filename == expected {
				found = true
				require.NotEmpty(t, f.Content, "File %s should have content", expected)
				break
			}
		}
		require.True(t, found, "Expected file %s not found in defaults", expected)
	}
}

func TestWriteDefaultsIfMissing_CreatesFilesOnFirstRun(t *testing.T) {
	// Test that files are created when config directory is empty
	tmpDir := t.TempDir()

	created, err := WriteDefaultsIfMissing(tmpDir)
	require.NoError(t, err)

	// Should have created all default files
	expectedFiles := []string{
		"maybe-dont.yaml",
		"cel_request_rules.yaml",
		"ai_request_rules.yaml",
		"cel_response_rules.yaml",
		"ai_response_rules.yaml",
	}

	require.Len(t, created, len(expectedFiles), "Should create all default files")

	// Verify each file exists and has content
	for _, filename := range expectedFiles {
		path := filepath.Join(tmpDir, filename)
		info, err := os.Stat(path)
		require.NoError(t, err, "File %s should exist", filename)
		require.True(t, info.Size() > 0, "File %s should have content", filename)
	}
}

func TestWriteDefaultsIfMissing_NeverOverwritesExisting(t *testing.T) {
	// Test that existing files are preserved
	tmpDir := t.TempDir()

	// Create an existing config file with custom content
	existingContent := "# My custom configuration\nlogger:\n  level: debug\n"
	existingPath := filepath.Join(tmpDir, "maybe-dont.yaml")
	err := os.WriteFile(existingPath, []byte(existingContent), 0600)
	require.NoError(t, err)

	// Run WriteDefaultsIfMissing
	created, err := WriteDefaultsIfMissing(tmpDir)
	require.NoError(t, err)

	// Should NOT have created maybe-dont.yaml (it already existed)
	for _, f := range created {
		require.NotEqual(t, "maybe-dont.yaml", f, "Should not overwrite existing maybe-dont.yaml")
	}

	// Verify original content is preserved
	content, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	require.Equal(t, existingContent, string(content), "Existing file content should be preserved")

	// But other files should have been created
	require.Len(t, created, 4, "Should create 4 files (all except maybe-dont.yaml)")
}

func TestWriteDefaultsIfMissing_FilePermissions(t *testing.T) {
	// Test that files are created with secure permissions (0600)
	tmpDir := t.TempDir()

	_, err := WriteDefaultsIfMissing(tmpDir)
	require.NoError(t, err)

	// Check permissions on created files
	files := GetDefaultFiles()
	for _, f := range files {
		path := filepath.Join(tmpDir, f.Filename)
		info, err := os.Stat(path)
		require.NoError(t, err)
		// Files should be 0600 (owner read/write only) for security
		require.Equal(t, os.FileMode(0600), info.Mode().Perm(),
			"File %s should have 0600 permissions", f.Filename)
	}
}

func TestWriteDefaultsIfMissing_PartialRun(t *testing.T) {
	// Test that only missing files are created on subsequent runs
	tmpDir := t.TempDir()

	// First run - creates all files
	created1, err := WriteDefaultsIfMissing(tmpDir)
	require.NoError(t, err)
	require.Len(t, created1, 5, "First run should create all 5 files")

	// Second run - should create no files
	created2, err := WriteDefaultsIfMissing(tmpDir)
	require.NoError(t, err)
	require.Len(t, created2, 0, "Second run should create no files")

	// Delete one file
	err = os.Remove(filepath.Join(tmpDir, "cel_request_rules.yaml"))
	require.NoError(t, err)

	// Third run - should only create the deleted file
	created3, err := WriteDefaultsIfMissing(tmpDir)
	require.NoError(t, err)
	require.Len(t, created3, 1, "Third run should create only 1 file")
	require.Equal(t, "cel_request_rules.yaml", created3[0])
}

func TestWriteDefaultsIfMissing_NonWritableDirectory(t *testing.T) {
	// Test behavior when directory is not writable
	// Skip on systems where we can't reliably test this
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	tmpDir := t.TempDir()

	// Make directory read-only
	err := os.Chmod(tmpDir, 0500)
	require.NoError(t, err)
	defer func() { _ = os.Chmod(tmpDir, 0755) }() // Restore for cleanup

	_, err = WriteDefaultsIfMissing(tmpDir)
	require.Error(t, err, "Should fail when directory is not writable")
}

func TestDumpDefaults_OverwritesExisting(t *testing.T) {
	// Test that DumpDefaults overwrites existing files (unlike WriteDefaultsIfMissing)
	tmpDir := t.TempDir()

	// Create an existing config file with custom content
	existingContent := "# My custom configuration\n"
	existingPath := filepath.Join(tmpDir, "maybe-dont.yaml")
	err := os.WriteFile(existingPath, []byte(existingContent), 0600)
	require.NoError(t, err)

	// Run DumpDefaults
	err = DumpDefaults(tmpDir)
	require.NoError(t, err)

	// Content should be overwritten with default
	content, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	require.NotEqual(t, existingContent, string(content), "DumpDefaults should overwrite existing files")
	require.Contains(t, string(content), "MCP Security Gateway Configuration",
		"Should contain default config content")
}

func TestDumpDefaults_CreatesDirectory(t *testing.T) {
	// Test that DumpDefaults creates the output directory if it doesn't exist
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "nested", "output", "dir")

	err := DumpDefaults(outputDir)
	require.NoError(t, err)

	// Verify directory was created
	info, err := os.Stat(outputDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	// Verify all files were created
	files := GetDefaultFiles()
	for _, f := range files {
		path := filepath.Join(outputDir, f.Filename)
		_, err := os.Stat(path)
		require.NoError(t, err, "File %s should exist", f.Filename)
	}
}

func TestDefaultConfigContent(t *testing.T) {
	// Test that the embedded default config is valid YAML and contains expected sections
	files := GetDefaultFiles()

	var mainConfig []byte
	for _, f := range files {
		if f.Filename == "maybe-dont.yaml" {
			mainConfig = f.Content
			break
		}
	}

	require.NotEmpty(t, mainConfig, "Default config should have content")

	// Check for key sections in the config
	configStr := string(mainConfig)
	require.Contains(t, configStr, "logger:", "Should contain logger section")
	require.Contains(t, configStr, "audit:", "Should contain audit section")
	require.Contains(t, configStr, "downstream_mcp_servers:", "Should contain downstream_mcp_servers section")
	require.Contains(t, configStr, "server:", "Should contain server section")
	require.Contains(t, configStr, "validation:", "Should contain validation section")
	require.Contains(t, configStr, "request_validation:", "Should contain request_validation section")
	require.Contains(t, configStr, "response_validation:", "Should contain response_validation section")
	require.Contains(t, configStr, "native_tools:", "Should contain native_tools section")
}
