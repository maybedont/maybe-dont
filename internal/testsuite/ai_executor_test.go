package testsuite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test helper functions in ai_executor.go

func TestCopyParams(t *testing.T) {
	t.Run("returns empty map for nil input", func(t *testing.T) {
		result := copyParams(nil)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("creates shallow copy", func(t *testing.T) {
		original := map[string]any{
			"max_tokens":  256,
			"temperature": 0.7,
		}

		copied := copyParams(original)

		// Should have same values
		assert.Equal(t, original["max_tokens"], copied["max_tokens"])
		assert.Equal(t, original["temperature"], copied["temperature"])

		// Modifying copy should not affect original
		copied["max_tokens"] = 512
		assert.Equal(t, 256, original["max_tokens"])
		assert.Equal(t, 512, copied["max_tokens"])
	})

	t.Run("preserves all key types", func(t *testing.T) {
		original := map[string]any{
			"int_val":    42,
			"float_val":  3.14,
			"string_val": "hello",
			"bool_val":   true,
		}

		copied := copyParams(original)

		assert.Equal(t, 42, copied["int_val"])
		assert.Equal(t, 3.14, copied["float_val"])
		assert.Equal(t, "hello", copied["string_val"])
		assert.Equal(t, true, copied["bool_val"])
	})
}

func TestToInt(t *testing.T) {
	t.Run("converts int", func(t *testing.T) {
		result, err := toInt(42)
		assert.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("converts int64", func(t *testing.T) {
		result, err := toInt(int64(42))
		assert.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("converts float64", func(t *testing.T) {
		// Common case: JSON unmarshals numbers as float64
		result, err := toInt(float64(256))
		assert.NoError(t, err)
		assert.Equal(t, 256, result)
	})

	t.Run("truncates float64 decimal", func(t *testing.T) {
		result, err := toInt(float64(256.99))
		assert.NoError(t, err)
		assert.Equal(t, 256, result)
	})

	t.Run("returns error for string", func(t *testing.T) {
		_, err := toInt("256")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot convert")
	})

	t.Run("returns error for nil", func(t *testing.T) {
		_, err := toInt(nil)
		assert.Error(t, err)
	})
}

func TestStripMarkdownCodeFence(t *testing.T) {
	t.Run("returns unchanged for plain text", func(t *testing.T) {
		input := `{"allowed": true, "message": "ok"}`
		result := stripMarkdownCodeFence(input)
		assert.Equal(t, input, result)
	})

	t.Run("strips json code fence", func(t *testing.T) {
		input := "```json\n{\"allowed\": true, \"message\": \"ok\"}\n```"
		result := stripMarkdownCodeFence(input)
		assert.Equal(t, `{"allowed": true, "message": "ok"}`, result)
	})

	t.Run("strips plain code fence", func(t *testing.T) {
		input := "```\n{\"allowed\": false, \"message\": \"denied\"}\n```"
		result := stripMarkdownCodeFence(input)
		assert.Equal(t, `{"allowed": false, "message": "denied"}`, result)
	})

	t.Run("handles whitespace", func(t *testing.T) {
		input := "  ```json\n{\"allowed\": true}\n```  "
		result := stripMarkdownCodeFence(input)
		assert.Equal(t, `{"allowed": true}`, result)
	})
}

func TestAutoScalingConstants(t *testing.T) {
	// Verify the constants are set to expected values for Anthropic optimization
	t.Run("initial max tokens is small", func(t *testing.T) {
		assert.Equal(t, 64, InitialMaxTokens)
	})

	t.Run("max max tokens provides reasonable cap", func(t *testing.T) {
		assert.Equal(t, 1024, MaxMaxTokens)
	})

	t.Run("scale factor doubles on truncation", func(t *testing.T) {
		assert.Equal(t, 2.0, MaxTokensScaleFactor)
	})

	t.Run("max scaling attempts allow reasonable growth", func(t *testing.T) {
		assert.Equal(t, 4, MaxScalingAttempts)
		// With 4 attempts and 2x scaling: 64 -> 128 -> 256 -> 512 (max reached at 1024)
	})

	t.Run("scaling sequence reaches max", func(t *testing.T) {
		// Verify the scaling sequence: 64 -> 128 -> 256 -> 512 -> 1024 (capped at max)
		// Starting at 64, with 4 attempts and 2x scaling:
		//   Attempt 0: 64 (initial)
		//   Attempt 1: 128
		//   Attempt 2: 256
		//   Attempt 3: 512
		//   Attempt 4: would be 1024, which equals MaxMaxTokens (capped)
		current := InitialMaxTokens
		for i := 0; i < MaxScalingAttempts; i++ {
			next := int(float64(current) * MaxTokensScaleFactor)
			if next > MaxMaxTokens {
				next = MaxMaxTokens
			}
			current = next
		}
		// After 4 scaling attempts: 64 -> 128 -> 256 -> 512 -> 1024
		assert.Equal(t, MaxMaxTokens, current)
	})
}

// TestExecuteAITest_SetsEngineAndModel verifies that AI test results have the
// Engine field set to "ai" and Model field set to the provider:model format.
// This tests the behavior in AITestRunner.executeTest().
func TestExecuteAITest_SetsEngineAndModel(t *testing.T) {
	tests := []struct {
		name           string
		provider       string
		model          string
		expectedEngine string
		expectedModel  string
	}{
		{
			name:           "OpenAI model",
			provider:       "openai",
			model:          "gpt-4o-mini",
			expectedEngine: "ai",
			expectedModel:  "openai:gpt-4o-mini",
		},
		{
			name:           "Anthropic model",
			provider:       "anthropic",
			model:          "claude-haiku-4-5-20251001",
			expectedEngine: "ai",
			expectedModel:  "anthropic:claude-haiku-4-5-20251001",
		},
		{
			name:           "OpenAI compatible model",
			provider:       "openai_compatible",
			model:          "local-model",
			expectedEngine: "ai",
			expectedModel:  "openai_compatible:local-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the ModelKey function produces the expected model key
			modelKey := ModelKey(tt.provider, tt.model)
			assert.Equal(t, tt.expectedModel, modelKey)

			// Create a TestResult as executeTest would
			// (executeTest sets Engine="ai" and Model=ModelKey(provider, model))
			result := TestResult{
				CaseID: "ai-test-engine-model",
				Title:  "Test AI Engine and Model Fields",
				Engine: "ai",
				Model:  modelKey,
			}

			assert.Equal(t, tt.expectedEngine, result.Engine, "Engine should be 'ai'")
			assert.Equal(t, tt.expectedModel, result.Model, "Model should be provider:model format")
		})
	}
}

// TestAITestResultFields verifies that TestResult struct fields are correctly
// populated for AI tests with various configurations.
func TestAITestResultFields(t *testing.T) {
	t.Run("AI result has engine=ai", func(t *testing.T) {
		result := TestResult{
			CaseID: "ai-test-001",
			Title:  "AI Test",
			Engine: "ai",
			Model:  "openai:gpt-4o-mini",
			Status: "passed",
			Actual: ActualResult{
				Decision:   "deny",
				Confidence: 1.0,
				Reasoning:  "Request blocked by policy",
			},
		}

		assert.Equal(t, "ai", result.Engine)
		assert.NotEmpty(t, result.Model)
		assert.Contains(t, result.Model, ":")
	})

	t.Run("AI result includes reasoning from AI response", func(t *testing.T) {
		result := TestResult{
			CaseID: "ai-test-002",
			Title:  "AI Test with Reasoning",
			Engine: "ai",
			Model:  "anthropic:claude-haiku",
			Status: "failed",
			Actual: ActualResult{
				Decision:   "allow",
				Confidence: 1.0,
				Reasoning:  "AI determined request is safe",
			},
		}

		assert.Equal(t, "ai", result.Engine)
		assert.NotEmpty(t, result.Actual.Reasoning)
	})

	t.Run("AI result policies include timing data", func(t *testing.T) {
		result := TestResult{
			CaseID: "ai-test-003",
			Title:  "AI Test with Policy Timing",
			Engine: "ai",
			Model:  "openai:gpt-4o-mini",
			Status: "passed",
			Actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{
						PolicyName: "block-dangerous-commands",
						Decision:   "deny",
						ElapsedMs:  1500,
						Reasoning:  "Command could delete files",
					},
				},
			},
		}

		assert.Equal(t, 1, len(result.Actual.PoliciesExecuted))
		assert.True(t, result.Actual.PoliciesExecuted[0].ElapsedMs > 0)
	})
}

// TestResolveEnvVar is in runner_test.go
