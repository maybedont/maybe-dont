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
		policies       []config.CELResponsePolicy
		request        mcp.CallToolRequest
		response       *mcp.CallToolResult
		expectAllowed  bool
		expectRedacted bool
	}{
		{
			name: "allow response - no policies match",
			policies: []config.CELResponsePolicy{
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
			policies: []config.CELResponsePolicy{
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
			policies: []config.CELResponsePolicy{
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

			err = engine.LoadPolicies(tt.policies)
			require.NoError(t, err)

			results, err := engine.EvaluateResponse(context.Background(), tt.request, tt.response)
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
		name              string
		content           string
		pattern           string
		replacement       string
		expectedContains  string
		expectedNotContains string
	}{
		{
			name:              "redact entire content",
			content:           "Sensitive data here",
			pattern:           ".*",
			replacement:       "[REDACTED]",
			expectedContains:  "[REDACTED]",
			expectedNotContains: "Sensitive",
		},
		{
			name:              "redact email addresses",
			content:           "Contact us at test@example.com",
			pattern:           "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}",
			replacement:       "[EMAIL]",
			expectedContains:  "Contact us at [EMAIL]",
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
