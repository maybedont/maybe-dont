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

	// Load test policies
	policies := []config.CELPolicy{
		{
			Name:       "allow-read-tool",
			Expression: `request.method == "tools/call" && request.params.name == "read_file"`,
			Action:     "allow",
			Message:    "Allowed to call read_file",
		},
		{
			Name:       "deny-delete-tool",
			Expression: `request.method == "tools/call" && request.params.name == "delete_file"`,
			Action:     "deny",
			Message:    "delete_file is not allowed",
		},
	}
	err = engine.LoadPolicies(policies)
	require.NoError(t, err)

	// Create validation chain
	chain := NewToolValidationChain(
		NewToolLoggingHandler(logger),
		NewToolCELValidationHandler(logger, engine),
	)

	tests := []struct {
		name        string
		req         mcp.CallToolRequest
		wantAllowed bool
		wantErr     bool
	}{
		{
			name: "allowed read_file request",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
				},
				Params: mcp.CallToolParams{
					Name: "read_file",
					Arguments: map[string]string{
						"command": "cat file.txt",
					},
				},
			},
			wantAllowed: true,
			wantErr:     false,
		},
		{
			name: "denied delete_file request",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
				},
				Params: mcp.CallToolParams{
					Name: "delete_file",
					Arguments: map[string]string{
						"command": "rm file.txt",
					},
				},
			},
			wantAllowed: false,
			wantErr:     false,
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

			// Verify audit logging result
			auditResult := results.Results[0]
			assert.Equal(t, "Audit Logging", auditResult.PolicyName)
			assert.Equal(t, "audit", auditResult.PolicyType)
			assert.True(t, auditResult.Allowed)
			assert.Empty(t, auditResult.Results)

			// Verify CEL validation result
			celResult := results.Results[1]
			assert.Equal(t, "CEL Policy", celResult.PolicyName)
			assert.Equal(t, "cel", celResult.PolicyType)
			assert.Equal(t, tt.wantAllowed, celResult.Allowed)
			assert.NotEmpty(t, celResult.Results)
		})
	}
}

func TestLoggingHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
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
			assert.Equal(t, "Audit Logging", result.PolicyName)
			assert.Equal(t, "audit", result.PolicyType)
			assert.Empty(t, result.Results)
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
