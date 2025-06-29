package proxy

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		Message: "Request denied by policy: delete_file is not allowed",
		Data: map[string]interface{}{
			"denied_policies": []string{"deny-dangerous-tool"},
			"denied_count":    1,
			"tool_name":       "delete_file",
		},
	}

	// Create a mock function to test the error handling logic
	handleError := func(err error) *mcp.CallToolResult {
		if policyErr, ok := err.(*PolicyDeniedError); ok {
			// Create error result with user-friendly message
			errorResult := mcp.NewToolResultError(policyErr.Message)

			// Add structured error data to the result
			if errorResult.Meta == nil {
				errorResult.Meta = make(map[string]interface{})
			}
			errorResult.Meta["error_code"] = -32600 // Invalid Request
			errorResult.Meta["error_data"] = policyErr.Data

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
	assert.Contains(t, textContent.Text, "delete_file is not allowed", "Should include the policy message")

	// Check that the error code is set correctly
	assert.NotNil(t, result.Meta, "Result should have metadata")
	assert.Equal(t, -32600, result.Meta["error_code"], "Error code should be -32600 (Invalid Request)")

	// Check that error data is included
	errorData, ok := result.Meta["error_data"].(map[string]interface{})
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
