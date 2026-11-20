package gateway

import (
	"errors"
	"fmt"
	"net/http"
)

// maxDownstreamRedirects matches net/http's default redirect limit.
const maxDownstreamRedirects = 10

// errCrossOriginRedirect is returned when a downstream MCP server redirects to
// a different scheme or host.
var errCrossOriginRedirect = errors.New("downstream redirect to a different origin refused")

// newDownstreamHTTPClient returns the HTTP client used for SSE and streamable
// HTTP downstream MCP connections.
//
// mcp-go's default client follows redirects to any host and re-sends the
// configured static and pass-through headers (GO-2026-6911). net/http strips
// Authorization on cross-domain redirects, but custom headers such as API keys
// survive, and a redirect to an internal address turns the gateway into an
// SSRF proxy. Restricting redirects to the original origin closes both.
func newDownstreamHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: sameOriginRedirectPolicy}
}

// sameOriginRedirectPolicy allows redirects only when the scheme and host
// (including port) match the original request.
func sameOriginRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= maxDownstreamRedirects {
		return fmt.Errorf("stopped after %d redirects", maxDownstreamRedirects)
	}
	original := via[0].URL
	if req.URL.Scheme != original.Scheme || req.URL.Host != original.Host {
		return fmt.Errorf("%w: %s://%s to %s://%s", errCrossOriginRedirect,
			original.Scheme, original.Host, req.URL.Scheme, req.URL.Host)
	}
	return nil
}
