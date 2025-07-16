package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// Manager provides a unified authentication interface
type Manager struct {
	authenticator   Authenticator
	tokenStorage    TokenStorage
	secretsManager  SecretsManager
	jwtManager      *JWTManager
	handlers        *AuthHandlers
	logger          *zap.Logger
	auditLogger     *zap.Logger
	config          *config.Config
}

// NewManager creates a new authentication manager
func NewManager(cfg *config.Config, logger *zap.Logger, auditLogger *zap.Logger) (*Manager, error) {
	manager := &Manager{
		config:      cfg,
		logger:      logger,
		auditLogger: auditLogger,
	}

	// Initialize secrets manager
	secretsFactory := NewSecretsManagerFactory(logger)
	secretsManager, err := secretsFactory.CreateSecretsManager(cfg.Secrets.Type, cfg.Secrets.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create secrets manager: %w", err)
	}
	manager.secretsManager = secretsManager

	// Initialize token storage
	tokenStorage := NewMemoryTokenStorage(logger) // For now, always use memory storage
	manager.tokenStorage = tokenStorage

	// Initialize JWT manager
	signingKey := cfg.Auth.JWTConfig.SigningKey
	if signingKey == "" {
		// Generate a random signing key if not provided
		randomKey, err := GenerateRandomString(32)
		if err != nil {
			return nil, fmt.Errorf("failed to generate JWT signing key: %w", err)
		}
		signingKey = randomKey
		logger.Warn("Using randomly generated JWT signing key - tokens will not persist across restarts")
	}

	issuer := cfg.Auth.JWTConfig.Issuer
	if issuer == "" {
		issuer = "maybe-dont-gateway"
	}

	audience := cfg.Auth.JWTConfig.Audience
	if len(audience) == 0 {
		audience = []string{"maybe-dont"}
	}

	tokenDuration := 24 * time.Hour // Default 24 hours
	jwtManager := NewJWTManager(signingKey, issuer, audience, tokenDuration, logger)
	manager.jwtManager = jwtManager

	// Initialize authenticator based on auth type
	switch cfg.Auth.Type {
	case "oauth2":
		if !cfg.Auth.OAuth2Config.Enabled {
			return nil, fmt.Errorf("OAuth2 is not enabled in configuration")
		}
		
		oauth2Auth, err := NewOAuth2Authenticator(cfg, tokenStorage, jwtManager, logger, auditLogger)
		if err != nil {
			return nil, fmt.Errorf("failed to create OAuth2 authenticator: %w", err)
		}
		manager.authenticator = oauth2Auth

		// Create HTTP handlers for OAuth2
		manager.handlers = NewAuthHandlers(oauth2Auth, logger, auditLogger)

	case "jwt":
		// For JWT-only authentication, create a simple JWT authenticator
		manager.authenticator = &JWTAuthenticator{
			jwtManager: jwtManager,
			logger:     logger,
		}

	default:
		return nil, fmt.Errorf("unsupported authentication type: %s", cfg.Auth.Type)
	}

	logger.Info("Authentication manager initialized",
		zap.String("auth_type", cfg.Auth.Type),
		zap.String("secrets_type", cfg.Secrets.Type))

	return manager, nil
}

// GetAuthenticator returns the authenticator instance
func (m *Manager) GetAuthenticator() Authenticator {
	return m.authenticator
}

// GetTokenStorage returns the token storage instance
func (m *Manager) GetTokenStorage() TokenStorage {
	return m.tokenStorage
}

// GetSecretsManager returns the secrets manager instance
func (m *Manager) GetSecretsManager() SecretsManager {
	return m.secretsManager
}

// GetJWTManager returns the JWT manager instance
func (m *Manager) GetJWTManager() *JWTManager {
	return m.jwtManager
}

// GetHandlers returns the HTTP handlers for OAuth2 (if available)
func (m *Manager) GetHandlers() *AuthHandlers {
	return m.handlers
}

// Authenticate validates a token and returns authentication context
func (m *Manager) Authenticate(ctx context.Context, token string) (*AuthContext, error) {
	return m.authenticator.Authenticate(ctx, token)
}

// JWTAuthenticator implements simple JWT-only authentication
type JWTAuthenticator struct {
	jwtManager *JWTManager
	logger     *zap.Logger
}

// Authenticate validates a JWT token
func (j *JWTAuthenticator) Authenticate(ctx context.Context, token string) (*AuthContext, error) {
	return j.jwtManager.ValidateToken(ctx, token)
}

// GetAuthURL is not supported for JWT-only authentication
func (j *JWTAuthenticator) GetAuthURL(ctx context.Context, clientID, state, codeChallenge, codeChallengeMethod string) (string, error) {
	return "", fmt.Errorf("OAuth flow not supported for JWT-only authentication")
}

// ExchangeCode is not supported for JWT-only authentication
func (j *JWTAuthenticator) ExchangeCode(ctx context.Context, code, codeVerifier, state string) (*TokenInfo, error) {
	return nil, fmt.Errorf("OAuth flow not supported for JWT-only authentication")
}

// RefreshToken is not supported for JWT-only authentication
func (j *JWTAuthenticator) RefreshToken(ctx context.Context, refreshToken string) (*TokenInfo, error) {
	return nil, fmt.Errorf("token refresh not supported for JWT-only authentication")
}

// RevokeToken is not supported for JWT-only authentication
func (j *JWTAuthenticator) RevokeToken(ctx context.Context, token string) error {
	return fmt.Errorf("token revocation not supported for JWT-only authentication")
}

// ValidateClient is not supported for JWT-only authentication
func (j *JWTAuthenticator) ValidateClient(ctx context.Context, clientID, redirectURI string) error {
	return fmt.Errorf("client validation not supported for JWT-only authentication")
}