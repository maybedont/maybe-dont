package proxy

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

func TestValidationChain(t *testing.T) {
	// Create validation chain
	chain := NewToolValidationChain(NewToolLoggingHandler(), NewToolCELValidationHandler())

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
			wantErr: false, // Currently no validation for method
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
	// Create handler
	handler := NewToolLoggingHandler()

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
	// Create handler
	handler := NewToolCELValidationHandler()

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
