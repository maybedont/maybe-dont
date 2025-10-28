package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
)

// SessionCapabilities stores the capabilities for all clients in a session
type SessionCapabilities struct {
	// Map of client name -> server capabilities
	Capabilities map[string]*mcp.ServerCapabilities
}

// SessionManager manages client sessions and their capabilities
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*SessionCapabilities // sessionID -> capabilities
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionCapabilities),
	}
}

// GenerateSessionID creates a new unique session ID
func GenerateSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// CreateSession creates a new session with empty capabilities
func (sm *SessionManager) CreateSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.sessions[sessionID] = &SessionCapabilities{
		Capabilities: make(map[string]*mcp.ServerCapabilities),
	}
}

// GetSessionCapabilities retrieves capabilities for a session
func (sm *SessionManager) GetSessionCapabilities(sessionID string) (*SessionCapabilities, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	caps, ok := sm.sessions[sessionID]
	return caps, ok
}

// SetClientCapabilities stores capabilities for a specific client in a session
func (sm *SessionManager) SetClientCapabilities(sessionID, clientName string, capabilities *mcp.ServerCapabilities) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[sessionID]
	if !ok {
		session = &SessionCapabilities{
			Capabilities: make(map[string]*mcp.ServerCapabilities),
		}
		sm.sessions[sessionID] = session
	}

	session.Capabilities[clientName] = capabilities
}

// GetClientCapabilities retrieves capabilities for a specific client in a session
func (sm *SessionManager) GetClientCapabilities(sessionID, clientName string) (*mcp.ServerCapabilities, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[sessionID]
	if !ok {
		return nil, false
	}

	caps, ok := session.Capabilities[clientName]
	return caps, ok
}

// HasClientCapabilities checks if capabilities have been loaded for a client in a session
func (sm *SessionManager) HasClientCapabilities(sessionID, clientName string) bool {
	_, ok := sm.GetClientCapabilities(sessionID, clientName)
	return ok
}

// DeleteSession removes a session and all its capabilities
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, sessionID)
}

// GetSessionID extracts or creates a session ID from context
func GetSessionID(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(SessionIDKey).(string)
	return sessionID, ok
}

// WithSessionID adds a session ID to the context
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, SessionIDKey, sessionID)
}
