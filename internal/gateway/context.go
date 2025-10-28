package gateway

import "context"

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// ServiceCredentialsKey stores per-client credentials extracted from request headers
	// Value type: *ServiceCredentials
	ServiceCredentialsKey contextKey = "service_credentials"

	// SessionIDKey stores the session ID for tracking capabilities per session
	// Value type: string
	SessionIDKey contextKey = "session_id"
)

// ServiceCredentials stores authentication credentials for all downstream MCP clients.
// It maps client names to their respective credential sets.
type ServiceCredentials struct {
	clients map[string]*ClientCredentials
}

// NewServiceCredentials creates a new ServiceCredentials instance.
func NewServiceCredentials() *ServiceCredentials {
	return &ServiceCredentials{
		clients: make(map[string]*ClientCredentials),
	}
}

// GetClient retrieves credentials for a specific client.
func (sc *ServiceCredentials) GetClient(clientName string) (*ClientCredentials, bool) {
	creds, ok := sc.clients[clientName]
	return creds, ok
}

// SetClient stores credentials for a specific client.
func (sc *ServiceCredentials) SetClient(clientName string, creds *ClientCredentials) {
	sc.clients[clientName] = creds
}

// ClientCredentials stores authentication headers for a single downstream client.
// It maps header names to their values.
type ClientCredentials struct {
	Headers map[string]string
}

// NewClientCredentials creates a new ClientCredentials instance.
func NewClientCredentials() *ClientCredentials {
	return &ClientCredentials{
		Headers: make(map[string]string),
	}
}

// SetHeader sets a header value in the credentials.
func (cc *ClientCredentials) SetHeader(name, value string) {
	cc.Headers[name] = value
}

// GetHeader retrieves a header value from the credentials.
func (cc *ClientCredentials) GetHeader(name string) (string, bool) {
	val, ok := cc.Headers[name]
	return val, ok
}

// WithServiceCredentials adds service credentials to the context
func WithServiceCredentials(ctx context.Context, credentials *ServiceCredentials) context.Context {
	return context.WithValue(ctx, ServiceCredentialsKey, credentials)
}

// GetServiceCredentials retrieves all credentials for a specific client from the context
func GetServiceCredentials(ctx context.Context, clientName string) (*ClientCredentials, bool) {
	allCreds, ok := ctx.Value(ServiceCredentialsKey).(*ServiceCredentials)
	if !ok {
		return nil, false
	}

	return allCreds.GetClient(clientName)
}
