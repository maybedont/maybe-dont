//go:build windows

package cliproxy

import (
	"os"
	"os/exec"
)

// ExecuteCommand runs the specified command and waits for it to complete.
// Unlike Unix, Windows cannot replace the process with syscall.Exec, so we
// run the command as a child process and forward all I/O.
//
// The command is looked up in PATH using exec.LookPath. If args is provided,
// they are passed as arguments to the command. If env is nil, the current
// process environment is used.
//
// Returns nil on success, or an error (including exit code errors).
func ExecuteCommand(command string, args []string, env []string) error {
	// Find the command in PATH to provide consistent error handling with Unix
	path, err := exec.LookPath(command)
	if err != nil {
		return err
	}

	cmd := exec.Command(path, args...)

	// Forward I/O to make this as transparent as possible
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set environment if provided, otherwise inherit current environment
	if env != nil {
		cmd.Env = env
	}

	// Run and wait for completion
	return cmd.Run()
}
