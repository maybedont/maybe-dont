package gateway

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestCLIValidationRequest_JSONMarshaling verifies that CLIValidationRequest
// correctly marshals to and unmarshals from JSON, preserving all fields including
// nested client_info.
func TestCLIValidationRequest_JSONMarshaling(t *testing.T) {
	req := CLIValidationRequest{
		Command:          "gh",
		Arguments:        []string{"pr", "comment", "123", "--body", "LGTM"},
		WorkingDirectory: "/home/user/project",
		ClientInfo: &CLIClientInfo{
			Hostname:   "dev-workstation",
			Username:   "developer",
			OS:         "darwin",
			Arch:       "arm64",
			Shell:      "/bin/zsh",
			CLIVersion: "1.2.0",
		},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var unmarshaled CLIValidationRequest
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, req.Command, unmarshaled.Command)
	assert.Equal(t, req.Arguments, unmarshaled.Arguments)
	assert.Equal(t, req.WorkingDirectory, unmarshaled.WorkingDirectory)
	assert.Equal(t, req.ClientInfo.Hostname, unmarshaled.ClientInfo.Hostname)
	assert.Equal(t, req.ClientInfo.Username, unmarshaled.ClientInfo.Username)
	assert.Equal(t, req.ClientInfo.OS, unmarshaled.ClientInfo.OS)
	assert.Equal(t, req.ClientInfo.Arch, unmarshaled.ClientInfo.Arch)
	assert.Equal(t, req.ClientInfo.Shell, unmarshaled.ClientInfo.Shell)
	assert.Equal(t, req.ClientInfo.CLIVersion, unmarshaled.ClientInfo.CLIVersion)
}

// TestCLIValidationResponse_AllowedJSON verifies that an allowed response marshals
// correctly with all expected fields present in the JSON output.
func TestCLIValidationResponse_AllowedJSON(t *testing.T) {
	resp := CLIValidationResponse{
		Allowed:            true,
		ValidationRequired: true,
		Message:            "Command approved by policy",
		ServerVersion:      "1.3.0",
		ClientVersion:      "1.2.0",
		Results: []CLIPolicyResult{
			{
				PolicyName: "github-cli-policy",
				PolicyType: "cel",
				Action:     "allow",
				Message:    "PR comments are permitted",
			},
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"allowed":true`)
	assert.Contains(t, string(data), `"validation_required":true`)
	assert.Contains(t, string(data), `"server_version":"1.3.0"`)
	assert.Contains(t, string(data), `"client_version":"1.2.0"`)
	assert.Contains(t, string(data), `"policy_name":"github-cli-policy"`)
	assert.Contains(t, string(data), `"policy_type":"cel"`)
}

// TestCLIValidationResponse_DeniedJSON verifies that a denied response marshals
// correctly and includes the action_reason field explaining why the command was blocked.
func TestCLIValidationResponse_DeniedJSON(t *testing.T) {
	resp := CLIValidationResponse{
		Allowed:            false,
		ValidationRequired: true,
		Message:            "Policy denied: destructive operation",
		ActionReason:       "request_policy",
		ServerVersion:      "1.3.0",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"allowed":false`)
	assert.Contains(t, string(data), `"action_reason":"request_policy"`)
	assert.Contains(t, string(data), `"validation_required":true`)
	assert.Contains(t, string(data), `"message":"Policy denied: destructive operation"`)
}

// TestCLIValidationRequest_MinimalJSON verifies that a minimal request (only required
// fields) marshals correctly and omits optional fields.
func TestCLIValidationRequest_MinimalJSON(t *testing.T) {
	req := CLIValidationRequest{
		Command:   "cat",
		Arguments: []string{"README.md"},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	// Verify required fields are present
	assert.Contains(t, string(data), `"command":"cat"`)
	assert.Contains(t, string(data), `"arguments":["README.md"]`)

	// Verify omitempty fields are absent
	assert.NotContains(t, string(data), `"working_directory"`)
	assert.NotContains(t, string(data), `"client_info"`)
}

// TestCLIValidationResponse_NoValidationRequired verifies the response structure
// when a command does not require validation.
func TestCLIValidationResponse_NoValidationRequired(t *testing.T) {
	resp := CLIValidationResponse{
		Allowed:            true,
		ValidationRequired: false,
		Message:            "Command does not require validation",
		ServerVersion:      "1.3.0",
		Results:            []CLIPolicyResult{},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"allowed":true`)
	assert.Contains(t, string(data), `"validation_required":false`)
	assert.Contains(t, string(data), `"results":[]`)
}

// TestCLIValidationError_JSON verifies that error responses marshal correctly
// with both error code and human-readable message.
func TestCLIValidationError_JSON(t *testing.T) {
	errResp := CLIValidationError{
		Error:   "missing_command",
		Message: "Required field 'command' is empty",
	}

	data, err := json.Marshal(errResp)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"error":"missing_command"`)
	assert.Contains(t, string(data), `"message":"Required field 'command' is empty"`)
}

// TestCLIClientInfo_AllFields verifies that all CLIClientInfo fields marshal correctly,
// including the optional os_version field.
func TestCLIClientInfo_AllFields(t *testing.T) {
	info := CLIClientInfo{
		Hostname:   "workstation-1",
		Username:   "developer",
		OS:         "linux",
		OSVersion:  "22.04",
		Arch:       "amd64",
		Shell:      "/bin/bash",
		CLIVersion: "1.0.0",
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var unmarshaled CLIClientInfo
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, info, unmarshaled)
}

// TestCLIPolicyResult_AIType verifies that AI policy results marshal correctly
// with the "ai" policy type.
func TestCLIPolicyResult_AIType(t *testing.T) {
	result := CLIPolicyResult{
		PolicyName: "general-safety-check",
		PolicyType: "ai",
		Action:     "deny",
		Message:    "Repository deletion could cause irreversible data loss",
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"policy_type":"ai"`)
	assert.Contains(t, string(data), `"action":"deny"`)
}

// TestHandleCLIValidation_DisabledReturns400 verifies that when CLI validation is disabled,
// the handler returns a 400 error with "cli_validation_disabled" error code.
func TestHandleCLIValidation_DisabledReturns400(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          false,
		ValidateCommands: []string{},
		Logger:           sessionLogger,
		Version:          "1.0.0",
	})

	reqBody := `{"command": "gh", "arguments": ["pr", "list"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var errResp CLIValidationError
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "cli_validation_disabled", errResp.Error)
}

// TestHandleCLIValidation_MissingCommand verifies that when the command field is empty,
// the handler returns a 400 error with "missing_command" error code.
func TestHandleCLIValidation_MissingCommand(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
	})

	reqBody := `{"command": "", "arguments": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var errResp CLIValidationError
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "missing_command", errResp.Error)
}

// TestHandleCLIValidation_CommandNotInAllowlist verifies that a command not in the
// validate_commands list returns validation_required: false and is allowed without
// policy evaluation.
func TestHandleCLIValidation_CommandNotInAllowlist(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"gh", "aws"}, // "cat" not in list
		Logger:           sessionLogger,
		Version:          "1.0.0",
	})

	reqBody := `{"command": "cat", "arguments": ["README.md"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CLIValidationResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	assert.False(t, resp.ValidationRequired)
	assert.Equal(t, "Command does not require validation", resp.Message)
}

// TestHandleCLIValidation_WildcardMatchesAll verifies that "*" in the validate_commands
// list matches all commands and requires validation.
func TestHandleCLIValidation_WildcardMatchesAll(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
		// No policy engines = allow by default
	})

	reqBody := `{"command": "any-command", "arguments": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CLIValidationResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	assert.True(t, resp.ValidationRequired)
}

// TestHandleCLIValidation_InvalidContentType verifies that when Content-Type is not
// application/json, the handler returns a 400 error with "invalid_content_type" error code.
func TestHandleCLIValidation_InvalidContentType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
	})

	reqBody := `{"command": "gh", "arguments": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var errResp CLIValidationError
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_content_type", errResp.Error)
}

// TestHandleCLIValidation_ContentTypeWithCharset verifies that Content-Type with charset
// parameter (e.g., "application/json; charset=utf-8") is accepted.
func TestHandleCLIValidation_ContentTypeWithCharset(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
	})

	reqBody := `{"command": "gh", "arguments": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CLIValidationResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
}

// TestHandleCLIValidation_InvalidJSON verifies that when the JSON body is malformed,
// the handler returns a 400 error with "invalid_request" error code.
func TestHandleCLIValidation_InvalidJSON(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
	})

	reqBody := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var errResp CLIValidationError
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_request", errResp.Error)
}

// TestHandleCLIValidation_RequestIDHeader verifies that when a X-Request-ID header
// is provided, the handler uses that value as the request ID instead of generating one.
func TestHandleCLIValidation_RequestIDHeader(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	var capturedCtx *CLIValidationContext
	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
		OnValidation: func(ctx *CLIValidationContext) {
			capturedCtx = ctx
		},
	})

	reqBody := `{"command": "gh", "arguments": ["pr", "list"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "custom-request-id-123")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedCtx, "OnValidation callback should have been called")
	assert.Equal(t, "custom-request-id-123", capturedCtx.RequestID)
}

// TestHandleCLIValidation_GeneratesRequestID verifies that when no X-Request-ID header
// is provided, the handler generates a 32-character hex string as the request ID.
func TestHandleCLIValidation_GeneratesRequestID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	var capturedCtx *CLIValidationContext
	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
		OnValidation: func(ctx *CLIValidationContext) {
			capturedCtx = ctx
		},
	})

	reqBody := `{"command": "gh", "arguments": ["pr", "list"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	// No X-Request-ID header
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedCtx, "OnValidation callback should have been called")
	assert.Len(t, capturedCtx.RequestID, 32, "Generated request ID should be 32 characters")

	// Verify it's valid hex
	_, err := hex.DecodeString(capturedCtx.RequestID)
	assert.NoError(t, err, "Generated request ID should be valid hex")
}

// TestHandleCLIValidation_ClientIDHeader verifies that the X-Maybe-Dont-Client-ID
// header is captured and available in the validation context.
func TestHandleCLIValidation_ClientIDHeader(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	var capturedCtx *CLIValidationContext
	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
		OnValidation: func(ctx *CLIValidationContext) {
			capturedCtx = ctx
		},
	})

	reqBody := `{"command": "gh", "arguments": ["pr", "list"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Maybe-Dont-Client-ID", "header-client-id")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedCtx, "OnValidation callback should have been called")
	assert.Equal(t, "header-client-id", capturedCtx.ClientID)
}

// TestHandleCLIValidation_ResponseIncludesRequestID verifies that the response JSON
// includes the request_id field with the correct value from the request header.
func TestHandleCLIValidation_ResponseIncludesRequestID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
	})

	reqBody := `{"command": "gh", "arguments": ["pr", "list"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "test-request-id")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CLIValidationResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "test-request-id", resp.RequestID)
}

// mockAuditWriter is a test implementation of AuditWriter that captures entries.
type mockAuditWriter struct {
	entries []*AuditEntry
	mu      sync.Mutex
}

func (m *mockAuditWriter) Write(entry *AuditEntry) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return true, nil
}

func (m *mockAuditWriter) Close() error {
	return nil
}

func (m *mockAuditWriter) getEntries() []*AuditEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries
}

// TestHandleCLIValidation_AuditLogWritten verifies that when an AuditWriter is configured,
// the handler writes an audit entry for each CLI validation request.
func TestHandleCLIValidation_AuditLogWritten(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	auditWriter := &mockAuditWriter{}

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
		AuditWriter:      auditWriter,
	})

	reqBody := `{"command": "gh", "arguments": ["pr", "list"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify audit entry was written
	entries := auditWriter.getEntries()
	require.Len(t, entries, 1, "Expected exactly one audit entry to be written")
}

// TestHandleCLIValidation_AuditLogNotWrittenWhenNilWriter verifies that when no AuditWriter
// is configured (nil), the handler still works and does not panic.
func TestHandleCLIValidation_AuditLogNotWrittenWhenNilWriter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
		AuditWriter:      nil, // Explicitly nil
	})

	reqBody := `{"command": "gh", "arguments": ["pr", "list"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleCLIValidation_AuditEntryPopulated verifies that the audit entry is correctly
// populated with CLI information, upstream request metadata, timing, and action.
func TestHandleCLIValidation_AuditEntryPopulated(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	auditWriter := &mockAuditWriter{}

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:               true,
		ValidateCommands:      []string{"*"},
		Logger:                sessionLogger,
		Version:               "1.0.0",
		AuditWriter:           auditWriter,
		IncludeArgumentValues: true,
	})

	reqBody := `{
		"command": "gh",
		"arguments": ["pr", "comment", "123", "--body", "LGTM"],
		"working_directory": "/home/user/project",
		"client_info": {
			"hostname": "dev-workstation",
			"username": "developer",
			"os": "darwin",
			"os_version": "24.1.0",
			"arch": "arm64",
			"shell": "/bin/zsh",
			"cli_version": "1.2.0"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "test-request-id-456")
	req.Header.Set("X-Maybe-Dont-Client-ID", "developer@company.com")
	req.Header.Set("User-Agent", "maybe-dont-cli/1.2.0")
	req.RemoteAddr = "192.168.1.100:54321"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify audit entry fields
	entries := auditWriter.getEntries()
	require.Len(t, entries, 1, "Expected exactly one audit entry")
	entry := entries[0]

	// Verify CLI info is populated (not Tool)
	assert.Nil(t, entry.Tool, "Tool should be nil for CLI validations")
	require.NotNil(t, entry.CLI, "CLI should be populated for CLI validations")
	assert.Equal(t, "gh", entry.CLI.Command)
	assert.Equal(t, []string{"pr", "comment", "123", "--body", "LGTM"}, entry.CLI.Arguments)
	assert.Equal(t, "/home/user/project", entry.CLI.WorkingDirectory)

	// Verify client info
	require.NotNil(t, entry.CLI.ClientInfo)
	assert.Equal(t, "dev-workstation", entry.CLI.ClientInfo.Hostname)
	assert.Equal(t, "developer", entry.CLI.ClientInfo.Username)
	assert.Equal(t, "darwin", entry.CLI.ClientInfo.OS)
	assert.Equal(t, "24.1.0", entry.CLI.ClientInfo.OSVersion)
	assert.Equal(t, "arm64", entry.CLI.ClientInfo.Arch)
	assert.Equal(t, "/bin/zsh", entry.CLI.ClientInfo.Shell)
	assert.Equal(t, "1.2.0", entry.CLI.ClientInfo.CLIVersion)

	// Verify upstream request info
	assert.Equal(t, "test-request-id-456", entry.UpstreamRequest.RequestID)
	assert.Equal(t, "developer@company.com", entry.UpstreamRequest.ClientID)
	assert.Equal(t, "192.168.1.100:54321", entry.UpstreamRequest.ClientIP)
	assert.Equal(t, "maybe-dont-cli/1.2.0", entry.UpstreamRequest.UserAgent)

	// Verify timing fields are populated
	assert.NotEmpty(t, entry.ValidationStarted, "ValidationStarted should be populated")
	assert.NotEmpty(t, entry.CreatedAt, "CreatedAt should be populated")
	assert.GreaterOrEqual(t, entry.DurationMs, int64(0), "DurationMs should be non-negative")

	// Verify action
	assert.Equal(t, "allow", entry.Action)
	assert.Empty(t, entry.ActionReason, "ActionReason should be empty for allowed commands")

	// Verify request validation is nil (no policy engines yet)
	assert.Nil(t, entry.RequestValidation, "RequestValidation should be nil until policy engines are integrated")
}

// TestHandleCLIValidation_AuditEntryNoValidationRequired verifies that audit entries
// are written even when the command does not require validation.
func TestHandleCLIValidation_AuditEntryNoValidationRequired(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	auditWriter := &mockAuditWriter{}

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"gh", "aws"}, // "cat" not in list
		Logger:           sessionLogger,
		Version:          "1.0.0",
		AuditWriter:      auditWriter,
	})

	reqBody := `{"command": "cat", "arguments": ["README.md"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify audit entry was written even for non-validated commands
	entries := auditWriter.getEntries()
	require.Len(t, entries, 1, "Expected audit entry even for non-validated commands")
	entry := entries[0]

	assert.Equal(t, "cat", entry.CLI.Command)
	assert.Equal(t, "allow", entry.Action)
}

// TestHandleCLIValidation_AuditEntryMinimalRequest verifies that audit entries
// are correctly populated even with minimal request data (no client_info).
func TestHandleCLIValidation_AuditEntryMinimalRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)
	auditWriter := &mockAuditWriter{}

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:          true,
		ValidateCommands: []string{"*"},
		Logger:           sessionLogger,
		Version:          "1.0.0",
		AuditWriter:      auditWriter,
	})

	reqBody := `{"command": "ls", "arguments": ["-la"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/validate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	entries := auditWriter.getEntries()
	require.Len(t, entries, 1)
	entry := entries[0]

	// Verify CLI info with minimal data
	require.NotNil(t, entry.CLI)
	assert.Equal(t, "ls", entry.CLI.Command)
	assert.Equal(t, []string{"-la"}, entry.CLI.Arguments)
	assert.Empty(t, entry.CLI.WorkingDirectory)
	assert.Nil(t, entry.CLI.ClientInfo, "ClientInfo should be nil when not provided")

	// Verify request ID was generated
	assert.NotEmpty(t, entry.UpstreamRequest.RequestID)
	assert.Len(t, entry.UpstreamRequest.RequestID, 32, "Generated request ID should be 32 characters")
}

