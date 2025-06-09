package config

import (
	"fmt"
	"os"

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
		LogLevel   string     `mapstructure:"log_level"`
		LogFormat  string     `mapstructure:"log_format"`
		SSE        struct {
			TLS struct {
				Enabled  bool   `mapstructure:"enabled"`
				CertFile string `mapstructure:"cert_file"`
				KeyFile  string `mapstructure:"key_file"`
			} `mapstructure:"tls"`
		} `mapstructure:"sse"`
	} `mapstructure:"server"`

	// Authentication configuration
	Auth struct {
		Type      string `mapstructure:"type"` // api_key, jwt, mtls
		APIKey    string `mapstructure:"api_key"`
		JWTConfig struct {
			JWKSUrl    string   `mapstructure:"jwks_url"`
			Issuer     string   `mapstructure:"issuer"`
			Audience   []string `mapstructure:"audience"`
			ClaimRoles string   `mapstructure:"claim_roles"`
		} `mapstructure:"jwt"`
		MTLSConfig struct {
			CAFile   string `mapstructure:"ca_file"`
			CertFile string `mapstructure:"cert_file"`
			KeyFile  string `mapstructure:"key_file"`
		} `mapstructure:"mtls"`
	} `mapstructure:"auth"`

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

	// Client configuration
	Client struct {
		Type          string   `mapstructure:"type"` // stdio, sse, http
		DownstreamURL string   `mapstructure:"downstream_url"`
		Command       string   `mapstructure:"command"`
		CommandArgs   []string `mapstructure:"command_args"`
		SSEConfig     struct {
			Headers map[string]string `mapstructure:"headers"`
		} `mapstructure:"sse"`
	} `mapstructure:"client"`

	// Audit configuration
	Audit struct {
		Path   string `mapstructure:"path"`
		Format string `mapstructure:"format"` // json or text
	} `mapstructure:"audit"`
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

// LoadConfig loads the configuration from all sources
func LoadConfig(configPath string) (*Config, error) {
	v := viper.New()

	// Set environment variable prefix
	v.SetEnvPrefix("MCP_PROXY")
	v.AutomaticEnv()

	// Set config file name
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Add config paths
	if configPath != "" {
		v.AddConfigPath(configPath)
	}
	v.AddConfigPath(".")                // Current directory
	v.AddConfigPath("$HOME/.mcp-proxy") // User home
	v.AddConfigPath("/etc/mcp-proxy")   // System-wide

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Try to read the file directly to get better error messages
			if configPath != "" {
				if data, readErr := os.ReadFile(configPath); readErr == nil {
					if parseErr := yaml.Unmarshal(data, &Config{}); parseErr != nil {
						return nil, fmt.Errorf("YAML parsing error in %s: %w", configPath, parseErr)
					}
				}
			}
			return nil, fmt.Errorf("error reading config file %s: %w", configPath, err)
		}
		// Config file not found, but that's okay - we'll use defaults
	}

	// Unmarshal config
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config from %s: %w", v.ConfigFileUsed(), err)
	}

	// Override OpenAI API key from environment variable if set
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		config.AIPolicyValidation.APIKey = apiKey
	}

	// Load policies from rules file if specified
	if config.PolicyValidation.RulesFile != "" {
		policies, err := LoadCELPoliciesFromFile(config.PolicyValidation.RulesFile)
		if err != nil {
			return nil, fmt.Errorf("error loading policies from file: %w", err)
		}
		config.PolicyValidation.Rules = policies
	}

	// Load AI policies from rules file if specified
	if config.AIPolicyValidation.RulesFile != "" {
		aiPolicies, err := LoadAIPoliciesFromFile(config.AIPolicyValidation.RulesFile)
		if err != nil {
			return nil, fmt.Errorf("error loading AI policies from file: %w", err)
		}
		config.AIPolicyValidation.Rules = aiPolicies
	}

	// Validate config
	if err := ValidateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// ValidateConfig validates the configuration
func ValidateConfig(cfg *Config) error {
	// Validate server type
	switch cfg.Server.Type {
	case ServerTypeSTDIO, ServerTypeHTTP, ServerTypeSSE:
		// Valid server type
	default:
		return fmt.Errorf("invalid server type: %s", cfg.Server.Type)
	}

	// Validate server configuration
	if cfg.Server.ListenAddr == "" {
		return fmt.Errorf("server.listen_addr is required")
	}

	// Validate SSE server configuration if SSE type
	if cfg.Server.Type == ServerTypeSSE {
		if cfg.Server.SSE.TLS.Enabled {
			if cfg.Server.SSE.TLS.CertFile == "" {
				return fmt.Errorf("server.sse.tls.cert_file is required when TLS is enabled")
			}
			if cfg.Server.SSE.TLS.KeyFile == "" {
				return fmt.Errorf("server.sse.tls.key_file is required when TLS is enabled")
			}
		}
	}

	// Validate auth configuration
	switch cfg.Auth.Type {
	case "api_key":
		if cfg.Auth.APIKey == "" {
			return fmt.Errorf("auth.api_key is required when auth.type is api_key")
		}
	case "jwt":
		if cfg.Auth.JWTConfig.JWKSUrl == "" {
			return fmt.Errorf("auth.jwt.jwks_url is required when auth.type is jwt")
		}
	case "mtls":
		if cfg.Auth.MTLSConfig.CAFile == "" {
			return fmt.Errorf("auth.mtls.ca_file is required when auth.type is mtls")
		}
	}

	// Validate client configuration
	switch cfg.Client.Type {
	case "stdio":
		if cfg.Client.Command == "" {
			return fmt.Errorf("client.command is required when client.type is stdio")
		}
	case "sse", "http":
		if cfg.Client.DownstreamURL == "" {
			return fmt.Errorf("client.downstream_url is required when client.type is %s", cfg.Client.Type)
		}
	}

	// Validate audit configuration
	if cfg.Audit.Path == "" {
		return fmt.Errorf("audit.path is required")
	}

	// Validate AI validation configuration
	if cfg.AIPolicyValidation.Enabled {
		if cfg.AIPolicyValidation.APIKey == "" {
			return fmt.Errorf("OPENAI_API_KEY environment variable is required when AI validation is enabled")
		}
		if cfg.AIPolicyValidation.Endpoint == "" {
			return fmt.Errorf("ai_validation.endpoint is required when AI validation is enabled")
		}
		if cfg.AIPolicyValidation.Model == "" {
			return fmt.Errorf("ai_validation.model is required when AI validation is enabled")
		}
	}

	return nil
}

// GetLogger creates a new logger based on the configuration
func GetLogger(cfg *Config) (*zap.Logger, error) {
	var config zap.Config

	if cfg.Server.LogFormat == "json" {
		config = zap.NewProductionConfig()
	} else {
		config = zap.NewDevelopmentConfig()
	}

	// Set log level
	level, err := zapcore.ParseLevel(cfg.Server.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}
	config.Level = zap.NewAtomicLevelAt(level)

	// Ensure logs go to stdout
	config.OutputPaths = []string{"stdout"}
	config.ErrorOutputPaths = []string{"stderr"}

	// Build the logger
	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	// Add logger type designation
	return logger.With(zap.String("logger", "application")), nil
}

// GetAuditLogger creates a new audit logger based on the configuration
func GetAuditLogger(cfg *Config) (*zap.Logger, error) {
	var config zap.Config

	if cfg.Audit.Format == "json" {
		config = zap.NewProductionConfig()
	} else {
		config = zap.NewDevelopmentConfig()
	}

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

	// Add logger type designation
	return logger.With(zap.String("logger", "audit")), nil
}
