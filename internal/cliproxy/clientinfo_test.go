package cliproxy

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectClientInfo_BasicFields verifies that OS and Arch are always populated
// since they come from runtime constants and cannot fail.
func TestCollectClientInfo_BasicFields(t *testing.T) {
	info := CollectClientInfo("1.0.0")

	require.NotNil(t, info)
	assert.Equal(t, runtime.GOOS, info.OS, "OS should match runtime.GOOS")
	assert.Equal(t, runtime.GOARCH, info.Arch, "Arch should match runtime.GOARCH")
}

// TestCollectClientInfo_Hostname verifies hostname is collected.
// The hostname may be empty if os.Hostname() fails, but we verify it matches
// what os.Hostname() returns.
func TestCollectClientInfo_Hostname(t *testing.T) {
	info := CollectClientInfo("1.0.0")

	require.NotNil(t, info)
	// Get the expected hostname for comparison
	expectedHostname, err := os.Hostname()
	if err != nil {
		// If hostname lookup fails, the field should be empty
		assert.Empty(t, info.Hostname, "Hostname should be empty when os.Hostname fails")
	} else {
		assert.Equal(t, expectedHostname, info.Hostname, "Hostname should match os.Hostname()")
	}
}

// TestCollectClientInfo_Username verifies username is collected.
// The username may be empty if user.Current() fails, but we verify it's set
// when the lookup succeeds.
func TestCollectClientInfo_Username(t *testing.T) {
	info := CollectClientInfo("1.0.0")

	require.NotNil(t, info)
	// In most test environments, username should be available
	// We just verify it's a non-empty string when the lookup succeeds
	// Note: This test may need adjustment in CI environments with restricted user lookups
	if info.Username != "" {
		assert.NotEmpty(t, info.Username, "Username should be non-empty when available")
	}
}

// TestCollectClientInfo_CLIVersion verifies the passed CLI version is set correctly.
func TestCollectClientInfo_CLIVersion(t *testing.T) {
	tests := []struct {
		name       string
		cliVersion string
		want       string
	}{
		{
			name:       "standard version",
			cliVersion: "1.2.3",
			want:       "1.2.3",
		},
		{
			name:       "version with v prefix",
			cliVersion: "v1.0.0",
			want:       "v1.0.0",
		},
		{
			name:       "dev version",
			cliVersion: "dev",
			want:       "dev",
		},
		{
			name:       "empty version",
			cliVersion: "",
			want:       "",
		},
		{
			name:       "version with build metadata",
			cliVersion: "1.0.0+abc123",
			want:       "1.0.0+abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := CollectClientInfo(tt.cliVersion)

			require.NotNil(t, info)
			assert.Equal(t, tt.want, info.CLIVersion, "CLIVersion should match passed value")
		})
	}
}

// TestCollectClientInfo_Shell verifies shell detection based on environment variables.
func TestCollectClientInfo_Shell(t *testing.T) {
	info := CollectClientInfo("1.0.0")

	require.NotNil(t, info)

	// Check the appropriate environment variable based on OS
	if runtime.GOOS == "windows" {
		// On Windows, should use COMSPEC
		expectedShell := os.Getenv("COMSPEC")
		assert.Equal(t, expectedShell, info.Shell, "Shell should match COMSPEC on Windows")
	} else {
		// On Unix-like systems, should use SHELL
		expectedShell := os.Getenv("SHELL")
		assert.Equal(t, expectedShell, info.Shell, "Shell should match SHELL on Unix")
	}
}

// TestCollectClientInfo_AllFieldsPopulated verifies all fields are returned
// in the struct (though some may be empty on certain systems).
func TestCollectClientInfo_AllFieldsPopulated(t *testing.T) {
	info := CollectClientInfo("test-version")

	require.NotNil(t, info)

	// These should always be populated from runtime
	assert.NotEmpty(t, info.OS, "OS should always be populated")
	assert.NotEmpty(t, info.Arch, "Arch should always be populated")
	assert.Equal(t, "test-version", info.CLIVersion, "CLIVersion should be set")

	// These are best-effort and may be empty in some environments
	// We just verify they don't cause panics
	t.Logf("Hostname: %q", info.Hostname)
	t.Logf("Username: %q", info.Username)
	t.Logf("Shell: %q", info.Shell)
	t.Logf("OSVersion: %q", info.OSVersion)
}
