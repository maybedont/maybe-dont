package gateway

import (
	"encoding/hex"
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

// --- Request/Response Marshaling Tests ---

// TestActionValidationRequest_JSONMarshaling verifies that ActionValidationRequest
// correctly marshals to and unmarshals from JSON, preserving all fields including
// nested context.
func TestActionValidationRequest_JSONMarshaling(t *testing.T) {
	req := ActionValidationRequest{
		ActionType: "tool_call",
		Target:     "execute_bash",
		Parameters: map[string]any{"command": "rm -rf /tmp/important-data"},
		Actor:      "openhands-agent",
		Context: &ActionContext{
			Thought: "I need to clean up temporary files",
			Summary: "removing temporary data",
		},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var unmarshaled ActionValidationRequest
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, req.ActionType, unmarshaled.ActionType)
	assert.Equal(t, req.Target, unmarshaled.Target)
	assert.Equal(t, req.Parameters["command"], unmarshaled.Parameters["command"])
	assert.Equal(t, req.Actor, unmarshaled.Actor)
	assert.Equal(t, req.Context.Thought, unmarshaled.Context.Thought)
	assert.Equal(t, req.Context.Summary, unmarshaled.Context.Summary)
}

// TestActionValidationRequest_MinimalJSON verifies that a minimal request (only target)
// marshals correctly and omits optional fields.
func TestActionValidationRequest_MinimalJSON(t *testing.T) {
	req := ActionValidationRequest{
		Target: "list_files",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"target":"list_files"`)
	assert.NotContains(t, string(data), `"action_type"`)
	assert.NotContains(t, string(data), `"parameters"`)
	assert.NotContains(t, string(data), `"actor"`)
	assert.NotContains(t, string(data), `"context"`)
}

// TestActionValidationResponse_AllowedJSON verifies that an allowed response marshals
// correctly with risk_level and all expected fields.
func TestActionValidationResponse_AllowedJSON(t *testing.T) {
	resp := ActionValidationResponse{
		RequestID:     "abc123",
		Allowed:       true,
		RiskLevel:     RiskLevelLow,
		Message:       "Action approved by policy",
		ServerVersion: "1.3.0",
		Results: []CLIPolicyResult{
			{
				PolicyName: "safety-check",
				PolicyType: "cel",
				Action:     "allow",
				Message:    "Safe operation",
			},
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"allowed":true`)
	assert.Contains(t, string(data), `"risk_level":"low"`)
	assert.Contains(t, string(data), `"server_version":"1.3.0"`)
	assert.Contains(t, string(data), `"policy_name":"safety-check"`)
}

// TestActionValidationResponse_DeniedJSON verifies that a denied response marshals
// correctly with high risk level.
func TestActionValidationResponse_DeniedJSON(t *testing.T) {
	resp := ActionValidationResponse{
		RequestID:     "def456",
		Allowed:       false,
		RiskLevel:     RiskLevelHigh,
		Message:       "Action denied by policy",
		ServerVersion: "1.3.0",
		Results: []CLIPolicyResult{
			{
				PolicyName: "no-destructive-ops",
				PolicyType: "ai",
				Action:     "deny",
				Message:    "Destructive operation blocked",
			},
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"allowed":false`)
	assert.Contains(t, string(data), `"risk_level":"high"`)
}

// TestActionValidationError_JSON verifies that error responses marshal correctly.
func TestActionValidationError_JSON(t *testing.T) {
	errResp := ActionValidationError{
		Error:   "missing_target",
		Message: "Required field 'target' is empty",
	}

	data, err := json.Marshal(errResp)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"error":"missing_target"`)
	assert.Contains(t, string(data), `"message":"Required field 'target' is empty"`)
}

// --- Handler HTTP Tests ---

// TestHandleActionValidation_BadRequests verifies that invalid requests return 400
// with the expected error code.
func TestHandleActionValidation_BadRequests(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		contentType   string
		wantStatus    int
		wantErrorCode string
	}{
		{
			name:          "missing target",
			body:          `{"action_type": "tool_call", "target": ""}`,
			contentType:   "application/json",
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "missing_target",
		},
		{
			name:          "invalid content type",
			body:          `{"target": "execute_bash"}`,
			contentType:   "text/plain",
			wantStatus:    http.StatusUnsupportedMediaType,
			wantErrorCode: "invalid_content_type",
		},
		{
			name:          "invalid JSON",
			body:          `{invalid json}`,
			contentType:   "application/json",
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "invalid_request",
		},
	}

	handler := newTestActionHandler(t, nil, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			var errResp ActionValidationError
			err := json.Unmarshal(w.Body.Bytes(), &errResp)
			require.NoError(t, err)
			assert.Equal(t, tt.wantErrorCode, errResp.Error)
		})
	}
}

// TestHandleActionValidation_ContentTypeWithCharset verifies that Content-Type with
// charset parameter is accepted.
func TestHandleActionValidation_ContentTypeWithCharset(t *testing.T) {
	handler := newTestActionHandler(t, nil, nil)

	reqBody := `{"target": "list_files"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- Risk Level Tests ---

// TestHandleActionValidation_NoEngines_UnknownRisk verifies that when no policy engines
// are configured, the response is allowed=true with risk_level="unknown".
func TestHandleActionValidation_NoEngines_UnknownRisk(t *testing.T) {
	handler := newTestActionHandler(t, nil, nil)

	resp := sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "ls"}}`)

	assert.True(t, resp.Allowed)
	assert.Equal(t, RiskLevelUnknown, resp.RiskLevel)
	assert.Equal(t, "No validation policies configured", resp.Message)
	assert.Empty(t, resp.Results)
}

// TestHandleActionValidation_AllowedByPolicy_LowRisk verifies that when policies
// evaluate and allow the action, the response is allowed=true with risk_level="low".
func TestHandleActionValidation_AllowedByPolicy_LowRisk(t *testing.T) {
	celEngine := newTestCELEngineWithAllowRule(t)
	handler := newTestActionHandler(t, celEngine, nil)

	resp := sendActionValidation(t, handler, `{"target": "list_files", "parameters": {"path": "."}}`)

	assert.True(t, resp.Allowed)
	assert.Equal(t, RiskLevelLow, resp.RiskLevel)
	assert.NotEmpty(t, resp.Results)
}

// TestHandleActionValidation_DeniedByPolicy_HighRisk verifies that when a policy denies
// the action, the response is allowed=false with risk_level="high".
func TestHandleActionValidation_DeniedByPolicy_HighRisk(t *testing.T) {
	celEngine := newTestCELEngineWithDenyRule(t)
	handler := newTestActionHandler(t, celEngine, nil)

	resp := sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "rm -rf /tmp/important-data"}}`)

	assert.False(t, resp.Allowed)
	assert.Equal(t, RiskLevelHigh, resp.RiskLevel)
	assert.NotEmpty(t, resp.Results)
}

// TestHandleActionValidation_AuditOnlyDeny_MediumRisk verifies that when a policy
// denies in audit_only mode, the response is allowed=true with risk_level="medium".
func TestHandleActionValidation_AuditOnlyDeny_MediumRisk(t *testing.T) {
	celEngine := newTestCELEngineWithAuditOnlyDenyRule(t)
	handler := newTestActionHandler(t, celEngine, nil)

	resp := sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "rm -rf /tmp/important-data"}}`)

	assert.True(t, resp.Allowed)
	assert.Equal(t, RiskLevelMedium, resp.RiskLevel)
}

// --- Conversion Tests ---

// TestActionValidationHandler_ToCallToolRequest verifies that the handler correctly
// converts an ActionValidationRequest to an mcp.CallToolRequest.
func TestActionValidationHandler_ToCallToolRequest(t *testing.T) {
	handler := newTestActionHandler(t, nil, nil)

	tests := []struct {
		name       string
		req        *ActionValidationRequest
		wantName   string
		wantArgs   map[string]any
		hasContext bool
	}{
		{
			name: "basic request without context",
			req: &ActionValidationRequest{
				Target:     "execute_bash",
				Parameters: map[string]any{"command": "ls"},
			},
			wantName:   "execute_bash",
			wantArgs:   map[string]any{"command": "ls"},
			hasContext: false,
		},
		{
			name: "request with context includes _agent_context",
			req: &ActionValidationRequest{
				Target:     "execute_bash",
				Parameters: map[string]any{"command": "ls"},
				Context: &ActionContext{
					Thought: "I need to see files",
					Summary: "listing files",
				},
			},
			wantName:   "execute_bash",
			hasContext: true,
		},
		{
			name: "request with nil parameters and context",
			req: &ActionValidationRequest{
				Target: "list_files",
				Context: &ActionContext{
					Thought: "checking directory",
				},
			},
			wantName:   "list_files",
			hasContext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcpReq := handler.toCallToolRequest(tt.req)

			assert.Equal(t, tt.wantName, mcpReq.Params.Name)
			assert.Equal(t, "tools/call", mcpReq.Method)

			args := mcpReq.GetArguments()
			if tt.hasContext {
				assert.Contains(t, args, "_agent_context")
				agentCtx, ok := args["_agent_context"].(map[string]any)
				require.True(t, ok)
				if tt.req.Context.Thought != "" {
					assert.Equal(t, tt.req.Context.Thought, agentCtx["thought"])
				}
				if tt.req.Context.Summary != "" {
					assert.Equal(t, tt.req.Context.Summary, agentCtx["summary"])
				}
			} else if tt.wantArgs != nil {
				for k, v := range tt.wantArgs {
					assert.Equal(t, v, args[k])
				}
				assert.NotContains(t, args, "_agent_context")
			}
		})
	}
}

// TestActionValidationHandler_ToCallToolRequest_DoesNotMutateOriginal verifies that
// the conversion does not modify the original request's parameters map.
func TestActionValidationHandler_ToCallToolRequest_DoesNotMutateOriginal(t *testing.T) {
	handler := newTestActionHandler(t, nil, nil)

	originalParams := map[string]any{"command": "ls"}
	req := &ActionValidationRequest{
		Target:     "execute_bash",
		Parameters: originalParams,
		Context: &ActionContext{
			Thought: "checking files",
		},
	}

	_ = handler.toCallToolRequest(req)

	// Original parameters should not be modified
	assert.NotContains(t, originalParams, "_agent_context")
	assert.Len(t, originalParams, 1)
}

// --- Header Extraction Tests ---

// TestHandleActionValidation_RequestIDHeader verifies that the X-Request-ID header
// is used as the request ID in the response.
func TestHandleActionValidation_RequestIDHeader(t *testing.T) {
	var capturedCtx *CLIValidationContext
	handler := newTestActionHandlerWithCallback(t, nil, nil, func(ctx *CLIValidationContext) {
		capturedCtx = ctx
	})

	reqBody := `{"target": "list_files"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "custom-request-id-123")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedCtx)
	assert.Equal(t, "custom-request-id-123", capturedCtx.RequestID)

	var resp ActionValidationResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "custom-request-id-123", resp.RequestID)
}

// TestHandleActionValidation_GeneratesRequestID verifies that when no X-Request-ID header
// is provided, the handler generates a 32-character hex string.
func TestHandleActionValidation_GeneratesRequestID(t *testing.T) {
	var capturedCtx *CLIValidationContext
	handler := newTestActionHandlerWithCallback(t, nil, nil, func(ctx *CLIValidationContext) {
		capturedCtx = ctx
	})

	reqBody := `{"target": "list_files"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedCtx)
	assert.Len(t, capturedCtx.RequestID, 32)

	_, err := hex.DecodeString(capturedCtx.RequestID)
	assert.NoError(t, err, "Generated request ID should be valid hex")
}

// TestHandleActionValidation_ClientIDFromHeader verifies that the X-Maybe-Dont-Client-ID
// header is captured in the validation context.
func TestHandleActionValidation_ClientIDFromHeader(t *testing.T) {
	var capturedCtx *CLIValidationContext
	handler := newTestActionHandlerWithCallback(t, nil, nil, func(ctx *CLIValidationContext) {
		capturedCtx = ctx
	})

	reqBody := `{"target": "list_files"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Maybe-Dont-Client-ID", "header-client-id")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedCtx)
	assert.Equal(t, "header-client-id", capturedCtx.ClientID)
}

// TestHandleActionValidation_ActorFallsBackToClientID verifies that when no
// X-Maybe-Dont-Client-ID header is present, the actor field from the request body
// is used as the client ID.
func TestHandleActionValidation_ActorFallsBackToClientID(t *testing.T) {
	var capturedCtx *CLIValidationContext
	handler := newTestActionHandlerWithCallback(t, nil, nil, func(ctx *CLIValidationContext) {
		capturedCtx = ctx
	})

	reqBody := `{"target": "list_files", "actor": "openhands-agent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedCtx)
	assert.Equal(t, "openhands-agent", capturedCtx.ClientID)
}

// TestHandleActionValidation_HeaderClientIDTakesPrecedence verifies that the
// X-Maybe-Dont-Client-ID header takes precedence over the actor field.
func TestHandleActionValidation_HeaderClientIDTakesPrecedence(t *testing.T) {
	var capturedCtx *CLIValidationContext
	handler := newTestActionHandlerWithCallback(t, nil, nil, func(ctx *CLIValidationContext) {
		capturedCtx = ctx
	})

	reqBody := `{"target": "list_files", "actor": "body-actor"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Maybe-Dont-Client-ID", "header-client-id")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedCtx)
	assert.Equal(t, "header-client-id", capturedCtx.ClientID)
}

// --- Audit Logging Tests ---

// TestHandleActionValidation_AuditLogWritten verifies that when an AuditWriter is configured,
// the handler writes an audit entry for each action validation request.
func TestHandleActionValidation_AuditLogWritten(t *testing.T) {
	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, nil, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "ls"}}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "execute_bash", entry.Tool.Name)
	assert.Equal(t, "execute_bash", entry.Tool.PrefixedName)
	assert.Equal(t, "allow", entry.Action)
	assert.NotEmpty(t, entry.ValidationStarted)
	assert.NotEmpty(t, entry.CreatedAt)
}

// TestHandleActionValidation_AuditLogNotWrittenWhenNilWriter verifies that when no
// AuditWriter is configured, the handler still works without panicking.
func TestHandleActionValidation_AuditLogNotWrittenWhenNilWriter(t *testing.T) {
	handler := newTestActionHandler(t, nil, nil) // nil audit writer

	reqBody := `{"target": "list_files"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleActionValidation_AuditLogIncludesClientID verifies that the audit entry
// includes the client ID from the actor field when no header is present.
func TestHandleActionValidation_AuditLogIncludesClientID(t *testing.T) {
	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, nil, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "execute_bash", "actor": "openhands-agent"}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "openhands-agent", entries[0].UpstreamRequest.ClientID)
}

// TestHandleActionValidation_AuditLogSourceIsAction verifies that audit log entries
// have source set to "action" to distinguish from MCP and CLI entries.
func TestHandleActionValidation_AuditLogSourceIsAction(t *testing.T) {
	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, nil, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "ls"}}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "action", entries[0].Source)
}

// TestHandleActionValidation_ExternalIDInAuditLog verifies that the external_id field
// from the request body is included in the audit log for caller-side correlation.
func TestHandleActionValidation_ExternalIDInAuditLog(t *testing.T) {
	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, nil, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "execute_bash", "external_id": "42"}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "42", entries[0].UpstreamRequest.ExternalID)
}

// TestHandleActionValidation_AuditLogOmitsParamsWhenDisabled verifies that when
// IncludeArgumentValues is false, parameters are not included in audit entries.
func TestHandleActionValidation_AuditLogOmitsParamsWhenDisabled(t *testing.T) {
	auditWriter := &mockAuditWriter{}
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewActionValidationHandler(ActionValidationHandlerConfig{
		Logger:                sessionLogger,
		Version:               "1.0.0-test",
		AuditWriter:           auditWriter,
		IncludeArgumentValues: false,
	})

	_ = sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "rm -rf /tmp/important-data"}}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Nil(t, entries[0].Tool.Params, "Params should be nil when IncludeArgumentValues is false")
}

// TestHandleActionValidation_AuditLogIncludesParamsWhenEnabled verifies that when
// IncludeArgumentValues is true, parameters are included in audit entries.
func TestHandleActionValidation_AuditLogIncludesParamsWhenEnabled(t *testing.T) {
	auditWriter := &mockAuditWriter{}
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewActionValidationHandler(ActionValidationHandlerConfig{
		Logger:                sessionLogger,
		Version:               "1.0.0-test",
		AuditWriter:           auditWriter,
		IncludeArgumentValues: true,
	})

	_ = sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "rm -rf /tmp/important-data"}}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Tool.Params, "Params should be set when IncludeArgumentValues is true")
	assert.Equal(t, "rm -rf /tmp/important-data", entries[0].Tool.Params["command"])
}

// TestHandleActionValidation_ExternalIDOmittedWhenEmpty verifies that when no external_id
// is provided, the field is omitted from the audit log (empty string).
func TestHandleActionValidation_ExternalIDOmittedWhenEmpty(t *testing.T) {
	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, nil, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "list_files"}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Empty(t, entries[0].UpstreamRequest.ExternalID)
}

// --- Audit Correlation Field Tests ---

// TestHandleActionValidation_AuditCorrelationFields verifies that all correlation fields
// (request_id, client_ip, user_agent, external_id, client_id) are correctly captured
// in audit entries from HTTP headers and request body fields.
func TestHandleActionValidation_AuditCorrelationFields(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		headers        map[string]string
		wantRequestID  string
		wantClientID   string
		wantExternalID string
		wantClientIP   string
		wantUserAgent  string
		wantAutoGenID  bool
	}{
		{
			name: "all correlation fields from headers and body",
			body: `{"target": "execute_bash", "actor": "body-actor", "external_id": "ext-123"}`,
			headers: map[string]string{
				"X-Request-ID":           "action-req-id-001",
				"X-Maybe-Dont-Client-ID": "header-client-id",
				"User-Agent":             "openhands-agent/3.0",
			},
			wantRequestID:  "action-req-id-001",
			wantClientID:   "header-client-id",
			wantExternalID: "ext-123",
			wantClientIP:   "10.0.0.1:9999",
			wantUserAgent:  "openhands-agent/3.0",
		},
		{
			name: "client_id falls back to actor when header absent",
			body: `{"target": "list_files", "actor": "fallback-agent"}`,
			headers: map[string]string{
				"X-Request-ID": "action-req-id-002",
			},
			wantRequestID: "action-req-id-002",
			wantClientID:  "fallback-agent",
			wantClientIP:  "10.0.0.1:9999",
		},
		{
			name:          "auto-generated request_id when no header",
			body:          `{"target": "list_files"}`,
			headers:       map[string]string{},
			wantAutoGenID: true,
			wantClientIP:  "10.0.0.1:9999",
		},
		{
			name: "user_agent captured from header",
			body: `{"target": "list_files"}`,
			headers: map[string]string{
				"X-Request-ID": "action-req-id-003",
				"User-Agent":   "custom-agent/1.0",
			},
			wantRequestID: "action-req-id-003",
			wantUserAgent: "custom-agent/1.0",
			wantClientIP:  "10.0.0.1:9999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auditWriter := &mockAuditWriter{}
			handler := newTestActionHandlerWithAudit(t, nil, nil, auditWriter)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "10.0.0.1:9999"
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)

			entries := auditWriter.getEntries()
			require.Len(t, entries, 1)

			entry := entries[0]
			assert.Equal(t, "action", entry.Source)

			if tt.wantAutoGenID {
				assert.Len(t, entry.UpstreamRequest.RequestID, 32, "auto-generated ID should be 32-char hex")
			} else {
				assert.Equal(t, tt.wantRequestID, entry.UpstreamRequest.RequestID)
			}

			if tt.wantClientID != "" {
				assert.Equal(t, tt.wantClientID, entry.UpstreamRequest.ClientID)
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

// --- Audit Entry Completeness Tests ---

// TestHandleActionValidation_AuditEntry_CELRulesDetails verifies that when a real
// CEL engine evaluates an action request, the audit entry's RequestValidation.CEL
// field is populated with rule evaluation details including rule name and timing.
func TestHandleActionValidation_AuditEntry_CELRulesDetails(t *testing.T) {
	celEngine := newTestCELEngineWithDenyRule(t)
	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, celEngine, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "rm -rf /"}}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "deny", entry.Action)
	require.NotNil(t, entry.RequestValidation, "should have RequestValidation for CEL deny")
	require.NotNil(t, entry.RequestValidation.CEL, "should have CEL details")
	assert.Equal(t, "deny", entry.RequestValidation.CEL.Action)
	assert.Equal(t, "deny-all", entry.RequestValidation.CEL.DecidingRule)
	assert.NotEmpty(t, entry.RequestValidation.CEL.Results)
	assert.Greater(t, entry.RequestValidation.CEL.Results[0].EvaluationMs, int64(-1),
		"per-rule duration should be non-negative")
}

// TestHandleActionValidation_AuditEntry_FailedOpen verifies that when a CEL rule
// causes a runtime error and fails open, the audit entry's action_reason is "fail_open".
func TestHandleActionValidation_AuditEntry_FailedOpen(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	celEngine, err := NewCELPolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)
	err = celEngine.LoadPolicies([]config.Policy{
		{
			Name:       "runtime-error",
			Expression: `request.params.arguments.nonexistent.deep == "crash"`,
			Action:     config.PolicyActionDeny,
			Message:    "should not reach here",
		},
	}, config.PolicyModeEnforce)
	require.NoError(t, err)

	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, celEngine, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "list_files"}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "allow", entry.Action, "fail-open should result in allow")
	assert.Equal(t, "fail_open", entry.ActionReason, "should have fail_open action reason")
}

// TestHandleActionValidation_AuditEntry_AuditModeBypass verifies that when a CEL rule
// is in audit_only mode and would deny, the audit entry's action_reason is "audit_mode".
func TestHandleActionValidation_AuditEntry_AuditModeBypass(t *testing.T) {
	celEngine := newTestCELEngineWithAuditOnlyDenyRule(t)
	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, celEngine, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "rm -rf /"}}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "allow", entry.Action, "audit_only mode should still allow")
	assert.Equal(t, "audit_mode", entry.ActionReason, "should have audit_mode action reason")
}

// TestHandleActionValidation_AuditEntry_TimingFields verifies that validation timing
// fields (validation_started, created_at, duration_ms) and per-rule evaluation_ms
// are populated with valid values in audit entries.
func TestHandleActionValidation_AuditEntry_TimingFields(t *testing.T) {
	celEngine := newTestCELEngineWithDenyRule(t)
	auditWriter := &mockAuditWriter{}
	handler := newTestActionHandlerWithAudit(t, celEngine, nil, auditWriter)

	_ = sendActionValidation(t, handler, `{"target": "execute_bash"}`)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.NotEmpty(t, entry.ValidationStarted, "ValidationStarted should be set")
	assert.NotEmpty(t, entry.CreatedAt, "CreatedAt should be set")
	assert.GreaterOrEqual(t, entry.DurationMs, int64(0), "DurationMs should be non-negative")

	require.NotNil(t, entry.RequestValidation)
	require.NotNil(t, entry.RequestValidation.CEL)
	assert.GreaterOrEqual(t, entry.RequestValidation.CEL.EvaluationMs, int64(0),
		"CEL evaluation_ms should be non-negative")
}

// --- Response Format Tests ---

// TestHandleActionValidation_ResponseIncludesServerVersion verifies the server_version
// field is present in the response.
func TestHandleActionValidation_ResponseIncludesServerVersion(t *testing.T) {
	handler := newTestActionHandler(t, nil, nil)

	resp := sendActionValidation(t, handler, `{"target": "list_files"}`)

	assert.Equal(t, "1.0.0-test", resp.ServerVersion)
}

// TestHandleActionValidation_ResponseIncludesEmptyResults verifies that when no policies
// are configured, results is an empty array (not null).
func TestHandleActionValidation_ResponseIncludesEmptyResults(t *testing.T) {
	handler := newTestActionHandler(t, nil, nil)

	reqBody := `{"target": "list_files"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify results is an empty array, not null
	body := w.Body.String()
	assert.Contains(t, body, `"results":[]`)
}

// --- Risk Level Derivation Unit Tests ---

// TestDeriveRiskLevel_TableDriven verifies risk level derivation for all cases.
func TestDeriveRiskLevel_TableDriven(t *testing.T) {
	handler := newTestActionHandler(t, nil, nil)

	tests := []struct {
		name     string
		results  ValidationResults
		expected RiskLevel
	}{
		{
			name: "denied action → high",
			results: ValidationResults{
				Allowed: false,
				Results: []ValidationResult{{PolicyName: "test", Action: "deny"}},
			},
			expected: RiskLevelHigh,
		},
		{
			name: "audit_only bypass → medium",
			results: ValidationResults{
				Allowed:         true,
				AuditModeBypass: true,
				Results:         []ValidationResult{{PolicyName: "test", Action: "deny"}},
			},
			expected: RiskLevelMedium,
		},
		{
			name: "allowed with results → low",
			results: ValidationResults{
				Allowed: true,
				Results: []ValidationResult{{PolicyName: "test", Action: "allow"}},
			},
			expected: RiskLevelLow,
		},
		{
			name: "no results (no engines) → unknown",
			results: ValidationResults{
				Allowed: true,
				Results: []ValidationResult{},
			},
			expected: RiskLevelUnknown,
		},
		{
			name: "failed open with no results → unknown",
			results: ValidationResults{
				Allowed:    true,
				FailedOpen: true,
				Results:    []ValidationResult{},
			},
			expected: RiskLevelUnknown,
		},
		{
			name: "AI engine ran (AIDetails present, no legacy Results) → low",
			results: ValidationResults{
				Allowed:   true,
				Results:   []ValidationResult{},
				AIDetails: &AuditAIResult{Action: "allow"},
			},
			expected: RiskLevelLow,
		},
		{
			name: "CEL engine ran (RulesDetails present, no legacy Results) → low",
			results: ValidationResults{
				Allowed:      true,
				Results:      []ValidationResult{},
				RulesDetails: &AuditRulesResult{Action: "allow"},
			},
			expected: RiskLevelLow,
		},
		{
			name: "AI async audit_only (AllowCount > 0, no details yet) → low",
			results: ValidationResults{
				Allowed:    true,
				Results:    []ValidationResult{},
				AllowCount: 1,
			},
			expected: RiskLevelLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.deriveRiskLevel(tt.results)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// --- Test Helpers ---

// newTestActionHandler creates an ActionValidationHandler for testing with optional engines.
func newTestActionHandler(t *testing.T, celEngine *CELPolicyEngine, aiEngine *AIPolicyEngine) *ActionValidationHandler {
	t.Helper()
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	var evaluator *PolicyEvaluator
	if celEngine != nil || aiEngine != nil {
		evaluator = &PolicyEvaluator{
			CELEngine: celEngine,
			AIEngine:  aiEngine,
			Logger:    sessionLogger,
		}
	}

	return NewActionValidationHandler(ActionValidationHandlerConfig{
		Logger:    sessionLogger,
		Version:   "1.0.0-test",
		Evaluator: evaluator,
	})
}

// newTestActionHandlerWithCallback creates a handler with an OnValidation callback.
func newTestActionHandlerWithCallback(
	t *testing.T,
	celEngine *CELPolicyEngine,
	aiEngine *AIPolicyEngine,
	onValidation func(*CLIValidationContext),
) *ActionValidationHandler {
	t.Helper()
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	var evaluator *PolicyEvaluator
	if celEngine != nil || aiEngine != nil {
		evaluator = &PolicyEvaluator{
			CELEngine: celEngine,
			AIEngine:  aiEngine,
			Logger:    sessionLogger,
		}
	}

	return NewActionValidationHandler(ActionValidationHandlerConfig{
		Logger:       sessionLogger,
		Version:      "1.0.0-test",
		Evaluator:    evaluator,
		OnValidation: onValidation,
	})
}

// newTestActionHandlerWithAudit creates a handler with an AuditWriter.
func newTestActionHandlerWithAudit(
	t *testing.T,
	celEngine *CELPolicyEngine,
	aiEngine *AIPolicyEngine,
	auditWriter AuditWriter,
) *ActionValidationHandler {
	t.Helper()
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	var evaluator *PolicyEvaluator
	if celEngine != nil || aiEngine != nil {
		evaluator = &PolicyEvaluator{
			CELEngine: celEngine,
			AIEngine:  aiEngine,
			Logger:    sessionLogger,
		}
	}

	return NewActionValidationHandler(ActionValidationHandlerConfig{
		Logger:      sessionLogger,
		Version:     "1.0.0-test",
		Evaluator:   evaluator,
		AuditWriter: auditWriter,
	})
}

// sendActionValidation sends a JSON body to the handler and returns the parsed response.
func sendActionValidation(t *testing.T, handler *ActionValidationHandler, body string) ActionValidationResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/action/validate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "Expected HTTP 200, got %d: %s", w.Code, w.Body.String())

	var resp ActionValidationResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	return resp
}

// newTestCELEngineWithAllowRule creates a CEL engine with a single allow rule
// that matches any tool call.
func newTestCELEngineWithAllowRule(t *testing.T) *CELPolicyEngine {
	t.Helper()

	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	engine, err := NewCELPolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)

	rules := []config.Policy{
		{
			Name:       "allow-all",
			Expression: "true",
			Action:     config.PolicyActionAllow,
			Message:    "Allowed by test rule",
		},
	}
	err = engine.LoadPolicies(rules, config.PolicyModeEnforce)
	require.NoError(t, err)

	return engine
}

// newTestCELEngineWithDenyRule creates a CEL engine with a single deny rule
// that matches any tool call.
func newTestCELEngineWithDenyRule(t *testing.T) *CELPolicyEngine {
	t.Helper()

	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	engine, err := NewCELPolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)

	rules := []config.Policy{
		{
			Name:       "deny-all",
			Expression: "true",
			Action:     config.PolicyActionDeny,
			Message:    "Denied by test rule",
		},
	}
	err = engine.LoadPolicies(rules, config.PolicyModeEnforce)
	require.NoError(t, err)

	return engine
}

// --- Cross-Engine Truth Table Test ---

// TestActionValidation_CrossEngineTruthTable verifies that all combinations of
// CEL and AI engine results produce the correct Allowed, AuditModeBypass, and risk level.
// This ensures the merging logic in evaluatePolicies handles every permutation correctly.
func TestActionValidation_CrossEngineTruthTable(t *testing.T) {
	// Engine result types used in the truth table
	type engineResult string
	const (
		resultNone          engineResult = "none"            // Engine not configured (nil)
		resultAllow         engineResult = "allow"           // Enforced allow rule matches
		resultDenyEnforced  engineResult = "deny_enforced"   // Enforced deny rule matches
		resultDenyAuditOnly engineResult = "deny_audit_only" // Audit_only deny rule matches
	)

	tests := []struct {
		name                string
		celResult           engineResult
		aiResult            engineResult
		wantAllowed         bool
		wantAuditModeBypass bool
		wantRiskLevel       RiskLevel
	}{
		// Both engines: no engines configured
		{
			name:          "no_engines → unknown",
			celResult:     resultNone,
			aiResult:      resultNone,
			wantAllowed:   true,
			wantRiskLevel: RiskLevelUnknown,
		},
		// Single engine: CEL only
		{
			name:          "cel_allow + no_ai → low",
			celResult:     resultAllow,
			aiResult:      resultNone,
			wantAllowed:   true,
			wantRiskLevel: RiskLevelLow,
		},
		{
			name:          "cel_deny_enforced + no_ai → high",
			celResult:     resultDenyEnforced,
			aiResult:      resultNone,
			wantAllowed:   false,
			wantRiskLevel: RiskLevelHigh,
		},
		{
			name:                "cel_deny_audit_only + no_ai → medium",
			celResult:           resultDenyAuditOnly,
			aiResult:            resultNone,
			wantAllowed:         true,
			wantAuditModeBypass: true,
			wantRiskLevel:       RiskLevelMedium,
		},
		// Single engine: AI only
		{
			name:          "no_cel + ai_allow → low",
			celResult:     resultNone,
			aiResult:      resultAllow,
			wantAllowed:   true,
			wantRiskLevel: RiskLevelLow,
		},
		{
			name:          "no_cel + ai_deny_enforced → high",
			celResult:     resultNone,
			aiResult:      resultDenyEnforced,
			wantAllowed:   false,
			wantRiskLevel: RiskLevelHigh,
		},
		{
			// AI audit_only rules complete asynchronously — the deny result is not known
			// at response time, so AuditModeBypass is not set. Risk is "low" (engine ran,
			// no blocking decision at response time). The deny appears in the async audit log.
			name:          "no_cel + ai_deny_audit_only → low (async, not known at response)",
			celResult:     resultNone,
			aiResult:      resultDenyAuditOnly,
			wantAllowed:   true,
			wantRiskLevel: RiskLevelLow,
		},
		// Both engines: allow combinations
		{
			name:          "cel_allow + ai_allow → low",
			celResult:     resultAllow,
			aiResult:      resultAllow,
			wantAllowed:   true,
			wantRiskLevel: RiskLevelLow,
		},
		// Both engines: enforced deny from either takes precedence
		{
			name:          "cel_allow + ai_deny_enforced → high",
			celResult:     resultAllow,
			aiResult:      resultDenyEnforced,
			wantAllowed:   false,
			wantRiskLevel: RiskLevelHigh,
		},
		{
			name:          "cel_deny_enforced + ai_allow → high",
			celResult:     resultDenyEnforced,
			aiResult:      resultAllow,
			wantAllowed:   false,
			wantRiskLevel: RiskLevelHigh,
		},
		{
			name:          "cel_deny_enforced + ai_deny_enforced → high",
			celResult:     resultDenyEnforced,
			aiResult:      resultDenyEnforced,
			wantAllowed:   false,
			wantRiskLevel: RiskLevelHigh,
		},
		// Both engines: audit_only deny from CEL + allow from AI → medium (CEL is synchronous)
		{
			name:                "cel_deny_audit_only + ai_allow → medium",
			celResult:           resultDenyAuditOnly,
			aiResult:            resultAllow,
			wantAllowed:         true,
			wantAuditModeBypass: true,
			wantRiskLevel:       RiskLevelMedium,
		},
		// AI audit_only deny is async — not known at response time, so no bypass from AI
		{
			name:          "cel_allow + ai_deny_audit_only → low (AI async)",
			celResult:     resultAllow,
			aiResult:      resultDenyAuditOnly,
			wantAllowed:   true,
			wantRiskLevel: RiskLevelLow,
		},
		// Both audit_only: CEL is synchronous (sets bypass), AI is async (does not)
		{
			name:                "cel_deny_audit_only + ai_deny_audit_only → medium (from CEL)",
			celResult:           resultDenyAuditOnly,
			aiResult:            resultDenyAuditOnly,
			wantAllowed:         true,
			wantAuditModeBypass: true,
			wantRiskLevel:       RiskLevelMedium,
		},
		// Both engines: enforced deny from either + audit_only deny from other → high
		{
			name:          "cel_deny_enforced + ai_deny_audit_only → high",
			celResult:     resultDenyEnforced,
			aiResult:      resultDenyAuditOnly,
			wantAllowed:   false,
			wantRiskLevel: RiskLevelHigh,
		},
		{
			name:                "cel_deny_audit_only + ai_deny_enforced → high (enforced wins, bypass cleared)",
			celResult:           resultDenyAuditOnly,
			aiResult:            resultDenyEnforced,
			wantAllowed:         false,
			wantAuditModeBypass: false, // Enforced deny clears bypass set by CEL
			wantRiskLevel:       RiskLevelHigh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var celEngine *CELPolicyEngine
			var aiEngine *AIPolicyEngine

			switch tt.celResult {
			case resultAllow:
				celEngine = newTestCELEngineWithAllowRule(t)
			case resultDenyEnforced:
				celEngine = newTestCELEngineWithDenyRule(t)
			case resultDenyAuditOnly:
				celEngine = newTestCELEngineWithAuditOnlyDenyRule(t)
			case resultNone:
				celEngine = nil
			}

			switch tt.aiResult {
			case resultAllow:
				aiEngine = newTestAIEngineWithAllowRule(t)
			case resultDenyEnforced:
				aiEngine = newTestAIEngineWithDenyRule(t)
			case resultDenyAuditOnly:
				aiEngine = newTestAIEngineWithAuditOnlyDenyRule(t)
			case resultNone:
				aiEngine = nil
			}

			// Wait for async audit goroutines to finish before the subtest's
			// testing.T is invalidated, preventing a data race on the test logger.
			if aiEngine != nil {
				t.Cleanup(aiEngine.WaitForAsync)
			}

			handler := newTestActionHandler(t, celEngine, aiEngine)
			resp := sendActionValidation(t, handler, `{"target": "execute_bash", "parameters": {"command": "test"}}`)

			assert.Equal(t, tt.wantAllowed, resp.Allowed, "Allowed")
			assert.Equal(t, tt.wantRiskLevel, resp.RiskLevel, "RiskLevel")

			// AuditModeBypass is not exposed in the response, so verify via risk level:
			// medium risk implies AuditModeBypass=true, non-medium with allowed=true implies false
			if tt.wantAuditModeBypass {
				assert.Equal(t, RiskLevelMedium, resp.RiskLevel,
					"AuditModeBypass should produce medium risk")
			} else if tt.wantAllowed && tt.wantRiskLevel != RiskLevelUnknown {
				assert.NotEqual(t, RiskLevelMedium, resp.RiskLevel,
					"No AuditModeBypass should not produce medium risk")
			}
		})
	}
}

// --- AI Engine Test Helpers ---

// newTestAIEngineWithAllowRule creates an AI engine with a mock that returns allow.
func newTestAIEngineWithAllowRule(t *testing.T) *AIPolicyEngine {
	t.Helper()
	return newTestAIEngineWithMockResponse(t, true, "Allowed by AI", config.PolicyModeEnforce)
}

// newTestAIEngineWithDenyRule creates an AI engine with a mock that returns deny (enforced).
func newTestAIEngineWithDenyRule(t *testing.T) *AIPolicyEngine {
	t.Helper()
	return newTestAIEngineWithMockResponse(t, false, "Denied by AI", config.PolicyModeEnforce)
}

// newTestAIEngineWithAuditOnlyDenyRule creates an AI engine with a mock that returns deny
// in audit_only mode.
func newTestAIEngineWithAuditOnlyDenyRule(t *testing.T) *AIPolicyEngine {
	t.Helper()
	return newTestAIEngineWithMockResponse(t, false, "Would deny by AI", config.PolicyModeAuditOnly)
}

// newTestAIEngineWithMockResponse creates an AI engine with a single deny rule
// and a mock provider that returns the specified response.
func newTestAIEngineWithMockResponse(t *testing.T, allowed bool, message string, mode config.PolicyMode) *AIPolicyEngine {
	t.Helper()

	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Build mock response JSON
	allowedStr := "false"
	if allowed {
		allowedStr = "true"
	}
	responseJSON := `{"allowed":` + allowedStr + `,"message":"` + message + `"}`

	mock := NewMockAIProviderClient()
	mock.SetResponse(AICompletionResult{
		RawText:           responseJSON,
		ParsedJSON:        json.RawMessage(responseJSON),
		ProviderRequestID: "mock-req-id",
	})

	cfg := &config.Config{}
	cfg.Validation.AI.Model = "mock-model"

	engine := &AIPolicyEngine{
		cfg:                 cfg,
		maxRuleEvaluationMs: 45000,
		providerClient:      mock,
	}
	err := InitAIPolicyEngine(sessionLogger, engine)
	require.NoError(t, err)

	// Load a deny rule — the mock response controls the outcome
	rules := []config.AIPolicy{
		{
			Name:   "test-ai-rule",
			Prompt: "Is this operation safe?",
			Action: config.PolicyActionDeny,
		},
	}
	err = engine.LoadPolicies(rules, mode)
	require.NoError(t, err)

	return engine
}

// newTestCELEngineWithAuditOnlyDenyRule creates a CEL engine with a deny rule
// in audit_only mode, which should result in AuditModeBypass.
func newTestCELEngineWithAuditOnlyDenyRule(t *testing.T) *CELPolicyEngine {
	t.Helper()

	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	engine, err := NewCELPolicyEngine(t.Context(), sessionLogger)
	require.NoError(t, err)

	rules := []config.Policy{
		{
			Name:       "audit-deny",
			Expression: "true",
			Action:     config.PolicyActionDeny,
			Message:    "Would deny but in audit mode",
		},
	}
	err = engine.LoadPolicies(rules, config.PolicyModeAuditOnly)
	require.NoError(t, err)

	return engine
}
