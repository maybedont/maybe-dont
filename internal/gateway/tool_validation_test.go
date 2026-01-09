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
			Allowed:    true,
			Message:    "Allowed by mock handler",
		},
	}

	denyHandler := &MockValidationHandler{
		name:        "deny-handler",
		shouldAllow: false,
		expectedResult: ValidationResult{
			PolicyName: "Deny Policy",
			PolicyType: "mock",
			Allowed:    false,
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
			assert.True(t, result.Allowed)
		}
		if result.PolicyName == "Deny Policy" {
			foundDeny = true
			assert.False(t, result.Allowed)
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
			Allowed:    true,
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

	policies := []config.CELPolicy{
		{
			Name:       "allow-read-tool",
			Expression: `request.method == "tools/call" && request.params.name == "read_file"`,
			Action:     "allow",
			Message:    "Allowed to call read_file",
		},
		{
			Name:       "deny-delete-tool",
			Expression: `request.method == "tools/call" && request.params.name == "delete_file"`,
			Action:     "deny",
			Message:    "delete_file is not allowed",
		},
	}

	err = celEngine.LoadPolicies(policies)
	require.NoError(t, err)

	// Create validation chain with real handlers
	chain := NewToolValidationChain(
		NewToolCELValidationHandler(sessionLogger, celEngine),
		NewToolLoggingHandler(sessionLogger),
	)

	tests := []struct {
		name        string
		req         mcp.CallToolRequest
		wantAllowed bool
		wantResults int // Expected number of validation results
	}{
		{
			name: "read_file with CEL and logging handlers",
			req: mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params: mcp.CallToolParams{
					Name:      "read_file",
					Arguments: map[string]any{"target_file": "test.txt"},
				},
			},
			wantAllowed: true,
			wantResults: 2, // CEL + Logging results
		},
		{
			name: "delete_file with CEL and logging handlers",
			req: mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params: mcp.CallToolParams{
					Name:      "delete_file",
					Arguments: map[string]any{"target_file": "test.txt"},
				},
			},
			wantAllowed: false,
			wantResults: 2, // CEL + Logging results
		},
		{
			name: "unknown tool with CEL and logging handlers",
			req: mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params: mcp.CallToolParams{
					Name:      "unknown_tool",
					Arguments: map[string]any{"arg": "value"},
				},
			},
			wantAllowed: true, // Default allow when no policies match
			wantResults: 2,    // CEL + Logging results
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := chain.Handle(context.Background(), tt.req)
			require.NoError(t, err)

			assert.Equal(t, tt.wantAllowed, results.Allowed)
			assert.Len(t, results.Results, tt.wantResults)

			// Verify we have both CEL and logging results
			hasCEL := false
			hasLogging := false
			for _, result := range results.Results {
				if result.PolicyType == "cel" {
					hasCEL = true
				}
				if result.PolicyType == "audit" {
					hasLogging = true
				}
			}

			// CEL result should always be present now, even if no policies match
			assert.True(t, hasCEL, "Should have CEL validation result")
			assert.True(t, hasLogging, "Should have audit logging result")

			if tt.name == "unknown tool with CEL and logging handlers" {
				// Check that the CEL result is the trace result
				foundTrace := false
				for _, result := range results.Results {
					if result.PolicyType == "cel" && result.Message == "No policies matched" {
						foundTrace = true
					}
				}
				assert.True(t, foundTrace, "Should have CEL trace result for no policies matched")
			}
		})
	}
}

func TestLoggingHandler_Isolation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	handler := NewToolLoggingHandler(sessionLogger)

	req := mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params: mcp.CallToolParams{
			Name:      "read_file",
			Arguments: map[string]any{"target_file": "test.txt"},
		},
	}

	results, err := handler.HandleToolCall(context.Background(), req)
	require.NoError(t, err)

	// Logging handler should always allow and not interfere
	assert.True(t, results.Allowed)
	assert.Equal(t, 0, results.AllowCount) // No explicit allow/deny counts
	assert.Equal(t, 0, results.DenyCount)
	assert.Len(t, results.Results, 1) // Should have one audit log result
	assert.Equal(t, "Audit Logging", results.Results[0].PolicyName)
	assert.Equal(t, "audit", results.Results[0].PolicyType)
	assert.True(t, results.Results[0].Allowed)
}

func TestCELValidationHandler_Isolation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	engine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)

	// Load a simple policy
	policies := []config.CELPolicy{
		{
			Name:       "allow-tools-call",
			Expression: `request.method == "tools/call"`,
			Action:     "allow",
			Message:    "Allowed to call tools",
		},
	}
	err = engine.LoadPolicies(policies)
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
