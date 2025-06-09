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

	// Create CEL engine
	celEngine, err := NewCELPolicyEngine(logger)
	require.NoError(t, err)
	err = celEngine.LoadPolicies(policies)
	require.NoError(t, err)

	// Create validation chain
	chain := NewToolValidationChain(
		NewToolCELValidationHandler(logger, celEngine),
		NewToolLoggingHandler(logger),
	)

	tests := []struct {
		name        string
		req         mcp.CallToolRequest
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "allowed read_file request",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
				},
				Params: mcp.CallToolParams{
					Name: "read_file",
					Arguments: map[string]any{
						"target_file": "test.txt",
					},
				},
			},
			wantAllowed: true,
			wantMessage: "Allowed to call read_file",
		},
		{
			name: "denied delete_file request",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
				},
				Params: mcp.CallToolParams{
					Name: "delete_file",
					Arguments: map[string]any{
						"target_file": "test.txt",
					},
				},
			},
			wantAllowed: false,
			wantMessage: "delete_file is not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, errList := chain.Handle(context.Background(), tt.req)
			require.Empty(t, errList)
			assert.Equal(t, tt.wantAllowed, results.Allowed)
			assert.Equal(t, tt.wantMessage, results.Message)
			assert.NotEmpty(t, results.Results)

			// Verify audit logging result
			var auditResult *ValidationResult
			for i := range results.Results {
				if results.Results[i].PolicyType == "audit" {
					auditResult = &results.Results[i]
					break
				}
			}
			if assert.NotNil(t, auditResult) {
				assert.Equal(t, "Audit Logging", auditResult.PolicyName)
				assert.Equal(t, "audit", auditResult.PolicyType)
				assert.True(t, auditResult.Allowed)
			}

			// Verify CEL validation result
			var celResult *ValidationResult
			for i := range results.Results {
				if results.Results[i].PolicyType == "cel" {
					celResult = &results.Results[i]
					break
				}
			}
			if assert.NotNil(t, celResult) {
				assert.Equal(t, tt.wantAllowed, celResult.Allowed)
				assert.Equal(t, tt.wantMessage, celResult.Message)
			}
		})
	}
}

func TestLoggingHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewToolLoggingHandler(logger)

	req := mcp.CallToolRequest{
		Request: mcp.Request{
			Method: "tools/call",
		},
		Params: mcp.CallToolParams{
			Name: "read_file",
			Arguments: map[string]any{
				"target_file": "test.txt",
			},
		},
	}

	results, err := handler.HandleToolCall(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, results.Allowed)
	assert.Equal(t, 1, results.AllowCount)
	assert.Equal(t, 0, results.DenyCount)
	assert.Len(t, results.Results, 1)

	auditResult := results.Results[0]
	assert.Equal(t, "Audit Logging", auditResult.PolicyName)
	assert.Equal(t, "audit", auditResult.PolicyType)
	assert.True(t, auditResult.Allowed)
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
			result, err := handler.HandleToolCall(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.True(t, result.Allowed)
			if assert.NotEmpty(t, result.Results) {
				assert.Equal(t, "allow-tools-call", result.Results[0].PolicyName)
				assert.Equal(t, "cel", result.Results[0].PolicyType)
			}
		})
	}
}
