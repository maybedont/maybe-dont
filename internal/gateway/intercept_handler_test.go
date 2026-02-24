package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// --- Test Helpers ---

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

	var errResp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], contains)
}
