package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNotifySession implements server.ClientSession for testing notifications.
type fakeNotifySession struct {
	id          string
	initialized bool
	notifyCh    chan mcp.JSONRPCNotification
}

func (f *fakeNotifySession) SessionID() string                                   { return f.id }
func (f *fakeNotifySession) Initialize()                                         { f.initialized = true }
func (f *fakeNotifySession) Initialized() bool                                   { return f.initialized }
func (f *fakeNotifySession) NotificationChannel() chan<- mcp.JSONRPCNotification { return f.notifyCh }

// TestDiscoverPassThroughTools_SendsToolsListChangedNotification verifies that
// after discover_tools completes (even when clients are already connected),
// the gateway sends a tools/list_changed notification to the requesting session.
// This ensures the MCP client refreshes its tool list, which is critical after
// transport reconnects where the client may have lost its tool registrations.
func TestDiscoverPassThroughTools_SendsToolsListChangedNotification(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	sessionID := "test-session-notify"

	// Create a pass-through client config
	githubConfig := config.ClientConfig{
		Type:    "stdio",
		Command: "echo",
	}
	githubConfig.Auth.PassThrough.Enabled = true
	githubConfig.Auth.PassThrough.Headers = []config.CredentialMapping{
		{SourceHeader: "Authorization", TargetHeader: "Authorization"},
	}

	// Set up client manager with a pass-through client
	cm := NewClientManager(ctx, logger)
	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"github": githubConfig,
	})
	require.NoError(t, err)

	// Create a session and simulate an already-connected client
	session, err := cm.sessionManager.CreateSession(sessionID)
	require.NoError(t, err)
	session.SetClient("github", &SessionClientInfo{
		Client: &client.Client{}, // Non-nil to satisfy the already-connected check
		Tools:  []mcp.Tool{{Name: "create_issue"}},
	})

	// Create an MCP server with tool capabilities enabled
	srv := server.NewMCPServer("test-gateway", "1.0.0",
		server.WithToolCapabilities(true),
	)

	// Create a fake session with a notification channel and put it in context
	notifyCh := make(chan mcp.JSONRPCNotification, 10)
	fakeSession := &fakeNotifySession{
		id:          sessionID,
		initialized: true,
		notifyCh:    notifyCh,
	}
	ctxWithSession := srv.WithContext(ctx, fakeSession)

	// Build a minimal Gateway with the server and client manager
	gw := &Gateway{
		logger:        logger,
		server:        srv,
		clientManager: cm,
		config:        &config.Config{},
	}

	// Call DiscoverPassThroughTools — github should be "already_connected"
	result, err := gw.DiscoverPassThroughTools(ctxWithSession, sessionID, "github")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.AlreadyConnected, "github",
		"github should be reported as already connected")

	// Verify that a tools/list_changed notification was sent to the session
	select {
	case notification := <-notifyCh:
		assert.Equal(t, string(mcp.MethodNotificationToolsListChanged), notification.Method,
			"notification method should be tools/list_changed")
	case <-time.After(time.Second):
		t.Fatal("expected tools/list_changed notification but none was received")
	}

	// Verify that tool handlers were registered globally for the already-connected client.
	// This is critical after a gateway restart — the session has reconnected clients but
	// the MCP server may not have handlers registered for their tools.
	registeredTool := srv.GetTool(PrefixName("github", "create_issue"))
	assert.NotNil(t, registeredTool,
		"tool handler should be registered globally for already-connected client tools")
}

// TestDiscoverPassThroughTools_NoNotificationWhenNoClients verifies that
// no notification is sent when there are no pass-through clients to discover.
func TestDiscoverPassThroughTools_NoNotificationWhenNoClients(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	sessionID := "test-session-no-clients"

	// Set up client manager with NO pass-through clients
	cm := NewClientManager(ctx, logger)
	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{
		"local": {
			Type:    "stdio",
			Command: "echo",
			// No pass-through auth
		},
	})
	require.NoError(t, err)

	srv := server.NewMCPServer("test-gateway", "1.0.0",
		server.WithToolCapabilities(true),
	)

	notifyCh := make(chan mcp.JSONRPCNotification, 10)
	fakeSession := &fakeNotifySession{
		id:          sessionID,
		initialized: true,
		notifyCh:    notifyCh,
	}
	ctxWithSession := srv.WithContext(ctx, fakeSession)

	gw := &Gateway{
		logger:        logger,
		server:        srv,
		clientManager: cm,
		config:        &config.Config{},
	}

	// No pass-through clients configured, so nothing to discover
	result, err := gw.DiscoverPassThroughTools(ctxWithSession, sessionID, "")
	require.NoError(t, err)
	assert.Empty(t, result.AlreadyConnected)
	assert.Empty(t, result.DiscoveredClients)

	// Verify no notification was sent
	select {
	case notification := <-notifyCh:
		t.Fatalf("unexpected notification received: %s", notification.Method)
	case <-time.After(100 * time.Millisecond):
		// Expected: no notification
	}
}
