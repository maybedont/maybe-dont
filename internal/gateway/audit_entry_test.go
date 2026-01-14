package gateway

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditAIResult_Serialization(t *testing.T) {
	t.Run("full_result_with_deciding_rule", func(t *testing.T) {
		result := AuditAIResult{
			Action:       "deny",
			BlockedMs:    847,
			EvaluationMs: 2341,
			DecidingRule: "block_destructive_ops",
			Reason:       "This operation would delete all user data",
			Results: []AuditAIRuleResult{
				{
					Rule:         "block_destructive_ops",
					Action:       "deny",
					Result:       "deny",
					EvaluationMs: 847,
				},
				{
					Rule:         "require_valid_repo",
					Action:       "allow",
					Result:       "allow",
					EvaluationMs: 1203,
				},
				{
					Rule:         "check_permissions",
					Action:       "deny",
					Mode:         "audit_only",
					Result:       "deny",
					EvaluationMs: 2341,
				},
			},
		}

		// Serialize to JSON
		jsonBytes, err := json.Marshal(result)
		require.NoError(t, err)

		// Verify expected fields are present
		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		assert.Equal(t, "deny", parsed["action"])
		assert.Equal(t, float64(847), parsed["blocked_ms"])
		assert.Equal(t, float64(2341), parsed["evaluation_ms"])
		assert.Equal(t, "block_destructive_ops", parsed["deciding_rule"])
		assert.Equal(t, "This operation would delete all user data", parsed["reason"])

		results := parsed["results"].([]interface{})
		assert.Len(t, results, 3)

		// Verify first result
		firstResult := results[0].(map[string]interface{})
		assert.Equal(t, "block_destructive_ops", firstResult["rule"])
		assert.Equal(t, "deny", firstResult["action"])
		assert.Equal(t, "deny", firstResult["result"])
		assert.Equal(t, float64(847), firstResult["evaluation_ms"])
		assert.Nil(t, firstResult["mode"]) // Should be omitted when not audit_only

		// Verify audit_only result has mode field
		thirdResult := results[2].(map[string]interface{})
		assert.Equal(t, "audit_only", thirdResult["mode"])
	})

	t.Run("allowed_result_no_deciding_rule", func(t *testing.T) {
		result := AuditAIResult{
			Action:       "allow",
			BlockedMs:    1847,
			EvaluationMs: 1847,
			// DecidingRule and Reason should be omitted
			Results: []AuditAIRuleResult{
				{
					Rule:         "block_destructive_ops",
					Action:       "deny",
					Result:       "allow",
					EvaluationMs: 847,
				},
			},
		}

		jsonBytes, err := json.Marshal(result)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		assert.Equal(t, "allow", parsed["action"])
		assert.Nil(t, parsed["deciding_rule"]) // Should be omitted
		assert.Nil(t, parsed["reason"])        // Should be omitted
	})

	t.Run("error_result", func(t *testing.T) {
		result := AuditAIResult{
			Action:       "allow",
			BlockedMs:    1203,
			EvaluationMs: 10000,
			Results: []AuditAIRuleResult{
				{
					Rule:         "require_valid_repo",
					Action:       "allow",
					Result:       "allow",
					EvaluationMs: 1203,
				},
				{
					Rule:         "check_slow_api",
					Action:       "deny",
					Mode:         "audit_only",
					Result:       "error",
					EvaluationMs: 10000,
					Error:        "timeout",
				},
			},
		}

		jsonBytes, err := json.Marshal(result)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		results := parsed["results"].([]interface{})
		errorResult := results[1].(map[string]interface{})
		assert.Equal(t, "error", errorResult["result"])
		assert.Equal(t, "timeout", errorResult["error"])
	})

	t.Run("audit_only_non_blocking", func(t *testing.T) {
		result := AuditAIResult{
			Action:       "allow",
			BlockedMs:    0, // Non-blocking
			EvaluationMs: 1847,
			Results: []AuditAIRuleResult{
				{
					Rule:         "log_destructive_ops",
					Action:       "deny",
					Mode:         "audit_only",
					Result:       "deny",
					EvaluationMs: 1200,
				},
				{
					Rule:         "log_permissions",
					Action:       "deny",
					Mode:         "audit_only",
					Result:       "allow",
					EvaluationMs: 1847,
				},
			},
		}

		jsonBytes, err := json.Marshal(result)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		assert.Equal(t, "allow", parsed["action"])
		assert.Equal(t, float64(0), parsed["blocked_ms"])
	})
}

func TestAuditAIResult_Deserialization(t *testing.T) {
	t.Run("full_result_from_json", func(t *testing.T) {
		jsonStr := `{
			"action": "deny",
			"blocked_ms": 847,
			"evaluation_ms": 2341,
			"deciding_rule": "block_destructive_ops",
			"reason": "This operation would delete all user data",
			"results": [
				{
					"rule": "block_destructive_ops",
					"action": "deny",
					"result": "deny",
					"evaluation_ms": 847
				},
				{
					"rule": "check_permissions",
					"action": "deny",
					"mode": "audit_only",
					"result": "deny",
					"evaluation_ms": 2341
				}
			]
		}`

		var result AuditAIResult
		err := json.Unmarshal([]byte(jsonStr), &result)
		require.NoError(t, err)

		assert.Equal(t, "deny", result.Action)
		assert.Equal(t, int64(847), result.BlockedMs)
		assert.Equal(t, int64(2341), result.EvaluationMs)
		assert.Equal(t, "block_destructive_ops", result.DecidingRule)
		assert.Equal(t, "This operation would delete all user data", result.Reason)
		assert.Len(t, result.Results, 2)

		assert.Equal(t, "block_destructive_ops", result.Results[0].Rule)
		assert.Equal(t, "deny", result.Results[0].Action)
		assert.Equal(t, "deny", result.Results[0].Result)
		assert.Equal(t, "", result.Results[0].Mode) // Not present in JSON

		assert.Equal(t, "check_permissions", result.Results[1].Rule)
		assert.Equal(t, "audit_only", result.Results[1].Mode)
	})

	t.Run("result_with_missing_optional_fields", func(t *testing.T) {
		jsonStr := `{
			"action": "allow",
			"blocked_ms": 1000,
			"evaluation_ms": 1000,
			"results": []
		}`

		var result AuditAIResult
		err := json.Unmarshal([]byte(jsonStr), &result)
		require.NoError(t, err)

		assert.Equal(t, "allow", result.Action)
		assert.Equal(t, "", result.DecidingRule) // Not present
		assert.Equal(t, "", result.Reason)       // Not present
		assert.Empty(t, result.Results)
	})
}

func TestAuditContext_SetRequestValidationAI(t *testing.T) {
	t.Run("sets_ai_details", func(t *testing.T) {
		ctx := NewAuditContext("github__create_issue", "github", "create_issue", "sess-123", "127.0.0.1", "req-456")

		aiResult := &AuditAIResult{
			Action:       "deny",
			BlockedMs:    500,
			EvaluationMs: 1000,
			DecidingRule: "test_rule",
			Reason:       "test reason",
			Results: []AuditAIRuleResult{
				{
					Rule:         "test_rule",
					Action:       "deny",
					Result:       "deny",
					EvaluationMs: 500,
				},
			},
		}

		ctx.SetRequestValidationAI(aiResult)

		entry := ctx.Entry()
		require.NotNil(t, entry.RequestValidation)
		require.NotNil(t, entry.RequestValidation.AI)
		assert.Equal(t, "deny", entry.RequestValidation.AI.Action)
		assert.Equal(t, int64(500), entry.RequestValidation.AI.BlockedMs)
		assert.Equal(t, "test_rule", entry.RequestValidation.AI.DecidingRule)
		assert.Len(t, entry.RequestValidation.AI.Results, 1)
	})
}

func TestAuditEntry_FullSerialization(t *testing.T) {
	t.Run("complete_entry_with_ai_validation", func(t *testing.T) {
		entry := &AuditEntry{
			CreatedAt: "2026-01-14T16:00:00Z",
			Tool: AuditToolInfo{
				Name:         "create_issue",
				Client:       "github",
				PrefixedName: "github__create_issue",
			},
			IncomingRequest: IncomingRequestInfo{
				RequestID: "req-123",
				SessionID: "sess-456",
				ClientIP:  "127.0.0.1",
			},
			Request: AuditRequestInfo{
				Params: map[string]interface{}{
					"title": "Test issue",
				},
			},
			RequestValidation: &AuditValidationInfo{
				AI: &AuditAIResult{
					Action:       "deny",
					BlockedMs:    847,
					EvaluationMs: 2341,
					DecidingRule: "block_destructive_ops",
					Reason:       "This operation would delete all user data",
					Results: []AuditAIRuleResult{
						{
							Rule:         "block_destructive_ops",
							Action:       "deny",
							Result:       "deny",
							EvaluationMs: 847,
						},
					},
				},
			},
			RecommendedAction: "deny",
			Action:            "deny",
			DurationMs:        2500,
		}

		jsonBytes, err := json.MarshalIndent(entry, "", "  ")
		require.NoError(t, err)

		// Parse back and verify
		var parsed AuditEntry
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		require.NotNil(t, parsed.RequestValidation)
		require.NotNil(t, parsed.RequestValidation.AI)
		assert.Equal(t, "deny", parsed.RequestValidation.AI.Action)
		assert.Equal(t, int64(847), parsed.RequestValidation.AI.BlockedMs)
		assert.Equal(t, "block_destructive_ops", parsed.RequestValidation.AI.DecidingRule)
		assert.Len(t, parsed.RequestValidation.AI.Results, 1)
	})
}

func TestAuditEntry_EarlyTerminationScenarios(t *testing.T) {
	t.Run("early_termination_deny", func(t *testing.T) {
		// Simulate early termination: first rule denies, second rule still running
		entry := &AuditEntry{
			CreatedAt: "2026-01-14T16:00:00Z",
			Tool: AuditToolInfo{
				Name:         "delete_repo",
				Client:       "github",
				PrefixedName: "github__delete_repo",
			},
			RequestValidation: &AuditValidationInfo{
				AI: &AuditAIResult{
					Action:       "deny",
					BlockedMs:    847,    // Stopped blocking after first deny
					EvaluationMs: 1500,   // Total time including all results
					DecidingRule: "block_destructive_ops",
					Reason:       "This operation would delete repository",
					Results: []AuditAIRuleResult{
						{
							Rule:         "block_destructive_ops",
							Action:       "deny",
							Result:       "deny",
							EvaluationMs: 847,
						},
						{
							Rule:         "check_permissions",
							Action:       "deny",
							Mode:         "audit_only",
							Result:       "allow",
							EvaluationMs: 1500,
						},
					},
				},
			},
			RecommendedAction: "deny",
			Action:            "deny",
			DurationMs:        1600,
		}

		jsonBytes, err := json.Marshal(entry)
		require.NoError(t, err)

		var parsed AuditEntry
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		// Verify early termination timing
		assert.Less(t, parsed.RequestValidation.AI.BlockedMs, parsed.RequestValidation.AI.EvaluationMs)
		assert.Equal(t, "block_destructive_ops", parsed.RequestValidation.AI.DecidingRule)
	})

	t.Run("all_policies_pass", func(t *testing.T) {
		// All policies pass - blocked_ms equals evaluation_ms
		entry := &AuditEntry{
			CreatedAt: "2026-01-14T16:00:00Z",
			Tool: AuditToolInfo{
				Name:         "create_issue",
				Client:       "github",
				PrefixedName: "github__create_issue",
			},
			RequestValidation: &AuditValidationInfo{
				AI: &AuditAIResult{
					Action:       "allow",
					BlockedMs:    1500,
					EvaluationMs: 1500,
					// No DecidingRule or Reason for allow
					Results: []AuditAIRuleResult{
						{
							Rule:         "block_destructive_ops",
							Action:       "deny",
							Result:       "allow",
							EvaluationMs: 800,
						},
						{
							Rule:         "require_valid_repo",
							Action:       "allow",
							Result:       "allow",
							EvaluationMs: 1500,
						},
					},
				},
			},
			RecommendedAction: "allow",
			Action:            "allow",
			DurationMs:        1600,
		}

		jsonBytes, err := json.Marshal(entry)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		// Verify deciding_rule and reason are omitted (not present) when action is allow
		reqVal := parsed["request_validation"].(map[string]interface{})
		ai := reqVal["ai"].(map[string]interface{})
		_, hasDecidingRule := ai["deciding_rule"]
		_, hasReason := ai["reason"]
		assert.False(t, hasDecidingRule, "deciding_rule should be omitted for allow")
		assert.False(t, hasReason, "reason should be omitted for allow")
	})

	t.Run("all_audit_only_non_blocking", func(t *testing.T) {
		// All policies are audit_only - blocked_ms is 0
		entry := &AuditEntry{
			CreatedAt: "2026-01-14T16:00:00Z",
			Tool: AuditToolInfo{
				Name:         "search_code",
				Client:       "github",
				PrefixedName: "github__search_code",
			},
			RequestValidation: &AuditValidationInfo{
				AI: &AuditAIResult{
					Action:       "allow",
					BlockedMs:    0,    // Non-blocking
					EvaluationMs: 2000, // Still wait for audit results
					Results: []AuditAIRuleResult{
						{
							Rule:         "log_search_patterns",
							Action:       "deny",
							Mode:         "audit_only",
							Result:       "deny",
							EvaluationMs: 1200,
						},
						{
							Rule:         "log_access",
							Action:       "deny",
							Mode:         "audit_only",
							Result:       "allow",
							EvaluationMs: 2000,
						},
					},
				},
			},
			RecommendedAction: "allow",
			Action:            "allow",
			DurationMs:        2100,
		}

		jsonBytes, err := json.Marshal(entry)
		require.NoError(t, err)

		var parsed AuditEntry
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		// Verify non-blocking behavior
		assert.Zero(t, parsed.RequestValidation.AI.BlockedMs)
		assert.Greater(t, parsed.RequestValidation.AI.EvaluationMs, int64(0))
		assert.Equal(t, "allow", parsed.RequestValidation.AI.Action)

		// Verify all results have mode field
		for _, result := range parsed.RequestValidation.AI.Results {
			assert.Equal(t, "audit_only", result.Mode)
		}
	})

	t.Run("error_on_enabled_policy", func(t *testing.T) {
		// Error on enabled policy causes deny (fail closed)
		entry := &AuditEntry{
			CreatedAt: "2026-01-14T16:00:00Z",
			Tool: AuditToolInfo{
				Name:         "create_pr",
				Client:       "github",
				PrefixedName: "github__create_pr",
			},
			RequestValidation: &AuditValidationInfo{
				AI: &AuditAIResult{
					Action:       "deny",
					BlockedMs:    10000,
					EvaluationMs: 10000,
					DecidingRule: "check_sensitive_changes",
					Reason:       "Rule evaluation failed: timeout",
					Results: []AuditAIRuleResult{
						{
							Rule:         "check_sensitive_changes",
							Action:       "deny",
							Result:       "error",
							EvaluationMs: 10000,
							Error:        "timeout",
						},
					},
				},
			},
			RecommendedAction: "deny",
			Action:            "deny",
			DurationMs:        10100,
		}

		jsonBytes, err := json.Marshal(entry)
		require.NoError(t, err)

		var parsed AuditEntry
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		// Verify error handling
		assert.Equal(t, "deny", parsed.RequestValidation.AI.Action)
		assert.Equal(t, "check_sensitive_changes", parsed.RequestValidation.AI.DecidingRule)
		assert.Contains(t, parsed.RequestValidation.AI.Reason, "timeout")
		assert.Equal(t, "error", parsed.RequestValidation.AI.Results[0].Result)
		assert.Equal(t, "timeout", parsed.RequestValidation.AI.Results[0].Error)
	})

	t.Run("mixed_enabled_and_audit_only", func(t *testing.T) {
		// Mix of enabled and audit_only policies
		entry := &AuditEntry{
			CreatedAt: "2026-01-14T16:00:00Z",
			Tool: AuditToolInfo{
				Name:         "merge_pr",
				Client:       "github",
				PrefixedName: "github__merge_pr",
			},
			RequestValidation: &AuditValidationInfo{
				AI: &AuditAIResult{
					Action:       "allow",
					BlockedMs:    1200,
					EvaluationMs: 2500,
					Results: []AuditAIRuleResult{
						{
							Rule:         "require_approval",
							Action:       "allow",
							Result:       "allow",
							EvaluationMs: 800,
						},
						{
							Rule:         "block_force_merge",
							Action:       "deny",
							Result:       "allow",
							EvaluationMs: 1200,
						},
						{
							Rule:         "log_merge_activity",
							Action:       "deny",
							Mode:         "audit_only",
							Result:       "deny",
							EvaluationMs: 2500,
						},
					},
				},
			},
			RecommendedAction: "allow",
			Action:            "allow",
			DurationMs:        2600,
		}

		jsonBytes, err := json.Marshal(entry)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		// Verify mode field is only present on audit_only policies
		reqVal := parsed["request_validation"].(map[string]interface{})
		ai := reqVal["ai"].(map[string]interface{})
		results := ai["results"].([]interface{})

		// First two rules are enabled (no mode field)
		firstResult := results[0].(map[string]interface{})
		_, hasMode := firstResult["mode"]
		assert.False(t, hasMode, "Enabled policies should not have mode field")

		secondResult := results[1].(map[string]interface{})
		_, hasMode = secondResult["mode"]
		assert.False(t, hasMode, "Enabled policies should not have mode field")

		// Third rule is audit_only (has mode field)
		thirdResult := results[2].(map[string]interface{})
		assert.Equal(t, "audit_only", thirdResult["mode"])
	})
}
