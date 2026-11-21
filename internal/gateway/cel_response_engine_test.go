package gateway

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCELResponsePolicyEngine_EvaluateResponse(t *testing.T) {
	logger := zap.NewNop()
	sessionLogger := config.NewSessionLogger(logger)

	tests := []struct {
		name           string
		policies       []config.ResponsePolicy
		request        mcp.CallToolRequest
		response       *mcp.CallToolResult
		expectAllowed  bool
		expectRedacted bool
	}{
		{
			name: "allow response - no policies match",
			policies: []config.ResponsePolicy{
				{
					Name:       "test-policy",
					Expression: `response.isError == true`,
					Action:     config.PolicyActionDeny,
					Message:    "Error responses not allowed",
				},
			},
			request: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{},
				},
				Params: mcp.CallToolParams{
					Name: "test-tool",
				},
			},
			response: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "Success response",
					},
				},
				IsError: false,
			},
			expectAllowed:  true,
			expectRedacted: false,
		},
		{
			name: "deny response - policy matches",
			policies: []config.ResponsePolicy{
				{
					Name:       "block-errors",
					Expression: `response.isError == true`,
					Action:     config.PolicyActionDeny,
					Message:    "Error responses not allowed",
				},
			},
			request: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{},
				},
				Params: mcp.CallToolParams{
					Name: "test-tool",
				},
			},
			response: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "Error occurred",
					},
				},
				IsError: true,
			},
			expectAllowed:  false,
			expectRedacted: false,
		},
		{
			name: "redact response content",
			policies: []config.ResponsePolicy{
				{
					Name:                 "redact-sensitive",
					Expression:           `size(response.content) > 0 && response.content[0].text.contains("password")`,
					Action:               "redact",
					Message:              "Sensitive content redacted",
					RedactionPattern:     "password.*",
					RedactionReplacement: "[REDACTED]",
				},
			},
			request: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{},
				},
				Params: mcp.CallToolParams{
					Name: "test-tool",
				},
			},
			response: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "The password is secret123",
					},
				},
				IsError: false,
			},
			expectAllowed:  true,
			expectRedacted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewCELResponsePolicyEngine(context.Background(), sessionLogger)
			require.NoError(t, err)

			err = engine.LoadPolicies(tt.policies, config.PolicyModeEnforce)
			require.NoError(t, err)

			results, err := engine.EvaluateResponse(context.Background(), tt.request, tt.response, nil)
			require.NoError(t, err)

			assert.Equal(t, tt.expectAllowed, results.Allowed, "Allowed status mismatch")
			if tt.expectRedacted {
				assert.NotNil(t, results.RedactedContent, "Expected redacted content")
				assert.Greater(t, results.RedactCount(), 0, "Expected redaction count > 0")
			} else {
				assert.Equal(t, 0, results.RedactCount(), "Expected no redactions")
			}
		})
	}
}

func TestCELResponsePolicyEngine_Redaction(t *testing.T) {
	logger := zap.NewNop()
	sessionLogger := config.NewSessionLogger(logger)

	tests := []struct {
		name                string
		content             string
		pattern             string
		replacement         string
		expectedContains    string
		expectedNotContains string
	}{
		{
			name:                "redact entire content",
			content:             "Sensitive data here",
			pattern:             ".*",
			replacement:         "[REDACTED]",
			expectedContains:    "[REDACTED]",
			expectedNotContains: "Sensitive",
		},
		{
			name:                "redact email addresses",
			content:             "Contact us at test@example.com",
			pattern:             "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}",
			replacement:         "[EMAIL]",
			expectedContains:    "Contact us at [EMAIL]",
			expectedNotContains: "test@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewCELResponsePolicyEngine(context.Background(), sessionLogger)
			require.NoError(t, err)

			redacted := engine.applyRedaction(tt.content, tt.pattern, tt.replacement)

			assert.Contains(t, redacted, tt.expectedContains)
			if tt.expectedNotContains != "" {
				assert.NotContains(t, redacted, tt.expectedNotContains)
			}
		})
	}
}

func TestCELResponsePolicyEngine_DuplicatePolicyNames(t *testing.T) {
	// Tests that LoadPolicies rejects policies with duplicate names
	logger := zap.NewNop()
	sessionLogger := config.NewSessionLogger(logger)
	engine, err := NewCELResponsePolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)

	policies := []config.ResponsePolicy{
		{
			Name:       "duplicate-name",
			Expression: `true`,
			Action:     config.PolicyActionDeny,
			Message:    "First policy",
		},
		{
			Name:       "duplicate-name",
			Expression: `false`,
			Action:     config.PolicyActionAllow,
			Message:    "Second policy with same name",
		},
	}

	err = engine.LoadPolicies(policies, config.PolicyModeEnforce)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate policy name 'duplicate-name'")
}

// TestCELResponsePolicyEngine_ToolVariable verifies that response rules written
// against the documented "tool" variable (tool.name, tool.arguments, and the
// has()/get() helpers) compile and evaluate to the expected decision, including
// when the call has no arguments, and that request.params rules are unchanged.
func TestCELResponsePolicyEngine_ToolVariable(t *testing.T) {
	sessionLogger := config.NewSessionLogger(zap.NewNop())

	readArgs := map[string]any{"path": "secrets/.env", "ref": "main"}

	tests := []struct {
		name        string
		expression  string
		toolName    string
		arguments   any
		wantAllowed bool
	}{
		{
			name:        "tool.name equality denies matching tool",
			expression:  `tool.name == "github__get_file_contents"`,
			toolName:    "github__get_file_contents",
			arguments:   readArgs,
			wantAllowed: false,
		},
		{
			name:        "tool.name equality ignores other tool",
			expression:  `tool.name == "github__get_file_contents"`,
			toolName:    "github__list_issues",
			arguments:   readArgs,
			wantAllowed: true,
		},
		{
			name:        "tool.name startsWith denies matching prefix",
			expression:  `tool.name.startsWith("github__")`,
			toolName:    "github__list_issues",
			wantAllowed: false,
		},
		{
			name:        "tool.name startsWith ignores other client",
			expression:  `tool.name.startsWith("github__")`,
			toolName:    "aws-docs__read_documentation",
			wantAllowed: true,
		},
		{
			name:        "tool.name in list denies listed tool",
			expression:  `tool.name in ["github__get_file_contents", "github__search_code"]`,
			toolName:    "github__search_code",
			wantAllowed: false,
		},
		{
			name:        "tool.name in list ignores unlisted tool",
			expression:  `tool.name in ["github__get_file_contents", "github__search_code"]`,
			toolName:    "github__list_issues",
			wantAllowed: true,
		},
		{
			name:        "get on tool.arguments denies matching value",
			expression:  `get(tool.arguments, "path", "").startsWith("secrets/")`,
			toolName:    "github__get_file_contents",
			arguments:   readArgs,
			wantAllowed: false,
		},
		{
			name:        "get on tool.arguments returns default for missing key",
			expression:  `get(tool.arguments, "owner", "") != ""`,
			toolName:    "github__get_file_contents",
			arguments:   readArgs,
			wantAllowed: true,
		},
		{
			name:        "has on tool.arguments denies present key",
			expression:  `has(tool.arguments, "ref")`,
			toolName:    "github__get_file_contents",
			arguments:   readArgs,
			wantAllowed: false,
		},
		{
			name:        "has on tool.arguments ignores absent key",
			expression:  `has(tool.arguments, "owner")`,
			toolName:    "github__get_file_contents",
			arguments:   readArgs,
			wantAllowed: true,
		},
		{
			name:        "nil arguments: has evaluates to false without error",
			expression:  `has(tool.arguments, "ref")`,
			toolName:    "github__get_file_contents",
			arguments:   nil,
			wantAllowed: true,
		},
		{
			name:        "nil arguments: get returns default without error",
			expression:  `get(tool.arguments, "path", "none") == "none"`,
			toolName:    "github__get_file_contents",
			arguments:   nil,
			wantAllowed: false,
		},
		{
			name:        "tool combined with response content",
			expression:  `tool.name == "github__get_file_contents" && response.content[0].text.contains("SECRET")`,
			toolName:    "github__get_file_contents",
			arguments:   readArgs,
			wantAllowed: false,
		},
		{
			name:        "existing request.params rule still denies",
			expression:  `request.params.name == "github__get_file_contents" && request.params.arguments.path == "secrets/.env"`,
			toolName:    "github__get_file_contents",
			arguments:   readArgs,
			wantAllowed: false,
		},
		{
			name:        "existing request.params rule still allows other tool",
			expression:  `request.params.name == "github__get_file_contents"`,
			toolName:    "github__list_issues",
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewCELResponsePolicyEngine(context.Background(), sessionLogger)
			require.NoError(t, err)

			err = engine.LoadPolicies([]config.ResponsePolicy{{
				Name:       "tool-rule",
				Expression: tt.expression,
				Action:     config.PolicyActionDeny,
				Message:    "matched",
			}}, config.PolicyModeEnforce)
			require.NoError(t, err, "rule should compile")

			req := mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params:  mcp.CallToolParams{Name: tt.toolName, Arguments: tt.arguments},
			}
			result := &mcp.CallToolResult{
				Content: []mcp.Content{mcp.TextContent{Type: "text", Text: "API_KEY=SECRET"}},
			}

			results, err := engine.EvaluateResponse(context.Background(), req, result, nil)
			require.NoError(t, err)

			// A runtime error would fail open and look like an allow, so rule it out explicitly.
			assert.False(t, results.FailedOpen, "rule should evaluate without error")
			require.NotNil(t, results.RulesDetails)
			for _, rr := range results.RulesDetails.Results {
				assert.NotEqual(t, "error", rr.Result, "rule %s errored: %s", rr.Rule, rr.Error)
			}
			assert.Equal(t, tt.wantAllowed, results.Allowed)
		})
	}
}

// TestCELResponsePolicyEngine_ContinuesAfterRuleError verifies that an enforced
// response rule that errors at runtime fails open for itself only: later rules
// still run, so a later redact rule still redacts and a later deny rule still
// denies (without the response being reported as fail-open).
func TestCELResponsePolicyEngine_ContinuesAfterRuleError(t *testing.T) {
	sessionLogger := config.NewSessionLogger(zap.NewNop())

	broken := func(expression string) config.ResponsePolicy {
		return config.ResponsePolicy{Name: "broken", Expression: expression, Action: config.PolicyActionDeny}
	}
	redactSecrets := config.ResponsePolicy{
		Name:                 "redact-secrets",
		Expression:           `size(response.content) > 0 && response.content[0].text.contains("secret")`,
		Action:               config.PolicyActionRedact,
		RedactionPattern:     "secret[0-9]+",
		RedactionReplacement: "[REDACTED]",
	}
	denyErrors := config.ResponsePolicy{
		Name:       "deny-errors",
		Expression: `response.isError == true`,
		Action:     config.PolicyActionDeny,
		Message:    "Error responses are not allowed",
	}
	const evalErrorExpr = `response.missing_key == "x"`
	const nonBoolExpr = `response.isError ? "yes" : "no"`

	tests := []struct {
		name             string
		policies         []config.ResponsePolicy
		text             string
		isError          bool
		wantAllowed      bool
		wantFailedOpen   bool
		wantRedacted     string
		wantRecommended  config.PolicyAction
		wantRuleResults  []string
		wantDecidingRule string
	}{
		{
			name:            "eval error then matching redact redacts",
			policies:        []config.ResponsePolicy{broken(evalErrorExpr), redactSecrets},
			text:            "token is secret123",
			wantAllowed:     true,
			wantFailedOpen:  true,
			wantRedacted:    "token is [REDACTED]",
			wantRuleResults: []string{"broken=error", "redact-secrets=redact"},
		},
		{
			name:             "eval error then matching deny denies",
			policies:         []config.ResponsePolicy{broken(evalErrorExpr), denyErrors},
			text:             "boom",
			isError:          true,
			wantAllowed:      false,
			wantFailedOpen:   false,
			wantRecommended:  config.PolicyActionDeny,
			wantRuleResults:  []string{"broken=error", "deny-errors=deny"},
			wantDecidingRule: "deny-errors",
		},
		{
			name:            "non-boolean result then matching redact redacts",
			policies:        []config.ResponsePolicy{broken(nonBoolExpr), redactSecrets},
			text:            "token is secret123",
			wantAllowed:     true,
			wantFailedOpen:  true,
			wantRedacted:    "token is [REDACTED]",
			wantRuleResults: []string{"broken=error", "redact-secrets=redact"},
		},
		{
			name:             "non-boolean result then matching deny denies",
			policies:         []config.ResponsePolicy{broken(nonBoolExpr), denyErrors},
			text:             "boom",
			isError:          true,
			wantAllowed:      false,
			wantFailedOpen:   false,
			wantRecommended:  config.PolicyActionDeny,
			wantRuleResults:  []string{"broken=error", "deny-errors=deny"},
			wantDecidingRule: "deny-errors",
		},
		{
			name:            "eval error then non-matching rules allows with fail-open",
			policies:        []config.ResponsePolicy{broken(evalErrorExpr), redactSecrets, denyErrors},
			text:            "nothing to see",
			wantAllowed:     true,
			wantFailedOpen:  true,
			wantRuleResults: []string{"broken=error", "redact-secrets=allow", "deny-errors=allow"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewCELResponsePolicyEngine(context.Background(), sessionLogger)
			require.NoError(t, err)
			require.NoError(t, engine.LoadPolicies(tt.policies, config.PolicyModeEnforce))

			req := mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params:  mcp.CallToolParams{Name: "github__get_file"},
			}
			result := &mcp.CallToolResult{
				Content: []mcp.Content{mcp.TextContent{Type: "text", Text: tt.text}},
				IsError: tt.isError,
			}
			results, err := engine.EvaluateResponse(context.Background(), req, result, NewBlockingBudget(90000))
			require.NoError(t, err)

			assert.Equal(t, tt.wantAllowed, results.Allowed, "Allowed")
			assert.Equal(t, tt.wantFailedOpen, results.FailedOpen, "FailedOpen")
			assert.Equal(t, tt.wantRecommended, results.RecommendedAction, "RecommendedAction")
			if tt.wantRedacted != "" {
				require.NotNil(t, results.RedactedContent, "RedactedContent")
				assert.Equal(t, tt.wantRedacted, *results.RedactedContent)
			} else {
				assert.Nil(t, results.RedactedContent, "RedactedContent")
			}
			require.NotNil(t, results.RulesDetails)
			assert.Equal(t, tt.wantRuleResults, ruleResultSummary(results.RulesDetails), "rule results")
			assert.Equal(t, tt.wantDecidingRule, results.RulesDetails.DecidingRule, "DecidingRule")
		})
	}
}
