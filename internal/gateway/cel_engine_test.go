package gateway

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestCELPolicyEngine_Evaluate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	engine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)

	policies := []config.CELPolicy{
		{
			Name:       "allow-read-tool",
			Expression: `request.method == "tools/call" && request.params.name == "read_file"`,
			Action:     config.PolicyActionAllow,
			Message:    "Allowed to call read_file",
		},
		{
			Name:       "deny-delete-tool",
			Expression: `request.method == "tools/call" && request.params.name == "delete_file"`,
			Action:     config.PolicyActionDeny,
			Message:    "delete_file is not allowed",
		},
	}

	err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
	require.NoError(t, err)

	tests := []struct {
		name        string
		req         mcp.CallToolRequest
		wantAllowed bool
		wantMessage string
		denyCount   int
		allowCount  int
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
			denyCount:   0,
			allowCount:  1,
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
			denyCount:   1,
			allowCount:  0,
		},
		{
			name: "no policies matched",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
				},
			},
			wantAllowed: true,
			wantMessage: "No policies matched",
			denyCount:   0,
			allowCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := engine.EvaluateToolCall(context.Background(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAllowed, results.Allowed)
			assert.Equal(t, tt.wantMessage, results.Message)
			assert.NotEmpty(t, results.Results)

			if len(results.Results) > 0 {
				assert.Equal(t, tt.wantMessage, results.Results[0].Message)
			}

			assert.Equal(t, tt.allowCount, results.AllowCount)
			assert.Equal(t, tt.denyCount, results.DenyCount)

		})
	}
}
