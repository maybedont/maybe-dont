package gateway

import (
	"context"
	"testing"

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
