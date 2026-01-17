package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
)

// ServerType represents the type of server to run
type ServerType string

const (
	ServerTypeSTDIO ServerType = "stdio"
	ServerTypeHTTP  ServerType = "http"
	ServerTypeSSE   ServerType = "sse"
)

// PolicyAction represents the action to take when a policy matches
type PolicyAction string

const (
	PolicyActionAllow  PolicyAction = "allow"
	PolicyActionDeny   PolicyAction = "deny"
	PolicyActionRedact PolicyAction = "redact"
)

// PolicyMode represents the execution mode for a policy or policy group
type PolicyMode string

const (
	PolicyModeEnabled   PolicyMode = "enabled"    // Policy is enforced (default)
	PolicyModeAuditOnly PolicyMode = "audit_only" // Policy executes and is recorded, but doesn't affect final result
	PolicyModeDisabled  PolicyMode = "disabled"   // Policy is not executed
)

// IsValid returns true if the PolicyMode is a valid value
func (m PolicyMode) IsValid() bool {
	return m == PolicyModeEnabled || m == PolicyModeAuditOnly || m == PolicyModeDisabled || m == ""
}

// RotationConfig contains log rotation settings for both logger and audit
type RotationConfig struct {
	MaxSizeMB  int  `mapstructure:"max_size_mb"`  // Max size in MB before rotation (default: 100)
	MaxBackups int  `mapstructure:"max_backups"`  // Max number of rotated files to keep (default: 5)
	MaxAgeDays int  `mapstructure:"max_age_days"` // Max days before deleting rotated files (default: 180, 0 = no limit)
	Compress   bool `mapstructure:"compress"`     // Gzip compress rotated files (default: true)
}

// newLumberjackLogger creates a lumberjack logger with the given rotation config
func newLumberjackLogger(path string, rotationCfg RotationConfig) io.WriteCloser {
	return &lumberjack.Logger{
		Filename:   path,
		MaxSize:    rotationCfg.MaxSizeMB,
		MaxBackups: rotationCfg.MaxBackups,
		MaxAge:     rotationCfg.MaxAgeDays,
		Compress:   rotationCfg.Compress,
	}
}

// ResolveValidationMode resolves the effective mode for a validation config section.
// Priority: Mode field > Enabled field (deprecated) > defaultMode parameter
// Each validation section can have its own default mode.
func ResolveValidationMode(mode PolicyMode, enabled *bool, defaultMode PolicyMode) PolicyMode {
	// If explicit mode is set, use it
	if mode != "" {
		return mode
	}
	// If deprecated enabled field is set, convert to mode
	if enabled != nil {
		if *enabled {
			return PolicyModeEnabled
		}
		return PolicyModeDisabled
	}
	// Use the provided default
	return defaultMode
}

// ResolvePolicyMode resolves the effective mode for an individual policy.
// Per-rule mode overrides top-level default mode.
func ResolvePolicyMode(ruleMode PolicyMode, defaultMode PolicyMode) PolicyMode {
	if ruleMode != "" {
		return ruleMode
	}
	return defaultMode
}

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
		// SessionTimeoutMinutes is the idle timeout for sessions in minutes.
		// Sessions inactive for longer than this will be cleaned up.
		// Default: 30 minutes. Set to 0 to disable session timeout (not recommended).
		SessionTimeoutMinutes int `mapstructure:"session_timeout_minutes"`
	} `mapstructure:"server"`

	// Global validation settings that apply to all validation phases
	Validation struct {
		MaxBlockingMs       int `mapstructure:"max_blocking_ms"`        // Max cumulative time to block request waiting for decisions across all phases (default: 90000ms)
		MaxRuleEvaluationMs int `mapstructure:"max_rule_evaluation_ms"` // Max time for any single rule to complete (default: 45000ms)
		// AI settings shared by all AI-powered validation (request, response) and AI tools (audit report)
		AI struct {
			Endpoint string `mapstructure:"endpoint"` // OpenAI-compatible API endpoint
			Model    string `mapstructure:"model"`    // Model to use for AI validation
			APIKey   string `mapstructure:"api_key"`  // API key for AI endpoint
		} `mapstructure:"ai"`
	} `mapstructure:"validation"`

	// Request validation configuration (deterministic rules)
	RequestValidation struct {
		Enabled   *bool      `mapstructure:"enabled"` // Deprecated: use Mode instead
		Mode      PolicyMode `mapstructure:"mode"`    // enabled, audit_only, disabled (default: enabled)
		RulesFile string     `mapstructure:"rules_file"`
		Rules     []Policy   `mapstructure:"rules"`
	} `mapstructure:"request_validation"`

	// AI request validation configuration
	AIRequestValidation struct {
		Enabled   *bool      `mapstructure:"enabled"` // Deprecated: use Mode instead
		Mode      PolicyMode `mapstructure:"mode"`    // enabled, audit_only, disabled (default: audit_only)
		RulesFile string     `mapstructure:"rules_file"`
		Rules     []AIPolicy `mapstructure:"rules"`
	} `mapstructure:"ai_request_validation"`

	// Response validation configuration (deterministic rules)
	ResponseValidation struct {
		Enabled   *bool            `mapstructure:"enabled"` // Deprecated: use Mode instead
		Mode      PolicyMode       `mapstructure:"mode"`    // enabled, audit_only, disabled (default: disabled)
		RulesFile string           `mapstructure:"rules_file"`
		Rules     []ResponsePolicy `mapstructure:"rules"`
	} `mapstructure:"response_validation"`

	// AI response validation configuration
	AIResponseValidation struct {
		Enabled   *bool              `mapstructure:"enabled"` // Deprecated: use Mode instead
		Mode      PolicyMode         `mapstructure:"mode"`    // enabled, audit_only, disabled (default: disabled)
		RulesFile string             `mapstructure:"rules_file"`
		Rules     []AIResponsePolicy `mapstructure:"rules"`
	} `mapstructure:"ai_response_validation"`

	// Downstream MCP servers configuration
	DownstreamMCPServers map[string]ClientConfig `mapstructure:"downstream_mcp_servers"`

	// Audit configuration
	Audit struct {
		Path   string `mapstructure:"path"`   // Default: maybedont-audit.log (resolved in log-dir), or stdout/stderr
		Filter string `mapstructure:"filter"` // "all" (default) or "deny_only"

		// Log rotation settings (only applicable when path is a filename)
		Rotation RotationConfig `mapstructure:"rotation"`
	} `mapstructure:"audit"`

	// Logger configuration (application logs)
	Logger struct {
		Level string `mapstructure:"level"`
		Path  string `mapstructure:"path"` // Default: stderr, or filename (resolved in log-dir)

		// Log rotation settings (only applicable when path is a filename)
		Rotation RotationConfig `mapstructure:"rotation"`
	} `mapstructure:"logger"`

	// NativeTools configuration for gateway-native tools
	NativeTools struct {
		AuditLog struct {
			Enabled    bool `mapstructure:"enabled"`
			MaxEntries int  `mapstructure:"max_entries"`
		} `mapstructure:"audit_log"`

		AuditReport struct {
			Enabled      bool   `mapstructure:"enabled"`
			MaxEntries   int    `mapstructure:"max_entries"`
			SystemPrompt string `mapstructure:"system_prompt"`
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

// Policy represents a single deterministic policy rule (uses CEL expressions internally)
type Policy struct {
	Name        string       `mapstructure:"name"`
	Description string       `mapstructure:"description"`
	Expression  string       `mapstructure:"expression"`
	Action      PolicyAction `mapstructure:"action"` // allow or deny
	Message     string       `mapstructure:"message"`
	Mode        PolicyMode   `mapstructure:"mode"` // Optional: overrides top-level mode
}

// AIPolicy represents a single AI policy rule
type AIPolicy struct {
	Name        string       `mapstructure:"name"`
	Description string       `mapstructure:"description"`
	Prompt      string       `mapstructure:"prompt"`
	Action      PolicyAction `mapstructure:"action"` // allow or deny
	Message     string       `mapstructure:"message"`
	Mode        PolicyMode   `mapstructure:"mode"` // Optional: overrides top-level mode
}

// ResponsePolicy represents a single deterministic response policy rule (uses CEL expressions internally)
type ResponsePolicy struct {
	Name                 string       `mapstructure:"name"`
	Description          string       `mapstructure:"description"`
	Expression           string       `mapstructure:"expression"`
	Action               PolicyAction `mapstructure:"action"` // allow, deny, or redact
	Message              string       `mapstructure:"message"`
	RedactionPattern     string       `mapstructure:"redaction_pattern"`
	RedactionReplacement string       `mapstructure:"redaction_replacement"`
	Mode                 PolicyMode   `mapstructure:"mode"` // Optional: overrides top-level mode
}

// AIResponsePolicy represents a single AI response policy rule
type AIResponsePolicy struct {
	Name        string       `mapstructure:"name"`
	Description string       `mapstructure:"description"`
	Prompt      string       `mapstructure:"prompt"`
	Action      PolicyAction `mapstructure:"action"` // allow, deny, or redact
	Message     string       `mapstructure:"message"`
	Mode        PolicyMode   `mapstructure:"mode"` // Optional: overrides top-level mode
}

// LoadPoliciesFromFile loads deterministic policies from a file
func LoadPoliciesFromFile(rulesFile string) ([]Policy, error) {
	if rulesFile == "" {
		return nil, nil
	}

	data, err := os.ReadFile(rulesFile)
	if err != nil {
		return nil, fmt.Errorf("error reading rules file: %w", err)
	}

	var policies struct {
		Rules []Policy `yaml:"rules"`
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

	// the action is optional, and default is deny.
	for i := range policies.Rules {
		if policies.Rules[i].Action == "" {
			policies.Rules[i].Action = PolicyActionDeny
		}
	}

	return policies.Rules, nil
}

// LoadResponsePoliciesFromFile loads deterministic response policies from a file
func LoadResponsePoliciesFromFile(rulesFile string) ([]ResponsePolicy, error) {
	if rulesFile == "" {
		return nil, nil
	}

	data, err := os.ReadFile(rulesFile)
	if err != nil {
		return nil, fmt.Errorf("error reading response rules file: %w", err)
	}

	var policies struct {
		Rules []ResponsePolicy `yaml:"rules"`
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

// applyEnvironmentOverrides walks through the config struct and overrides fields
// with values from environment variables using the specified prefix.
// For example, with envPrefix="MAYBE_DONT", ai_validation.api_key can be set via MAYBE_DONT_AI_VALIDATION_API_KEY.
// This provides a general mechanism for Docker/container deployments without hardcoding bindings.
//
// Supported types:
//   - string: set directly from env var value
//   - bool: parsed from "true"/"false" (case-insensitive)
//   - int, int64: parsed as base-10 integers
//   - float64: parsed as floating point numbers
//   - []string: parsed as comma-separated values (e.g., "a,b,c" -> ["a", "b", "c"])
//   - nested structs: recursively processed
func applyEnvironmentOverrides(v reflect.Value, t reflect.Type, pathPrefix string, envPrefix string) {
	if !v.IsValid() {
		return
	}

	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			fieldType := t.Field(i)

			if !field.CanSet() {
				continue
			}

			// Get the mapstructure tag to determine the config key name
			tag := fieldType.Tag.Get("mapstructure")
			if tag == "" {
				continue
			}

			// Build the full path for this field
			var fullPath string
			if pathPrefix == "" {
				fullPath = tag
			} else {
				fullPath = pathPrefix + "_" + tag
			}

			envKey := envPrefix + "_" + strings.ToUpper(fullPath)
			envVal := os.Getenv(envKey)

			// Only process if environment variable is set
			if envVal == "" {
				// Still need to recurse into nested structs
				if field.Kind() == reflect.Struct {
					applyEnvironmentOverrides(field, fieldType.Type, fullPath, envPrefix)
				} else if field.Kind() == reflect.Ptr && !field.IsNil() {
					applyEnvironmentOverrides(field.Elem(), fieldType.Type.Elem(), fullPath, envPrefix)
				}
				continue
			}

			// Apply the environment variable based on field type
			switch field.Kind() {
			case reflect.String:
				field.SetString(envVal)

			case reflect.Bool:
				if boolVal, err := strconv.ParseBool(envVal); err == nil {
					field.SetBool(boolVal)
				}

			case reflect.Int:
				if intVal, err := strconv.ParseInt(envVal, 10, 0); err == nil {
					field.SetInt(intVal)
				}

			case reflect.Int64:
				if intVal, err := strconv.ParseInt(envVal, 10, 64); err == nil {
					field.SetInt(intVal)
				}

			case reflect.Float64:
				if floatVal, err := strconv.ParseFloat(envVal, 64); err == nil {
					field.SetFloat(floatVal)
				}

			case reflect.Slice:
				// Handle []string slices with comma-separated values
				if field.Type().Elem().Kind() == reflect.String {
					// Split by comma and trim whitespace from each element
					parts := strings.Split(envVal, ",")
					result := make([]string, 0, len(parts))
					for _, part := range parts {
						trimmed := strings.TrimSpace(part)
						if trimmed != "" {
							result = append(result, trimmed)
						}
					}
					field.Set(reflect.ValueOf(result))
				}

			case reflect.Struct:
				// Recursively process nested structs
				applyEnvironmentOverrides(field, fieldType.Type, fullPath, envPrefix)
			}
		}

	case reflect.Ptr:
		if !v.IsNil() {
			applyEnvironmentOverrides(v.Elem(), t.Elem(), pathPrefix, envPrefix)
		}
	}
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

// ResolveConfigDir resolves the configuration directory with fallback logic.
// Priority: 1) provided dir, 2) ./config (if exists), 3) $HOME/.maybe-dont/config (if exists), 4) current directory
func ResolveConfigDir(configDir string) string {
	if configDir != "" {
		return configDir
	}

	// Check ./config first
	if info, err := os.Stat("./config"); err == nil && info.IsDir() {
		return "./config"
	}

	// Fall back to $HOME/.maybe-dont/config only if it exists
	homeDir, err := os.UserHomeDir()
	if err == nil {
		homeCfgDir := filepath.Join(homeDir, ".maybe-dont", "config")
		if info, err := os.Stat(homeCfgDir); err == nil && info.IsDir() {
			return homeCfgDir
		}
	}

	// Last resort: current directory
	return "."
}

// ResolveLogDir resolves the log directory.
// If logDir is provided, it is used directly.
// Otherwise, the log directory defaults to a "logs" subdirectory within the config directory.
// For example, if configDir is "./config", log-dir defaults to "./config/logs".
// If configDir is "$HOME/.maybe-dont", log-dir defaults to "$HOME/.maybe-dont/logs".
func ResolveLogDir(logDir, configDir string) string {
	if logDir != "" {
		return logDir
	}

	// Default to logs subdirectory within config directory
	return filepath.Join(configDir, "logs")
}

// LoadConfig loads the configuration from all sources.
// configDir: directory containing config files (resolved via ResolveConfigDir if empty)
// configFileName: name of config file (defaults to "maybedont.yaml", falls back to "gateway-config.yaml")
//
// Configuration can be provided via:
// 1. A YAML config file (maybedont.yaml or gateway-config.yaml)
// 2. Environment variables with MAYBE_DONT_ prefix (e.g., MAYBE_DONT_SERVER_TYPE)
// 3. A combination of both (environment variables override config file values)
//
// A config file is NOT required if all necessary values are provided via environment variables.
func LoadConfig(configDir, configFileName string) (*Config, error) {
	// Use the global viper instance to ensure flag bindings work
	v := viper.GetViper()

	// Set environment variable prefix
	v.SetEnvPrefix("MAYBE_DONT")
	v.AutomaticEnv()

	// Set up environment variable key mappings for nested map structures
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Resolve config directory
	resolvedConfigDir := ResolveConfigDir(configDir)

	// Set config type
	v.SetConfigType("yaml")

	// Add config path
	v.AddConfigPath(resolvedConfigDir)

	// Set defaults before reading config
	// Please note that if you add additional defaults, be sure to add the use case to TestViperConfigPathsMatchStruct.
	v.SetDefault("native_tools.audit_log.enabled", true)
	v.SetDefault("native_tools.audit_report.enabled", true)
	v.SetDefault("native_tools.list_servers.enabled", true)
	v.SetDefault("native_tools.list_sessions.enabled", true)
	v.SetDefault("native_tools.audit_log.max_entries", 100)
	v.SetDefault("native_tools.audit_report.max_entries", 1_000)
	v.SetDefault("logger.path", "stderr")
	v.SetDefault("logger.level", "info")
	v.SetDefault("logger.rotation.max_size_mb", 100)
	v.SetDefault("logger.rotation.max_backups", 5)
	v.SetDefault("logger.rotation.max_age_days", 180)
	v.SetDefault("logger.rotation.compress", true)
	v.SetDefault("audit.path", "maybedont-audit.log")
	v.SetDefault("audit.filter", "all")
	v.SetDefault("audit.rotation.max_size_mb", 100)
	v.SetDefault("audit.rotation.max_backups", 5)
	v.SetDefault("audit.rotation.max_age_days", 180)
	v.SetDefault("audit.rotation.compress", true)
	v.SetDefault("validation.max_blocking_ms", 90_000)
	v.SetDefault("validation.max_rule_evaluation_ms", 45_000)
	v.SetDefault("server.session_timeout_minutes", 30)

	// Try to find config file with fallback logic
	configFileFound := false

	if configFileName != "" {
		// User specified a config file name - use it directly
		// Strip .yaml/.yml extension if present for viper
		baseName := strings.TrimSuffix(strings.TrimSuffix(configFileName, ".yaml"), ".yml")
		v.SetConfigName(baseName)
		if err := v.ReadInConfig(); err == nil {
			configFileFound = true
		}
	} else {
		// Try maybedont.yaml first, then fall back to gateway-config.yaml (deprecated)
		v.SetConfigName("maybedont")
		if err := v.ReadInConfig(); err == nil {
			configFileFound = true
		} else {
			// Fall back to deprecated config file - gateway-config.yaml
			v.SetConfigName("gateway-config")
			if err := v.ReadInConfig(); err == nil {
				configFileFound = true

				// Warn the user if they are using a deprecated config file
				fmt.Printf("Filename gateway-config.yaml is deprecated, rename config file to maybedont.yaml\n")
			}
		}
	}

	// Get the directory for resolving relative rules file paths
	// If a config file was found, use its directory; otherwise use the resolved config directory
	var configFileDir string
	if configFileFound {
		configFileDir = filepath.Dir(v.ConfigFileUsed())
	} else {
		configFileDir = resolvedConfigDir
	}

	// Unmarshal config (viper will use defaults and env vars even without a config file)
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		if configFileFound {
			return nil, fmt.Errorf("error unmarshaling config from %s: %w", v.ConfigFileUsed(), err)
		}
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Expand environment variables in all string fields (handles ${VAR} syntax in config values)
	expandEnvironmentVariables(reflect.ValueOf(&config).Elem())

	// Apply environment variable overrides using viper's configured prefix
	// This allows any config field to be set via environment variable, e.g.:
	// MAYBE_DONT_AI_VALIDATION_API_KEY, MAYBE_DONT_SERVER_LISTEN_ADDR, etc.
	applyEnvironmentOverrides(reflect.ValueOf(&config).Elem(), reflect.TypeOf(config), "", v.GetEnvPrefix())

	// Set default server type to stdio if not configured
	if config.Server.Type == "" {
		config.Server.Type = ServerTypeSTDIO
	}

	// Set default listen address to 127.0.0.1 for non-stdio servers if not set
	if config.Server.Type != ServerTypeSTDIO && config.Server.ListenAddr == "" {
		config.Server.ListenAddr = "127.0.0.1"
	}

	// Set default values for native tools configuration
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

	// Resolve and store validation modes with their respective defaults
	// Request validation defaults to enabled
	config.RequestValidation.Mode = ResolveValidationMode(
		config.RequestValidation.Mode, config.RequestValidation.Enabled, PolicyModeEnabled)

	// AI request validation defaults to audit_only (non-blocking by default)
	config.AIRequestValidation.Mode = ResolveValidationMode(
		config.AIRequestValidation.Mode, config.AIRequestValidation.Enabled, PolicyModeAuditOnly)

	// Response validation defaults to disabled
	config.ResponseValidation.Mode = ResolveValidationMode(
		config.ResponseValidation.Mode, config.ResponseValidation.Enabled, PolicyModeDisabled)

	// AI response validation defaults to disabled
	config.AIResponseValidation.Mode = ResolveValidationMode(
		config.AIResponseValidation.Mode, config.AIResponseValidation.Enabled, PolicyModeDisabled)

	// Collect errors from loading policy rules files
	// These are collected here so they can be reported alongside validation errors
	var loadErrors []string

	// Load request policies from rules file (if mode is not disabled)
	if config.RequestValidation.Mode != PolicyModeDisabled {
		if config.RequestValidation.RulesFile == "" {
			loadErrors = append(loadErrors, "request_validation is enabled but rules_file is not specified")
		} else if err := ValidateRelativePath(config.RequestValidation.RulesFile); err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("request_validation.rules_file: %s", err.Error()))
		} else {
			resolvedPath := resolveRulesFilePath(config.RequestValidation.RulesFile, configFileDir)
			policies, err := LoadPoliciesFromFile(resolvedPath)
			if err != nil {
				loadErrors = append(loadErrors, fmt.Sprintf("error loading request policies from file: %v", err))
			} else {
				config.RequestValidation.Rules = policies
			}
		}
	}

	// Load AI request policies from rules file (if mode is not disabled)
	if config.AIRequestValidation.Mode != PolicyModeDisabled {
		if config.AIRequestValidation.RulesFile == "" {
			loadErrors = append(loadErrors, "ai_request_validation is enabled but rules_file is not specified")
		} else if err := ValidateRelativePath(config.AIRequestValidation.RulesFile); err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("ai_request_validation.rules_file: %s", err.Error()))
		} else {
			resolvedPath := resolveRulesFilePath(config.AIRequestValidation.RulesFile, configFileDir)
			aiPolicies, err := LoadAIPoliciesFromFile(resolvedPath)
			if err != nil {
				loadErrors = append(loadErrors, fmt.Sprintf("error loading AI request policies from file: %v", err))
			} else {
				config.AIRequestValidation.Rules = aiPolicies
			}
		}
	}

	// Load response policies from rules file (if mode is not disabled)
	if config.ResponseValidation.Mode != PolicyModeDisabled {
		if config.ResponseValidation.RulesFile == "" {
			loadErrors = append(loadErrors, "response_validation is enabled but rules_file is not specified")
		} else if err := ValidateRelativePath(config.ResponseValidation.RulesFile); err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("response_validation.rules_file: %s", err.Error()))
		} else {
			resolvedPath := resolveRulesFilePath(config.ResponseValidation.RulesFile, configFileDir)
			responsePolicies, err := LoadResponsePoliciesFromFile(resolvedPath)
			if err != nil {
				loadErrors = append(loadErrors, fmt.Sprintf("error loading response policies from file: %v", err))
			} else {
				config.ResponseValidation.Rules = responsePolicies
			}
		}
	}

	// Load AI response policies from rules file (if mode is not disabled)
	if config.AIResponseValidation.Mode != PolicyModeDisabled {
		if config.AIResponseValidation.RulesFile == "" {
			loadErrors = append(loadErrors, "ai_response_validation is enabled but rules_file is not specified")
		} else if err := ValidateRelativePath(config.AIResponseValidation.RulesFile); err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("ai_response_validation.rules_file: %s", err.Error()))
		} else {
			resolvedPath := resolveRulesFilePath(config.AIResponseValidation.RulesFile, configFileDir)
			aiResponsePolicies, err := LoadAIResponsePoliciesFromFile(resolvedPath)
			if err != nil {
				loadErrors = append(loadErrors, fmt.Sprintf("error loading AI response policies from file: %v", err))
			} else {
				config.AIResponseValidation.Rules = aiResponsePolicies
			}
		}
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

	// Validate config with context about whether a config file was found
	if err := validateConfigWithOptions(&config, configFileFound, loadErrors); err != nil {
		return nil, err
	}

	return &config, nil
}

// ValidationContext provides additional context for configuration validation
type ValidationContext struct {
	ConfigFileFound bool
}

// ValidateConfig validates the configuration and collects all errors.
// This is a convenience wrapper around ValidateConfigWithContext that assumes
// a config file was found (for backwards compatibility with tests).
func ValidateConfig(cfg *Config) error {
	return ValidateConfigWithContext(cfg, true)
}

// ValidateConfigWithContext validates the configuration and collects all errors.
// The configFileFound parameter indicates whether a config file was successfully loaded.
// If false and validation fails, additional guidance is provided about using environment variables.
func ValidateConfigWithContext(cfg *Config, configFileFound bool) error {
	return validateConfigWithOptions(cfg, configFileFound, nil)
}

// validateConfigWithOptions is the internal implementation that validates the configuration,
// collects all errors (including any pre-existing load errors), and provides contextual guidance.
func validateConfigWithOptions(cfg *Config, configFileFound bool, loadErrors []string) error {
	// Start with any errors from loading policy files
	var errors []string
	errors = append(errors, loadErrors...)

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

	// Validate AI configuration when any AI feature is enabled
	// AI credentials are required when AI request validation, AI response validation, or audit report is enabled
	aiRequestEnabled := cfg.AIRequestValidation.Mode != PolicyModeDisabled && cfg.AIRequestValidation.Mode != ""
	aiResponseEnabled := cfg.AIResponseValidation.Mode != PolicyModeDisabled && cfg.AIResponseValidation.Mode != ""
	auditReportEnabled := cfg.NativeTools.AuditReport.Enabled

	if aiRequestEnabled || aiResponseEnabled || auditReportEnabled {
		if cfg.Validation.AI.APIKey == "" {
			errors = append(errors, "validation.ai.api_key is required when AI validation or audit report is enabled")
		}
		if cfg.Validation.AI.Endpoint == "" {
			errors = append(errors, "validation.ai.endpoint is required when AI validation or audit report is enabled")
		}
		if cfg.Validation.AI.Model == "" {
			errors = append(errors, "validation.ai.model is required when AI validation or audit report is enabled")
		}
	}

	// Validate global validation timeout values
	if cfg.Validation.MaxBlockingMs < 0 {
		errors = append(errors, "validation.max_blocking_ms must be non-negative")
	}
	if cfg.Validation.MaxBlockingMs > 120000 {
		errors = append(errors, "validation.max_blocking_ms must be less than 120000ms (2 minutes)")
	}
	if cfg.Validation.MaxRuleEvaluationMs < 0 {
		errors = append(errors, "validation.max_rule_evaluation_ms must be non-negative")
	}
	if cfg.Validation.MaxRuleEvaluationMs > 120000 {
		errors = append(errors, "validation.max_rule_evaluation_ms must be less than 120000ms (2 minutes)")
	}

	// Validate native tools
	if cfg.NativeTools.AuditLog.Enabled {
		validateRange(cfg.NativeTools.AuditLog.MaxEntries, 10, 500, "native_tools.audit_log.max_entries", &errors)
	}

	if cfg.NativeTools.AuditReport.Enabled {
		validateRange(cfg.NativeTools.AuditReport.MaxEntries, 10, 2_000, "native_tools.audit_report.max_entries", &errors)
	}

	// Validate logger.path - must be stdout, stderr, or a safe relative path
	if cfg.Logger.Path != "" && cfg.Logger.Path != "stdout" && cfg.Logger.Path != "stderr" {
		if err := ValidateRelativePath(cfg.Logger.Path); err != nil {
			errors = append(errors, fmt.Sprintf("logger.path: %s", err.Error()))
		}
	}

	// Validate audit.path - must be stdout, stderr, or a safe relative path
	if cfg.Audit.Path != "" && cfg.Audit.Path != "stdout" && cfg.Audit.Path != "stderr" {
		if err := ValidateRelativePath(cfg.Audit.Path); err != nil {
			errors = append(errors, fmt.Sprintf("audit.path: %s", err.Error()))
		}
	}

	// Return collected errors with contextual guidance
	if len(errors) > 0 {
		errMsg := fmt.Sprintf("configuration validation failed with %d error(s):\n", len(errors))
		for i, err := range errors {
			errMsg += fmt.Sprintf("  %d. %s\n", i+1, err)
		}

		// Add guidance about config file status and environment variables
		if !configFileFound {
			errMsg += "\nNote: No configuration file was found. This is acceptable if you intend to configure\n"
			errMsg += "the gateway entirely via environment variables (MAYBE_DONT_* prefix).\n"
			errMsg += "For example:\n"
			errMsg += "  - MAYBE_DONT_SERVER_TYPE=stdio\n"
			errMsg += "  - MAYBE_DONT_AI_VALIDATION_API_KEY=your-api-key\n"
			errMsg += "\nAlternatively, create a config file (maybedont.yaml) in one of these locations:\n"
			errMsg += "  - ./config/\n"
			errMsg += "  - ~/.maybe-dont/config/\n"
			errMsg += "  - Current directory\n"
		}
		return fmt.Errorf("%s", errMsg)
	}

	return nil
}

// GetLogger creates a new session-aware logger based on the configuration.
// logDir: the resolved log directory where log files should be written.
// If logger.path is "stdout" or "stderr", logs go directly there.
// Otherwise, logger.path is treated as a filename and resolved within logDir with rotation support.
func GetLogger(cfg *Config, logDir string) (*SessionLogger, error) {
	// Set log level
	level, err := zapcore.ParseLevel(cfg.Logger.Level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	// Set output based on configuration
	logPath := cfg.Logger.Path

	var core zapcore.Core
	encoderConfig := zap.NewProductionEncoderConfig()
	encoder := zapcore.NewJSONEncoder(encoderConfig)

	switch logPath {
	case "", "stderr":
		// Default to stderr
		core = zapcore.NewCore(encoder, zapcore.AddSync(os.Stderr), level)
	case "stdout":
		// Log to stdout
		core = zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)
	default:
		// Path is a filename - resolve it within logDir and use lumberjack for rotation
		fullPath := filepath.Join(logDir, logPath)
		lumberjackLogger := newLumberjackLogger(fullPath, cfg.Logger.Rotation)
		core = zapcore.NewCore(encoder, zapcore.AddSync(lumberjackLogger), level)
	}

	// Build the logger
	logger := zap.New(core)

	// Add logger type designation and wrap in SessionLogger
	zapLogger := logger.With(zap.String("logger", "application"))
	return NewSessionLogger(zapLogger), nil
}

// GetAuditLogger creates a new session-aware audit logger based on the configuration.
// logDir: the resolved log directory where the audit log file should be written.
// If audit.path is "stdout" or "stderr", logs go directly there.
// Otherwise, audit.path is treated as a filename and resolved within logDir.
func GetAuditLogger(cfg *Config, logDir string) (*SessionLogger, error) {
	config := zap.NewProductionConfig()

	// Set log level to info for audit logs
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)

	// Set output path based on configuration
	auditPath := cfg.Audit.Path
	switch auditPath {
	case "stdout":
		config.OutputPaths = []string{"stdout"}
		config.ErrorOutputPaths = []string{"stderr"}
	case "stderr":
		config.OutputPaths = []string{"stderr"}
		config.ErrorOutputPaths = []string{"stderr"}
	default:
		// Path is a filename - resolve it within logDir
		// Default to "maybedont-audit.log" if empty
		if auditPath == "" {
			auditPath = "maybedont-audit.log"
		}
		fullPath := filepath.Join(logDir, auditPath)
		config.OutputPaths = []string{fullPath}
		config.ErrorOutputPaths = []string{fullPath}
	}

	// Build the logger
	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build audit logger: %w", err)
	}

	// Add logger type designation and wrap in SessionLogger
	zapLogger := logger.With(zap.String("logger", "audit"))
	return NewSessionLogger(zapLogger), nil
}

func validateRange(value, min, max int, fieldName string, errors *[]string) {
	if value < min || value > max {
		*errors = append(*errors, fmt.Sprintf("%s is invalid. The value must be >= %d and <= %d", fieldName, min, max))
	}
}

// ValidateRelativePath validates that a path is safe for use as a relative path within a base directory.
// It allows subdirectories (e.g., "logs/audit.log") but prevents path traversal attacks.
//
// The function checks for:
// - Absolute paths (starting with / or drive letters on Windows)
// - Parent directory references (..)
// - Hidden files/directories (starting with .)
// - Null bytes and control characters
// - URL-encoded traversal attempts (%2e, %2f, etc.)
// - Unicode normalization attacks
// - Backslash usage (even on Unix, for consistency)
// - Empty path components (e.g., "foo//bar")
//
// Returns nil if the path is safe, or an error describing the issue.
func ValidateRelativePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Check for null bytes and control characters (can be used to bypass validation)
	for i, c := range path {
		if c == 0 {
			return fmt.Errorf("path contains null byte at position %d", i)
		}
		if c < 32 && c != '\t' {
			return fmt.Errorf("path contains control character at position %d", i)
		}
	}

	// Check for URL-encoded characters that could be used for traversal
	// Common encodings: %2e = '.', %2f = '/', %5c = '\', %00 = null
	lowerPath := strings.ToLower(path)
	encodedPatterns := []string{
		"%2e",    // .
		"%2f",    // /
		"%5c",    // \
		"%00",    // null
		"%252e",  // double-encoded .
		"%252f",  // double-encoded /
		"%c0%ae", // overlong UTF-8 encoding of .
		"%c0%af", // overlong UTF-8 encoding of /
		"%c1%9c", // overlong UTF-8 encoding of \
	}
	for _, pattern := range encodedPatterns {
		if strings.Contains(lowerPath, pattern) {
			return fmt.Errorf("path contains potentially malicious URL encoding: %s", pattern)
		}
	}

	// Normalize the path to handle any platform-specific quirks
	// filepath.Clean will normalize separators and collapse redundant components
	cleanPath := filepath.Clean(path)

	// Check for absolute paths
	if filepath.IsAbs(cleanPath) {
		return fmt.Errorf("absolute paths are not allowed")
	}

	// After cleaning, check if the path tries to escape the base directory
	// filepath.Clean converts ".." sequences, so we check the cleaned result
	if strings.HasPrefix(cleanPath, "..") {
		return fmt.Errorf("path cannot reference parent directory")
	}

	// Split the path and validate each component
	// Use both / and \ as separators to handle cross-platform issues
	// Replace backslashes with forward slashes for consistent handling
	normalizedPath := strings.ReplaceAll(path, "\\", "/")
	components := strings.Split(normalizedPath, "/")

	for i, component := range components {
		// Check for empty components (indicates double slashes or trailing slashes)
		if component == "" {
			if i == 0 {
				return fmt.Errorf("path cannot start with a separator")
			}
			// Allow trailing empty component (trailing slash)
			if i == len(components)-1 {
				continue
			}
			return fmt.Errorf("path contains empty component (consecutive separators)")
		}

		// Check for parent directory reference
		if component == ".." {
			return fmt.Errorf("path cannot contain '..' (parent directory reference)")
		}

		// Check for current directory reference (usually harmless but unnecessary)
		if component == "." {
			return fmt.Errorf("path cannot contain '.' (current directory reference)")
		}

		// Check for hidden files/directories (starting with .)
		if strings.HasPrefix(component, ".") {
			return fmt.Errorf("path cannot contain hidden files or directories (starting with '.')")
		}

		// Check for multiple consecutive dots (potential traversal variant)
		if strings.Contains(component, "...") {
			return fmt.Errorf("path contains suspicious dot sequence")
		}

		// Check for whitespace at start/end of component (can be used to bypass checks)
		if strings.TrimSpace(component) != component {
			return fmt.Errorf("path component cannot have leading or trailing whitespace")
		}

		// Check for Windows-specific issues
		// Colon is used for drive letters and NTFS alternate data streams
		if strings.Contains(component, ":") {
			return fmt.Errorf("path cannot contain colons")
		}
	}

	return nil
}

// ResolveAuditLogPath returns the full path to the audit log file.
// If audit.path is "stdout" or "stderr", it returns empty string (log goes to stdout/stderr).
// Otherwise, it returns the path resolved within logDir.
func ResolveAuditLogPath(cfg *Config, logDir string) string {
	auditPath := cfg.Audit.Path
	if auditPath == "stdout" || auditPath == "stderr" {
		return "" // Audit goes to stdout/stderr, no file path
	}
	if auditPath == "" {
		auditPath = "maybedont-audit.log"
	}
	return filepath.Join(logDir, auditPath)
}
