package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// JWTManager handles JWT token operations
type JWTManager struct {
	signingKey    []byte
	issuer        string
	audience      []string
	tokenDuration time.Duration
	logger        *zap.Logger
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(signingKey, issuer string, audience []string, tokenDuration time.Duration, logger *zap.Logger) *JWTManager {
	return &JWTManager{
		signingKey:    []byte(signingKey),
		issuer:        issuer,
		audience:      audience,
		tokenDuration: tokenDuration,
		logger:        logger,
	}
}

// Claims represents JWT claims for our tokens
type Claims struct {
	UserID    string            `json:"user_id"`
	ClientID  string            `json:"client_id"`
	Roles     []string          `json:"roles"`
	Scopes    []string          `json:"scopes"`
	Provider  string            `json:"provider"`
	SessionID string            `json:"session_id"`
	Metadata  map[string]string `json:"metadata"`
	jwt.RegisteredClaims
}

// GenerateToken generates a JWT token for the given auth context
func (j *JWTManager) GenerateToken(ctx context.Context, authCtx *AuthContext) (string, error) {
	now := time.Now()
	expiresAt := now.Add(j.tokenDuration)
	
	claims := &Claims{
		UserID:    authCtx.UserID,
		ClientID:  authCtx.ClientID,
		Roles:     authCtx.Roles,
		Scopes:    authCtx.Scopes,
		Provider:  authCtx.Provider,
		SessionID: authCtx.SessionID,
		Metadata:  authCtx.Metadata,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   authCtx.UserID,
			Audience:  j.audience,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        authCtx.SessionID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(j.signingKey)
	if err != nil {
		j.logger.Error("Failed to sign JWT token", zap.Error(err))
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	j.logger.Debug("Generated JWT token",
		zap.String("user_id", authCtx.UserID),
		zap.String("client_id", authCtx.ClientID),
		zap.String("session_id", authCtx.SessionID),
		zap.Time("expires_at", expiresAt))

	return tokenString, nil
}

// ValidateToken validates a JWT token and returns the auth context
func (j *JWTManager) ValidateToken(ctx context.Context, tokenString string) (*AuthContext, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.signingKey, nil
	})

	if err != nil {
		j.logger.Warn("Failed to parse JWT token", zap.Error(err))
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		j.logger.Warn("Invalid JWT token claims")
		return nil, fmt.Errorf("invalid token claims")
	}

	// Validate audience
	if len(j.audience) > 0 {
		validAudience := false
		for _, aud := range j.audience {
			for _, claimAud := range claims.Audience {
				if aud == claimAud {
					validAudience = true
					break
				}
			}
			if validAudience {
				break
			}
		}
		if !validAudience {
			j.logger.Warn("Invalid JWT token audience", zap.Strings("expected", j.audience), zap.Strings("actual", claims.Audience))
			return nil, fmt.Errorf("invalid token audience")
		}
	}

	// Validate issuer
	if j.issuer != "" && claims.Issuer != j.issuer {
		j.logger.Warn("Invalid JWT token issuer", zap.String("expected", j.issuer), zap.String("actual", claims.Issuer))
		return nil, fmt.Errorf("invalid token issuer")
	}

	authCtx := &AuthContext{
		UserID:    claims.UserID,
		ClientID:  claims.ClientID,
		Roles:     claims.Roles,
		Scopes:    claims.Scopes,
		Provider:  claims.Provider,
		SessionID: claims.SessionID,
		ExpiresAt: claims.ExpiresAt.Time,
		Metadata:  claims.Metadata,
		IssuedAt:  claims.IssuedAt.Time,
	}

	j.logger.Debug("Validated JWT token",
		zap.String("user_id", authCtx.UserID),
		zap.String("client_id", authCtx.ClientID),
		zap.String("session_id", authCtx.SessionID))

	return authCtx, nil
}

// GenerateRandomString generates a cryptographically secure random string
func GenerateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

// GenerateState generates a random state parameter for OAuth2
func GenerateState() (string, error) {
	return GenerateRandomString(32)
}

// GenerateSessionID generates a random session ID
func GenerateSessionID() (string, error) {
	return GenerateRandomString(32)
}