package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// AuthHandlers provides HTTP handlers for OAuth2 authentication
type AuthHandlers struct {
	authenticator *OAuth2Authenticator
	logger        *zap.Logger
	auditLogger   *zap.Logger
}

// NewAuthHandlers creates new authentication handlers
func NewAuthHandlers(authenticator *OAuth2Authenticator, logger *zap.Logger, auditLogger *zap.Logger) *AuthHandlers {
	return &AuthHandlers{
		authenticator: authenticator,
		logger:        logger,
		auditLogger:   auditLogger,
	}
}

// AuthorizeHandler handles OAuth2 authorization requests
func (h *AuthHandlers) AuthorizeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Parse query parameters
	query := r.URL.Query()
	clientID := query.Get("client_id")
	redirectURI := query.Get("redirect_uri")
	responseType := query.Get("response_type")
	state := query.Get("state")
	codeChallenge := query.Get("code_challenge")
	codeChallengeMethod := query.Get("code_challenge_method")
	scope := query.Get("scope")
	provider := query.Get("provider")

	// Validate required parameters
	if clientID == "" {
		h.writeError(w, "invalid_request", "client_id is required", http.StatusBadRequest)
		return
	}
	if redirectURI == "" {
		h.writeError(w, "invalid_request", "redirect_uri is required", http.StatusBadRequest)
		return
	}
	if responseType != "code" {
		h.writeError(w, "unsupported_response_type", "only 'code' response type is supported", http.StatusBadRequest)
		return
	}
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		h.writeError(w, "invalid_request", "PKCE with S256 method is required", http.StatusBadRequest)
		return
	}

	// Default provider if not specified
	if provider == "" {
		provider = "google" // Default to Google
	}

	// Parse scopes
	var scopes []string
	if scope != "" {
		scopes = strings.Split(scope, " ")
	}

	// Validate client and redirect URI
	if err := h.authenticator.ValidateClient(ctx, clientID, redirectURI); err != nil {
		h.logger.Warn("Client validation failed", zap.String("client_id", clientID), zap.Error(err))
		h.writeError(w, "invalid_client", "invalid client or redirect URI", http.StatusBadRequest)
		return
	}

	// Initiate OAuth flow
	challenge, authURL, err := h.authenticator.InitiateOAuthFlow(ctx, clientID, redirectURI, provider, scopes)
	if err != nil {
		h.logger.Error("Failed to initiate OAuth flow", zap.Error(err))
		h.writeError(w, "server_error", "failed to initiate OAuth flow", http.StatusInternalServerError)
		return
	}

	// Store state in challenge for validation
	if state != "" {
		challenge.State = state
		if err := h.authenticator.tokenStorage.StorePKCEChallenge(ctx, state, challenge, 10*time.Minute); err != nil {
			h.logger.Error("Failed to update PKCE challenge with state", zap.Error(err))
		}
	}

	// Redirect to OAuth provider
	http.Redirect(w, r, authURL, http.StatusFound)
}

// CallbackHandler handles OAuth2 callback from providers
func (h *AuthHandlers) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Parse query parameters
	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	errorParam := query.Get("error")
	errorDescription := query.Get("error_description")

	// Check for OAuth errors
	if errorParam != "" {
		h.logger.Warn("OAuth callback error", zap.String("error", errorParam), zap.String("description", errorDescription))
		h.writeError(w, errorParam, errorDescription, http.StatusBadRequest)
		return
	}

	// Validate required parameters
	if code == "" {
		h.writeError(w, "invalid_request", "authorization code is required", http.StatusBadRequest)
		return
	}
	if state == "" {
		h.writeError(w, "invalid_request", "state parameter is required", http.StatusBadRequest)
		return
	}

	// Validate that PKCE challenge exists for this state
	_, err := h.authenticator.tokenStorage.GetPKCEChallenge(ctx, state)
	if err != nil {
		h.logger.Warn("PKCE challenge not found", zap.String("state", state), zap.Error(err))
		h.writeError(w, "invalid_request", "invalid state parameter", http.StatusBadRequest)
		return
	}

	// For this example, we'll assume the code verifier is passed in a header or form parameter
	// In a real implementation, this would come from the client
	codeVerifier := r.Header.Get("X-Code-Verifier")
	if codeVerifier == "" {
		codeVerifier = r.FormValue("code_verifier")
	}
	if codeVerifier == "" {
		h.writeError(w, "invalid_request", "code_verifier is required", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens
	tokenInfo, err := h.authenticator.ExchangeCode(ctx, code, codeVerifier, state)
	if err != nil {
		h.logger.Error("Token exchange failed", zap.Error(err))
		h.writeError(w, "invalid_grant", "token exchange failed", http.StatusBadRequest)
		return
	}

	// Create JWT token
	jwtToken, err := h.authenticator.CreateJWTFromTokenInfo(ctx, tokenInfo)
	if err != nil {
		h.logger.Error("JWT creation failed", zap.Error(err))
		h.writeError(w, "server_error", "failed to create JWT token", http.StatusInternalServerError)
		return
	}

	// Return token response
	response := map[string]interface{}{
		"access_token": jwtToken,
		"token_type":   "Bearer",
		"expires_in":   int(time.Until(tokenInfo.ExpiresAt).Seconds()),
		"scope":        strings.Join(tokenInfo.Scopes, " "),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	json.NewEncoder(w).Encode(response)
}

// TokenHandler handles token endpoint requests (for refresh, etc.)
func (h *AuthHandlers) TokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, "invalid_request", "only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		h.writeError(w, "invalid_request", "failed to parse form data", http.StatusBadRequest)
		return
	}

	grantType := r.FormValue("grant_type")
	switch grantType {
	case "refresh_token":
		h.handleRefreshToken(w, r)
	default:
		h.writeError(w, "unsupported_grant_type", fmt.Sprintf("grant type '%s' is not supported", grantType), http.StatusBadRequest)
	}
}

// handleRefreshToken handles refresh token requests
func (h *AuthHandlers) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	refreshToken := r.FormValue("refresh_token")
	if refreshToken == "" {
		h.writeError(w, "invalid_request", "refresh_token is required", http.StatusBadRequest)
		return
	}

	// Refresh token
	tokenInfo, err := h.authenticator.RefreshToken(ctx, refreshToken)
	if err != nil {
		h.logger.Error("Token refresh failed", zap.Error(err))
		h.writeError(w, "invalid_grant", "token refresh failed", http.StatusBadRequest)
		return
	}

	// Create new JWT token
	jwtToken, err := h.authenticator.CreateJWTFromTokenInfo(ctx, tokenInfo)
	if err != nil {
		h.logger.Error("JWT creation failed", zap.Error(err))
		h.writeError(w, "server_error", "failed to create JWT token", http.StatusInternalServerError)
		return
	}

	// Return token response
	response := map[string]interface{}{
		"access_token": jwtToken,
		"token_type":   "Bearer",
		"expires_in":   int(time.Until(tokenInfo.ExpiresAt).Seconds()),
		"scope":        strings.Join(tokenInfo.Scopes, " "),
	}

	if tokenInfo.RefreshToken != "" {
		response["refresh_token"] = tokenInfo.RefreshToken
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	json.NewEncoder(w).Encode(response)
}

// RevokeHandler handles token revocation requests
func (h *AuthHandlers) RevokeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, "invalid_request", "only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	
	// Parse form data
	if err := r.ParseForm(); err != nil {
		h.writeError(w, "invalid_request", "failed to parse form data", http.StatusBadRequest)
		return
	}

	token := r.FormValue("token")
	if token == "" {
		h.writeError(w, "invalid_request", "token is required", http.StatusBadRequest)
		return
	}

	// Revoke token
	if err := h.authenticator.RevokeToken(ctx, token); err != nil {
		h.logger.Error("Token revocation failed", zap.Error(err))
		// Don't return error to client for security reasons
	}

	// Always return success for revocation
	w.WriteHeader(http.StatusOK)
}

// UserInfoHandler returns information about the authenticated user
func (h *AuthHandlers) UserInfoHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Extract token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		h.writeError(w, "invalid_request", "Authorization header is required", http.StatusUnauthorized)
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		h.writeError(w, "invalid_request", "invalid Authorization header format", http.StatusUnauthorized)
		return
	}

	token := parts[1]

	// Authenticate token
	authCtx, err := h.authenticator.Authenticate(ctx, token)
	if err != nil {
		h.logger.Warn("Authentication failed", zap.Error(err))
		h.writeError(w, "invalid_token", "token authentication failed", http.StatusUnauthorized)
		return
	}

	// Return user info
	userInfo := map[string]interface{}{
		"sub":      authCtx.UserID,
		"client_id": authCtx.ClientID,
		"roles":    authCtx.Roles,
		"scopes":   authCtx.Scopes,
		"provider": authCtx.Provider,
		"exp":      authCtx.ExpiresAt.Unix(),
		"iat":      authCtx.IssuedAt.Unix(),
	}

	// Add metadata
	for key, value := range authCtx.Metadata {
		userInfo[key] = value
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userInfo)
}

// writeError writes an OAuth2 error response
func (h *AuthHandlers) writeError(w http.ResponseWriter, errorCode, description string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	errorResponse := map[string]string{
		"error":             errorCode,
		"error_description": description,
	}
	
	json.NewEncoder(w).Encode(errorResponse)
}

// AuthMiddleware provides authentication middleware for HTTP requests
func (h *AuthHandlers) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		
		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			h.writeError(w, "invalid_request", "Authorization header is required", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			h.writeError(w, "invalid_request", "invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]

		// Authenticate token
		authCtx, err := h.authenticator.Authenticate(ctx, token)
		if err != nil {
			h.logger.Warn("Authentication failed", zap.Error(err))
			h.writeError(w, "invalid_token", "token authentication failed", http.StatusUnauthorized)
			return
		}

		// Add auth context to request context
		ctx = context.WithValue(ctx, "auth_context", authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetAuthContext extracts authentication context from request context
func GetAuthContext(ctx context.Context) (*AuthContext, bool) {
	authCtx, ok := ctx.Value("auth_context").(*AuthContext)
	return authCtx, ok
}