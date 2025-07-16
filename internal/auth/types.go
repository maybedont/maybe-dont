package auth

import (
	"context"
	"time"

	"golang.org/x/oauth2"
)

// AuthContext represents the authentication context for a request
type AuthContext struct {
	UserID       string            `json:"user_id"`
	ClientID     string            `json:"client_id"`
	Roles        []string          `json:"roles"`
	Scopes       []string          `json:"scopes"`
	Provider     string            `json:"provider"`
	SessionID    string            `json:"session_id"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Metadata     map[string]string `json:"metadata"`
	IssuedAt     time.Time         `json:"issued_at"`
	RefreshToken string            `json:"-"` // Never serialize refresh tokens
}

// TokenInfo represents stored token information
type TokenInfo struct {
	AccessToken  string            `json:"access_token"`
	RefreshToken string            `json:"refresh_token"`
	TokenType    string            `json:"token_type"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Scopes       []string          `json:"scopes"`
	UserID       string            `json:"user_id"`
	ClientID     string            `json:"client_id"`
	Provider     string            `json:"provider"`
	Metadata     map[string]string `json:"metadata"`
}

// PKCEChallenge represents a PKCE challenge for OAuth2 flow
type PKCEChallenge struct {
	CodeVerifier  string    `json:"code_verifier"`
	CodeChallenge string    `json:"code_challenge"`
	Method        string    `json:"method"` // S256
	State         string    `json:"state"`
	ClientID      string    `json:"client_id"`
	RedirectURI   string    `json:"redirect_uri"`
	Scopes        []string  `json:"scopes"`
	Provider      string    `json:"provider"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// UserInfo represents user information from OAuth provider
type UserInfo struct {
	ID       string            `json:"id"`
	Email    string            `json:"email"`
	Name     string            `json:"name"`
	Username string            `json:"username"`
	Roles    []string          `json:"roles"`
	Metadata map[string]string `json:"metadata"`
}

// AuthenticationResult represents the result of authentication
type AuthenticationResult struct {
	Success     bool         `json:"success"`
	AuthContext *AuthContext `json:"auth_context,omitempty"`
	Error       string       `json:"error,omitempty"`
	RedirectURL string       `json:"redirect_url,omitempty"`
}

// Authenticator interface defines the authentication contract
type Authenticator interface {
	// Authenticate validates a request and returns authentication context
	Authenticate(ctx context.Context, token string) (*AuthContext, error)
	
	// GetAuthURL returns the OAuth authorization URL for a client
	GetAuthURL(ctx context.Context, clientID, state, codeChallenge, codeChallengeMethod string) (string, error)
	
	// ExchangeCode exchanges authorization code for tokens
	ExchangeCode(ctx context.Context, code, codeVerifier, state string) (*TokenInfo, error)
	
	// RefreshToken refreshes an access token using refresh token
	RefreshToken(ctx context.Context, refreshToken string) (*TokenInfo, error)
	
	// RevokeToken revokes a token
	RevokeToken(ctx context.Context, token string) error
	
	// ValidateClient validates a client ID and redirect URI
	ValidateClient(ctx context.Context, clientID, redirectURI string) error
}

// TokenStorage interface defines token storage contract
type TokenStorage interface {
	// StoreToken stores a token with expiration
	StoreToken(ctx context.Context, key string, token *TokenInfo, ttl time.Duration) error
	
	// GetToken retrieves a token
	GetToken(ctx context.Context, key string) (*TokenInfo, error)
	
	// DeleteToken deletes a token
	DeleteToken(ctx context.Context, key string) error
	
	// StorePKCEChallenge stores a PKCE challenge
	StorePKCEChallenge(ctx context.Context, state string, challenge *PKCEChallenge, ttl time.Duration) error
	
	// GetPKCEChallenge retrieves a PKCE challenge
	GetPKCEChallenge(ctx context.Context, state string) (*PKCEChallenge, error)
	
	// DeletePKCEChallenge deletes a PKCE challenge
	DeletePKCEChallenge(ctx context.Context, state string) error
}

// OAuthProvider interface defines OAuth provider contract
type OAuthProvider interface {
	// GetConfig returns OAuth2 configuration
	GetConfig() *oauth2.Config
	
	// GetUserInfo retrieves user information using access token
	GetUserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error)
	
	// GetProviderName returns the provider name
	GetProviderName() string
	
	// ValidateToken validates a token with the provider
	ValidateToken(ctx context.Context, token string) (*UserInfo, error)
}

// SecretsManager interface defines secrets management contract
type SecretsManager interface {
	// GetSecret retrieves a secret by key
	GetSecret(ctx context.Context, key string) (string, error)
	
	// SetSecret stores a secret with optional TTL
	SetSecret(ctx context.Context, key, value string, ttl time.Duration) error
	
	// DeleteSecret deletes a secret
	DeleteSecret(ctx context.Context, key string) error
	
	// ListSecrets lists all secret keys (for management)
	ListSecrets(ctx context.Context, prefix string) ([]string, error)
}

// AuthEvent represents an authentication event for audit logging
type AuthEvent struct {
	Timestamp   time.Time         `json:"timestamp"`
	EventType   string            `json:"event_type"` // login, logout, token_refresh, token_revoke, auth_failure
	UserID      string            `json:"user_id,omitempty"`
	ClientID    string            `json:"client_id,omitempty"`
	Provider    string            `json:"provider,omitempty"`
	IPAddress   string            `json:"ip_address,omitempty"`
	UserAgent   string            `json:"user_agent,omitempty"`
	Success     bool              `json:"success"`
	Error       string            `json:"error,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	TokenScopes []string          `json:"token_scopes,omitempty"`
}