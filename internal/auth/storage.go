package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MemoryTokenStorage implements TokenStorage using in-memory storage
type MemoryTokenStorage struct {
	tokens     map[string]*tokenEntry
	challenges map[string]*challengeEntry
	mu         sync.RWMutex
	logger     *zap.Logger
}

type tokenEntry struct {
	token     *TokenInfo
	expiresAt time.Time
}

type challengeEntry struct {
	challenge *PKCEChallenge
	expiresAt time.Time
}

// NewMemoryTokenStorage creates a new in-memory token storage
func NewMemoryTokenStorage(logger *zap.Logger) *MemoryTokenStorage {
	storage := &MemoryTokenStorage{
		tokens:     make(map[string]*tokenEntry),
		challenges: make(map[string]*challengeEntry),
		logger:     logger,
	}

	// Start cleanup goroutine
	go storage.cleanup()

	return storage
}

// StoreToken stores a token with expiration
func (m *MemoryTokenStorage) StoreToken(ctx context.Context, key string, token *TokenInfo, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	expiresAt := time.Now().Add(ttl)
	m.tokens[key] = &tokenEntry{
		token:     token,
		expiresAt: expiresAt,
	}

	m.logger.Debug("Stored token",
		zap.String("key", key),
		zap.String("user_id", token.UserID),
		zap.String("client_id", token.ClientID),
		zap.Time("expires_at", expiresAt))

	return nil
}

// GetToken retrieves a token
func (m *MemoryTokenStorage) GetToken(ctx context.Context, key string) (*TokenInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.tokens[key]
	if !exists {
		return nil, fmt.Errorf("token not found")
	}

	if time.Now().After(entry.expiresAt) {
		// Token expired, clean it up
		delete(m.tokens, key)
		return nil, fmt.Errorf("token expired")
	}

	m.logger.Debug("Retrieved token",
		zap.String("key", key),
		zap.String("user_id", entry.token.UserID),
		zap.String("client_id", entry.token.ClientID))

	return entry.token, nil
}

// DeleteToken deletes a token
func (m *MemoryTokenStorage) DeleteToken(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.tokens, key)
	m.logger.Debug("Deleted token", zap.String("key", key))
	return nil
}

// StorePKCEChallenge stores a PKCE challenge
func (m *MemoryTokenStorage) StorePKCEChallenge(ctx context.Context, state string, challenge *PKCEChallenge, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	expiresAt := time.Now().Add(ttl)
	m.challenges[state] = &challengeEntry{
		challenge: challenge,
		expiresAt: expiresAt,
	}

	m.logger.Debug("Stored PKCE challenge",
		zap.String("state", state),
		zap.String("client_id", challenge.ClientID),
		zap.Time("expires_at", expiresAt))

	return nil
}

// GetPKCEChallenge retrieves a PKCE challenge
func (m *MemoryTokenStorage) GetPKCEChallenge(ctx context.Context, state string) (*PKCEChallenge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.challenges[state]
	if !exists {
		return nil, fmt.Errorf("PKCE challenge not found")
	}

	if time.Now().After(entry.expiresAt) {
		// Challenge expired, clean it up
		delete(m.challenges, state)
		return nil, fmt.Errorf("PKCE challenge expired")
	}

	m.logger.Debug("Retrieved PKCE challenge",
		zap.String("state", state),
		zap.String("client_id", entry.challenge.ClientID))

	return entry.challenge, nil
}

// DeletePKCEChallenge deletes a PKCE challenge
func (m *MemoryTokenStorage) DeletePKCEChallenge(ctx context.Context, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.challenges, state)
	m.logger.Debug("Deleted PKCE challenge", zap.String("state", state))
	return nil
}

// cleanup removes expired entries periodically
func (m *MemoryTokenStorage) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanupExpired()
	}
}

// cleanupExpired removes expired tokens and challenges
func (m *MemoryTokenStorage) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	
	// Clean up expired tokens
	for key, entry := range m.tokens {
		if now.After(entry.expiresAt) {
			delete(m.tokens, key)
			m.logger.Debug("Cleaned up expired token", zap.String("key", key))
		}
	}

	// Clean up expired challenges
	for state, entry := range m.challenges {
		if now.After(entry.expiresAt) {
			delete(m.challenges, state)
			m.logger.Debug("Cleaned up expired PKCE challenge", zap.String("state", state))
		}
	}
}