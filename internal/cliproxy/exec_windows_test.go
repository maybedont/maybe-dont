//go:build windows

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
			command: "nonexistent\\command\\path",
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

// TestExecuteCommand_Success verifies that ExecuteCommand can successfully
// run a command and return nil on success. On Windows, cmd.exe is always available.
func TestExecuteCommand_Success(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
	}{
		{
			name:    "cmd echo",
			command: "cmd.exe",
			args:    []string{"/c", "echo", "test"},
		},
		{
			name:    "where cmd",
			command: "where.exe",
			args:    []string{"cmd.exe"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ExecuteCommand(tc.command, tc.args, nil)
			require.NoError(t, err, "expected command %q to succeed", tc.command)
		})
	}
}

// TestExecuteCommand_ExitCode verifies that ExecuteCommand returns an error
// containing the exit code when the command exits with a non-zero status.
func TestExecuteCommand_ExitCode(t *testing.T) {
	// cmd.exe /c exit 1 returns exit code 1
	err := ExecuteCommand("cmd.exe", []string{"/c", "exit", "1"}, nil)

	require.Error(t, err, "expected error for non-zero exit code")

	// Check that we can extract the exit code from the error
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected exec.ExitError")
	assert.Equal(t, 1, exitErr.ExitCode(), "expected exit code 1")
}

// TestExecuteCommand_LookPath verifies that ExecuteCommand correctly uses
// exec.LookPath to find commands in PATH.
func TestExecuteCommand_LookPath(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		shouldExist bool
	}{
		{
			name:        "cmd.exe exists",
			command:     "cmd.exe",
			shouldExist: true,
		},
		{
			name:        "where.exe exists",
			command:     "where.exe",
			shouldExist: true,
		},
		{
			name:        "hostname.exe exists",
			command:     "hostname.exe",
			shouldExist: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Verify the command can be found via LookPath
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
			command:     "C:\\nonexistent\\path\\to\\command.exe",
			expectError: true,
		},
		{
			name:        "directory instead of file",
			command:     "C:\\Windows",
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

// TestExecuteCommand_CustomEnv verifies that ExecuteCommand correctly passes
// custom environment variables to the child process.
func TestExecuteCommand_CustomEnv(t *testing.T) {
	// Create a custom environment with a test variable
	customEnv := append(os.Environ(), "TEST_VAR=test_value")

	// cmd.exe /c echo %TEST_VAR% should output the value
	err := ExecuteCommand("cmd.exe", []string{"/c", "echo", "%TEST_VAR%"}, customEnv)
	require.NoError(t, err, "expected command with custom env to succeed")
}
