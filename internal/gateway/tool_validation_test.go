package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// MockValidationHandler for testing chain behavior
type MockValidationHandler struct {
	name           string
	shouldAllow    bool
	shouldError    bool
	expectedResult ValidationResult
}

func (m *MockValidationHandler) HandleToolCall(context.Context, mcp.CallToolRequest) (ValidationResults, error) {
	if m.shouldError {
		return ValidationResults{}, errors.New("mock error")
	}

	results := ValidationResults{
		Results: []ValidationResult{m.expectedResult},
		Allowed: m.shouldAllow,
		Message: m.expectedResult.Message,
	}

	if m.shouldAllow {
		results.AllowCount = 1
	} else {
		results.DenyCount = 1
	}

	return results, nil
}

func TestValidationChain_HandlerComposition(t *testing.T) {
	// Create mock handlers
	allowHandler := &MockValidationHandler{
		name:        "allow-handler",
		shouldAllow: true,
		expectedResult: ValidationResult{
			PolicyName: "Allow Policy",
			PolicyType: "mock",
			Action:     config.PolicyActionAllow,
			Message:    "Allowed by mock handler",
		},
	}

	denyHandler := &MockValidationHandler{
		name:        "deny-handler",
		shouldAllow: false,
		expectedResult: ValidationResult{
			PolicyName: "Deny Policy",
			PolicyType: "mock",
			Action:     config.PolicyActionDeny,
			Message:    "Denied by mock handler",
		},
	}

	// Test chain with multiple handlers
	chain := NewToolValidationChain(allowHandler, denyHandler)

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "test_tool"},
	}

	results, err := chain.Handle(context.Background(), req)
	require.NoError(t, err)

	// Should aggregate results from both handlers
	assert.Len(t, results.Results, 2)
	assert.Equal(t, 1, results.AllowCount)
	assert.Equal(t, 1, results.DenyCount)

	// Check individual results
	foundAllow := false
	foundDeny := false
	for _, result := range results.Results {
		if result.PolicyName == "Allow Policy" {
			foundAllow = true
			assert.Equal(t, config.PolicyActionAllow, result.Action)
		}
		if result.PolicyName == "Deny Policy" {
			foundDeny = true
			assert.Equal(t, config.PolicyActionDeny, result.Action)
		}
	}
	assert.True(t, foundAllow)
	assert.True(t, foundDeny)
}

func TestValidationChain_ErrorHandling(t *testing.T) {
	// Create handlers where one will error
	errorHandler := &MockValidationHandler{
		name:        "error-handler",
		shouldError: true,
	}

	workingHandler := &MockValidationHandler{
		name:        "working-handler",
		shouldAllow: true,
		expectedResult: ValidationResult{
			PolicyName: "Working Policy",
			PolicyType: "mock",
			Action:     config.PolicyActionAllow,
			Message:    "Working handler succeeded",
		},
	}

	// Test chain with error handler
	chain := NewToolValidationChain(errorHandler, workingHandler)

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "test_tool"},
	}

	results, err := chain.Handle(context.Background(), req)

	// Should still get results from working handler
	assert.Error(t, err) // Should have error from errorHandler
	assert.Len(t, results.Results, 1)
	assert.Equal(t, 1, results.AllowCount)
	assert.Equal(t, 0, results.DenyCount)
	assert.Equal(t, "Working Policy", results.Results[0].PolicyName)
}

func TestValidationChain_RealHandlers(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create CEL engine with simple policies
	celEngine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)

	policies := []config.Policy{
		{
			Name:       "allow-read-tool",
			Expression: `request.method == "tools/call" && request.params.name == "read_file"`,
			Action:     config.PolicyActionAllow,
			Message:    "Allowed to call read_file",
		},
		{
			Name:       "deny-delete-tool",
			Expression: `request.method == "tools/call" && request.params.name == "delete_file"`,
			Action:     config.PolicyActionDeny,
			Message:    "delete_file is not allowed",
		},
	}

	err = celEngine.LoadPolicies(policies, "")
	require.NoError(t, err)

	// Create validation chain with CEL handler
	chain := NewToolValidationChain(
		NewToolCELValidationHandler(sessionLogger, celEngine),
	)

	tests := []struct {
		name          string
		req           mcp.CallToolRequest
		wantAllowed   bool
		wantCELAction string
		wantCELRules  int // Number of CEL rules evaluated
	}{
		{
			name: "read_file with CEL handler",
			req: mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params: mcp.CallToolParams{
					Name:      "read_file",
					Arguments: map[string]any{"target_file": "test.txt"},
				},
			},
			wantAllowed:   true,
			wantCELAction: "allow",
			wantCELRules:  2, // Both rules evaluated
		},
		{
			name: "delete_file with CEL handler",
			req: mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params: mcp.CallToolParams{
					Name:      "delete_file",
					Arguments: map[string]any{"target_file": "test.txt"},
				},
			},
			wantAllowed:   false,
			wantCELAction: "deny",
			wantCELRules:  2, // Early termination on deny, but both rules evaluated before match
		},
		{
			name: "unknown tool with CEL handler",
			req: mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params: mcp.CallToolParams{
					Name:      "unknown_tool",
					Arguments: map[string]any{"arg": "value"},
				},
			},
			wantAllowed:   true, // Default allow when no policies match
			wantCELAction: "allow",
			wantCELRules:  2, // Both rules evaluated, neither matched
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := chain.Handle(context.Background(), tt.req)
			require.NoError(t, err)

			assert.Equal(t, tt.wantAllowed, results.Allowed)

			// CEL results should be in RulesDetails now
			assert.NotNil(t, results.RulesDetails, "Should have rules details")
			assert.Equal(t, tt.wantCELAction, results.RulesDetails.Action, "Rules action should match")
			assert.Len(t, results.RulesDetails.Results, tt.wantCELRules, "Should have expected number of rules results")
		})
	}
}

func TestCELValidationHandler_Isolation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	engine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)

	// Load a simple policy
	policies := []config.Policy{
		{
			Name:       "allow-tools-call",
			Expression: `request.method == "tools/call"`,
			Action:     config.PolicyActionAllow,
			Message:    "Allowed to call tools",
		},
	}
	err = engine.LoadPolicies(policies, "")
	require.NoError(t, err)

	handler := NewToolCELValidationHandler(sessionLogger, engine)

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "any_tool"},
	}

	results, err := handler.HandleToolCall(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, results.Allowed)
	assert.Equal(t, 1, results.AllowCount)
	assert.Equal(t, 0, results.DenyCount)
	assert.Len(t, results.Results, 1)
	assert.Equal(t, "allow-tools-call", results.Results[0].PolicyName)
	assert.Equal(t, "cel", results.Results[0].PolicyType)
}

func TestValidationChain_EmptyChain(t *testing.T) {
	// Test behavior with no handlers
	chain := NewToolValidationChain()

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "test_tool"},
	}

	results, err := chain.Handle(context.Background(), req)
	require.NoError(t, err)

	// Empty chain should return empty results
	assert.Len(t, results.Results, 0)
	assert.Equal(t, 0, results.AllowCount)
	assert.Equal(t, 0, results.DenyCount)
}

// TestValidationChain_FailedOpenPropagation verifies that FailedOpen flag
// propagates through the validation chain correctly.
func TestValidationChain_FailedOpenPropagation(t *testing.T) {
	// Create a handler that sets FailedOpen
	failedOpenHandler := &MockValidationHandlerWithFlags{
		name:        "failed-open-handler",
		shouldAllow: true,
		failedOpen:  true,
	}

	workingHandler := &MockValidationHandler{
		name:        "working-handler",
		shouldAllow: true,
		expectedResult: ValidationResult{
			PolicyName: "Working Policy",
			PolicyType: "mock",
			Action:     config.PolicyActionAllow,
			Message:    "Working handler succeeded",
		},
	}

	chain := NewToolValidationChain(failedOpenHandler, workingHandler)

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "test_tool"},
	}

	results, err := chain.Handle(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, results.Allowed, "Should be allowed")
	assert.True(t, results.FailedOpen, "FailedOpen flag should propagate")
}

// TestValidationChain_AuditModeBypassPropagation verifies that AuditModeBypass flag
// and RecommendedAction propagate through the validation chain correctly.
func TestValidationChain_AuditModeBypassPropagation(t *testing.T) {
	// Create a handler that sets AuditModeBypass with an allow count
	// This simulates a scenario where an audit_only deny policy matched
	// but allowed the request because of audit mode
	auditModeHandler := &MockValidationHandlerWithFlags{
		name:              "audit-mode-handler",
		shouldAllow:       true,
		allowCount:        1, // Need an allow count for the chain to recognize results
		auditModeBypass:   true,
		recommendedAction: config.PolicyActionDeny,
	}

	chain := NewToolValidationChain(auditModeHandler)

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "test_tool"},
	}

	results, err := chain.Handle(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, results.Allowed, "Should be allowed despite audit_only deny")
	assert.True(t, results.AuditModeBypass, "AuditModeBypass flag should propagate")
	assert.Equal(t, config.PolicyActionDeny, results.RecommendedAction, "RecommendedAction should be deny")
}

// TestValidationChain_AuditModeBypassClearedOnEnforcedDeny verifies that when
// one handler sets AuditModeBypass (audit-only deny) but another handler issues
// an enforced deny, the AuditModeBypass flag is cleared. Without this fix, the
// chain could report Allowed=false and AuditModeBypass=true simultaneously.
func TestValidationChain_AuditModeBypassClearedOnEnforcedDeny(t *testing.T) {
	// First handler: audit-only deny (sets AuditModeBypass=true, allows)
	auditOnlyHandler := &MockValidationHandlerWithFlags{
		name:              "audit-only-handler",
		shouldAllow:       true,
		allowCount:        1,
		auditModeBypass:   true,
		recommendedAction: config.PolicyActionDeny,
	}

	// Second handler: enforced deny (actually blocks)
	enforcedDenyHandler := &MockValidationHandler{
		name:        "enforced-deny-handler",
		shouldAllow: false,
		expectedResult: ValidationResult{
			PolicyName: "enforced-deny",
			PolicyType: "mock",
			Action:     config.PolicyActionDeny,
			Message:    "Enforced deny",
		},
	}

	chain := NewToolValidationChain(auditOnlyHandler, enforcedDenyHandler)

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "test_tool"},
	}

	results, err := chain.Handle(context.Background(), req)
	require.NoError(t, err)

	assert.False(t, results.Allowed, "Should be denied by enforced deny handler")
	assert.False(t, results.AuditModeBypass, "AuditModeBypass must be cleared when an enforced deny overrides it")
	assert.Equal(t, config.PolicyActionDeny, results.RecommendedAction)
}

// MockValidationHandlerWithFlags extends MockValidationHandler to support
// testing FailedOpen and AuditModeBypass flag propagation.
type MockValidationHandlerWithFlags struct {
	name              string
	shouldAllow       bool
	failedOpen        bool
	auditModeBypass   bool
	recommendedAction config.PolicyAction
	allowCount        int
	denyCount         int
}

func (m *MockValidationHandlerWithFlags) HandleToolCall(context.Context, mcp.CallToolRequest) (ValidationResults, error) {
	return ValidationResults{
		Allowed:           m.shouldAllow,
		AllowCount:        m.allowCount,
		DenyCount:         m.denyCount,
		FailedOpen:        m.failedOpen,
		AuditModeBypass:   m.auditModeBypass,
		RecommendedAction: m.recommendedAction,
	}, nil
}

// TestCELValidationHandler_FailOpenOnRuntimeError verifies that CEL runtime errors
// result in fail-open behavior and set the FailedOpen flag.
func TestCELValidationHandler_FailOpenOnRuntimeError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	engine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)

	// Load a policy that will cause a runtime error
	policies := []config.Policy{
		{
			Name:       "runtime-error-policy",
			Expression: `request.params.nonexistent.nested`, // Will fail at runtime
			Action:     config.PolicyActionDeny,
			Message:    "Should not reach this",
		},
	}
	err = engine.LoadPolicies(policies, "")
	require.NoError(t, err)

	handler := NewToolCELValidationHandler(sessionLogger, engine)

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params:  mcp.CallToolParams{Name: "test_tool"},
	}

	results, err := handler.HandleToolCall(context.Background(), req)
	require.NoError(t, err, "Handler should not return error on CEL runtime error")

	assert.True(t, results.Allowed, "Should fail-open and allow")
	assert.True(t, results.FailedOpen, "FailedOpen flag should be set")
	assert.Contains(t, results.Message, "fail-open", "Message should indicate fail-open")
}

// TestDeriveAuditActionReason verifies that the shared action reason derivation
// function produces the correct result for all combinations of validation flags
// and deny reasons used by CLI, Action, and Intercept handlers.
func TestDeriveAuditActionReason(t *testing.T) {
	tests := []struct {
		name            string
		allowed         bool
		auditModeBypass bool
		failedOpen      bool
		denyReason      ActionReason
		want            string
	}{
		{
			name:       "allowed with no special conditions → empty",
			allowed:    true,
			denyReason: ActionReasonRequestPolicy,
			want:       "",
		},
		{
			name:       "denied with request_policy reason",
			allowed:    false,
			denyReason: ActionReasonRequestPolicy,
			want:       "request_policy",
		},
		{
			name:       "denied with response_policy reason",
			allowed:    false,
			denyReason: ActionReasonResponsePolicy,
			want:       "response_policy",
		},
		{
			name:       "denied with empty deny reason (CLI/Action style)",
			allowed:    false,
			denyReason: "",
			want:       "",
		},
		{
			name:            "audit_mode bypass takes precedence over fail_open",
			allowed:         true,
			auditModeBypass: true,
			failedOpen:      true,
			denyReason:      ActionReasonRequestPolicy,
			want:            "audit_mode",
		},
		{
			name:       "fail_open when allowed and no audit bypass",
			allowed:    true,
			failedOpen: true,
			denyReason: ActionReasonRequestPolicy,
			want:       "fail_open",
		},
		{
			name:            "denied overrides audit_mode (deny reason wins)",
			allowed:         false,
			auditModeBypass: true,
			denyReason:      ActionReasonRequestPolicy,
			want:            "request_policy",
		},
		{
			name:       "denied overrides fail_open (deny reason wins)",
			allowed:    false,
			failedOpen: true,
			denyReason: ActionReasonResponsePolicy,
			want:       "response_policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveAuditActionReason(tt.allowed, tt.auditModeBypass, tt.failedOpen, tt.denyReason)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestActionReasonTypeString verifies ActionReason type string conversion works correctly.
func TestActionReasonTypeString(t *testing.T) {
	tests := []struct {
		name     string
		reason   ActionReason
		expected string
	}{
		{
			name:     "request_policy converts correctly",
			reason:   ActionReasonRequestPolicy,
			expected: "request_policy",
		},
		{
			name:     "response_policy converts correctly",
			reason:   ActionReasonResponsePolicy,
			expected: "response_policy",
		},
		{
			name:     "audit_mode converts correctly",
			reason:   ActionReasonAuditMode,
			expected: "audit_mode",
		},
		{
			name:     "fail_open converts correctly",
			reason:   ActionReasonFailOpen,
			expected: "fail_open",
		},
		{
			name:     "empty reason stays empty",
			reason:   ActionReason(""),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.reason))
		})
	}
}
