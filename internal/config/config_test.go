package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		want    *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: `
server:
  type: http
  listen_addr: ":8080"
  log_level: info
  log_format: json
  http:
    read_timeout: 30
    write_timeout: 30
    idle_timeout: 120
    tls:
      enabled: true
      cert_file: "cert.pem"
      key_file: "key.pem"
    cors:
      enabled: true
      allowed_origins: ["*"]
      allowed_methods: ["GET", "POST"]
      allowed_headers: ["*"]
auth:
  type: api_key
  api_key: "test-key"
transport:
  type: stdio
  command: "echo"
audit:
  enabled: true
  path: "/tmp/audit.log"
  format: "json"
Policy:
  rules_file: "rules.json"
  default: deny
`,
			want: &Config{
				Server: struct {
					Type       ServerType `mapstructure:"type"`
					ListenAddr string     `mapstructure:"listen_addr"`
					LogLevel   string     `mapstructure:"log_level"`
					LogFormat  string     `mapstructure:"log_format"`
					HTTP       struct {
						ReadTimeout  int `mapstructure:"read_timeout"`
						WriteTimeout int `mapstructure:"write_timeout"`
						IdleTimeout  int `mapstructure:"idle_timeout"`
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
						ReadTimeout    int `mapstructure:"read_timeout"`
						WriteTimeout   int `mapstructure:"write_timeout"`
						IdleTimeout    int `mapstructure:"idle_timeout"`
						RetryInterval  int `mapstructure:"retry_interval"`
						MaxConnections int `mapstructure:"max_connections"`
						TLS            struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
				}{
					Type:       ServerTypeHTTP,
					ListenAddr: ":8080",
					LogLevel:   "info",
					LogFormat:  "json",
					HTTP: struct {
						ReadTimeout  int `mapstructure:"read_timeout"`
						WriteTimeout int `mapstructure:"write_timeout"`
						IdleTimeout  int `mapstructure:"idle_timeout"`
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
					}{
						ReadTimeout:  30,
						WriteTimeout: 30,
						IdleTimeout:  120,
						TLS: struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						}{
							Enabled:  true,
							CertFile: "cert.pem",
							KeyFile:  "key.pem",
						},
						CORS: struct {
							Enabled        bool     `mapstructure:"enabled"`
							AllowedOrigins []string `mapstructure:"allowed_origins"`
							AllowedMethods []string `mapstructure:"allowed_methods"`
							AllowedHeaders []string `mapstructure:"allowed_headers"`
						}{
							Enabled:        true,
							AllowedOrigins: []string{"*"},
							AllowedMethods: []string{"GET", "POST"},
							AllowedHeaders: []string{"*"},
						},
					},
					SSE: struct {
						ReadTimeout    int `mapstructure:"read_timeout"`
						WriteTimeout   int `mapstructure:"write_timeout"`
						IdleTimeout    int `mapstructure:"idle_timeout"`
						RetryInterval  int `mapstructure:"retry_interval"`
						MaxConnections int `mapstructure:"max_connections"`
						TLS            struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					}{
						ReadTimeout:    30,
						WriteTimeout:   30,
						IdleTimeout:    120,
						RetryInterval:  3000,
						MaxConnections: 1000,
						TLS: struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						}{},
					},
				},
				Auth: struct {
					Type      string `mapstructure:"type"`
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
				}{
					Type:   "api_key",
					APIKey: "test-key",
					JWTConfig: struct {
						JWKSUrl    string   `mapstructure:"jwks_url"`
						Issuer     string   `mapstructure:"issuer"`
						Audience   []string `mapstructure:"audience"`
						ClaimRoles string   `mapstructure:"claim_roles"`
					}{
						ClaimRoles: "roles",
					},
				},
				Transport: struct {
					Type          string   `mapstructure:"type"`
					DownstreamURL string   `mapstructure:"downstream_url"`
					Command       string   `mapstructure:"command"`
					CommandArgs   []string `mapstructure:"command_args"`
					SSEConfig     struct {
						Headers map[string]string `mapstructure:"headers"`
						Timeout int               `mapstructure:"timeout"`
					} `mapstructure:"sse"`
				}{
					Type:    "stdio",
					Command: "echo",
					SSEConfig: struct {
						Headers map[string]string `mapstructure:"headers"`
						Timeout int               `mapstructure:"timeout"`
					}{
						Timeout: 30,
					},
				},
				Audit: struct {
					Enabled bool   `mapstructure:"enabled"`
					Path    string `mapstructure:"path"`
					Format  string `mapstructure:"format"`
				}{
					Enabled: true,
					Path:    "/tmp/audit.log",
					Format:  "json",
				},
				Policy: struct {
					RulesFile string `mapstructure:"rules_file"`
					Default   string `mapstructure:"default"`
				}{
					RulesFile: "rules.json",
					Default:   "deny",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid server type",
			config: `
server:
  type: invalid
`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "missing required fields",
			config: `
server:
  type: http
`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test files
			tmpDir, err := os.MkdirTemp("", "config-test")
			require.NoError(t, err)
			defer func() {
				if err := os.RemoveAll(tmpDir); err != nil {
					t.Errorf("failed to remove temp dir: %v", err)
				}
			}()

			// Write config to temporary file
			configPath := filepath.Join(tmpDir, "config.yaml")
			err = os.WriteFile(configPath, []byte(tt.config), 0644)
			require.NoError(t, err)

			// Test loading config
			got, err := LoadConfig(tmpDir)
			if tt.name == "valid config" {
				fmt.Printf("Loaded config: %+v\n", got)
				fmt.Printf("Error: %v\n", err)
			}
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Server: struct {
					Type       ServerType `mapstructure:"type"`
					ListenAddr string     `mapstructure:"listen_addr"`
					LogLevel   string     `mapstructure:"log_level"`
					LogFormat  string     `mapstructure:"log_format"`
					HTTP       struct {
						ReadTimeout  int `mapstructure:"read_timeout"`
						WriteTimeout int `mapstructure:"write_timeout"`
						IdleTimeout  int `mapstructure:"idle_timeout"`
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
						ReadTimeout    int `mapstructure:"read_timeout"`
						WriteTimeout   int `mapstructure:"write_timeout"`
						IdleTimeout    int `mapstructure:"idle_timeout"`
						RetryInterval  int `mapstructure:"retry_interval"`
						MaxConnections int `mapstructure:"max_connections"`
						TLS            struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
				}{
					Type:       ServerTypeHTTP,
					ListenAddr: ":8080",
					LogLevel:   "info",
					LogFormat:  "json",
					HTTP: struct {
						ReadTimeout  int `mapstructure:"read_timeout"`
						WriteTimeout int `mapstructure:"write_timeout"`
						IdleTimeout  int `mapstructure:"idle_timeout"`
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
					}{
						ReadTimeout:  30,
						WriteTimeout: 30,
						IdleTimeout:  120,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid server type",
			config: &Config{
				Server: struct {
					Type       ServerType `mapstructure:"type"`
					ListenAddr string     `mapstructure:"listen_addr"`
					LogLevel   string     `mapstructure:"log_level"`
					LogFormat  string     `mapstructure:"log_format"`
					HTTP       struct {
						ReadTimeout  int `mapstructure:"read_timeout"`
						WriteTimeout int `mapstructure:"write_timeout"`
						IdleTimeout  int `mapstructure:"idle_timeout"`
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
						ReadTimeout    int `mapstructure:"read_timeout"`
						WriteTimeout   int `mapstructure:"write_timeout"`
						IdleTimeout    int `mapstructure:"idle_timeout"`
						RetryInterval  int `mapstructure:"retry_interval"`
						MaxConnections int `mapstructure:"max_connections"`
						TLS            struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
				}{
					Type: "invalid",
				},
			},
			wantErr: true,
		},
		{
			name: "missing required fields",
			config: &Config{
				Server: struct {
					Type       ServerType `mapstructure:"type"`
					ListenAddr string     `mapstructure:"listen_addr"`
					LogLevel   string     `mapstructure:"log_level"`
					LogFormat  string     `mapstructure:"log_format"`
					HTTP       struct {
						ReadTimeout  int `mapstructure:"read_timeout"`
						WriteTimeout int `mapstructure:"write_timeout"`
						IdleTimeout  int `mapstructure:"idle_timeout"`
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
						ReadTimeout    int `mapstructure:"read_timeout"`
						WriteTimeout   int `mapstructure:"write_timeout"`
						IdleTimeout    int `mapstructure:"idle_timeout"`
						RetryInterval  int `mapstructure:"retry_interval"`
						MaxConnections int `mapstructure:"max_connections"`
						TLS            struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
				}{
					Type: ServerTypeHTTP,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestGetLogger(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Server: struct {
					Type       ServerType `mapstructure:"type"`
					ListenAddr string     `mapstructure:"listen_addr"`
					LogLevel   string     `mapstructure:"log_level"`
					LogFormat  string     `mapstructure:"log_format"`
					HTTP       struct {
						ReadTimeout  int `mapstructure:"read_timeout"`
						WriteTimeout int `mapstructure:"write_timeout"`
						IdleTimeout  int `mapstructure:"idle_timeout"`
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
						ReadTimeout    int `mapstructure:"read_timeout"`
						WriteTimeout   int `mapstructure:"write_timeout"`
						IdleTimeout    int `mapstructure:"idle_timeout"`
						RetryInterval  int `mapstructure:"retry_interval"`
						MaxConnections int `mapstructure:"max_connections"`
						TLS            struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
				}{
					LogLevel:  "info",
					LogFormat: "json",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid log level",
			config: &Config{
				Server: struct {
					Type       ServerType `mapstructure:"type"`
					ListenAddr string     `mapstructure:"listen_addr"`
					LogLevel   string     `mapstructure:"log_level"`
					LogFormat  string     `mapstructure:"log_format"`
					HTTP       struct {
						ReadTimeout  int `mapstructure:"read_timeout"`
						WriteTimeout int `mapstructure:"write_timeout"`
						IdleTimeout  int `mapstructure:"idle_timeout"`
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
						ReadTimeout    int `mapstructure:"read_timeout"`
						WriteTimeout   int `mapstructure:"write_timeout"`
						IdleTimeout    int `mapstructure:"idle_timeout"`
						RetryInterval  int `mapstructure:"retry_interval"`
						MaxConnections int `mapstructure:"max_connections"`
						TLS            struct {
							Enabled  bool   `mapstructure:"enabled"`
							CertFile string `mapstructure:"cert_file"`
							KeyFile  string `mapstructure:"key_file"`
						} `mapstructure:"tls"`
					} `mapstructure:"sse"`
				}{
					LogLevel:  "invalid",
					LogFormat: "json",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := GetLogger(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, logger)
		})
	}
}
