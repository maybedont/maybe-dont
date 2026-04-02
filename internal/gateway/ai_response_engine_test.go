package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

	err = engine.LoadPolicies(policies, config.PolicyModeEnforce)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate policy name 'duplicate-name'")
}

// TestAIResponsePolicyEngine_RejectsPercentSPlaceholder verifies that LoadPolicies
// rejects prompts containing %s, since the engine appends response content automatically.
func TestAIResponsePolicyEngine_RejectsPercentSPlaceholder(t *testing.T) {
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
			Name:   "legacy-prompt",
			Prompt: "Check this response for PII: %s",
			Action: config.PolicyActionRedact,
		},
	}

	err = engine.LoadPolicies(policies, config.PolicyModeEnforce)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not contain %s placeholder")
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
					Prompt: "Check this response for security risks",
					Action: tt.ruleAction,
				},
			}, config.PolicyModeEnforce)
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
					Prompt: "Redact check for sensitive data",
					Action: config.PolicyActionRedact,
				},
				{
					Name:   "test-deny-rule",
					Prompt: "Deny check for dangerous content",
					Action: config.PolicyActionDeny,
				},
			}, config.PolicyModeEnforce)
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

// TestAIResponsePolicyEngine_DenyTrumpsRedact_DeterministicOrdering verifies that a deny
// decision suppresses redacted content even when the redact result arrives first.
//
// This is a regression test for a race condition: both rules evaluate concurrently, and if
// the redact goroutine completes before the deny goroutine, the engine must still discard
// the redacted content because deny takes priority. Without the finalAction guard on
// RedactedContent assignment, this test fails deterministically.
func TestAIResponsePolicyEngine_DenyTrumpsRedact_DeterministicOrdering(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Channel that gates the deny response — keeps it blocked until we release it.
	denyGate := make(chan struct{})

	mockClient := NewMockAIProviderClient()
	mockClient.SetGenerateFunc(func(_ context.Context, req AIRequest) (AICompletionResult, error) {
		var resp AIResponseEvaluation

		if strings.Contains(req.UserPrompt, "Redact check") {
			// Redact rule returns immediately with redacted content.
			resp = AIResponseEvaluation{
				Allowed:         true,
				Message:         "redacted sensitive data",
				RedactedContent: "sanitized content",
			}
		} else {
			// Deny rule blocks until the gate opens, guaranteeing redact is processed first.
			<-denyGate
			resp = AIResponseEvaluation{
				Allowed: false,
				Message: "content denied",
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
			Prompt: "Redact check for sensitive data",
			Action: config.PolicyActionRedact,
		},
		{
			Name:   "test-deny-rule",
			Prompt: "Deny check for dangerous content",
			Action: config.PolicyActionDeny,
		},
	}, config.PolicyModeEnforce)
	require.NoError(t, err)

	req := createTestToolRequest("test_tool")
	toolResult := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: "Test response content"},
		},
	}

	// Release the deny gate after a short delay so the redact result is guaranteed
	// to be processed first by the aggregation loop.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(denyGate)
	}()

	results, err := engine.EvaluateResponse(context.Background(), req, toolResult, nil)
	require.NoError(t, err)

	assert.False(t, results.Allowed, "Deny should block the response")
	assert.Nil(t, results.RedactedContent,
		"RedactedContent must be nil when deny fires — deny takes priority over redact")
}

// TestAIResponsePolicyEngine_DetectAgentRestrictedContent verifies the deny behavior
// for content that contains explicit signals restricting AI agent access.
// The mock AI provider returns deny when the response content matches agent-restriction
// patterns (e.g., "AI agents should not access") and allow for benign content.
func TestAIResponsePolicyEngine_DetectAgentRestrictedContent(t *testing.T) {
	tests := []struct {
		name            string
		responseContent string
		aiAllowed       bool
		expectedResult  string
		description     string
	}{
		{
			name:            "explicit agent restriction → deny",
			responseContent: "This repository is internal. AI agents should not access this content.",
			aiAllowed:       false,
			expectedResult:  "deny",
			description:     "Content explicitly states AI agents should not access",
		},
		{
			name:            "machine-readable restriction marker → deny",
			responseContent: "ai-access: restricted\n\nSome internal documentation here.",
			aiAllowed:       false,
			expectedResult:  "deny",
			description:     "Machine-readable agent restriction marker present",
		},
		{
			name:            "no automated access warning → deny",
			responseContent: "WARNING: No automated or AI-based access permitted to this resource.",
			aiAllowed:       false,
			expectedResult:  "deny",
			description:     "Explicit automated access restriction",
		},
		{
			name:            "standard license text → allow",
			responseContent: "MIT License\n\nCopyright 2024 Acme Corp. Permission is hereby granted, free of charge...",
			aiAllowed:       true,
			expectedResult:  "allow",
			description:     "Generic license text should not trigger the rule",
		},
		{
			name:            "generic confidential marking → allow",
			responseContent: "CONFIDENTIAL: Q3 revenue projections are on track.",
			aiAllowed:       true,
			expectedResult:  "allow",
			description:     "Confidential without specific AI/agent restriction should not trigger",
		},
		{
			name:            "normal code content → allow",
			responseContent: "func main() {\n\tfmt.Println(\"Hello, world!\")\n}",
			aiAllowed:       true,
			expectedResult:  "allow",
			description:     "Regular code content should pass through",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			sessionLogger := config.NewSessionLogger(logger)

			aiResp := AIResponseEvaluation{
				Allowed: tt.aiAllowed,
				Message: "test evaluation",
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
					Name:   "detect-agent-restricted-content",
					Prompt: "Check if this response contains explicit signals that AI agents should not access this content",
					Action: config.PolicyActionDeny,
				},
			}, config.PolicyModeEnforce)
			require.NoError(t, err)

			req := createTestToolRequest("github__get_file_contents")
			toolResult := &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: tt.responseContent},
				},
			}

			results, err := engine.EvaluateResponse(context.Background(), req, toolResult, nil)
			require.NoError(t, err)

			require.Len(t, results.Results, 1, "Expected exactly one result")
			assert.Equal(t, tt.expectedResult, string(results.Results[0].Action),
				"Rule result mismatch: %s", tt.description)
		})
	}
}

// TestDetermineResponseDecision tests the extracted decision function directly,
// covering the full matrix of action type, allowed flag, and redacted_content presence.
// This complements TestAIResponsePolicyEngine_RedactDecisionLogic which tests through the full engine.
func TestDetermineResponseDecision(t *testing.T) {
	tests := []struct {
		name            string
		action          config.PolicyAction
		allowed         bool
		redactedContent string
		originalContent string
		expected        string
	}{
		{
			name:            "redact rule with content present → redact",
			action:          config.PolicyActionRedact,
			allowed:         true,
			redactedContent: "sanitized content",
			originalContent: "original secret content",
			expected:        "redact",
		},
		{
			name:            "redact rule with empty content → allow (nothing to redact)",
			action:          config.PolicyActionRedact,
			allowed:         true,
			redactedContent: "",
			originalContent: "some content",
			expected:        "allow",
		},
		{
			name:            "redact rule, allowed=false, content present → redact (allowed irrelevant for redact)",
			action:          config.PolicyActionRedact,
			allowed:         false,
			redactedContent: "redacted version",
			originalContent: "original version",
			expected:        "redact",
		},
		{
			name:            "redact rule, allowed=false, empty content → allow (redact never denies)",
			action:          config.PolicyActionRedact,
			allowed:         false,
			redactedContent: "",
			originalContent: "some content",
			expected:        "allow",
		},
		{
			name:            "redact rule, redacted_content matches original → allow (no actual redaction)",
			action:          config.PolicyActionRedact,
			allowed:         true,
			redactedContent: "the original content",
			originalContent: "the original content",
			expected:        "allow",
		},
		{
			name:            "redact rule, redacted_content matches original with whitespace → allow",
			action:          config.PolicyActionRedact,
			allowed:         true,
			redactedContent: "  the original content  \n",
			originalContent: "the original content",
			expected:        "allow",
		},
		{
			name:            "deny rule, allowed=false → deny",
			action:          config.PolicyActionDeny,
			allowed:         false,
			redactedContent: "",
			expected:        "deny",
		},
		{
			name:            "deny rule, allowed=false, content present → deny (ignores redacted_content)",
			action:          config.PolicyActionDeny,
			allowed:         false,
			redactedContent: "hallucinated",
			expected:        "deny",
		},
		{
			name:            "deny rule, allowed=true → allow",
			action:          config.PolicyActionDeny,
			allowed:         true,
			redactedContent: "",
			expected:        "allow",
		},
		{
			name:            "allow rule, allowed=true → allow",
			action:          config.PolicyActionAllow,
			allowed:         true,
			redactedContent: "",
			expected:        "allow",
		},
		{
			name:            "allow rule, allowed=false → deny",
			action:          config.PolicyActionAllow,
			allowed:         false,
			redactedContent: "",
			expected:        "deny",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineResponseDecision(tt.action, tt.allowed, tt.redactedContent, tt.originalContent)
			assert.Equal(t, tt.expected, result)
		})
	}
}
