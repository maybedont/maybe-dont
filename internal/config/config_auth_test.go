package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeAuthConfig writes a maybe-dont.yaml with the given body to a temp dir and loads it.
func loadAuthConfig(t *testing.T, body string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "maybe-dont.yaml"), []byte(body), 0o644))
	return LoadConfig(dir, "")
}

// TestClientAuthConfigEffectiveType verifies auth-type resolution including the legacy
// pass_through.enabled backward-compat path.
func TestClientAuthConfigEffectiveType(t *testing.T) {
	tests := []struct {
		name         string
		cfg          ClientAuthConfig
		wantType     string
		wantPerSess  bool
		wantExchange bool
		wantPassThru bool
	}{
		{"empty", ClientAuthConfig{}, "", false, false, false},
		{"explicit pass_through", ClientAuthConfig{Type: AuthTypePassThrough}, AuthTypePassThrough, true, false, true},
		{"legacy pass_through", ClientAuthConfig{PassThrough: PassThroughConfig{Enabled: true}}, AuthTypePassThrough, true, false, true},
		{"token_exchange", ClientAuthConfig{Type: AuthTypeTokenExchange}, AuthTypeTokenExchange, true, true, false},
		{"enterprise_managed", ClientAuthConfig{Type: AuthTypeEnterpriseManaged}, AuthTypeEnterpriseManaged, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantType, tt.cfg.EffectiveType())
			require.Equal(t, tt.wantPerSess, tt.cfg.RequiresPerSessionCredentials())
			require.Equal(t, tt.wantExchange, tt.cfg.IsTokenExchange())
			require.Equal(t, tt.wantPassThru, tt.cfg.IsPassThrough())
		})
	}
}

// TestLoadConfigEmbeddedASDefaults verifies embedded_as mode loads and derives sensible
// defaults (audience from resource, embedded issuer from resource origin).
func TestLoadConfigEmbeddedASDefaults(t *testing.T) {
	cfg, err := loadAuthConfig(t, `
server:
  type: http
  listen_addr: "127.0.0.1:8080"
  auth:
    mode: embedded_as
    resource: https://maybedont.example/mcp
idp:
  issuer: https://idp.example
  jwks_url: https://idp.example/jwks
request_validation:
  cel:
    enabled: false
  ai:
    enabled: false
native_tools:
  audit_report:
    enabled: false
`)
	require.NoError(t, err)
	require.True(t, cfg.AuthEnabled())
	require.Equal(t, "https://maybedont.example/mcp", cfg.Server.Auth.Audience, "audience defaults to resource")
	require.Equal(t, "https://maybedont.example", cfg.Server.Auth.EmbeddedAS.Issuer, "embedded issuer defaults to resource origin")
	require.Equal(t, 3600, cfg.Server.Auth.EmbeddedAS.AccessTokenTTLSeconds)
}

// TestLoadConfigAuthValidation covers the auth-related startup validation rules.
func TestLoadConfigAuthValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "invalid mode rejected",
			body: `
server: {type: http, listen_addr: "127.0.0.1:8080", auth: {mode: bogus, resource: https://x/mcp}}
idp: {issuer: https://idp.example, jwks_url: https://idp.example/jwks}
request_validation: {cel: {enabled: false}, ai: {enabled: false}}
`,
			wantErr: "server.auth.mode",
		},
		{
			name: "auth enabled without resource rejected",
			body: `
server: {type: http, listen_addr: "127.0.0.1:8080", auth: {mode: jwt_validation}}
idp: {issuer: https://idp.example, jwks_url: https://idp.example/jwks}
request_validation: {cel: {enabled: false}, ai: {enabled: false}}
`,
			wantErr: "server.auth.resource",
		},
		{
			name: "token_exchange without auth enabled rejected",
			body: `
server: {type: http, listen_addr: "127.0.0.1:8080"}
downstream_mcp_servers:
  api: {type: http, url: "https://api.example/mcp/", auth: {type: token_exchange}}
request_validation: {cel: {enabled: false}, ai: {enabled: false}}
`,
			wantErr: "requires server.auth.mode to be enabled",
		},
		{
			name: "token_exchange on stdio rejected",
			body: `
server:
  type: http
  listen_addr: "127.0.0.1:8080"
  auth: {mode: jwt_validation, resource: https://x/mcp}
idp:
  issuer: https://idp.example
  jwks_url: https://idp.example/jwks
  token_endpoint: https://idp.example/token
  client_id: gw
  client_secret: shh
downstream_mcp_servers:
  api: {type: stdio, command: echo, auth: {type: token_exchange}}
request_validation: {cel: {enabled: false}, ai: {enabled: false}}
`,
			wantErr: "only supported for http and sse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadAuthConfig(t, tt.body)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
