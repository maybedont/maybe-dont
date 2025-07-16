package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCEGenerator handles PKCE (Proof Key for Code Exchange) operations
type PKCEGenerator struct{}

// NewPKCEGenerator creates a new PKCE generator
func NewPKCEGenerator() *PKCEGenerator {
	return &PKCEGenerator{}
}

// GenerateChallenge generates a PKCE code verifier and challenge
func (p *PKCEGenerator) GenerateChallenge() (*PKCEChallenge, error) {
	// Generate code verifier (43-128 characters, URL-safe)
	codeVerifier, err := p.generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %w", err)
	}

	// Generate code challenge using S256 method
	codeChallenge := p.generateCodeChallenge(codeVerifier)

	// Generate state parameter
	state, err := GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	return &PKCEChallenge{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		Method:        "S256",
		State:         state,
	}, nil
}

// ValidateChallenge validates a code verifier against a challenge
func (p *PKCEGenerator) ValidateChallenge(codeVerifier, codeChallenge, method string) bool {
	if method != "S256" {
		return false
	}

	expectedChallenge := p.generateCodeChallenge(codeVerifier)
	return expectedChallenge == codeChallenge
}

// generateCodeVerifier generates a cryptographically random code verifier
func (p *PKCEGenerator) generateCodeVerifier() (string, error) {
	// Generate 32 random bytes (will result in 43 characters when base64url encoded)
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Encode using base64url without padding
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes), nil
}

// generateCodeChallenge generates a code challenge from a verifier using S256 method
func (p *PKCEGenerator) generateCodeChallenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
}