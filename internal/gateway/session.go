package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

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
	ID       string
	ClientIP string // IP address of the upstream client
	mu       sync.RWMutex
	clients  map[string]*SessionClientInfo // clientName -> downstream client for this session
	closing  bool                          // true if session is being closed, prevents new clients
}

// NewSession creates a new session
func NewSession(id string) *Session {
	return &Session{
		ID:      id,
		clients: make(map[string]*SessionClientInfo),
	}
}

// SetClientIP sets the client IP address for this session
func (s *Session) SetClientIP(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ClientIP = ip
}

// GetClientIP returns the client IP address for this session
func (s *Session) GetClientIP() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ClientIP
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
	mu       sync.RWMutex
	sessions map[string]*Session // sessionID -> session
	logger   *config.SessionLogger
}

// NewSessionManager creates a new session manager
func NewSessionManager(logger *config.SessionLogger) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		logger:   logger,
	}
}

// CreateSession creates a new session
func (sm *SessionManager) CreateSession(sessionID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session := NewSession(sessionID)
	sm.sessions[sessionID] = session
	return session
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, ok := sm.sessions[sessionID]
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

// GetSessionClient retrieves a specific downstream client for a session
func (sm *SessionManager) GetSessionClient(sessionID, clientName string) (*SessionClientInfo, bool) {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !ok {
		return nil, false
	}

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
