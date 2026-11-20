package testsuite

import (
	"context"
	"testing"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/maybedont/maybe-dont/internal/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteCELTest_ToolVariable verifies that test-policies evaluates rules
// written against the documented "tool" variable the same way the gateway does,
// for both request and response phases, including test cases that omit
// arguments (a typed nil map from YAML).
func TestExecuteCELTest_ToolVariable(t *testing.T) {
	logger := createTestLogger()

	requestEngine, err := gateway.NewCELPolicyEngine(context.Background(), logger)
	require.NoError(t, err)
	require.NoError(t, requestEngine.LoadPolicies([]config.Policy{
		{
			Name:          "deny-github-deletes",
			MCPExpression: `tool.name in ["github__delete_file", "github__delete_repo"] && !has(tool.arguments, "dry_run")`,
			Action:        config.PolicyActionDeny,
			Message:       "deletes blocked",
		},
		{
			Name:          "deny-secret-paths",
			MCPExpression: `tool.name.startsWith("github__") && get(tool.arguments, "path", "").startsWith("secrets/")`,
			Action:        config.PolicyActionDeny,
			Message:       "secret paths blocked",
		},
	}, config.PolicyModeEnforce))

	responseEngine, err := gateway.NewCELResponsePolicyEngine(context.Background(), logger)
	require.NoError(t, err)
	require.NoError(t, responseEngine.LoadPolicies([]config.ResponsePolicy{
		{
			Name:       "deny-secret-file-contents",
			Expression: `tool.name == "github__get_file_contents" && get(tool.arguments, "path", "") == ".env"`,
			Action:     config.PolicyActionDeny,
			Message:    "secret contents blocked",
		},
	}, config.PolicyModeEnforce))

	executor := &Executor{suite: &Suite{}, celEngine: requestEngine, celResponseEngine: responseEngine, logger: logger}

	tests := []struct {
		name         string
		phase        string
		request      RequestConfig
		wantDecision string
	}{
		{
			name:         "request: listed delete tool without arguments is denied",
			phase:        "request",
			request:      RequestConfig{ToolName: "github__delete_repo"},
			wantDecision: "deny",
		},
		{
			name:         "request: dry_run argument allows delete",
			phase:        "request",
			request:      RequestConfig{ToolName: "github__delete_file", Arguments: map[string]any{"dry_run": true}},
			wantDecision: "allow",
		},
		{
			name:         "request: secret path is denied",
			phase:        "request",
			request:      RequestConfig{ToolName: "github__get_file_contents", Arguments: map[string]any{"path": "secrets/key"}},
			wantDecision: "deny",
		},
		{
			name:         "request: unrelated tool without arguments is allowed",
			phase:        "request",
			request:      RequestConfig{ToolName: "aws-docs__search_documentation"},
			wantDecision: "allow",
		},
		{
			name:         "response: secret file contents are denied",
			phase:        "response",
			request:      RequestConfig{ToolName: "github__get_file_contents", Arguments: map[string]any{"path": ".env"}},
			wantDecision: "deny",
		},
		{
			name:         "response: call without arguments is allowed",
			phase:        "response",
			request:      RequestConfig{ToolName: "github__get_file_contents"},
			wantDecision: "allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := TestCase{
				CaseID:       "tool-variable",
				Phase:        tt.phase,
				Request:      tt.request,
				Response:     &ResponseConfig{Content: []ContentItem{{Type: "text", Text: "API_KEY=abc"}}},
				Expectations: ExpectationsConfig{Decision: tt.wantDecision},
			}

			result := executor.executeCELTest(context.Background(), tc)

			require.Nil(t, result.Error)
			assert.Equal(t, tt.wantDecision, result.Actual.Decision)
			assert.Equal(t, "passed", result.Status, "failures: %v", result.Failures)
		})
	}
}
