package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closedPortURL returns an http URL on a local port that nothing listens on.
func closedPortURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + addr
}

// TestDiscoverPassThroughTools_ErrorsAreSafe verifies that when a pass-through
// downstream fails to connect, the DiscoveryError returned to the agent is a
// fixed message for the failure kind and never carries the raw error, which
// can include the downstream URL and any credential in its query string.
func TestDiscoverPassThroughTools_ErrorsAreSafe(t *testing.T) {
	const secret = "SECRET"

	tests := []struct {
		name string
		// downstreamURL returns the configured URL; status servers are closed by t.Cleanup.
		downstreamURL func(t *testing.T) string
		wantError     string
	}{
		{
			name:          "400 on initialize",
			downstreamURL: statusServerURL(http.StatusBadRequest),
			wantError:     "failed to connect to downstream server 'github': " + errInitializeRejected4xx.Error(),
		},
		{
			name:          "401 on initialize",
			downstreamURL: statusServerURL(http.StatusUnauthorized),
			wantError:     "failed to connect to downstream server 'github': " + msgPassThroughAuthorizationRequired,
		},
		{
			name:          "closed port",
			downstreamURL: closedPortURL,
			wantError:     "failed to connect to downstream server 'github'; see the gateway log for details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := newTestLogger(t)
			downstreamURL := tt.downstreamURL(t) + "/mcp?token=" + secret

			cfg := config.ClientConfig{Type: "http", DownstreamURL: downstreamURL}
			cfg.Auth.PassThrough.Enabled = true
			cfg.Auth.PassThrough.Headers = []config.CredentialMapping{
				{SourceHeader: "X-GitHub-Token", TargetHeader: "Authorization", Format: "Bearer {value}"},
			}

			cm := NewClientManager(ctx, logger)
			require.NoError(t, cm.InitializeClients(ctx, map[string]config.ClientConfig{"github": cfg}))
			t.Cleanup(func() { _ = cm.Close(ctx) })

			gw := &Gateway{
				logger:        logger,
				server:        server.NewMCPServer("test-gateway", "1.0.0", server.WithToolCapabilities(true)),
				clientManager: cm,
				config:        &config.Config{},
			}

			result, err := gw.DiscoverPassThroughTools(ctx, "test-session-errors", "github")
			require.NoError(t, err)
			require.Len(t, result.Errors, 1)

			discoveryErr := result.Errors[0]
			assert.Equal(t, "github", discoveryErr.ClientName)
			assert.Equal(t, tt.wantError, discoveryErr.Error)
			assert.NotContains(t, discoveryErr.Error, secret)
			assert.NotContains(t, discoveryErr.Error, downstreamURL)
			assert.NotContains(t, discoveryErr.Error, "127.0.0.1")
		})
	}
}

// statusServerURL returns a downstreamURL func for a server that answers
// every request with status.
func statusServerURL(status int) func(t *testing.T) string {
	return func(t *testing.T) string {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
}

// TestSafeDownstreamConnectError verifies the mapping from a downstream
// connect error to the message a caller sees: recognized errors get a fixed
// message, anything else the generic one, and no text from the raw error
// (which here carries a credential) ever comes through.
func TestSafeDownstreamConnectError(t *testing.T) {
	const secret = "SECRET"
	passThrough := config.ClientConfig{Type: "http"}
	passThrough.Auth.PassThrough.Enabled = true
	staticHeaders := config.ClientConfig{Type: "http"}

	tests := []struct {
		name string
		cfg  config.ClientConfig
		err  error
		want string
	}{
		{
			name: "initialize rejected with 4xx",
			cfg:  staticHeaders,
			err:  fmt.Errorf("failed to create client github: %w: %w", errInitializeRejected4xx, errors.New("url?token="+secret)),
			want: "failed to connect to downstream server 'github': " + errInitializeRejected4xx.Error(),
		},
		{
			name: "401 on pass-through client",
			cfg:  passThrough,
			err:  fmt.Errorf("token=%s: %w", secret, transport.ErrAuthorizationRequired),
			want: "failed to connect to downstream server 'github': " + msgPassThroughAuthorizationRequired,
		},
		{
			name: "401 on client with configured headers",
			cfg:  staticHeaders,
			err:  fmt.Errorf("token=%s: %w", secret, &transport.AuthorizationRequiredError{}),
			want: "failed to connect to downstream server 'github': " + msgAuthorizationRequired,
		},
		{
			name: "legacy SSE sentinel without the 4xx rewrite",
			cfg:  staticHeaders,
			err:  fmt.Errorf("token=%s: %w", secret, transport.ErrLegacySSEServer),
			want: "failed to connect to downstream server 'github'; see the gateway log for details",
		},
		{
			name: "unrecognized error",
			cfg:  passThrough,
			err:  errors.New("dial http://host/mcp?token=" + secret),
			want: "failed to connect to downstream server 'github'; see the gateway log for details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeDownstreamConnectError("github", tt.cfg, tt.err)
			assert.Equal(t, tt.want, got.Error())
			assert.NotContains(t, got.Error(), secret)
		})
	}
}
