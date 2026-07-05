package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// DefaultSessionTimeout is the default idle timeout for sessions (30 minutes)
const DefaultSessionTimeout = 30 * time.Minute

// SessionCleanupInterval is how often we check for expired sessions
const SessionCleanupInterval = 1 * time.Minute

// SessionClientInfo holds a downstream client instance for a specific session
type SessionClientInfo struct {
	Name         string
	Client       *client.Client
	Config       config.ClientConfig
	Capabilities *mcp.ServerCapabilities
	Tools        []mcp.Tool
	Prompts      []mcp.Prompt
	Resources    []mcp.Resource
	Templates    []mcp.ResourceTemplate
}

// Session represents an upstream client session with its downstream clients
type Session struct {
	ID           string
	CreatedAt    time.Time
	clientIP     string // IP address of the upstream client
	userAgent    string // User-Agent header from the upstream client
	lastActivity time.Time
	mu           sync.RWMutex
	clients      map[string]*SessionClientInfo // clientName -> downstream client for this session
	closing      bool                          // true if session is being closed, prevents new clients
	connected    bool                          // true if the MCP SDK has an active SSE connection
	identitySub  string                        // `sub` of the identity bound to this session (empty if unauthenticated)
}

// SetIdentitySubject binds an authenticated user subject to this session. The first
// authenticated request wins; later requests are checked against it.
func (s *Session) SetIdentitySubject(sub string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.identitySub == "" {
		s.identitySub = sub
	}
}

// IdentitySubject returns the `sub` bound to this session (empty if none).
func (s *Session) IdentitySubject() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identitySub
}

// NewSession creates a new session
func NewSession(id string) *Session {
	now := time.Now()
	return &Session{
		ID:           id,
		CreatedAt:    now,
		lastActivity: now,
		clients:      make(map[string]*SessionClientInfo),
	}
}

// TouchActivity updates the last activity timestamp for this session
func (s *Session) TouchActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = time.Now()
}

// LastActivity returns the last activity timestamp for this session
func (s *Session) LastActivity() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastActivity
}

// IsExpired returns true if the session has been idle longer than the timeout
func (s *Session) IsExpired(timeout time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.lastActivity) > timeout
}

// SetClientIP sets the client IP address for this session
func (s *Session) SetClientIP(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientIP = ip
}

// GetClientIP returns the client IP address for this session
func (s *Session) GetClientIP() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clientIP
}

// SetUserAgent sets the User-Agent header for this session
func (s *Session) SetUserAgent(userAgent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userAgent = userAgent
}

// GetUserAgent returns the User-Agent header for this session
func (s *Session) GetUserAgent() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userAgent
}

// SetConnected sets whether the MCP SDK has an active SSE connection for this session
func (s *Session) SetConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
}

// IsConnected returns whether the MCP SDK has an active SSE connection for this session
func (s *Session) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// GetClient returns a downstream client for this session
func (s *Session) GetClient(clientName string) (*SessionClientInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clientInfo, ok := s.clients[clientName]
	return clientInfo, ok
}

// SetClient stores a downstream client for this session.
// Returns true if the client was stored successfully, false if the session is closing.
// If false is returned, the caller is responsible for closing the client.
func (s *Session) SetClient(clientName string, clientInfo *SessionClientInfo) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false // Session is closing, don't accept new clients
	}
	s.clients[clientName] = clientInfo
	return true
}

// GetAllClients returns all downstream clients for this session
func (s *Session) GetAllClients() map[string]*SessionClientInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*SessionClientInfo)
	for k, v := range s.clients {
		result[k] = v
	}
	return result
}

// Close closes all downstream clients for this session.
// After Close is called, SetClient will reject new clients to prevent resource leaks
// from async operations that complete after the session is closed.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Mark as closing to prevent new clients from being added
	s.closing = true

	var errs []error
	for name, clientInfo := range s.clients {
		if clientInfo.Client != nil {
			if err := clientInfo.Client.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close client %s: %w", name, err))
			}
		}
	}
	s.clients = make(map[string]*SessionClientInfo)

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// SessionManager manages upstream client sessions and their downstream clients
type SessionManager struct {
	mu             sync.RWMutex
	sessions       map[string]*Session // sessionID -> session
	logger         *config.SessionLogger
	sessionTimeout time.Duration
	stopCleanup    chan struct{}
	cleanupDone    chan struct{}
	// onSessionDeleted, if set, is invoked with the session ID after a session is removed
	// (used to evict cached downstream tokens for that session).
	onSessionDeleted func(sessionID string)
}

// SetOnSessionDeleted registers a callback invoked when a session is deleted or expires.
func (sm *SessionManager) SetOnSessionDeleted(fn func(sessionID string)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onSessionDeleted = fn
}

// notifySessionDeleted invokes the delete callback if one is registered.
func (sm *SessionManager) notifySessionDeleted(sessionID string) {
	sm.mu.RLock()
	fn := sm.onSessionDeleted
	sm.mu.RUnlock()
	if fn != nil {
		fn(sessionID)
	}
}

// NewSessionManager creates a new session manager with the default timeout
func NewSessionManager(logger *config.SessionLogger) *SessionManager {
	return NewSessionManagerWithTimeout(logger, DefaultSessionTimeout)
}

// NewSessionManagerWithTimeout creates a new session manager with a custom timeout
func NewSessionManagerWithTimeout(logger *config.SessionLogger, timeout time.Duration) *SessionManager {
	sm := &SessionManager{
		sessions:       make(map[string]*Session),
		logger:         logger,
		sessionTimeout: timeout,
		stopCleanup:    make(chan struct{}),
		cleanupDone:    make(chan struct{}),
	}
	go sm.cleanupLoop()
	return sm
}

// cleanupLoop periodically checks for and removes expired sessions
func (sm *SessionManager) cleanupLoop() {
	defer close(sm.cleanupDone)
	ticker := time.NewTicker(SessionCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopCleanup:
			return
		case <-ticker.C:
			sm.cleanupExpiredSessions()
		}
	}
}

// cleanupExpiredSessions removes all expired sessions
func (sm *SessionManager) cleanupExpiredSessions() {
	sm.mu.Lock()
	var expiredSessions []*Session
	var expiredIDs []string

	for id, session := range sm.sessions {
		if session.IsExpired(sm.sessionTimeout) {
			expiredSessions = append(expiredSessions, session)
			expiredIDs = append(expiredIDs, id)
			delete(sm.sessions, id)
		}
	}
	sm.mu.Unlock()

	// Close expired sessions outside the lock
	for i, session := range expiredSessions {
		idleTime := time.Since(session.LastActivity())
		clientCount := len(session.GetAllClients())

		sm.notifySessionDeleted(expiredIDs[i])
		if err := session.Close(); err != nil {
			sm.logger.Error(context.Background(), "Error closing expired session",
				zap.String("session_id", expiredIDs[i]),
				zap.Duration("idle_time", idleTime),
				zap.Error(err))
		} else {
			sm.logger.Info(context.Background(), "Session expired and closed due to inactivity",
				zap.String("session_id", expiredIDs[i]),
				zap.Duration("idle_time", idleTime),
				zap.Int("client_count", clientCount))
		}
	}
}

// StopCleanup stops the cleanup goroutine. Call this when shutting down.
func (sm *SessionManager) StopCleanup() {
	close(sm.stopCleanup)
	<-sm.cleanupDone
}

// GetSessionTimeout returns the configured session timeout
func (sm *SessionManager) GetSessionTimeout() time.Duration {
	return sm.sessionTimeout
}

// CreateSession creates a new session. Returns an error if a session with the
// given ID already exists — callers should use GetSession for existing sessions.
func (sm *SessionManager) CreateSession(sessionID string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.sessions[sessionID]; ok {
		return nil, fmt.Errorf("session %q already exists", sessionID)
	}

	session := NewSession(sessionID)
	sm.sessions[sessionID] = session
	return session, nil
}

// GetSession retrieves a session by ID and updates its last activity time
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if ok {
		session.TouchActivity()
	}
	return session, ok
}

// DeleteSession removes a session and closes all its downstream clients
func (sm *SessionManager) DeleteSession(ctx context.Context, sessionID string) error {
	sm.mu.Lock()
	session, ok := sm.sessions[sessionID]
	if !ok {
		sm.mu.Unlock()
		return nil
	}
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	sm.notifySessionDeleted(sessionID)

	// Close all downstream clients for this session
	if err := session.Close(); err != nil {
		sm.logger.Error(ctx, "Error closing session clients",
			zap.String("session_id", sessionID),
			zap.Error(err))
		return err
	}

	sm.logger.Info(ctx, "Session deleted and clients closed",
		zap.String("session_id", sessionID))
	return nil
}

// GetSessionClient retrieves a specific downstream client for a session and updates its last activity time
func (sm *SessionManager) GetSessionClient(sessionID, clientName string) (*SessionClientInfo, bool) {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !ok {
		return nil, false
	}

	session.TouchActivity()
	return session.GetClient(clientName)
}

// SetSessionClient stores a downstream client for a session.
// Returns true if the client was stored successfully, false if the session doesn't exist
// or is closing. If false is returned, the caller is responsible for closing the client
// to prevent resource leaks.
func (sm *SessionManager) SetSessionClient(sessionID, clientName string, clientInfo *SessionClientInfo) bool {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !ok {
		// Session doesn't exist (was already deleted), reject the client
		return false
	}

	return session.SetClient(clientName, clientInfo)
}

// HasSession checks if a session exists
func (sm *SessionManager) HasSession(sessionID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, ok := sm.sessions[sessionID]
	return ok
}

// GetAllSessions returns all session IDs
func (sm *SessionManager) GetAllSessions() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	ids := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		ids = append(ids, id)
	}
	return ids
}

// CloseAllSessions closes all sessions and their downstream clients
func (sm *SessionManager) CloseAllSessions(ctx context.Context) error {
	sm.mu.Lock()
	sessions := make(map[string]*Session)
	for k, v := range sm.sessions {
		sessions[k] = v
	}
	sm.sessions = make(map[string]*Session)
	sm.mu.Unlock()

	var errs []error
	for sessionID, session := range sessions {
		if err := session.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close session %s: %w", sessionID, err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// GenerateRequestID creates a new unique request ID
func GenerateRequestID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GetRequestID extracts a request ID from context
func GetRequestID(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(RequestIDKey).(string)
	return requestID, ok
}

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// WithSessionID adds a session ID to the context for logging purposes
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, config.SessionIDKey, sessionID)
}

// testSessionIDKey is a context key used for testing to inject a mock session ID.
// This allows tests to bypass the SDK's session mechanism.
type testSessionIDKeyType struct{}

var testSessionIDKey = testSessionIDKeyType{}

// GetSessionIDFromContext extracts the MCP session ID from context using the SDK.
// For testing, it also checks for a test-specific session ID injected via testSessionIDKey.
func GetSessionIDFromContext(ctx context.Context) (string, bool) {
	// Check for test-injected session ID first
	if testSessionID, ok := ctx.Value(testSessionIDKey).(string); ok {
		return testSessionID, true
	}

	// Use the SDK's session mechanism
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return "", false
	}
	return session.SessionID(), true
}
