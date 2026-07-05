package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDefaultServerType(t *testing.T) {
	// Verify that LoadConfig defaults to http with listen_addr 127.0.0.1:8080
	// when no server.type is specified in the config file.
	viper.Reset()

	tmpDir := t.TempDir()

	// Config with no server.type — LoadConfig should default to http
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "maybe-dont.yaml"), []byte(configContent), 0o644))

	config, err := LoadConfig(tmpDir, "")
	require.NoError(t, err)
	require.Equal(t, ServerTypeHTTP, config.Server.Type, "default server type should be http")
	require.Equal(t, "127.0.0.1:8080", config.Server.ListenAddr, "default listen_addr should include port")
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
				Server: ServerConfig{
					Type:       tt.serverType,
					ListenAddr: tt.listenAddr,
				},
				DownstreamMCPServers: map[string]ClientConfig{
					"test": {
						Type:    "stdio",
						Command: "echo",
					},
				},
				Audit: AuditConfig{
					Path: "audit.log",
				},
				RequestValidation: RequestValidationConfig{
					CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
					AI:  AIRequestValidationConfig{Mode: PolicyModeAuditOnly},
				},
				ResponseValidation: ResponseValidationConfig{
					CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
					AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
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
				Server: ServerConfig{
					Type:       tt.serverType,
					ListenAddr: tt.listenAddr,
				},
				DownstreamMCPServers: map[string]ClientConfig{
					"test": {
						Type:    "stdio",
						Command: "echo",
					},
				},
				Audit: AuditConfig{
					Path: "audit.log",
				},
				RequestValidation: RequestValidationConfig{
					CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
					AI:  AIRequestValidationConfig{Mode: PolicyModeAuditOnly},
				},
				ResponseValidation: ResponseValidationConfig{
					CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
					AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
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
		Server: ServerConfig{
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
		Audit: AuditConfig{
			// No required fields in Audit anymore
		},
		RequestValidation: RequestValidationConfig{
			CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
			AI: AIRequestValidationConfig{
				Enabled: true, // AI enabled without credentials - Error 11 (api_key), 12 (model)
				Mode:    PolicyModeAuditOnly,
				// Note: endpoint is not required for default provider (openai)
			},
		},
		ResponseValidation: ResponseValidationConfig{
			CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
		},
	}

	err := ValidateConfig(config)
	require.Error(t, err)

	// Check that the error message contains all expected errors
	errMsg := err.Error()

	// Check for multiple errors reported
	// Note: 12 errors because endpoint is no longer required for default provider (openai)
	require.Contains(t, errMsg, "12 error(s)")

	// Check for specific errors (new format includes env var hints for key errors)
	require.Contains(t, errMsg, "invalid server type: invalid-type")
	require.Contains(t, errMsg, "downstream_mcp_servers[test1].command")
	require.Contains(t, errMsg, "required when type is stdio")
	require.Contains(t, errMsg, "downstream_mcp_servers[test1].startup_timeout_ms must be non-negative")
	require.Contains(t, errMsg, "downstream_mcp_servers[test1].initialization_retries must be less than 10")
	require.Contains(t, errMsg, "downstream_mcp_servers[test1].retry_delay_ms must be non-negative")
	require.Contains(t, errMsg, "downstream_mcp_servers[test2].url")
	require.Contains(t, errMsg, "downstream_mcp_servers[test2].capability_discovery_delay_ms must be non-negative")
	require.Contains(t, errMsg, "downstream_mcp_servers[test2].capability_discovery_retries must be less than 10")
	require.Contains(t, errMsg, "downstream_mcp_servers[test2].capability_retry_delay_ms must be less than 30000ms")
	require.Contains(t, errMsg, "validation.ai.api_key")
	require.Contains(t, errMsg, "required when AI validation or audit report is enabled")
	// Verify env var hints are included in error messages
	require.Contains(t, errMsg, "MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_TEST1_COMMAND")
	require.Contains(t, errMsg, "MAYBE_DONT_VALIDATION_AI_API_KEY")
}

func TestValidateConfigSuccess(t *testing.T) {
	// Test configuration with no errors
	config := &Config{
		Server: ServerConfig{
			Type: ServerTypeSTDIO,
		},
		DownstreamMCPServers: map[string]ClientConfig{
			"test": {
				Type:    "stdio",
				Command: "echo",
			},
		},
		Audit: AuditConfig{
			Path: "audit.log",
		},
		RequestValidation: RequestValidationConfig{
			CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIRequestValidationConfig{Mode: PolicyModeAuditOnly},
		},
		ResponseValidation: ResponseValidationConfig{
			CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
		},
	}

	err := ValidateConfig(config)
	require.NoError(t, err)
}

func TestAIProviderValidation(t *testing.T) {
	// Helper to create a minimal valid config with AI enabled
	baseConfig := func() *Config {
		return &Config{
			Server: ServerConfig{
				Type: ServerTypeSTDIO,
			},
			DownstreamMCPServers: map[string]ClientConfig{
				"test": {Type: "stdio", Command: "echo"},
			},
			Validation: struct {
				MaxBlockingMs       int `mapstructure:"max_blocking_ms"`
				MaxRuleEvaluationMs int `mapstructure:"max_rule_evaluation_ms"`
				AI                  struct {
					Provider    string            `mapstructure:"provider"`
					Endpoint    string            `mapstructure:"endpoint"`
					Model       string            `mapstructure:"model"`
					APIKey      string            `mapstructure:"api_key"`
					Parameters  map[string]any    `mapstructure:"parameters"`
					QueryParams map[string]string `mapstructure:"query_params"`
					Headers     map[string]string `mapstructure:"headers"`
				} `mapstructure:"ai"`
			}{
				AI: struct {
					Provider    string            `mapstructure:"provider"`
					Endpoint    string            `mapstructure:"endpoint"`
					Model       string            `mapstructure:"model"`
					APIKey      string            `mapstructure:"api_key"`
					Parameters  map[string]any    `mapstructure:"parameters"`
					QueryParams map[string]string `mapstructure:"query_params"`
					Headers     map[string]string `mapstructure:"headers"`
				}{
					APIKey: "test-key",
					Model:  "gpt-4",
				},
			},
			RequestValidation: RequestValidationConfig{
				CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
				AI:  AIRequestValidationConfig{Enabled: true, Mode: PolicyModeAuditOnly},
			},
			ResponseValidation: ResponseValidationConfig{
				CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
				AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
			},
		}
	}

	tests := []struct {
		name        string
		provider    string
		endpoint    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "empty provider defaults to openai (valid)",
			provider: "",
			endpoint: "",
			wantErr:  false,
		},
		{
			name:     "explicit openai provider without endpoint (valid)",
			provider: "openai",
			endpoint: "",
			wantErr:  false,
		},
		{
			name:     "explicit openai provider with endpoint (valid)",
			provider: "openai",
			endpoint: "https://custom-proxy.com/v1",
			wantErr:  false,
		},
		{
			name:     "anthropic provider without endpoint (valid)",
			provider: "anthropic",
			endpoint: "",
			wantErr:  false,
		},
		{
			name:     "anthropic provider with endpoint (valid)",
			provider: "anthropic",
			endpoint: "https://custom-proxy.com/v1",
			wantErr:  false,
		},
		{
			name:     "openai_compatible with endpoint (valid)",
			provider: "openai_compatible",
			endpoint: "https://my-server.com/v1/chat/completions",
			wantErr:  false,
		},
		{
			name:        "openai_compatible without endpoint (invalid)",
			provider:    "openai_compatible",
			endpoint:    "",
			wantErr:     true,
			errContains: "required when provider is openai_compatible",
		},
		{
			name:        "invalid provider value",
			provider:    "invalid_provider",
			endpoint:    "",
			wantErr:     true,
			errContains: "must be one of: openai, openai_compatible, anthropic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Validation.AI.Provider = tt.provider
			cfg.Validation.AI.Endpoint = tt.endpoint

			err := ValidateConfig(cfg)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
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
	configPath := tmpDir + "/maybe-dont.yaml"

	// Create a minimal config file without API key
	configContent := `
server:
  type: stdio

downstream_mcp_servers:
  test:
    type: stdio
    command: echo

# Request validation with CEL disabled, AI enabled
request_validation:
  cel:
    enabled: false
  ai:
    enabled: true
    rules_file: ai_request_rules.yaml

validation:
  ai:
    endpoint: https://api.openai.com/v1
    model: gpt-4
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Create a rules file
	rulesContent := `
rules:
  - name: test-rule
    description: Test rule
    prompt: Test prompt
    message: Test message
`
	err = os.WriteFile(tmpDir+"/ai_request_rules.yaml", []byte(rulesContent), 0o644)
	require.NoError(t, err)

	// Set the API key via environment variable
	err = os.Setenv("MAYBE_DONT_VALIDATION_AI_API_KEY", "test-api-key-from-env")
	require.NoError(t, err)
	defer func() {
		_ = os.Unsetenv("MAYBE_DONT_VALIDATION_AI_API_KEY")
	}()

	// Load config - need to reset viper to avoid state from previous tests
	// Note: This test modifies global viper state, so it may interfere with other tests
	// if run in parallel. For proper isolation, viper would need to be injected.

	config, err := LoadConfig(tmpDir, "")
	require.NoError(t, err)

	// Verify the API key was loaded from the environment variable
	require.Equal(t, "test-api-key-from-env", config.Validation.AI.APIKey,
		"API key should be loaded from MAYBE_DONT_VALIDATION_AI_API_KEY environment variable")
}

// TestApplyEnvironmentOverrides_MapStringAny verifies that map[string]any fields
// (like validation.ai.parameters) can be populated via environment variables.
// e.g., MAYBE_DONT_VALIDATION_AI_PARAMETERS_TEMPERATURE=0.5
func TestApplyEnvironmentOverrides_MapStringAny(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		envValue string
		expKey   string
		expValue any
	}{
		{
			name:     "float value",
			envVar:   "MAYBE_DONT_VALIDATION_AI_PARAMETERS_TEMPERATURE",
			envValue: "0.5",
			expKey:   "temperature",
			expValue: 0.5,
		},
		{
			name:     "zero is float64 not int64",
			envVar:   "MAYBE_DONT_VALIDATION_AI_PARAMETERS_TEMPERATURE",
			envValue: "0",
			expKey:   "temperature",
			expValue: float64(0),
		},
		{
			name:     "integer value parsed as float64",
			envVar:   "MAYBE_DONT_VALIDATION_AI_PARAMETERS_MAX_TOKENS",
			envValue: "4096",
			expKey:   "max_tokens",
			expValue: float64(4096),
		},
		{
			name:     "boolean value",
			envVar:   "MAYBE_DONT_VALIDATION_AI_PARAMETERS_STREAM",
			envValue: "true",
			expKey:   "stream",
			expValue: true,
		},
		{
			name:     "string value",
			envVar:   "MAYBE_DONT_VALIDATION_AI_PARAMETERS_STOP",
			envValue: "END",
			expKey:   "stop",
			expValue: "END",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, tt.envValue)

			config := &Config{}
			applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

			require.NotNil(t, config.Validation.AI.Parameters, "Parameters map should be initialized")
			val, ok := config.Validation.AI.Parameters[tt.expKey]
			require.True(t, ok, "Key %q should exist in parameters", tt.expKey)
			require.Equal(t, tt.expValue, val, "Value for key %q should match", tt.expKey)
		})
	}
}

// TestApplyEnvironmentOverrides_MapOverridesYAMLDefault verifies that an env var
// overrides a parameter value set in the YAML config (e.g., default temperature).
func TestApplyEnvironmentOverrides_MapOverridesYAMLDefault(t *testing.T) {
	t.Setenv("MAYBE_DONT_VALIDATION_AI_PARAMETERS_TEMPERATURE", "0.7")

	// Start with a config that has a default temperature (as shipped in defaults)
	config := &Config{}
	config.Validation.AI.Parameters = map[string]any{
		"temperature": 0.0,
	}

	applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

	require.Equal(t, 0.7, config.Validation.AI.Parameters["temperature"],
		"Env var should override YAML default temperature")
}

// TestApplyEnvironmentOverrides_AllConfigFields uses reflection to discover all settable
// fields in the Config struct and verifies that each can be set via environment variables.
// This ensures that any new fields added to Config are automatically tested.
func TestApplyEnvironmentOverrides_AllConfigFields(t *testing.T) {
	// Collect all field paths and their types from the Config struct
	fields := collectConfigFields(reflect.TypeOf(Config{}), "")

	for _, field := range fields {
		t.Run(field.envVar, func(t *testing.T) {
			// Set the environment variable
			err := os.Setenv(field.envVar, field.testValue)
			require.NoError(t, err)
			defer func() {
				_ = os.Unsetenv(field.envVar)
			}()

			// Create a fresh config and apply overrides
			config := &Config{}
			applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

			// Get the actual value using reflection
			actualValue := getFieldValue(reflect.ValueOf(config).Elem(), field.path)
			require.NotNil(t, actualValue, "Could not get value for path: %s", field.path)

			// Compare based on type - use reflect to handle type aliases like ServerType
			actualVal := reflect.ValueOf(actualValue)
			switch field.kind {
			case reflect.String:
				// Handle string and string-based type aliases (like ServerType, PolicyMode)
				require.Equal(t, field.testValue, actualVal.String(),
					"String field %s was not set correctly from %s", field.path, field.envVar)

			case reflect.Bool:
				expected, _ := strconv.ParseBool(field.testValue)
				require.Equal(t, expected, actualVal.Bool(),
					"Bool field %s was not set correctly from %s", field.path, field.envVar)

			case reflect.Int:
				expected, _ := strconv.Atoi(field.testValue)
				require.Equal(t, int64(expected), actualVal.Int(),
					"Int field %s was not set correctly from %s", field.path, field.envVar)

			case reflect.Slice:
				// For []string slices
				expectedParts := strings.Split(field.testValue, ",")
				var expected []string
				for _, p := range expectedParts {
					if trimmed := strings.TrimSpace(p); trimmed != "" {
						expected = append(expected, trimmed)
					}
				}
				// Convert reflect.Value to []string
				actualSlice := make([]string, actualVal.Len())
				for i := 0; i < actualVal.Len(); i++ {
					actualSlice[i] = actualVal.Index(i).String()
				}
				require.Equal(t, expected, actualSlice,
					"Slice field %s was not set correctly from %s", field.path, field.envVar)

			case reflect.Ptr:
				// *bool field — value should be a non-nil *bool after env override
				expected, _ := strconv.ParseBool(field.testValue)
				require.False(t, actualVal.IsNil(),
					"Ptr field %s should not be nil after env override from %s", field.path, field.envVar)
				require.Equal(t, expected, actualVal.Elem().Bool(),
					"Ptr field %s was not set correctly from %s", field.path, field.envVar)
			}
		})
	}
}

// configFieldInfo holds metadata about a config field for testing
type configFieldInfo struct {
	path      string       // dot-separated path like "server.listen_addr"
	envVar    string       // environment variable name like "MAYBE_DONT_SERVER_LISTEN_ADDR"
	kind      reflect.Kind // the field's kind (string, bool, int, etc.)
	testValue string       // a valid test value for this field type
}

// collectConfigFields recursively discovers all settable fields in a struct type.
// It skips maps, slices of structs, and non-bool pointer types.
func collectConfigFields(t reflect.Type, pathPrefix string) []configFieldInfo {
	var fields []configFieldInfo

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return fields
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get the mapstructure tag
		tag := field.Tag.Get("mapstructure")
		if tag == "" {
			continue
		}

		// Build the full path
		var fullPath string
		if pathPrefix == "" {
			fullPath = tag
		} else {
			fullPath = pathPrefix + "." + tag
		}

		// Build the environment variable name
		envVar := "MAYBE_DONT_" + strings.ToUpper(strings.ReplaceAll(fullPath, ".", "_"))

		fieldType := field.Type
		kind := fieldType.Kind()

		switch kind {
		case reflect.String:
			testValue := "test-value-" + tag
			// Use a valid policy mode for PolicyMode fields to avoid validation errors
			if fieldType.Name() == "PolicyMode" {
				testValue = string(PolicyModeEnforce)
			}
			fields = append(fields, configFieldInfo{
				path:      fullPath,
				envVar:    envVar,
				kind:      kind,
				testValue: testValue,
			})

		case reflect.Bool:
			fields = append(fields, configFieldInfo{
				path:      fullPath,
				envVar:    envVar,
				kind:      kind,
				testValue: "true",
			})

		case reflect.Int, reflect.Int64:
			fields = append(fields, configFieldInfo{
				path:      fullPath,
				envVar:    envVar,
				kind:      reflect.Int, // treat int64 as int for testing purposes
				testValue: "42",
			})

		case reflect.Float64:
			fields = append(fields, configFieldInfo{
				path:      fullPath,
				envVar:    envVar,
				kind:      kind,
				testValue: "3.14",
			})

		case reflect.Slice:
			// Only handle []string slices
			if fieldType.Elem().Kind() == reflect.String {
				fields = append(fields, configFieldInfo{
					path:      fullPath,
					envVar:    envVar,
					kind:      kind,
					testValue: "value1,value2,value3",
				})
			}
			// Skip other slice types (e.g., []CELPolicy)

		case reflect.Struct:
			// Recursively collect fields from nested structs
			nestedFields := collectConfigFields(fieldType, fullPath)
			fields = append(fields, nestedFields...)

		case reflect.Map:
			// Skip maps (like DownstreamMCPServers) - they can't be set via simple env vars

		case reflect.Ptr:
			// Handle pointer-to-bool fields (e.g., *bool for optional config like IncludeArgumentValues)
			if fieldType.Elem().Kind() == reflect.Bool {
				fields = append(fields, configFieldInfo{
					path:      fullPath,
					envVar:    envVar,
					kind:      reflect.Ptr,
					testValue: "true",
				})
			}
		}
	}

	return fields
}

// getFieldValue navigates to a field using a dot-separated path and returns its value
func getFieldValue(v reflect.Value, path string) interface{} {
	parts := strings.Split(path, ".")

	current := v
	for _, part := range parts {
		if current.Kind() == reflect.Ptr {
			if current.IsNil() {
				return nil
			}
			current = current.Elem()
		}

		if current.Kind() != reflect.Struct {
			return nil
		}

		// Find field by mapstructure tag
		found := false
		for i := 0; i < current.NumField(); i++ {
			field := current.Type().Field(i)
			tag := field.Tag.Get("mapstructure")
			if tag == part {
				current = current.Field(i)
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	return current.Interface()
}

func TestApplyEnvironmentOverrides_InvalidValues(t *testing.T) {
	// Test that invalid values don't crash and leave defaults unchanged

	t.Run("invalid bool leaves default", func(t *testing.T) {
		err := os.Setenv("MAYBE_DONT_LOGGER_ROTATION_COMPRESS", "not-a-bool")
		require.NoError(t, err)
		defer func() {
			_ = os.Unsetenv("MAYBE_DONT_LOGGER_ROTATION_COMPRESS")
		}()

		config := &Config{}
		config.Logger.Rotation.Compress = true // set a default

		applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

		// Should remain unchanged because "not-a-bool" can't be parsed
		require.True(t, config.Logger.Rotation.Compress, "Invalid bool should leave default unchanged")
	})

	t.Run("invalid int leaves default", func(t *testing.T) {
		err := os.Setenv("MAYBE_DONT_NATIVE_TOOLS_AUDIT_LOG_MAX_ENTRIES", "not-a-number")
		require.NoError(t, err)
		defer func() {
			_ = os.Unsetenv("MAYBE_DONT_NATIVE_TOOLS_AUDIT_LOG_MAX_ENTRIES")
		}()

		config := &Config{}
		config.NativeTools.AuditLog.MaxEntries = 100 // set a default

		applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

		require.Equal(t, 100, config.NativeTools.AuditLog.MaxEntries,
			"Invalid int should leave default unchanged")
	})

	t.Run("invalid *bool leaves default nil", func(t *testing.T) {
		t.Setenv("MAYBE_DONT_AUDIT_INCLUDE_ARGUMENT_VALUES", "not-a-bool")

		config := &Config{}

		applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

		// Should remain nil because "not-a-bool" can't be parsed
		require.Nil(t, config.Audit.IncludeArgumentValues,
			"Invalid *bool should leave pointer nil")
	})
}

func TestApplyEnvironmentOverrides_StringSliceEdgeCases(t *testing.T) {
	// Test edge cases for []string parsing that aren't covered by the main test
	tests := []struct {
		name     string
		envVar   string
		envValue string
		expected []string
	}{
		{
			name:     "with spaces between values",
			envVar:   "MAYBE_DONT_SERVER_TRUSTED_PROXIES",
			envValue: "10.0.0.1, 10.0.0.2 , 10.0.0.3",
			expected: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		},
		{
			name:     "empty entries filtered",
			envVar:   "MAYBE_DONT_SERVER_TRUSTED_PROXIES",
			envValue: "10.0.0.1,,10.0.0.2",
			expected: []string{"10.0.0.1", "10.0.0.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := os.Setenv(tt.envVar, tt.envValue)
			require.NoError(t, err)
			defer func() {
				_ = os.Unsetenv(tt.envVar)
			}()

			config := &Config{}
			applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

			require.Equal(t, tt.expected, config.Server.TrustedProxies)
		})
	}
}

func TestApplyEnvironmentOverrides_DoesNotOverrideWhenNotSet(t *testing.T) {
	// Ensure that when env var is not set, existing values are preserved

	config := &Config{}
	config.Validation.AI.APIKey = "original-key"
	config.Logger.Rotation.Compress = true
	config.NativeTools.AuditLog.MaxEntries = 100
	config.Server.TrustedProxies = []string{"original"}

	// Don't set any environment variables
	applyEnvironmentOverrides(reflect.ValueOf(config).Elem(), reflect.TypeOf(*config), "", "MAYBE_DONT")

	require.Equal(t, "original-key", config.Validation.AI.APIKey)
	require.True(t, config.Logger.Rotation.Compress)
	require.Equal(t, 100, config.NativeTools.AuditLog.MaxEntries)
	require.Equal(t, []string{"original"}, config.Server.TrustedProxies)
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
		"native_tools.audit_report.timeout_seconds",
		"logger.path",
		"logger.level",
		"logger.rotation.max_size_mb",
		"logger.rotation.max_backups",
		"logger.rotation.max_age_days",
		"logger.rotation.compress",
		"audit.path",
		"audit.filter",
		"audit.rotation.max_size_mb",
		"audit.rotation.max_backups",
		"audit.rotation.max_age_days",
		"audit.rotation.compress",
		"validation.max_blocking_ms",
		"validation.max_rule_evaluation_ms",
		"server.session_timeout_minutes",
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
			// When audit_report is enabled, AI credentials are required
			if tt.enabled {
				config.Validation.AI.APIKey = "test-key"
				config.Validation.AI.Endpoint = "https://api.example.com"
				config.Validation.AI.Model = "test-model"
			}

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
		"file/", // trailing slash is allowed
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

func TestValidateConfig_LoggerPathWithSubdirectory(t *testing.T) {
	// Test that subdirectories are now allowed in logger.path
	config := createValidBaseConfig()
	config.Logger.Path = "logs/subdir/app.log"

	err := ValidateConfig(config)
	require.NoError(t, err, "Subdirectory paths should be allowed for logger.path")
}

func TestValidateConfig_AuditPathWithSubdirectory(t *testing.T) {
	// Test that subdirectories are now allowed in audit.path
	config := createValidBaseConfig()
	config.Audit.Path = "audit/2024/01/audit.log"

	err := ValidateConfig(config)
	require.NoError(t, err, "Subdirectory paths should be allowed for audit.path")
}

func TestValidateConfig_LoggerPathTraversalRejected(t *testing.T) {
	// Test that path traversal is rejected in logger.path
	config := createValidBaseConfig()
	config.Logger.Path = "../../../etc/passwd"

	err := ValidateConfig(config)
	require.Error(t, err, "Path traversal should be rejected in logger.path")
	require.Contains(t, err.Error(), "logger.path")
}

func TestValidateConfig_AuditPathTraversalRejected(t *testing.T) {
	// Test that path traversal is rejected in audit.path
	config := createValidBaseConfig()
	config.Audit.Path = "logs/../../../etc/passwd"

	err := ValidateConfig(config)
	require.Error(t, err, "Path traversal should be rejected in audit.path")
	require.Contains(t, err.Error(), "audit.path")
}

func TestLoadConfig_RulesFilePathTraversalRejected(t *testing.T) {
	// Test that path traversal is rejected in rules_file paths during LoadConfig
	viper.Reset()
	tmpDir := t.TempDir()

	// Create a config file with path traversal in request_validation.rules_file
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: true
    rules_file: "../../../etc/passwd"
  ai:
    enabled: false
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	_, err = LoadConfig(tmpDir, "")
	require.Error(t, err, "Path traversal should be rejected in request_validation.cel.rules_file")
	require.Contains(t, err.Error(), "request_validation.cel.rules_file")
	require.Contains(t, err.Error(), "parent directory")
}

func TestLoadConfig_AIRequestRulesFilePathTraversalRejected(t *testing.T) {
	// Test that path traversal is rejected in request_validation.ai.rules_file
	viper.Reset()
	tmpDir := t.TempDir()

	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

validation:
  ai:
    endpoint: https://api.example.com/v1
    model: test-model
    api_key: test-key

request_validation:
  cel:
    enabled: false
  ai:
    enabled: true
    rules_file: "../../secrets/rules.yaml"
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	_, err = LoadConfig(tmpDir, "")
	require.Error(t, err, "Path traversal should be rejected in request_validation.ai.rules_file")
	require.Contains(t, err.Error(), "request_validation.ai.rules_file")
	require.Contains(t, err.Error(), "parent directory")
}

func TestLoadConfig_ResponseRulesFilePathTraversalRejected(t *testing.T) {
	// Test that path traversal is rejected in response_validation.cel.rules_file
	viper.Reset()
	tmpDir := t.TempDir()

	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  cel:
    enabled: true
    rules_file: "../secret/response_rules.yaml"
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	_, err = LoadConfig(tmpDir, "")
	require.Error(t, err, "Path traversal should be rejected in response_validation.cel.rules_file")
	require.Contains(t, err.Error(), "response_validation.cel.rules_file")
	require.Contains(t, err.Error(), "parent directory")
}

func TestLoadConfig_AIResponseRulesFilePathTraversalRejected(t *testing.T) {
	// Test that path traversal is rejected in response_validation.ai.rules_file
	viper.Reset()
	tmpDir := t.TempDir()

	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

validation:
  ai:
    endpoint: https://api.example.com/v1
    model: test-model
    api_key: test-key

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  ai:
    enabled: true
    rules_file: "/etc/passwd"
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	_, err = LoadConfig(tmpDir, "")
	require.Error(t, err, "Absolute path should be rejected in response_validation.ai.rules_file")
	require.Contains(t, err.Error(), "response_validation.ai.rules_file")
	require.Contains(t, err.Error(), "absolute path")
}

func TestLoadConfig_RulesFileSubdirectoryAllowed(t *testing.T) {
	// Test that subdirectory paths are allowed for rules_file
	viper.Reset()
	tmpDir := t.TempDir()

	// Create subdirectory and rules file
	err := os.MkdirAll(tmpDir+"/rules/custom", 0o755)
	require.NoError(t, err)

	rulesContent := `
rules:
  - name: test-rule
    description: Test rule
    match:
      tools: ["*"]
    action: allow
`
	err = os.WriteFile(tmpDir+"/rules/custom/my-rules.yaml", []byte(rulesContent), 0o644)
	require.NoError(t, err)

	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: true
    rules_file: "rules/custom/my-rules.yaml"
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false
`
	err = os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, err := LoadConfig(tmpDir, "")
	require.NoError(t, err, "Subdirectory paths should be allowed for rules_file")
	require.Len(t, cfg.RequestValidation.CEL.Rules, 1)
	require.Equal(t, "test-rule", cfg.RequestValidation.CEL.Rules[0].Name)
}

func TestLoadConfig_RulesFileHiddenFileRejected(t *testing.T) {
	// Test that hidden files are rejected in rules_file paths
	viper.Reset()
	tmpDir := t.TempDir()

	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: true
    rules_file: ".hidden_rules.yaml"
  ai:
    enabled: false
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	_, err = LoadConfig(tmpDir, "")
	require.Error(t, err, "Hidden files should be rejected in rules_file")
	require.Contains(t, err.Error(), "request_validation.cel.rules_file")
	require.Contains(t, err.Error(), "hidden")
}

func TestLoadConfig_RulesFileURLEncodedTraversalRejected(t *testing.T) {
	// Test that URL-encoded path traversal is rejected
	viper.Reset()
	tmpDir := t.TempDir()

	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: true
    rules_file: "%2e%2e/rules.yaml"
  ai:
    enabled: false
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	_, err = LoadConfig(tmpDir, "")
	require.Error(t, err, "URL-encoded path traversal should be rejected")
	require.Contains(t, err.Error(), "request_validation.cel.rules_file")
}

func TestLoadConfigWithoutConfigFile(t *testing.T) {
	// Reset viper to avoid state from previous tests
	viper.Reset()

	// Create a temporary empty directory (no config file)
	tmpDir := t.TempDir()

	// Set required environment variables to configure downstream MCP server
	// Also disable policy_validation which defaults to enabled
	envVars := map[string]string{
		"MAYBE_DONT_SERVER_TYPE":            "stdio",
		"MAYBE_DONT_POLICY_VALIDATION_MODE": "disabled",
		// Note: downstream_mcp_servers cannot be set via env vars directly
		// because they are a map type, but we can test the error message
	}

	for k, v := range envVars {
		err := os.Setenv(k, v)
		require.NoError(t, err)
	}
	defer func() {
		for k := range envVars {
			_ = os.Unsetenv(k)
		}
	}()

	// Load config from empty directory - should fail validation but not panic
	_, err := LoadConfig(tmpDir, "")
	require.Error(t, err)

	// Error message should contain guidance about environment variables
	require.Contains(t, err.Error(), "No configuration file was found")
	require.Contains(t, err.Error(), "MAYBE_DONT_")
	require.Contains(t, err.Error(), "environment variables")
}

func TestValidateConfigWithContext_NoConfigFileShowsGuidance(t *testing.T) {
	// Test that when config file is not found and validation fails,
	// we get helpful guidance about using environment variables
	config := &Config{
		Server: ServerConfig{
			Type: ServerType("invalid"), // Invalid server type triggers a validation error
		},
		Audit: AuditConfig{
			Path: "audit.log",
		},
		RequestValidation: RequestValidationConfig{
			CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIRequestValidationConfig{Mode: PolicyModeAuditOnly},
		},
		ResponseValidation: ResponseValidationConfig{
			CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
		},
	}

	// Call with configFileFound=false
	err := ValidateConfigWithContext(config, false)
	require.Error(t, err)

	// Should contain guidance about environment variables
	errMsg := err.Error()
	require.Contains(t, errMsg, "No configuration file was found")
	require.Contains(t, errMsg, "MAYBE_DONT_")
	require.Contains(t, errMsg, "maybe-dont.yaml")
}

func TestValidateConfigWithContext_WithConfigFileNoGuidance(t *testing.T) {
	// Test that when config file IS found and validation fails,
	// we do NOT show guidance about environment variables
	config := &Config{
		Server: ServerConfig{
			Type: ServerType("invalid"), // Invalid server type triggers a validation error
		},
		Audit: AuditConfig{
			Path: "audit.log",
		},
		RequestValidation: RequestValidationConfig{
			CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIRequestValidationConfig{Mode: PolicyModeAuditOnly},
		},
		ResponseValidation: ResponseValidationConfig{
			CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
		},
	}

	// Call with configFileFound=true
	err := ValidateConfigWithContext(config, true)
	require.Error(t, err)

	// Should NOT contain guidance about environment variables
	errMsg := err.Error()
	require.NotContains(t, errMsg, "No configuration file was found")
	require.NotContains(t, errMsg, "maybe-dont.yaml")
}

// TestValidateConfig_DownstreamServersOptional verifies that downstream MCP servers
// are optional — the gateway can run with only native tools and CLI validation.
func TestValidateConfig_DownstreamServersOptional(t *testing.T) {
	tests := []struct {
		name    string
		servers map[string]ClientConfig
	}{
		{"nil map", nil},
		{"empty map", map[string]ClientConfig{}},
		{"one valid server", map[string]ClientConfig{
			"test": {Type: "stdio", Command: "echo"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Type: ServerTypeSTDIO,
				},
				DownstreamMCPServers: tt.servers,
				Audit: AuditConfig{
					Path: "audit.log",
				},
				RequestValidation: RequestValidationConfig{
					CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
					AI:  AIRequestValidationConfig{Mode: PolicyModeAuditOnly},
				},
				ResponseValidation: ResponseValidationConfig{
					CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
					AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
				},
			}

			err := ValidateConfig(cfg)
			require.NoError(t, err)
		})
	}
}

func TestLoadConfigWithEnvVarsOnly_ValidConfig(t *testing.T) {
	// Reset viper to avoid state from previous tests
	viper.Reset()

	// Create a temporary directory with a minimal config file
	// that only has the downstream MCP servers (since they can't be set via env vars)
	tmpDir := t.TempDir()
	configPath := tmpDir + "/maybe-dont.yaml"

	// Minimal config with just downstream MCP servers
	// Notes:
	// - request_validation.cel defaults to enabled, so we must disable it since we have no rules file
	// - request_validation.ai defaults to audit_only, so we must disable it since we have no API key
	// - audit_report defaults to enabled, so we must disable it since we have no AI credentials
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Set server type via environment variable to prove it works
	err = os.Setenv("MAYBE_DONT_SERVER_TYPE", "stdio")
	require.NoError(t, err)
	defer func() {
		_ = os.Unsetenv("MAYBE_DONT_SERVER_TYPE")
	}()

	// Load config
	config, err := LoadConfig(tmpDir, "")
	require.NoError(t, err)

	// Verify server type was set (either from env var or default)
	require.Equal(t, ServerTypeSTDIO, config.Server.Type)
}

// createValidBaseConfig creates a Config with all required fields set to valid values.
// Use this as a starting point when testing specific validation rules.
func createValidBaseConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Type: ServerTypeSTDIO,
		},
		DownstreamMCPServers: map[string]ClientConfig{
			"test": {
				Type:    "stdio",
				Command: "echo",
			},
		},
		Audit: AuditConfig{
			Path: "audit.log",
		},
		RequestValidation: RequestValidationConfig{
			CEL: CELRequestValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIRequestValidationConfig{Mode: PolicyModeAuditOnly},
		},
		ResponseValidation: ResponseValidationConfig{
			CEL: CELResponseValidationConfig{Mode: PolicyModeAuditOnly},
			AI:  AIResponseValidationConfig{Mode: PolicyModeAuditOnly},
		},
		NativeTools: struct {
			AuditLog struct {
				Enabled    bool `mapstructure:"enabled"`
				MaxEntries int  `mapstructure:"max_entries"`
			} `mapstructure:"audit_log"`
			AuditReport struct {
				Enabled        bool   `mapstructure:"enabled"`
				MaxEntries     int    `mapstructure:"max_entries"`
				TimeoutSeconds int    `mapstructure:"timeout_seconds"`
				SystemPrompt   string `mapstructure:"system_prompt"`
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
				Enabled        bool   `mapstructure:"enabled"`
				MaxEntries     int    `mapstructure:"max_entries"`
				TimeoutSeconds int    `mapstructure:"timeout_seconds"`
				SystemPrompt   string `mapstructure:"system_prompt"`
			}{
				Enabled:        false,
				MaxEntries:     1000,
				TimeoutSeconds: 180,
			},
		},
	}
}

// TestParseCompactHeaders tests the compact header format parsing
func TestParseCompactHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []CredentialMapping
		wantErr  bool
		errMsg   string
	}{
		{
			name:  "single header with source and target only",
			input: "X-GitHub-Token:Authorization",
			expected: []CredentialMapping{
				{SourceHeader: "X-GitHub-Token", TargetHeader: "Authorization"},
			},
		},
		{
			name:  "single header with format",
			input: "X-GitHub-Token:Authorization:Bearer {value}",
			expected: []CredentialMapping{
				{SourceHeader: "X-GitHub-Token", TargetHeader: "Authorization", Format: "Bearer {value}"},
			},
		},
		{
			name:  "multiple headers with semicolon separator",
			input: "X-Token:Authorization:Bearer {value};X-Tenant:X-Downstream-Tenant",
			expected: []CredentialMapping{
				{SourceHeader: "X-Token", TargetHeader: "Authorization", Format: "Bearer {value}"},
				{SourceHeader: "X-Tenant", TargetHeader: "X-Downstream-Tenant"},
			},
		},
		{
			name:  "format containing colon",
			input: "X-Token:Authorization:Prefix: {value}",
			expected: []CredentialMapping{
				{SourceHeader: "X-Token", TargetHeader: "Authorization", Format: "Prefix: {value}"},
			},
		},
		{
			name:    "missing colon - error",
			input:   "X-GitHub-Token",
			wantErr: true,
			errMsg:  "must contain at least one colon",
		},
		{
			name:    "empty source header - error",
			input:   ":Authorization",
			wantErr: true,
			errMsg:  "source_header cannot be empty",
		},
		{
			name:    "empty target header - error",
			input:   "X-Token:",
			wantErr: true,
			errMsg:  "target_header cannot be empty",
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:  "whitespace handling",
			input: " X-Token : Authorization : Bearer {value} ",
			expected: []CredentialMapping{
				{SourceHeader: "X-Token", TargetHeader: "Authorization", Format: "Bearer {value}"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCompactHeaders(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractClientNameAndPath tests client name and field path extraction
func TestExtractClientNameAndPath(t *testing.T) {
	tests := []struct {
		name           string
		suffix         string
		expectedClient string
		expectedPath   string
		expectedOk     bool
	}{
		{
			name:           "simple client with TYPE",
			suffix:         "GITHUB_TYPE",
			expectedClient: "github",
			expectedPath:   "TYPE",
			expectedOk:     true,
		},
		{
			name:           "multi-word client name",
			suffix:         "AWS_DOCS_TYPE",
			expectedClient: "aws-docs",
			expectedPath:   "TYPE",
			expectedOk:     true,
		},
		{
			name:           "client with URL field",
			suffix:         "GITHUB_URL",
			expectedClient: "github",
			expectedPath:   "URL",
			expectedOk:     true,
		},
		{
			name:           "nested auth config",
			suffix:         "GITHUB_AUTH_PASS_THROUGH_ENABLED",
			expectedClient: "github",
			expectedPath:   "AUTH_PASS_THROUGH_ENABLED",
			expectedOk:     true,
		},
		{
			name:           "indexed headers",
			suffix:         "GITHUB_AUTH_PASS_THROUGH_HEADERS_0_SOURCE_HEADER",
			expectedClient: "github",
			expectedPath:   "AUTH_PASS_THROUGH_HEADERS_0_SOURCE_HEADER",
			expectedOk:     true,
		},
		{
			name:           "compact headers",
			suffix:         "MY_SERVER_AUTH_PASS_THROUGH_HEADERS",
			expectedClient: "my-server",
			expectedPath:   "AUTH_PASS_THROUGH_HEADERS",
			expectedOk:     true,
		},
		{
			name:           "http headers",
			suffix:         "GITHUB_HTTP_HEADERS_AUTHORIZATION",
			expectedClient: "github",
			expectedPath:   "HTTP_HEADERS_AUTHORIZATION",
			expectedOk:     true,
		},
		{
			name:       "unrecognized field path",
			suffix:     "GITHUB_UNKNOWN_FIELD",
			expectedOk: false,
		},
		{
			name:       "empty suffix",
			suffix:     "",
			expectedOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientName, fieldPath, ok := extractClientNameAndPath(tt.suffix)
			require.Equal(t, tt.expectedOk, ok, "ok mismatch")
			if tt.expectedOk {
				require.Equal(t, tt.expectedClient, clientName, "client name mismatch")
				require.Equal(t, tt.expectedPath, fieldPath, "field path mismatch")
			}
		})
	}
}

// TestParseDownstreamServersFromEnv tests parsing downstream servers from environment variables
func TestParseDownstreamServersFromEnv(t *testing.T) {
	// Helper to set and clean up env vars
	setEnvVars := func(vars map[string]string) func() {
		for k, v := range vars {
			_ = os.Setenv(k, v)
		}
		return func() {
			for k := range vars {
				_ = os.Unsetenv(k)
			}
		}
	}

	t.Run("basic client configuration", func(t *testing.T) {
		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE": "http",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL":  "https://api.github.com/mcp/",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(nil, "MAYBE_DONT")
		require.Len(t, servers, 1)
		require.Equal(t, "http", servers["github"].Type)
		require.Equal(t, "https://api.github.com/mcp/", servers["github"].URL)
	})

	t.Run("multi-word client name with underscore to hyphen conversion", func(t *testing.T) {
		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_AWS_DOCS_TYPE": "http",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_AWS_DOCS_URL":  "https://aws.example.com/",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(nil, "MAYBE_DONT")
		require.Len(t, servers, 1)
		require.Equal(t, "http", servers["aws-docs"].Type)
		require.Equal(t, "https://aws.example.com/", servers["aws-docs"].URL)
	})

	t.Run("pass-through auth with compact headers", func(t *testing.T) {
		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE":                      "http",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL":                       "https://api.github.com/",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_ENABLED": "true",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS": "X-Token:Authorization:Bearer {value}",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(nil, "MAYBE_DONT")
		require.Len(t, servers, 1)
		require.True(t, servers["github"].Auth.PassThrough.Enabled)
		require.Len(t, servers["github"].Auth.PassThrough.Headers, 1)
		require.Equal(t, "X-Token", servers["github"].Auth.PassThrough.Headers[0].SourceHeader)
		require.Equal(t, "Authorization", servers["github"].Auth.PassThrough.Headers[0].TargetHeader)
		require.Equal(t, "Bearer {value}", servers["github"].Auth.PassThrough.Headers[0].Format)
	})

	t.Run("pass-through auth with indexed headers", func(t *testing.T) {
		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE":                                      "http",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL":                                       "https://api.github.com/",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_ENABLED":                 "true",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_SOURCE_HEADER": "X-Token",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_TARGET_HEADER": "Authorization",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_FORMAT":        "Bearer {value}",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_1_SOURCE_HEADER": "X-Tenant",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_1_TARGET_HEADER": "X-Downstream-Tenant",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(nil, "MAYBE_DONT")
		require.Len(t, servers, 1)
		require.True(t, servers["github"].Auth.PassThrough.Enabled)
		require.Len(t, servers["github"].Auth.PassThrough.Headers, 2)
		require.Equal(t, "X-Token", servers["github"].Auth.PassThrough.Headers[0].SourceHeader)
		require.Equal(t, "Authorization", servers["github"].Auth.PassThrough.Headers[0].TargetHeader)
		require.Equal(t, "Bearer {value}", servers["github"].Auth.PassThrough.Headers[0].Format)
		require.Equal(t, "X-Tenant", servers["github"].Auth.PassThrough.Headers[1].SourceHeader)
		require.Equal(t, "X-Downstream-Tenant", servers["github"].Auth.PassThrough.Headers[1].TargetHeader)
	})

	t.Run("stdio client with command and args", func(t *testing.T) {
		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_TYPE":    "stdio",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_COMMAND": "/usr/local/bin/mcp-server",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_ARGS":    "--verbose,--port=8080",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(nil, "MAYBE_DONT")
		require.Len(t, servers, 1)
		require.Equal(t, "stdio", servers["local"].Type)
		require.Equal(t, "/usr/local/bin/mcp-server", servers["local"].Command)
		require.Equal(t, []string{"--verbose", "--port=8080"}, servers["local"].Args)
	})

	t.Run("multiple clients", func(t *testing.T) {
		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE":   "http",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL":    "https://api.github.com/",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_TYPE":    "stdio",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_COMMAND": "/usr/bin/mcp",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(nil, "MAYBE_DONT")
		require.Len(t, servers, 2)
		require.Equal(t, "http", servers["github"].Type)
		require.Equal(t, "stdio", servers["local"].Type)
	})

	t.Run("env vars override existing YAML config", func(t *testing.T) {
		existing := map[string]ClientConfig{
			"github": {
				Type: "http",
				URL:  "https://old-url.com/",
			},
		}

		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL": "https://new-url.com/",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(existing, "MAYBE_DONT")
		require.Len(t, servers, 1)
		require.Equal(t, "http", servers["github"].Type)                // Unchanged
		require.Equal(t, "https://new-url.com/", servers["github"].URL) // Overridden
	})

	t.Run("http headers configuration", func(t *testing.T) {
		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_API_TYPE":                     "http",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_API_URL":                      "https://api.example.com/",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_API_HTTP_HEADERS_X_API_KEY":   "secret123",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_API_HTTP_HEADERS_X_CLIENT_ID": "my-client",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(nil, "MAYBE_DONT")
		require.Len(t, servers, 1)
		require.Equal(t, "secret123", servers["api"].HTTPConfig.Headers["X_API_KEY"])
		require.Equal(t, "my-client", servers["api"].HTTPConfig.Headers["X_CLIENT_ID"])
	})

	t.Run("integer fields", func(t *testing.T) {
		cleanup := setEnvVars(map[string]string{
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_TEST_TYPE":                   "stdio",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_TEST_COMMAND":                "echo",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_TEST_STARTUP_TIMEOUT_MS":     "5000",
			"MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_TEST_INITIALIZATION_RETRIES": "3",
		})
		defer cleanup()

		servers := parseDownstreamServersFromEnv(nil, "MAYBE_DONT")
		require.Len(t, servers, 1)
		require.Equal(t, 5000, servers["test"].StartupTimeoutMs)
		require.Equal(t, 3, servers["test"].InitializationRetries)
	})
}

// TestConfigPathToEnvVar tests the YAML path to env var conversion
func TestConfigPathToEnvVar(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "server.type",
			expected: "MAYBE_DONT_SERVER_TYPE",
		},
		{
			name:     "path with brackets",
			path:     "downstream_mcp_servers[github].url",
			expected: "MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL",
		},
		{
			name:     "path with hyphen in client name",
			path:     "downstream_mcp_servers[aws-docs].type",
			expected: "MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_AWS_DOCS_TYPE",
		},
		{
			name:     "nested path with index",
			path:     "downstream_mcp_servers[github].auth.pass_through.headers[0].source_header",
			expected: "MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_SOURCE_HEADER",
		},
		{
			name:     "validation ai config",
			path:     "validation.ai.api_key",
			expected: "MAYBE_DONT_VALIDATION_AI_API_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConfigPathToEnvVar(tt.path)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveConfigDir_XDGSupport tests XDG Base Directory support for config resolution
func TestResolveConfigDir_XDGSupport(t *testing.T) {
	// Save original env vars to restore after test
	origXDGConfig := os.Getenv("XDG_CONFIG_HOME")
	origHome := os.Getenv("HOME")
	defer func() {
		if origXDGConfig != "" {
			_ = os.Setenv("XDG_CONFIG_HOME", origXDGConfig)
		} else {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		}
		if origHome != "" {
			_ = os.Setenv("HOME", origHome)
		}
	}()

	t.Run("CLI flag takes precedence over all", func(t *testing.T) {
		_ = os.Setenv("XDG_CONFIG_HOME", "/should/be/ignored")
		defer func() { _ = os.Unsetenv("XDG_CONFIG_HOME") }()

		result, err := ResolveConfigDir("/explicit/path")
		require.NoError(t, err)
		require.Equal(t, "/explicit/path", result)
	})

	t.Run("XDG_CONFIG_HOME takes precedence when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
		defer func() { _ = os.Unsetenv("XDG_CONFIG_HOME") }()

		result, err := ResolveConfigDir("")
		require.NoError(t, err)
		require.Equal(t, tmpDir+"/maybe-dont", result)

		// Verify directory was created
		info, err := os.Stat(result)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("falls back to HOME/.config/maybe-dont when XDG_CONFIG_HOME not set", func(t *testing.T) {
		_ = os.Unsetenv("XDG_CONFIG_HOME")
		tmpHome := t.TempDir()
		_ = os.Setenv("HOME", tmpHome)

		result, err := ResolveConfigDir("")
		require.NoError(t, err)
		require.Equal(t, tmpHome+"/.config/maybe-dont", result)

		// Verify directory was created
		info, err := os.Stat(result)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("returns error when no valid config directory found", func(t *testing.T) {
		_ = os.Unsetenv("XDG_CONFIG_HOME")
		_ = os.Setenv("HOME", "/nonexistent/readonly/path")

		result, err := ResolveConfigDir("")
		require.Error(t, err)
		require.Empty(t, result)
		require.Contains(t, err.Error(), "no config directory found")
	})

	t.Run("does not fall back to ./config - requires explicit flag", func(t *testing.T) {
		_ = os.Unsetenv("XDG_CONFIG_HOME")
		_ = os.Setenv("HOME", "/nonexistent/readonly/path")

		// Create a temp directory with ./config inside it
		tmpDir := t.TempDir()
		oldWd, _ := os.Getwd()
		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(oldWd) }()

		_ = os.MkdirAll("./config", 0o755)

		// Should still fail - ./config is NOT a fallback
		result, err := ResolveConfigDir("")
		require.Error(t, err)
		require.Empty(t, result)

		// But if explicitly provided, it works
		result, err = ResolveConfigDir("./config")
		require.NoError(t, err)
		require.Equal(t, "./config", result)
	})

	t.Run("directory created with 0700 permissions for security", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
		defer func() { _ = os.Unsetenv("XDG_CONFIG_HOME") }()

		result, err := ResolveConfigDir("")
		require.NoError(t, err)

		info, err := os.Stat(result)
		require.NoError(t, err)
		// Check permissions (masking off type bits) - 0700 for security
		require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	})
}

// TestResolveLogDir_XDGSupport tests XDG Base Directory support for log resolution
func TestResolveLogDir_XDGSupport(t *testing.T) {
	// Save original env vars to restore after test
	origXDGState := os.Getenv("XDG_STATE_HOME")
	origHome := os.Getenv("HOME")
	defer func() {
		if origXDGState != "" {
			_ = os.Setenv("XDG_STATE_HOME", origXDGState)
		} else {
			_ = os.Unsetenv("XDG_STATE_HOME")
		}
		if origHome != "" {
			_ = os.Setenv("HOME", origHome)
		}
	}()

	t.Run("CLI flag takes precedence over all", func(t *testing.T) {
		_ = os.Setenv("XDG_STATE_HOME", "/should/be/ignored")
		defer func() { _ = os.Unsetenv("XDG_STATE_HOME") }()

		result, err := ResolveLogDir("/explicit/path")
		require.NoError(t, err)
		require.Equal(t, "/explicit/path", result)
	})

	t.Run("XDG_STATE_HOME takes precedence when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Setenv("XDG_STATE_HOME", tmpDir)
		defer func() { _ = os.Unsetenv("XDG_STATE_HOME") }()

		result, err := ResolveLogDir("")
		require.NoError(t, err)
		require.Equal(t, tmpDir+"/maybe-dont", result)

		// Verify directory was created
		info, statErr := os.Stat(result)
		require.NoError(t, statErr)
		require.True(t, info.IsDir())
	})

	t.Run("falls back to HOME/.local/state/maybe-dont when XDG_STATE_HOME not set", func(t *testing.T) {
		_ = os.Unsetenv("XDG_STATE_HOME")
		tmpHome := t.TempDir()
		_ = os.Setenv("HOME", tmpHome)

		result, err := ResolveLogDir("")
		require.NoError(t, err)
		require.Equal(t, tmpHome+"/.local/state/maybe-dont", result)

		// Verify directory was created
		info, statErr := os.Stat(result)
		require.NoError(t, statErr)
		require.True(t, info.IsDir())
	})

	t.Run("directory created with 0700 permissions for security", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Setenv("XDG_STATE_HOME", tmpDir)
		defer func() { _ = os.Unsetenv("XDG_STATE_HOME") }()

		result, err := ResolveLogDir("")
		require.NoError(t, err)

		info, statErr := os.Stat(result)
		require.NoError(t, statErr)
		// 0700 for security - logs may contain sensitive data
		require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	})
}

// TestEnsureDir tests the helper function for directory creation
func TestEnsureDir(t *testing.T) {
	t.Run("returns true when directory already exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.True(t, ensureDir(tmpDir))
	})

	t.Run("creates directory when it does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		newDir := tmpDir + "/new/nested/dir"

		require.True(t, ensureDir(newDir))

		info, err := os.Stat(newDir)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("returns false when directory cannot be created", func(t *testing.T) {
		// Try to create a directory in a non-existent path
		result := ensureDir("/nonexistent/readonly/system/path/test")
		require.False(t, result)
	})

	t.Run("returns false for file path instead of directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := tmpDir + "/existingfile"

		// Create a file at the path
		err := os.WriteFile(filePath, []byte("test"), 0o644)
		require.NoError(t, err)

		// ensureDir should return false since it's a file, not a directory
		require.False(t, ensureDir(filePath))
	})
}

// TestEnsureFileWritable tests the helper function for file writability validation
func TestEnsureFileWritable(t *testing.T) {
	t.Run("succeeds for writable path", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := tmpDir + "/test.log"

		err := ensureFileWritable(filePath)
		require.NoError(t, err)

		// Verify the file was created
		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr)
	})

	t.Run("creates parent directories if needed", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := tmpDir + "/nested/dirs/test.log"

		err := ensureFileWritable(filePath)
		require.NoError(t, err)

		// Verify the file was created
		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr)
	})

	t.Run("fails for unwritable directory", func(t *testing.T) {
		// Try to write to a system path that doesn't exist and can't be created
		err := ensureFileWritable("/nonexistent/readonly/system/path/test.log")
		require.Error(t, err)
	})
}

// TestGetLogger_FailFast tests that GetLogger fails at startup when log file cannot be written
func TestGetLogger_FailFast(t *testing.T) {
	t.Run("succeeds with stdout", func(t *testing.T) {
		cfg := &Config{
			Logger: struct {
				Level    string         `mapstructure:"level"`
				Path     string         `mapstructure:"path"`
				Rotation RotationConfig `mapstructure:"rotation"`
			}{
				Level: "info",
				Path:  "stdout",
			},
		}

		logger, err := GetLogger(cfg, "/any/path/ignored")
		require.NoError(t, err)
		require.NotNil(t, logger)
	})

	t.Run("succeeds with stderr", func(t *testing.T) {
		cfg := &Config{
			Logger: struct {
				Level    string         `mapstructure:"level"`
				Path     string         `mapstructure:"path"`
				Rotation RotationConfig `mapstructure:"rotation"`
			}{
				Level: "info",
				Path:  "stderr",
			},
		}

		logger, err := GetLogger(cfg, "/any/path/ignored")
		require.NoError(t, err)
		require.NotNil(t, logger)
	})

	t.Run("succeeds with writable file path", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &Config{
			Logger: struct {
				Level    string         `mapstructure:"level"`
				Path     string         `mapstructure:"path"`
				Rotation RotationConfig `mapstructure:"rotation"`
			}{
				Level: "info",
				Path:  "app.log",
			},
		}

		logger, err := GetLogger(cfg, tmpDir)
		require.NoError(t, err)
		require.NotNil(t, logger)
	})

	t.Run("fails fast with unwritable file path", func(t *testing.T) {
		cfg := &Config{
			Logger: struct {
				Level    string         `mapstructure:"level"`
				Path     string         `mapstructure:"path"`
				Rotation RotationConfig `mapstructure:"rotation"`
			}{
				Level: "info",
				Path:  "app.log",
			},
		}

		// Use a path that cannot be written to
		_, err := GetLogger(cfg, "/nonexistent/readonly/system/path")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot write to log file")
	})
}

// writeMinimalConfig writes a minimal valid config file to the given directory.
// This creates a config with all validation disabled and a basic downstream server.
func writeMinimalConfig(t *testing.T, configDir string) {
	t.Helper()
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false
`
	writeConfigFile(t, configDir, configContent)
}

// writeConfigFile writes the given config content to maybe-dont.yaml in the config directory.
func writeConfigFile(t *testing.T, configDir, configContent string) {
	t.Helper()
	err := os.WriteFile(configDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)
}

func TestCLIRequestValidationConfig_Defaults(t *testing.T) {
	// Test: CLI request validation should be disabled by default with empty validate_commands
	viper.Reset()
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir)

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)

	require.False(t, cfg.CLIRequestValidation.Enabled)
	require.Empty(t, cfg.CLIRequestValidation.ValidateCommands)
}

func TestCLIRequestValidationConfig_Enabled(t *testing.T) {
	// Test: CLI request validation can be enabled with specific commands
	viper.Reset()
	configDir := t.TempDir()
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false

cli_request_validation:
  enabled: true
  validate_commands:
    - gh
    - aws
    - kubectl
`
	writeConfigFile(t, configDir, configContent)

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)

	require.True(t, cfg.CLIRequestValidation.Enabled)
	require.Equal(t, []string{"gh", "aws", "kubectl"}, cfg.CLIRequestValidation.ValidateCommands)
}

func TestCLIRequestValidationConfig_WildcardAll(t *testing.T) {
	// Test: "*" validates all commands
	viper.Reset()
	configDir := t.TempDir()
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false

cli_request_validation:
  enabled: true
  validate_commands:
    - "*"
`
	writeConfigFile(t, configDir, configContent)

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)

	require.True(t, cfg.CLIRequestValidation.Enabled)
	require.Equal(t, []string{"*"}, cfg.CLIRequestValidation.ValidateCommands)
}

func TestCLIRequestValidationConfig_ErrorWhenEnabledWithEmptyList(t *testing.T) {
	// Test: enabled=true with empty validate_commands is a config error.
	// This catches misconfigurations where CLI validation is enabled but
	// no commands are specified to validate.
	viper.Reset()
	configDir := t.TempDir()
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false

cli_request_validation:
  enabled: true
  validate_commands: []
`
	writeConfigFile(t, configDir, configContent)

	_, err := LoadConfig(configDir, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "validate_commands")
	require.Contains(t, err.Error(), "empty")
}

func TestCLIRequestValidationConfig_NoErrorWhenDisabled(t *testing.T) {
	// Test: enabled=false with empty list is OK.
	// When CLI validation is disabled, an empty validate_commands list is acceptable.
	viper.Reset()
	configDir := t.TempDir()
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false

cli_request_validation:
  enabled: false
  validate_commands: []
`
	writeConfigFile(t, configDir, configContent)

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)
	require.False(t, cfg.CLIRequestValidation.Enabled)
}

func TestCLIRequestValidationConfig_ErrorWhenEnabledWithOmittedList(t *testing.T) {
	// Test: enabled=true with validate_commands omitted entirely (nil slice) is a config error.
	// This catches the case where someone enables CLI validation but forgets to add commands.
	viper.Reset()
	configDir := t.TempDir()
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false

cli_request_validation:
  enabled: true
`
	writeConfigFile(t, configDir, configContent)

	_, err := LoadConfig(configDir, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "validate_commands")
	require.Contains(t, err.Error(), "empty")
}

func TestCLIRequestValidationConfig_EnvVarOverride(t *testing.T) {
	// Test: Environment variables override config file values for CLI validation settings.
	// This validates that both enabled (bool) and validate_commands ([]string) can be
	// set via environment variables, following the MAYBE_DONT_ prefix convention.
	viper.Reset()
	configDir := t.TempDir()
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: false

response_validation:
  cel:
    enabled: false
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false

cli_request_validation:
  enabled: false
  validate_commands: []
`
	writeConfigFile(t, configDir, configContent)

	t.Setenv("MAYBE_DONT_CLI_REQUEST_VALIDATION_ENABLED", "true")
	t.Setenv("MAYBE_DONT_CLI_REQUEST_VALIDATION_VALIDATE_COMMANDS", "gh,aws,kubectl")

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)

	require.True(t, cfg.CLIRequestValidation.Enabled)
	require.Equal(t, []string{"gh", "aws", "kubectl"}, cfg.CLIRequestValidation.ValidateCommands)
}

func TestCLIRequestValidationConfig_EnvVarWildcard(t *testing.T) {
	// Test: "*" works via environment variable for validate_commands.
	// This validates that the wildcard value can be set via env var to enable
	// validation of all CLI commands.
	viper.Reset()
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir)

	t.Setenv("MAYBE_DONT_CLI_REQUEST_VALIDATION_ENABLED", "true")
	t.Setenv("MAYBE_DONT_CLI_REQUEST_VALIDATION_VALIDATE_COMMANDS", "*")

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)

	require.True(t, cfg.CLIRequestValidation.Enabled)
	require.Equal(t, []string{"*"}, cfg.CLIRequestValidation.ValidateCommands)
}

// TestPolicyExpressionFields verifies the Policy struct correctly loads:
// 1. The legacy 'expression' field for backwards compatibility
// 2. The new 'mcp_expression' field for MCP-specific validation
// 3. The new 'cli_expression' field for CLI command validation
// 4. Combinations of all three fields
func TestPolicyExpressionFields(t *testing.T) {
	tests := []struct {
		name              string
		yamlContent       string
		wantExpression    string
		wantMCPExpression string
		wantCLIExpression string
		wantName          string
	}{
		{
			name: "legacy expression field only (backwards compatibility)",
			yamlContent: `
rules:
  - name: legacy-rule
    description: Uses legacy expression field
    expression: 'request.params.name == "test"'
    action: deny
    message: Denied by legacy rule
`,
			wantExpression:    `request.params.name == "test"`,
			wantMCPExpression: "",
			wantCLIExpression: "",
			wantName:          "legacy-rule",
		},
		{
			name: "mcp_expression field only",
			yamlContent: `
rules:
  - name: mcp-only-rule
    description: Uses mcp_expression field
    mcp_expression: 'request.params.name.startsWith("dangerous")'
    action: deny
    message: Denied by MCP rule
`,
			wantExpression:    "",
			wantMCPExpression: `request.params.name.startsWith("dangerous")`,
			wantCLIExpression: "",
			wantName:          "mcp-only-rule",
		},
		{
			name: "cli_expression field only",
			yamlContent: `
rules:
  - name: cli-only-rule
    description: Uses cli_expression field
    cli_expression: 'cli.command == "rm" && cli.args.exists(a, a == "-rf")'
    action: deny
    message: Denied by CLI rule
`,
			wantExpression:    "",
			wantMCPExpression: "",
			wantCLIExpression: `cli.command == "rm" && cli.args.exists(a, a == "-rf")`,
			wantName:          "cli-only-rule",
		},
		{
			name: "all three expression fields",
			yamlContent: `
rules:
  - name: dual-rule
    description: Has expressions for both MCP and CLI
    expression: 'true'
    mcp_expression: 'request.params.name == "mcp_tool"'
    cli_expression: 'cli.command == "dangerous"'
    action: deny
    message: Blocked
`,
			wantExpression:    "true",
			wantMCPExpression: `request.params.name == "mcp_tool"`,
			wantCLIExpression: `cli.command == "dangerous"`,
			wantName:          "dual-rule",
		},
		{
			name: "mcp_expression and cli_expression without legacy expression",
			yamlContent: `
rules:
  - name: new-style-rule
    description: Uses new fields without legacy expression
    mcp_expression: 'request.params.name == "tool"'
    cli_expression: 'cli.command == "cmd"'
    action: deny
    message: Blocked
`,
			wantExpression:    "",
			wantMCPExpression: `request.params.name == "tool"`,
			wantCLIExpression: `cli.command == "cmd"`,
			wantName:          "new-style-rule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file with YAML content
			tmpFile, err := os.CreateTemp("", "policy-*.yaml")
			require.NoError(t, err)
			defer func() { _ = os.Remove(tmpFile.Name()) }()

			_, err = tmpFile.WriteString(tt.yamlContent)
			require.NoError(t, err)
			err = tmpFile.Close()
			require.NoError(t, err)

			// Load policies from file
			policies, err := LoadPoliciesFromFile(tmpFile.Name())
			require.NoError(t, err)
			require.Len(t, policies, 1)

			policy := policies[0]
			require.Equal(t, tt.wantName, policy.Name)
			require.Equal(t, tt.wantExpression, policy.Expression, "Expression field mismatch")
			require.Equal(t, tt.wantMCPExpression, policy.MCPExpression, "MCPExpression field mismatch")
			require.Equal(t, tt.wantCLIExpression, policy.CLIExpression, "CLIExpression field mismatch")
		})
	}
}

// TestPolicyExpressionFields_MultipleRules verifies that multiple rules
// with different expression configurations can be loaded from a single file.
func TestPolicyExpressionFields_MultipleRules(t *testing.T) {
	yamlContent := `
rules:
  - name: legacy-rule
    expression: 'true'
    action: deny
    message: Legacy
  - name: mcp-rule
    mcp_expression: 'request.params.name == "mcp"'
    action: deny
    message: MCP only
  - name: cli-rule
    cli_expression: 'cli.command == "rm"'
    action: deny
    message: CLI only
  - name: dual-rule
    mcp_expression: 'request.method == "tools/call"'
    cli_expression: 'cli.command != ""'
    action: allow
    message: Dual expressions
`

	tmpFile, err := os.CreateTemp("", "policy-multi-*.yaml")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	_, err = tmpFile.WriteString(yamlContent)
	require.NoError(t, err)
	err = tmpFile.Close()
	require.NoError(t, err)

	policies, err := LoadPoliciesFromFile(tmpFile.Name())
	require.NoError(t, err)
	require.Len(t, policies, 4)

	// Verify each rule loaded correctly
	require.Equal(t, "legacy-rule", policies[0].Name)
	require.Equal(t, "true", policies[0].Expression)
	require.Empty(t, policies[0].MCPExpression)
	require.Empty(t, policies[0].CLIExpression)

	require.Equal(t, "mcp-rule", policies[1].Name)
	require.Empty(t, policies[1].Expression)
	require.Equal(t, `request.params.name == "mcp"`, policies[1].MCPExpression)
	require.Empty(t, policies[1].CLIExpression)

	require.Equal(t, "cli-rule", policies[2].Name)
	require.Empty(t, policies[2].Expression)
	require.Empty(t, policies[2].MCPExpression)
	require.Equal(t, `cli.command == "rm"`, policies[2].CLIExpression)

	require.Equal(t, "dual-rule", policies[3].Name)
	require.Empty(t, policies[3].Expression)
	require.Equal(t, `request.method == "tools/call"`, policies[3].MCPExpression)
	require.Equal(t, `cli.command != ""`, policies[3].CLIExpression)
}

// TestLoadConfig_DeprecationWarningAppearsOnce verifies that when validation.ai.provider is not set,
// the deprecation warning is emitted exactly once during LoadConfig. LoadConfig validates internally
// via validateConfigWithOptions; callers should NOT call ValidateConfig separately, as that would
// produce duplicate warnings and discard context (configFileFound, loadErrors).
func TestLoadConfig_DeprecationWarningAppearsOnce(t *testing.T) {
	viper.Reset()

	tmpDir := t.TempDir()

	// Config with AI validation enabled and provider deliberately unset to trigger the warning.
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: true
    rules_file: ai_request_rules.yaml

validation:
  ai:
    endpoint: https://api.openai.com/v1
    model: gpt-4
    api_key: test-key
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	rulesContent := `
rules:
  - name: test-rule
    description: Test rule
    prompt: Test prompt
    message: Test message
`
	err = os.WriteFile(tmpDir+"/ai_request_rules.yaml", []byte(rulesContent), 0o644)
	require.NoError(t, err)

	// Capture stderr to count deprecation warnings
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() {
		os.Stderr = origStderr
		_ = w.Close()
		_ = r.Close()
	}()

	_, err = LoadConfig(tmpDir, "")
	require.NoError(t, err)

	_ = w.Close()
	os.Stderr = origStderr

	captured, err := io.ReadAll(r)
	require.NoError(t, err)
	_ = r.Close()
	output := string(captured)

	deprecationMsg := "DEPRECATION: validation.ai.provider not set"
	count := strings.Count(output, deprecationMsg)
	require.Equal(t, 1, count,
		"deprecation warning should appear exactly once, got %d in output: %q", count, output)
}

// TestLoadConfig_ProviderSetExplicitly verifies that when validation.ai.provider is explicitly
// set in the YAML config, no deprecation warning is emitted.
func TestLoadConfig_ProviderSetExplicitly(t *testing.T) {
	viper.Reset()

	tmpDir := t.TempDir()

	// Config with provider explicitly set — should produce no deprecation warning
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: true
    rules_file: ai_request_rules.yaml

validation:
  ai:
    provider: openai
    endpoint: "https://api.openai.com/v1/chat/completions"
    model: "gpt-5.2"
    api_key: test-key
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	rulesContent := `
rules:
  - name: test-rule
    description: Test rule
    prompt: Test prompt
    message: Test message
`
	err = os.WriteFile(tmpDir+"/ai_request_rules.yaml", []byte(rulesContent), 0o644)
	require.NoError(t, err)

	// Capture stderr to check for deprecation warnings
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = origStderr }() // safety net if LoadConfig panics

	cfg, loadErr := LoadConfig(tmpDir, "")

	// Restore stderr and capture output immediately after function under test
	os.Stderr = origStderr
	require.NoError(t, w.Close())
	captured, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	// Now assert on config and captured output
	require.NoError(t, loadErr)
	require.Equal(t, "openai", cfg.Validation.AI.Provider, "Provider should be 'openai' from YAML")

	require.NotContains(t, string(captured), "DEPRECATION",
		"No deprecation warning should appear when provider is explicitly set, but got: %q", string(captured))
}

// TestLoadConfig_ProviderFromYAMLWithAPIKeyFromEnv verifies that setting api_key via environment
// variable does not clobber the provider field set in YAML. This reproduces a scenario where
// viper's AutomaticEnv may interfere with sibling fields in nested structs.
func TestLoadConfig_ProviderFromYAMLWithAPIKeyFromEnv(t *testing.T) {
	viper.Reset()

	tmpDir := t.TempDir()

	// Config with provider set in YAML but api_key NOT set (will come from env var)
	configContent := `
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
  ai:
    enabled: true
    rules_file: ai_request_rules.yaml

validation:
  ai:
    provider: openai
    endpoint: "https://api.openai.com/v1/chat/completions"
    model: "gpt-5.2"
`
	err := os.WriteFile(tmpDir+"/maybe-dont.yaml", []byte(configContent), 0o644)
	require.NoError(t, err)

	rulesContent := `
rules:
  - name: test-rule
    description: Test rule
    prompt: Test prompt
    message: Test message
`
	err = os.WriteFile(tmpDir+"/ai_request_rules.yaml", []byte(rulesContent), 0o644)
	require.NoError(t, err)

	// Set api_key via env var (like the user does)
	t.Setenv("MAYBE_DONT_VALIDATION_AI_API_KEY", "sk-test-key-from-env")

	// Capture stderr to check for deprecation warnings
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = origStderr }() // safety net if LoadConfig panics

	cfg, loadErr := LoadConfig(tmpDir, "")

	// Restore stderr and capture output immediately after function under test
	os.Stderr = origStderr
	require.NoError(t, w.Close())
	captured, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	// Now assert on config and captured output
	require.NoError(t, loadErr)
	require.Equal(t, "openai", cfg.Validation.AI.Provider,
		"Provider should be 'openai' from YAML even when api_key is set via env var")
	require.Equal(t, "sk-test-key-from-env", cfg.Validation.AI.APIKey,
		"API key should come from env var")

	require.NotContains(t, string(captured), "DEPRECATION",
		"No deprecation warning should appear when provider is explicitly set, but got: %q", string(captured))
}

// TestAuditConfigShouldIncludeArgumentValues verifies the default and explicit behavior
// of the ShouldIncludeArgumentValues method on AuditConfig.
func TestAuditConfigShouldIncludeArgumentValues(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name     string
		config   AuditConfig
		expected bool
	}{
		{
			name:     "nil defaults to true",
			config:   AuditConfig{},
			expected: true,
		},
		{
			name:     "explicitly true",
			config:   AuditConfig{IncludeArgumentValues: boolPtr(true)},
			expected: true,
		},
		{
			name:     "explicitly false",
			config:   AuditConfig{IncludeArgumentValues: boolPtr(false)},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ShouldIncludeArgumentValues()
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestAuditIncludeArgumentValues_EnvVarOverride verifies that the audit.include_argument_values
// setting can be overridden via the MAYBE_DONT_AUDIT_INCLUDE_ARGUMENT_VALUES environment variable.
// The YAML default is true; this test confirms the env var can flip it to false.
func TestAuditIncludeArgumentValues_EnvVarOverride(t *testing.T) {
	viper.Reset()
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir)

	t.Setenv("MAYBE_DONT_AUDIT_INCLUDE_ARGUMENT_VALUES", "false")

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)

	require.False(t, cfg.Audit.ShouldIncludeArgumentValues())
}

// TestInterceptConfigDefaults verifies that intercept config loads with
// correct defaults: enabled=true, default shell tool names.
func TestInterceptConfigDefaults(t *testing.T) {
	viper.Reset()
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir)

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)

	require.True(t, cfg.Intercept.Enabled)
	require.NotEmpty(t, cfg.Intercept.ShellToolNames)
	require.Contains(t, cfg.Intercept.ShellToolNames, "Bash")
	require.Contains(t, cfg.Intercept.ShellToolNames, "execute_command")
}

// TestInterceptConfigEnvOverride verifies that intercept config can be
// overridden via MAYBE_DONT_INTERCEPT_ENABLED env var.
func TestInterceptConfigEnvOverride(t *testing.T) {
	viper.Reset()
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir)

	t.Setenv("MAYBE_DONT_INTERCEPT_ENABLED", "false")

	cfg, err := LoadConfig(configDir, "")
	require.NoError(t, err)

	require.False(t, cfg.Intercept.Enabled)
}

// TestPolicyMode_IsValid verifies that IsValid accepts audit_only and enforce,
// but rejects empty string and unknown values.
func TestPolicyMode_IsValid(t *testing.T) {
	tests := []struct {
		mode PolicyMode
		want bool
	}{
		{PolicyModeAuditOnly, true},
		{PolicyModeEnforce, true},
		{"", false},
		{"block", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		name := string(tt.mode)
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.mode.IsValid())
		})
	}
}

// TestPolicyMode_Normalize verifies that Normalize converts empty string
// to enforce and leaves other values unchanged.
func TestPolicyMode_Normalize(t *testing.T) {
	tests := []struct {
		name        string
		mode        PolicyMode
		defaultMode PolicyMode
		want        PolicyMode
	}{
		{"empty with audit_only default", "", PolicyModeAuditOnly, PolicyModeAuditOnly},
		{"empty with enforce default", "", PolicyModeEnforce, PolicyModeEnforce},
		{"audit_only unchanged regardless of default", PolicyModeAuditOnly, PolicyModeEnforce, PolicyModeAuditOnly},
		{"enforce unchanged regardless of default", PolicyModeEnforce, PolicyModeAuditOnly, PolicyModeEnforce},
		{"invalid values pass through (caught by IsValid)", "block", PolicyModeEnforce, "block"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.mode.Normalize(tt.defaultMode))
		})
	}
}

// TestPolicyMode_IsAuditOnly verifies that only audit_only returns true;
// enforce means rules can block.
func TestPolicyMode_IsAuditOnly(t *testing.T) {
	tests := []struct {
		mode PolicyMode
		want bool
	}{
		{PolicyModeAuditOnly, true},
		{PolicyModeEnforce, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			require.Equal(t, tt.want, tt.mode.IsAuditOnly())
		})
	}
}

// TestResolvePolicyMode_Enforce verifies that the enforce mode resolves
// correctly at both top-level and per-rule scope. The function always returns
// one of the two valid constants (audit_only or enforce), never empty string.
func TestResolvePolicyMode_Enforce(t *testing.T) {
	tests := []struct {
		name     string
		topLevel PolicyMode
		rule     PolicyMode
		want     PolicyMode
	}{
		{"top-level audit_only overrides rule enforce", PolicyModeAuditOnly, PolicyModeEnforce, PolicyModeAuditOnly},
		{"top-level enforce with rule audit_only", PolicyModeEnforce, PolicyModeAuditOnly, PolicyModeAuditOnly},
		{"top-level enforce with rule enforce", PolicyModeEnforce, PolicyModeEnforce, PolicyModeEnforce},
		{"both audit_only", PolicyModeAuditOnly, PolicyModeAuditOnly, PolicyModeAuditOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResolvePolicyMode(tt.topLevel, tt.rule))
		})
	}
}

// TestPolicyMode_ViaEnvVar verifies that setting mode via environment variable
// correctly overrides the default audit_only mode for valid values.
func TestPolicyMode_ViaEnvVar(t *testing.T) {
	tests := []struct {
		name      string
		envValue  string
		wantMode  PolicyMode
		wantAudit bool
	}{
		{"enforce via env var", "enforce", PolicyModeEnforce, false},
		{"audit_only via env var", "audit_only", PolicyModeAuditOnly, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			writeMinimalConfig(t, configDir)

			t.Setenv("MAYBE_DONT_REQUEST_VALIDATION_CEL_MODE", tt.envValue)

			cfg, err := LoadConfig(configDir, "")
			require.NoError(t, err)
			require.Equal(t, tt.wantMode, cfg.RequestValidation.CEL.Mode)
			require.Equal(t, tt.wantAudit, cfg.RequestValidation.CEL.Mode.IsAuditOnly())
		})
	}
}

// TestPolicyMode_InvalidModeRejected verifies that invalid policy mode values
// are rejected by LoadConfig regardless of how they're set.
func TestPolicyMode_InvalidModeRejected(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
	}{
		{"block", "block"},
		{"enabled", "enabled"},
		{"disabled", "disabled"},
		{"wrong case AUDIT_ONLY", "AUDIT_ONLY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			writeMinimalConfig(t, configDir)

			t.Setenv("MAYBE_DONT_REQUEST_VALIDATION_CEL_MODE", tt.envValue)

			_, err := LoadConfig(configDir, "")
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid policy mode")
		})
	}
}

// TestPolicyMode_InvalidPerRuleModeRejected verifies that an invalid mode
// on a per-rule policy is rejected during validation across all four phases.
func TestPolicyMode_InvalidPerRuleModeRejected(t *testing.T) {
	tests := []struct {
		name  string
		setup func(cfg *Config)
	}{
		{"CEL request rule", func(cfg *Config) {
			cfg.RequestValidation.CEL.Rules = []Policy{{Name: "bad-rule", Mode: "block"}}
		}},
		{"AI request rule", func(cfg *Config) {
			cfg.RequestValidation.AI.Rules = []AIPolicy{{Name: "bad-rule", Mode: "block"}}
		}},
		{"CEL response rule", func(cfg *Config) {
			cfg.ResponseValidation.CEL.Rules = []ResponsePolicy{{Name: "bad-rule", Mode: "block"}}
		}},
		{"AI response rule", func(cfg *Config) {
			cfg.ResponseValidation.AI.Rules = []AIResponsePolicy{{Name: "bad-rule", Mode: "block"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Server.Type = ServerTypeHTTP
			cfg.Server.ListenAddr = "127.0.0.1:8080"
			cfg.RequestValidation.CEL.Mode = PolicyModeAuditOnly
			cfg.RequestValidation.AI.Mode = PolicyModeAuditOnly
			cfg.ResponseValidation.CEL.Mode = PolicyModeAuditOnly
			cfg.ResponseValidation.AI.Mode = PolicyModeAuditOnly
			tt.setup(cfg)
			err := ValidateConfig(cfg)
			require.Error(t, err)
			require.Contains(t, err.Error(), "bad-rule")
			require.Contains(t, err.Error(), "invalid policy mode")
		})
	}
}

// TestPolicyMode_InvalidModeInYAML verifies that an invalid mode value written
// directly in the YAML config file is rejected by LoadConfig across all four phases.
func TestPolicyMode_InvalidModeInYAML(t *testing.T) {
	tests := []struct {
		name     string
		badField string // which mode field gets the invalid value
		badValue string
	}{
		{"CEL request mode", "request_validation.cel", "block"},
		{"AI request mode", "request_validation.ai", "enabled"},
		{"CEL response mode", "response_validation.cel", "disabled"},
		{"AI response mode", "response_validation.ai", "AUDIT_ONLY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build a config where only the targeted field has an invalid mode
			celReqMode := "audit_only"
			aiReqMode := "audit_only"
			celRespMode := "audit_only"
			aiRespMode := "audit_only"

			switch tt.badField {
			case "request_validation.cel":
				celReqMode = tt.badValue
			case "request_validation.ai":
				aiReqMode = tt.badValue
			case "response_validation.cel":
				celRespMode = tt.badValue
			case "response_validation.ai":
				aiRespMode = tt.badValue
			}

			configDir := t.TempDir()
			writeConfigFile(t, configDir, fmt.Sprintf(`
downstream_mcp_servers:
  test:
    type: stdio
    command: echo

request_validation:
  cel:
    enabled: false
    mode: %s
  ai:
    enabled: false
    mode: %s

response_validation:
  cel:
    enabled: false
    mode: %s
  ai:
    enabled: false
    mode: %s

native_tools:
  audit_report:
    enabled: false
`, celReqMode, aiReqMode, celRespMode, aiRespMode))

			_, err := LoadConfig(configDir, "")
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid policy mode")
			require.Contains(t, err.Error(), tt.badValue)
		})
	}
}
