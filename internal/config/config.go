package config

import (
	"fmt"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
		// Server-specific configuration
		HTTP struct {
			ReadTimeout  int `mapstructure:"read_timeout"`  // in seconds
			WriteTimeout int `mapstructure:"write_timeout"` // in seconds
			IdleTimeout  int `mapstructure:"idle_timeout"`  // in seconds
			TLS          struct {
				Enabled  bool   `mapstructure:"enabled"`
				CertFile string `mapstructure:"cert_file"`
				KeyFile  string `mapstructure:"key_file"`
			} `mapstructure:"tls"`
			CORS struct {
				Enabled        bool     `mapstructure:"enabled"`
				AllowedOrigins []string `mapstructure:"allowed_origins"`
				AllowedMethods []string `mapstructure:"allowed_methods"`
				AllowedHeaders []string `mapstructure:"allowed_headers"`
			} `mapstructure:"cors"`
		} `mapstructure:"http"`
		SSE struct {
			ReadTimeout    int `mapstructure:"read_timeout"`   // in seconds
			WriteTimeout   int `mapstructure:"write_timeout"`  // in seconds
			IdleTimeout    int `mapstructure:"idle_timeout"`   // in seconds
			RetryInterval  int `mapstructure:"retry_interval"` // in milliseconds
			MaxConnections int `mapstructure:"max_connections"`
			TLS            struct {
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
	Policy struct {
		RulesFile string      `mapstructure:"rules_file"`
		Default   string      `mapstructure:"default"` // allow or deny
		Rules     []CELPolicy `mapstructure:"rules"`
	} `mapstructure:"policy"`

	// Transport configuration
	Transport struct {
		Type          string   `mapstructure:"type"` // stdio, sse, http
		DownstreamURL string   `mapstructure:"downstream_url"`
		Command       string   `mapstructure:"command"`
		CommandArgs   []string `mapstructure:"command_args"`
		SSEConfig     struct {
			Headers map[string]string `mapstructure:"headers"`
			Timeout int               `mapstructure:"timeout"`
		} `mapstructure:"sse"`
	} `mapstructure:"transport"`

	// Audit configuration
	Audit struct {
		Enabled bool   `mapstructure:"enabled"`
		Path    string `mapstructure:"path"`
		Format  string `mapstructure:"format"` // json or text
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

// LoadConfig loads the configuration from all sources
func LoadConfig(configPath string) (*Config, error) {
	v := viper.New()

	// Set default values
	setDefaults(v)

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
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found, but that's okay - we'll use defaults
	}

	// Unmarshal config
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Validate config
	if err := ValidateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// setDefaults sets the default configuration values
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.type", "stdio")
	v.SetDefault("server.listen_addr", "localhost:8080")
	v.SetDefault("server.log_level", "info")
	v.SetDefault("server.log_format", "json")

	// HTTP server defaults
	v.SetDefault("server.http.read_timeout", 30)
	v.SetDefault("server.http.write_timeout", 30)
	v.SetDefault("server.http.idle_timeout", 120)
	v.SetDefault("server.http.tls.enabled", false)
	v.SetDefault("server.http.cors.enabled", false)
	v.SetDefault("server.http.cors.allowed_methods", []string{"GET", "POST", "OPTIONS"})
	v.SetDefault("server.http.cors.allowed_headers", []string{"Content-Type", "Authorization"})

	// SSE server defaults
	v.SetDefault("server.sse.read_timeout", 30)
	v.SetDefault("server.sse.write_timeout", 30)
	v.SetDefault("server.sse.idle_timeout", 120)
	v.SetDefault("server.sse.retry_interval", 3000)
	v.SetDefault("server.sse.max_connections", 1000)
	v.SetDefault("server.sse.tls.enabled", false)

	// Auth defaults
	v.SetDefault("auth.type", "api_key")
	v.SetDefault("auth.jwt.claim_roles", "roles")

	// Policy defaults
	v.SetDefault("policy.default", "deny")

	// Transport defaults
	v.SetDefault("transport.type", "stdio")
	v.SetDefault("transport.sse.timeout", 30)

	// Audit defaults
	v.SetDefault("audit.enabled", true)
	v.SetDefault("audit.format", "json")
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

	// Validate HTTP server configuration if HTTP type
	if cfg.Server.Type == ServerTypeHTTP {
		if cfg.Server.HTTP.TLS.Enabled {
			if cfg.Server.HTTP.TLS.CertFile == "" {
				return fmt.Errorf("server.http.tls.cert_file is required when TLS is enabled")
			}
			if cfg.Server.HTTP.TLS.KeyFile == "" {
				return fmt.Errorf("server.http.tls.key_file is required when TLS is enabled")
			}
		}
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

	// Validate transport configuration
	switch cfg.Transport.Type {
	case "stdio":
		if cfg.Transport.Command == "" {
			return fmt.Errorf("transport.command is required when transport.type is stdio")
		}
	case "sse", "http":
		if cfg.Transport.DownstreamURL == "" {
			return fmt.Errorf("transport.downstream_url is required when transport.type is %s", cfg.Transport.Type)
		}
	}

	// Validate audit configuration
	if cfg.Audit.Enabled && cfg.Audit.Path == "" {
		return fmt.Errorf("audit.path is required when audit.enabled is true")
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

	return config.Build()
}
