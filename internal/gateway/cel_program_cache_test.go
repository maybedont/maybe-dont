package gateway

import (
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestCELEngines_EvaluateWithoutRecompiling verifies that rule programs are built
// once in LoadPolicies and reused: after loading, the engines' CEL environment is
// removed, so any per-request Compile or Program call would panic. Each evaluation
// must still return the right decision.
func TestCELEngines_EvaluateWithoutRecompiling(t *testing.T) {
	sessionLogger := config.NewSessionLogger(zaptest.NewLogger(t))

	requestEngine, err := NewCELPolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)
	require.NoError(t, requestEngine.LoadPolicies([]config.Policy{{
		Name:          "deny-delete-repo",
		MCPExpression: `tool.name == "github__delete_repo"`,
		CLIExpression: `cli.command == "gh" && cli.arguments[0] == "repo" && cli.arguments[1] == "delete"`,
		Action:        config.PolicyActionDeny,
	}}, config.PolicyModeEnforce))

	responseEngine, err := NewCELResponsePolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)
	require.NoError(t, responseEngine.LoadPolicies([]config.ResponsePolicy{{
		Name:       "deny-errors",
		Expression: `response.isError == true`,
		Action:     config.PolicyActionDeny,
	}}, config.PolicyModeEnforce))

	requestEngine.env = nil
	responseEngine.env = nil

	tests := []struct {
		name        string
		evaluate    func() (bool, error)
		wantAllowed bool
	}{
		{
			name: "MCP deny",
			evaluate: func() (bool, error) {
				r, err := requestEngine.EvaluateToolCall(t.Context(), createTestToolRequest("github__delete_repo"), nil)
				return r.Allowed, err
			},
			wantAllowed: false,
		},
		{
			name: "MCP allow",
			evaluate: func() (bool, error) {
				r, err := requestEngine.EvaluateToolCall(t.Context(), createTestToolRequest("github__get_file"), nil)
				return r.Allowed, err
			},
			wantAllowed: true,
		},
		{
			name: "CLI deny",
			evaluate: func() (bool, error) {
				r, err := requestEngine.EvaluateCLICommand(t.Context(), &CLIValidationRequest{Command: "gh", Arguments: []string{"repo", "delete", "x"}}, nil)
				return r.Allowed, err
			},
			wantAllowed: false,
		},
		{
			name: "response deny",
			evaluate: func() (bool, error) {
				r, err := responseEngine.EvaluateResponse(t.Context(), createTestToolRequest("github__get_file"),
					&mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("boom")}, IsError: true}, nil)
				return r.Allowed, err
			},
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 3 {
				allowed, err := tt.evaluate()
				require.NoError(t, err)
				assert.Equal(t, tt.wantAllowed, allowed)
			}
		})
	}
}

// TestCELEngines_ConcurrentEvaluation shares one loaded engine of each kind across
// many goroutines so `go test -race` can catch shared state in the cached programs.
// Every goroutine must get the same decision a sequential evaluation would.
func TestCELEngines_ConcurrentEvaluation(t *testing.T) {
	sessionLogger := config.NewSessionLogger(zaptest.NewLogger(t))

	requestEngine, err := NewCELPolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)
	require.NoError(t, requestEngine.LoadPolicies([]config.Policy{
		{
			Name:       "broken",
			Expression: `request.params.arguments.missing_key == "x"`,
			Action:     config.PolicyActionDeny,
		},
		{
			Name:          "deny-delete-repo",
			MCPExpression: `get(tool.arguments, "repo", "") != "" && tool.name == "github__delete_repo"`,
			CLIExpression: `cli.command == "gh" && cli.arguments[0] == "repo" && cli.arguments[1] == "delete"`,
			Action:        config.PolicyActionDeny,
		},
	}, config.PolicyModeEnforce))

	responseEngine, err := NewCELResponsePolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)
	require.NoError(t, responseEngine.LoadPolicies([]config.ResponsePolicy{{
		Name:             "redact-secrets",
		Expression:       `response.content[0].text.contains("secret")`,
		Action:           config.PolicyActionRedact,
		RedactionPattern: "secret[0-9]+",
	}}, config.PolicyModeEnforce))

	toolRequest := func(name string) mcp.CallToolRequest {
		return mcp.CallToolRequest{
			Request: mcp.Request{Method: "tools/call"},
			Params:  mcp.CallToolParams{Name: name, Arguments: map[string]any{"repo": "maybedont/x"}},
		}
	}

	const goroutines = 32
	const iterations = 50
	var wg sync.WaitGroup
	errs := make(chan string, goroutines*iterations*4)

	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				// Alternate tools so goroutines evaluate the same program with different inputs.
				tool, wantAllowed := "github__get_file", true
				if g%2 == 0 {
					tool, wantAllowed = "github__delete_repo", false
				}
				r, err := requestEngine.EvaluateToolCall(t.Context(), toolRequest(tool), NewBlockingBudget(90000))
				if err != nil || r.Allowed != wantAllowed || r.FailedOpen != wantAllowed {
					errs <- "MCP " + tool
				}

				cli, err := requestEngine.EvaluateCLICommand(t.Context(), &CLIValidationRequest{Command: "gh", Arguments: []string{"repo", "delete", "x"}}, nil)
				if err != nil || cli.Allowed {
					errs <- "CLI"
				}

				resp, err := responseEngine.EvaluateResponse(t.Context(), toolRequest("github__get_file"),
					&mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("key secret42")}}, nil)
				if err != nil || resp.RedactedContent == nil || *resp.RedactedContent != "key [REDACTED]" {
					errs <- "response"
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	var failures []string
	for e := range errs {
		failures = append(failures, e)
	}
	assert.Empty(t, failures, "concurrent evaluations returned unexpected decisions")
}
