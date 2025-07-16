# Authentication in Maybe Don't MCP Gateway

This document describes the OAuth 2.1 with PKCE authentication implementation in the Maybe Don't MCP Gateway.

## Overview

The authentication system provides secure access control for the MCP gateway using industry-standard OAuth 2.1 with PKCE (Proof Key for Code Exchange). The implementation follows RFC 7636 and OAuth 2.1 specifications.

## Features

- **OAuth 2.1 with PKCE**: Full implementation of OAuth 2.1 authorization code flow with PKCE
- **Multiple Providers**: Support for Google, GitHub, and custom OAuth providers
- **JWT Tokens**: Secure JWT token generation and validation using HMAC-SHA256
- **Flexible Storage**: Pluggable token storage (memory, Redis, Vault)
- **Secrets Management**: Integrated secrets management with multiple backends
- **Audit Logging**: Comprehensive audit logging for all authentication events
- **Role-Based Access**: Support for role-based access control
- **Session Management**: Configurable session timeouts and refresh thresholds

## Architecture

The authentication system consists of several key components:

### Core Components

1. **AuthManager**: Central authentication manager that coordinates all auth components
2. **OAuth2Authenticator**: Implements OAuth 2.1 with PKCE flow
3. **JWTManager**: Handles JWT token creation and validation
4. **PKCEGenerator**: Generates and validates PKCE challenges
5. **TokenStorage**: Stores tokens and PKCE challenges
6. **SecretsManager**: Manages OAuth client secrets and signing keys
7. **AuthHandlers**: HTTP handlers for OAuth endpoints

### Interfaces

The system uses interfaces for maximum flexibility:

```go
type Authenticator interface {
    GetAuthURL(ctx context.Context, clientID, state, codeChallenge, codeChallengeMethod string) (string, error)
    ExchangeCode(ctx context.Context, code, codeVerifier, state string) (*TokenInfo, error)
    ValidateToken(ctx context.Context, token string) (*AuthContext, error)
    RefreshToken(ctx context.Context, refreshToken string) (*TokenInfo, error)
    RevokeToken(ctx context.Context, token string) error
}

type TokenStorage interface {
    StoreToken(ctx context.Context, userID string, token *TokenInfo, ttl time.Duration) error
    GetToken(ctx context.Context, userID string) (*TokenInfo, error)
    DeleteToken(ctx context.Context, userID string) error
    StorePKCEChallenge(ctx context.Context, state string, challenge *PKCEChallenge, ttl time.Duration) error
    GetPKCEChallenge(ctx context.Context, state string) (*PKCEChallenge, error)
    DeletePKCEChallenge(ctx context.Context, state string) error
}

type SecretsManager interface {
    GetSecret(ctx context.Context, key string) (string, error)
    SetSecret(ctx context.Context, key, value string) error
    DeleteSecret(ctx context.Context, key string) error
}
```

## Configuration

### Basic OAuth2 Configuration

```yaml
auth:
  type: oauth2
  oauth2:
    enabled: true
    session_timeout: "24h"
    refresh_threshold: "5m"
    
    providers:
      google:
        type: google
        client_id: "your-google-client-id"
        client_secret: "your-google-client-secret"
        redirect_url: "http://localhost:8080/oauth/callback"
        scopes:
          - "openid"
          - "profile"
          - "email"
    
    clients:
      cli-client:
        name: "CLI Client"
        client_id: "cli-client"
        redirect_uris:
          - "http://localhost:8080/oauth/callback"
        scopes:
          - "read"
          - "write"
        public: true  # PKCE required
        roles:
          - "user"
    
    token_storage:
      type: memory
      config: {}

  jwt:
    issuer: "maybe-dont-gateway"
    audience:
      - "maybe-dont"
    signing_key: "your-jwt-signing-key"

secrets:
  type: memory
  config: {}
```

### Supported Providers

#### Google OAuth2
```yaml
providers:
  google:
    type: google
    client_id: "your-google-client-id"
    client_secret: "your-google-client-secret"
    redirect_url: "http://localhost:8080/oauth/callback"
    scopes:
      - "openid"
      - "profile"
      - "email"
```

#### GitHub OAuth2
```yaml
providers:
  github:
    type: github
    client_id: "your-github-client-id"
    client_secret: "your-github-client-secret"
    redirect_url: "http://localhost:8080/oauth/callback"
    scopes:
      - "user:email"
```

#### Custom OAuth2 Provider
```yaml
providers:
  custom:
    type: custom
    client_id: "your-custom-client-id"
    client_secret: "your-custom-client-secret"
    auth_url: "https://your-provider.com/oauth/authorize"
    token_url: "https://your-provider.com/oauth/token"
    user_info_url: "https://your-provider.com/oauth/userinfo"
    redirect_url: "http://localhost:8080/oauth/callback"
    scopes:
      - "read"
      - "write"
```

### Storage Backends

#### Memory Storage (Development)
```yaml
token_storage:
  type: memory
  config: {}
```

#### Redis Storage (Production)
```yaml
token_storage:
  type: redis
  config:
    address: "localhost:6379"
    password: ""
    db: 0
```

#### Vault Storage (Enterprise)
```yaml
token_storage:
  type: vault
  config:
    address: "https://vault.example.com"
    token: "vault-token"
    path: "secret/tokens"
```

## OAuth 2.1 Flow

### Authorization Code Flow with PKCE

1. **Client Registration**: Client registers with the gateway and receives a client_id
2. **Authorization Request**: Client redirects user to `/oauth/authorize` with PKCE challenge
3. **User Authentication**: User authenticates with the configured OAuth provider
4. **Authorization Grant**: Provider redirects back with authorization code
5. **Token Exchange**: Client exchanges code + code_verifier for access token at `/oauth/token`
6. **API Access**: Client uses JWT token to access protected MCP endpoints

### Endpoints

- `GET /oauth/authorize` - Start OAuth authorization flow
- `POST /oauth/token` - Exchange authorization code for tokens
- `POST /oauth/revoke` - Revoke access token
- `GET /oauth/userinfo` - Get user information
- `GET /oauth/callback` - OAuth provider callback

## Security Features

### PKCE (Proof Key for Code Exchange)

All OAuth flows use PKCE to prevent authorization code interception attacks:

- **Code Verifier**: Cryptographically random string (43-128 characters)
- **Code Challenge**: SHA256 hash of code verifier, base64url encoded
- **Challenge Method**: Always "S256" for maximum security

### JWT Security

- **Algorithm**: HMAC-SHA256 for symmetric signing
- **Claims**: Standard JWT claims plus custom auth context
- **Expiration**: Configurable token lifetime with refresh capability
- **Audience**: Validates token is intended for this gateway

### Audit Logging

All authentication events are logged for security monitoring:

```go
type AuthEvent struct {
    Timestamp    time.Time         `json:"timestamp"`
    Event        string            `json:"event"`
    UserID       string            `json:"user_id,omitempty"`
    ClientID     string            `json:"client_id,omitempty"`
    Provider     string            `json:"provider,omitempty"`
    Success      bool              `json:"success"`
    Error        string            `json:"error,omitempty"`
    Metadata     map[string]string `json:"metadata,omitempty"`
}
```

## Usage Examples

### CLI Client Authentication

```bash
# Start OAuth flow
curl "http://localhost:8080/oauth/authorize?client_id=cli-client&response_type=code&code_challenge=CHALLENGE&code_challenge_method=S256&state=STATE"

# Exchange code for token
curl -X POST "http://localhost:8080/oauth/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=authorization_code&code=CODE&code_verifier=VERIFIER&client_id=cli-client"

# Use token to access MCP
curl "http://localhost:8080/mcp" \
  -H "Authorization: Bearer JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}'
```

### Web Application Integration

```javascript
// Generate PKCE challenge
const codeVerifier = generateCodeVerifier();
const codeChallenge = await generateCodeChallenge(codeVerifier);

// Redirect to authorization endpoint
const authUrl = new URL('http://localhost:8080/oauth/authorize');
authUrl.searchParams.set('client_id', 'web-client');
authUrl.searchParams.set('response_type', 'code');
authUrl.searchParams.set('code_challenge', codeChallenge);
authUrl.searchParams.set('code_challenge_method', 'S256');
authUrl.searchParams.set('state', generateState());

window.location.href = authUrl.toString();
```

## Deployment Considerations

### Production Security

1. **Use HTTPS**: Always use HTTPS in production
2. **Secure Secrets**: Use proper secrets management (Vault, AWS Secrets Manager)
3. **Token Storage**: Use Redis or database for token storage
4. **Monitoring**: Monitor authentication events and failures
5. **Rate Limiting**: Implement rate limiting on auth endpoints

### High Availability

1. **Stateless Design**: JWT tokens enable stateless authentication
2. **Shared Storage**: Use shared Redis/database for token storage
3. **Load Balancing**: Multiple gateway instances can share auth state
4. **Session Persistence**: Configure appropriate session timeouts

### Monitoring and Alerting

Monitor these key metrics:

- Authentication success/failure rates
- Token refresh patterns
- PKCE challenge validation failures
- Unusual access patterns
- Provider authentication latency

## Troubleshooting

### Common Issues

1. **Invalid PKCE Challenge**: Ensure code_verifier matches code_challenge
2. **Token Expiration**: Check token expiration and refresh logic
3. **Provider Configuration**: Verify OAuth provider settings
4. **Redirect URI Mismatch**: Ensure redirect URIs match exactly
5. **Scope Issues**: Check requested scopes are allowed for client

### Debug Logging

Enable debug logging to troubleshoot authentication issues:

```yaml
logging:
  level: debug
```

This will log detailed information about OAuth flows, token validation, and PKCE challenges.

## Future Enhancements

Planned improvements include:

- **OpenID Connect**: Full OIDC support with ID tokens
- **Device Flow**: OAuth device authorization flow for CLI tools
- **Multi-Factor Authentication**: Integration with MFA providers
- **Certificate Authentication**: mTLS client certificate support
- **API Key Authentication**: Simple API key authentication option
- **SAML Integration**: SAML 2.0 identity provider support