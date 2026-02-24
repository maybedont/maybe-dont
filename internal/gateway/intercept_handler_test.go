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
