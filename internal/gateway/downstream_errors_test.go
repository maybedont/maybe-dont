package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestRedactURLs verifies that every http or https URL in a piece of text
// loses its userinfo, query string and fragment, keeping scheme, host and
// path, including URLs that Go's %q quoting escaped, and that text with no
// URL is unchanged.
func TestRedactURLs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "configured URL with query",
			in:   `Post "http://127.0.0.1:1/mcp?token=SECRET": dial tcp 127.0.0.1:1: connect: connection refused`,
			want: `Post "http://127.0.0.1:1/mcp": dial tcp 127.0.0.1:1: connect: connection refused`,
		},
		{
			name: "user and password as net/http masks them",
			in:   `Post "http://user:***@host.example/mcp?token=SECRET": EOF`,
			want: `Post "http://host.example/mcp": EOF`,
		},
		{
			name: "user and password",
			in:   `https://user:SECRET@host.example/mcp`,
			want: `https://host.example/mcp`,
		},
		{
			name: "user-only token on an SSE endpoint",
			in:   `Post "https://SECRET@host.example/messages/?session_id=abc": EOF`,
			want: `Post "https://host.example/messages/": EOF`,
		},
		{
			name: "redirect target with trailing slash",
			in:   `Post "http://host.example/mcp/?token=SECRET": unexpected EOF`,
			want: `Post "http://host.example/mcp/": unexpected EOF`,
		},
		{
			name: "quoted token with a quote, backslash and non-printable",
			in:   fmt.Sprintf("Post %q: EOF", "http://host.example/mcp?token=SE\"C\\R\x00ET"),
			want: `Post "http://host.example/mcp": EOF`,
		},
		{
			name: "quoted userinfo with a quote",
			in:   fmt.Sprintf("Post %q: EOF", "http://us\"er:SECRET@host.example/mcp"),
			want: `Post "http://host.example/mcp": EOF`,
		},
		{
			name: "slash in userinfo",
			in:   `https://us/er:SECRET@host.example/mcp`,
			want: `https://host.example/mcp`,
		},
		{
			name: "fragment",
			in:   `GET https://host.example/sse#SECRET failed`,
			want: `GET https://host.example/sse failed`,
		},
		{
			name: "upper-case scheme",
			in:   `HTTPS://user:SECRET@host.example/mcp?token=SECRET`,
			want: `HTTPS://host.example/mcp`,
		},
		{
			name: "multiple URLs",
			in:   `redirect from https://a.example/x?t=SECRET to http://SECRET@b.example/y#SECRET`,
			want: `redirect from https://a.example/x to http://b.example/y`,
		},
		{
			name: "no URL",
			in:   `failed to initialize MCP server: connection refused`,
			want: `failed to initialize MCP server: connection refused`,
		},
		{
			name: "short word",
			in:   `x`,
			want: `x`,
		},
		{
			name: "scheme only",
			in:   `use http:// or https://`,
			want: `use http:// or https://`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactURLs(tt.in)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "SECRET")
		})
	}
}

// TestDownstreamSlogLogger verifies that the slog logger handed to mcp-go
// writes through the gateway's zap logger at the matching level, drops
// records below the zap level, and redacts URLs in the message and in every
// attribute, including grouped and pre-bound ones.
func TestDownstreamSlogLogger(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	logger := newDownstreamSlogLogger(zap.New(core), "github")

	closeErr := errors.New(`Delete "http://user:pass@host.example/mcp?token=SECRET": connection refused`)
	logger.Debug("dropped below the zap level", "err", closeErr)
	logger.Error("failed to send close request", "err", closeErr)
	logger.With("url", "https://host.example/mcp?token=SECRET").
		WithGroup("retry").
		Warn("failed to listen to server https://SECRET@host.example/mcp", "err", closeErr, "retryIn", 2)

	entries := logs.All()
	require.Len(t, entries, 2)

	assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	assert.Equal(t, "failed to send close request", entries[0].Message)
	assert.Equal(t, map[string]any{
		"client": "github",
		"err":    `Delete "http://host.example/mcp": connection refused`,
	}, entries[0].ContextMap())

	assert.Equal(t, zapcore.WarnLevel, entries[1].Level)
	assert.Equal(t, "failed to listen to server https://host.example/mcp", entries[1].Message)
	assert.Equal(t, map[string]any{
		"client":        "github",
		"url":           "https://host.example/mcp",
		"retry.err":     `Delete "http://host.example/mcp": connection refused`,
		"retry.retryIn": "2",
	}, entries[1].ContextMap())

	requireLogsExclude(t, logs, "SECRET")
}

// TestCreateClientMCPGoLogsRedacted verifies that mcp-go's own logs for an
// http downstream go to the gateway's logger, not slog.Default, with URLs
// redacted. Closing a client whose downstream has gone away makes mcp-go log
// the failed session DELETE, whose error quotes the URL and its token.
func TestCreateClientMCPGoLogsRedacted(t *testing.T) {
	ctx := context.Background()
	ts := httptest.NewServer(server.NewStreamableHTTPServer(newE2EDownstream("remote")))
	defer ts.Close()

	logger, logs := newObservedLogger()
	cm := NewClientManager(ctx, logger)
	cfg := config.ClientConfig{Type: "http", DownstreamURL: ts.URL + "/mcp?token=SECRET"}

	clientInfo, err := cm.createClient(ctx, "remote", cfg)
	require.NoError(t, err)

	ts.CloseClientConnections()
	ts.Close()
	_ = clientInfo.Client.Close()

	require.NotEmpty(t, logs.FilterMessage("failed to send close request").All(),
		"mcp-go's close error should reach the gateway logger")
	requireLogsExclude(t, logs, "SECRET")
}
