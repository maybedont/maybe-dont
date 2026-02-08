package gateway

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// trackingResponseHandler is a test double that records whether HandleResponse was called.
type trackingResponseHandler struct {
	called atomic.Int32
}

func (h *trackingResponseHandler) HandleResponse(_ context.Context, _ mcp.CallToolRequest, _ *mcp.CallToolResult) (ResponseValidationResults, error) {
	h.called.Add(1)
	return ResponseValidationResults{
		Allowed: true,
		Message: "tracking handler called",
		Results: []ResponseValidationResult{},
	}, nil
}

// TestResponseValidationSkipsEmptyContent verifies that response validation
// is skipped when the tool result has no content. The guard is in gateway.go
// (the `len(result.Content) > 0` condition), but we test the principle here:
// the response validation chain itself processes whatever it receives, so the
// caller (gateway) must gate on content presence.
//
// Note: nil result is not tested because result is guaranteed non-nil at the
// guard site in gateway.go. A nil result there would be a programming error
// that should panic (fail-fast), not be silently skipped.
func TestResponseValidationSkipsEmptyContent(t *testing.T) {
	tests := []struct {
		name          string
		content       []mcp.Content
		shouldProcess bool
		description   string
	}{
		{
			name:          "non-empty content is processed",
			content:       []mcp.Content{mcp.TextContent{Type: "text", Text: "some content"}},
			shouldProcess: true,
			description:   "Normal response with content should be validated",
		},
		{
			name:          "empty content slice is skipped",
			content:       []mcp.Content{},
			shouldProcess: false,
			description:   "Empty content means nothing to validate",
		},
		{
			name:          "nil content slice is skipped",
			content:       nil,
			shouldProcess: false,
			description:   "Nil content means nothing to validate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			sessionLogger := config.NewSessionLogger(logger)

			handler := &trackingResponseHandler{}
			chain := NewResponseValidationChain(sessionLogger, handler)

			req := mcp.CallToolRequest{
				Request: mcp.Request{Method: "tools/call"},
				Params:  mcp.CallToolParams{Name: "test_tool"},
			}

			result := &mcp.CallToolResult{Content: tt.content}

			// Simulate the gateway guard: only call chain if content is present.
			// This mirrors the condition in gateway.go:
			//   if g.responseValidationChain != nil && len(result.Content) > 0 {
			if chain != nil && len(result.Content) > 0 {
				chainResults, err := chain.Handle(context.Background(), req, result)
				require.NoError(t, err)
				assert.True(t, chainResults.Allowed)
			}

			if tt.shouldProcess {
				assert.Equal(t, int32(1), handler.called.Load(),
					"Handler should have been called: %s", tt.description)
			} else {
				assert.Equal(t, int32(0), handler.called.Load(),
					"Handler should NOT have been called: %s", tt.description)
			}
		})
	}
}
