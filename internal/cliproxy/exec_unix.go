//go:build !windows

package cliproxy

import (
	"os"
	"os/exec"
	"syscall"
)

// ExecuteCommand replaces the current process with the specified command.
// This function does not return on success - the current process is replaced.
// On error (command not found, permission denied), it returns an error.
//
// The command is looked up in PATH using exec.LookPath. If args is provided,
// they are passed as arguments to the command (args[0] becomes argv[1], etc.).
// If env is nil, the current process environment is used.
func ExecuteCommand(command string, args []string, env []string) error {
	// Find the command in PATH
	path, err := exec.LookPath(command)
	if err != nil {
		return err
	}

	// Build argv - first element is the command name (argv[0])
	argv := append([]string{command}, args...)

	// If env is nil, use current environment
	if env == nil {
		env = os.Environ()
	}

	// Replace current process with the target command.
	// This does not return on success - the process image is replaced.
	return syscall.Exec(path, argv, env)
}
