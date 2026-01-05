package gateway

import (
	"context"
	"sync"
	"testing"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// newTestLogger creates a test logger for use in tests
func newTestLogger(t *testing.T) *config.SessionLogger {
	logger := zaptest.NewLogger(t)
	return config.NewSessionLogger(logger)
}

func TestSessionManager_CreateSession(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Create a session
	session := sm.CreateSession("session-1")
	require.NotNil(t, session)
	assert.Equal(t, "session-1", session.ID)

	// Verify session exists
	assert.True(t, sm.HasSession("session-1"))

	// Verify we can retrieve it
	retrieved, ok := sm.GetSession("session-1")
	assert.True(t, ok)
	assert.Equal(t, session, retrieved)
}

func TestSessionManager_MultipleSessionsAreIsolated(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Create two sessions
	session1 := sm.CreateSession("session-1")
	session2 := sm.CreateSession("session-2")

	// Add clients to each session
	client1 := &SessionClientInfo{Name: "client-for-session-1"}
	client2 := &SessionClientInfo{Name: "client-for-session-2"}

	session1.SetClient("test-client", client1)
	session2.SetClient("test-client", client2)

	// Verify clients are isolated per session
	retrieved1, ok := session1.GetClient("test-client")
	assert.True(t, ok)
	assert.Equal(t, "client-for-session-1", retrieved1.Name)

	retrieved2, ok := session2.GetClient("test-client")
	assert.True(t, ok)
	assert.Equal(t, "client-for-session-2", retrieved2.Name)

	// Verify they're different instances
	assert.NotEqual(t, retrieved1, retrieved2)
}

func TestSessionManager_DeleteSession(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)
	ctx := context.Background()

	// Create a session
	sm.CreateSession("session-1")
	assert.True(t, sm.HasSession("session-1"))

	// Delete it
	err := sm.DeleteSession(ctx, "session-1")
	require.NoError(t, err)

	// Verify it's gone
	assert.False(t, sm.HasSession("session-1"))
}

func TestSessionManager_GetSessionClient(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Create session and add client
	sm.CreateSession("session-1")
	clientInfo := &SessionClientInfo{Name: "aws"}
	sm.SetSessionClient("session-1", "aws", clientInfo)

	// Retrieve client
	retrieved, ok := sm.GetSessionClient("session-1", "aws")
	assert.True(t, ok)
	assert.Equal(t, "aws", retrieved.Name)

	// Try to get non-existent client
	_, ok = sm.GetSessionClient("session-1", "non-existent")
	assert.False(t, ok)

	// Try to get client from non-existent session
	_, ok = sm.GetSessionClient("non-existent", "aws")
	assert.False(t, ok)
}

func TestSessionManager_ConcurrentAccess(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Concurrent session creation
	var wg sync.WaitGroup
	numSessions := 100

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sessionID := string(rune('a'+id%26)) + "-" + string(rune('0'+id%10))
			sm.CreateSession(sessionID)
			sm.SetSessionClient(sessionID, "client", &SessionClientInfo{Name: "test"})
		}(i)
	}

	wg.Wait()

	// Verify all sessions were created (some may have same ID due to modulo)
	sessions := sm.GetAllSessions()
	assert.Greater(t, len(sessions), 0)
}

func TestSession_ClientIsolation(t *testing.T) {
	session := NewSession("test-session")

	// Add multiple clients
	session.SetClient("aws", &SessionClientInfo{Name: "aws-client"})
	session.SetClient("github", &SessionClientInfo{Name: "github-client"})

	// Retrieve and verify
	awsClient, ok := session.GetClient("aws")
	assert.True(t, ok)
	assert.Equal(t, "aws-client", awsClient.Name)

	githubClient, ok := session.GetClient("github")
	assert.True(t, ok)
	assert.Equal(t, "github-client", githubClient.Name)

	// Get all clients
	allClients := session.GetAllClients()
	assert.Len(t, allClients, 2)
}

func TestClientManager_CreateSessionClients(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)

	// Note: We can't easily test actual client creation without a real MCP server
	// But we can test that the session is created and configs are stored

	// Store a config (this simulates InitializeClients)
	configs := map[string]config.ClientConfig{
		"test-server": {
			Type:    "stdio",
			Command: "echo", // Simple command that exits immediately
		},
	}

	err := cm.InitializeClients(ctx, configs)
	require.NoError(t, err)

	// Verify configs are stored
	storedConfigs := cm.GetClientConfigs()
	assert.Len(t, storedConfigs, 1)
	assert.Contains(t, storedConfigs, "test-server")
}

func TestClientManager_SessionClientRetrieval(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)

	// Manually create a session with a client (bypassing actual MCP connection)
	cm.sessionManager.CreateSession("test-session")
	cm.sessionManager.SetSessionClient("test-session", "aws", &SessionClientInfo{
		Name: "aws",
		Config: config.ClientConfig{
			Type: "http",
		},
	})

	// Retrieve the client
	clientInfo, err := cm.GetSessionClient("test-session", "aws")
	require.NoError(t, err)
	assert.Equal(t, "aws", clientInfo.Name)

	// Try to get from non-existent session
	_, err = cm.GetSessionClient("non-existent", "aws")
	assert.Error(t, err)

	// Try to get non-existent client
	_, err = cm.GetSessionClient("test-session", "non-existent")
	assert.Error(t, err)
}

func TestClientManager_CloseSessionClients(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)

	// Create a session
	cm.sessionManager.CreateSession("test-session")
	cm.sessionManager.SetSessionClient("test-session", "test", &SessionClientInfo{
		Name:   "test",
		Client: nil, // No actual client to close
	})

	// Verify session exists
	assert.True(t, cm.sessionManager.HasSession("test-session"))

	// Close session clients
	err := cm.CloseSessionClients(ctx, "test-session")
	require.NoError(t, err)

	// Verify session is gone
	assert.False(t, cm.sessionManager.HasSession("test-session"))
}

func TestMultipleUpstreamSessions_IsolatedDownstreamClients(t *testing.T) {
	// This test verifies the core behavior: each upstream session
	// gets its own set of downstream clients

	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)

	// Simulate two upstream sessions connecting
	session1ID := "upstream-session-1"
	session2ID := "upstream-session-2"

	// Create sessions (simulating what onSessionRegister does)
	cm.sessionManager.CreateSession(session1ID)
	cm.sessionManager.CreateSession(session2ID)

	// Add different downstream clients to each session
	// (simulating what CreateSessionClients would do)
	cm.sessionManager.SetSessionClient(session1ID, "aws", &SessionClientInfo{
		Name: "aws-for-session-1",
	})
	cm.sessionManager.SetSessionClient(session1ID, "github", &SessionClientInfo{
		Name: "github-for-session-1",
	})

	cm.sessionManager.SetSessionClient(session2ID, "aws", &SessionClientInfo{
		Name: "aws-for-session-2",
	})
	cm.sessionManager.SetSessionClient(session2ID, "github", &SessionClientInfo{
		Name: "github-for-session-2",
	})

	// Verify each session has its own isolated clients
	aws1, err := cm.GetSessionClient(session1ID, "aws")
	require.NoError(t, err)
	assert.Equal(t, "aws-for-session-1", aws1.Name)

	aws2, err := cm.GetSessionClient(session2ID, "aws")
	require.NoError(t, err)
	assert.Equal(t, "aws-for-session-2", aws2.Name)

	// Verify they're different instances
	assert.NotEqual(t, aws1, aws2)

	// Close session 1
	err = cm.CloseSessionClients(ctx, session1ID)
	require.NoError(t, err)

	// Session 1 clients should be gone
	_, err = cm.GetSessionClient(session1ID, "aws")
	assert.Error(t, err)

	// Session 2 clients should still exist
	aws2Again, err := cm.GetSessionClient(session2ID, "aws")
	require.NoError(t, err)
	assert.Equal(t, "aws-for-session-2", aws2Again.Name)
}

func TestGenerateRequestID(t *testing.T) {
	// Generate multiple request IDs and verify they're unique
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := GenerateRequestID()
		require.NoError(t, err)
		assert.Len(t, id, 32) // 16 bytes = 32 hex characters
		assert.False(t, ids[id], "Generated duplicate ID")
		ids[id] = true
	}
}
