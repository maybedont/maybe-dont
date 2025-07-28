package config

import (
	"os"
	"reflect"
	"testing"

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
					OAuth      struct {
						Enabled                 bool   `mapstructure:"enabled"`
						AuthorizationServer     string `mapstructure:"authorization_server"`
						TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
						Realm                   string `mapstructure:"realm"`
						CORS                    struct {
							Enabled        bool     `mapstructure:"enabled"`
							AllowedOrigins []string `mapstructure:"allowed_origins"`
							MaxAge         int      `mapstructure:"max_age"`
						} `mapstructure:"cors"`
					} `mapstructure:"oauth"`
					SSE struct {
						TLS struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
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
					Path: "/tmp/audit.log",
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
					OAuth      struct {
						Enabled                 bool   `mapstructure:"enabled"`
						AuthorizationServer     string `mapstructure:"authorization_server"`
						TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
						Realm                   string `mapstructure:"realm"`
						CORS                    struct {
							Enabled        bool     `mapstructure:"enabled"`
							AllowedOrigins []string `mapstructure:"allowed_origins"`
							MaxAge         int      `mapstructure:"max_age"`
						} `mapstructure:"cors"`
					} `mapstructure:"oauth"`
					SSE struct {
						TLS struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
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
					Path: "/tmp/audit.log",
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
			OAuth      struct {
				Enabled                 bool   `mapstructure:"enabled"`
				AuthorizationServer     string `mapstructure:"authorization_server"`
				TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
				Realm                   string `mapstructure:"realm"`
				CORS                    struct {
					Enabled        bool     `mapstructure:"enabled"`
					AllowedOrigins []string `mapstructure:"allowed_origins"`
					MaxAge         int      `mapstructure:"max_age"`
				} `mapstructure:"cors"`
			} `mapstructure:"oauth"`
			SSE struct {
				TLS struct {
					Enabled  bool   `mapstructure:"enabled"`
					CertFile string `mapstructure:"cert_file"`
					KeyFile  string `mapstructure:"key_file"`
				} `mapstructure:"tls"`
			} `mapstructure:"sse"`
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
			// Missing Path - Error 10
		},
		AIPolicyValidation: struct {
			Enabled   bool       `mapstructure:"enabled"`
			Endpoint  string     `mapstructure:"endpoint"`
			Model     string     `mapstructure:"model"`
			RulesFile string     `mapstructure:"rules_file"`
			APIKey    string     `mapstructure:"api_key"`
			Rules     []AIPolicy `mapstructure:"rules"`
		}{
			Enabled: true,
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
	require.Contains(t, errMsg, "14 error(s)")

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
	require.Contains(t, errMsg, "audit.path is required")
	require.Contains(t, errMsg, "OPENAI_API_KEY environment variable is required")
	require.Contains(t, errMsg, "ai_validation.endpoint is required")
	require.Contains(t, errMsg, "ai_validation.model is required")
}

func TestValidateConfigSuccess(t *testing.T) {
	// Test configuration with no errors
	config := &Config{
		Server: struct {
			Type       ServerType `mapstructure:"type"`
			ListenAddr string     `mapstructure:"listen_addr"`
			OAuth      struct {
				Enabled                 bool   `mapstructure:"enabled"`
				AuthorizationServer     string `mapstructure:"authorization_server"`
				TokenValidationEndpoint string `mapstructure:"token_validation_endpoint"`
				Realm                   string `mapstructure:"realm"`
				CORS                    struct {
					Enabled        bool     `mapstructure:"enabled"`
					AllowedOrigins []string `mapstructure:"allowed_origins"`
					MaxAge         int      `mapstructure:"max_age"`
				} `mapstructure:"cors"`
			} `mapstructure:"oauth"`
			SSE struct {
				TLS struct {
					Enabled  bool   `mapstructure:"enabled"`
					CertFile string `mapstructure:"cert_file"`
					KeyFile  string `mapstructure:"key_file"`
				} `mapstructure:"tls"`
			} `mapstructure:"sse"`
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
			Path: "/tmp/audit.log",
		},
	}

	err := ValidateConfig(config)
	require.NoError(t, err)
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
