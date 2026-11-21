package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// newObservedLogger returns a logger that records every entry, down to Debug.
func newObservedLogger() (*config.SessionLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return config.NewSessionLogger(zap.New(core)), logs
}

// requireLogsExclude fails if any recorded message or field contains s.
func requireLogsExclude(t *testing.T, logs *observer.ObservedLogs, s string) {
	t.Helper()
	for _, entry := range logs.All() {
		require.NotContains(t, entry.Message, s)
		for key, value := range entry.ContextMap() {
			require.NotContains(t, fmt.Sprint(value), s, "log %q field %q", entry.Message, key)
		}
	}
}

// closedPortURLWithUserinfo is closedPortURL with a username and password,
// which net/http shows as user:*** in its errors.
func closedPortURLWithUserinfo(t *testing.T) string {
	return strings.Replace(closedPortURL(t), "http://", "http://user:pass@", 1)
}

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
// can include the downstream URL and any credential in its query string. The
// logged error keeps the detail but has the credential-bearing URL parts
// removed.
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
		{
			name:          "closed port with userinfo",
			downstreamURL: closedPortURLWithUserinfo,
			wantError:     "failed to connect to downstream server 'github'; see the gateway log for details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger, logs := newObservedLogger()
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
			require.NotEmpty(t, logs.FilterMessage("Failed to connect to pass-through client").All())
			requireLogsExclude(t, logs, secret)
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

// TestDownstreamCallError verifies what a caller sees and what is logged when
// a request to a connected downstream fails. Transport failures get a fixed
// message (the 401 hint for authorization failures, otherwise the generic
// one), a JSON-RPC error from the downstream passes through untouched, a
// cancelled request logs at Debug, and no log carries the URL's credential.
func TestDownstreamCallError(t *testing.T) {
	const secret = "SECRET"
	const wantGeneric = "call to downstream server 'github' failed; see the gateway log for details"

	// A real net/http failure, so the test sees the URL the way net/http
	// writes it (password masked, query kept).
	dialErr := func(t *testing.T, rawURL string) error {
		t.Helper()
		resp, err := http.Post(rawURL, "application/json", nil) //nolint:noctx // test helper
		if resp != nil {
			_ = resp.Body.Close()
		}
		require.Error(t, err)
		require.Contains(t, err.Error(), secret, "precondition: raw error carries the secret")
		return transport.NewError(err)
	}
	jsonRPCErr := fmt.Errorf("%w: message must not be empty", mcp.ErrInvalidParams)

	tests := []struct {
		name        string
		passThrough bool
		userinfo    bool
		err         func(t *testing.T, rawURL string) error
		wantErr     string
		wantLevel   zapcore.Level
		wantNoLog   bool
	}{
		{
			name:      "connection refused",
			err:       dialErr,
			wantErr:   wantGeneric,
			wantLevel: zapcore.WarnLevel,
		},
		{
			name:      "connection refused with userinfo in URL",
			userinfo:  true,
			err:       dialErr,
			wantErr:   wantGeneric,
			wantLevel: zapcore.WarnLevel,
		},
		{
			name:        "401 on pass-through client",
			passThrough: true,
			err: func(*testing.T, string) error {
				return transport.NewError(&transport.AuthorizationRequiredError{})
			},
			wantErr:   "call to downstream server 'github' failed: " + msgPassThroughAuthorizationRequired,
			wantLevel: zapcore.WarnLevel,
		},
		{
			name: "401 on client with configured headers",
			err: func(*testing.T, string) error {
				return transport.NewError(&transport.AuthorizationRequiredError{})
			},
			wantErr:   "call to downstream server 'github' failed: " + msgAuthorizationRequired,
			wantLevel: zapcore.WarnLevel,
		},
		{
			name: "cancelled request",
			err: func(*testing.T, string) error {
				return transport.NewError(context.Canceled)
			},
			wantErr:   wantGeneric,
			wantLevel: zapcore.DebugLevel,
		},
		{
			name:      "JSON-RPC error from the downstream",
			err:       func(*testing.T, string) error { return jsonRPCErr },
			wantErr:   jsonRPCErr.Error(),
			wantNoLog: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawURL := closedPortURL(t)
			if tt.userinfo {
				rawURL = closedPortURLWithUserinfo(t)
			}
			rawURL += "/mcp?token=" + secret
			cfg := config.ClientConfig{Type: "http", DownstreamURL: rawURL}
			cfg.Auth.PassThrough.Enabled = tt.passThrough

			logger, logs := newObservedLogger()
			gw := &Gateway{logger: logger}

			got := gw.downstreamCallError(context.Background(), "session-1", "github", cfg, "tools/call", tt.err(t, rawURL))
			require.Error(t, got)
			assert.Equal(t, tt.wantErr, got.Error())
			requireLogsExclude(t, logs, secret)

			if tt.wantNoLog {
				assert.Zero(t, logs.Len())
				return
			}
			entries := logs.FilterMessage("Downstream request failed").All()
			require.Len(t, entries, 1)
			assert.Equal(t, tt.wantLevel, entries[0].Level)
		})
	}
}
