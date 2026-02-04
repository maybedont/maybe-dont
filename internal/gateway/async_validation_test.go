package gateway

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// createAsyncTestMock creates a mock AI provider client configured for async validation tests.
// It returns an AICompletionResult with the JSON-encoded AIResponse.
func createAsyncTestMock(delay time.Duration, allowed bool, message string) *MockAIProviderClient {
	mock := NewMockAIProviderClient()
	mock.Delay = delay

	// Create an AIResponse and JSON-encode it
	resp := AIResponse{Allowed: allowed, Message: message}
	jsonBytes, _ := json.Marshal(resp)

	mock.Response = AICompletionResult{
		RawText:           string(jsonBytes),
		ParsedJSON:        jsonBytes,
		ProviderRequestID: "mock-request-id",
	}
	return mock
}

// createAsyncTestEngine creates an AIPolicyEngine configured for async validation tests.
func createAsyncTestEngine(mock *MockAIProviderClient, maxRuleEvaluationMs int) *AIPolicyEngine {
	cfg := &config.Config{}
	cfg.Validation.AI.Model = "gpt-4o-mini"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Provider = "openai"

	return &AIPolicyEngine{
		cfg:                 cfg,
		maxRuleEvaluationMs: maxRuleEvaluationMs,
		providerClient:      mock,
	}
}

// createAsyncTestResponseEngine creates an AIResponsePolicyEngine configured for async validation tests.
func createAsyncTestResponseEngine(mock *MockAIProviderClient, maxRuleEvaluationMs int) *AIResponsePolicyEngine {
	cfg := &config.Config{}
	cfg.Validation.AI.Model = "gpt-4o-mini"
	cfg.Validation.AI.APIKey = "test-key"
	cfg.Validation.AI.Provider = "openai"

	return &AIResponsePolicyEngine{
		cfg:                 cfg,
		maxRuleEvaluationMs: maxRuleEvaluationMs,
		providerClient:      mock,
	}
}

// TestAsyncValidation_AllAuditOnlyReturnsImmediately verifies that when all policies
// are audit_only, EvaluateToolCall returns immediately with AsyncCompletion channel.
func TestAsyncValidation_AllAuditOnlyReturnsImmediately(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create mock client with slow response to simulate real AI call
	mockClient := createAsyncTestMock(500*time.Millisecond, true, "Approved")
	engine := createAsyncTestEngine(mockClient, 30000)
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
	mockClient := createAsyncTestMock(200*time.Millisecond, true, "Approved")
	engine := createAsyncTestEngine(mockClient, 30000)
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	// Load one enabled policy
	policies := []config.AIPolicy{
		{
			Name:   "enabled_policy",
			Prompt: "Check: %s",
			Action: config.PolicyActionDeny,
			// Mode not set - can block
		},
	}
	err = engine.LoadPolicies(policies, "")
	require.NoError(t, err)

	// Create blocking budget (required for enabled policies)
	budget := NewBlockingBudget(90000) // 90 second budget

	// Measure how long EvaluateToolCall takes
	startTime := time.Now()
	results, err := engine.EvaluateToolCall(context.Background(), createTestToolRequest("test_tool"), budget)
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
	mockClient := createAsyncTestMock(100*time.Millisecond, true, "Approved")
	engine := createAsyncTestEngine(mockClient, 30000)
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	// Load mixed mode policies
	policies := []config.AIPolicy{
		{
			Name:   "enabled_policy",
			Prompt: "Check: %s",
			Action: config.PolicyActionDeny,
			// Mode not set - can block
		},
		{
			Name:   "audit_only_policy",
			Prompt: "Check: %s",
			Action: config.PolicyActionDeny,
			Mode:   config.PolicyModeAuditOnly,
		},
	}
	err = engine.LoadPolicies(policies, "")
	require.NoError(t, err)

	// Create blocking budget (required for enabled policies)
	budget := NewBlockingBudget(90000) // 90 second budget

	results, err := engine.EvaluateToolCall(context.Background(), createTestToolRequest("test_tool"), budget)
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

	// Create mock client and engine
	mockClient := createAsyncTestMock(100*time.Millisecond, true, "Approved")
	engine := createAsyncTestEngine(mockClient, 30000)
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

	mockClient := createAsyncTestMock(50*time.Millisecond, false, "Suspicious activity")
	engine := createAsyncTestEngine(mockClient, 30000)
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

	mockClient := createAsyncTestMock(200*time.Millisecond, true, "Safe response")
	engine := createAsyncTestResponseEngine(mockClient, 30000)
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

// TestAsyncValidation_FullChainWithCELAndAI tests the complete validation flow
// with both CEL and AI handlers in the chain, ensuring AsyncCompletion propagates
// through to the audit context correctly.
func TestAsyncValidation_FullChainWithCELAndAI(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create CEL engine with a simple allow-all policy
	celEngine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)
	err = celEngine.LoadPolicies([]config.Policy{
		{
			Name:       "allow_all",
			Expression: "true",
			Action:     config.PolicyActionAllow,
		},
	}, "")
	require.NoError(t, err)

	// Create AI engine with audit_only policy
	mockClient := createAsyncTestMock(100*time.Millisecond, true, "Approved")
	aiEngine := createAsyncTestEngine(mockClient, 30000)
	err = InitAIPolicyEngine(sessionLogger, aiEngine)
	require.NoError(t, err)
	err = aiEngine.LoadPolicies([]config.AIPolicy{
		{
			Name:   "audit_policy",
			Prompt: "Check: %s",
			Action: config.PolicyActionDeny,
			Mode:   config.PolicyModeAuditOnly,
		},
	}, config.PolicyModeAuditOnly)
	require.NoError(t, err)

	// Create handlers and chain (same order as real gateway: CEL first, then AI)
	celHandler := NewToolCELValidationHandler(sessionLogger, celEngine)
	aiHandler := NewToolAIValidationHandler(sessionLogger, aiEngine)
	chain := NewToolValidationChain(celHandler, aiHandler)

	// Create blocking budget (required for AI validation)
	budget := NewBlockingBudget(90000)
	ctx := WithBlockingBudget(context.Background(), budget)

	// Handle through the chain
	results, err := chain.Handle(ctx, createTestToolRequest("test_tool"))
	require.NoError(t, err)

	// Verify both CEL and AI results are propagated
	assert.True(t, results.Allowed, "Should be allowed")
	assert.NotNil(t, results.RulesDetails, "Should have CEL rules details")
	assert.NotNil(t, results.AsyncCompletion, "Should have async completion from AI handler")

	// Now simulate what populateRequestValidationAudit does
	audit := NewAuditContext("client__test_tool", "client", "test_tool", "session-123", "1.2.3.4", "req-123")

	// This is the exact logic from populateRequestValidationAudit
	if results.RulesDetails != nil {
		audit.SetRequestValidationRules(results.RulesDetails)
	}
	if results.AIDetails != nil {
		audit.SetRequestValidationAI(results.AIDetails)
	}
	if results.AsyncCompletion != nil {
		audit.SetRequestAIResultsAsync(results.AsyncCompletion)
	}

	// Critical assertion: HasAsyncWork should return true
	assert.True(t, audit.HasAsyncWork(), "Audit context should have async work pending")

	// Call FinalizeAsync and verify it waits for AI results
	startTime := time.Now()
	entry := audit.FinalizeAsync()
	waitDuration := time.Since(startTime)

	// Should have waited for async results
	assert.GreaterOrEqual(t, waitDuration, 80*time.Millisecond, "Should wait for async AI completion")

	// Verify the audit entry has both CEL and AI results
	require.NotNil(t, entry.RequestValidation, "Should have request validation")
	assert.NotNil(t, entry.RequestValidation.CEL, "Should have CEL results")
	assert.NotNil(t, entry.RequestValidation.AI, "Should have AI results")
	assert.Len(t, entry.RequestValidation.AI.Results, 1, "Should have 1 AI rule result")
}

// TestAsyncValidation_PopulateAuditWithAsyncCompletion specifically tests that
// when ValidationResults has AsyncCompletion set, it gets propagated to the
// audit context and HasAsyncWork returns true.
func TestAsyncValidation_PopulateAuditWithAsyncCompletion(t *testing.T) {
	// Create a mock AsyncCompletion channel
	completionChan := make(chan AsyncCompletion, 1)

	// Create ValidationResults with AsyncCompletion set (simulating all audit_only AI)
	results := ValidationResults{
		Allowed:    true,
		AllowCount: 1,
		RulesDetails: &AuditRulesResult{
			Action:  "allow",
			Results: []AuditRulesRuleResult{},
		},
		AIDetails:       nil, // nil because it's async
		AsyncCompletion: completionChan,
	}

	// Create audit context
	audit := NewAuditContext("client__test_tool", "client", "test_tool", "session-123", "1.2.3.4", "req-123")

	// Simulate populateRequestValidationAudit logic
	if results.RulesDetails != nil {
		audit.SetRequestValidationRules(results.RulesDetails)
	}
	if results.AIDetails != nil {
		audit.SetRequestValidationAI(results.AIDetails)
	}
	if results.AsyncCompletion != nil {
		audit.SetRequestAIResultsAsync(results.AsyncCompletion)
	}

	// The key assertion: HasAsyncWork MUST return true
	assert.True(t, audit.HasAsyncWork(), "HasAsyncWork must return true when AsyncCompletion is set")

	// Send completion in background
	go func() {
		completionChan <- AsyncCompletion{
			AIDetails: &AuditAIResult{
				Action:       "allow",
				EvaluationMs: 100,
				Results: []AuditAIRuleResult{
					{Rule: "test_rule", Action: "deny", Mode: "audit_only", Result: "allow"},
				},
			},
		}
	}()

	// FinalizeAsync should wait and populate AI results
	entry := audit.FinalizeAsync()

	// Verify AI results are present
	require.NotNil(t, entry.RequestValidation)
	require.NotNil(t, entry.RequestValidation.AI, "AI results should be populated after FinalizeAsync")
	assert.Equal(t, "allow", entry.RequestValidation.AI.Action)
}

// TestAsyncValidation_WriteAuditLogBehavior tests that the audit log writing
// behavior correctly handles async completion - ensuring it waits for async
// results before writing the entry.
func TestAsyncValidation_WriteAuditLogBehavior(t *testing.T) {
	// This test simulates the writeAuditLog closure behavior in HandleToolCall

	// Create audit context with async completion
	audit := NewAuditContext("client__test_tool", "client", "test_tool", "session-123", "1.2.3.4", "req-123")

	// Set up CEL results (synchronous)
	audit.SetRequestValidationRules(&AuditRulesResult{
		Action:  "allow",
		Results: []AuditRulesRuleResult{},
	})

	// Set up async AI completion
	completionChan := make(chan AsyncCompletion, 1)
	audit.SetRequestAIResultsAsync(completionChan)

	// Simulate AI completion arriving after a delay
	go func() {
		time.Sleep(150 * time.Millisecond)
		completionChan <- AsyncCompletion{
			AIDetails: &AuditAIResult{
				Action:       "allow",
				EvaluationMs: 150,
				Results: []AuditAIRuleResult{
					{Rule: "audit_rule", Action: "deny", Mode: "audit_only", Result: "allow"},
				},
			},
		}
	}()

	// Simulate writeAuditLog behavior
	var entry *AuditEntry
	var writeDuration time.Duration

	if audit.HasAsyncWork() {
		// Async path - should wait for completion
		startTime := time.Now()
		entry = audit.FinalizeAsync()
		writeDuration = time.Since(startTime)
	} else {
		// Sync path - would NOT have AI results
		t.Fatal("HasAsyncWork returned false - this is the bug we're testing for")
	}

	// Verify we took the async path and waited
	assert.GreaterOrEqual(t, writeDuration, 100*time.Millisecond, "Should have waited for async completion")

	// Verify the entry has complete results
	require.NotNil(t, entry.RequestValidation)
	assert.NotNil(t, entry.RequestValidation.CEL, "Should have CEL results")
	assert.NotNil(t, entry.RequestValidation.AI, "Should have AI results from async completion")
	assert.Len(t, entry.RequestValidation.AI.Results, 1)
}

// TestAsyncValidation_AllAuditOnlyWritesAuditLog is an end-to-end integration test
// that verifies when ALL policies (both CEL and AI) are set to audit_only mode,
// the audit log entry is still written to the file with complete results.
// This test was added to verify the fix for a bug where audit logs were not written
// when all policies were audit_only.
func TestAsyncValidation_AllAuditOnlyWritesAuditLog(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create a temporary file for audit log
	tmpDir := t.TempDir()
	auditPath := tmpDir + "/audit.log"

	// Create audit writer
	auditWriter, err := NewJSONLAuditWriter(auditPath, "", config.RotationConfig{
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Compress:   false,
	}, "all")
	require.NoError(t, err)
	defer func() { _ = auditWriter.Close() }()

	// Create CEL engine with audit_only policy
	celEngine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)
	err = celEngine.LoadPolicies([]config.Policy{
		{
			Name:       "cel_audit_policy",
			Expression: "true", // Always matches
			Action:     config.PolicyActionDeny,
			Mode:       config.PolicyModeAuditOnly, // audit_only - should not block
		},
	}, config.PolicyModeAuditOnly) // Default mode is also audit_only
	require.NoError(t, err)

	// Create AI engine with audit_only policy
	mockClient := createAsyncTestMock(50*time.Millisecond, false, "Would deny")
	aiEngine := createAsyncTestEngine(mockClient, 30000)
	err = InitAIPolicyEngine(sessionLogger, aiEngine)
	require.NoError(t, err)
	err = aiEngine.LoadPolicies([]config.AIPolicy{
		{
			Name:   "ai_audit_policy",
			Prompt: "Check security: %s",
			Action: config.PolicyActionDeny,
			Mode:   config.PolicyModeAuditOnly, // audit_only - should not block
		},
	}, config.PolicyModeAuditOnly) // Default mode is also audit_only
	require.NoError(t, err)

	// Create handlers and chain
	celHandler := NewToolCELValidationHandler(sessionLogger, celEngine)
	aiHandler := NewToolAIValidationHandler(sessionLogger, aiEngine)
	chain := NewToolValidationChain(celHandler, aiHandler)

	// Create blocking budget
	budget := NewBlockingBudget(90000)
	ctx := WithBlockingBudget(context.Background(), budget)

	// Create audit context
	audit := NewAuditContext("client__test_tool", "client", "test_tool", "session-123", "1.2.3.4", "req-123")

	// Handle through the chain
	results, err := chain.Handle(ctx, createTestToolRequest("test_tool"))
	require.NoError(t, err)

	// Should be allowed despite both policies saying deny (because audit_only)
	assert.True(t, results.Allowed, "Should be allowed - all policies are audit_only")

	// Populate audit context (same logic as gateway.populateRequestValidationAudit)
	if results.RulesDetails != nil {
		audit.SetRequestValidationRules(results.RulesDetails)
	}
	if results.AIDetails != nil {
		audit.SetRequestValidationAI(results.AIDetails)
	}
	if results.AsyncCompletion != nil {
		audit.SetRequestAIResultsAsync(results.AsyncCompletion)
	}

	// Set actions (simulating gateway behavior)
	audit.SetActions("deny", "allow", ActionReasonAuditMode)

	// CRITICAL: Verify HasAsyncWork returns true
	assert.True(t, audit.HasAsyncWork(), "HasAsyncWork MUST return true when AsyncCompletion is set")

	// Write audit log using the async path (same as gateway)
	var entry *AuditEntry
	if audit.HasAsyncWork() {
		// This is the async path - wait for completion then write
		entry = audit.FinalizeAsync()
	} else {
		t.Fatal("HasAsyncWork returned false - this is the bug we're testing for!")
	}

	// Write the entry
	written, err := auditWriter.Write(entry)
	require.NoError(t, err)
	assert.True(t, written, "Audit entry should have been written")

	// Read and verify the audit log file
	content, err := os.ReadFile(auditPath)
	require.NoError(t, err)

	contentStr := string(content)
	t.Logf("Audit log content:\n%s", contentStr)

	// Verify the file is not empty
	assert.NotEmpty(t, contentStr, "Audit log file should not be empty")

	// Verify CEL results are present
	assert.Contains(t, contentStr, `"cel"`, "Audit log should contain CEL results")
	assert.Contains(t, contentStr, `"cel_audit_policy"`, "Audit log should contain CEL policy name")

	// Verify AI results are present
	assert.Contains(t, contentStr, `"ai"`, "Audit log should contain AI results")
	assert.Contains(t, contentStr, `"ai_audit_policy"`, "Audit log should contain AI policy name")

	// Verify audit_only mode is recorded
	assert.Contains(t, contentStr, `"mode":"audit_only"`, "Audit log should record audit_only mode")

	// Verify actions are correct
	assert.Contains(t, contentStr, `"recommended_action":"deny"`, "Recommended action should be deny")
	assert.Contains(t, contentStr, `"action":"allow"`, "Actual action should be allow")
	assert.Contains(t, contentStr, `"action_reason":"audit_mode"`, "Action reason should be audit_mode")

	// Parse and verify the JSON structure
	var auditEntry AuditEntry
	err = json.Unmarshal(content, &auditEntry)
	require.NoError(t, err, "Audit log should be valid JSON")

	// Verify request validation structure
	require.NotNil(t, auditEntry.RequestValidation, "RequestValidation should not be nil")
	require.NotNil(t, auditEntry.RequestValidation.CEL, "CEL results should not be nil")
	require.NotNil(t, auditEntry.RequestValidation.AI, "AI results should not be nil")

	// Verify CEL rule results
	assert.Equal(t, "allow", auditEntry.RequestValidation.CEL.Action, "CEL action should be allow (audit_only bypassed)")
	assert.Len(t, auditEntry.RequestValidation.CEL.Results, 1, "Should have 1 CEL rule result")
	assert.Equal(t, "cel_audit_policy", auditEntry.RequestValidation.CEL.Results[0].Rule)
	assert.Equal(t, "audit_only", auditEntry.RequestValidation.CEL.Results[0].Mode)

	// Verify AI rule results
	assert.Equal(t, "allow", auditEntry.RequestValidation.AI.Action, "AI action should be allow (audit_only bypassed)")
	assert.Len(t, auditEntry.RequestValidation.AI.Results, 1, "Should have 1 AI rule result")
	assert.Equal(t, "ai_audit_policy", auditEntry.RequestValidation.AI.Results[0].Rule)
	assert.Equal(t, "audit_only", auditEntry.RequestValidation.AI.Results[0].Mode)
}
