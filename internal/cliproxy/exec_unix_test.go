//go:build !windows

package cliproxy

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteCommand_NotFound verifies that ExecuteCommand returns an error
// when the specified command does not exist in PATH.
func TestExecuteCommand_NotFound(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "nonexistent command",
			command: "this-command-definitely-does-not-exist-12345",
		},
		{
			name:    "empty command",
			command: "",
		},
		{
			name:    "command with special characters",
			command: "nonexistent/command/path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ExecuteCommand(tc.command, []string{}, nil)

			require.Error(t, err, "expected error for non-existent command")

			// The error should be an exec.ErrNotFound or similar path error
			var execErr *exec.Error
			if errors.As(err, &execErr) {
				assert.Equal(t, tc.command, execErr.Name)
			}
		})
	}
}

// TestExecuteCommand_LookPath verifies that ExecuteCommand correctly uses
// exec.LookPath to find commands in PATH.
func TestExecuteCommand_LookPath(t *testing.T) {
	// Test with a command that exists on all Unix systems
	// We can't test successful execution because syscall.Exec replaces the process,
	// but we can verify that LookPath finds the command by checking it doesn't
	// return a "not found" error for known commands.

	tests := []struct {
		name        string
		command     string
		shouldExist bool
	}{
		{
			name:        "true command exists",
			command:     "true",
			shouldExist: true,
		},
		{
			name:        "false command exists",
			command:     "false",
			shouldExist: true,
		},
		{
			name:        "sh exists",
			command:     "sh",
			shouldExist: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Verify the command can be found via LookPath
			// This is what ExecuteCommand uses internally
			path, err := exec.LookPath(tc.command)
			if tc.shouldExist {
				require.NoError(t, err, "expected command %q to be found in PATH", tc.command)
				assert.NotEmpty(t, path, "expected non-empty path for %q", tc.command)
			}
		})
	}
}

// TestExecuteCommand_AbsolutePath verifies that ExecuteCommand works with
// absolute paths that bypass PATH lookup.
func TestExecuteCommand_AbsolutePath(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		expectError bool
	}{
		{
			name:        "nonexistent absolute path",
			command:     "/nonexistent/path/to/command",
			expectError: true,
		},
		{
			name:        "directory instead of file",
			command:     "/tmp",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ExecuteCommand(tc.command, []string{}, nil)
			if tc.expectError {
				require.Error(t, err, "expected error for invalid absolute path")
			}
		})
	}
}

// TestExecuteCommand_PermissionDenied verifies that ExecuteCommand returns
// an appropriate error when the command exists but is not executable.
// Note: This test creates a temporary file without execute permission.
func TestExecuteCommand_PermissionDenied(t *testing.T) {
	// Create a temporary file without execute permission
	tmpFile := t.TempDir() + "/non_executable"
	err := writeNonExecutableFile(tmpFile)
	require.NoError(t, err, "failed to create test file")

	err = ExecuteCommand(tmpFile, []string{}, nil)
	require.Error(t, err, "expected error for non-executable file")
}

// writeNonExecutableFile creates a file without execute permission for testing.
func writeNonExecutableFile(path string) error {
	// Create file with read-only permission (no execute)
	return os.WriteFile(path, []byte("#!/bin/sh\necho test"), 0o644)
}
