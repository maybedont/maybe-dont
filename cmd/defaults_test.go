package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultsExportCommand_RequiresOutputDir(t *testing.T) {
	// Test that the command fails when --output-dir is not provided
	rootCmd.SetArgs([]string{"gateway", "defaults", "export"})
	err := rootCmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "output-dir")
}

func TestDefaultsExportCommand_CreatesFiles(t *testing.T) {
	// Test that the command creates all expected files in an empty directory
	tmpDir := t.TempDir()

	rootCmd.SetArgs([]string{"gateway", "defaults", "export", "--output-dir", tmpDir})
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

func TestDefaultsExportCommand_SkipsExistingByDefault(t *testing.T) {
	// Test that existing files are preserved without --force
	tmpDir := t.TempDir()

	// Create an existing file with custom content
	existingPath := filepath.Join(tmpDir, "maybe-dont.yaml")
	customContent := "# Custom content that should be preserved\n"
	err := os.WriteFile(existingPath, []byte(customContent), 0o600)
	require.NoError(t, err)

	// Run defaults export without --force
	rootCmd.SetArgs([]string{"gateway", "defaults", "export", "--output-dir", tmpDir})
	err = rootCmd.Execute()
	require.NoError(t, err)

	// Verify existing content was preserved
	content, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	require.Equal(t, customContent, string(content), "Existing file should be preserved without --force")

	// Other files should still be created
	for _, filename := range []string{"cel_request_rules.yaml", "ai_request_rules.yaml"} {
		path := filepath.Join(tmpDir, filename)
		_, err := os.Stat(path)
		require.NoError(t, err, "File %s should have been created", filename)
	}
}

func TestDefaultsExportCommand_OverwritesWithForce(t *testing.T) {
	// Test that --force overwrites existing files
	tmpDir := t.TempDir()

	// Create an existing file with custom content
	existingPath := filepath.Join(tmpDir, "maybe-dont.yaml")
	customContent := "# Custom content that should be overwritten\n"
	err := os.WriteFile(existingPath, []byte(customContent), 0o600)
	require.NoError(t, err)

	// Run defaults export with --force
	rootCmd.SetArgs([]string{"gateway", "defaults", "export", "--output-dir", tmpDir, "--force"})
	err = rootCmd.Execute()
	require.NoError(t, err)

	// Verify content was overwritten
	content, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	require.NotEqual(t, customContent, string(content), "File should be overwritten with --force")
	require.Contains(t, string(content), "Maybe Don't AI", "Should contain default content")
}
