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
	engine, err := NewCELPolicyEngine(logger)
	require.NoError(t, err)

	policies := []config.CELPolicy{
		{
			Name:       "allow-read-tool",
			Expression: `request.method == "tools/call" && request.params.meta.additionalFields.name == "read_file"`,
			Action:     "allow",
			Message:    "Allowed to call read_file",
		},
		{
			Name:       "deny-delete-tool",
			Expression: `request.method == "tools/call" && request.params.meta.additionalFields.name == "delete_file"`,
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
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{
							AdditionalFields: map[string]any{
								"name": "read_file",
							},
						},
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
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{
							AdditionalFields: map[string]any{
								"name": "delete_file",
							},
						},
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
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{
							AdditionalFields: map[string]any{
								"name": "unknown_tool",
							},
						},
					},
				},
			},
			want: false,
			msg:  "no matching policy found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, msg, err := engine.Evaluate(tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.want, allowed)
			require.Equal(t, tt.msg, msg)
		})
	}
}
