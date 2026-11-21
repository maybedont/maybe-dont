package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateClientHTTPInitializeRejected verifies that when an http downstream
// rejects the initialize POST with a 4xx other than 401, the error names the
// downstream and points at credentials and URL rather than only reporting
// mcp-go's "likely a legacy SSE server". A 401 keeps mcp-go's authorization
// error and is not rewritten. The original error stays in the chain.
func TestCreateClientHTTPInitializeRejected(t *testing.T) {
	const token = "ghp_secret-token-value"

	tests := []struct {
		name        string
		status      int
		wantErr     error
		wantHint    bool
		wantNoMatch error
	}{
		{name: "400 bad request", status: http.StatusBadRequest, wantErr: transport.ErrLegacySSEServer, wantHint: true},
		{name: "403 forbidden", status: http.StatusForbidden, wantErr: transport.ErrLegacySSEServer, wantHint: true},
		{name: "404 not found", status: http.StatusNotFound, wantErr: transport.ErrLegacySSEServer, wantHint: true},
		{name: "401 unauthorized", status: http.StatusUnauthorized, wantErr: transport.ErrAuthorizationRequired, wantNoMatch: transport.ErrLegacySSEServer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			cm := NewClientManager(context.Background(), newTestLogger(t))
			cfg := config.ClientConfig{
				Type:          "http",
				DownstreamURL: server.URL,
			}
			cfg.HTTPConfig.Headers = map[string]string{"Authorization": "Bearer " + token}

			_, err := cm.createClient(context.Background(), "github", cfg)
			require.Error(t, err)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantNoMatch != nil {
				assert.NotErrorIs(t, err, tt.wantNoMatch)
			}
			assert.NotContains(t, err.Error(), token, "error must not include credentials")

			if tt.wantHint {
				assert.Contains(t, err.Error(), `downstream "github" rejected the MCP initialize request with a 4xx status`)
				assert.Contains(t, err.Error(), "type: sse")
			} else {
				assert.NotContains(t, err.Error(), "rejected the MCP initialize request")
			}
		})
	}
}
