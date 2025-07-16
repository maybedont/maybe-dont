package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"go.uber.org/zap"
)

// BaseOAuthProvider provides common OAuth2 functionality
type BaseOAuthProvider struct {
	config   *oauth2.Config
	name     string
	userURL  string
	logger   *zap.Logger
	client   *http.Client
}

// NewBaseOAuthProvider creates a new base OAuth provider
func NewBaseOAuthProvider(name, clientID, clientSecret, authURL, tokenURL, userURL, redirectURL string, scopes []string, logger *zap.Logger) *BaseOAuthProvider {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},
		RedirectURL: redirectURL,
		Scopes:      scopes,
	}

	return &BaseOAuthProvider{
		config:  config,
		name:    name,
		userURL: userURL,
		logger:  logger,
		client:  &http.Client{},
	}
}

// GetConfig returns OAuth2 configuration
func (p *BaseOAuthProvider) GetConfig() *oauth2.Config {
	return p.config
}

// GetProviderName returns the provider name
func (p *BaseOAuthProvider) GetProviderName() string {
	return p.name
}

// GetUserInfo retrieves user information using access token
func (p *BaseOAuthProvider) GetUserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error) {
	if p.userURL == "" {
		return nil, fmt.Errorf("user info URL not configured for provider %s", p.name)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.userURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user info request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info request failed with status %d", resp.StatusCode)
	}

	var userInfo map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	return p.parseUserInfo(userInfo), nil
}

// ValidateToken validates a token with the provider
func (p *BaseOAuthProvider) ValidateToken(ctx context.Context, tokenString string) (*UserInfo, error) {
	token := &oauth2.Token{AccessToken: tokenString}
	return p.GetUserInfo(ctx, token)
}

// parseUserInfo parses user info from provider response (override in specific providers)
func (p *BaseOAuthProvider) parseUserInfo(data map[string]interface{}) *UserInfo {
	userInfo := &UserInfo{
		Metadata: make(map[string]string),
	}

	// Generic parsing - specific providers should override this
	if id, ok := data["id"].(string); ok {
		userInfo.ID = id
	}
	if email, ok := data["email"].(string); ok {
		userInfo.Email = email
	}
	if name, ok := data["name"].(string); ok {
		userInfo.Name = name
	}
	if username, ok := data["login"].(string); ok { // GitHub uses "login"
		userInfo.Username = username
	} else if username, ok := data["username"].(string); ok {
		userInfo.Username = username
	}

	return userInfo
}

// GoogleOAuthProvider implements Google OAuth2
type GoogleOAuthProvider struct {
	*BaseOAuthProvider
}

// NewGoogleOAuthProvider creates a new Google OAuth provider
func NewGoogleOAuthProvider(clientID, clientSecret, redirectURL string, scopes []string, logger *zap.Logger) *GoogleOAuthProvider {
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	}

	base := &BaseOAuthProvider{
		config:  config,
		name:    "google",
		userURL: "https://www.googleapis.com/oauth2/v2/userinfo",
		logger:  logger,
		client:  &http.Client{},
	}

	return &GoogleOAuthProvider{BaseOAuthProvider: base}
}

// parseUserInfo parses Google-specific user info
func (p *GoogleOAuthProvider) parseUserInfo(data map[string]interface{}) *UserInfo {
	userInfo := &UserInfo{
		Metadata: make(map[string]string),
	}

	if id, ok := data["id"].(string); ok {
		userInfo.ID = id
	}
	if email, ok := data["email"].(string); ok {
		userInfo.Email = email
	}
	if name, ok := data["name"].(string); ok {
		userInfo.Name = name
	}
	if picture, ok := data["picture"].(string); ok {
		userInfo.Metadata["picture"] = picture
	}
	if verified, ok := data["verified_email"].(bool); ok {
		userInfo.Metadata["email_verified"] = fmt.Sprintf("%t", verified)
	}

	return userInfo
}

// GitHubOAuthProvider implements GitHub OAuth2
type GitHubOAuthProvider struct {
	*BaseOAuthProvider
}

// NewGitHubOAuthProvider creates a new GitHub OAuth provider
func NewGitHubOAuthProvider(clientID, clientSecret, redirectURL string, scopes []string, logger *zap.Logger) *GitHubOAuthProvider {
	if len(scopes) == 0 {
		scopes = []string{"user:email"}
	}

	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://github.com/login/oauth/authorize",
			TokenURL: "https://github.com/login/oauth/access_token",
		},
		RedirectURL: redirectURL,
		Scopes:      scopes,
	}

	base := &BaseOAuthProvider{
		config:  config,
		name:    "github",
		userURL: "https://api.github.com/user",
		logger:  logger,
		client:  &http.Client{},
	}

	return &GitHubOAuthProvider{BaseOAuthProvider: base}
}

// parseUserInfo parses GitHub-specific user info
func (p *GitHubOAuthProvider) parseUserInfo(data map[string]interface{}) *UserInfo {
	userInfo := &UserInfo{
		Metadata: make(map[string]string),
	}

	if id, ok := data["id"].(float64); ok {
		userInfo.ID = fmt.Sprintf("%.0f", id)
	}
	if email, ok := data["email"].(string); ok {
		userInfo.Email = email
	}
	if name, ok := data["name"].(string); ok {
		userInfo.Name = name
	}
	if login, ok := data["login"].(string); ok {
		userInfo.Username = login
	}
	if avatarURL, ok := data["avatar_url"].(string); ok {
		userInfo.Metadata["avatar_url"] = avatarURL
	}
	if company, ok := data["company"].(string); ok && company != "" {
		userInfo.Metadata["company"] = company
	}

	return userInfo
}

// CustomOAuthProvider implements custom OAuth2 providers
type CustomOAuthProvider struct {
	*BaseOAuthProvider
}

// NewCustomOAuthProvider creates a new custom OAuth provider
func NewCustomOAuthProvider(name, clientID, clientSecret, authURL, tokenURL, userURL, redirectURL string, scopes []string, logger *zap.Logger) *CustomOAuthProvider {
	base := NewBaseOAuthProvider(name, clientID, clientSecret, authURL, tokenURL, userURL, redirectURL, scopes, logger)
	return &CustomOAuthProvider{BaseOAuthProvider: base}
}

// ProviderFactory creates OAuth providers based on configuration
type ProviderFactory struct {
	logger *zap.Logger
}

// NewProviderFactory creates a new provider factory
func NewProviderFactory(logger *zap.Logger) *ProviderFactory {
	return &ProviderFactory{logger: logger}
}

// CreateProvider creates an OAuth provider based on configuration
func (f *ProviderFactory) CreateProvider(name string, config map[string]interface{}) (OAuthProvider, error) {
	providerType, ok := config["type"].(string)
	if !ok {
		return nil, fmt.Errorf("provider type not specified for %s", name)
	}

	clientID, ok := config["client_id"].(string)
	if !ok {
		return nil, fmt.Errorf("client_id not specified for provider %s", name)
	}

	clientSecret, ok := config["client_secret"].(string)
	if !ok {
		return nil, fmt.Errorf("client_secret not specified for provider %s", name)
	}

	redirectURL, ok := config["redirect_url"].(string)
	if !ok {
		return nil, fmt.Errorf("redirect_url not specified for provider %s", name)
	}

	scopes := []string{}
	if scopesInterface, ok := config["scopes"]; ok {
		if scopesSlice, ok := scopesInterface.([]interface{}); ok {
			for _, scope := range scopesSlice {
				if scopeStr, ok := scope.(string); ok {
					scopes = append(scopes, scopeStr)
				}
			}
		}
	}

	switch strings.ToLower(providerType) {
	case "google":
		return NewGoogleOAuthProvider(clientID, clientSecret, redirectURL, scopes, f.logger), nil
	case "github":
		return NewGitHubOAuthProvider(clientID, clientSecret, redirectURL, scopes, f.logger), nil
	case "custom":
		authURL, ok := config["auth_url"].(string)
		if !ok {
			return nil, fmt.Errorf("auth_url not specified for custom provider %s", name)
		}
		tokenURL, ok := config["token_url"].(string)
		if !ok {
			return nil, fmt.Errorf("token_url not specified for custom provider %s", name)
		}
		userURL, _ := config["user_info_url"].(string) // Optional
		return NewCustomOAuthProvider(name, clientID, clientSecret, authURL, tokenURL, userURL, redirectURL, scopes, f.logger), nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}