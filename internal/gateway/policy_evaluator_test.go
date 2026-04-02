package gateway

import (
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestPolicyEvaluator_EvaluateToolCall_NoEngines verifies that when no engines
// are configured, evaluation returns allowed=true with an informational message.
func TestPolicyEvaluator_EvaluateToolCall_NoEngines(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	evaluator := &PolicyEvaluator{
		Logger: logger,
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "test_tool"

	results := evaluator.EvaluateToolCall(t.Context(), req)

	assert.True(t, results.Allowed)
	assert.Equal(t, "No validation policies configured", results.Message)
	assert.Empty(t, results.Results)
}

// TestPolicyEvaluator_EvaluateToolCall_CELDeny verifies that a CEL deny
// result propagates through to the final ValidationResults.
func TestPolicyEvaluator_EvaluateToolCall_CELDeny(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	engine := newTestCELEngineWithDenyRule(t)

	evaluator := &PolicyEvaluator{
		CELEngine: engine,
		Logger:    logger,
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "test_tool"

	results := evaluator.EvaluateToolCall(t.Context(), req)

	assert.False(t, results.Allowed)
	assert.False(t, results.AuditModeBypass)
	assert.Equal(t, 1, results.DenyCount)
}

// TestPolicyEvaluator_EvaluateToolCall_AuditModeBypassClearedOnDeny verifies
// that AuditModeBypass is cleared when an enforced deny overrides it.
func TestPolicyEvaluator_EvaluateToolCall_AuditModeBypassClearedOnDeny(t *testing.T) {
	// We need a CEL engine with an enforced deny rule — the audit-only flag
	// should be cleared because the final result is denied.
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	engine := newTestCELEngineWithDenyRule(t)

	evaluator := &PolicyEvaluator{
		CELEngine: engine,
		Logger:    logger,
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "test_tool"

	results := evaluator.EvaluateToolCall(t.Context(), req)

	// When denied, AuditModeBypass should be cleared
	assert.False(t, results.Allowed)
	assert.False(t, results.AuditModeBypass)
}

// TestPolicyEvaluator_EvaluateToolCall_AuditModeDenyBypassed verifies
// that AuditModeBypass is set when a deny rule is in audit_only mode.
func TestPolicyEvaluator_EvaluateToolCall_AuditModeDenyBypassed(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	engine := newTestCELEngineWithAuditOnlyDenyRule(t)

	evaluator := &PolicyEvaluator{
		CELEngine: engine,
		Logger:    logger,
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "test_tool"

	results := evaluator.EvaluateToolCall(t.Context(), req)

	assert.True(t, results.Allowed)
	assert.True(t, results.AuditModeBypass)
}

// TestPolicyEvaluator_EvaluateCLICommand_CELDeny verifies CLI command
// evaluation routes through correctly using cli_expression.
func TestPolicyEvaluator_EvaluateCLICommand_CELDeny(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	engine := newTestCELEngineWithCLIDenyRule(t)

	evaluator := &PolicyEvaluator{
		CELEngine: engine,
		Logger:    logger,
	}

	req := &CLIValidationRequest{
		Command:   "gh",
		Arguments: []string{"repo", "delete"},
	}

	results := evaluator.EvaluateCLICommand(t.Context(), req)

	assert.False(t, results.Allowed)
	assert.Equal(t, 1, results.DenyCount)
}

// TestPolicyEvaluator_EvaluateCLICommand_NoEngines verifies that when no engines
// are configured, CLI evaluation returns allowed=true.
func TestPolicyEvaluator_EvaluateCLICommand_NoEngines(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	evaluator := &PolicyEvaluator{
		Logger: logger,
	}

	req := &CLIValidationRequest{
		Command:   "gh",
		Arguments: []string{"repo", "list"},
	}

	results := evaluator.EvaluateCLICommand(t.Context(), req)

	assert.True(t, results.Allowed)
	assert.Equal(t, "No validation policies configured", results.Message)
}

// TestPolicyEvaluator_EvaluateResponse_Allowed verifies response phase
// evaluation delegates to the ResponseValidationChain.
func TestPolicyEvaluator_EvaluateResponse_Allowed(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	// Create a response chain with no handlers — should return allowed
	chain := NewResponseValidationChain(logger)

	evaluator := &PolicyEvaluator{
		ResponseChain: chain,
		Logger:        logger,
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "test_tool"

	result := &mcp.CallToolResult{}
	result.Content = []mcp.Content{
		mcp.NewTextContent("some output"),
	}

	resp, err := evaluator.EvaluateResponse(t.Context(), req, result)
	require.NoError(t, err)

	assert.True(t, resp.Allowed)
}

// TestPolicyEvaluator_EvaluateResponse_NilChain verifies graceful handling
// when no response validation chain is configured.
func TestPolicyEvaluator_EvaluateResponse_NilChain(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	evaluator := &PolicyEvaluator{
		Logger: logger,
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "test_tool"

	result := &mcp.CallToolResult{}

	resp, err := evaluator.EvaluateResponse(t.Context(), req, result)
	require.NoError(t, err)

	assert.True(t, resp.Allowed)
	assert.Equal(t, "No response validation configured", resp.Message)
}

// TestWriteAsyncAuditCompletion verifies the async audit goroutine writes
// an entry when completion arrives.
func TestWriteAsyncAuditCompletion(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	writer := &mockAuditWriter{}

	completionCh := make(chan AsyncCompletion, 1)
	completionCh <- AsyncCompletion{
		AIDetails:    &AuditAIResult{},
		EvaluationMs: 100,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	WriteAsyncAuditCompletion(
		writer,
		logger,
		"test-request-id",
		completionCh,
		func(completion AsyncCompletion) *AuditEntry {
			return &AuditEntry{
				Source: "test",
				UpstreamRequest: UpstreamRequestInfo{
					RequestID: "test-request-id-async",
				},
				RequestValidation: &AuditValidationInfo{
					AI: completion.AIDetails,
				},
				DurationMs: completion.EvaluationMs,
			}
		},
		func() { wg.Done() },
	)

	// Wait for async goroutine to complete
	wg.Wait()

	entries := writer.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "test", entries[0].Source)
	assert.Equal(t, "test-request-id-async", entries[0].UpstreamRequest.RequestID)
}

// TestWriteAsyncAuditCompletion_NilAIDetails verifies no entry is written
// when the completion has nil AIDetails.
func TestWriteAsyncAuditCompletion_NilAIDetails(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	writer := &mockAuditWriter{}

	completionCh := make(chan AsyncCompletion, 1)
	completionCh <- AsyncCompletion{
		AIDetails:    nil,
		EvaluationMs: 50,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	WriteAsyncAuditCompletion(
		writer,
		logger,
		"test-request-id",
		completionCh,
		func(completion AsyncCompletion) *AuditEntry {
			return &AuditEntry{
				Source: "test",
			}
		},
		func() { wg.Done() },
	)

	wg.Wait()

	entries := writer.getEntries()
	assert.Empty(t, entries)
}

// TestWriteAsyncAuditCompletion_NilWriter verifies that a nil AuditWriter
// does not cause a panic when a valid completion arrives.
func TestWriteAsyncAuditCompletion_NilWriter(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	completionCh := make(chan AsyncCompletion, 1)
	completionCh <- AsyncCompletion{
		AIDetails:    &AuditAIResult{Action: "allow", EvaluationMs: 200},
		EvaluationMs: 200,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Should not panic with nil writer
	WriteAsyncAuditCompletion(
		nil, // nil writer
		logger,
		"test-request-id",
		completionCh,
		func(completion AsyncCompletion) *AuditEntry {
			return &AuditEntry{
				Source: "test",
				RequestValidation: &AuditValidationInfo{
					AI: completion.AIDetails,
				},
			}
		},
		func() { wg.Done() },
	)

	wg.Wait()
	// If we reach here without panic, the test passes
}

// newTestCELEngineWithCLIDenyRule creates a CEL engine with a deny rule
// that uses cli_expression to match any CLI command.
func newTestCELEngineWithCLIDenyRule(t *testing.T) *CELPolicyEngine {
	t.Helper()

	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	engine, err := NewCELPolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)

	rules := []config.Policy{
		{
			Name:          "deny-all-cli",
			CLIExpression: "true",
			Action:        config.PolicyActionDeny,
			Message:       "Denied by CLI test rule",
		},
	}
	err = engine.LoadPolicies(rules, config.PolicyModeEnforce)
	require.NoError(t, err)

	return engine
}
