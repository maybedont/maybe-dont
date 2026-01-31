package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultsExportCommand_RequiresOutputDir(t *testing.T) {
	// Test that the command fails when --output-dir is not provided
	rootCmd.SetArgs([]string{"defaults", "export"})
	err := rootCmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "output-dir")
}

func TestDefaultsExportCommand_CreatesFiles(t *testing.T) {
	// Test that the command creates all expected files
	tmpDir := t.TempDir()

	rootCmd.SetArgs([]string{"defaults", "export", "--output-dir", tmpDir})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Verify expected files were created
	expectedFiles := []string{
		"maybe-dont.yaml",
		"cel_request_rules.yaml",
		"ai_request_rules.yaml",
		"cel_response_rules.yaml",
		"ai_response_rules.yaml",
	}

	for _, filename := range expectedFiles {
		path := filepath.Join(tmpDir, filename)
		info, err := os.Stat(path)
		require.NoError(t, err, "File %s should exist", filename)
		require.True(t, info.Size() > 0, "File %s should have content", filename)
	}
}

func TestDefaultsExportCommand_OverwritesExisting(t *testing.T) {
	// Test that the command overwrites existing files
	tmpDir := t.TempDir()

	// Create an existing file with custom content
	existingPath := filepath.Join(tmpDir, "maybe-dont.yaml")
	customContent := "# Custom content that should be overwritten\n"
	err := os.WriteFile(existingPath, []byte(customContent), 0600)
	require.NoError(t, err)

	// Run defaults export
	rootCmd.SetArgs([]string{"defaults", "export", "--output-dir", tmpDir})
	err = rootCmd.Execute()
	require.NoError(t, err)

	// Verify content was overwritten
	content, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	require.NotEqual(t, customContent, string(content), "File should be overwritten")
	require.Contains(t, string(content), "Maybe Don't AI", "Should contain default content")
}
