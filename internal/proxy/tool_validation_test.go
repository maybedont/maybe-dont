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
	engine, err := NewCELPolicyEngine(logger, "deny")
	require.NoError(t, err)

	// Load default policies
	defaultPolicies := []config.CELPolicy{
		{
			Name:       "allow-tools-call",
			Expression: `request.method == "tools/call" && request.params.name == "read_file"`,
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
		name           string
		req            mcp.CallToolRequest
		wantAllowed    bool
		wantErr        bool
		wantResultType string
	}{
		{
			name: "valid tool call",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{},
				},
				Params: mcp.CallToolParams{
					Name: "read_file",
					Arguments: map[string]string{
						"command": "cat file.txt",
					},
				},
			},
			wantAllowed:    true,
			wantErr:        false,
			wantResultType: "cel",
		},
		{
			name: "invalid method",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "invalid_method",
				},
			},
			wantAllowed:    false,
			wantErr:        false,
			wantResultType: "cel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := chain.Handle(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAllowed, results.Allowed)

			// Check that we have the expected validation results
			found := false
			for _, result := range results.Results {
				if result.PolicyType == tt.wantResultType {
					found = true
					assert.Equal(t, tt.wantAllowed, result.Allowed)
				}
			}
			assert.True(t, found, "Expected to find validation result of type %s", tt.wantResultType)
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
			result, err := handler.HandleToolCall(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.True(t, result.Allowed)
			assert.Equal(t, "Logging", result.PolicyName)
			assert.Equal(t, "logging", result.PolicyType)
		})
	}
}

func TestCELValidationHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	engine, err := NewCELPolicyEngine(logger, "deny")
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
			result, err := handler.HandleToolCall(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.True(t, result.Allowed)
			assert.Equal(t, "CEL Policy", result.PolicyName)
			assert.Equal(t, "cel", result.PolicyType)
		})
	}
}
