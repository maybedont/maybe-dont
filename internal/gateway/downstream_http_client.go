package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// maxDownstreamRedirects matches net/http's default redirect limit.
const maxDownstreamRedirects = 10

// errCrossOriginRedirect is returned when a downstream MCP server redirects to
// a different origin and the server is not configured to allow it.
var errCrossOriginRedirect = errors.New("downstream redirect to a different origin refused (set allow_cross_origin_redirects to permit)")

// newDownstreamHTTPClient returns the HTTP client used for SSE and streamable
// HTTP downstream MCP connections.
//
// mcp-go's default client follows redirects to any host and re-sends the
// configured static and pass-through headers (GO-2026-6911). net/http strips
// Authorization on cross-domain redirects, but custom headers such as API keys
// survive, and a redirect to an internal address turns the gateway into an
// SSRF proxy. Unless the operator opts in per server, redirects are limited to
// the original origin.
func newDownstreamHTTPClient(allowCrossOriginRedirects bool) *http.Client {
	if allowCrossOriginRedirects {
		return &http.Client{}
	}
	return &http.Client{CheckRedirect: sameOriginRedirectPolicy}
}

// sameOriginRedirectPolicy allows redirects only to the original host and port.
// The scheme must match, except that an http to https upgrade is allowed since
// it only makes the connection safer.
func sameOriginRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= maxDownstreamRedirects {
		return fmt.Errorf("stopped after %d redirects", maxDownstreamRedirects)
	}
	original := via[0].URL
	next := req.URL
	if sameOrigin(original.Scheme, original.Host, next.Scheme, next.Host) {
		return nil
	}
	return fmt.Errorf("%w: %s://%s to %s://%s", errCrossOriginRedirect,
		original.Scheme, original.Host, next.Scheme, next.Host)
}

// sameOrigin compares origins, treating an http to https upgrade on the same
// hostname as the same origin. The upgrade changes the default port, so for
// that case only the hostname and any explicit port are compared.
func sameOrigin(fromScheme, fromHost, toScheme, toHost string) bool {
	if fromScheme == toScheme {
		return fromHost == toHost
	}
	if fromScheme != "http" || toScheme != "https" {
		return false
	}
	return stripDefaultPort(fromHost, "80") == stripDefaultPort(toHost, "443")
}

// stripDefaultPort removes the scheme's default port so "host:80" over http
// and "host" over https compare equal after an upgrade.
func stripDefaultPort(host, defaultPort string) string {
	return strings.TrimSuffix(host, ":"+defaultPort)
}
