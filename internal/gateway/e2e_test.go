package gateway

// End-to-end tests that run the real gateway (config loading, HTTP server,
// session handling, policy chain, audit log) between a real MCP client and
// real downstream MCP servers over stdio and streamable HTTP.
//
// The stdio downstream is this test binary re-executed with
// e2eStdioServerArg, so the tests need no external tools (npx, etc.).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/maybedont/maybe-dont/internal/config"
)

// e2eStdioServerArg makes the test binary serve the e2e downstream over stdio
// instead of running tests. Checked in TestMain before flag parsing.
const e2eStdioServerArg = "maybedont-e2e-stdio-downstream"

// e2ePassThroughHeader is the caller header the pass-through downstream needs.
const e2ePassThroughHeader = "X-E2e-Token"

func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		if arg == e2eStdioServerArg {
			if err := server.ServeStdio(newE2EDownstream("stdio")); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

// newE2EDownstream builds a small MCP server with a tool, a prompt and a
// resource. Every reply names the downstream so tests can tell which server
// actually answered, and echo reports the pass-through header it received.
func newE2EDownstream(name string) *server.MCPServer {
	s := server.NewMCPServer("e2e-"+name, "1.0.0",
		server.WithToolCapabilities(false),
		server.WithPromptCapabilities(false),
		server.WithResourceCapabilities(false, false),
	)
	s.AddTool(
		mcp.NewTool("echo", mcp.WithString("message", mcp.Required())),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			msg := req.GetString("message", "")
			token := req.Header.Get(e2ePassThroughHeader)
			return mcp.NewToolResultText(fmt.Sprintf("%s echo: %s token=%q", name, msg, token)), nil
		},
	)
	s.AddTool(
		mcp.NewTool("blocked"),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText(name + " blocked tool ran"), nil
		},
	)
	s.AddPrompt(mcp.NewPrompt("greet"),
		func(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return mcp.NewGetPromptResult("greeting", []mcp.PromptMessage{
				mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(name+" says hello")),
			}), nil
		},
	)
	s.AddResource(mcp.NewResource("test://hello", "hello", mcp.WithMIMEType("text/plain")),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: req.Params.URI, MIMEType: "text/plain", Text: name + " resource",
			}}, nil
		},
	)
	return s
}

// e2eGateway is a running gateway plus where to reach it and its audit log.
type e2eGateway struct {
	url       string
	auditPath string
}

// startE2EGateway writes configYAML (with %s for the listen address) and a CEL
// rule file denying "*__blocked" into a temp config dir, loads it through the
// normal config loader and starts the gateway on a free port.
func startE2EGateway(t *testing.T, downstreamsYAML string) *e2eGateway {
	t.Helper()

	configDir := t.TempDir()
	logDir := t.TempDir()
	addr := freeLocalAddr(t)

	rules := `rules:
  - name: deny-blocked-tool
    expression: get(request.params, "name", "").endsWith("__blocked")
    action: deny
    message: blocked by e2e rule
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "cel_request_rules.yaml"), []byte(rules), 0o600))

	cfgYAML := fmt.Sprintf(`server:
  type: http
  listen_addr: %q
logger:
  level: error
audit:
  path: maybedont-audit.log
request_validation:
  cel:
    enabled: true
    mode: enforce
    rules_file: cel_request_rules.yaml
  ai:
    enabled: false
native_tools:
  audit_report:
    enabled: false
downstream_mcp_servers:
%s`, addr, downstreamsYAML)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "maybe-dont.yaml"), []byte(cfgYAML), 0o600))

	viper.Reset()
	t.Cleanup(viper.Reset)
	cfg, err := config.LoadConfig(configDir, "maybe-dont.yaml")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	gw, err := New(ctx, cfg, logger, "e2e", logDir)
	require.NoError(t, err)
	require.NoError(t, gw.Start(ctx))
	t.Cleanup(func() {
		cancel()
		_ = gw.Stop(context.Background())
	})

	url := "http://" + addr + "/mcp"
	waitForListener(t, addr)
	return &e2eGateway{url: url, auditPath: filepath.Join(logDir, "maybedont-audit.log")}
}

// connect opens an MCP session to the gateway the way an agent would,
// sending headers on every request. With no options the client probes for the
// newest protocol first and falls back, as current SDK clients do.
func (g *e2eGateway) connect(t *testing.T, headers map[string]string, opts ...client.ClientOption) *client.Client {
	t.Helper()
	tr, err := transport.NewStreamableHTTP(g.url, transport.WithHTTPHeaders(headers))
	require.NoError(t, err)
	c := client.NewClient(tr, opts...)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, c.Start(ctx))
	initReq := mcp.InitializeRequest{}
	initReq.Params.ClientInfo = mcp.Implementation{Name: "e2e-client", Version: "1.0.0"}
	_, err = c.Initialize(ctx, initReq)
	require.NoError(t, err)
	return c
}

// auditEntries returns the audit log lines that mention toolName.
func (g *e2eGateway) auditEntries(t *testing.T, toolName string) []map[string]any {
	t.Helper()
	f, err := os.Open(g.auditPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var entries []map[string]any
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if !strings.Contains(scanner.Text(), toolName) {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &entry))
		entries = append(entries, entry)
	}
	require.NoError(t, scanner.Err())
	return entries
}

func freeLocalAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func waitForListener(t *testing.T, addr string) {
	t.Helper()
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 5*time.Second, 20*time.Millisecond)
}

func stdioDownstreamYAML(t *testing.T, name string) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return fmt.Sprintf(`  %s:
    type: stdio
    command: %q
    args: [%q]
    capability_discovery_delay_ms: 1
`, name, exe, e2eStdioServerArg)
}

func httpDownstreamYAML(t *testing.T, name string, passThrough bool) string {
	t.Helper()
	ts := httptest.NewServer(server.NewStreamableHTTPServer(newE2EDownstream(name)))
	t.Cleanup(ts.Close)
	y := fmt.Sprintf(`  %s:
    type: http
    url: %q
`, name, ts.URL+"/mcp")
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

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected text content, got %T", res.Content[0])
	return text.Text
}

func callTool(ctx context.Context, c *client.Client, name string, args map[string]any) (*mcp.CallToolResult, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return c.CallTool(ctx, req)
}

// TestE2E_DownstreamRouting verifies that a caller can list and use tools,
// prompts and resources on every downstream type through the gateway, in a
// fresh session, with and without pass-through clients configured. Calls must
// succeed without first calling maybedont__discover_tools or tools/list.
func TestE2E_DownstreamRouting(t *testing.T) {
	tests := []struct {
		name string
		// downstreams returns the downstream_mcp_servers YAML block.
		downstreams func(t *testing.T) string
		headers     map[string]string
		// listFirst calls tools/list before anything else, as most agents do.
		listFirst bool
		// wantEcho maps tool name to the substring its reply must contain.
		wantEcho map[string]string
		// blockedClient names a downstream whose "blocked" tool the policy
		// must deny.
		blockedClient string
		// promptClient names a downstream whose prompt and resource must be
		// reachable. Pass-through clients only proxy tools today.
		promptClient string
	}{
		{
			name:          "stdio only, no pass-through configured, call without listing",
			downstreams:   func(t *testing.T) string { return stdioDownstreamYAML(t, "local") },
			wantEcho:      map[string]string{"local__echo": "stdio echo: hi"},
			blockedClient: "local",
			promptClient:  "local",
		},
		{
			name:          "stdio only, list then call",
			downstreams:   func(t *testing.T) string { return stdioDownstreamYAML(t, "local") },
			listFirst:     true,
			wantEcho:      map[string]string{"local__echo": "stdio echo: hi"},
			blockedClient: "local",
			promptClient:  "local",
		},
		{
			name: "stdio and static http, no pass-through configured",
			downstreams: func(t *testing.T) string {
				return stdioDownstreamYAML(t, "local") + httpDownstreamYAML(t, "remote", false)
			},
			listFirst: true,
			wantEcho: map[string]string{
				"local__echo":  "stdio echo: hi",
				"remote__echo": "remote echo: hi",
			},
			blockedClient: "remote",
			promptClient:  "remote",
		},
		{
			name: "pass-through configured, caller sends no credentials, non-pass-through still works",
			downstreams: func(t *testing.T) string {
				return stdioDownstreamYAML(t, "local") + httpDownstreamYAML(t, "secure", true)
			},
			listFirst:     true,
			wantEcho:      map[string]string{"local__echo": "stdio echo: hi"},
			blockedClient: "local",
			promptClient:  "local",
		},
		{
			name: "pass-through configured, caller sends credentials, both work and header is forwarded",
			downstreams: func(t *testing.T) string {
				return stdioDownstreamYAML(t, "local") + httpDownstreamYAML(t, "secure", true)
			},
			headers:   map[string]string{e2ePassThroughHeader: "s3cret"},
			listFirst: true,
			wantEcho: map[string]string{
				"local__echo":  "stdio echo: hi",
				"secure__echo": `secure echo: hi token="s3cret"`,
			},
			blockedClient: "secure",
			promptClient:  "local",
		},
		{
			name: "pass-through configured, caller sends credentials, call without listing",
			downstreams: func(t *testing.T) string {
				return httpDownstreamYAML(t, "secure", true)
			},
			headers:       map[string]string{e2ePassThroughHeader: "s3cret"},
			wantEcho:      map[string]string{"secure__echo": `secure echo: hi token="s3cret"`},
			blockedClient: "secure",
			promptClient:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := startE2EGateway(t, tt.downstreams(t))
			c := gw.connect(t, tt.headers)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			if tt.listFirst {
				tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
				require.NoError(t, err)
				names := make([]string, 0, len(tools.Tools))
				for _, tool := range tools.Tools {
					names = append(names, tool.Name)
				}
				for toolName := range tt.wantEcho {
					require.Contains(t, names, toolName)
				}
			}

			for toolName, want := range tt.wantEcho {
				res, err := callTool(ctx, c, toolName, map[string]any{"message": "hi"})
				require.NoError(t, err, "calling %s", toolName)
				require.False(t, res.IsError, "calling %s: %s", toolName, toolText(t, res))
				require.Contains(t, toolText(t, res), want)

				entries := gw.auditEntries(t, toolName)
				require.NotEmpty(t, entries, "no audit entry for %s", toolName)
			}

			// A policy denial still applies to downstream tools.
			blocked := tt.blockedClient + "__blocked"
			res, err := callTool(ctx, c, blocked, nil)
			if err == nil {
				require.True(t, res.IsError, "expected %s to be denied", blocked)
				require.Contains(t, toolText(t, res), "blocked by e2e rule")
			} else {
				require.Contains(t, err.Error(), "blocked by e2e rule")
			}
			require.NotEmpty(t, gw.auditEntries(t, blocked), "no audit entry for denied %s", blocked)

			if tt.promptClient == "" {
				return
			}
			promptReq := mcp.GetPromptRequest{}
			promptReq.Params.Name = tt.promptClient + "__greet"
			prompt, err := c.GetPrompt(ctx, promptReq)
			require.NoError(t, err)
			require.NotEmpty(t, prompt.Messages)

			resReq := mcp.ReadResourceRequest{}
			resReq.Params.URI = tt.promptClient + "__test://hello"
			resource, err := c.ReadResource(ctx, resReq)
			require.NoError(t, err)
			require.NotEmpty(t, resource.Contents)
		})
	}
}

// TestE2E_PassThroughClientWithoutCredentials verifies that a caller without
// a pass-through client's credential header can't reach it, both before any
// session has discovered its tools and after another session has.
func TestE2E_PassThroughClientWithoutCredentials(t *testing.T) {
	gw := startE2EGateway(t, httpDownstreamYAML(t, "secure", true))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	requireRefused := func(t *testing.T, c *client.Client, wantErr string) {
		t.Helper()
		res, err := callTool(ctx, c, "secure__echo", map[string]any{"message": "hi"})
		if err == nil {
			require.True(t, res.IsError)
			require.Contains(t, toolText(t, res), wantErr)
			require.NotContains(t, toolText(t, res), "secure echo")
		} else {
			require.Contains(t, err.Error(), wantErr)
		}
	}

	// No session has discovered the pass-through tools, so they don't exist.
	requireRefused(t, gw.connect(t, nil), "tool not found")

	// Another session discovers them with credentials.
	withCreds := gw.connect(t, map[string]string{e2ePassThroughHeader: "s3cret"})
	res, err := callTool(ctx, withCreds, "secure__echo", map[string]any{"message": "hi"})
	require.NoError(t, err)
	require.Contains(t, toolText(t, res), `token="s3cret"`)

	// A session without credentials gets the guidance error, not the
	// other session's connection.
	requireRefused(t, gw.connect(t, nil), "not connected for this session")
}

// TestE2E_ProtocolNegotiation verifies that clients end up on the newest
// session-based protocol whether or not they probe for the stateless
// 2026-07-28 protocol first, and that downstream calls work either way.
func TestE2E_ProtocolNegotiation(t *testing.T) {
	tests := []struct {
		name string
		opts []client.ClientOption
	}{
		{name: "client probes server/discover first"},
		// Deprecated upstream, but it's how older clients behave.
		{name: "legacy-only client", opts: []client.ClientOption{client.WithLegacyProtocolOnly()}}, //nolint:staticcheck
		{name: "client pinned to 2025-03-26", opts: []client.ClientOption{client.WithProtocolVersion(mcp.ProtocolVersion20250326)}},
	}

	gw := startE2EGateway(t, stdioDownstreamYAML(t, "local"))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := gw.connect(t, nil, tt.opts...)
			require.False(t, mcp.IsModernProtocol(c.ProtocolVersion()), "negotiated %s", c.ProtocolVersion())

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			res, err := callTool(ctx, c, "local__echo", map[string]any{"message": "hi"})
			require.NoError(t, err)
			require.False(t, res.IsError, toolText(t, res))
			require.Contains(t, toolText(t, res), "stdio echo: hi")
		})
	}
}
