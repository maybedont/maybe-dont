package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap/zaptest"
)

func TestAIValidationHandler(t *testing.T) {
	// Create a test server that simulates the AI API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		// Parse request body
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens int `json:"max_tokens"`
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify request
		assert.Equal(t, "test-model", req.Model)
		assert.Equal(t, 100, req.MaxTokens)
		assert.Len(t, req.Messages, 2)
		assert.Equal(t, "system", req.Messages[0].Role)
		assert.Equal(t, "user", req.Messages[1].Role)

		// Return a mock response
		resp := struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{
						Content: `{"allowed": true, "reason": "Tool call is safe"}`,
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Set up test environment
	t.Setenv("MCP_PROXY_OPENAI_API_KEY", "test-key")

	// Create handler with test configuration
	handler := NewAIValidationHandler(
		zaptest.NewLogger(t),
		&config.AIValidation{
			Enabled:   true,
			Endpoint:  server.URL,
			Model:     "test-model",
			Timeout:   30,
			MaxTokens: 100,
		},
	)

	// Test cases
	tests := []struct {
		name    string
		req     mcp.CallToolRequest
		wantErr bool
	}{
		{
			name: "allowed tool call",
			req: mcp.CallToolRequest{
				Request: mcp.Request{
					Method: "tools/call",
					Params: mcp.RequestParams{
						Meta: &mcp.Meta{},
					},
				},
				Params: mcp.CallToolParams{
					Name: "read_file",
					Arguments: map[string]string{
						"path": "/safe/file.txt",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.HandleToolCall(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
