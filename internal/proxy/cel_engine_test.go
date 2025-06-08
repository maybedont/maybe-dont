package proxy

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap/zaptest"
)

func TestCELPolicyEngine_Evaluate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	engine, err := NewCELPolicyEngine(logger, "deny")
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
		name string
		req  mcp.CallToolRequest
		want bool
		msg  string
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
						"command": "cat file.txt",
					},
				},
			},
			want: true,
			msg:  "",
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
						"command": "rm file.txt",
					},
				},
			},
			want: false,
			msg:  "delete_file is not allowed",
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
			want: false,
			msg:  "no matching policy found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, msg, err := engine.EvaluateToolCall(tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.want, allowed)
			require.Equal(t, tt.msg, msg)
		})
	}
}
