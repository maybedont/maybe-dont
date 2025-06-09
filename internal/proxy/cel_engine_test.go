package proxy

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap/zaptest"
)

func TestCELPolicyEngine_Evaluate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	engine, err := NewCELPolicyEngine(logger)
	require.NoError(t, err)

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

	tests := []struct {
		name        string
		req         mcp.CallToolRequest
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "allow read_file",
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
			name: "deny delete_file",
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
		{
			name: "deny unknown tool",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
				},
				Params: mcp.CallToolParams{
					Name: "unknown_tool",
				},
			},
			wantAllowed: false,
			wantMessage: "Denied by default policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := engine.EvaluateToolCall(tt.req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAllowed, results.Allowed)
			assert.Equal(t, tt.wantMessage, results.Message)
			assert.NotEmpty(t, results.Results)

			// Verify policy evaluation results
			for _, policyResult := range results.Results {
				assert.Equal(t, "cel", policyResult.PolicyType)
				if policyResult.PolicyName == "allow-read-tool" {
					assert.Equal(t, tt.req.Params.Name == "read_file", policyResult.Allowed)
				} else if policyResult.PolicyName == "deny-delete-tool" {
					assert.Equal(t, tt.req.Params.Name == "delete_file", policyResult.Allowed)
				}
			}

			// Verify allow/deny counts
			if tt.wantAllowed {
				assert.Equal(t, 1, results.AllowCount)
				assert.Equal(t, 0, results.DenyCount)
			} else {
				assert.Equal(t, 0, results.AllowCount)
				assert.Equal(t, 1, results.DenyCount)
			}
		})
	}
}
