package gateway

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/auth"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestCELAuthVariable verifies that CEL policies can reference the authenticated identity
// via the auth.* variable, and that it is populated from the context identity (or zeroed
// for unauthenticated requests).
func TestCELAuthVariable(t *testing.T) {
	sessionLogger := config.NewSessionLogger(zaptest.NewLogger(t))

	// Policy: deny deleting repos unless the user is in the eng-admins group.
	policies := []config.Policy{
		{
			Name: "require-admin-group",
			MCPExpression: `request.params.name == "github__delete_repo" && ` +
				`!("eng-admins" in get(auth.user.claims, "groups", []))`,
			Action:  config.PolicyActionDeny,
			Message: "repo deletion requires eng-admins membership",
		},
	}

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "github__delete_repo"},
	}

	tests := []struct {
		name       string
		identity   *auth.Identity
		wantDenied bool
	}{
		{
			name:       "unauthenticated is denied (no groups)",
			identity:   nil,
			wantDenied: true,
		},
		{
			name: "non-admin user is denied",
			identity: &auth.Identity{
				Subject: "user-1",
				Claims:  map[string]any{"groups": []any{"eng"}},
			},
			wantDenied: true,
		},
		{
			name: "admin user is allowed",
			identity: &auth.Identity{
				Subject: "user-2",
				Claims:  map[string]any{"groups": []any{"eng", "eng-admins"}},
			},
			wantDenied: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
			require.NoError(t, err)
			require.NoError(t, engine.LoadPolicies(policies, config.PolicyModeEnforce))

			ctx := context.Background()
			if tt.identity != nil {
				ctx = WithIdentity(ctx, tt.identity)
			}

			results, err := engine.EvaluateToolCall(ctx, req, nil)
			require.NoError(t, err)
			if tt.wantDenied {
				require.Positive(t, results.DenyCount, "expected a deny")
			} else {
				require.Zero(t, results.DenyCount, "expected allow")
			}
		})
	}
}
