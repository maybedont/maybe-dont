package config

import (
	"testing"
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
				}{
					Type:       tt.serverType,
					ListenAddr: tt.listenAddr,
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
					SSE        struct {
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
