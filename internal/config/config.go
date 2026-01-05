package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"
)

// ServerType represents the type of server to run
type ServerType string

const (
	ServerTypeSTDIO ServerType = "stdio"
	ServerTypeHTTP  ServerType = "http"
	ServerTypeSSE   ServerType = "sse"
)

// Config represents the application configuration
type Config struct {
	// Server configuration
	Server struct {
		Type       ServerType `mapstructure:"type"` // stdio, http, sse
		ListenAddr string     `mapstructure:"listen_addr"`
		SSE        struct {
			TLS struct {
				Enabled  bool   `mapstructure:"enabled"`
				CertFile string `mapstructure:"cert_file"`
				KeyFile  string `mapstructure:"key_file"`
			} `mapstructure:"tls"`
		} `mapstructure:"sse"`
		// TrustedProxies is a list of IP addresses, IPv6 addresses, or CIDR blocks
		// that are trusted to provide accurate X-Forwarded-For headers.
		// When configured, only the rightmost IP in X-Forwarded-For that is NOT a trusted proxy
		// will be used as the client IP (this is the most secure approach).
		// If empty or not set, all proxies are trusted and the leftmost (first) IP is used.
		// Examples: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1", "fc00::/7"]
		TrustedProxies []string `mapstructure:"trusted_proxies"`
	} `mapstructure:"server"`

	// Policy configuration
	PolicyValidation struct {
		Enabled   bool        `mapstructure:"enabled"`
		RulesFile string      `mapstructure:"rules_file"`
		Rules     []CELPolicy `mapstructure:"rules"`
	} `mapstructure:"policy_validation"`

	// AI validation configuration
	AIPolicyValidation struct {
		Enabled   bool       `mapstructure:"enabled"`
		Endpoint  string     `mapstructure:"endpoint"`
		Model     string     `mapstructure:"model"`
		RulesFile string     `mapstructure:"rules_file"`
		APIKey    string     `mapstructure:"api_key"`
		Rules     []AIPolicy `mapstructure:"rules"`
	} `mapstructure:"ai_validation"`

	// Response validation configuration
	ResponseValidation struct {
		Enabled   bool                `mapstructure:"enabled"`
		RulesFile string              `mapstructure:"rules_file"`
		Rules     []CELResponsePolicy `mapstructure:"rules"`
	} `mapstructure:"response_validation"`

	// AI response validation configuration
	AIResponseValidation struct {
		Enabled   bool               `mapstructure:"enabled"`
		RulesFile string             `mapstructure:"rules_file"`
		Rules     []AIResponsePolicy `mapstructure:"rules"`
	} `mapstructure:"ai_response_validation"`

	// Downstream MCP servers configuration
	DownstreamMCPServers map[string]ClientConfig `mapstructure:"downstream_mcp_servers"`

	// Audit configuration
	Audit struct {
		Enabled bool   `mapstructure:"enabled"`
		Path    string `mapstructure:"path"`
	} `mapstructure:"audit"`

	// Logging configuration
	Logging struct {
		LogLevel string `mapstructure:"level"`
		Path     string `mapstructure:"path"`
	} `mapstructure:"logging"`

	// NativeTools configuration for gateway-native tools
	NativeTools struct {
		Enabled bool `mapstructure:"enabled"`

		AuditLog struct {
			Enabled          bool  `mapstructure:"enabled"`
			MaxEntries       int   `mapstructure:"max_entries"`
			MaxFileSizeBytes int64 `mapstructure:"max_file_size_bytes"`
		} `mapstructure:"audit_log"`

		AuditReport struct {
			Enabled             bool   `mapstructure:"enabled"`
			Endpoint            string `mapstructure:"endpoint"`
			Model               string `mapstructure:"model"`
			APIKey              string `mapstructure:"api_key"`
			MaxEntriesForReport int    `mapstructure:"max_entries_for_report"`
			SystemPrompt        string `mapstructure:"system_prompt"`
		} `mapstructure:"audit_report"`

		ListServers struct {
			Enabled bool `mapstructure:"enabled"`
		} `mapstructure:"list_servers"`

		ListSessions struct {
			Enabled bool `mapstructure:"enabled"`
		} `mapstructure:"list_sessions"`
	} `mapstructure:"native_tools"`
}

// ClientConfig represents configuration for a single MCP client
type ClientConfig struct {
	Type          string   `mapstructure:"type"` // stdio, sse, http
	DownstreamURL string   `mapstructure:"downstream_url"`
	URL           string   `mapstructure:"url"` // Alias for downstream_url
	Command       string   `mapstructure:"command"`
	CommandArgs   []string `mapstructure:"command_args"`
	Args          []string `mapstructure:"args"` // Alias for command_args

	// Initialization configuration for stdio clients
	StartupTimeoutMs      int `mapstructure:"startup_timeout_ms"`     // Timeout for process startup (default: 30000ms)
	InitializationRetries int `mapstructure:"initialization_retries"` // Number of retry attempts (default: 5)
	RetryDelayMs          int `mapstructure:"retry_delay_ms"`         // Base delay between retries (default: 100ms)

	// Capability discovery configuration
	CapabilityDiscoveryDelayMs int `mapstructure:"capability_discovery_delay_ms"` // Delay before discovering capabilities (default: 1000ms for stdio, 0 for others)
	CapabilityDiscoveryRetries int `mapstructure:"capability_discovery_retries"`  // Retries for empty capability lists (default: 3)
	CapabilityRetryDelayMs     int `mapstructure:"capability_retry_delay_ms"`     // Delay between capability retries (default: 500ms)

	SSEConfig struct {
		Headers map[string]string `mapstructure:"headers"`
	} `mapstructure:"sse"`
	HTTPConfig struct {
		Headers map[string]string `mapstructure:"headers"`
	} `mapstructure:"http"`

	// Pass-through authentication configuration
	Auth struct {
		PassThrough struct {
			Enabled bool `mapstructure:"enabled"`

			// HTTP/SSE: List of header mappings
			Headers []CredentialMapping `mapstructure:"headers"`
		} `mapstructure:"pass_through"`
	} `mapstructure:"auth"`
}

// CredentialMapping defines how to map a credential from incoming headers to downstream
type CredentialMapping struct {
	SourceHeader string `mapstructure:"source_header"` // Incoming HTTP header name to extract credential from (e.g., "X-GitHub-Token")
	TargetHeader string `mapstructure:"target_header"` // Downstream HTTP header name (e.g., "Authorization"). Also used as session storage key.
	Format       string `mapstructure:"format"`        // Optional. Template for value formatting. Use {value} placeholder. Default: "{value}" (raw value passthrough). Examples: "Bearer {value}", "sha256={value}"
}

// CELPolicy represents a single CEL policy rule
type CELPolicy struct {
	Name        string `mapstructure:"name"`
	Description string `mapstructure:"description"`
	Expression  string `mapstructure:"expression"`
	Action      string `mapstructure:"action"` // allow or deny
	Message     string `mapstructure:"message"`
}

type AIPolicy struct {
	Name        string `mapstructure:"name"`
	Description string `mapstructure:"description"`
	Prompt      string `mapstructure:"prompt"`
	Message     string `mapstructure:"message"`
}

// CELResponsePolicy represents a single CEL response policy rule
type CELResponsePolicy struct {
	Name                 string `mapstructure:"name"`
	Description          string `mapstructure:"description"`
	Expression           string `mapstructure:"expression"`
	Action               string `mapstructure:"action"` // allow, deny, or redact
	Message              string `mapstructure:"message"`
	RedactionPattern     string `mapstructure:"redaction_pattern"`
	RedactionReplacement string `mapstructure:"redaction_replacement"`
}

// AIResponsePolicy represents a single AI response policy rule
type AIResponsePolicy struct {
	Name        string `mapstructure:"name"`
	Description string `mapstructure:"description"`
	Prompt      string `mapstructure:"prompt"`
	Action      string `mapstructure:"action"` // allow, deny, or redact
	Message     string `mapstructure:"message"`
}

// LoadPoliciesFromFile loads CEL policies from a file
func LoadCELPoliciesFromFile(rulesFile string) ([]CELPolicy, error) {
	if rulesFile == "" {
		return nil, nil
	}

	data, err := os.ReadFile(rulesFile)
	if err != nil {
		return nil, fmt.Errorf("error reading rules file: %w", err)
	}

	var policies struct {
		Rules []CELPolicy `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("error unmarshaling rules: %w", err)
	}

	return policies.Rules, nil
}

// LoadAIPoliciesFromFile loads AI policies from a file
func LoadAIPoliciesFromFile(path string) ([]AIPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading AI policies file: %w", err)
	}

	var policies struct {
		Rules []AIPolicy `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("error unmarshaling AI policies: %w", err)
	}

	return policies.Rules, nil
}

// LoadCELResponsePoliciesFromFile loads CEL response policies from a file
func LoadCELResponsePoliciesFromFile(rulesFile string) ([]CELResponsePolicy, error) {
	if rulesFile == "" {
		return nil, nil
	}

	data, err := os.ReadFile(rulesFile)
	if err != nil {
		return nil, fmt.Errorf("error reading response rules file: %w", err)
	}

	var policies struct {
		Rules []CELResponsePolicy `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("error unmarshaling response rules: %w", err)
	}

	return policies.Rules, nil
}

// LoadAIResponsePoliciesFromFile loads AI response policies from a file
func LoadAIResponsePoliciesFromFile(path string) ([]AIResponsePolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading AI response policies file: %w", err)
	}

	var policies struct {
		Rules []AIResponsePolicy `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("error unmarshaling AI response policies: %w", err)
	}

	return policies.Rules, nil
}

// resolveRulesFilePath resolves a rules file path relative to the config directory
// If the path is empty or absolute, it returns the path as-is
// If the path is relative, it joins it with the config directory
func resolveRulesFilePath(rulesFile, configDir string) string {
	if rulesFile == "" {
		return ""
	}
	if filepath.IsAbs(rulesFile) {
		return rulesFile
	}
	return filepath.Join(configDir, rulesFile)
}

// expandEnvironmentVariables recursively expands environment variables in string fields
// of a struct using os.ExpandEnv. This processes ${VAR} and $VAR syntax.
func expandEnvironmentVariables(v reflect.Value) {
	if !v.IsValid() || !v.CanSet() {
		return
	}

	switch v.Kind() {
	case reflect.String:
		// Expand environment variables in string fields
		expanded := os.ExpandEnv(v.String())
		v.SetString(expanded)

	case reflect.Struct:
		// Recursively process struct fields
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.CanSet() {
				expandEnvironmentVariables(field)
			}
		}

	case reflect.Slice:
		// Process slice elements
		for i := 0; i < v.Len(); i++ {
			expandEnvironmentVariables(v.Index(i))
		}

	case reflect.Map:
		// Process map values
		if v.Type().Elem().Kind() == reflect.String {
			// Handle string maps (like headers)
			for _, key := range v.MapKeys() {
				val := v.MapIndex(key)
				if val.IsValid() && val.Kind() == reflect.String {
					expanded := os.ExpandEnv(val.String())
					v.SetMapIndex(key, reflect.ValueOf(expanded))
				}
			}
		} else {
			// Handle maps with non-string values (like map[string]ClientConfig)
			for _, key := range v.MapKeys() {
				val := v.MapIndex(key)
				if val.IsValid() {
					// For struct values in maps, we need to create a new value
					// since map elements are not addressable
					elemCopy := reflect.New(val.Type()).Elem()
					elemCopy.Set(val)
					expandEnvironmentVariables(elemCopy)
					v.SetMapIndex(key, elemCopy)
				}
			}
		}

	case reflect.Ptr:
		// Dereference pointer and process
		if !v.IsNil() {
			expandEnvironmentVariables(v.Elem())
		}

	case reflect.Interface:
		// Process interface values
		if !v.IsNil() {
			expandEnvironmentVariables(v.Elem())
		}
	}
}

// LoadConfig loads the configuration from all sources
func LoadConfig(configPath string) (*Config, error) {
	// Use the global viper instance to ensure flag bindings work
	v := viper.GetViper()

	// Set environment variable prefix
	v.SetEnvPrefix("MCP_GATEWAY")
	v.AutomaticEnv()

	// Set up environment variable key mappings for nested map structures
	// This allows environment variables to properly map to nested config fields
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Set config file name
	v.SetConfigName("gateway-config")
	v.SetConfigType("yaml")

	// Add config paths
	if configPath != "" {
		v.AddConfigPath(configPath)
	} else {
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.maybe-dont")
	}

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config from path %s: %w", configPath, err)
	}

	// Get the directory containing the config file for resolving relative rules file paths
	configFileDir := filepath.Dir(v.ConfigFileUsed())

	// Unmarshal config
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config from %s: %w", v.ConfigFileUsed(), err)
	}

	// Expand environment variables in all string fields
	expandEnvironmentVariables(reflect.ValueOf(&config).Elem())

	// Set default server type to stdio if not configured
	if config.Server.Type == "" {
		config.Server.Type = ServerTypeSTDIO
	}

	// Set default listen address to 127.0.0.1 for non-stdio servers if not set
	if config.Server.Type != ServerTypeSTDIO && config.Server.ListenAddr == "" {
		config.Server.ListenAddr = "127.0.0.1"
	}

	// TODO : Seems like this environment variable should be configurable.
	// Override OpenAI API key from environment variable if set
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		config.AIPolicyValidation.APIKey = apiKey
	}

	// Set default values for native tools configuration
	if config.NativeTools.AuditLog.MaxEntries == 0 {
		config.NativeTools.AuditLog.MaxEntries = 1000
	}
	if config.NativeTools.AuditLog.MaxFileSizeBytes == 0 {
		config.NativeTools.AuditLog.MaxFileSizeBytes = 10 * 1024 * 1024 // 10MB
	}
	if config.NativeTools.AuditReport.MaxEntriesForReport == 0 {
		config.NativeTools.AuditReport.MaxEntriesForReport = 500
	}
	// Inherit AI settings from ai_validation if not specified
	if config.NativeTools.AuditReport.Endpoint == "" {
		config.NativeTools.AuditReport.Endpoint = config.AIPolicyValidation.Endpoint
	}
	if config.NativeTools.AuditReport.Model == "" {
		config.NativeTools.AuditReport.Model = config.AIPolicyValidation.Model
	}
	if config.NativeTools.AuditReport.APIKey == "" {
		config.NativeTools.AuditReport.APIKey = config.AIPolicyValidation.APIKey
	}
	if config.NativeTools.AuditReport.SystemPrompt == "" {
		config.NativeTools.AuditReport.SystemPrompt = `You are an AI security analyst reviewing MCP gateway audit logs.
Analyze the tool calls, validation results, and patterns to provide insights on:
- Security concerns or policy violations
- Usage patterns and anomalies
- Recommendations for policy improvements

When reporting concerns, prioritize them by potential business impact:
1. HIGH: Direct monetary loss, data breach, regulatory violations, service outages
2. MEDIUM: Reputational damage, customer trust erosion, operational inefficiencies
3. LOW: Minor policy deviations, informational findings, optimization opportunities

For each concern, estimate the potential impact category and explain the reasoning.`
	}

	// Load CEL request policies from rules file
	if config.PolicyValidation.Enabled {
		if config.PolicyValidation.RulesFile == "" {
			// TODO : Note that the docs say this is optional and we have default rules?
			return nil, fmt.Errorf("policy_validation is enabled but rules_file is not specified")
		}
		resolvedPath := resolveRulesFilePath(config.PolicyValidation.RulesFile, configFileDir)
		policies, err := LoadCELPoliciesFromFile(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("error loading CEL policies from file: %w", err)
		}
		config.PolicyValidation.Rules = policies
	}

	// Load AI request policies from rules file
	if config.AIPolicyValidation.Enabled {
		if config.AIPolicyValidation.RulesFile == "" {
			return nil, fmt.Errorf("ai_validation is enabled but rules_file is not specified")
		}
		resolvedPath := resolveRulesFilePath(config.AIPolicyValidation.RulesFile, configFileDir)
		aiPolicies, err := LoadAIPoliciesFromFile(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("error loading AI policies from file: %w", err)
		}
		config.AIPolicyValidation.Rules = aiPolicies
	}

	// Load CEL response policies from rules file
	if config.ResponseValidation.Enabled {
		if config.ResponseValidation.RulesFile == "" {
			return nil, fmt.Errorf("response_validation is enabled but rules_file is not specified")
		}
		resolvedPath := resolveRulesFilePath(config.ResponseValidation.RulesFile, configFileDir)
		responsePolicies, err := LoadCELResponsePoliciesFromFile(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("error loading CEL response policies from file: %w", err)
		}
		config.ResponseValidation.Rules = responsePolicies
	}

	// Load AI response policies from rules file
	if config.AIResponseValidation.Enabled {
		if config.AIResponseValidation.RulesFile == "" {
			return nil, fmt.Errorf("ai_response_validation is enabled but rules_file is not specified")
		}
		resolvedPath := resolveRulesFilePath(config.AIResponseValidation.RulesFile, configFileDir)
		aiResponsePolicies, err := LoadAIResponsePoliciesFromFile(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("error loading AI response policies from file: %w", err)
		}
		config.AIResponseValidation.Rules = aiResponsePolicies
	}

	// Normalize client configs - handle field aliases
	for name, client := range config.DownstreamMCPServers {
		// Handle URL alias
		if client.URL != "" && client.DownstreamURL == "" {
			client.DownstreamURL = client.URL
		}
		// Handle Args alias
		if len(client.Args) > 0 && len(client.CommandArgs) == 0 {
			client.CommandArgs = client.Args
		}
		config.DownstreamMCPServers[name] = client
	}

	// Set default values for client initialization configuration
	for name, client := range config.DownstreamMCPServers {
		if client.StartupTimeoutMs == 0 {
			client.StartupTimeoutMs = 30000 // 30 seconds
		}
		if client.InitializationRetries == 0 {
			client.InitializationRetries = 5
		}
		if client.RetryDelayMs == 0 {
			client.RetryDelayMs = 100 // 100ms base delay
		}

		// Set default values for capability discovery
		if client.CapabilityDiscoveryDelayMs == 0 {
			if client.Type == "stdio" {
				client.CapabilityDiscoveryDelayMs = 1000 // 1 second for stdio clients
			} else {
				client.CapabilityDiscoveryDelayMs = 0 // No delay for http/sse clients
			}
		}
		if client.CapabilityDiscoveryRetries == 0 {
			client.CapabilityDiscoveryRetries = 3
		}
		if client.CapabilityRetryDelayMs == 0 {
			client.CapabilityRetryDelayMs = 500 // 500ms delay between capability retries
		}
		config.DownstreamMCPServers[name] = client
	}

	// Validate config
	if err := ValidateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// ValidateConfig validates the configuration and collects all errors
func ValidateConfig(cfg *Config) error {
	var errors []string

	// Validate server type
	switch cfg.Server.Type {
	case ServerTypeSTDIO, ServerTypeHTTP, ServerTypeSSE:
		// Valid server type
	default:
		errors = append(errors, fmt.Sprintf("invalid server type: %s", cfg.Server.Type))
	}

	// Validate server configuration
	if cfg.Server.Type != ServerTypeSTDIO && cfg.Server.ListenAddr == "" {
		errors = append(errors, fmt.Sprintf("server.listen_addr is required for %s server type", cfg.Server.Type))
	}

	// Validate SSE server configuration if SSE type
	if cfg.Server.Type == ServerTypeSSE {
		if cfg.Server.SSE.TLS.Enabled {
			if cfg.Server.SSE.TLS.CertFile == "" {
				errors = append(errors, "server.sse.tls.cert_file is required when TLS is enabled")
			}
			if cfg.Server.SSE.TLS.KeyFile == "" {
				errors = append(errors, "server.sse.tls.key_file is required when TLS is enabled")
			}
		}
	}

	// Validate client configuration
	if len(cfg.DownstreamMCPServers) == 0 {
		errors = append(errors, "at least one downstream MCP server must be configured")
	}

	// Validate each client in the map
	for name, client := range cfg.DownstreamMCPServers {
		// Validate client type and required fields
		switch client.Type {
		case "stdio":
			if client.Command == "" {
				errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].command is required when type is stdio", name))
			}
		case "sse", "http":
			if client.DownstreamURL == "" {
				errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].downstream_url (or url) is required when type is %s", name, client.Type))
			}
		default:
			errors = append(errors, fmt.Sprintf("invalid client type for downstream_mcp_servers[%s]: %s", name, client.Type))
		}

		// Validate timeout/retry values
		if client.StartupTimeoutMs < 0 {
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].startup_timeout_ms must be non-negative", name))
		}
		if client.StartupTimeoutMs > 300000 { // 5 minutes max
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].startup_timeout_ms must be less than 300000ms (5 minutes)", name))
		}
		if client.InitializationRetries < 0 {
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].initialization_retries must be non-negative", name))
		}
		if client.InitializationRetries > 10 {
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].initialization_retries must be less than 10", name))
		}
		if client.RetryDelayMs < 0 {
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].retry_delay_ms must be non-negative", name))
		}
		if client.RetryDelayMs > 10000 { // 10 seconds max
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].retry_delay_ms must be less than 10000ms (10 seconds)", name))
		}
		if client.CapabilityDiscoveryDelayMs < 0 {
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].capability_discovery_delay_ms must be non-negative", name))
		}
		if client.CapabilityDiscoveryDelayMs > 60000 { // 1 minute max
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].capability_discovery_delay_ms must be less than 60000ms (1 minute)", name))
		}
		if client.CapabilityDiscoveryRetries < 0 {
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].capability_discovery_retries must be non-negative", name))
		}
		if client.CapabilityDiscoveryRetries > 10 {
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].capability_discovery_retries must be less than 10", name))
		}
		if client.CapabilityRetryDelayMs < 0 {
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].capability_retry_delay_ms must be non-negative", name))
		}
		if client.CapabilityRetryDelayMs > 30000 { // 30 seconds max
			errors = append(errors, fmt.Sprintf("downstream_mcp_servers[%s].capability_retry_delay_ms must be less than 30000ms (30 seconds)", name))
		}

		// Validate pass-through auth configuration
		if client.Auth.PassThrough.Enabled {
			// Only HTTP/SSE transports support pass-through auth
			if client.Type == "http" || client.Type == "sse" {
				// HTTP/SSE requires header mappings
				if len(client.Auth.PassThrough.Headers) == 0 {
					errors = append(errors,
						fmt.Sprintf("downstream_mcp_servers[%s]: pass_through enabled but no headers configured for %s transport",
							name, client.Type))
				}

				// Validate each header mapping
				for i, mapping := range client.Auth.PassThrough.Headers {
					if mapping.SourceHeader == "" {
						errors = append(errors,
							fmt.Sprintf("downstream_mcp_servers[%s].auth.pass_through.headers[%d]: source_header is required",
								name, i))
					}
					if mapping.TargetHeader == "" {
						errors = append(errors,
							fmt.Sprintf("downstream_mcp_servers[%s].auth.pass_through.headers[%d]: target_header is required",
								name, i))
					}
				}
			} else {
				errors = append(errors,
					fmt.Sprintf("downstream_mcp_servers[%s]: pass_through auth is only supported for http and sse transports, not %s",
						name, client.Type))
			}
		}
	}

	// Validate audit configuration
	if cfg.Audit.Path == "" {
		errors = append(errors, "audit.path is required")
	}

	// Validate AI validation configuration
	if cfg.AIPolicyValidation.Enabled {
		if cfg.AIPolicyValidation.APIKey == "" {
			errors = append(errors, "OPENAI_API_KEY environment variable is required when AI validation is enabled")
		}
		if cfg.AIPolicyValidation.Endpoint == "" {
			errors = append(errors, "ai_validation.endpoint is required when AI validation is enabled")
		}
		if cfg.AIPolicyValidation.Model == "" {
			errors = append(errors, "ai_validation.model is required when AI validation is enabled")
		}
	}

	// Return collected errors
	if len(errors) > 0 {
		errMsg := fmt.Sprintf("configuration validation failed with %d error(s):\n", len(errors))
		for i, err := range errors {
			errMsg += fmt.Sprintf("  %d. %s\n", i+1, err)
		}
		return fmt.Errorf("%s", errMsg)
	}

	return nil
}

// GetLogger creates a new session-aware logger based on the configuration
func GetLogger(cfg *Config) (*SessionLogger, error) {
	config := zap.NewProductionConfig()

	// Set log level
	level, err := zapcore.ParseLevel(cfg.Logging.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}
	config.Level = zap.NewAtomicLevelAt(level)

	// Set output paths based on configuration
	if cfg.Logging.Path != "" {
		// Use configured log file path
		config.OutputPaths = []string{cfg.Logging.Path}
		config.ErrorOutputPaths = []string{cfg.Logging.Path}
	} else {
		// Default to stdout/stderr if no path configured
		config.OutputPaths = []string{"stdout"}
		config.ErrorOutputPaths = []string{"stderr"}
	}

	// Build the logger
	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	// Add logger type designation and wrap in SessionLogger
	zapLogger := logger.With(zap.String("logger", "application"))
	return NewSessionLogger(zapLogger), nil
}

// GetAuditLogger creates a new session-aware audit logger based on the configuration
func GetAuditLogger(cfg *Config) (*SessionLogger, error) {
	config := zap.NewProductionConfig()

	// Set log level to info for audit logs
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)

	// Set output path to the configured audit path
	config.OutputPaths = []string{cfg.Audit.Path}
	config.ErrorOutputPaths = []string{cfg.Audit.Path}

	// Build the logger
	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build audit logger: %w", err)
	}

	// Add logger type designation and wrap in SessionLogger
	zapLogger := logger.With(zap.String("logger", "audit"))
	return NewSessionLogger(zapLogger), nil
}
