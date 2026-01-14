package gateway

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestToolLoggingHandler_RequestID(t *testing.T) {
	// Create an observed logger to capture log output
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	sessionLogger := config.NewSessionLogger(logger)

	// Create the logging handler
	handler := NewToolLoggingHandler(sessionLogger)

	// Test case 1: With request ID in context
	t.Run("with_request_id", func(t *testing.T) {
		recorded.TakeAll() // Clear previous logs

		ctx := WithRequestID(context.Background(), "test-session-123")
		req := mcp.CallToolRequest{
			Request: mcp.Request{Method: "tools/call"},
			Params: mcp.CallToolParams{
				Name: "test_tool",
				Arguments: map[string]interface{}{
					"arg1": "value1",
				},
			},
		}

		result, err := handler.HandleToolCall(ctx, req)
		require.NoError(t, err)
		assert.True(t, result.Allowed)

		// Verify the log entry contains the correct request_id
		logs := recorded.All()
		require.Len(t, logs, 1)
		assert.Equal(t, "Tool call audit log", logs[0].Message)

		// Find the request_id field
		requestIDField := logs[0].ContextMap()["request_id"]
		assert.Equal(t, "test-session-123", requestIDField)
	})

	// Test case 2: Without request ID in context
	t.Run("without_request_id", func(t *testing.T) {
		recorded.TakeAll() // Clear previous logs

		ctx := context.Background()
		req := mcp.CallToolRequest{
			Request: mcp.Request{Method: "tools/call"},
			Params: mcp.CallToolParams{
				Name: "test_tool",
			},
		}

		result, err := handler.HandleToolCall(ctx, req)
		require.NoError(t, err)
		assert.True(t, result.Allowed)

		// Verify the log entry contains "-" for request_id when not set in context
		logs := recorded.All()
		require.Len(t, logs, 1)

		requestIDField := logs[0].ContextMap()["request_id"]
		assert.Equal(t, "-", requestIDField)
	})
}
