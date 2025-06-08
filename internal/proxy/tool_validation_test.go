package proxy

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap/zaptest"
)

func TestValidationChain(t *testing.T) {
	logger := zaptest.NewLogger(t)
	engine, err := NewCELPolicyEngine(logger)
	require.NoError(t, err)

	// Load default policies
	defaultPolicies := []config.CELPolicy{
		{
			Name:       "allow-tools-call",
			Expression: `request.method == "tools/call"`,
			Action:     "allow",
			Message:    "Allowed to call tools",
		},
	}
	err = engine.LoadPolicies(defaultPolicies)
	require.NoError(t, err)

	// Create validation chain
	chain := NewToolValidationChain(
		NewToolLoggingHandler(logger),
		NewToolCELValidationHandler(logger, engine),
	)

	tests := []struct {
		name    string
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name: "valid tool call with meta",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid tool call without meta",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid method",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "invalid_method",
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{},
					},
				},
			},
			wantErr: true, // Expect error for invalid method
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chain.Handle(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoggingHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create handler
	handler := NewToolLoggingHandler(logger)

	tests := []struct {
		name    string
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name: "valid request with meta",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request without meta",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.HandleToolCall(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCELValidationHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	engine, err := NewCELPolicyEngine(logger)
	require.NoError(t, err)

	// Load default policies
	defaultPolicies := []config.CELPolicy{
		{
			Name:       "allow-tools-call",
			Expression: `request.method == "tools/call"`,
			Action:     "allow",
			Message:    "Allowed to call tools",
		},
	}
	err = engine.LoadPolicies(defaultPolicies)
	require.NoError(t, err)

	// Create handler
	handler := NewToolCELValidationHandler(logger, engine)

	tests := []struct {
		name    string
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name: "valid request with meta",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request without meta",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.HandleToolCall(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
