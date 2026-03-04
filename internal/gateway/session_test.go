package gateway

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// TestSessionManager_CreateSession_Idempotent verifies that CreateSession returns the
// existing session when called with the same ID, preserving downstream clients and metadata.
// This prevents onSessionRegister (SDK re-registration) from wiping out an active session.
func TestSessionManager_CreateSession_Idempotent(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Create session and populate it with metadata and a downstream client
	session := sm.CreateSession("session-1")
	session.SetClientIP("192.168.1.100")
	session.SetUserAgent("Claude-Code/1.0.0")
	session.SetClient("github", &SessionClientInfo{Name: "github"})

	// Call CreateSession again with the same ID — should return existing session
	second := sm.CreateSession("session-1")

	// Must be the exact same session object, not a new one
	assert.Same(t, session, second)

	// All metadata and clients must be preserved
	assert.Equal(t, "192.168.1.100", second.GetClientIP())
	assert.Equal(t, "Claude-Code/1.0.0", second.GetUserAgent())
	client, ok := second.GetClient("github")
	require.True(t, ok)
	assert.Equal(t, "github", client.Name)
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
	ok := cm.sessionManager.SetSessionClient("test-session", "test", &SessionClientInfo{
		Name:   "test",
		Client: nil, // No actual client to close
	})
	require.True(t, ok, "SetSessionClient should succeed for active session")

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
	require.True(t, cm.sessionManager.SetSessionClient(session1ID, "aws", &SessionClientInfo{
		Name: "aws-for-session-1",
	}))
	require.True(t, cm.sessionManager.SetSessionClient(session1ID, "github", &SessionClientInfo{
		Name: "github-for-session-1",
	}))

	require.True(t, cm.sessionManager.SetSessionClient(session2ID, "aws", &SessionClientInfo{
		Name: "aws-for-session-2",
	}))
	require.True(t, cm.sessionManager.SetSessionClient(session2ID, "github", &SessionClientInfo{
		Name: "github-for-session-2",
	}))

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

func TestSession_SetClientRejectsAfterClose(t *testing.T) {
	// Test that SetClient rejects new clients after session is closed
	session := NewSession("test-session")

	// Add a client before closing - should succeed
	ok := session.SetClient("client1", &SessionClientInfo{Name: "client1"})
	assert.True(t, ok, "SetClient should succeed before close")

	// Close the session
	err := session.Close()
	require.NoError(t, err)

	// Try to add a client after closing - should be rejected
	ok = session.SetClient("client2", &SessionClientInfo{Name: "client2"})
	assert.False(t, ok, "SetClient should be rejected after close")

	// Verify client2 was not added
	_, exists := session.GetClient("client2")
	assert.False(t, exists, "client2 should not exist after rejected SetClient")
}

func TestSessionManager_SetSessionClientRejectsDeletedSession(t *testing.T) {
	// Test that SetSessionClient rejects clients for deleted sessions
	ctx := context.Background()
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Create a session
	sm.CreateSession("test-session")

	// Add a client - should succeed
	ok := sm.SetSessionClient("test-session", "client1", &SessionClientInfo{Name: "client1"})
	assert.True(t, ok, "SetSessionClient should succeed for existing session")

	// Delete the session
	err := sm.DeleteSession(ctx, "test-session")
	require.NoError(t, err)

	// Try to add a client to deleted session - should be rejected
	ok = sm.SetSessionClient("test-session", "client2", &SessionClientInfo{Name: "client2"})
	assert.False(t, ok, "SetSessionClient should be rejected for deleted session")

	// Verify session was not recreated
	assert.False(t, sm.HasSession("test-session"), "Session should not be recreated")
}

func TestSessionManager_SetSessionClientRejectsClosingSession(t *testing.T) {
	// Test that SetSessionClient rejects clients for sessions that are closing
	// This simulates the race condition where async discovery completes after
	// DeleteSession has started but before it completes
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Create a session
	session := sm.CreateSession("test-session")

	// Manually mark session as closing (simulates DeleteSession in progress)
	session.mu.Lock()
	session.closing = true
	session.mu.Unlock()

	// Try to add a client to closing session - should be rejected
	ok := sm.SetSessionClient("test-session", "client1", &SessionClientInfo{Name: "client1"})
	assert.False(t, ok, "SetSessionClient should be rejected for closing session")
}

func TestSessionCleanup_RaceCondition(t *testing.T) {
	// This test simulates the race condition scenario:
	// 1. Async goroutine creates a downstream client
	// 2. Session is deleted before the client can be stored
	// 3. SetSessionClient should reject the client (returning false)
	// 4. Caller is responsible for closing the orphaned client
	//
	// This ensures no resource leaks when sessions are deleted during async discovery.

	ctx := context.Background()
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Create and immediately delete a session
	sm.CreateSession("test-session")
	err := sm.DeleteSession(ctx, "test-session")
	require.NoError(t, err)

	// Simulate async discovery completing after session deletion
	// The SetSessionClient call should be rejected
	ok := sm.SetSessionClient("test-session", "orphaned-client", &SessionClientInfo{
		Name:   "orphaned-client",
		Client: nil, // In real scenario this would be an actual client that needs closing
	})

	assert.False(t, ok, "SetSessionClient should reject client for deleted session")
	assert.False(t, sm.HasSession("test-session"), "Session should not be recreated by SetSessionClient")
}

func TestSessionCleanup_ConcurrentDeleteAndSetClient(t *testing.T) {
	// Test concurrent DeleteSession and SetSessionClient calls
	// This tests thread-safety of the closing flag mechanism

	ctx := context.Background()
	logger := newTestLogger(t)
	sm := NewSessionManager(logger)

	// Create session
	sm.CreateSession("test-session")

	// Run concurrent operations
	const numGoroutines = 100
	var wg sync.WaitGroup
	deleteDone := make(chan struct{})

	// Start goroutine that will delete the session
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Small delay to let some SetSessionClient calls start
		time.Sleep(time.Millisecond)
		_ = sm.DeleteSession(ctx, "test-session")
		close(deleteDone)
	}()

	// Start goroutines that try to set clients
	rejectedCount := int32(0)
	acceptedCount := int32(0)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			clientName := fmt.Sprintf("client-%d", idx)
			ok := sm.SetSessionClient("test-session", clientName, &SessionClientInfo{
				Name:   clientName,
				Client: nil,
			})
			if ok {
				atomic.AddInt32(&acceptedCount, 1)
			} else {
				atomic.AddInt32(&rejectedCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Verify session is gone
	assert.False(t, sm.HasSession("test-session"))

	// Some calls may have succeeded (before delete), some may have been rejected (after delete)
	// The exact counts depend on timing, but we should have processed all of them
	assert.Equal(t, int32(numGoroutines), acceptedCount+rejectedCount,
		"All SetSessionClient calls should complete (accepted or rejected)")

	t.Logf("Accepted: %d, Rejected: %d", acceptedCount, rejectedCount)
}

func TestSession_ActivityTracking(t *testing.T) {
	session := NewSession("test-session")

	// New session should have recent activity
	initialActivity := session.LastActivity()
	assert.WithinDuration(t, time.Now(), initialActivity, time.Second)

	// Wait a bit and touch activity
	time.Sleep(10 * time.Millisecond)
	session.TouchActivity()

	// Activity should be updated
	newActivity := session.LastActivity()
	assert.True(t, newActivity.After(initialActivity))
}

func TestSession_ClientIPAndUserAgent(t *testing.T) {
	session := NewSession("test-session")

	// New session should have empty ClientIP and UserAgent
	assert.Empty(t, session.GetClientIP())
	assert.Empty(t, session.GetUserAgent())

	// Set ClientIP and UserAgent
	session.SetClientIP("192.168.1.100")
	session.SetUserAgent("Claude-Code/1.0.0")

	// Verify values are stored and retrieved correctly
	assert.Equal(t, "192.168.1.100", session.GetClientIP())
	assert.Equal(t, "Claude-Code/1.0.0", session.GetUserAgent())

	// Update values
	session.SetClientIP("10.0.0.50")
	session.SetUserAgent("MCP-Client/2.0")

	// Verify updated values
	assert.Equal(t, "10.0.0.50", session.GetClientIP())
	assert.Equal(t, "MCP-Client/2.0", session.GetUserAgent())
}

func TestSession_IsExpired(t *testing.T) {
	session := NewSession("test-session")

	// New session should not be expired
	assert.False(t, session.IsExpired(time.Hour))
	assert.False(t, session.IsExpired(time.Second))

	// Wait and check expiration with very short timeout
	time.Sleep(20 * time.Millisecond)
	assert.True(t, session.IsExpired(10*time.Millisecond))
	assert.False(t, session.IsExpired(time.Hour))
}

func TestSessionManager_CleanupExpiredSessions(t *testing.T) {
	logger := newTestLogger(t)
	// Use a very short timeout for testing
	timeout := 100 * time.Millisecond
	sm := NewSessionManagerWithTimeout(logger, timeout)
	defer sm.StopCleanup()

	// Create two sessions
	session1 := sm.CreateSession("session-1")
	sm.CreateSession("session-2")

	// Wait past the timeout
	time.Sleep(150 * time.Millisecond)

	// Touch session-1 to keep it alive (reset its activity)
	session1.TouchActivity()

	// Trigger cleanup - session-2 should expire, session-1 should stay
	sm.cleanupExpiredSessions()

	// session-1 should still exist (was just touched)
	assert.True(t, sm.HasSession("session-1"), "session-1 should still exist after touch")

	// session-2 should be gone (not touched since creation, past timeout)
	assert.False(t, sm.HasSession("session-2"), "session-2 should have been cleaned up")
}

func TestSessionManager_GetSessionTouchesActivity(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManagerWithTimeout(logger, time.Hour)
	defer sm.StopCleanup()

	// Create a session
	session := sm.CreateSession("test-session")
	initialActivity := session.LastActivity()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Get the session - this should touch activity
	retrieved, ok := sm.GetSession("test-session")
	require.True(t, ok)
	require.NotNil(t, retrieved)

	// Activity should be updated
	assert.True(t, session.LastActivity().After(initialActivity))
}

func TestSessionManager_GetSessionClientTouchesActivity(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManagerWithTimeout(logger, time.Hour)
	defer sm.StopCleanup()

	// Create a session and add a client
	session := sm.CreateSession("test-session")
	clientInfo := &SessionClientInfo{Name: "test-client"}
	session.SetClient("test-client", clientInfo)

	initialActivity := session.LastActivity()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Get the client - this should touch activity
	retrieved, ok := sm.GetSessionClient("test-session", "test-client")
	require.True(t, ok)
	require.NotNil(t, retrieved)

	// Activity should be updated
	assert.True(t, session.LastActivity().After(initialActivity))
}

func TestSessionManager_StopCleanup(t *testing.T) {
	logger := newTestLogger(t)
	sm := NewSessionManagerWithTimeout(logger, time.Hour)

	// Stop cleanup should not block
	done := make(chan struct{})
	go func() {
		sm.StopCleanup()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("StopCleanup blocked for too long")
	}
}

func TestSessionManager_GetSessionTimeout(t *testing.T) {
	logger := newTestLogger(t)

	// Test default timeout
	sm1 := NewSessionManager(logger)
	defer sm1.StopCleanup()
	assert.Equal(t, DefaultSessionTimeout, sm1.GetSessionTimeout())

	// Test custom timeout
	customTimeout := 5 * time.Minute
	sm2 := NewSessionManagerWithTimeout(logger, customTimeout)
	defer sm2.StopCleanup()
	assert.Equal(t, customTimeout, sm2.GetSessionTimeout())
}

func TestClientManager_IsClientConfigured(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Initialize with some clients
	configs := map[string]config.ClientConfig{
		"github": {
			Type:    "http",
			URL:     "https://api.github.com",
			Command: "",
		},
		"aws": {
			Type:    "stdio",
			Command: "aws-mcp",
		},
	}
	require.NoError(t, cm.InitializeClients(ctx, configs))

	// Test configured clients
	assert.True(t, cm.IsClientConfigured("github"))
	assert.True(t, cm.IsClientConfigured("aws"))

	// Test non-configured clients
	assert.False(t, cm.IsClientConfigured("azure"))
	assert.False(t, cm.IsClientConfigured("maybedont")) // Native tool prefix, not a downstream client
	assert.False(t, cm.IsClientConfigured(""))
}

func TestClientManager_GetConfiguredClientNames(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Empty initially
	assert.Empty(t, cm.GetConfiguredClientNames())

	// Initialize with some clients
	configs := map[string]config.ClientConfig{
		"github": {Type: "http", URL: "https://api.github.com"},
		"aws":    {Type: "stdio", Command: "aws-mcp"},
	}
	require.NoError(t, cm.InitializeClients(ctx, configs))

	// Should return all configured names
	names := cm.GetConfiguredClientNames()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "github")
	assert.Contains(t, names, "aws")
}

func TestClientManager_HasSession(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// No sessions initially
	assert.False(t, cm.HasSession("session-1"))
	assert.False(t, cm.HasSession("session-2"))

	// Create a session via the session manager
	cm.sessionManager.CreateSession("session-1")

	// Now should have session-1 but not session-2
	assert.True(t, cm.HasSession("session-1"))
	assert.False(t, cm.HasSession("session-2"))

	// Delete session-1
	require.NoError(t, cm.sessionManager.DeleteSession(ctx, "session-1"))
	assert.False(t, cm.HasSession("session-1"))
}

// TestClientManager_GetActiveSessions_ReturnsClientMetadata verifies that
// GetActiveSessions returns session metadata including ClientIP, UserAgent,
// and DownstreamClients (with tool counts) when sessions have downstream clients.
func TestClientManager_GetActiveSessions_ReturnsClientMetadata(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Create a session and set metadata (in the correct order - session first, then metadata)
	cm.sessionManager.CreateSession("session-123")
	cm.SetSessionClientIP("session-123", "192.168.1.100")
	cm.SetSessionUserAgent("session-123", "Claude-Code/1.0.0")

	// Add downstream clients to the session
	cm.sessionManager.SetSessionClient("session-123", "github", &SessionClientInfo{
		Name:   "github",
		Config: config.ClientConfig{Type: "http"},
	})
	cm.sessionManager.SetSessionClient("session-123", "aws-docs", &SessionClientInfo{
		Name:   "aws-docs",
		Config: config.ClientConfig{Type: "http"},
	})

	// Create a second session with different metadata
	cm.sessionManager.CreateSession("session-456")
	cm.SetSessionClientIP("session-456", "10.0.0.50")
	cm.SetSessionUserAgent("session-456", "MCP-Client/2.0")

	cm.sessionManager.SetSessionClient("session-456", "github", &SessionClientInfo{
		Name:   "github",
		Config: config.ClientConfig{Type: "http"},
	})

	// Get active sessions
	sessions := cm.GetActiveSessions()

	// Verify we have 2 sessions
	require.Len(t, sessions, 2)

	// Find sessions by ID (order may vary)
	var session123, session456 *SessionInfo
	for i := range sessions {
		switch sessions[i].SessionID {
		case "session-123":
			session123 = &sessions[i]
		case "session-456":
			session456 = &sessions[i]
		}
	}

	// Verify session-123 metadata
	require.NotNil(t, session123, "session-123 should exist")
	assert.Equal(t, "192.168.1.100", session123.ClientIP, "session-123 should have correct ClientIP")
	assert.Equal(t, "Claude-Code/1.0.0", session123.UserAgent, "session-123 should have correct UserAgent")
	assert.Len(t, session123.DownstreamClients, 2, "session-123 should have 2 downstream clients")
	clientNames123 := make([]string, len(session123.DownstreamClients))
	for i, c := range session123.DownstreamClients {
		clientNames123[i] = c.Name
	}
	assert.Contains(t, clientNames123, "github")
	assert.Contains(t, clientNames123, "aws-docs")

	// Verify session-456 metadata
	require.NotNil(t, session456, "session-456 should exist")
	assert.Equal(t, "10.0.0.50", session456.ClientIP, "session-456 should have correct ClientIP")
	assert.Equal(t, "MCP-Client/2.0", session456.UserAgent, "session-456 should have correct UserAgent")
	assert.Len(t, session456.DownstreamClients, 1, "session-456 should have 1 downstream client")
	assert.Equal(t, "github", session456.DownstreamClients[0].Name)
}

// TestClientManager_SetSessionClientIP_RequiresExistingSession verifies that
// SetSessionClientIP and SetSessionUserAgent silently fail when called before
// the session exists. This tests the low-level API behavior.
//
// Note: Session creation is deferred to CreateSessionClients (not onSessionRegister),
// so these methods should only be called after the session has been created.
func TestClientManager_SetSessionClientIP_RequiresExistingSession(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Set metadata BEFORE session exists - these calls silently fail
	cm.SetSessionClientIP("session-123", "192.168.1.100")
	cm.SetSessionUserAgent("session-123", "Claude-Code/1.0.0")

	// Now create the session
	cm.sessionManager.CreateSession("session-123")

	// Add a downstream client
	cm.sessionManager.SetSessionClient("session-123", "github", &SessionClientInfo{
		Name:   "github",
		Config: config.ClientConfig{Type: "http"},
	})

	// Get active sessions
	sessions := cm.GetActiveSessions()
	require.Len(t, sessions, 1)

	// ClientIP and UserAgent are empty because they were set before session existed
	assert.Empty(t, sessions[0].ClientIP, "ClientIP should be empty because it was set before session existed")
	assert.Empty(t, sessions[0].UserAgent, "UserAgent should be empty because it was set before session existed")

	// The downstream client should still be present
	assert.Len(t, sessions[0].DownstreamClients, 1)
	assert.Equal(t, "github", sessions[0].DownstreamClients[0].Name)
}

// TestClientManager_CreateSessionClients_PreservesExistingSession verifies that
// CreateSessionClients uses "get or create" semantics, preserving metadata that
// was set on an existing session (e.g., by onSessionRegister before async discovery).
func TestClientManager_CreateSessionClients_PreservesExistingSession(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	cm := NewClientManager(ctx, logger)
	defer func() { _ = cm.Close(ctx) }()

	// Initialize with no clients (we're testing session creation, not client creation)
	err := cm.InitializeClients(ctx, map[string]config.ClientConfig{})
	require.NoError(t, err)

	// Step 1: Create session first and set metadata (mimics onSessionRegister flow)
	cm.sessionManager.CreateSession("session-123")
	cm.SetSessionClientIP("session-123", "192.168.1.100")
	cm.SetSessionUserAgent("session-123", "Claude-Code/1.0.0")

	// Verify metadata is set
	session, exists := cm.sessionManager.GetSession("session-123")
	require.True(t, exists)
	assert.Equal(t, "192.168.1.100", session.GetClientIP())
	assert.Equal(t, "Claude-Code/1.0.0", session.GetUserAgent())

	// Step 2: Call CreateSessionClients (mimics async discovery)
	// This should NOT overwrite the existing session
	result, err := cm.CreateSessionClients(ctx, "session-123")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify metadata is preserved after CreateSessionClients
	session, exists = cm.sessionManager.GetSession("session-123")
	require.True(t, exists)
	assert.Equal(t, "192.168.1.100", session.GetClientIP(), "ClientIP should be preserved after CreateSessionClients")
	assert.Equal(t, "Claude-Code/1.0.0", session.GetUserAgent(), "UserAgent should be preserved after CreateSessionClients")

	// Verify GetActiveSessions returns the metadata
	sessions := cm.GetActiveSessions()
	require.Len(t, sessions, 1)
	assert.Equal(t, "session-123", sessions[0].SessionID)
	assert.Equal(t, "192.168.1.100", sessions[0].ClientIP)
	assert.Equal(t, "Claude-Code/1.0.0", sessions[0].UserAgent)
}
