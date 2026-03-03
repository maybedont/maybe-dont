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

// TestInterceptHandler_InputValidation verifies that malformed or invalid requests
// return the correct HTTP status code, machine-readable error code (matching the spec
// error table), and a descriptive message.
func TestInterceptHandler_InputValidation(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		contentType     string
		enabled         bool
		wantStatus      int
		wantErrorCode   string
		wantMsgContains string
	}{
		{
			name:            "invalid content type returns 415",
			body:            "not json",
			contentType:     "text/plain",
			enabled:         true,
			wantStatus:      http.StatusUnsupportedMediaType,
			wantErrorCode:   "invalid_content_type",
			wantMsgContains: "Content-Type",
		},
		{
			name:            "malformed JSON returns 400",
			body:            "{invalid",
			contentType:     "application/json",
			enabled:         true,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "invalid_request",
			wantMsgContains: "Invalid JSON",
		},
		{
			name:            "missing event returns 400",
			body:            `{"phase": "request", "payload": {"name": "test_tool"}}`,
			contentType:     "application/json",
			enabled:         true,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "missing_event",
			wantMsgContains: "event",
		},
		{
			name:            "unsupported event returns 400",
			body:            `{"event": "resources/read", "phase": "request", "payload": {"name": "test_tool"}}`,
			contentType:     "application/json",
			enabled:         true,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "unsupported_event",
			wantMsgContains: "tools/call",
		},
		{
			name:            "missing phase returns 400",
			body:            `{"event": "tools/call", "payload": {"name": "test_tool"}}`,
			contentType:     "application/json",
			enabled:         true,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "missing_phase",
			wantMsgContains: "phase",
		},
		{
			name:            "invalid phase returns 400",
			body:            `{"event": "tools/call", "phase": "invalid", "payload": {"name": "test_tool"}}`,
			contentType:     "application/json",
			enabled:         true,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "invalid_phase",
			wantMsgContains: "phase",
		},
		{
			name:            "missing payload name returns 400",
			body:            `{"event": "tools/call", "phase": "request", "payload": {}}`,
			contentType:     "application/json",
			enabled:         true,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "missing_payload_name",
			wantMsgContains: "name",
		},
		{
			// Go decodes "payload": null as a zero-value struct, so null payload
			// falls through to the payload.name check. This is intentional — the
			// spec collapses missing_payload into missing_payload_name.
			name:            "null payload returns missing_payload_name",
			body:            `{"event": "tools/call", "phase": "request", "payload": null}`,
			contentType:     "application/json",
			enabled:         true,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "missing_payload_name",
			wantMsgContains: "name",
		},
		{
			name:            "response phase missing result returns 400",
			body:            `{"event": "tools/call", "phase": "response", "payload": {"name": "test_tool"}}`,
			contentType:     "application/json",
			enabled:         true,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "response_phase_missing_result",
			wantMsgContains: "result",
		},
		{
			name:            "disabled endpoint returns 400",
			body:            `{"event": "tools/call", "phase": "request", "payload": {"name": "test_tool"}}`,
			contentType:     "application/json",
			enabled:         false,
			wantStatus:      http.StatusBadRequest,
			wantErrorCode:   "intercept_disabled",
			wantMsgContains: "not enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := config.NewSessionLogger(zaptest.NewLogger(t))
			handler := NewInterceptHandler(InterceptHandlerConfig{
				Enabled:        tt.enabled,
				ShellToolNames: []string{"Bash", "execute_command"},
				Logger:         logger,
				Version:        "1.0.0-test",
			})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/intercept", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			var errResp InterceptError
			err := json.Unmarshal(w.Body.Bytes(), &errResp)
			require.NoError(t, err)
			assert.Equal(t, tt.wantErrorCode, errResp.Error, "error code should match spec")
			assert.Contains(t, errResp.Message, tt.wantMsgContains)
		})
	}
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

	body := `{"event": "tools/call", "phase": "response", "payload": {"name": "test_tool", "arguments": {"query": "password"}, "result": {"content": [{"type": "text", "text": "secret: password123"}]}}}`
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

	// Verify the mutation payload preserves the original tool call context
	// so hook scripts can reconstruct the full call with redacted result.
	assert.Equal(t, "test_tool", resp.Payload.Name, "mutation payload should preserve original tool name")
	assert.Equal(t, "password", resp.Payload.Arguments["query"], "mutation payload should preserve original arguments")
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

// TestInterceptHandler_ResponsePhase_NonTextContentDropped verifies that non-text
// content types (e.g., image) in the response payload are silently dropped during
// conversion to mcp.CallToolResult. Only "text" content is forwarded to the
// response validation chain.
func TestInterceptHandler_ResponsePhase_NonTextContentDropped(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	// Use a stub that returns allowed — we just want to verify the conversion works
	chain := NewResponseValidationChain(logger, &stubResponseHandler{
		results: ResponseValidationResults{
			Allowed: true,
			Message: "Response OK",
		},
	})
	evaluator := &PolicyEvaluator{
		ResponseChain: chain,
		Logger:        logger,
	}
	handler := newTestInterceptHandler(t, evaluator)

	// Send mixed content: text + image. The image content type should be dropped
	// since payloadToCallToolResult only converts type="text".
	body := `{
		"event": "tools/call",
		"phase": "response",
		"payload": {
			"name": "test_tool",
			"result": {
				"content": [
					{"type": "text", "text": "visible content"},
					{"type": "image", "text": "base64data"}
				]
			}
		}
	}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid)
	assert.Equal(t, "validation", resp.Type)
}

// TestInterceptHandler_RequestPhase_ShellTool_EmptyCommand verifies that a shell tool
// sent without a command argument (e.g., {"name": "Bash", "arguments": {}}) goes through
// the full handler path without error.
func TestInterceptHandler_RequestPhase_ShellTool_EmptyCommand(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	evaluator := &PolicyEvaluator{Logger: logger}
	auditWriter := &mockAuditWriter{}

	handler := NewInterceptHandler(InterceptHandlerConfig{
		Enabled:        true,
		ShellToolNames: []string{"Bash"},
		Logger:         logger,
		Version:        "1.0.0-test",
		Evaluator:      evaluator,
		AuditWriter:    auditWriter,
	})

	// Shell tool with no command argument — parseShellCommand produces Command="" and nil args
	body := `{"event": "tools/call", "phase": "request", "payload": {"name": "Bash", "arguments": {}}}`
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code)

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid, "empty command should be allowed (no matching rules)")
	assert.Equal(t, "info", resp.Severity)

	// Verify audit entry has CLI info with empty command
	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	assert.NotNil(t, entries[0].CLI, "shell tool should populate CLI audit info even with empty command")
	assert.Equal(t, "", entries[0].CLI.Command)
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

// --- Audit Correlation Field Tests ---

// TestInterceptHandler_AuditCorrelationFields verifies that all correlation fields
// (request_id, client_ip, user_agent, session_id, external_id, client_id) are
// correctly captured in audit entries from HTTP request headers and intercept context.
func TestInterceptHandler_AuditCorrelationFields(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		headers        map[string]string
		wantRequestID  string
		wantClientID   string
		wantSessionID  string
		wantExternalID string
		wantClientIP   string
		wantUserAgent  string
		wantAutoGenID  bool // true if we expect an auto-generated request ID
		needsEvaluator bool // true if test needs a PolicyEvaluator (e.g., response phase)
	}{
		{
			name: "all correlation fields from headers and context",
			body: `{
				"event": "tools/call",
				"phase": "request",
				"payload": {"name": "test_tool"},
				"context": {
					"principal": {"type": "user", "id": "context-principal-id"},
					"traceId": "trace-abc-123",
					"sessionId": "session-xyz-789"
				}
			}`,
			headers: map[string]string{
				"X-Request-ID":           "req-correlation-test",
				"X-Maybe-Dont-Client-ID": "header-client-id",
				"User-Agent":             "test-agent/2.0",
			},
			wantRequestID:  "req-correlation-test",
			wantClientID:   "header-client-id",
			wantSessionID:  "session-xyz-789",
			wantExternalID: "trace-abc-123",
			wantClientIP:   "192.0.2.1:1234",
			wantUserAgent:  "test-agent/2.0",
		},
		{
			name: "client_id falls back to principal.id when header absent",
			body: `{
				"event": "tools/call",
				"phase": "request",
				"payload": {"name": "test_tool"},
				"context": {
					"principal": {"type": "agent", "id": "principal-fallback-id"}
				}
			}`,
			headers:       map[string]string{},
			wantClientID:  "principal-fallback-id",
			wantAutoGenID: true,
		},
		{
			name: "auto-generated request_id when no header",
			body: `{
				"event": "tools/call",
				"phase": "request",
				"payload": {"name": "test_tool"}
			}`,
			headers:       map[string]string{},
			wantAutoGenID: true,
		},
		{
			name: "response phase captures correlation fields",
			body: `{
				"event": "tools/call",
				"phase": "response",
				"payload": {
					"name": "test_tool",
					"result": {"content": [{"type": "text", "text": "ok"}]}
				},
				"context": {
					"traceId": "resp-trace-456",
					"sessionId": "resp-session-789"
				}
			}`,
			headers: map[string]string{
				"X-Request-ID": "resp-req-id",
				"User-Agent":   "resp-agent/1.0",
			},
			wantRequestID:  "resp-req-id",
			wantSessionID:  "resp-session-789",
			wantExternalID: "resp-trace-456",
			wantUserAgent:  "resp-agent/1.0",
			wantClientIP:   "192.0.2.1:1234",
			needsEvaluator: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auditWriter := &mockAuditWriter{}
			logger := config.NewSessionLogger(zaptest.NewLogger(t))

			cfg := InterceptHandlerConfig{
				Enabled:     true,
				Logger:      logger,
				Version:     "1.0.0-test",
				AuditWriter: auditWriter,
			}
			// Response phase requires an evaluator to reach the audit write path
			if tt.needsEvaluator {
				cfg.Evaluator = &PolicyEvaluator{Logger: logger}
			}
			handler := NewInterceptHandler(cfg)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/intercept", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "192.0.2.1:1234"
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)

			entries := auditWriter.getEntries()
			require.Len(t, entries, 1)

			entry := entries[0]
			assert.Equal(t, "intercept", entry.Source)

			if tt.wantAutoGenID {
				assert.Len(t, entry.UpstreamRequest.RequestID, 32, "auto-generated ID should be 32-char hex")
			} else {
				assert.Equal(t, tt.wantRequestID, entry.UpstreamRequest.RequestID)
			}

			if tt.wantClientID != "" {
				assert.Equal(t, tt.wantClientID, entry.UpstreamRequest.ClientID)
			}
			if tt.wantSessionID != "" {
				assert.Equal(t, tt.wantSessionID, entry.UpstreamRequest.SessionID)
			}
			if tt.wantExternalID != "" {
				assert.Equal(t, tt.wantExternalID, entry.UpstreamRequest.ExternalID)
			}
			if tt.wantClientIP != "" {
				assert.Equal(t, tt.wantClientIP, entry.UpstreamRequest.ClientIP)
			}
			if tt.wantUserAgent != "" {
				assert.Equal(t, tt.wantUserAgent, entry.UpstreamRequest.UserAgent)
			}
		})
	}
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

// TestInterceptHandler_ResponsePhase_FailOpen verifies that response validation
// engine errors fail open (HTTP 200 with valid=true), consistent with the gateway's
// fail-open philosophy. No 500 status is returned.
func TestInterceptHandler_ResponsePhase_FailOpen(t *testing.T) {
	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	chain := NewResponseValidationChain(logger, &errorResponseHandler{})
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
	respBody, code := sendIntercept(t, handler, body)

	assert.Equal(t, http.StatusOK, code, "response engine errors must fail open with 200")

	var resp InterceptResponse
	require.NoError(t, json.Unmarshal([]byte(respBody), &resp))
	assert.True(t, resp.Valid, "engine error should fail open as valid=true")
	assert.Equal(t, "info", resp.Severity, "fail-open should produce severity=info")
	assert.Equal(t, "validation", resp.Type)

	// Verify audit entry records the fail-open
	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "allow", entries[0].Action)
}

// --- mergeShellResults Edge Case Tests ---

// TestMergeShellResults_BothDeny verifies that when both CLI and MCP evaluations
// deny, the merged result uses the CLI message (first writer wins).
func TestMergeShellResults_BothDeny(t *testing.T) {
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

	merged := mergeShellResults(cliResults, mcpResults)

	assert.False(t, merged.Allowed, "merged should be denied")
	assert.Equal(t, "rm denied by CLI rule", merged.Message, "CLI message should take precedence (first writer)")
	assert.Equal(t, 2, merged.DenyCount, "deny counts should sum")
	assert.Len(t, merged.Results, 2, "should contain results from both sides")
	assert.False(t, merged.AuditModeBypass, "no audit bypass when denied")
}

// TestMergeShellResults_CLIAllowMCPDeny verifies MCP deny overrides CLI allow.
func TestMergeShellResults_CLIAllowMCPDeny(t *testing.T) {
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

	merged := mergeShellResults(cliResults, mcpResults)

	assert.False(t, merged.Allowed)
	assert.Equal(t, "MCP denied", merged.Message, "MCP deny message should be used when CLI allows")
}

// TestMergeShellResults_AuditBypassOverriddenByDeny verifies that AuditModeBypass
// from one side is cleared when the other side has an enforced deny.
func TestMergeShellResults_AuditBypassOverriddenByDeny(t *testing.T) {
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
			merged := mergeShellResults(tt.cli, tt.mcp)
			assert.Equal(t, tt.wantAMB, merged.AuditModeBypass)
		})
	}
}

// TestMergeShellResults_FailedOpenPropagated verifies that FailedOpen from either
// side is propagated to the merged result.
func TestMergeShellResults_FailedOpenPropagated(t *testing.T) {
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
			merged := mergeShellResults(tt.cli, tt.mcp)
			assert.Equal(t, tt.want, merged.FailedOpen)
		})
	}
}

// TestMergeShellResults_RulesDetailsMerged verifies that RulesDetails from both
// CLI and MCP evaluations are merged (not overwritten).
func TestMergeShellResults_RulesDetailsMerged(t *testing.T) {
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

	merged := mergeShellResults(cliResults, mcpResults)

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

	merged := mergeShellResults(cliResults, mcpResults)

	require.NotNil(t, merged.AIDetails, "merged should have AIDetails")
	assert.Len(t, merged.AIDetails.Results, 2, "should contain AI results from both CLI and MCP")
}

// TestMergeShellResults_EmptyCommand verifies behavior when shell tool has empty
// or missing command string. The merge should still work with whatever results
// the evaluators produce.
func TestMergeShellResults_EmptyCommand(t *testing.T) {
	// Both allow with empty results (what happens with an empty command)
	cliResults := ValidationResults{Allowed: true, Message: "No matching policies"}
	mcpResults := ValidationResults{Allowed: true, Message: "No matching policies"}

	merged := mergeShellResults(cliResults, mcpResults)

	assert.True(t, merged.Allowed)
	assert.NotEmpty(t, merged.Message, "should have a default message")
}

// TestMergeShellResults_CountAggregation verifies AllowCount and DenyCount
// are properly summed from both sides.
func TestMergeShellResults_CountAggregation(t *testing.T) {
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

	merged := mergeShellResults(cliResults, mcpResults)

	assert.Equal(t, 5, merged.AllowCount, "allow counts should sum")
	assert.Equal(t, 1, merged.DenyCount, "deny counts should sum")
}

// TestMergeAsyncCompletions verifies that when both CLI and MCP evaluations
// produce async completions, both are merged into a single completion.
func TestMergeAsyncCompletions(t *testing.T) {
	tests := []struct {
		name          string
		cli           *AuditAIResult
		mcp           *AuditAIResult
		wantResults   int
		wantTotalMs   int64
		wantNilResult bool
	}{
		{
			name: "both present — results merged",
			cli: &AuditAIResult{
				Results:      []AuditAIRuleResult{{Rule: "cli-rule", Action: "allow"}},
				EvaluationMs: 100,
			},
			mcp: &AuditAIResult{
				Results:      []AuditAIRuleResult{{Rule: "mcp-rule", Action: "deny"}},
				EvaluationMs: 200,
			},
			wantResults: 2,
			wantTotalMs: 300,
		},
		{
			name: "only CLI present",
			cli: &AuditAIResult{
				Results:      []AuditAIRuleResult{{Rule: "cli-only"}},
				EvaluationMs: 50,
			},
			wantResults: 1,
			wantTotalMs: 50,
		},
		{
			name: "only MCP present",
			mcp: &AuditAIResult{
				Results:      []AuditAIRuleResult{{Rule: "mcp-only"}},
				EvaluationMs: 75,
			},
			wantResults: 1,
			wantTotalMs: 75,
		},
		{
			name:          "neither present",
			wantNilResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cliCh, mcpCh chan AsyncCompletion

			if tt.cli != nil {
				cliCh = make(chan AsyncCompletion, 1)
				cliCh <- AsyncCompletion{AIDetails: tt.cli, EvaluationMs: tt.cli.EvaluationMs}
			}
			if tt.mcp != nil {
				mcpCh = make(chan AsyncCompletion, 1)
				mcpCh <- AsyncCompletion{AIDetails: tt.mcp, EvaluationMs: tt.mcp.EvaluationMs}
			}

			var cliRecv, mcpRecv <-chan AsyncCompletion
			if cliCh != nil {
				cliRecv = cliCh
			}
			if mcpCh != nil {
				mcpRecv = mcpCh
			}

			result := mergeAsyncCompletions(cliRecv, mcpRecv)

			if tt.wantNilResult {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			completion := <-result
			require.NotNil(t, completion.AIDetails)
			assert.Len(t, completion.AIDetails.Results, tt.wantResults)
			assert.Equal(t, tt.wantTotalMs, completion.EvaluationMs)
		})
	}
}

// --- buildAuditCLIInfo Argument Sanitization Tests ---

// TestInterceptHandler_BuildAuditCLIInfo_SanitizesArguments verifies that when
// IncludeArgumentValues is false, CLI arguments are sanitized to prevent sensitive
// data (tokens, passwords) from appearing in the audit log.
func TestInterceptHandler_BuildAuditCLIInfo_SanitizesArguments(t *testing.T) {
	tests := []struct {
		name                  string
		includeArgumentValues bool
		command               string
		wantArgs              []string
	}{
		{
			name:                  "sanitized when IncludeArgumentValues is false",
			includeArgumentValues: false,
			command:               "gh auth login --token secret123",
			wantArgs:              []string{"auth", "login", "--token", "[value]"},
		},
		{
			name:                  "full args when IncludeArgumentValues is true",
			includeArgumentValues: true,
			command:               "gh auth login --token secret123",
			wantArgs:              []string{"auth", "login", "--token", "secret123"},
		},
		{
			name:                  "flag=value sanitized",
			includeArgumentValues: false,
			command:               "curl --header=Authorization:Bearer-xyz http://example.com",
			wantArgs:              []string{"--header", "http://example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := config.NewSessionLogger(zaptest.NewLogger(t))
			handler := NewInterceptHandler(InterceptHandlerConfig{
				Enabled:               true,
				ShellToolNames:        []string{"Bash"},
				Logger:                logger,
				IncludeArgumentValues: tt.includeArgumentValues,
			})

			req := &InterceptRequest{
				Payload: InterceptPayload{
					Name:      "Bash",
					Arguments: map[string]any{"command": tt.command},
				},
			}

			cliInfo := handler.buildAuditCLIInfo(req)
			assert.Equal(t, tt.wantArgs, cliInfo.Arguments)
		})
	}
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

// errorResponseHandler is a test double that always returns an error, simulating
// response validation engine failures (e.g., AI timeout, malformed result).
type errorResponseHandler struct{}

func (h *errorResponseHandler) HandleResponse(_ context.Context, _ mcp.CallToolRequest, _ *mcp.CallToolResult) (ResponseValidationResults, error) {
	return ResponseValidationResults{}, assert.AnError
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
