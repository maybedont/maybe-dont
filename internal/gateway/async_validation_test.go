package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestAsyncValidation_AllAuditOnlyReturnsImmediately verifies that when all policies
// are audit_only, EvaluateToolCall returns immediately with AsyncCompletion channel.
func TestAsyncValidation_AllAuditOnlyReturnsImmediately(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create mock client with slow response to simulate real AI call
	mockClient := NewMockAIClient()
	mockClient.DefaultDelay = 500 * time.Millisecond
	mockClient.DefaultResponse = AIResponse{Allowed: true, Message: "Approved"}

	engine := &AIPolicyEngine{
		apiKey:              "test-key",
		model:               "gpt-4o-mini",
		maxRuleEvaluationMs: 30000,
		client:              mockClient, // Inject mock
	}
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	// Load only audit_only policies
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

	// Measure how long EvaluateToolCall takes to return
	startTime := time.Now()
	results, err := engine.EvaluateToolCall(context.Background(), createTestToolRequest("test_tool"), nil)
	returnDuration := time.Since(startTime)

	require.NoError(t, err)

	// Should return quickly (much less than the 500ms delay per policy)
	assert.Less(t, returnDuration, 100*time.Millisecond, "Should return immediately without waiting for AI")

	// Should be allowed (audit_only policies don't affect the decision)
	assert.True(t, results.Allowed)

	// Should have async completion channel
	assert.NotNil(t, results.AsyncCompletion, "Should have async completion channel")

	// AIDetails should be nil (comes via async channel)
	assert.Nil(t, results.AIDetails, "AIDetails should come via async channel")

	// Wait for async completion
	completion := <-results.AsyncCompletion

	// Should have AIDetails now
	assert.NotNil(t, completion.AIDetails)
	assert.Equal(t, "allow", completion.AIDetails.Action)
	assert.Equal(t, int64(0), completion.AIDetails.BlockedMs, "Blocked time should be 0 for audit_only")
	assert.Len(t, completion.AIDetails.Results, 2)
}

// TestAsyncValidation_EnabledPoliciesBlock verifies that enabled policies
// block until they complete, while audit_only continue in background.
func TestAsyncValidation_EnabledPoliciesBlock(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create mock client with configurable delay
	mockClient := NewMockAIClient()
	mockClient.DefaultDelay = 200 * time.Millisecond
	mockClient.DefaultResponse = AIResponse{Allowed: true, Message: "Approved"}

	engine := &AIPolicyEngine{
		apiKey:              "test-key",
		model:               "gpt-4o-mini",
		maxRuleEvaluationMs: 30000,
		client:              mockClient,
	}
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	// Load one enabled policy
	policies := []config.AIPolicy{
		{
			Name:   "enabled_policy",
			Prompt: "Check: %s",
			Action: config.PolicyActionDeny,
			Mode:   config.PolicyModeEnabled,
		},
	}
	err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
	require.NoError(t, err)

	// Measure how long EvaluateToolCall takes
	startTime := time.Now()
	results, err := engine.EvaluateToolCall(context.Background(), createTestToolRequest("test_tool"), nil)
	returnDuration := time.Since(startTime)

	require.NoError(t, err)

	// Should block for at least the delay time (enabled policy must complete)
	assert.GreaterOrEqual(t, returnDuration, 180*time.Millisecond, "Should block waiting for enabled policy")

	// Should be allowed (mock returns allowed)
	assert.True(t, results.Allowed)

	// Should NOT have async completion (all policies completed synchronously)
	assert.Nil(t, results.AsyncCompletion, "Should not have async completion when all enabled")

	// Should have AIDetails populated
	assert.NotNil(t, results.AIDetails)
	assert.Equal(t, "allow", results.AIDetails.Action)
	assert.Greater(t, results.AIDetails.BlockedMs, int64(0), "Should have blocked time for enabled policy")
}

// TestAsyncValidation_MixedModePolicies verifies behavior with both enabled and audit_only policies.
func TestAsyncValidation_MixedModePolicies(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create mock client
	mockClient := NewMockAIClient()
	mockClient.DefaultDelay = 100 * time.Millisecond
	mockClient.DefaultResponse = AIResponse{Allowed: true, Message: "Approved"}

	engine := &AIPolicyEngine{
		apiKey:              "test-key",
		model:               "gpt-4o-mini",
		maxRuleEvaluationMs: 30000,
		client:              mockClient,
	}
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	// Load mixed mode policies
	policies := []config.AIPolicy{
		{
			Name:   "enabled_policy",
			Prompt: "Check: %s",
			Action: config.PolicyActionDeny,
			Mode:   config.PolicyModeEnabled,
		},
		{
			Name:   "audit_only_policy",
			Prompt: "Check: %s",
			Action: config.PolicyActionDeny,
			Mode:   config.PolicyModeAuditOnly,
		},
	}
	err = engine.LoadPolicies(policies, config.PolicyModeEnabled)
	require.NoError(t, err)

	results, err := engine.EvaluateToolCall(context.Background(), createTestToolRequest("test_tool"), nil)
	require.NoError(t, err)

	// Should be allowed
	assert.True(t, results.Allowed)

	// May have async completion if audit_only policy is still running
	// (depends on timing, so we just check for correctness)
	if results.AsyncCompletion != nil {
		completion := <-results.AsyncCompletion
		assert.NotNil(t, completion.AIDetails)
		assert.Len(t, completion.AIDetails.Results, 2)
	} else {
		// All policies completed synchronously
		assert.NotNil(t, results.AIDetails)
	}
}

// TestAsyncValidation_AuditContextFinalizeAsync verifies that FinalizeAsync
// waits for async completion channels before finalizing.
func TestAsyncValidation_AuditContextFinalizeAsync(t *testing.T) {
	// Create an audit context
	audit := NewAuditContext("test_tool", "client", "original_tool", "session-123", "1.2.3.4", "req-123")

	// Create a completion channel with delayed results
	completionChan := make(chan AsyncCompletion, 1)

	// Register async completion
	audit.SetRequestAIResultsAsync(completionChan)

	// Start a goroutine to send results after a delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		completionChan <- AsyncCompletion{
			AIDetails: &AuditAIResult{
				Action:       "allow",
				BlockedMs:    0,
				EvaluationMs: 200,
				Results: []AuditAIRuleResult{
					{Rule: "test_rule", Action: "deny", Mode: "audit_only", Result: "allow"},
				},
			},
			EvaluationMs: 200,
		}
	}()

	// Verify async work is pending
	assert.True(t, audit.HasAsyncWork())

	// Call FinalizeAsync and measure time
	startTime := time.Now()
	entry := audit.FinalizeAsync()
	waitDuration := time.Since(startTime)

	// Should have waited for the async results
	assert.GreaterOrEqual(t, waitDuration, 180*time.Millisecond, "Should wait for async completion")

	// Should have the AI results populated
	assert.NotNil(t, entry.RequestValidation)
	assert.NotNil(t, entry.RequestValidation.AI)
	assert.Equal(t, "allow", entry.RequestValidation.AI.Action)
	assert.Len(t, entry.RequestValidation.AI.Results, 1)
}

// TestAsyncValidation_ValidationChainPropagatesAsync verifies that the validation
// chain properly propagates AsyncCompletion from handlers.
func TestAsyncValidation_ValidationChainPropagatesAsync(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create mock client
	mockClient := NewMockAIClient()
	mockClient.DefaultDelay = 100 * time.Millisecond
	mockClient.DefaultResponse = AIResponse{Allowed: true, Message: "Approved"}

	// Create AI engine with all audit_only policies
	engine := &AIPolicyEngine{
		apiKey:              "test-key",
		model:               "gpt-4o-mini",
		maxRuleEvaluationMs: 30000,
		client:              mockClient,
	}
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

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

	// Create handler and chain
	handler := NewToolAIValidationHandler(sessionLogger, engine)
	chain := NewToolValidationChain(handler)

	// Handle through the chain
	results, err := chain.Handle(context.Background(), createTestToolRequest("test_tool"))
	require.NoError(t, err)

	// Should be allowed
	assert.True(t, results.Allowed)

	// Should have async completion propagated from handler
	assert.NotNil(t, results.AsyncCompletion, "Chain should propagate async completion from handler")

	// Wait for async completion
	completion := <-results.AsyncCompletion
	assert.NotNil(t, completion.AIDetails)
}

// TestAsyncCompletion_ChannelDeliversCorrectResults verifies the async completion
// channel delivers the correct AI evaluation results.
func TestAsyncCompletion_ChannelDeliversCorrectResults(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	mockClient := NewMockAIClient()
	mockClient.DefaultDelay = 50 * time.Millisecond
	mockClient.DefaultResponse = AIResponse{Allowed: false, Message: "Suspicious activity"}

	engine := &AIPolicyEngine{
		apiKey:              "test-key",
		model:               "gpt-4o-mini",
		maxRuleEvaluationMs: 30000,
		client:              mockClient,
	}
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	policies := []config.AIPolicy{
		{
			Name:   "audit_security_policy",
			Prompt: "Check: %s",
			Action: config.PolicyActionDeny,
			Mode:   config.PolicyModeAuditOnly,
		},
	}
	err = engine.LoadPolicies(policies, config.PolicyModeAuditOnly)
	require.NoError(t, err)

	results, err := engine.EvaluateToolCall(context.Background(), createTestToolRequest("test_tool"), nil)
	require.NoError(t, err)

	// Should be allowed (audit_only doesn't block)
	assert.True(t, results.Allowed)
	require.NotNil(t, results.AsyncCompletion)

	// Wait for async completion
	completion := <-results.AsyncCompletion

	// Verify the AI details are correct
	require.NotNil(t, completion.AIDetails)
	assert.Equal(t, "allow", completion.AIDetails.Action, "Overall action should be allow for audit_only")
	assert.Len(t, completion.AIDetails.Results, 1)

	// The individual rule result should show "deny" because AI returned allowed=false for a deny rule
	ruleResult := completion.AIDetails.Results[0]
	assert.Equal(t, "audit_security_policy", ruleResult.Rule)
	assert.Equal(t, "deny", ruleResult.Action) // The policy's configured action
	assert.Equal(t, "audit_only", ruleResult.Mode)
	// Result is "deny" because AI said allowed=false for a deny policy
	assert.Equal(t, "deny", ruleResult.Result)
}

// TestAsyncValidation_ResponseEngine_AllAuditOnly verifies async behavior
// for the AI response validation engine.
func TestAsyncValidation_ResponseEngine_AllAuditOnly(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	mockClient := NewMockAIClient()
	mockClient.DefaultDelay = 200 * time.Millisecond
	mockClient.DefaultResponse = AIResponse{Allowed: true, Message: "Safe response"}

	engine := &AIResponsePolicyEngine{
		apiKey:              "test-key",
		model:               "gpt-4o-mini",
		maxRuleEvaluationMs: 30000,
		client:              mockClient,
	}
	err := InitAIResponsePolicyEngine(context.Background(), sessionLogger, engine)
	require.NoError(t, err)

	policies := []config.AIResponsePolicy{
		{
			Name:   "audit_response_policy",
			Prompt: "Check response: %s",
			Action: config.PolicyActionDeny,
			Mode:   config.PolicyModeAuditOnly,
		},
	}
	err = engine.LoadPolicies(policies, config.PolicyModeAuditOnly)
	require.NoError(t, err)

	// Create test response
	req := createTestToolRequest("test_tool")
	toolResult := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: "Test response content"},
		},
		IsError: false,
	}

	// Should return immediately for audit_only
	startTime := time.Now()
	results, err := engine.EvaluateResponse(context.Background(), req, toolResult, nil)
	returnDuration := time.Since(startTime)

	require.NoError(t, err)

	// Should return quickly
	assert.Less(t, returnDuration, 100*time.Millisecond, "Should return immediately for audit_only")

	// Should be allowed
	assert.True(t, results.Allowed)

	// Should have async completion
	assert.NotNil(t, results.AsyncCompletion)

	// Wait for completion
	completion := <-results.AsyncCompletion
	assert.NotNil(t, completion.AIDetails)
	assert.Equal(t, "allow", completion.AIDetails.Action)
}
