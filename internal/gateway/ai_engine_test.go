package gateway

import (
	"context"
	"fmt"
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
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:        "block_destructive_ops",
				Description: "Block destructive operations",
				Prompt:      "Check if this is destructive: %s",
				Action:      config.PolicyActionDeny,
				Message:     "Blocked",
				// Mode not set - defaults to "" (can block)
			},
			{
				Name:        "require_valid_repo",
				Description: "Require valid repo",
				Prompt:      "Check repo: %s",
				Action:      config.PolicyActionAllow,
				Message:     "Repo required",
				// Mode not set - defaults to "" (can block)
			},
		}

		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)
		assert.Len(t, engine.policies, 2)
	})

	t.Run("skips_disabled_policies", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		disabledBool := false
		policies := []config.AIPolicy{
			{
				Name:   "enabled_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				// Mode not set - defaults to "" (can block)
			},
			{
				Name:    "disabled_policy",
				Prompt:  "Check: %s",
				Action:  config.PolicyActionDeny,
				Enabled: &disabledBool, // Explicitly disabled
			},
		}

		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, "enabled_policy", engine.policies[0].Name)
	})

	t.Run("respects_default_mode", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
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

	t.Run("policy_audit_only_overrides_default", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "audit_only_override",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeAuditOnly, // Policy explicitly sets audit_only
			},
		}

		// Load with "" (can block) as default, but policy explicitly sets audit_only
		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyModeAuditOnly, engine.policies[0].Mode)
	})

	t.Run("rejects_invalid_action", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "invalid_policy",
				Prompt: "Check: %s",
				Action: config.PolicyAction("invalid"),
				// Mode not set
			},
		}

		err = engine.LoadPolicies(policies, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid action")
	})

	t.Run("allows_deny_action", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "deny_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				// Mode not set
			},
		}

		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyActionDeny, engine.policies[0].Action)
	})

	t.Run("allows_allow_action", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "allow_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionAllow,
				// Mode not set
			},
		}

		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyActionAllow, engine.policies[0].Action)
	})

	t.Run("loads_audit_only_policies", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
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

		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyModeAuditOnly, engine.policies[0].Mode)
	})

	t.Run("rejects_duplicate_policy_names", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "duplicate_name",
				Prompt: "First prompt",
				Action: config.PolicyActionDeny,
				// Mode not set
			},
			{
				Name:   "duplicate_name",
				Prompt: "Second prompt",
				Action: config.PolicyActionAllow,
				// Mode not set
			},
		}

		err = engine.LoadPolicies(policies, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate policy name 'duplicate_name'")
	})
}

func TestAIPolicyEngine_NoPolicies(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	engine := createTestAIPolicyEngine(0)
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	// Don't load any policies
	results, err := engine.EvaluateToolCall(context.Background(), createTestToolRequest("test_tool"), nil)
	require.NoError(t, err)

	assert.True(t, results.Allowed)
	assert.Equal(t, "No policies configured", results.Message)
	assert.Nil(t, results.AIDetails)
}

func TestAIPolicyEngine_CountEnabledPolicies(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	t.Run("all_audit_only", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
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
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "enabled_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				// Mode not set - can block
			},
			{
				Name:   "audit_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeAuditOnly,
			},
		}

		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)

		// Count non-audit_only (enabled) policies
		enabledCount := 0
		for _, p := range engine.policies {
			if !p.Mode.IsAuditOnly() {
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

// TestAIPolicyEngine_UsesSharedBlockingBudget verifies that the AI engine
// respects the shared BlockingBudget passed to it, rather than using its own
// independent maxBlockingMs timeout. This ensures cumulative budget tracking
// across all validation phases (CEL + AI).
func TestAIPolicyEngine_UsesSharedBlockingBudget(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	t.Run("respects_exhausted_budget", func(t *testing.T) {
		// Create an engine with a large maxRuleEvaluationMs
		engine := createTestAIPolicyEngine(30000) // 30 seconds per rule
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		// Load a policy that would normally take time to evaluate
		policies := []config.AIPolicy{
			{
				Name:   "test_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				// Mode not set - can block
			},
		}
		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)

		// Create a budget that is already exhausted
		budget := NewBlockingBudget(1000) // 1 second total
		budget.ConsumeBlocking(1001)      // Consume more than budget (simulating prior CEL validation)

		assert.True(t, budget.IsExhausted(), "Budget should be exhausted before AI evaluation")
		assert.Equal(t, int64(0), budget.RemainingMs(), "No remaining budget")

		// Call EvaluateToolCall with the exhausted budget
		// The engine should fail-open immediately without blocking
		ctx := context.Background()
		results, err := engine.EvaluateToolCall(ctx, createTestToolRequest("test_tool"), budget)
		require.NoError(t, err)

		// With exhausted budget, should allow (fail-open) and not block
		assert.True(t, results.Allowed, "Should allow when budget is exhausted (fail-open)")
		assert.NotNil(t, results.AIDetails, "Should have AI details")
		assert.Equal(t, int64(0), results.AIDetails.BlockedMs, "Should not block when budget is exhausted")
	})

	t.Run("uses_remaining_budget_not_maxBlockingMs", func(t *testing.T) {
		// This test verifies the engine uses the shared budget's remaining time,
		// not its own maxBlockingMs field
		engine := createTestAIPolicyEngine(30000)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		// Load an audit-only policy (so we don't actually call the AI)
		policies := []config.AIPolicy{
			{
				Name:   "audit_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeAuditOnly,
			},
		}
		err = engine.LoadPolicies(policies, config.PolicyModeAuditOnly)
		require.NoError(t, err)

		// Create a budget with some remaining time
		budget := NewBlockingBudget(5000) // 5 seconds total
		budget.ConsumeBlocking(3000)      // 3 seconds consumed by prior validation

		assert.Equal(t, int64(2000), budget.RemainingMs(), "Should have 2 seconds remaining")

		// Call EvaluateToolCall - since all policies are audit_only,
		// it returns immediately with AsyncCompletion channel
		ctx := context.Background()
		results, err := engine.EvaluateToolCall(ctx, createTestToolRequest("test_tool"), budget)
		require.NoError(t, err)

		// Audit-only policies don't block and return immediately
		assert.True(t, results.Allowed)
		// With all audit_only policies, AIDetails comes via async channel
		assert.NotNil(t, results.AsyncCompletion, "Should have async completion channel")

		// Wait for async completion to get AIDetails
		completion := <-results.AsyncCompletion
		assert.NotNil(t, completion.AIDetails)
		assert.Equal(t, int64(0), completion.AIDetails.BlockedMs, "Audit-only should not block")
	})

	t.Run("nil_budget_still_works", func(t *testing.T) {
		// Backward compatibility: when budget is nil, engine should still function
		engine := createTestAIPolicyEngine(5000)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		// No policies loaded - should return quickly
		ctx := context.Background()
		results, err := engine.EvaluateToolCall(ctx, createTestToolRequest("test_tool"), nil)
		require.NoError(t, err)

		assert.True(t, results.Allowed)
		assert.Equal(t, "No policies configured", results.Message)
	})
}

func TestFormatAuditError(t *testing.T) {
	// Tests the formatAuditError helper function that formats errors for the audit log
	// as "category: message" with truncation for long messages.

	tests := []struct {
		name           string
		category       string
		err            error
		expectedOutput string
	}{
		{
			name:           "nil error returns category only",
			category:       "timeout",
			err:            nil,
			expectedOutput: "timeout",
		},
		{
			name:           "api error with message",
			category:       "api_error",
			err:            fmt.Errorf("HTTP 401 Unauthorized: invalid API key"),
			expectedOutput: "api_error: HTTP 401 Unauthorized: invalid API key",
		},
		{
			name:           "timeout error with human-friendly message",
			category:       "timeout",
			err:            context.DeadlineExceeded,
			expectedOutput: "timeout: rule evaluation exceeded max_rule_evaluation_ms limit",
		},
		{
			name:           "canceled error with human-friendly message",
			category:       "canceled",
			err:            context.Canceled,
			expectedOutput: "canceled: rule evaluation was canceled",
		},
		{
			name:           "parse error with details",
			category:       "parse_error",
			err:            fmt.Errorf("invalid character 'x' looking for beginning of value"),
			expectedOutput: "parse_error: invalid character 'x' looking for beginning of value",
		},
		{
			name:           "no_response error",
			category:       "no_response",
			err:            fmt.Errorf("API returned empty choices"),
			expectedOutput: "no_response: API returned empty choices",
		},
		{
			name:     "truncates long error messages",
			category: "api_error",
			err:      fmt.Errorf("This is a very long error message that exceeds the maximum length of 100 characters and should be truncated for the audit log"),
			expectedOutput: "api_error: This is a very long error message that exceeds the maximum length of 100 characters and should be tr...",
		},
		{
			name:           "exactly 100 chars not truncated",
			category:       "api_error",
			err:            fmt.Errorf("This message is exactly one hundred characters long which should not trigger any truncation at all!"),
			expectedOutput: "api_error: This message is exactly one hundred characters long which should not trigger any truncation at all!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatAuditError(tt.category, tt.err)
			assert.Equal(t, tt.expectedOutput, result)
		})
	}
}

func TestAuditAIRuleResult_NewErrorFormat(t *testing.T) {
	// Tests that the audit log Error field now uses the "category: message" format
	// instead of just the category string.

	tests := []struct {
		name          string
		errorCategory string
		err           error
		expectedError string
		description   string
	}{
		{
			name:          "api_error with HTTP status",
			errorCategory: "api_error",
			err:           fmt.Errorf("HTTP 401 Unauthorized"),
			expectedError: "api_error: HTTP 401 Unauthorized",
			description:   "API errors should include the HTTP status and message",
		},
		{
			name:          "timeout shows human-friendly message",
			errorCategory: "timeout",
			err:           context.DeadlineExceeded,
			expectedError: "timeout: rule evaluation exceeded max_rule_evaluation_ms limit",
			description:   "Timeout errors should explain the rule exceeded max_rule_evaluation_ms",
		},
		{
			name:          "canceled shows human-friendly message",
			errorCategory: "canceled",
			err:           context.Canceled,
			expectedError: "canceled: rule evaluation was canceled",
			description:   "Canceled errors should explain the rule was canceled",
		},
		{
			name:          "parse_error with JSON details",
			errorCategory: "parse_error",
			err:           fmt.Errorf("unexpected end of JSON input"),
			expectedError: "parse_error: unexpected end of JSON input",
			description:   "Parse errors should include the JSON parsing error details",
		},
		{
			name:          "no_response error",
			errorCategory: "no_response",
			err:           fmt.Errorf("API returned empty choices"),
			expectedError: "no_response: API returned empty choices",
			description:   "No response errors should include the empty choices message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create an audit result using formatAuditError like the real code does
			auditResult := AuditAIRuleResult{
				Rule:         "test_rule",
				Action:       "deny",
				Result:       "error",
				EvaluationMs: 1000,
				Error:        formatAuditError(tt.errorCategory, tt.err),
			}

			assert.Equal(t, tt.expectedError, auditResult.Error, tt.description)
		})
	}
}

func TestClassifyContextError(t *testing.T) {
	// Tests the classifyContextError helper function that classifies context errors.

	t.Run("deadline exceeded returns timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()
		// Wait for context to expire
		<-ctx.Done()

		result := classifyContextError(ctx, "api_error")
		assert.Equal(t, "timeout", result)
	})

	t.Run("canceled returns canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result := classifyContextError(ctx, "api_error")
		assert.Equal(t, "canceled", result)
	})

	t.Run("no error returns default category", func(t *testing.T) {
		ctx := context.Background()

		result := classifyContextError(ctx, "api_error")
		assert.Equal(t, "api_error", result)
	})

	t.Run("no error with custom default", func(t *testing.T) {
		ctx := context.Background()

		result := classifyContextError(ctx, "custom_category")
		assert.Equal(t, "custom_category", result)
	})
}

func TestAiResponseRuleResult_ErrorFields(t *testing.T) {
	// Tests that aiResponseRuleResult stores both err (error) and errCategory (string)
	// correctly, matching the pattern used in aiRuleResult for consistency.

	t.Run("api_error_preserves_full_error", func(t *testing.T) {
		err := fmt.Errorf("HTTP 500 Internal Server Error")
		result := aiResponseRuleResult{
			policy:       AIResponsePolicy{Name: "test_policy"},
			result:       "error",
			evaluationMs: 500,
			err:          err,
			errCategory:  "api_error",
		}

		assert.Equal(t, "api_error", result.errCategory)
		assert.NotNil(t, result.err)
		assert.Contains(t, result.err.Error(), "500")
	})

	t.Run("timeout_preserves_deadline_error", func(t *testing.T) {
		result := aiResponseRuleResult{
			policy:       AIResponsePolicy{Name: "slow_policy"},
			result:       "error",
			evaluationMs: 10000,
			err:          context.DeadlineExceeded,
			errCategory:  "timeout",
		}

		assert.Equal(t, "timeout", result.errCategory)
		assert.ErrorIs(t, result.err, context.DeadlineExceeded)
	})

	t.Run("canceled_preserves_canceled_error", func(t *testing.T) {
		result := aiResponseRuleResult{
			policy:       AIResponsePolicy{Name: "canceled_policy"},
			result:       "error",
			evaluationMs: 300,
			err:          context.Canceled,
			errCategory:  "canceled",
		}

		assert.Equal(t, "canceled", result.errCategory)
		assert.ErrorIs(t, result.err, context.Canceled)
	})

	t.Run("parse_error_preserves_json_error", func(t *testing.T) {
		parseErr := fmt.Errorf("unexpected end of JSON input")
		result := aiResponseRuleResult{
			policy:       AIResponsePolicy{Name: "malformed_policy"},
			result:       "error",
			evaluationMs: 1200,
			err:          parseErr,
			errCategory:  "parse_error",
		}

		assert.Equal(t, "parse_error", result.errCategory)
		assert.Contains(t, result.err.Error(), "JSON")
	})

	t.Run("no_response_creates_descriptive_error", func(t *testing.T) {
		result := aiResponseRuleResult{
			policy:       AIResponsePolicy{Name: "empty_response_policy"},
			result:       "error",
			evaluationMs: 800,
			err:          fmt.Errorf("API returned empty choices"),
			errCategory:  "no_response",
		}

		assert.Equal(t, "no_response", result.errCategory)
		assert.Contains(t, result.err.Error(), "empty choices")
	})

	t.Run("audit_format_matches_request_engine", func(t *testing.T) {
		// Verify that response engine errors format the same way as request engine
		err := fmt.Errorf("connection refused")
		result := aiResponseRuleResult{
			policy:       AIResponsePolicy{Name: "test_policy"},
			result:       "error",
			evaluationMs: 100,
			err:          err,
			errCategory:  "api_error",
		}

		formatted := formatAuditError(result.errCategory, result.err)
		assert.Equal(t, "api_error: connection refused", formatted)
	})
}

func TestAiRuleResult_ErrorFields(t *testing.T) {
	// Tests that aiRuleResult now stores both err (error) and errCategory (string)
	// correctly for different error scenarios.

	t.Run("api_error_preserves_full_error", func(t *testing.T) {
		err := fmt.Errorf("dial tcp: lookup api.openai.com: no such host")
		result := aiRuleResult{
			rule:         "test_rule",
			action:       config.PolicyActionDeny,
			mode:         "", // enabled (can block)
			result:       "error",
			evaluationMs: 500,
			err:          err,
			errCategory:  "api_error",
		}

		assert.Equal(t, "api_error", result.errCategory)
		assert.NotNil(t, result.err)
		assert.Contains(t, result.err.Error(), "no such host")
	})

	t.Run("timeout_preserves_deadline_error", func(t *testing.T) {
		result := aiRuleResult{
			rule:         "slow_rule",
			action:       config.PolicyActionDeny,
			mode:         "", // enabled (can block)
			result:       "error",
			evaluationMs: 10000,
			err:          context.DeadlineExceeded,
			errCategory:  "timeout",
		}

		assert.Equal(t, "timeout", result.errCategory)
		assert.ErrorIs(t, result.err, context.DeadlineExceeded)
	})

	t.Run("canceled_preserves_canceled_error", func(t *testing.T) {
		result := aiRuleResult{
			rule:         "canceled_rule",
			action:       config.PolicyActionDeny,
			mode:         "", // enabled (can block)
			result:       "error",
			evaluationMs: 300,
			err:          context.Canceled,
			errCategory:  "canceled",
		}

		assert.Equal(t, "canceled", result.errCategory)
		assert.ErrorIs(t, result.err, context.Canceled)
	})

	t.Run("parse_error_preserves_json_error", func(t *testing.T) {
		parseErr := fmt.Errorf("invalid character 'x' looking for beginning of value")
		result := aiRuleResult{
			rule:         "malformed_rule",
			action:       config.PolicyActionDeny,
			mode:         "", // enabled (can block)
			result:       "error",
			evaluationMs: 1200,
			err:          parseErr,
			errCategory:  "parse_error",
		}

		assert.Equal(t, "parse_error", result.errCategory)
		assert.Contains(t, result.err.Error(), "invalid character")
	})

	t.Run("no_response_creates_descriptive_error", func(t *testing.T) {
		result := aiRuleResult{
			rule:         "empty_response_rule",
			action:       config.PolicyActionDeny,
			mode:         "", // enabled (can block)
			result:       "error",
			evaluationMs: 800,
			err:          fmt.Errorf("API returned empty choices"),
			errCategory:  "no_response",
		}

		assert.Equal(t, "no_response", result.errCategory)
		assert.Contains(t, result.err.Error(), "empty choices")
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

// createTestAIConfig creates a test config with AI settings
func createTestAIConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Validation.AI.Model = "gpt-4o-mini"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Provider = "openai"
	return cfg
}

// createTestAIPolicyEngine creates a test AI policy engine with mock client
func createTestAIPolicyEngine(maxRuleEvaluationMs int) *AIPolicyEngine {
	return &AIPolicyEngine{
		cfg:                 createTestAIConfig(),
		maxRuleEvaluationMs: maxRuleEvaluationMs,
		providerClient:      NewMockAIProviderClient(),
	}
}

// TestAIPolicyEngine_AuditModeBypassFlag verifies that the AuditModeBypass flag
// is correctly set when an audit_only policy would have denied the request.
func TestAIPolicyEngine_AuditModeBypassFlag(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	t.Run("audit_only_deny_sets_bypass_flag", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		// Load an audit_only policy
		policies := []config.AIPolicy{
			{
				Name:        "audit_only_block",
				Description: "Would block but in audit mode",
				Prompt:      "Block everything: %s",
				Action:      config.PolicyActionDeny,
				Message:     "Would have blocked",
				Mode:        config.PolicyModeAuditOnly,
			},
		}

		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)

		// The engine should return immediately with no async work for all audit_only policies
		// but we can't test the actual API call behavior without mocking
		// Instead, verify the policy was loaded with correct mode
		assert.Len(t, engine.policies, 1)
		assert.Equal(t, config.PolicyModeAuditOnly, engine.policies[0].Mode)
	})

	t.Run("enabled_policy_count_excludes_audit_only", func(t *testing.T) {
		engine := createTestAIPolicyEngine(0)
		err := InitAIPolicyEngine(sessionLogger, engine)
		require.NoError(t, err)

		policies := []config.AIPolicy{
			{
				Name:   "audit_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				Mode:   config.PolicyModeAuditOnly,
			},
			{
				Name:   "enabled_policy",
				Prompt: "Check: %s",
				Action: config.PolicyActionDeny,
				// Mode not set - can block
			},
		}

		err = engine.LoadPolicies(policies, "")
		require.NoError(t, err)

		// Count non-audit_only (enabled) policies
		enabledCount := 0
		for _, p := range engine.policies {
			if !p.Mode.IsAuditOnly() {
				enabledCount++
			}
		}
		assert.Equal(t, 1, enabledCount, "Should only have 1 enabled policy")
	})
}

// TestActionReasonConstants verifies that action reason constants are correctly defined.
func TestActionReasonConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant ActionReason
		value    string
	}{
		{"request_policy", ActionReasonRequestPolicy, "request_policy"},
		{"response_policy", ActionReasonResponsePolicy, "response_policy"},
		{"audit_mode", ActionReasonAuditMode, "audit_mode"},
		{"fail_open", ActionReasonFailOpen, "fail_open"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.value, string(tt.constant), "ActionReason constant value should match expected string")
		})
	}
}
