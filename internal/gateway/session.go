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
	sessions map[string]*SessionCapabilities // requestID -> capabilities
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionCapabilities),
	}
}

// GenerateRequestID creates a new unique request ID
func GenerateRequestID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// CreateSession creates a new session with empty capabilities
func (sm *SessionManager) CreateSession(requestID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.sessions[requestID] = &SessionCapabilities{
		Capabilities: make(map[string]*mcp.ServerCapabilities),
	}
}

// GetSessionCapabilities retrieves capabilities for a session
func (sm *SessionManager) GetSessionCapabilities(requestID string) (*SessionCapabilities, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	caps, ok := sm.sessions[requestID]
	return caps, ok
}

// SetClientCapabilities stores capabilities for a specific client in a session
func (sm *SessionManager) SetClientCapabilities(requestID, clientName string, capabilities *mcp.ServerCapabilities) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[requestID]
	if !ok {
		session = &SessionCapabilities{
			Capabilities: make(map[string]*mcp.ServerCapabilities),
		}
		sm.sessions[requestID] = session
	}

	session.Capabilities[clientName] = capabilities
}

// GetClientCapabilities retrieves capabilities for a specific client in a session
func (sm *SessionManager) GetClientCapabilities(requestID, clientName string) (*mcp.ServerCapabilities, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[requestID]
	if !ok {
		return nil, false
	}

	caps, ok := session.Capabilities[clientName]
	return caps, ok
}

// HasClientCapabilities checks if capabilities have been loaded for a client in a session
func (sm *SessionManager) HasClientCapabilities(requestID, clientName string) bool {
	_, ok := sm.GetClientCapabilities(requestID, clientName)
	return ok
}

// DeleteSession removes a session and all its capabilities
func (sm *SessionManager) DeleteSession(requestID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, requestID)
}

// GetRequestID extracts or creates a request ID from context
func GetRequestID(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(RequestIDKey).(string)
	return requestID, ok
}

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}
