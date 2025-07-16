package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MemorySecretsManager implements SecretsManager using in-memory storage
type MemorySecretsManager struct {
	secrets map[string]*secretEntry
	mu      sync.RWMutex
	logger  *zap.Logger
}

type secretEntry struct {
	value     string
	expiresAt *time.Time
}

// NewMemorySecretsManager creates a new in-memory secrets manager
func NewMemorySecretsManager(logger *zap.Logger) *MemorySecretsManager {
	manager := &MemorySecretsManager{
		secrets: make(map[string]*secretEntry),
		logger:  logger,
	}

	// Start cleanup goroutine
	go manager.cleanup()

	return manager
}

// GetSecret retrieves a secret by key
func (m *MemorySecretsManager) GetSecret(ctx context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.secrets[key]
	if !exists {
		return "", fmt.Errorf("secret not found: %s", key)
	}

	if entry.expiresAt != nil && time.Now().After(*entry.expiresAt) {
		delete(m.secrets, key)
		return "", fmt.Errorf("secret expired: %s", key)
	}

	m.logger.Debug("Retrieved secret", zap.String("key", key))
	return entry.value, nil
}

// SetSecret stores a secret with optional TTL
func (m *MemorySecretsManager) SetSecret(ctx context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := &secretEntry{value: value}
	if ttl > 0 {
		expiresAt := time.Now().Add(ttl)
		entry.expiresAt = &expiresAt
	}

	m.secrets[key] = entry
	m.logger.Debug("Stored secret", zap.String("key", key), zap.Duration("ttl", ttl))
	return nil
}

// DeleteSecret deletes a secret
func (m *MemorySecretsManager) DeleteSecret(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.secrets, key)
	m.logger.Debug("Deleted secret", zap.String("key", key))
	return nil
}

// ListSecrets lists all secret keys with a prefix
func (m *MemorySecretsManager) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for key := range m.secrets {
		if prefix == "" || len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// cleanup removes expired secrets periodically
func (m *MemorySecretsManager) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanupExpired()
	}
}

// cleanupExpired removes expired secrets
func (m *MemorySecretsManager) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for key, entry := range m.secrets {
		if entry.expiresAt != nil && now.After(*entry.expiresAt) {
			delete(m.secrets, key)
			m.logger.Debug("Cleaned up expired secret", zap.String("key", key))
		}
	}
}

// FileSecretsManager implements SecretsManager using file storage
type FileSecretsManager struct {
	basePath string
	logger   *zap.Logger
	mu       sync.RWMutex
}

// NewFileSecretsManager creates a new file-based secrets manager
func NewFileSecretsManager(basePath string, logger *zap.Logger) (*FileSecretsManager, error) {
	// Create base directory if it doesn't exist
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create secrets directory: %w", err)
	}

	return &FileSecretsManager{
		basePath: basePath,
		logger:   logger,
	}, nil
}

// GetSecret retrieves a secret by key
func (f *FileSecretsManager) GetSecret(ctx context.Context, key string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	filePath := filepath.Join(f.basePath, key+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("secret not found: %s", key)
		}
		return "", fmt.Errorf("failed to read secret file: %w", err)
	}

	var entry struct {
		Value     string     `json:"value"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}

	if err := json.Unmarshal(data, &entry); err != nil {
		return "", fmt.Errorf("failed to unmarshal secret: %w", err)
	}

	if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
		os.Remove(filePath)
		return "", fmt.Errorf("secret expired: %s", key)
	}

	f.logger.Debug("Retrieved secret from file", zap.String("key", key))
	return entry.Value, nil
}

// SetSecret stores a secret with optional TTL
func (f *FileSecretsManager) SetSecret(ctx context.Context, key, value string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	entry := struct {
		Value     string     `json:"value"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}{
		Value: value,
	}

	if ttl > 0 {
		expiresAt := time.Now().Add(ttl)
		entry.ExpiresAt = &expiresAt
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal secret: %w", err)
	}

	filePath := filepath.Join(f.basePath, key+".json")
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write secret file: %w", err)
	}

	f.logger.Debug("Stored secret to file", zap.String("key", key), zap.Duration("ttl", ttl))
	return nil
}

// DeleteSecret deletes a secret
func (f *FileSecretsManager) DeleteSecret(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	filePath := filepath.Join(f.basePath, key+".json")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete secret file: %w", err)
	}

	f.logger.Debug("Deleted secret file", zap.String("key", key))
	return nil
}

// ListSecrets lists all secret keys with a prefix
func (f *FileSecretsManager) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	entries, err := os.ReadDir(f.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read secrets directory: %w", err)
	}

	var keys []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}

		key := name[:len(name)-5] // Remove .json extension
		if prefix == "" || len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// SecretsManagerFactory creates secrets managers based on configuration
type SecretsManagerFactory struct {
	logger *zap.Logger
}

// NewSecretsManagerFactory creates a new secrets manager factory
func NewSecretsManagerFactory(logger *zap.Logger) *SecretsManagerFactory {
	return &SecretsManagerFactory{logger: logger}
}

// CreateSecretsManager creates a secrets manager based on configuration
func (f *SecretsManagerFactory) CreateSecretsManager(secretsType string, config map[string]interface{}) (SecretsManager, error) {
	switch secretsType {
	case "memory", "":
		return NewMemorySecretsManager(f.logger), nil
	case "file":
		basePath, ok := config["path"].(string)
		if !ok {
			basePath = "./secrets"
		}
		return NewFileSecretsManager(basePath, f.logger)
	default:
		return nil, fmt.Errorf("unsupported secrets manager type: %s", secretsType)
	}
}