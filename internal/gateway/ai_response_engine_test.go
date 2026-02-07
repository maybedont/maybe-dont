package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// createTestResponseConfig creates a minimal config for testing the AIResponsePolicyEngine.
func createTestResponseConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Validation.AI.Model = "gpt-4o-mini"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Provider = "openai"
	return cfg
}

func TestAIResponsePolicyEngine_DuplicatePolicyNames(t *testing.T) {
	// Tests that LoadPolicies rejects policies with duplicate names
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	engine := &AIResponsePolicyEngine{
		cfg:                 createTestResponseConfig(),
		maxRuleEvaluationMs: 30000,
		providerClient:      NewMockAIProviderClient(),
	}
	err := InitAIResponsePolicyEngine(context.Background(), sessionLogger, engine)
	require.NoError(t, err)

	policies := []config.AIResponsePolicy{
		{
			Name:   "duplicate-name",
			Prompt: "First prompt",
			Action: config.PolicyActionDeny,
		},
		{
			Name:   "duplicate-name",
			Prompt: "Second prompt",
			Action: config.PolicyActionAllow,
		},
	}

	err = engine.LoadPolicies(policies, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate policy name 'duplicate-name'")
}

// TestAIResponsePolicyEngine_RedactDecisionLogic verifies the decision logic for
// redact vs deny rules. Redact rules should never produce "deny" — only the
// presence of redacted_content determines whether the result is "redact" or "allow".
// For deny rules, the standard allowed field drives the decision.
func TestAIResponsePolicyEngine_RedactDecisionLogic(t *testing.T) {
	tests := []struct {
		name           string
		ruleAction     config.PolicyAction
		aiAllowed      bool
		redactContent  string
		expectedResult string
		description    string
	}{
		{
			name:           "redact rule + allowed true + content present → redact",
			ruleAction:     config.PolicyActionRedact,
			aiAllowed:      true,
			redactContent:  "sanitized content",
			expectedResult: "redact",
			description:    "Model provided sanitized content, use it",
		},
		{
			name:           "redact rule + allowed true + content empty → allow",
			ruleAction:     config.PolicyActionRedact,
			aiAllowed:      true,
			redactContent:  "",
			expectedResult: "allow",
			description:    "Nothing to redact, pass through original",
		},
		{
			name:           "redact rule + allowed false + content present → redact",
			ruleAction:     config.PolicyActionRedact,
			aiAllowed:      false,
			redactContent:  "redacted version",
			expectedResult: "redact",
			description:    "Regression: allowed is irrelevant for redact rules; content present means redact",
		},
		{
			name:           "redact rule + allowed false + content empty → allow",
			ruleAction:     config.PolicyActionRedact,
			aiAllowed:      false,
			redactContent:  "",
			expectedResult: "allow",
			description:    "Regression: redact rules never deny; no content means allow",
		},
		{
			name:           "deny rule + allowed false → deny",
			ruleAction:     config.PolicyActionDeny,
			aiAllowed:      false,
			redactContent:  "",
			expectedResult: "deny",
			description:    "Standard deny rule behavior",
		},
		{
			name:           "deny rule + allowed false + content present → deny",
			ruleAction:     config.PolicyActionDeny,
			aiAllowed:      false,
			redactContent:  "hallucinated redaction",
			expectedResult: "deny",
			description:    "Deny rules ignore redacted_content; allowed field drives the decision",
		},
		{
			name:           "deny rule + allowed true → allow",
			ruleAction:     config.PolicyActionDeny,
			aiAllowed:      true,
			redactContent:  "",
			expectedResult: "allow",
			description:    "Standard deny rule with safe content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			sessionLogger := config.NewSessionLogger(logger)

			// Build mock response matching the test case
			aiResp := AIResponseEvaluation{
				Allowed:         tt.aiAllowed,
				Message:         "test message",
				RedactedContent: tt.redactContent,
			}
			respJSON, err := json.Marshal(aiResp)
			require.NoError(t, err)

			mockClient := NewMockAIProviderClient()
			mockClient.SetResponse(AICompletionResult{
				RawText:    string(respJSON),
				ParsedJSON: json.RawMessage(respJSON),
			})

			engine := &AIResponsePolicyEngine{
				cfg:                 createTestResponseConfig(),
				maxRuleEvaluationMs: 30000,
				providerClient:      mockClient,
			}
			err = InitAIResponsePolicyEngine(context.Background(), sessionLogger, engine)
			require.NoError(t, err)

			err = engine.LoadPolicies([]config.AIResponsePolicy{
				{
					Name:   "test-policy",
					Prompt: "Check: %s",
					Action: tt.ruleAction,
				},
			}, "")
			require.NoError(t, err)

			req := createTestToolRequest("test_tool")
			toolResult := &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "Test response content"},
				},
			}

			results, err := engine.EvaluateResponse(context.Background(), req, toolResult, nil)
			require.NoError(t, err)

			// Check individual rule result
			require.Len(t, results.Results, 1, "Expected exactly one result")
			assert.Equal(t, tt.expectedResult, string(results.Results[0].Action),
				"Rule result mismatch: %s", tt.description)
		})
	}
}

// TestAIResponsePolicyEngine_MixedActionAggregation verifies that when multiple
// rules with different action types are loaded, the finalAction priority logic
// works correctly: "deny" trumps everything, "redact" upgrades from "allow",
// and the overall Allowed field reflects the correct outcome.
func TestAIResponsePolicyEngine_MixedActionAggregation(t *testing.T) {
	tests := []struct {
		name            string
		redactAllowed   bool
		redactContent   string
		denyAllowed     bool
		expectedAllowed bool
		expectedRedact  bool
		description     string
	}{
		{
			name:            "redact fires + deny allows → redact, allowed",
			redactAllowed:   true,
			redactContent:   "sanitized content",
			denyAllowed:     true,
			expectedAllowed: true,
			expectedRedact:  true,
			description:     "Redact rule provides content, deny rule passes — final is redact with Allowed=true",
		},
		{
			name:            "both allow → allow, no redaction",
			redactAllowed:   true,
			redactContent:   "",
			denyAllowed:     true,
			expectedAllowed: true,
			expectedRedact:  false,
			description:     "Neither rule triggers — everything passes through",
		},
		{
			name:            "deny fires + redact fires → deny trumps redact",
			redactAllowed:   true,
			redactContent:   "sanitized content",
			denyAllowed:     false,
			expectedAllowed: false,
			expectedRedact:  false,
			description:     "Deny takes priority over redact — response is blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			sessionLogger := config.NewSessionLogger(logger)

			// Use GenerateFunc to return different responses based on which rule is calling.
			// The redact rule's prompt contains "Redact check" and the deny rule's contains "Deny check".
			mockClient := NewMockAIProviderClient()
			mockClient.SetGenerateFunc(func(_ context.Context, req AIRequest) (AICompletionResult, error) {
				var resp AIResponseEvaluation
				if strings.Contains(req.UserPrompt, "Redact check") {
					resp = AIResponseEvaluation{
						Allowed:         tt.redactAllowed,
						Message:         "redact rule response",
						RedactedContent: tt.redactContent,
					}
				} else {
					resp = AIResponseEvaluation{
						Allowed: tt.denyAllowed,
						Message: "deny rule response",
					}
				}
				respJSON, _ := json.Marshal(resp)
				return AICompletionResult{
					RawText:    string(respJSON),
					ParsedJSON: json.RawMessage(respJSON),
				}, nil
			})

			engine := &AIResponsePolicyEngine{
				cfg:                 createTestResponseConfig(),
				maxRuleEvaluationMs: 30000,
				providerClient:      mockClient,
			}
			err := InitAIResponsePolicyEngine(context.Background(), sessionLogger, engine)
			require.NoError(t, err)

			err = engine.LoadPolicies([]config.AIResponsePolicy{
				{
					Name:   "test-redact-rule",
					Prompt: "Redact check: %s",
					Action: config.PolicyActionRedact,
				},
				{
					Name:   "test-deny-rule",
					Prompt: "Deny check: %s",
					Action: config.PolicyActionDeny,
				},
			}, "")
			require.NoError(t, err)

			req := createTestToolRequest("test_tool")
			toolResult := &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "Test response content"},
				},
			}

			results, err := engine.EvaluateResponse(context.Background(), req, toolResult, nil)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedAllowed, results.Allowed,
				"Allowed mismatch: %s", tt.description)

			if tt.expectedRedact {
				assert.NotNil(t, results.RedactedContent,
					"Expected redacted content: %s", tt.description)
			} else {
				assert.Nil(t, results.RedactedContent,
					"Expected no redacted content: %s", tt.description)
			}
		})
	}
}
