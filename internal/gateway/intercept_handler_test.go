package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// --- Input Validation Tests ---

// TestInterceptHandler_InvalidContentType verifies 400 on non-JSON content type.
func TestInterceptHandler_InvalidContentType(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intercept", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertInterceptError(t, w, "Content-Type must be application/json")
}

// TestInterceptHandler_InvalidJSON verifies 400 on malformed JSON.
func TestInterceptHandler_InvalidJSON(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intercept", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestInterceptHandler_MissingEvent verifies 400 when event field is empty.
func TestInterceptHandler_MissingEvent(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)
	body := `{"phase": "request", "payload": {"name": "test_tool"}}`

	resp, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, resp, "event")
}

// TestInterceptHandler_UnsupportedEvent verifies 400 when event is not "tools/call".
func TestInterceptHandler_UnsupportedEvent(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)
	body := `{"event": "resources/read", "phase": "request", "payload": {"name": "test_tool"}}`

	resp, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, resp, "tools/call")
}

// TestInterceptHandler_MissingPhase verifies 400 when phase field is empty.
func TestInterceptHandler_MissingPhase(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)
	body := `{"event": "tools/call", "payload": {"name": "test_tool"}}`

	resp, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, resp, "phase")
}

// TestInterceptHandler_InvalidPhase verifies 400 when phase is not "request" or "response".
func TestInterceptHandler_InvalidPhase(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)
	body := `{"event": "tools/call", "phase": "invalid", "payload": {"name": "test_tool"}}`

	resp, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, resp, "phase")
}

// TestInterceptHandler_MissingPayloadName verifies 400 when payload.name is empty.
func TestInterceptHandler_MissingPayloadName(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)
	body := `{"event": "tools/call", "phase": "request", "payload": {}}`

	resp, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, resp, "name")
}

// TestInterceptHandler_ResponsePhaseMissingResult verifies 400 when phase is
// "response" but payload.result is not provided.
func TestInterceptHandler_ResponsePhaseMissingResult(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)
	body := `{"event": "tools/call", "phase": "response", "payload": {"name": "test_tool"}}`

	resp, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, resp, "result")
}

// TestInterceptHandler_Disabled verifies 400 when intercept is not enabled.
func TestInterceptHandler_Disabled(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled: false,
		Logger:  logger,
	})

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intercept", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertInterceptError(t, w, "not enabled")
}

// TestInterceptHandler_ValidRequestReturns200 verifies that a valid request phase
// request returns 200 (placeholder for now, full evaluation tested in Task 6).
func TestInterceptHandler_ValidRequestReturns200(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)
	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool", "arguments": {"key": "value"}}}`

	_, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusOK, code)
}

// --- Request Phase Evaluation Tests ---

// TestInterceptHandler_RequestPhase_MCPTool_Allowed verifies that a non-shell
// MCP tool call is evaluated as a tool call and returns valid=true with severity="info".
func TestInterceptHandler_RequestPhase_MCPTool_Allowed(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "some_mcp_tool", "arguments": {"key": "value"}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid)
	assert.Equal(t, "info", resp.Severity)
	assert.Equal(t, "validation", resp.Type)
	assert.Equal(t, "request", resp.Phase)
	assert.Equal(t, "maybe-dont", resp.Interceptor)
}

// TestInterceptHandler_RequestPhase_MCPTool_Denied verifies that a denied MCP
// tool call returns valid=false with severity="error" and messages.
func TestInterceptHandler_RequestPhase_MCPTool_Denied(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	celEngine := newTestCELEngineWithDenyRule(t)
	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "dangerous_tool", "arguments": {"path": "/etc/passwd"}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.False(t, resp.Valid)
	assert.Equal(t, "error", resp.Severity)
	assert.NotEmpty(t, resp.Messages)
}

// TestInterceptHandler_RequestPhase_ShellTool_CLIDeny verifies that a shell
// tool (e.g., "Bash") triggers CLI command parsing and cli_expression evaluation.
func TestInterceptHandler_RequestPhase_ShellTool_CLIDeny(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	celEngine := newTestCELEngineWithCLIDenyRule(t)
	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "Bash", "arguments": {"command": "rm -rf /"}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.False(t, resp.Valid)
	assert.Equal(t, "error", resp.Severity)
}

// TestInterceptHandler_RequestPhase_ShellTool_MCPAllow verifies that a shell
// tool also evaluates mcp_expression (not just cli_expression).
func TestInterceptHandler_RequestPhase_ShellTool_MCPAllow(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	// Use a deny rule with Expression (mcp_expression), not cli_expression
	celEngine := newTestCELEngineWithDenyRule(t)
	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	// Shell tool — should also be evaluated as MCP tool call
	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "Bash", "arguments": {"command": "echo hello"}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	// Should be denied because the mcp_expression deny-all rule triggers on the tool call
	assert.False(t, resp.Valid)
}

// TestInterceptHandler_RequestPhase_AuditModeBypass verifies that audit-only
// deny returns valid=true with severity="warn".
func TestInterceptHandler_RequestPhase_AuditModeBypass(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	celEngine := newTestCELEngineWithAuditOnlyDenyRule(t)
	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "some_tool"}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid)
	assert.Equal(t, "warn", resp.Severity)
}

// TestInterceptHandler_RequestPhase_NoEngines verifies that when no engines
// are configured, the response is valid=true.
func TestInterceptHandler_RequestPhase_NoEngines(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool"}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid)
	assert.Equal(t, "info", resp.Severity)
}

// TestInterceptHandler_ResponseFormat verifies the SEP-1763 response structure:
// interceptor, type, phase, valid, severity, messages, durationMs, info fields.
func TestInterceptHandler_ResponseFormat(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool"}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))

	assert.Equal(t, "maybe-dont", resp.Interceptor)
	assert.Equal(t, "validation", resp.Type)
	assert.Equal(t, "request", resp.Phase)
	assert.NotEmpty(t, resp.Info.RequestID)
	assert.Equal(t, "1.0.0-test", resp.Info.ServerVersion)
	assert.NotNil(t, resp.Messages)
	assert.NotNil(t, resp.Info.Results)
	assert.GreaterOrEqual(t, resp.DurationMs, int64(0))
}

// --- Response Phase Evaluation Tests ---

// TestInterceptHandler_ResponsePhase_Allowed verifies that a response with
// no policy violations returns type="validation", valid=true.
func TestInterceptHandler_ResponsePhase_Allowed(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	chain := NewResponseValidationChain(logger, &stubResponseHandler{
		results: ResponseValidationResults{
			Allowed: true,
			Message: "Response approved",
			Results: []ResponseValidationResult{},
		},
	})
	evaluator := &PolicyEvaluator{
		ResponseChain: chain,
		Logger:        logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "response", "payload": {"name": "test_tool", "result": {"content": [{"type": "text", "text": "hello world"}]}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid)
	assert.Equal(t, "info", resp.Severity)
	assert.Equal(t, "validation", resp.Type)
	assert.Equal(t, "response", resp.Phase)
	assert.False(t, resp.Modified)
	assert.Nil(t, resp.Payload)
}

// TestInterceptHandler_ResponsePhase_Denied verifies that a denied response
// returns type="validation", valid=false, severity="error".
func TestInterceptHandler_ResponsePhase_Denied(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	chain := NewResponseValidationChain(logger, &stubResponseHandler{
		results: ResponseValidationResults{
			Allowed: false,
			Message: "Response denied by policy",
			Results: []ResponseValidationResult{
				{PolicyName: "deny-response", PolicyType: "cel", Action: "deny"},
			},
		},
	})
	evaluator := &PolicyEvaluator{
		ResponseChain: chain,
		Logger:        logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "response", "payload": {"name": "test_tool", "result": {"content": [{"type": "text", "text": "sensitive data"}]}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.False(t, resp.Valid)
	assert.Equal(t, "error", resp.Severity)
	assert.Equal(t, "validation", resp.Type)
	assert.NotEmpty(t, resp.Messages)
}

// TestInterceptHandler_ResponsePhase_Redacted verifies that a redacted response
// returns type="mutation", modified=true, with the redacted payload.
func TestInterceptHandler_ResponsePhase_Redacted(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	redacted := "[REDACTED]"
	chain := NewResponseValidationChain(logger, &stubResponseHandler{
		results: ResponseValidationResults{
			Allowed:         true,
			Message:         "Content redacted",
			RedactedContent: &redacted,
			Results: []ResponseValidationResult{
				{PolicyName: "redact-secrets", PolicyType: "cel", Action: "redact", RedactedContent: redacted},
			},
		},
	})
	evaluator := &PolicyEvaluator{
		ResponseChain: chain,
		Logger:        logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "response", "payload": {"name": "test_tool", "result": {"content": [{"type": "text", "text": "secret: password123"}]}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid)
	require.Equal(t, "mutation", resp.Type)
	require.True(t, resp.Modified)
	require.NotNil(t, resp.Payload)
	require.NotNil(t, resp.Payload.Result)
	require.NotEmpty(t, resp.Payload.Result.Content)
	assert.Equal(t, "[REDACTED]", resp.Payload.Result.Content[0].Text)
}

// TestInterceptHandler_ResponsePhase_NoChain verifies graceful handling when
// no response validation chain is configured.
func TestInterceptHandler_ResponsePhase_NoChain(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "response", "payload": {"name": "test_tool", "result": {"content": [{"type": "text", "text": "hello"}]}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid)
	assert.Equal(t, "info", resp.Severity)
	assert.Equal(t, "validation", resp.Type)
}

// --- Audit Logging Tests ---

// TestInterceptHandler_AuditEntry_MCPTool verifies audit entry has source="intercept",
// Tool field populated with payload.name, and UpstreamRequest with mapped context fields.
func TestInterceptHandler_AuditEntry_MCPTool(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	auditWriter := &mockAuditWriter{}

	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:               true,
		ShellToolNames:        []string{"Bash"},
		Logger:                logger,
		Version:               "1.0.0-test",
		Evaluator:             evaluator,
		AuditWriter:           auditWriter,
		IncludeArgumentValues: true,
	})

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "some_mcp_tool", "arguments": {"key": "value"}}, "context": {"traceId": "trace-123", "sessionId": "session-456"}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)
	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "intercept", entry.Source)
	require.NotNil(t, entry.Tool)
	assert.Equal(t, "some_mcp_tool", entry.Tool.Name)
	assert.Equal(t, "some_mcp_tool", entry.Tool.PrefixedName)
	assert.Equal(t, map[string]any{"key": "value"}, entry.Tool.Params)
	assert.Nil(t, entry.CLI)
	assert.Equal(t, "trace-123", entry.UpstreamRequest.ExternalID)
	assert.Equal(t, "session-456", entry.UpstreamRequest.SessionID)
	assert.Equal(t, "allow", entry.Action)
}

// TestInterceptHandler_AuditEntry_ShellTool verifies audit entry has both
// Tool and CLI fields populated for shell tools.
func TestInterceptHandler_AuditEntry_ShellTool(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	auditWriter := &mockAuditWriter{}

	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:               true,
		ShellToolNames:        []string{"Bash"},
		Logger:                logger,
		Version:               "1.0.0-test",
		Evaluator:             evaluator,
		AuditWriter:           auditWriter,
		IncludeArgumentValues: true,
	})

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "Bash", "arguments": {"command": "echo hello"}}, "config": {"working_directory": "/home/user"}}`
	_, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "intercept", entry.Source)
	require.NotNil(t, entry.Tool)
	assert.Equal(t, "Bash", entry.Tool.Name)
	require.NotNil(t, entry.CLI)
	assert.Equal(t, "echo", entry.CLI.Command)
	assert.Equal(t, []string{"hello"}, entry.CLI.Arguments)
	assert.Equal(t, "/home/user", entry.CLI.WorkingDirectory)
}

// TestInterceptHandler_AuditEntry_ContextMapping verifies context.traceId maps
// to UpstreamRequest.ExternalID and context.sessionId maps to UpstreamRequest.SessionID.
func TestInterceptHandler_AuditEntry_ContextMapping(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	auditWriter := &mockAuditWriter{}

	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:     true,
		Logger:      logger,
		Version:     "1.0.0-test",
		Evaluator:   evaluator,
		AuditWriter: auditWriter,
	})

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool"}, "context": {"traceId": "trace-abc", "spanId": "span-def", "sessionId": "session-ghi", "timestamp": "2026-02-24T00:00:00Z"}}`
	_, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "trace-abc", entry.UpstreamRequest.ExternalID)
	assert.Equal(t, "session-ghi", entry.UpstreamRequest.SessionID)
}

// TestInterceptHandler_AuditEntry_PrincipalAsClientID verifies context.principal.id
// maps to UpstreamRequest.ClientID when X-Maybe-Dont-Client-ID header is not set.
func TestInterceptHandler_AuditEntry_PrincipalAsClientID(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	auditWriter := &mockAuditWriter{}

	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:     true,
		Logger:      logger,
		Version:     "1.0.0-test",
		Evaluator:   evaluator,
		AuditWriter: auditWriter,
	})

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool"}, "context": {"principal": {"type": "agent", "id": "claude-code-v1"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intercept", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No X-Maybe-Dont-Client-ID header
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "claude-code-v1", entry.UpstreamRequest.ClientID)
}

// TestInterceptHandler_AuditEntry_ResponsePhase verifies that response phase
// evaluation produces an audit entry with ResponseValidation populated (not RequestValidation),
// and the correct action/actionReason for denied responses.
func TestInterceptHandler_AuditEntry_ResponsePhase(t *testing.T) {
	tests := []struct {
		name           string
		results        ResponseValidationResults
		expectAction   string
		expectReason   string
		expectRespVal  bool
		expectRedacted bool
	}{
		{
			name: "allowed response",
			results: ResponseValidationResults{
				Allowed: true,
				Message: "Response OK",
				Results: []ResponseValidationResult{},
			},
			expectAction:  "allow",
			expectRespVal: false, // no rules details
		},
		{
			name: "denied response",
			results: ResponseValidationResults{
				Allowed: false,
				Message: "Response denied",
				Results: []ResponseValidationResult{
					{PolicyName: "deny-secrets", PolicyType: "cel", Action: "deny"},
				},
				RulesDetails: &AuditRulesResult{
					Action:       "deny",
					DecidingRule: "deny-secrets",
				},
			},
			expectAction:  "deny",
			expectReason:  "response_policy",
			expectRespVal: true,
		},
		{
			name: "redacted response",
			results: ResponseValidationResults{
				Allowed:         true,
				Message:         "Content redacted",
				RedactedContent: ptrStr("[REDACTED]"),
				Results: []ResponseValidationResult{
					{PolicyName: "redact-secrets", PolicyType: "cel", Action: "redact"},
				},
				RulesDetails: &AuditRulesResult{
					Action:       "redact",
					DecidingRule: "redact-secrets",
				},
			},
			expectAction:   "redact",
			expectRespVal:  true,
			expectRedacted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := config.NewSessionLogger(zaptest.NewLogger(t))
			chain := NewResponseValidationChain(logger, &stubResponseHandler{results: tt.results})
			evaluator := &PolicyEvaluator{
				ResponseChain: chain,
				Logger:        logger,
			}
			auditWriter := &mockAuditWriter{}

			handler := NewInterceptHandler(InterceptHandlerConfig{
				Enabled:     true,
				Logger:      logger,
				Version:     "1.0.0-test",
				Evaluator:   evaluator,
				AuditWriter: auditWriter,
			})

			body := `{"event": "tools/call", "phase": "response", "payload": {"name": "test_tool", "result": {"content": [{"type": "text", "text": "some content"}]}}}`
			_, code := sendIntercept(t, handler, body)
			assert.Equal(t, http.StatusOK, code)

			entries := auditWriter.getEntries()
			require.Len(t, entries, 1, "response phase should produce exactly one audit entry")

			entry := entries[0]
			assert.Equal(t, "intercept", entry.Source)
			assert.Equal(t, tt.expectAction, entry.Action)
			assert.Equal(t, tt.expectReason, entry.ActionReason)
			assert.Nil(t, entry.RequestValidation, "response phase should not set RequestValidation")

			if tt.expectRespVal {
				require.NotNil(t, entry.ResponseValidation, "should have ResponseValidation")
				require.NotNil(t, entry.ResponseValidation.CEL, "should have CEL details")
			} else {
				assert.Nil(t, entry.ResponseValidation)
			}
		})
	}
}

// TestInterceptHandler_AuditEntry_ArgumentFiltering verifies that
// IncludeArgumentValues=false suppresses params in the audit entry.
func TestInterceptHandler_AuditEntry_ArgumentFiltering(t *testing.T) {
	tests := []struct {
		name         string
		includeArgs  bool
		expectParams bool
		toolName     string
		body         string
		description  string
	}{
		{
			name:         "MCP tool with args included",
			includeArgs:  true,
			expectParams: true,
			toolName:     "some_tool",
			body:         `{"event": "tools/call", "phase": "request", "payload": {"name": "some_tool", "arguments": {"secret": "password123"}}}`,
			description:  "When IncludeArgumentValues=true, params should be populated",
		},
		{
			name:         "MCP tool with args excluded",
			includeArgs:  false,
			expectParams: false,
			toolName:     "some_tool",
			body:         `{"event": "tools/call", "phase": "request", "payload": {"name": "some_tool", "arguments": {"secret": "password123"}}}`,
			description:  "When IncludeArgumentValues=false, params should be nil",
		},
		{
			name:         "shell tool with args excluded",
			includeArgs:  false,
			expectParams: false,
			toolName:     "Bash",
			body:         `{"event": "tools/call", "phase": "request", "payload": {"name": "Bash", "arguments": {"command": "echo secret"}}}`,
			description:  "Shell tool params should also respect IncludeArgumentValues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := config.NewSessionLogger(zaptest.NewLogger(t))
			evaluator := &PolicyEvaluator{Logger: logger}
			auditWriter := &mockAuditWriter{}

			handler := NewInterceptHandler(InterceptHandlerConfig{
				Enabled:               true,
				ShellToolNames:        []string{"Bash"},
				Logger:                logger,
				Version:               "1.0.0-test",
				Evaluator:             evaluator,
				AuditWriter:           auditWriter,
				IncludeArgumentValues: tt.includeArgs,
			})

			_, code := sendIntercept(t, handler, tt.body)
			assert.Equal(t, http.StatusOK, code)

			entries := auditWriter.getEntries()
			require.Len(t, entries, 1)

			entry := entries[0]
			require.NotNil(t, entry.Tool)
			if tt.expectParams {
				assert.NotNil(t, entry.Tool.Params, tt.description)
			} else {
				assert.Nil(t, entry.Tool.Params, tt.description)
			}
		})
	}
}

// --- Blocking Budget Tests ---

// TestInterceptHandler_BlockingBudget_CELWithBudget verifies that the evaluator's
// MaxBlockingMs and MaxRuleEvaluationMs are used (not zero-defaults) when evaluating
// requests through the intercept handler with a real CEL engine.
func TestInterceptHandler_BlockingBudget_CELWithBudget(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	celEngine := newTestCELEngineWithDenyRule(t)
	evaluator := &PolicyEvaluator{
		CELEngine:           celEngine,
		MaxBlockingMs:       5000,
		MaxRuleEvaluationMs: 2000,
		Logger:              logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	// This should be denied by the CEL rule, confirming the full evaluation
	// pipeline (handler → evaluator → budget → CEL engine) works.
	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool", "arguments": {"key": "value"}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.False(t, resp.Valid, "CEL deny rule should trigger through the budget path")
	assert.Equal(t, "error", resp.Severity)
	assert.GreaterOrEqual(t, resp.DurationMs, int64(0))
}

// TestInterceptHandler_ShellTool_DualEvaluation verifies that shell tool
// evaluation runs both CLI and MCP paths independently. Each path creates
// its own blocking budget within PolicyEvaluator.
func TestInterceptHandler_ShellTool_DualEvaluation(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	// Create an engine with BOTH mcp_expression and cli_expression rules.
	// The mcp_expression rule denies tool "Bash"; the cli_expression rule denies command "rm".
	celEngine, err := NewCELPolicyEngine(context.Background(), logger)
	require.NoError(t, err)
	err = celEngine.LoadPolicies([]config.Policy{
		{
			Name:          "deny-rm-cli",
			CLIExpression: `cli.command == "rm"`,
			Action:        config.PolicyActionDeny,
			Message:       "rm denied by CLI rule",
		},
		{
			Name:       "deny-bash-mcp",
			Expression: `request.params.name == "Bash"`,
			Action:     config.PolicyActionDeny,
			Message:    "Bash denied by MCP rule",
		},
	}, "")
	require.NoError(t, err)

	evaluator := &PolicyEvaluator{
		CELEngine:           celEngine,
		MaxBlockingMs:       10000,
		MaxRuleEvaluationMs: 5000,
		Logger:              logger,
	}
	auditWriter := &mockAuditWriter{}
	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:        true,
		ShellToolNames: []string{"Bash"},
		Logger:         logger,
		Version:        "1.0.0-test",
		Evaluator:      evaluator,
		AuditWriter:    auditWriter,
	})

	// Shell tool "Bash" with command "rm -rf /" — should trigger BOTH deny rules
	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "Bash", "arguments": {"command": "rm -rf /"}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.False(t, resp.Valid, "Should be denied (both CLI and MCP rules trigger)")

	// Verify both evaluations ran by checking policy results contain both rules
	require.NotEmpty(t, resp.Info.Results)
	policyNames := make([]string, 0, len(resp.Info.Results))
	for _, r := range resp.Info.Results {
		policyNames = append(policyNames, r.PolicyName)
	}
	assert.Contains(t, policyNames, "deny-rm-cli", "CLI expression rule should be evaluated")
	assert.Contains(t, policyNames, "deny-bash-mcp", "MCP expression rule should be evaluated")
}

// TestInterceptHandler_ShellTool_CLIAllowMCPDeny verifies that when CLI evaluation
// allows but MCP evaluation denies, the merged result is deny.
func TestInterceptHandler_ShellTool_CLIAllowMCPDeny(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	// MCP rule denies all; no CLI rule
	celEngine, err := NewCELPolicyEngine(context.Background(), logger)
	require.NoError(t, err)
	err = celEngine.LoadPolicies([]config.Policy{
		{
			Name:       "deny-all-mcp",
			Expression: `true`,
			Action:     config.PolicyActionDeny,
			Message:    "Denied by MCP rule",
		},
	}, "")
	require.NoError(t, err)

	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "Bash", "arguments": {"command": "echo safe"}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.False(t, resp.Valid, "MCP deny should override CLI allow in merged results")
}

// --- Audit Entry Completeness with Real CEL Evaluation ---

// TestInterceptHandler_AuditEntry_CELRulesDetails verifies that when a real
// CEL engine evaluates a request, the audit entry's RequestValidation.CEL field
// is populated with rule evaluation details.
func TestInterceptHandler_AuditEntry_CELRulesDetails(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	celEngine := newTestCELEngineWithDenyRule(t)
	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    logger,
	}
	auditWriter := &mockAuditWriter{}

	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:     true,
		Logger:      logger,
		Version:     "1.0.0-test",
		Evaluator:   evaluator,
		AuditWriter: auditWriter,
	})

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool"}}`
	_, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusOK, code)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "deny", entry.Action)
	require.NotNil(t, entry.RequestValidation, "should have RequestValidation for CEL deny")
	require.NotNil(t, entry.RequestValidation.CEL, "should have CEL details")
	assert.Equal(t, "deny", entry.RequestValidation.CEL.Action)
	assert.Equal(t, "deny-all", entry.RequestValidation.CEL.DecidingRule)
	assert.NotEmpty(t, entry.RequestValidation.CEL.Results)
}

// TestInterceptHandler_ShellTool_AuditMergesRulesDetails verifies that when a shell
// tool triggers both CLI and MCP CEL evaluation, the audit entry contains merged
// RulesDetails with rule results from both evaluation paths.
func TestInterceptHandler_ShellTool_AuditMergesRulesDetails(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	// Create CEL engine with both CLI-expression and MCP-expression rules
	celEngine, err := NewCELPolicyEngine(context.Background(), logger)
	require.NoError(t, err)
	err = celEngine.LoadPolicies([]config.Policy{
		{
			Name:          "deny-rm-cli",
			CLIExpression: `cli.command == "rm"`,
			Action:        config.PolicyActionDeny,
			Message:       "rm denied",
		},
		{
			Name:       "deny-bash-mcp",
			Expression: `request.params.name == "Bash"`,
			Action:     config.PolicyActionDeny,
			Message:    "Bash denied",
		},
	}, "")
	require.NoError(t, err)

	evaluator := &PolicyEvaluator{
		CELEngine:           celEngine,
		MaxBlockingMs:       10000,
		MaxRuleEvaluationMs: 5000,
		Logger:              logger,
	}
	auditWriter := &mockAuditWriter{}

	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:        true,
		ShellToolNames: []string{"Bash"},
		Logger:         logger,
		Version:        "1.0.0-test",
		Evaluator:      evaluator,
		AuditWriter:    auditWriter,
	})

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "Bash", "arguments": {"command": "rm -rf /"}}}`
	_, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusOK, code)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "deny", entry.Action)
	assert.Equal(t, "intercept", entry.Source)
	require.NotNil(t, entry.RequestValidation, "should have RequestValidation")
	require.NotNil(t, entry.RequestValidation.CEL, "should have CEL details")

	// The merged RulesDetails should contain results from BOTH evaluation paths
	ruleNames := make([]string, 0, len(entry.RequestValidation.CEL.Results))
	for _, r := range entry.RequestValidation.CEL.Results {
		ruleNames = append(ruleNames, r.Rule)
	}
	assert.Contains(t, ruleNames, "deny-rm-cli", "CLI rule result should be in audit")
	assert.Contains(t, ruleNames, "deny-bash-mcp", "MCP rule result should be in audit")
	assert.Equal(t, "deny", entry.RequestValidation.CEL.Action)
}

// TestInterceptHandler_FailedOpen_ActionReason verifies that when a policy evaluation
// fails open, the audit entry's action_reason reflects the fail-open condition.
func TestInterceptHandler_FailedOpen_ActionReason(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	// Create a CEL engine with a rule that will cause a runtime error
	celEngine, err := NewCELPolicyEngine(context.Background(), logger)
	require.NoError(t, err)
	err = celEngine.LoadPolicies([]config.Policy{
		{
			Name:       "runtime-error",
			Expression: `request.params.arguments.nonexistent.deep_field == "crash"`,
			Action:     config.PolicyActionDeny,
			Message:    "should not reach here",
		},
	}, "")
	require.NoError(t, err)

	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    logger,
	}
	auditWriter := &mockAuditWriter{}

	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:     true,
		Logger:      logger,
		Version:     "1.0.0-test",
		Evaluator:   evaluator,
		AuditWriter: auditWriter,
	})

	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool"}}`
	_, code := sendIntercept(t, handler, body)
	assert.Equal(t, http.StatusOK, code)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "allow", entry.Action, "fail-open should result in allow")
	assert.Equal(t, "fail_open", entry.ActionReason, "should have fail_open action reason")
}

// --- mergeShellResults Edge Case Tests ---

// TestMergeShellResults_BothDeny verifies that when both CLI and MCP evaluations
// deny, the merged result uses the CLI message (first writer wins).
func TestMergeShellResults_BothDeny(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	cliResults := ValidationResults{
		Allowed:   false,
		Message:   "rm denied by CLI rule",
		Results:   []ValidationResult{{PolicyName: "deny-rm-cli", Action: config.PolicyActionDeny, Message: "rm denied by CLI rule"}},
		DenyCount: 1,
	}
	mcpResults := ValidationResults{
		Allowed:   false,
		Message:   "Bash denied by MCP rule",
		Results:   []ValidationResult{{PolicyName: "deny-bash-mcp", Action: config.PolicyActionDeny, Message: "Bash denied by MCP rule"}},
		DenyCount: 1,
	}

	merged := handler.mergeShellResults(cliResults, mcpResults)

	assert.False(t, merged.Allowed, "merged should be denied")
	assert.Equal(t, "rm denied by CLI rule", merged.Message, "CLI message should take precedence (first writer)")
	assert.Equal(t, 2, merged.DenyCount, "deny counts should sum")
	assert.Len(t, merged.Results, 2, "should contain results from both sides")
	assert.False(t, merged.AuditModeBypass, "no audit bypass when denied")
}

// TestMergeShellResults_CLIAllowMCPDeny verifies MCP deny overrides CLI allow.
func TestMergeShellResults_CLIAllowMCPDeny(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	cliResults := ValidationResults{
		Allowed:    true,
		Message:    "CLI allowed",
		AllowCount: 1,
	}
	mcpResults := ValidationResults{
		Allowed:   false,
		Message:   "MCP denied",
		DenyCount: 1,
	}

	merged := handler.mergeShellResults(cliResults, mcpResults)

	assert.False(t, merged.Allowed)
	assert.Equal(t, "MCP denied", merged.Message, "MCP deny message should be used when CLI allows")
}

// TestMergeShellResults_AuditBypassOverriddenByDeny verifies that AuditModeBypass
// from one side is cleared when the other side has an enforced deny.
func TestMergeShellResults_AuditBypassOverriddenByDeny(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	tests := []struct {
		name    string
		cli     ValidationResults
		mcp     ValidationResults
		wantAMB bool
	}{
		{
			name: "CLI audit_only bypass cleared by MCP enforced deny",
			cli: ValidationResults{
				Allowed:         true,
				AuditModeBypass: true,
			},
			mcp: ValidationResults{
				Allowed: false,
				Message: "denied by MCP",
			},
			wantAMB: false,
		},
		{
			name: "MCP audit_only bypass cleared by CLI enforced deny",
			cli: ValidationResults{
				Allowed: false,
				Message: "denied by CLI",
			},
			mcp: ValidationResults{
				Allowed:         true,
				AuditModeBypass: true,
			},
			wantAMB: false,
		},
		{
			name: "Both audit_only bypass preserved when both allow",
			cli: ValidationResults{
				Allowed:         true,
				AuditModeBypass: true,
			},
			mcp: ValidationResults{
				Allowed:         true,
				AuditModeBypass: true,
			},
			wantAMB: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := handler.mergeShellResults(tt.cli, tt.mcp)
			assert.Equal(t, tt.wantAMB, merged.AuditModeBypass)
		})
	}
}

// TestMergeShellResults_FailedOpenPropagated verifies that FailedOpen from either
// side is propagated to the merged result.
func TestMergeShellResults_FailedOpenPropagated(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	tests := []struct {
		name string
		cli  ValidationResults
		mcp  ValidationResults
		want bool
	}{
		{
			name: "CLI failed open propagated",
			cli:  ValidationResults{Allowed: true, FailedOpen: true},
			mcp:  ValidationResults{Allowed: true},
			want: true,
		},
		{
			name: "MCP failed open propagated",
			cli:  ValidationResults{Allowed: true},
			mcp:  ValidationResults{Allowed: true, FailedOpen: true},
			want: true,
		},
		{
			name: "both failed open",
			cli:  ValidationResults{Allowed: true, FailedOpen: true},
			mcp:  ValidationResults{Allowed: true, FailedOpen: true},
			want: true,
		},
		{
			name: "neither failed open",
			cli:  ValidationResults{Allowed: true},
			mcp:  ValidationResults{Allowed: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := handler.mergeShellResults(tt.cli, tt.mcp)
			assert.Equal(t, tt.want, merged.FailedOpen)
		})
	}
}

// TestMergeShellResults_RulesDetailsMerged verifies that RulesDetails from both
// CLI and MCP evaluations are merged (not overwritten).
func TestMergeShellResults_RulesDetailsMerged(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	cliResults := ValidationResults{
		Allowed: true,
		RulesDetails: &AuditRulesResult{
			Action:       "allow",
			EvaluationMs: 5,
			Results: []AuditRulesRuleResult{
				{Rule: "cli-rule-1", Action: "allow", Result: "allow", EvaluationMs: 5},
			},
		},
	}
	mcpResults := ValidationResults{
		Allowed: false,
		Message: "denied by MCP",
		RulesDetails: &AuditRulesResult{
			Action:       "deny",
			DecidingRule: "mcp-rule-1",
			EvaluationMs: 10,
			Results: []AuditRulesRuleResult{
				{Rule: "mcp-rule-1", Action: "deny", Result: "deny", EvaluationMs: 10},
			},
		},
	}

	merged := handler.mergeShellResults(cliResults, mcpResults)

	require.NotNil(t, merged.RulesDetails, "merged should have RulesDetails")
	assert.Len(t, merged.RulesDetails.Results, 2, "should contain rule results from both CLI and MCP")

	ruleNames := make([]string, 0, len(merged.RulesDetails.Results))
	for _, r := range merged.RulesDetails.Results {
		ruleNames = append(ruleNames, r.Rule)
	}
	assert.Contains(t, ruleNames, "cli-rule-1", "CLI rule result should be preserved")
	assert.Contains(t, ruleNames, "mcp-rule-1", "MCP rule result should be preserved")
}

// TestMergeShellResults_AIDetailsMerged verifies that AIDetails from both
// CLI and MCP evaluations are merged (not overwritten).
func TestMergeShellResults_AIDetailsMerged(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	cliResults := ValidationResults{
		Allowed: true,
		AIDetails: &AuditAIResult{
			Action:       "allow",
			EvaluationMs: 100,
			Results: []AuditAIRuleResult{
				{Rule: "cli-ai-rule", Action: "allow", Result: "allow"},
			},
		},
	}
	mcpResults := ValidationResults{
		Allowed: true,
		AIDetails: &AuditAIResult{
			Action:       "allow",
			EvaluationMs: 200,
			Results: []AuditAIRuleResult{
				{Rule: "mcp-ai-rule", Action: "allow", Result: "allow"},
			},
		},
	}

	merged := handler.mergeShellResults(cliResults, mcpResults)

	require.NotNil(t, merged.AIDetails, "merged should have AIDetails")
	assert.Len(t, merged.AIDetails.Results, 2, "should contain AI results from both CLI and MCP")
}

// TestMergeShellResults_EmptyCommand verifies behavior when shell tool has empty
// or missing command string. The merge should still work with whatever results
// the evaluators produce.
func TestMergeShellResults_EmptyCommand(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	// Both allow with empty results (what happens with an empty command)
	cliResults := ValidationResults{Allowed: true, Message: "No matching policies"}
	mcpResults := ValidationResults{Allowed: true, Message: "No matching policies"}

	merged := handler.mergeShellResults(cliResults, mcpResults)

	assert.True(t, merged.Allowed)
	assert.NotEmpty(t, merged.Message, "should have a default message")
}

// TestMergeShellResults_CountAggregation verifies AllowCount and DenyCount
// are properly summed from both sides.
func TestMergeShellResults_CountAggregation(t *testing.T) {
	handler := newTestInterceptHandler(t, nil)

	cliResults := ValidationResults{
		Allowed:    true,
		AllowCount: 3,
		DenyCount:  1,
	}
	mcpResults := ValidationResults{
		Allowed:    true,
		AllowCount: 2,
		DenyCount:  0,
	}

	merged := handler.mergeShellResults(cliResults, mcpResults)

	assert.Equal(t, 5, merged.AllowCount, "allow counts should sum")
	assert.Equal(t, 1, merged.DenyCount, "deny counts should sum")
}

// --- Test Helpers ---

func ptrStr(s string) *string { return &s }

// stubResponseHandler is a test double that returns preconfigured results.
type stubResponseHandler struct {
	results ResponseValidationResults
}

func (h *stubResponseHandler) HandleResponse(_ context.Context, _ mcp.CallToolRequest, _ *mcp.CallToolResult) (ResponseValidationResults, error) {
	return h.results, nil
}

func newTestInterceptHandler(t *testing.T, evaluator *PolicyEvaluator) *InterceptHandler {
	t.Helper()
	logger := config.NewSessionLogger(zaptest.NewLogger(t))

	return NewInterceptHandler(InterceptHandlerConfig{
		Enabled:        true,
		ShellToolNames: []string{"Bash", "execute_command"},
		Logger:         logger,
		Version:        "1.0.0-test",
		Evaluator:      evaluator,
	})
}

func sendIntercept(t *testing.T, handler *InterceptHandler, body string) (string, int) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intercept", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	return w.Body.String(), w.Code
}

func assertInterceptError(t *testing.T, w *httptest.ResponseRecorder, contains string) {
	t.Helper()

	var errResp InterceptError
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Message, contains)
}
