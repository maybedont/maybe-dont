package gateway

import (
	"context"
	"net/http"

	"github.com/maybedont/maybe-dont/internal/config"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// ServiceCredentialsKey stores per-client credentials extracted from request headers
	// Value type: *ServiceCredentials
	ServiceCredentialsKey contextKey = "service_credentials"

	// ClientIPKey stores the IP address of the upstream client
	// Value type: string
	ClientIPKey contextKey = "client_ip"

	// UserAgentKey stores the User-Agent header from the upstream client
	// Value type: string
	UserAgentKey contextKey = "user_agent"

	// RawRequestHeadersKey stores the raw HTTP headers from the incoming request
	// for lazy credential extraction when making downstream requests.
	// Value type: http.Header
	RawRequestHeadersKey contextKey = "raw_request_headers"

	// CallerKey stores the caller identifier from the gateway auth header.
	// Value type: string
	CallerKey contextKey = "caller"
)

// RequestIDKey stores the request ID for tracking capabilities per session
// Value type: string
// Note: We use the same key as config.RequestIDKey to ensure consistency across packages
var RequestIDKey = config.RequestIDKey

// WithClientIP adds the client IP address to the context
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ClientIPKey, ip)
}

// GetClientIP retrieves the client IP address from the context
func GetClientIP(ctx context.Context) (string, bool) {
	ip, ok := ctx.Value(ClientIPKey).(string)
	return ip, ok
}

// WithUserAgent adds the User-Agent header to the context
func WithUserAgent(ctx context.Context, userAgent string) context.Context {
	return context.WithValue(ctx, UserAgentKey, userAgent)
}

// GetUserAgent retrieves the User-Agent header from the context
func GetUserAgent(ctx context.Context) (string, bool) {
	ua, ok := ctx.Value(UserAgentKey).(string)
	return ua, ok
}

// WithRawRequestHeaders adds the raw HTTP request headers to the context.
// This allows lazy credential extraction when making downstream requests.
func WithRawRequestHeaders(ctx context.Context, headers http.Header) context.Context {
	return context.WithValue(ctx, RawRequestHeadersKey, headers)
}

// GetRawRequestHeaders retrieves the raw HTTP request headers from the context.
func GetRawRequestHeaders(ctx context.Context) (http.Header, bool) {
	headers, ok := ctx.Value(RawRequestHeadersKey).(http.Header)
	return headers, ok
}

// WithCaller adds the caller identifier to the context.
func WithCaller(ctx context.Context, caller string) context.Context {
	return context.WithValue(ctx, CallerKey, caller)
}

// GetCaller retrieves the caller identifier from the context.
func GetCaller(ctx context.Context) (string, bool) {
	caller, ok := ctx.Value(CallerKey).(string)
	return caller, ok
}

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
