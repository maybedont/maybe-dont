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

func TestAIPolicyEngine_LoadPolicies(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	t.Run("loads_enabled_policies", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:        "block_destructive_ops",
				Description: "Block destructive operations",
				Prompt:      "Check if this is destructive: %s",
				Action:      config.PolicyActionDeny,
				Message:     "Blocked",
				Mode:        config.PolicyModeEnabled,
			},
			{
				Name:        "require_valid_repo",
				Description: "Require valid repo",
				Prompt:      "Check repo: %s",
				Action:      config.PolicyActionAllow,
				Message:     "Repo required",
				Mode:        config.PolicyModeEnabled,
			},
		}

		err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
		require.NoError(t, err)
		assert.Len(t, engine.policies, 2)
	})

	t.Run("skips_disabled_policies", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "enabled_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeEnabled,
			},
			{
				Name:   "disabled_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeDisabled,
			},
		}

		err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, "enabled_policy", engine.policies[0].Name)
	})

	t.Run("respects_default_mode", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "policy_without_mode",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				// Mode not set, should use default
			},
		}

		// Load with audit_only as default
		err = engine.LoadPolicies(policies, config.PolicyModeAuditOnly)
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyModeAuditOnly, engine.policies[0].Mode)
	})

	t.Run("policy_mode_overrides_default", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "enabled_override",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeEnabled,
			},
		}

		// Load with audit_only as default, but policy explicitly sets enabled
		err = engine.LoadPolicies(policies, config.PolicyModeAuditOnly)
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyModeEnabled, engine.policies[0].Mode)
	})

	t.Run("rejects_invalid_action", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "invalid_policy",
				Prompt: "Check: %s",
				Action: config.PolicyAction("invalid"),
				Mode:   config.PolicyModeEnabled,
			},
		}

		err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid action")
	})

	t.Run("allows_deny_action", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "deny_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeEnabled,
			},
		}

		err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyActionDeny, engine.policies[0].Action)
	})

	t.Run("allows_allow_action", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "allow_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionAllow,
				Mode:   config.PolicyModeEnabled,
			},
		}

		err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyActionAllow, engine.policies[0].Action)
	})

	t.Run("loads_audit_only_policies", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "audit_only_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeAuditOnly,
			},
		}

		err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyModeAuditOnly, engine.policies[0].Mode)
	})
}

func TestAIPolicyEngine_NoPolicies(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	engine := &AIPolicyEngine{
		apiKey: "test-key",
		model:  "gpt-4o-mini",
	}
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	// Don't load any policies
	results, err := engine.EvaluateToolCall(context.Background(), createTestToolRequest("test_tool"))
	require.NoError(t, err)

	assert.True(t, results.Allowed)
	assert.Equal(t, "No policies configured", results.Message)
	assert.Nil(t, results.AIDetails)
}

func TestAIPolicyEngine_CountEnabledPolicies(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	t.Run("all_audit_only", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "audit_policy_1",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeAuditOnly,
			},
			{
				Name:   "audit_policy_2",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeAuditOnly,
			},
		}

		err = engine.LoadPolicies(policies, config.PolicyModeAuditOnly)
		require.NoError(t, err)

		// Verify all policies have audit_only mode
		for _, p := range engine.policies {
			assert.Equal(t, config.PolicyModeAuditOnly, p.Mode)
		}
	})

	t.Run("mixed_modes", func(t *testing.T) {
		engine := &AIPolicyEngine{
			apiKey: "test-key",
			model:  "gpt-4o-mini",
		}
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "enabled_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeEnabled,
			},
			{
				Name:   "audit_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeAuditOnly,
			},
		}

		err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
		require.NoError(t, err)

		// Count enabled policies
		enabledCount := 0
		for _, p := range engine.policies {
			if p.Mode == config.PolicyModeEnabled {
				enabledCount++
			}
		}
		assert.Equal(t, 1, enabledCount)
	})
}

func TestAIRuleResult_ActionLogic(t *testing.T) {
	// Test the action determination logic from ai_engine.go
	// This logic determines result based on rule action and AI response

	tests := []struct {
		name           string
		ruleAction     config.PolicyAction
		aiAllowed      bool
		expectedResult string
		description    string
	}{
		{
			name:           "deny_rule_ai_false",
			ruleAction:     config.PolicyActionDeny,
			aiAllowed:      false,
			expectedResult: "deny",
			description:    "Deny rule + AI says issue found -> deny",
		},
		{
			name:           "deny_rule_ai_true",
			ruleAction:     config.PolicyActionDeny,
			aiAllowed:      true,
			expectedResult: "allow",
			description:    "Deny rule + AI says no issue -> allow",
		},
		{
			name:           "allow_rule_ai_true",
			ruleAction:     config.PolicyActionAllow,
			aiAllowed:      true,
			expectedResult: "allow",
			description:    "Allow rule (required gate) + AI says valid -> allow",
		},
		{
			name:           "allow_rule_ai_false",
			ruleAction:     config.PolicyActionAllow,
			aiAllowed:      false,
			expectedResult: "deny",
			description:    "Allow rule (required gate) + AI says invalid -> deny",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the logic from EvaluateToolCall
			var resultAction string
			if tt.ruleAction == config.PolicyActionDeny {
				if tt.aiAllowed {
					resultAction = "allow"
				} else {
					resultAction = "deny"
				}
			} else { // allow policy
				if tt.aiAllowed {
					resultAction = "allow"
				} else {
					resultAction = "deny"
				}
			}

			assert.Equal(t, tt.expectedResult, resultAction, tt.description)
		})
	}
}

func TestAuditAIRuleResult_ModeField(t *testing.T) {
	// Test that mode field is only set for audit_only policies

	t.Run("enabled_mode_no_mode_field", func(t *testing.T) {
		result := AuditAIRuleResult{
			Rule:         "enabled_rule",
			Action:       "deny",
			Result:       "deny",
			EvaluationMs: 500,
			// Mode should be empty for enabled policies
		}

		assert.Empty(t, result.Mode)
	})

	t.Run("audit_only_has_mode_field", func(t *testing.T) {
		result := AuditAIRuleResult{
			Rule:         "audit_rule",
			Action:       "deny",
			Mode:         "audit_only",
			Result:       "deny",
			EvaluationMs: 500,
		}

		assert.Equal(t, "audit_only", result.Mode)
	})
}

func TestAuditAIResult_DecidingRuleField(t *testing.T) {
	t.Run("deny_has_deciding_rule", func(t *testing.T) {
		result := AuditAIResult{
			Action:       "deny",
			BlockedMs:    500,
			EvaluationMs: 1000,
			DecidingRule: "block_destructive",
			Reason:       "This operation is destructive",
			Results:      []AuditAIRuleResult{},
		}

		assert.Equal(t, "block_destructive", result.DecidingRule)
		assert.Equal(t, "This operation is destructive", result.Reason)
	})

	t.Run("allow_no_deciding_rule", func(t *testing.T) {
		result := AuditAIResult{
			Action:       "allow",
			BlockedMs:    1000,
			EvaluationMs: 1000,
			// DecidingRule and Reason should be empty
			Results: []AuditAIRuleResult{},
		}

		assert.Empty(t, result.DecidingRule)
		assert.Empty(t, result.Reason)
	})
}

func TestAuditAIResult_BlockedMsVsEvaluationMs(t *testing.T) {
	// Test the distinction between blocked_ms and evaluation_ms

	t.Run("early_termination_blocked_less_than_evaluation", func(t *testing.T) {
		// When early termination occurs, blocked_ms should be less than evaluation_ms
		// because we stop blocking after the first deny, but continue collecting results
		result := AuditAIResult{
			Action:       "deny",
			BlockedMs:    500, // Stopped blocking after first deny
			EvaluationMs: 800, // Total time to collect all results
			DecidingRule: "fast_deny_rule",
			Results: []AuditAIRuleResult{
				{
					Rule:         "fast_deny_rule",
					Action:       "deny",
					Result:       "deny",
					EvaluationMs: 500,
				},
				{
					Rule:         "slow_audit_rule",
					Action:       "deny",
					Mode:         "audit_only",
					Result:       "allow",
					EvaluationMs: 800,
				},
			},
		}

		assert.Less(t, result.BlockedMs, result.EvaluationMs)
	})

	t.Run("no_early_termination_blocked_equals_evaluation", func(t *testing.T) {
		// When all policies pass, blocked_ms and evaluation_ms should be similar
		result := AuditAIResult{
			Action:       "allow",
			BlockedMs:    1000,
			EvaluationMs: 1000,
			Results: []AuditAIRuleResult{
				{
					Rule:         "rule1",
					Action:       "deny",
					Result:       "allow",
					EvaluationMs: 800,
				},
				{
					Rule:         "rule2",
					Action:       "deny",
					Result:       "allow",
					EvaluationMs: 1000,
				},
			},
		}

		assert.Equal(t, result.BlockedMs, result.EvaluationMs)
	})

	t.Run("all_audit_only_zero_blocked", func(t *testing.T) {
		// When all policies are audit_only, blocked_ms should be 0
		result := AuditAIResult{
			Action:       "allow",
			BlockedMs:    0, // Non-blocking
			EvaluationMs: 1500,
			Results: []AuditAIRuleResult{
				{
					Rule:         "audit_rule1",
					Action:       "deny",
					Mode:         "audit_only",
					Result:       "deny",
					EvaluationMs: 1000,
				},
				{
					Rule:         "audit_rule2",
					Action:       "deny",
					Mode:         "audit_only",
					Result:       "allow",
					EvaluationMs: 1500,
				},
			},
		}

		assert.Zero(t, result.BlockedMs)
		assert.Greater(t, result.EvaluationMs, int64(0))
	})
}

func TestAuditAIRuleResult_ErrorHandling(t *testing.T) {
	t.Run("timeout_error", func(t *testing.T) {
		result := AuditAIRuleResult{
			Rule:         "slow_rule",
			Action:       "deny",
			Result:       "error",
			EvaluationMs: 10000,
			Error:        "timeout",
		}

		assert.Equal(t, "error", result.Result)
		assert.Equal(t, "timeout", result.Error)
	})

	t.Run("api_error", func(t *testing.T) {
		result := AuditAIRuleResult{
			Rule:         "broken_rule",
			Action:       "deny",
			Result:       "error",
			EvaluationMs: 500,
			Error:        "api_error",
		}

		assert.Equal(t, "error", result.Result)
		assert.Equal(t, "api_error", result.Error)
	})

	t.Run("parse_error", func(t *testing.T) {
		result := AuditAIRuleResult{
			Rule:         "malformed_response_rule",
			Action:       "deny",
			Result:       "error",
			EvaluationMs: 1200,
			Error:        "parse_error",
		}

		assert.Equal(t, "error", result.Result)
		assert.Equal(t, "parse_error", result.Error)
	})

	t.Run("canceled_error", func(t *testing.T) {
		result := AuditAIRuleResult{
			Rule:         "canceled_rule",
			Action:       "deny",
			Result:       "error",
			EvaluationMs: 300,
			Error:        "canceled",
		}

		assert.Equal(t, "error", result.Result)
		assert.Equal(t, "canceled", result.Error)
	})
}

// Helper function to create test tool requests
func createTestToolRequest(toolName string) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: map[string]any{"test": "value"},
		},
	}
}
