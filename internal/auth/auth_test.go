package auth

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestJWTManager(t *testing.T) {
	logger := zap.NewNop()
	
	manager := NewJWTManager(
		"test-signing-key-that-is-long-enough-for-hmac",
		"test-issuer",
		[]string{"test-audience"},
		time.Hour,
		logger,
	)
	
	// Test token creation
	authCtx := &AuthContext{
		UserID:   "test-user",
		ClientID: "test-client",
		Scopes:   []string{"read", "write"},
		Roles:    []string{"user"},
	}
	
	token, err := manager.GenerateToken(context.Background(), authCtx)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}
	
	if token == "" {
		t.Fatal("Token is empty")
	}
	
	// Test token validation
	ctx := context.Background()
	validatedCtx, err := manager.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}
	
	if validatedCtx.UserID != authCtx.UserID {
		t.Errorf("Expected user ID %s, got %s", authCtx.UserID, validatedCtx.UserID)
	}
	
	if validatedCtx.ClientID != authCtx.ClientID {
		t.Errorf("Expected client ID %s, got %s", authCtx.ClientID, validatedCtx.ClientID)
	}
}

func TestPKCEGenerator(t *testing.T) {
	generator := NewPKCEGenerator()
	
	// Test PKCE challenge generation
	challenge, err := generator.GenerateChallenge()
	if err != nil {
		t.Fatalf("Failed to generate PKCE challenge: %v", err)
	}
	
	if challenge.CodeChallenge == "" {
		t.Fatal("Code challenge is empty")
	}
	
	if challenge.CodeVerifier == "" {
		t.Fatal("Code verifier is empty")
	}
	
	if challenge.Method != "S256" {
		t.Errorf("Expected method S256, got %s", challenge.Method)
	}
	
	// Test challenge validation
	valid := generator.ValidateChallenge(challenge.CodeVerifier, challenge.CodeChallenge, challenge.Method)
	if !valid {
		t.Error("PKCE challenge validation failed")
	}
	
	// Test invalid challenge
	invalid := generator.ValidateChallenge("wrong-verifier", challenge.CodeChallenge, challenge.Method)
	if invalid {
		t.Error("PKCE challenge validation should have failed for wrong verifier")
	}
}

func TestMemoryTokenStorage(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	storage := NewMemoryTokenStorage(logger)
	
	// Test PKCE challenge storage
	challenge := &PKCEChallenge{
		CodeChallenge: "test-challenge",
		CodeVerifier:  "test-verifier",
		Method:        "S256",
		ClientID:      "test-client",
		State:         "test-state",
		Provider:      "test-provider",
	}
	
	err := storage.StorePKCEChallenge(ctx, "test-state", challenge, time.Minute)
	if err != nil {
		t.Fatalf("Failed to store PKCE challenge: %v", err)
	}
	
	// Test PKCE challenge retrieval
	retrieved, err := storage.GetPKCEChallenge(ctx, "test-state")
	if err != nil {
		t.Fatalf("Failed to get PKCE challenge: %v", err)
	}
	
	if retrieved.CodeChallenge != challenge.CodeChallenge {
		t.Errorf("Expected challenge %s, got %s", challenge.CodeChallenge, retrieved.CodeChallenge)
	}
	
	// Test token storage
	tokenInfo := &TokenInfo{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		UserID:       "test-user",
		ClientID:     "test-client",
		Provider:     "test-provider",
	}
	
	err = storage.StoreToken(ctx, "test-user", tokenInfo, time.Hour)
	if err != nil {
		t.Fatalf("Failed to store token: %v", err)
	}
	
	// Test token retrieval
	retrievedToken, err := storage.GetToken(ctx, "test-user")
	if err != nil {
		t.Fatalf("Failed to get token: %v", err)
	}
	
	if retrievedToken.AccessToken != tokenInfo.AccessToken {
		t.Errorf("Expected access token %s, got %s", tokenInfo.AccessToken, retrievedToken.AccessToken)
	}
}

func TestBasicAuthComponents(t *testing.T) {
	// Test that basic auth components can be created
	logger := zap.NewNop()
	
	// Test JWT manager creation
	jwtManager := NewJWTManager(
		"test-signing-key-that-is-long-enough-for-hmac",
		"test-issuer",
		[]string{"test-audience"},
		time.Hour,
		logger,
	)
	if jwtManager == nil {
		t.Fatal("JWT manager is nil")
	}
	
	// Test PKCE generator creation
	pkceGen := NewPKCEGenerator()
	if pkceGen == nil {
		t.Fatal("PKCE generator is nil")
	}
	
	// Test memory storage creation
	storage := NewMemoryTokenStorage(logger)
	if storage == nil {
		t.Fatal("Memory storage is nil")
	}
	
	// Test memory secrets manager creation
	secretsManager := NewMemorySecretsManager(logger)
	if secretsManager == nil {
		t.Fatal("Secrets manager is nil")
	}
}