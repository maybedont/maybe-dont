package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
	"golang.org/x/oauth2"
	"go.uber.org/zap"
)

// OAuth2Authenticator implements OAuth 2.1 with PKCE authentication
type OAuth2Authenticator struct {
	providers     map[string]OAuthProvider
	clients       map[string]config.OAuthClient
	tokenStorage  TokenStorage
	jwtManager    *JWTManager
	pkceGenerator *PKCEGenerator
	logger        *zap.Logger
	auditLogger   *zap.Logger
	
	// Configuration
	sessionTimeout   time.Duration
	refreshThreshold time.Duration
}

// NewOAuth2Authenticator creates a new OAuth2 authenticator
func NewOAuth2Authenticator(cfg *config.Config, tokenStorage TokenStorage, jwtManager *JWTManager, logger *zap.Logger, auditLogger *zap.Logger) (*OAuth2Authenticator, error) {
	auth := &OAuth2Authenticator{
		providers:     make(map[string]OAuthProvider),
		clients:       cfg.Auth.OAuth2Config.Clients,
		tokenStorage:  tokenStorage,
		jwtManager:    jwtManager,
		pkceGenerator: NewPKCEGenerator(),
		logger:        logger,
		auditLogger:   auditLogger,
	}

	// Parse session timeout
	if cfg.Auth.OAuth2Config.SessionTimeout != "" {
		timeout, err := time.ParseDuration(cfg.Auth.OAuth2Config.SessionTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid session timeout: %w", err)
		}
		auth.sessionTimeout = timeout
	} else {
		auth.sessionTimeout = 24 * time.Hour // Default 24 hours
	}

	// Parse refresh threshold
	if cfg.Auth.OAuth2Config.RefreshThreshold != "" {
		threshold, err := time.ParseDuration(cfg.Auth.OAuth2Config.RefreshThreshold)
		if err != nil {
			return nil, fmt.Errorf("invalid refresh threshold: %w", err)
		}
		auth.refreshThreshold = threshold
	} else {
		auth.refreshThreshold = 5 * time.Minute // Default 5 minutes
	}

	// Initialize providers
	factory := NewProviderFactory(logger)
	for name, providerConfig := range cfg.Auth.OAuth2Config.Providers {
		// Convert config.OAuthProvider to map[string]interface{}
		configMap := map[string]interface{}{
			"type":          providerConfig.Type,
			"client_id":     providerConfig.ClientID,
			"client_secret": providerConfig.ClientSecret,
			"auth_url":      providerConfig.AuthURL,
			"token_url":     providerConfig.TokenURL,
			"user_info_url": providerConfig.UserInfoURL,
			"redirect_url":  providerConfig.RedirectURL,
			"scopes":        interfaceSlice(providerConfig.Scopes),
		}

		provider, err := factory.CreateProvider(name, configMap)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider %s: %w", name, err)
		}

		auth.providers[name] = provider
		logger.Info("Initialized OAuth provider", zap.String("name", name), zap.String("type", providerConfig.Type))
	}

	return auth, nil
}

// interfaceSlice converts []string to []interface{}
func interfaceSlice(strings []string) []interface{} {
	interfaces := make([]interface{}, len(strings))
	for i, s := range strings {
		interfaces[i] = s
	}
	return interfaces
}

// Authenticate validates a JWT token and returns authentication context
func (a *OAuth2Authenticator) Authenticate(ctx context.Context, token string) (*AuthContext, error) {
	authCtx, err := a.jwtManager.ValidateToken(ctx, token)
	if err != nil {
		a.logAuthEvent(ctx, "auth_failure", "", "", "", false, err.Error(), nil)
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	// Check if token needs refresh
	if time.Until(authCtx.ExpiresAt) < a.refreshThreshold {
		a.logger.Debug("Token approaching expiration, refresh recommended",
			zap.String("user_id", authCtx.UserID),
			zap.Time("expires_at", authCtx.ExpiresAt))
	}

	a.logAuthEvent(ctx, "auth_success", authCtx.UserID, authCtx.ClientID, authCtx.Provider, true, "", map[string]string{
		"session_id": authCtx.SessionID,
	})

	return authCtx, nil
}

// GetAuthURL returns the OAuth authorization URL for a client
func (a *OAuth2Authenticator) GetAuthURL(ctx context.Context, clientID, state, codeChallenge, codeChallengeMethod string) (string, error) {
	_, exists := a.clients[clientID]
	if !exists {
		return "", fmt.Errorf("client not found: %s", clientID)
	}

	// For now, use the first provider configured
	// In a real implementation, you might want to specify which provider to use
	var provider OAuthProvider
	var providerName string
	for name, p := range a.providers {
		provider = p
		providerName = name
		break
	}

	if provider == nil {
		return "", fmt.Errorf("no OAuth providers configured")
	}

	config := provider.GetConfig()
	
	// Build auth URL with PKCE parameters
	authURL := config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", codeChallengeMethod),
		oauth2.SetAuthURLParam("client_id", clientID),
	)

	a.logger.Info("Generated auth URL",
		zap.String("client_id", clientID),
		zap.String("provider", providerName),
		zap.String("state", state))

	return authURL, nil
}

// ExchangeCode exchanges authorization code for tokens
func (a *OAuth2Authenticator) ExchangeCode(ctx context.Context, code, codeVerifier, state string) (*TokenInfo, error) {
	// Get PKCE challenge from storage
	challenge, err := a.tokenStorage.GetPKCEChallenge(ctx, state)
	if err != nil {
		a.logAuthEvent(ctx, "token_exchange_failure", "", challenge.ClientID, challenge.Provider, false, "PKCE challenge not found", nil)
		return nil, fmt.Errorf("invalid state parameter: %w", err)
	}

	// Validate PKCE challenge
	if !a.pkceGenerator.ValidateChallenge(codeVerifier, challenge.CodeChallenge, challenge.Method) {
		a.logAuthEvent(ctx, "token_exchange_failure", "", challenge.ClientID, challenge.Provider, false, "PKCE validation failed", nil)
		return nil, fmt.Errorf("PKCE validation failed")
	}

	// Get provider
	provider, exists := a.providers[challenge.Provider]
	if !exists {
		return nil, fmt.Errorf("provider not found: %s", challenge.Provider)
	}

	// Exchange code for token
	config := provider.GetConfig()
	token, err := config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		a.logAuthEvent(ctx, "token_exchange_failure", "", challenge.ClientID, challenge.Provider, false, err.Error(), nil)
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	// Get user info
	userInfo, err := provider.GetUserInfo(ctx, token)
	if err != nil {
		a.logAuthEvent(ctx, "token_exchange_failure", "", challenge.ClientID, challenge.Provider, false, "failed to get user info", nil)
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Get client configuration for roles
	client := a.clients[challenge.ClientID]
	roles := client.Roles
	if len(roles) == 0 {
		roles = []string{"user"} // Default role
	}

	// Create token info
	tokenInfo := &TokenInfo{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresAt:    token.Expiry,
		Scopes:       challenge.Scopes,
		UserID:       userInfo.ID,
		ClientID:     challenge.ClientID,
		Provider:     challenge.Provider,
		Metadata: map[string]string{
			"email":    userInfo.Email,
			"name":     userInfo.Name,
			"username": userInfo.Username,
		},
	}

	// Store token
	tokenKey := fmt.Sprintf("token:%s:%s", challenge.ClientID, userInfo.ID)
	if err := a.tokenStorage.StoreToken(ctx, tokenKey, tokenInfo, a.sessionTimeout); err != nil {
		a.logger.Error("Failed to store token", zap.Error(err))
	}

	// Clean up PKCE challenge
	if err := a.tokenStorage.DeletePKCEChallenge(ctx, state); err != nil {
		a.logger.Warn("Failed to delete PKCE challenge", zap.Error(err))
	}

	a.logAuthEvent(ctx, "login", userInfo.ID, challenge.ClientID, challenge.Provider, true, "", map[string]string{
		"email": userInfo.Email,
		"name":  userInfo.Name,
	})

	return tokenInfo, nil
}

// RefreshToken refreshes an access token using refresh token
func (a *OAuth2Authenticator) RefreshToken(ctx context.Context, refreshToken string) (*TokenInfo, error) {
	// In a real implementation, you would need to store refresh token metadata
	// to know which provider and user it belongs to
	return nil, fmt.Errorf("token refresh not implemented yet")
}

// RevokeToken revokes a token
func (a *OAuth2Authenticator) RevokeToken(ctx context.Context, token string) error {
	// Parse token to get user info
	authCtx, err := a.jwtManager.ValidateToken(ctx, token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	// Delete token from storage
	tokenKey := fmt.Sprintf("token:%s:%s", authCtx.ClientID, authCtx.UserID)
	if err := a.tokenStorage.DeleteToken(ctx, tokenKey); err != nil {
		a.logger.Warn("Failed to delete token from storage", zap.Error(err))
	}

	a.logAuthEvent(ctx, "logout", authCtx.UserID, authCtx.ClientID, authCtx.Provider, true, "", map[string]string{
		"session_id": authCtx.SessionID,
	})

	return nil
}

// ValidateClient validates a client ID and redirect URI
func (a *OAuth2Authenticator) ValidateClient(ctx context.Context, clientID, redirectURI string) error {
	client, exists := a.clients[clientID]
	if !exists {
		return fmt.Errorf("client not found: %s", clientID)
	}

	// Validate redirect URI
	validRedirect := false
	for _, uri := range client.RedirectURIs {
		if uri == redirectURI {
			validRedirect = true
			break
		}
	}

	if !validRedirect {
		return fmt.Errorf("invalid redirect URI for client %s", clientID)
	}

	return nil
}

// InitiateOAuthFlow initiates an OAuth flow for a client
func (a *OAuth2Authenticator) InitiateOAuthFlow(ctx context.Context, clientID, redirectURI, provider string, scopes []string) (*PKCEChallenge, string, error) {
	// Validate client
	if err := a.ValidateClient(ctx, clientID, redirectURI); err != nil {
		return nil, "", err
	}

	// Generate PKCE challenge
	challenge, err := a.pkceGenerator.GenerateChallenge()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate PKCE challenge: %w", err)
	}

	// Set additional fields
	challenge.ClientID = clientID
	challenge.RedirectURI = redirectURI
	challenge.Provider = provider
	challenge.Scopes = scopes
	challenge.CreatedAt = time.Now()
	challenge.ExpiresAt = time.Now().Add(10 * time.Minute) // PKCE challenges expire in 10 minutes

	// Store PKCE challenge
	if err := a.tokenStorage.StorePKCEChallenge(ctx, challenge.State, challenge, 10*time.Minute); err != nil {
		return nil, "", fmt.Errorf("failed to store PKCE challenge: %w", err)
	}

	// Generate auth URL
	authURL, err := a.GetAuthURL(ctx, clientID, challenge.State, challenge.CodeChallenge, challenge.Method)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate auth URL: %w", err)
	}

	return challenge, authURL, nil
}

// CreateJWTFromTokenInfo creates a JWT token from token info
func (a *OAuth2Authenticator) CreateJWTFromTokenInfo(ctx context.Context, tokenInfo *TokenInfo) (string, error) {
	sessionID, err := GenerateSessionID()
	if err != nil {
		return "", fmt.Errorf("failed to generate session ID: %w", err)
	}

	// Get client configuration for roles
	client := a.clients[tokenInfo.ClientID]
	roles := client.Roles
	if len(roles) == 0 {
		roles = []string{"user"} // Default role
	}

	authCtx := &AuthContext{
		UserID:    tokenInfo.UserID,
		ClientID:  tokenInfo.ClientID,
		Roles:     roles,
		Scopes:    tokenInfo.Scopes,
		Provider:  tokenInfo.Provider,
		SessionID: sessionID,
		ExpiresAt: tokenInfo.ExpiresAt,
		Metadata:  tokenInfo.Metadata,
		IssuedAt:  time.Now(),
	}

	return a.jwtManager.GenerateToken(ctx, authCtx)
}

// logAuthEvent logs an authentication event for audit purposes
func (a *OAuth2Authenticator) logAuthEvent(ctx context.Context, eventType, userID, clientID, provider string, success bool, errorMsg string, metadata map[string]string) {
	event := AuthEvent{
		Timestamp: time.Now(),
		EventType: eventType,
		UserID:    userID,
		ClientID:  clientID,
		Provider:  provider,
		Success:   success,
		Error:     errorMsg,
		Metadata:  metadata,
	}

	// Extract IP and User-Agent from context if available
	// This would need to be set by the HTTP handler

	a.auditLogger.Info("Authentication event",
		zap.String("event_type", event.EventType),
		zap.String("user_id", event.UserID),
		zap.String("client_id", event.ClientID),
		zap.String("provider", event.Provider),
		zap.Bool("success", event.Success),
		zap.String("error", event.Error),
		zap.Any("metadata", event.Metadata),
		zap.Time("timestamp", event.Timestamp))
}