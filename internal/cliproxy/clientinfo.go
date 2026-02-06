package cliproxy

import (
	"os"
	"os/user"
	"runtime"
)

// CollectClientInfo gathers client environment information for audit attribution.
// The cliVersion should be passed from build-time version info.
//
// This function is designed to be resilient - it never panics and ignores errors
// gracefully. If a particular piece of information cannot be collected, the
// corresponding field is left empty.
func CollectClientInfo(cliVersion string) *ClientInfo {
	info := &ClientInfo{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		CLIVersion: cliVersion,
	}

	// Hostname - ignore errors, leave empty on failure
	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	}

	// Username - ignore errors, leave empty on failure
	if currentUser, err := user.Current(); err == nil {
		info.Username = currentUser.Username
	}

	// Shell - platform-specific environment variable
	info.Shell = getShellFromEnv()

	// OSVersion - best-effort, platform-specific
	info.OSVersion = getOSVersion()

	return info
}

// getShellFromEnv returns the user's shell from environment variables.
// On Unix-like systems, this is $SHELL. On Windows, this is $COMSPEC.
func getShellFromEnv() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("COMSPEC")
	}
	return os.Getenv("SHELL")
}

// getOSVersion returns the OS version string.
// This is best-effort and returns empty string if version cannot be determined.
// Platform-specific implementations can be added as needed.
func getOSVersion() string {
	// Best-effort OS version detection
	// For now, return empty - can be extended with platform-specific logic
	// such as reading /etc/os-release on Linux, or using syscalls on macOS/Windows
	return ""
}
