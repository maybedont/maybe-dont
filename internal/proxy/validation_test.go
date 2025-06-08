package proxy

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

func TestValidationChain(t *testing.T) {
	// Create validation chain
	chain := NewValidationChain(NewLoggingHandler(), NewCELValidationHandler())

	tests := []struct {
		name    string
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name: "basic tool call",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{},
					},
				},
			},
			wantErr: false, // No CEL validation yet
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
	handler := NewLoggingHandler()

	// Test request
	req := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: "tools/call",
			Params: mcp.RequestParams{
				Meta: &mcp.Meta{},
			},
		},
	}

	// Test handler
	err := handler.Handle(context.Background(), req)
	assert.NoError(t, err)
}

func TestCELValidationHandler(t *testing.T) {
	// Create handler
	handler := NewCELValidationHandler()

	// Test request
	req := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: "tools/call",
			Params: mcp.RequestParams{
				Meta: &mcp.Meta{},
			},
		},
	}

	// Test handler
	err := handler.Handle(context.Background(), req)
	assert.NoError(t, err) // No CEL validation yet
}
