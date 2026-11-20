package gateway

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDownstreamHTTPClientRedirects verifies the downstream client follows
// same-origin redirects but refuses cross-origin ones without sending the
// configured headers to the new host (GO-2026-6911).
func TestDownstreamHTTPClientRedirects(t *testing.T) {
	const apiKeyHeader = "X-Api-Key"

	tests := []struct {
		name           string
		crossOrigin    bool
		wantErr        error
		wantTargetHits int32
	}{
		{name: "same-origin redirect is followed", crossOrigin: false, wantTargetHits: 1},
		{name: "cross-origin redirect is refused", crossOrigin: true, wantErr: errCrossOriginRedirect, wantTargetHits: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var otherHostHits atomic.Int32
			otherHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				otherHostHits.Add(1)
				assert.Empty(t, r.Header.Get(apiKeyHeader), "header leaked to another origin")
			}))
			defer otherHost.Close()

			var sameHostTargetHits atomic.Int32
			mux := http.NewServeMux()
			mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
				sameHostTargetHits.Add(1)
				assert.Equal(t, "secret", r.Header.Get(apiKeyHeader))
			})
			origin := httptest.NewServer(mux)
			defer origin.Close()
			mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
				location := origin.URL + "/target"
				if tt.crossOrigin {
					location = otherHost.URL + "/target"
				}
				http.Redirect(w, r, location, http.StatusTemporaryRedirect)
			})

			req, err := http.NewRequest(http.MethodGet, origin.URL+"/mcp", nil)
			require.NoError(t, err)
			req.Header.Set(apiKeyHeader, "secret")

			resp, err := newDownstreamHTTPClient().Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantTargetHits, sameHostTargetHits.Load()+otherHostHits.Load())
			assert.Zero(t, otherHostHits.Load(), "request reached another origin")
		})
	}
}

// TestSameOriginRedirectPolicy covers origin comparison edge cases: scheme
// downgrade, port change, and the redirect limit.
func TestSameOriginRedirectPolicy(t *testing.T) {
	newReq := func(rawURL string) *http.Request {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		require.NoError(t, err)
		return req
	}
	original := newReq("https://mcp.example.com/mcp")

	tests := []struct {
		name    string
		next    string
		hops    int
		wantErr bool
	}{
		{name: "same origin, different path", next: "https://mcp.example.com/mcp/", hops: 1},
		{name: "scheme downgrade", next: "http://mcp.example.com/mcp", hops: 1, wantErr: true},
		{name: "different port", next: "https://mcp.example.com:8443/mcp", hops: 1, wantErr: true},
		{name: "different host", next: "https://169.254.169.254/latest", hops: 1, wantErr: true},
		{name: "subdomain", next: "https://evil.mcp.example.com/mcp", hops: 1, wantErr: true},
		{name: "redirect limit reached", next: "https://mcp.example.com/mcp", hops: maxDownstreamRedirects, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			via := make([]*http.Request, tt.hops)
			for i := range via {
				via[i] = original
			}
			err := sameOriginRedirectPolicy(newReq(tt.next), via)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
