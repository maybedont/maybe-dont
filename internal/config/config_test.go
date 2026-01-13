package config

import (
	"os"
	"reflect"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDefaultServerType(t *testing.T) {
	// Test that when no server type is configured, it defaults to stdio
	config := &Config{}

	// Simulate the default setting logic from LoadConfig
	if config.Server.Type == "" {
		config.Server.Type = ServerTypeSTDIO
	}

	if config.Server.Type != ServerTypeSTDIO {
		t.Errorf("Expected default server type to be %s, got %s", ServerTypeSTDIO, config.Server.Type)
	}
}

func TestServerTypeValidation(t *testing.T) {
	tests := []struct {
		name        string
		serverType  ServerType
		listenAddr  string
		shouldError bool
	}{
		{"stdio", ServerTypeSTDIO, "", false},
		{"http", ServerTypeHTTP, ":8080", false},
		{"sse", ServerTypeSSE, ":8080", false},
		{"invalid", "invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Server: struct {
					Type       ServerType `mapstructure:"type"`
					ListenAddr string     `mapstructure:"listen_addr"`
					SSE        struct {
						TLS struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
					TrustedProxies []string `mapstructure:"trusted_proxies"`
				}{
					Type:       tt.serverType,
					ListenAddr: tt.listenAddr,
				},
				DownstreamMCPServers: map[string]ClientConfig{
					"test": {
						Type:    "stdio",
						Command: "echo",
					},
				},
				Audit: struct {
					Enabled bool   `mapstructure:"enabled"`
					Path    string `mapstructure:"path"`
				}{
					Path: "audit.log",
				},
			}

			err := ValidateConfig(config)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for server type %s, but got none", tt.serverType)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for server type %s, but got: %v", tt.serverType, err)
			}
		})
	}
}

func TestListenAddrValidation(t *testing.T) {
	tests := []struct {
		name        string
		serverType  ServerType
		listenAddr  string
		shouldError bool
	}{
		{"stdio with empty listen addr", ServerTypeSTDIO, "", false},
		{"http with empty listen addr", ServerTypeHTTP, "", true},
		{"sse with empty listen addr", ServerTypeSSE, "", true},
		{"stdio with listen addr", ServerTypeSTDIO, ":8080", false},
		{"http with listen addr", ServerTypeHTTP, ":8080", false},
		{"sse with listen addr", ServerTypeSSE, ":8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Server: struct {
					Type       ServerType `mapstructure:"type"`
					ListenAddr string     `mapstructure:"listen_addr"`
					SSE        struct {
						TLS struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
					TrustedProxies []string `mapstructure:"trusted_proxies"`
				}{
					Type:       tt.serverType,
					ListenAddr: tt.listenAddr,
				},
				DownstreamMCPServers: map[string]ClientConfig{
					"test": {
						Type:    "stdio",
						Command: "echo",
					},
				},
				Audit: struct {
					Enabled bool   `mapstructure:"enabled"`
					Path    string `mapstructure:"path"`
				}{
					Path: "audit.log",
				},
			}

			err := ValidateConfig(config)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for %s, but got none", tt.name)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for %s, but got: %v", tt.name, err)
			}
		})
	}
}

func TestExpandEnvironmentVariables(t *testing.T) {
	// Set up test environment variables
	err := os.Setenv("TEST_AUTH_TOKEN", "test-token-123")
	require.NoError(t, err)
	err = os.Setenv("TEST_URL", "https://example.com")
	require.NoError(t, err)
	defer func() {
		_ = os.Unsetenv("TEST_AUTH_TOKEN")
		_ = os.Unsetenv("TEST_URL")
	}()

	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "expand string with env var",
			input:    "${TEST_AUTH_TOKEN}",
			expected: "test-token-123",
		},
		{
			name:     "expand string with multiple env vars",
			input:    "${TEST_URL}/path/${TEST_AUTH_TOKEN}",
			expected: "https://example.com/path/test-token-123",
		},
		{
			name:     "no expansion needed",
			input:    "literal-string",
			expected: "literal-string",
		},
		{
			name:     "missing env var becomes empty",
			input:    "${MISSING_VAR}",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a struct with a string field to test
			type testStruct struct {
				Field string
			}

			test := &testStruct{Field: tt.input.(string)}
			expandEnvironmentVariables(reflect.ValueOf(test).Elem())

			if test.Field != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, test.Field)
			}
		})
	}
}

func TestExpandEnvironmentVariablesInHeaders(t *testing.T) {
	// Set up test environment variables
	err := os.Setenv("TEST_GITHUB_TOKEN", "ghp_test123")
	require.NoError(t, err)
	err = os.Setenv("TEST_API_KEY", "api_key_456")
	require.NoError(t, err)
	defer func() {
		_ = os.Unsetenv("TEST_GITHUB_TOKEN")
		_ = os.Unsetenv("TEST_API_KEY")
	}()

	// Test with ClientConfig structure
	config := ClientConfig{
		Type: "http",
		HTTPConfig: struct {
			Headers map[string]string `mapstructure:"headers"`
		}{
			Headers: map[string]string{
				"Authorization": "Bearer ${TEST_GITHUB_TOKEN}",
				"X-API-Key":     "${TEST_API_KEY}",
				"Content-Type":  "application/json", // No expansion needed
			},
		},
	}

	expandEnvironmentVariables(reflect.ValueOf(&config).Elem())

	expectedHeaders := map[string]string{
		"Authorization": "Bearer ghp_test123",
		"X-API-Key":     "api_key_456",
		"Content-Type":  "application/json",
	}

	for key, expected := range expectedHeaders {
		if actual := config.HTTPConfig.Headers[key]; actual != expected {
			t.Errorf("Header %s: expected %q, got %q", key, expected, actual)
		}
	}
}

func TestValidateConfigCollectsAllErrors(t *testing.T) {
	// Test configuration with multiple errors
	config := &Config{
		Server: struct {
			Type       ServerType `mapstructure:"type"`
			ListenAddr string     `mapstructure:"listen_addr"`
			SSE        struct {
				TLS struct {
					Enabled  bool   `mapstructure:"enabled"`
					CertFile string `mapstructure:"cert_file"`
					KeyFile  string `mapstructure:"key_file"`
				} `mapstructure:"tls"`
			} `mapstructure:"sse"`
			TrustedProxies []string `mapstructure:"trusted_proxies"`
		}{
			Type: "invalid-type", // Error 1: invalid server type
		},
		DownstreamMCPServers: map[string]ClientConfig{
			"test1": {
				Type: "stdio",
				// Missing Command - Error 2
				StartupTimeoutMs:      -1,   // Error 3: negative timeout
				InitializationRetries: 20,   // Error 4: too many retries
				RetryDelayMs:          -100, // Error 5: negative retry delay
			},
			"test2": {
				Type: "http",
				// Missing DownstreamURL - Error 6
				CapabilityDiscoveryDelayMs: -1,    // Error 7: negative delay
				CapabilityDiscoveryRetries: 15,    // Error 8: too many retries
				CapabilityRetryDelayMs:     40000, // Error 9: delay too large
			},
		},
		Audit: struct {
			Enabled bool   `mapstructure:"enabled"`
			Path    string `mapstructure:"path"`
		}{
			// No required fields in Audit anymore
		},
		AIPolicyValidation: struct {
			Enabled   *bool      `mapstructure:"enabled"`
			Mode      PolicyMode `mapstructure:"mode"`
			Endpoint  string     `mapstructure:"endpoint"`
			Model     string     `mapstructure:"model"`
			RulesFile string     `mapstructure:"rules_file"`
			APIKey    string     `mapstructure:"api_key"`
			Rules     []AIPolicy `mapstructure:"rules"`
		}{
			Mode: PolicyModeEnabled, // Use Mode instead of deprecated Enabled
			// Missing APIKey - Error 11
			// Missing Endpoint - Error 12
			// Missing Model - Error 13
		},
	}

	err := ValidateConfig(config)
	require.Error(t, err)

	// Check that the error message contains all expected errors
	errMsg := err.Error()

	// Check for multiple errors reported
	require.Contains(t, errMsg, "13 error(s)")

	// Check for specific errors
	require.Contains(t, errMsg, "invalid server type: invalid-type")
	require.Contains(t, errMsg, "downstream_mcp_servers[test1].command is required")
	require.Contains(t, errMsg, "downstream_mcp_servers[test1].startup_timeout_ms must be non-negative")
	require.Contains(t, errMsg, "downstream_mcp_servers[test1].initialization_retries must be less than 10")
	require.Contains(t, errMsg, "downstream_mcp_servers[test1].retry_delay_ms must be non-negative")
	require.Contains(t, errMsg, "downstream_mcp_servers[test2].downstream_url")
	require.Contains(t, errMsg, "downstream_mcp_servers[test2].capability_discovery_delay_ms must be non-negative")
	require.Contains(t, errMsg, "downstream_mcp_servers[test2].capability_discovery_retries must be less than 10")
	require.Contains(t, errMsg, "downstream_mcp_servers[test2].capability_retry_delay_ms must be less than 30000ms")
	require.Contains(t, errMsg, "ai_validation.api_key is required")
	require.Contains(t, errMsg, "ai_validation.endpoint is required")
	require.Contains(t, errMsg, "ai_validation.model is required")
}

func TestValidateConfigSuccess(t *testing.T) {
	// Test configuration with no errors
	config := &Config{
		Server: struct {
			Type       ServerType `mapstructure:"type"`
			ListenAddr string     `mapstructure:"listen_addr"`
			SSE        struct {
				TLS struct {
					Enabled  bool   `mapstructure:"enabled"`
					CertFile string `mapstructure:"cert_file"`
					KeyFile  string `mapstructure:"key_file"`
				} `mapstructure:"tls"`
			} `mapstructure:"sse"`
			TrustedProxies []string `mapstructure:"trusted_proxies"`
		}{
			Type: ServerTypeSTDIO,
		},
		DownstreamMCPServers: map[string]ClientConfig{
			"test": {
				Type:    "stdio",
				Command: "echo",
			},
		},
		Audit: struct {
			Enabled bool   `mapstructure:"enabled"`
			Path    string `mapstructure:"path"`
		}{
			Path: "audit.log",
		},
	}

	err := ValidateConfig(config)
	require.NoError(t, err)
}

func TestLoadConfigWithEnvironmentVariableOverride(t *testing.T) {
	// Reset viper to avoid state from previous tests
	viper.Reset()

	// Save and clear any existing audit/logging path env vars that might interfere
	oldAuditPath := os.Getenv("MAYBE_DONT_AUDIT_PATH")
	oldLoggingPath := os.Getenv("MAYBE_DONT_LOGGING_PATH")
	_ = os.Unsetenv("MAYBE_DONT_AUDIT_PATH")
	_ = os.Unsetenv("MAYBE_DONT_LOGGING_PATH")
	defer func() {
		if oldAuditPath != "" {
			_ = os.Setenv("MAYBE_DONT_AUDIT_PATH", oldAuditPath)
		}
		if oldLoggingPath != "" {
			_ = os.Setenv("MAYBE_DONT_LOGGING_PATH", oldLoggingPath)
		}
	}()

	// Create a temporary directory and config file
	tmpDir := t.TempDir()
	configPath := tmpDir + "/gateway-config.yaml"

	// Create a minimal config file without API key
	configContent := `
server:
  type: stdio

downstream_mcp_servers:
  test:
    type: stdio
    command: echo

# Explicitly disable CEL policy validation (it defaults to enabled)
policy_validation:
  mode: disabled

ai_validation:
  enabled: true
  endpoint: https://api.openai.com/v1
  model: gpt-4
  rules_file: ai.rules.yaml
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Create a rules file
	rulesContent := `
rules:
  - name: test-rule
    description: Test rule
    prompt: Test prompt
    message: Test message
`
	err = os.WriteFile(tmpDir+"/ai.rules.yaml", []byte(rulesContent), 0644)
	require.NoError(t, err)

	// Set the API key via environment variable
	err = os.Setenv("MAYBE_DONT_AI_VALIDATION_API_KEY", "test-api-key-from-env")
	require.NoError(t, err)
	defer func() {
		_ = os.Unsetenv("MAYBE_DONT_AI_VALIDATION_API_KEY")
	}()

	// Load config - need to reset viper to avoid state from previous tests
	// Note: This test modifies global viper state, so it may interfere with other tests
	// if run in parallel. For proper isolation, viper would need to be injected.

	config, err := LoadConfig(tmpDir, "")
	require.NoError(t, err)

	// Verify the API key was loaded from the environment variable
	require.Equal(t, "test-api-key-from-env", config.AIPolicyValidation.APIKey,
		"API key should be loaded from MAYBE_DONT_AI_VALIDATION_API_KEY environment variable")
}

func TestApplyEnvironmentOverrides(t *testing.T) {
	// Test that applyEnvironmentOverrides works for various nested config paths

	tests := []struct {
		name     string
		envVar   string
		envValue string
		check    func(*Config) string
	}{
		{
			name:     "ai_validation.api_key",
			envVar:   "MAYBE_DONT_AI_VALIDATION_API_KEY",
			envValue: "ai-key-123",
			check:    func(c *Config) string { return c.AIPolicyValidation.APIKey },
		},
		{
			name:     "ai_validation.endpoint",
			envVar:   "MAYBE_DONT_AI_VALIDATION_ENDPOINT",
			envValue: "https://custom-endpoint.example.com",
			check:    func(c *Config) string { return c.AIPolicyValidation.Endpoint },
		},
		{
			name:     "ai_validation.model",
			envVar:   "MAYBE_DONT_AI_VALIDATION_MODEL",
			envValue: "gpt-4-turbo",
			check:    func(c *Config) string { return c.AIPolicyValidation.Model },
		},
		{
			name:     "native_tools.audit_report.api_key",
			envVar:   "MAYBE_DONT_NATIVE_TOOLS_AUDIT_REPORT_API_KEY",
			envValue: "audit-key-456",
			check:    func(c *Config) string { return c.NativeTools.AuditReport.APIKey },
		},
		{
			name:     "audit.path",
			envVar:   "MAYBE_DONT_AUDIT_PATH",
			envValue: "custom-audit.log",
			check:    func(c *Config) string { return c.Audit.Path },
		},
		{
			name:     "logging.level",
			envVar:   "MAYBE_DONT_LOGGING_LEVEL",
			envValue: "debug",
			check:    func(c *Config) string { return c.Logging.LogLevel },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variable
			err := os.Setenv(tt.envVar, tt.envValue)
			require.NoError(t, err)
			defer func() {
				_ = os.Unsetenv(tt.envVar)
			}()

			// Create a config with empty values
			config := &Config{}

			// Apply environment overrides with MAYBE_DONT prefix
			applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

			// Verify the value was set
			actual := tt.check(config)
			require.Equal(t, tt.envValue, actual,
				"Expected %s to be set from %s", tt.name, tt.envVar)
		})
	}
}

// TestViperConfigPathsMatchStruct validates that the string paths used for viper.SetDefault
// actually correspond to real fields in the Config struct. This prevents silent breakage
// if struct field names or mapstructure tags are changed.
func TestViperConfigPathsMatchStruct(t *testing.T) {
	// These are the paths used in LoadConfig's viper.SetDefault calls
	// If you add new SetDefault calls, add the paths here
	viperPaths := []string{
		"native_tools.audit_log.enabled",
		"native_tools.audit_report.enabled",
		"native_tools.list_servers.enabled",
		"native_tools.list_sessions.enabled",
		"native_tools.audit_log.max_entries",
		"native_tools.audit_report.max_entries",
		"logging.path",
		"audit.path",
	}

	for _, path := range viperPaths {
		t.Run(path, func(t *testing.T) {
			err := validateConfigPath(path, reflect.TypeOf(Config{}))
			require.NoError(t, err, "Config path %q does not match Config struct", path)
		})
	}
}

// validateConfigPath checks if a dot-separated path (e.g., "native_tools.audit_log.enabled")
// corresponds to a valid field path in the given struct type using mapstructure tags.
func validateConfigPath(path string, t reflect.Type) error {
	parts := splitPath(path)
	return validatePathParts(parts, t, "")
}

// splitPath splits a dot-separated path into parts
func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// validatePathParts recursively validates that path parts match struct fields via mapstructure tags
func validatePathParts(parts []string, t reflect.Type, currentPath string) error {
	if len(parts) == 0 {
		return nil
	}

	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return &configPathError{
			path:    currentPath,
			message: "expected struct type",
		}
	}

	targetTag := parts[0]
	remainingParts := parts[1:]

	// Find the field with matching mapstructure tag
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("mapstructure")
		if tag == targetTag {
			// Found the field
			newPath := targetTag
			if currentPath != "" {
				newPath = currentPath + "." + targetTag
			}

			if len(remainingParts) == 0 {
				// This is the final path component - success
				return nil
			}

			// Continue validating remaining parts
			return validatePathParts(remainingParts, field.Type, newPath)
		}
	}

	// No field found with matching tag
	fullPath := targetTag
	if currentPath != "" {
		fullPath = currentPath + "." + targetTag
	}
	return &configPathError{
		path:    fullPath,
		message: "no field found with mapstructure tag",
	}
}

type configPathError struct {
	path    string
	message string
}

func (e *configPathError) Error() string {
	return e.path + ": " + e.message
}

func TestExpandEnvironmentVariablesInMultipleClients(t *testing.T) {
	// Set up test environment variables
	err := os.Setenv("GITHUB_AUTH", "github-token")
	require.NoError(t, err)
	err = os.Setenv("AWS_KEY", "aws-key")
	require.NoError(t, err)
	defer func() {
		err := os.Unsetenv("GITHUB_AUTH")
		require.NoError(t, err)
		err = os.Unsetenv("AWS_KEY")
		require.NoError(t, err)
	}()

	config := Config{
		DownstreamMCPServers: map[string]ClientConfig{
			"github": {
				Type:          "http",
				DownstreamURL: "${GITHUB_URL}",
				HTTPConfig: struct {
					Headers map[string]string `mapstructure:"headers"`
				}{
					Headers: map[string]string{
						"Authorization": "Bearer ${GITHUB_AUTH}",
					},
				},
			},
			"aws": {
				Type:        "stdio",
				Command:     "uvx",
				CommandArgs: []string{"awslabs.aws-documentation-mcp-server@latest"},
			},
		},
	}

	expandEnvironmentVariables(reflect.ValueOf(&config).Elem())

	// Check that environment variables were expanded in the github client headers
	githubClient := config.DownstreamMCPServers["github"]
	if auth := githubClient.HTTPConfig.Headers["Authorization"]; auth != "Bearer github-token" {
		t.Errorf("Expected 'Bearer github-token', got %q", auth)
	}

	// Check that DownstreamURL with missing env var becomes empty
	if githubClient.DownstreamURL != "" {
		t.Errorf("Expected empty string for missing GITHUB_URL, got %q", githubClient.DownstreamURL)
	}

	// Check that aws client (stdio) is unchanged
	awsClient := config.DownstreamMCPServers["aws"]
	if awsClient.Command != "uvx" {
		t.Errorf("Expected 'uvx', got %q", awsClient.Command)
	}
}

func TestValidateNativeToolsAuditLogMaxEntries(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		maxEntries  int
		shouldError bool
		errorMsg    string
	}{
		{"disabled with zero value", false, 0, false, ""},
		{"disabled with invalid value", false, 5, false, ""},
		{"enabled with valid min value", true, 10, false, ""},
		{"enabled with valid mid value", true, 100, false, ""},
		{"enabled with valid max value", true, 500, false, ""},
		{"enabled with value below min", true, 9, true, "configuration validation failed with 1 error(s):\n  1. native_tools.audit_log.max_entries is invalid. The value must be >= 10 and <= 500\n"},
		{"enabled with value above max", true, 501, true, "configuration validation failed with 1 error(s):\n  1. native_tools.audit_log.max_entries is invalid. The value must be >= 10 and <= 500\n"},
		{"enabled with zero value", true, 0, true, "configuration validation failed with 1 error(s):\n  1. native_tools.audit_log.max_entries is invalid. The value must be >= 10 and <= 500\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createValidBaseConfig()
			config.NativeTools.AuditLog.Enabled = tt.enabled
			config.NativeTools.AuditLog.MaxEntries = tt.maxEntries

			err := ValidateConfig(config)

			if tt.shouldError {
				require.Error(t, err)
				require.Equal(t, tt.errorMsg, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateNativeToolsAuditReportMaxEntries(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		maxEntries  int
		shouldError bool
		errorMsg    string
	}{
		{"disabled with zero value", false, 0, false, ""},
		{"disabled with invalid value", false, 5, false, ""},
		{"enabled with valid min value", true, 10, false, ""},
		{"enabled with valid mid value", true, 1000, false, ""},
		{"enabled with valid max value", true, 2000, false, ""},
		{"enabled with value below min", true, 9, true, "configuration validation failed with 1 error(s):\n  1. native_tools.audit_report.max_entries is invalid. The value must be >= 10 and <= 2000\n"},
		{"enabled with value above max", true, 2001, true, "configuration validation failed with 1 error(s):\n  1. native_tools.audit_report.max_entries is invalid. The value must be >= 10 and <= 2000\n"},
		{"enabled with zero value", true, 0, true, "configuration validation failed with 1 error(s):\n  1. native_tools.audit_report.max_entries is invalid. The value must be >= 10 and <= 2000\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createValidBaseConfig()
			config.NativeTools.AuditReport.Enabled = tt.enabled
			config.NativeTools.AuditReport.MaxEntries = tt.maxEntries

			err := ValidateConfig(config)

			if tt.shouldError {
				require.Error(t, err)
				require.Equal(t, tt.errorMsg, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRelativePath_ValidPaths(t *testing.T) {
	// These paths should all be valid
	validPaths := []string{
		"audit.log",
		"logs/audit.log",
		"logs/subdir/audit.log",
		"my-file.log",
		"my_file.log",
		"file123.log",
		"logs/2024/01/audit.log",
		"a/b/c/d/e/f/g.log",
		"CamelCase/File.Log",
		"file/",  // trailing slash is allowed
	}

	for _, path := range validPaths {
		t.Run(path, func(t *testing.T) {
			err := ValidateRelativePath(path)
			require.NoError(t, err, "Path %q should be valid", path)
		})
	}
}

func TestValidateRelativePath_ParentDirectoryTraversal(t *testing.T) {
	// These paths all attempt parent directory traversal and should be rejected
	tests := []struct {
		name string
		path string
	}{
		{"simple parent ref", ".."},
		{"parent then file", "../file.log"},
		{"parent with subdir", "../logs/file.log"},
		{"double parent", "../../file.log"},
		{"hidden parent traversal", "logs/../../../etc/passwd"},
		{"parent in middle", "logs/../file.log"},
		{"parent at end", "logs/.."},
		{"triple dots", "logs/.../file.log"},
		{"backslash parent", "..\\file.log"},
		{"mixed separators parent", "logs\\..\\file.log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelativePath(tt.path)
			require.Error(t, err, "Path %q should be rejected", tt.path)
		})
	}
}

func TestValidateRelativePath_AbsolutePaths(t *testing.T) {
	// Absolute paths should be rejected
	tests := []struct {
		name string
		path string
	}{
		{"unix absolute", "/etc/passwd"},
		{"unix absolute with subdir", "/var/log/audit.log"},
		{"root only", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelativePath(tt.path)
			require.Error(t, err, "Path %q should be rejected", tt.path)
		})
	}
}

func TestValidateRelativePath_HiddenFiles(t *testing.T) {
	// Hidden files/directories should be rejected
	tests := []struct {
		name string
		path string
	}{
		{"hidden file", ".hidden"},
		{"hidden file in subdir", "logs/.hidden"},
		{"hidden dir", ".hidden/file.log"},
		{"hidden dir in path", "logs/.hidden/file.log"},
		{"dotfile", ".bashrc"},
		{"dot dir", ".config/file.log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelativePath(tt.path)
			require.Error(t, err, "Path %q should be rejected", tt.path)
		})
	}
}

func TestValidateRelativePath_URLEncodedTraversal(t *testing.T) {
	// URL-encoded traversal attempts should be rejected
	tests := []struct {
		name string
		path string
	}{
		{"encoded dot", "%2e%2e/file.log"},
		{"encoded slash", "logs%2f..%2ffile.log"},
		{"encoded backslash", "logs%5c..%5cfile.log"},
		{"encoded null", "file%00.log"},
		{"double encoded dot", "%252e%252e/file.log"},
		{"overlong utf8 dot", "%c0%ae%c0%ae/file.log"},
		{"overlong utf8 slash", "logs%c0%af..%c0%affile.log"},
		{"mixed case encoding", "%2E%2E/file.log"},
		{"uppercase encoding", "%2F"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelativePath(tt.path)
			require.Error(t, err, "Path %q should be rejected", tt.path)
		})
	}
}

func TestValidateRelativePath_ControlCharacters(t *testing.T) {
	// Control characters should be rejected
	tests := []struct {
		name string
		path string
	}{
		{"null byte", "file\x00.log"},
		{"null in middle", "logs/fi\x00le.log"},
		{"newline", "file\n.log"},
		{"carriage return", "file\r.log"},
		{"bell", "file\x07.log"},
		{"escape", "file\x1b.log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelativePath(tt.path)
			require.Error(t, err, "Path %q should be rejected", tt.path)
		})
	}
}

func TestValidateRelativePath_EmptyAndWhitespace(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		shouldError bool
	}{
		{"empty string", "", true},
		{"double slash", "logs//file.log", true},
		{"triple slash", "logs///file.log", true},
		{"leading slash", "/logs/file.log", true},
		{"leading space", " file.log", true},
		{"trailing space", "file.log ", true},
		{"space in component", "logs/ file.log", true},
		{"only spaces", "   ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelativePath(tt.path)
			if tt.shouldError {
				require.Error(t, err, "Path %q should be rejected", tt.path)
			} else {
				require.NoError(t, err, "Path %q should be valid", tt.path)
			}
		})
	}
}

func TestValidateRelativePath_WindowsSpecific(t *testing.T) {
	// Windows-specific path issues that should be rejected on all platforms
	tests := []struct {
		name string
		path string
	}{
		{"drive letter", "C:file.log"},
		{"drive with path", "C:\\logs\\file.log"},
		{"alternate data stream", "file.log:hidden"},
		{"UNC path style", "\\\\server\\share\\file.log"},
		{"backslash traversal", "logs\\..\\..\\etc\\passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelativePath(tt.path)
			require.Error(t, err, "Path %q should be rejected", tt.path)
		})
	}
}

func TestValidateRelativePath_CurrentDirectory(t *testing.T) {
	// Current directory references should be rejected
	tests := []struct {
		name string
		path string
	}{
		{"single dot", "."},
		{"dot slash file", "./file.log"},
		{"dot in middle", "logs/./file.log"},
		{"multiple dots in middle", "logs/././file.log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelativePath(tt.path)
			require.Error(t, err, "Path %q should be rejected", tt.path)
		})
	}
}

func TestValidateConfig_LoggingPathWithSubdirectory(t *testing.T) {
	// Test that subdirectories are now allowed in logging.path
	config := createValidBaseConfig()
	config.Logging.Path = "logs/subdir/app.log"

	err := ValidateConfig(config)
	require.NoError(t, err, "Subdirectory paths should be allowed for logging.path")
}

func TestValidateConfig_AuditPathWithSubdirectory(t *testing.T) {
	// Test that subdirectories are now allowed in audit.path
	config := createValidBaseConfig()
	config.Audit.Path = "audit/2024/01/audit.log"

	err := ValidateConfig(config)
	require.NoError(t, err, "Subdirectory paths should be allowed for audit.path")
}

func TestValidateConfig_LoggingPathTraversalRejected(t *testing.T) {
	// Test that path traversal is rejected in logging.path
	config := createValidBaseConfig()
	config.Logging.Path = "../../../etc/passwd"

	err := ValidateConfig(config)
	require.Error(t, err, "Path traversal should be rejected in logging.path")
	require.Contains(t, err.Error(), "logging.path")
}

func TestValidateConfig_AuditPathTraversalRejected(t *testing.T) {
	// Test that path traversal is rejected in audit.path
	config := createValidBaseConfig()
	config.Audit.Path = "logs/../../../etc/passwd"

	err := ValidateConfig(config)
	require.Error(t, err, "Path traversal should be rejected in audit.path")
	require.Contains(t, err.Error(), "audit.path")
}

// createValidBaseConfig creates a Config with all required fields set to valid values.
// Use this as a starting point when testing specific validation rules.
func createValidBaseConfig() *Config {
	return &Config{
		Server: struct {
			Type       ServerType `mapstructure:"type"`
			ListenAddr string     `mapstructure:"listen_addr"`
			SSE        struct {
				TLS struct {
					Enabled  bool   `mapstructure:"enabled"`
					CertFile string `mapstructure:"cert_file"`
					KeyFile  string `mapstructure:"key_file"`
				} `mapstructure:"tls"`
			} `mapstructure:"sse"`
			TrustedProxies []string `mapstructure:"trusted_proxies"`
		}{
			Type: ServerTypeSTDIO,
		},
		DownstreamMCPServers: map[string]ClientConfig{
			"test": {
				Type:    "stdio",
				Command: "echo",
			},
		},
		Audit: struct {
			Enabled bool   `mapstructure:"enabled"`
			Path    string `mapstructure:"path"`
		}{
			Path: "audit.log",
		},
		NativeTools: struct {
			AuditLog struct {
				Enabled    bool `mapstructure:"enabled"`
				MaxEntries int  `mapstructure:"max_entries"`
			} `mapstructure:"audit_log"`
			AuditReport struct {
				Enabled      bool   `mapstructure:"enabled"`
				Endpoint     string `mapstructure:"endpoint"`
				Model        string `mapstructure:"model"`
				APIKey       string `mapstructure:"api_key"`
				MaxEntries   int    `mapstructure:"max_entries"`
				SystemPrompt string `mapstructure:"system_prompt"`
			} `mapstructure:"audit_report"`
			ListServers struct {
				Enabled bool `mapstructure:"enabled"`
			} `mapstructure:"list_servers"`
			ListSessions struct {
				Enabled bool `mapstructure:"enabled"`
			} `mapstructure:"list_sessions"`
		}{
			AuditLog: struct {
				Enabled    bool `mapstructure:"enabled"`
				MaxEntries int  `mapstructure:"max_entries"`
			}{
				Enabled:    false,
				MaxEntries: 100,
			},
			AuditReport: struct {
				Enabled      bool   `mapstructure:"enabled"`
				Endpoint     string `mapstructure:"endpoint"`
				Model        string `mapstructure:"model"`
				APIKey       string `mapstructure:"api_key"`
				MaxEntries   int    `mapstructure:"max_entries"`
				SystemPrompt string `mapstructure:"system_prompt"`
			}{
				Enabled:    false,
				MaxEntries: 1000,
			},
		},
	}
}
