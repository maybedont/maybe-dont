package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

// e2eURLSecret is put in downstream URLs' query strings so tests can check it
// never reaches the agent.
const e2eURLSecret = "SECRET"

// e2eDownstreamRejection is what the "reject" tool's JSON-RPC error says.
const e2eDownstreamRejection = "message must not be empty"

// startSecretURLDownstream serves newE2EDownstream(name), plus a "reject" tool
// that answers with a JSON-RPC error, and returns the server and YAML that
// configures it with a credential in its URL's query string.
func startSecretURLDownstream(t *testing.T, name string, passThrough bool) (*httptest.Server, string) {
	t.Helper()
	mcpServer := newE2EDownstream(name)
	mcpServer.AddTool(mcp.NewTool("reject"),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, fmt.Errorf("%w: %s", mcp.ErrInvalidParams, e2eDownstreamRejection)
		},
	)
	ts := httptest.NewServer(server.NewStreamableHTTPServer(mcpServer))
	t.Cleanup(ts.Close)
	return ts, secretURLDownstreamYAML(name, ts.URL, passThrough)
}

func secretURLDownstreamYAML(name, baseURL string, passThrough bool) string {
	y := fmt.Sprintf(`  %s:
    type: http
    url: %q
`, name, baseURL+"/mcp?token="+e2eURLSecret)
	if passThrough {
		y += fmt.Sprintf(`    auth:
      pass_through:
        enabled: true
        headers:
          - source_header: %q
            target_header: %q
`, e2ePassThroughHeader, e2ePassThroughHeader)
	}
	return y
}

// requireSafeError checks that err is the expected message and carries neither
// the URL secret nor the downstream's address.
func requireSafeError(t *testing.T, err error, want, downstreamAddr string) {
	t.Helper()
	require.Error(t, err)
	require.Contains(t, err.Error(), want)
	require.NotContains(t, err.Error(), e2eURLSecret)
	require.NotContains(t, err.Error(), downstreamAddr)
	require.NotContains(t, err.Error(), "127.0.0.1")
}

// TestE2E_DownstreamFailureAfterConnect verifies that when a connected http
// downstream goes away, the tool call, prompt get or resource read that hits
// the transport failure returns a generic message to the agent, not the raw
// error with the downstream URL and the credential in its query string. A
// new session then fails to connect and gets the generic connect message.
func TestE2E_DownstreamFailureAfterConnect(t *testing.T) {
	const wantCallFailed = "call to downstream server 'remote' failed; see the gateway log for details"
	const wantConnectFailed = "failed to connect to downstream server 'remote'; see the gateway log for details"

	tests := []struct {
		name string
		call func(ctx context.Context, c *client.Client) error
	}{
		{
			name: "tool call",
			call: func(ctx context.Context, c *client.Client) error {
				_, err := callTool(ctx, c, "remote__echo", map[string]any{"message": "hi"})
				return err
			},
		},
		{
			name: "prompt get",
			call: func(ctx context.Context, c *client.Client) error {
				req := mcp.GetPromptRequest{}
				req.Params.Name = "remote__greet"
				_, err := c.GetPrompt(ctx, req)
				return err
			},
		},
		{
			name: "resource read",
			call: func(ctx context.Context, c *client.Client) error {
				req := mcp.ReadResourceRequest{}
				req.Params.URI = "remote__test://hello"
				_, err := c.ReadResource(ctx, req)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, yaml := startSecretURLDownstream(t, "remote", false)
			gw := startE2EGateway(t, yaml)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			c := gw.connect(t, nil)
			require.NoError(t, tt.call(ctx, c), "call before the downstream closed")

			ts.CloseClientConnections()
			ts.Close()

			requireSafeError(t, tt.call(ctx, c), wantCallFailed, ts.Listener.Addr().String())
			requireSafeError(t, tt.call(ctx, gw.connect(t, nil)), wantConnectFailed, ts.Listener.Addr().String())
		})
	}
}

// TestE2E_PassThroughConnectFailureIsSafe verifies that when a request with
// pass-through credentials makes the gateway connect a downstream that can't
// be reached (the request-initialization hook path), the agent gets the
// generic connect message, not the URL or the credential in its query string.
func TestE2E_PassThroughConnectFailureIsSafe(t *testing.T) {
	addr := freeLocalAddr(t)
	gw := startE2EGateway(t, secretURLDownstreamYAML("secure", "http://"+addr, true))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := gw.connect(t, map[string]string{e2ePassThroughHeader: "s3cret"})
	_, err := callTool(ctx, c, "secure__echo", map[string]any{"message": "hi"})
	requireSafeError(t, err, "failed to connect to downstream server 'secure'; see the gateway log for details", addr)
}

// TestE2E_DownstreamJSONRPCErrorPassesThrough verifies that a JSON-RPC error
// the downstream returns for a tool call reaches the agent with the
// downstream's own message, since that text is the server's answer and not a
// gateway transport failure.
func TestE2E_DownstreamJSONRPCErrorPassesThrough(t *testing.T) {
	_, yaml := startSecretURLDownstream(t, "remote", false)
	gw := startE2EGateway(t, yaml)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := callTool(ctx, gw.connect(t, nil), "remote__reject", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), e2eDownstreamRejection)
	require.True(t, errors.Is(err, mcp.ErrInvalidParams) || errors.Is(err, mcp.ErrInternalError), err.Error())
	require.NotContains(t, err.Error(), "see the gateway log")
}
