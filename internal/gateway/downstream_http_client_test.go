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
// same-origin redirects, refuses cross-origin ones without sending configured
// headers to the new host (GO-2026-6911), and follows them when the server is
// configured with allow_cross_origin_redirects.
func TestDownstreamHTTPClientRedirects(t *testing.T) {
	const apiKeyHeader = "X-Api-Key"

	tests := []struct {
		name               string
		crossOrigin        bool
		allowCrossOrigin   bool
		wantErr            error
		wantSameHostHits   int32
		wantOtherHostHits  int32
		wantHeaderForwards bool
	}{
		{name: "same-origin redirect is followed", wantSameHostHits: 1},
		{name: "cross-origin redirect is refused by default", crossOrigin: true, wantErr: errCrossOriginRedirect},
		{name: "cross-origin redirect is followed when allowed", crossOrigin: true, allowCrossOrigin: true, wantOtherHostHits: 1, wantHeaderForwards: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var otherHostHits atomic.Int32
			var otherHostSawHeader atomic.Bool
			otherHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				otherHostHits.Add(1)
				otherHostSawHeader.Store(r.Header.Get(apiKeyHeader) != "")
			}))
			defer otherHost.Close()

			var sameHostHits atomic.Int32
			mux := http.NewServeMux()
			mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
				sameHostHits.Add(1)
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

			resp, err := newDownstreamHTTPClient(tt.allowCrossOrigin).Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantSameHostHits, sameHostHits.Load())
			assert.Equal(t, tt.wantOtherHostHits, otherHostHits.Load())
			assert.Equal(t, tt.wantHeaderForwards, otherHostSawHeader.Load())
		})
	}
}

// TestSameOriginRedirectPolicy covers origin comparison edge cases: the
// http to https upgrade, scheme downgrade, port and host changes, and the
// redirect limit.
func TestSameOriginRedirectPolicy(t *testing.T) {
	newReq := func(rawURL string) *http.Request {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		require.NoError(t, err)
		return req
	}

	tests := []struct {
		name     string
		original string
		next     string
		hops     int
		wantErr  bool
	}{
		{name: "same origin, different path", original: "https://mcp.example.com/mcp", next: "https://mcp.example.com/mcp/", hops: 1},
		{name: "http to https upgrade", original: "http://mcp.example.com/mcp", next: "https://mcp.example.com/mcp", hops: 1},
		{name: "http to https upgrade with explicit default ports", original: "http://mcp.example.com:80/mcp", next: "https://mcp.example.com:443/mcp", hops: 1},
		{name: "http to https upgrade keeping a custom port", original: "http://mcp.example.com:8080/mcp", next: "https://mcp.example.com:8080/mcp", hops: 1},
		{name: "http to https upgrade on a different host", original: "http://mcp.example.com/mcp", next: "https://evil.example.com/mcp", hops: 1, wantErr: true},
		{name: "https to http downgrade", original: "https://mcp.example.com/mcp", next: "http://mcp.example.com/mcp", hops: 1, wantErr: true},
		{name: "different port", original: "https://mcp.example.com/mcp", next: "https://mcp.example.com:8443/mcp", hops: 1, wantErr: true},
		{name: "different host", original: "https://mcp.example.com/mcp", next: "https://169.254.169.254/latest", hops: 1, wantErr: true},
		{name: "subdomain", original: "https://mcp.example.com/mcp", next: "https://evil.mcp.example.com/mcp", hops: 1, wantErr: true},
		{name: "redirect limit reached", original: "https://mcp.example.com/mcp", next: "https://mcp.example.com/mcp", hops: maxDownstreamRedirects, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			via := make([]*http.Request, tt.hops)
			for i := range via {
				via[i] = newReq(tt.original)
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
