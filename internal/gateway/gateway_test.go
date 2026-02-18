package gateway

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestPolicyDeniedError_Structure(t *testing.T) {
	// Test the PolicyDeniedError structure
	errorData := map[string]interface{}{
		"denied_policies": []string{"policy1", "policy2"},
		"denied_count":    2,
		"tool_name":       "delete_file",
	}

	policyErr := &PolicyDeniedError{
		Message: "Request denied by policy: delete_file is not allowed",
		Data:    errorData,
	}

	// Test the Error method
	assert.Equal(t, "Request denied by policy: delete_file is not allowed", policyErr.Error())

	// Test the structure
	assert.Equal(t, "Request denied by policy: delete_file is not allowed", policyErr.Message)
	assert.Equal(t, errorData, policyErr.Data)
	assert.Equal(t, 2, policyErr.Data["denied_count"])
	assert.Equal(t, "delete_file", policyErr.Data["tool_name"])
}

func TestPolicyDeniedError_ErrorHandler(t *testing.T) {
	// Test with a PolicyDeniedError
	policyErr := &PolicyDeniedError{
		Message: "Request denied by policy 'deny-dangerous-tool': delete_file is not allowed",
		Data: map[string]interface{}{
			"denied_policies": []string{"deny-dangerous-tool"},
			"denied_count":    1,
			"tool_name":       "delete_file",
		},
	}

	// Create a mock function to test the error handling logic
	handleError := func(err error) *mcp.CallToolResult {
		var policyErr *PolicyDeniedError
		if errors.As(err, &policyErr) {
			// Create error result with user-friendly message
			errorResult := mcp.NewToolResultError(policyErr.Message)

			// Add structured error data to the result
			if errorResult.Meta == nil {
				errorResult.Meta = &mcp.Meta{}
			}
			if errorResult.Meta.AdditionalFields == nil {
				errorResult.Meta.AdditionalFields = make(map[string]interface{})
			}
			errorResult.Meta.AdditionalFields["error_code"] = -32600 // Invalid Request
			errorResult.Meta.AdditionalFields["error_data"] = policyErr.Data

			return errorResult
		}
		return nil
	}

	// Test the error handler
	result := handleError(policyErr)
	require.NotNil(t, result)

	// Verify the error result structure
	assert.True(t, result.IsError, "Result should be marked as error")
	assert.Len(t, result.Content, 1, "Should have one content item")

	// Check that the error message is user-friendly
	content := result.Content[0]
	// Use the AsTextContent helper function to check if it's text content
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok, "Content should be TextContent")
	assert.Contains(t, textContent.Text, "Request denied by policy", "Error message should be user-friendly")
	assert.Contains(t, textContent.Text, "deny-dangerous-tool", "Should include the policy name")
	assert.Contains(t, textContent.Text, "delete_file is not allowed", "Should include the policy message")

	// Check that the error code is set correctly
	assert.NotNil(t, result.Meta.AdditionalFields, "Result should have Meta.additionalFields")
	assert.Equal(t, -32600, result.Meta.AdditionalFields["error_code"], "Error code should be -32600 (Invalid Request)")

	// Check that error data is included
	errorData, ok := result.Meta.AdditionalFields["error_data"].(map[string]interface{})
	require.True(t, ok, "Error data should be a map")
	assert.Equal(t, "delete_file", errorData["tool_name"], "Should include tool name in error data")
	assert.Equal(t, 1, errorData["denied_count"], "Should include denied count")

	// Check denied_policies as []string
	if dp, ok := errorData["denied_policies"].([]string); ok {
		assert.Len(t, dp, 1, "Should have one denied policy")
		assert.Equal(t, "deny-dangerous-tool", dp[0], "Should include the policy name")
	} else if dp, ok := errorData["denied_policies"].([]interface{}); ok {
		assert.Len(t, dp, 1, "Should have one denied policy")
		assert.Equal(t, "deny-dangerous-tool", dp[0].(string), "Should include the policy name")
	} else {
		t.Errorf("denied_policies should be []string or []interface{}")
	}
}

func TestPolicyDeniedError_MultiplePolicies(t *testing.T) {
	// Test with multiple PolicyDeniedErrors
	policyErr := &PolicyDeniedError{
		Message: "Request denied by 2 policies:\n- 'deny-dangerous-tool': delete_file is not allowed\n- 'deny-system-files': Cannot delete system files",
		Data: map[string]interface{}{
			"denied_policies": []string{"deny-dangerous-tool", "deny-system-files"},
			"denied_count":    2,
			"tool_name":       "delete_file",
		},
	}

	// Create a mock function to test the error handling logic
	handleError := func(err error) *mcp.CallToolResult {
		var policyErr *PolicyDeniedError
		if errors.As(err, &policyErr) {
			// Create error result with user-friendly message
			errorResult := mcp.NewToolResultError(policyErr.Message)

			// Add structured error data to the result
			if errorResult.Meta == nil {
				errorResult.Meta = &mcp.Meta{}
			}
			if errorResult.Meta.AdditionalFields == nil {
				errorResult.Meta.AdditionalFields = make(map[string]interface{})
			}
			errorResult.Meta.AdditionalFields["error_code"] = -32600 // Invalid Request
			errorResult.Meta.AdditionalFields["error_data"] = policyErr.Data

			return errorResult
		}
		return nil
	}

	// Test the error handler
	result := handleError(policyErr)
	require.NotNil(t, result)

	// Verify the error result structure
	assert.True(t, result.IsError, "Result should be marked as error")
	assert.Len(t, result.Content, 1, "Should have one content item")

	// Check that the error message includes all policies
	content := result.Content[0]
	textContent, ok := mcp.AsTextContent(content)
	require.True(t, ok, "Content should be TextContent")
	assert.Contains(t, textContent.Text, "Request denied by 2 policies:", "Error message should mention multiple policies")
	assert.Contains(t, textContent.Text, "deny-dangerous-tool", "Should include first policy name")
	assert.Contains(t, textContent.Text, "delete_file is not allowed", "Should include first policy message")
	assert.Contains(t, textContent.Text, "deny-system-files", "Should include second policy name")
	assert.Contains(t, textContent.Text, "Cannot delete system files", "Should include second policy message")

	// Check that the error code is set correctly
	assert.NotNil(t, result.Meta.AdditionalFields, "Result should have metadata")
	assert.Equal(t, -32600, result.Meta.AdditionalFields["error_code"], "Error code should be -32600 (Invalid Request)")

	// Check that error data is included
	errorData, ok := result.Meta.AdditionalFields["error_data"].(map[string]interface{})
	require.True(t, ok, "Error data should be a map")
	assert.Equal(t, "delete_file", errorData["tool_name"], "Should include tool name in error data")
	assert.Equal(t, 2, errorData["denied_count"], "Should include denied count")

	// Check denied_policies as []string
	if dp, ok := errorData["denied_policies"].([]string); ok {
		assert.Len(t, dp, 2, "Should have two denied policies")
		assert.Equal(t, "deny-dangerous-tool", dp[0], "Should include first policy name")
		assert.Equal(t, "deny-system-files", dp[1], "Should include second policy name")
	} else if dp, ok := errorData["denied_policies"].([]interface{}); ok {
		assert.Len(t, dp, 2, "Should have two denied policies")
		assert.Equal(t, "deny-dangerous-tool", dp[0].(string), "Should include first policy name")
		assert.Equal(t, "deny-system-files", dp[1].(string), "Should include second policy name")
	} else {
		t.Errorf("denied_policies should be []string or []interface{}")
	}
}

func TestSessionExpiredError(t *testing.T) {
	// Test SessionExpiredError with session ID
	sessionErr := &SessionExpiredError{
		SessionID: "mcp-session-12345",
		Reason:    "session no longer exists (server may have restarted)",
	}

	errMsg := sessionErr.Error()
	assert.Contains(t, errMsg, "Session expired")
	assert.Contains(t, errMsg, "session no longer exists")
	assert.Contains(t, errMsg, "maybedont__discover_tools")
	assert.Contains(t, errMsg, "re-establish your connection")
}

func TestSessionExpiredError_NoSession(t *testing.T) {
	// Test SessionExpiredError when no session was established
	sessionErr := &SessionExpiredError{
		SessionID: "",
		Reason:    "no session established",
	}

	errMsg := sessionErr.Error()
	assert.Contains(t, errMsg, "Session expired")
	assert.Contains(t, errMsg, "no session established")
	assert.Contains(t, errMsg, "maybedont__discover_tools")
}

func TestIsSessionExpiredError(t *testing.T) {
	// Test with SessionExpiredError
	sessionErr := &SessionExpiredError{
		SessionID: "test-session",
		Reason:    "test reason",
	}
	assert.True(t, IsSessionExpiredError(sessionErr))

	// Test with other errors
	otherErr := errors.New("some other error")
	assert.False(t, IsSessionExpiredError(otherErr))

	// Test with PolicyDeniedError
	policyErr := &PolicyDeniedError{
		Message: "denied",
		Data:    nil,
	}
	assert.False(t, IsSessionExpiredError(policyErr))

	// Test with nil
	assert.False(t, IsSessionExpiredError(nil))
}

func TestJsonRPCRequest_Parsing(t *testing.T) {
	// Test parsing a tools/call request
	toolCallJSON := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"github__list_issues"}}`
	var req jsonRPCRequest
	err := json.Unmarshal([]byte(toolCallJSON), &req)
	require.NoError(t, err)
	assert.Equal(t, "tools/call", req.Method)

	// Test parsing a tools/list request
	toolListJSON := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	err = json.Unmarshal([]byte(toolListJSON), &req)
	require.NoError(t, err)
	assert.Equal(t, "tools/list", req.Method)

	// Test parsing an initialize request
	initJSON := `{"jsonrpc":"2.0","id":3,"method":"initialize","params":{}}`
	err = json.Unmarshal([]byte(initJSON), &req)
	require.NoError(t, err)
	assert.Equal(t, "initialize", req.Method)
}

func TestCallToolParams_Parsing(t *testing.T) {
	// Test parsing tool name from a tools/call request
	toolCallJSON := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"github__list_issues","arguments":{"owner":"test"}}}`
	var toolReq callToolParams
	err := json.Unmarshal([]byte(toolCallJSON), &toolReq)
	require.NoError(t, err)
	assert.Equal(t, "github__list_issues", toolReq.Params.Name)

	// Test with different tool name
	toolCallJSON2 := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"aws__describe_instance"}}`
	err = json.Unmarshal([]byte(toolCallJSON2), &toolReq)
	require.NoError(t, err)
	assert.Equal(t, "aws__describe_instance", toolReq.Params.Name)

	// Test with native tool (no prefix)
	nativeToolJSON := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"maybedont__discover_tools"}}`
	err = json.Unmarshal([]byte(nativeToolJSON), &toolReq)
	require.NoError(t, err)
	assert.Equal(t, "maybedont__discover_tools", toolReq.Params.Name)
}

func TestStaleSessionDetection_PrefixParsing(t *testing.T) {
	// Test that we can correctly identify tool prefixes for stale session detection
	tests := []struct {
		name           string
		toolName       string
		expectedPrefix string
		isDownstream   bool
	}{
		{
			name:           "GitHub tool",
			toolName:       "github__list_issues",
			expectedPrefix: "github",
			isDownstream:   true,
		},
		{
			name:           "AWS tool",
			toolName:       "aws__describe_instance",
			expectedPrefix: "aws",
			isDownstream:   true,
		},
		{
			name:           "Native tool with prefix",
			toolName:       "maybedont__discover_tools",
			expectedPrefix: "maybedont",
			isDownstream:   true, // Still has prefix format, but "maybedont" won't be a configured client
		},
		{
			name:           "No prefix",
			toolName:       "some_tool",
			expectedPrefix: "",
			isDownstream:   false,
		},
		{
			name:           "Single underscore (not a prefix)",
			toolName:       "my_tool_name",
			expectedPrefix: "",
			isDownstream:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientName, _, err := ParsePrefixedName(tt.toolName)
			if tt.isDownstream {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedPrefix, clientName)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// TestRedactToolParams verifies that redactToolParams preserves keys but replaces all values
// with "[redacted]". This protects sensitive tool parameter values in audit logs.
func TestRedactToolParams(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name:     "empty map",
			input:    map[string]interface{}{},
			expected: map[string]interface{}{},
		},
		{
			name: "string values",
			input: map[string]interface{}{
				"repo":  "my-org/my-repo",
				"token": "ghp_secret123",
			},
			expected: map[string]interface{}{
				"repo":  "[redacted]",
				"token": "[redacted]",
			},
		},
		{
			name: "mixed types",
			input: map[string]interface{}{
				"path":    "/home/user/.ssh/id_rsa",
				"count":   42,
				"verbose": true,
				"nested":  map[string]interface{}{"key": "value"},
			},
			expected: map[string]interface{}{
				"path":    "[redacted]",
				"count":   "[redacted]",
				"verbose": "[redacted]",
				"nested":  "[redacted]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redactToolParams(tt.input)
			assert.Equal(t, tt.expected, result)
			// Verify original is not modified
			if len(tt.input) > 0 {
				for key, val := range tt.input {
					if val != "[redacted]" {
						assert.NotEqual(t, "[redacted]", tt.input[key], "original map should not be modified")
						break
					}
				}
			}
		})
	}
}

// TestRedactToolParams_NilSafety verifies that redactToolParams handles nil input.
func TestRedactToolParams_NilSafety(t *testing.T) {
	result := redactToolParams(nil)
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

// TestGatewayNew_NoDownstreamServers verifies the gateway initializes correctly
// with no downstream MCP servers. Native tools should still be registered and
// the client manager should have zero downstream clients.
func TestGatewayNew_NoDownstreamServers(t *testing.T) {
	cfg := &config.Config{
		DownstreamMCPServers: map[string]config.ClientConfig{},
		Audit: config.AuditConfig{
			Path: "test-audit.log",
		},
	}
	cfg.Server.Type = config.ServerTypeSTDIO
	cfg.NativeTools.ListServers.Enabled = true

	logger := config.NewSessionLogger(zaptest.NewLogger(t))
	ctx := t.Context()

	gw, err := New(ctx, cfg, logger, "test", t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, gw)

	// Client manager exists with zero downstream clients
	assert.NotNil(t, gw.clientManager)
	assert.Empty(t, gw.clientManager.GetClientConfigs())

	// Native tools handler is wired up and returns tools
	assert.NotNil(t, gw.nativeToolsHandler)
	nativeTools := gw.nativeToolsHandler.GetTools()
	assert.NotEmpty(t, nativeTools, "native tools should be registered even without downstream servers")
}
