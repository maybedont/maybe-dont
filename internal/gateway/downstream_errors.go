package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// errInitializeRejected4xx marks an http downstream that answered the MCP
// initialize POST with a 4xx other than 401. mcp-go reports that as a likely
// legacy SSE server, but servers such as GitHub's answer a bad token with 400,
// so the message points at credentials and URL first. The text holds no
// configured values, so it is safe to return to callers.
var errInitializeRejected4xx = errors.New("rejected the MCP initialize request with a 4xx status; " +
	"check its credentials (headers or pass-through auth) and URL; " +
	"only servers that use the old SSE transport need `type: sse`")

// Messages for a downstream that answered 401. Pass-through clients send the
// caller's credentials, so the caller can fix them; other clients send
// configured headers, which only the operator can fix.
const (
	msgPassThroughAuthorizationRequired = "authorization required; check the pass-through credentials your client sends"
	msgAuthorizationRequired            = "authorization required; check the headers configured for this server"
)

// errDownstreamConnectFailed is what a caller sees when a downstream server
// can't be connected for a reason with no safe, specific message. The
// underlying error can carry URLs with credentials or local command paths, so
// it is only logged.
func errDownstreamConnectFailed(clientName string) error {
	return fmt.Errorf("failed to connect to downstream server '%s'; see the gateway log for details", clientName)
}

// safeDownstreamConnectError maps a downstream connect error to an error that
// can be returned to an MCP caller. It never includes text from err: callers
// must log err themselves, with downstreamErrorField. Recognized failures get
// a fixed message that says what to check; everything else gets
// errDownstreamConnectFailed.
func safeDownstreamConnectError(clientName string, cfg config.ClientConfig, err error) error {
	var detail string
	switch {
	case errors.Is(err, errInitializeRejected4xx):
		detail = errInitializeRejected4xx.Error()
	case errors.Is(err, transport.ErrAuthorizationRequired):
		detail = authorizationRequiredHint(cfg)
	default:
		return errDownstreamConnectFailed(clientName)
	}
	return fmt.Errorf("failed to connect to downstream server '%s': %s", clientName, detail)
}

// authorizationRequiredHint is the fixed text for a downstream that answered
// 401, worded for whoever can fix the credentials.
func authorizationRequiredHint(cfg config.ClientConfig) string {
	if cfg.Auth.PassThrough.Enabled {
		return msgPassThroughAuthorizationRequired
	}
	return msgAuthorizationRequired
}

// errDownstreamCallFailed is what a caller sees when a request to a connected
// downstream fails in transport. The underlying error can carry the URL with
// a credential, or a local command path, so it is only logged.
func errDownstreamCallFailed(clientName string) error {
	return fmt.Errorf("call to downstream server '%s' failed; see the gateway log for details", clientName)
}

// downstreamCallError returns the error a caller sees for a failed tool call,
// prompt get or resource read on a connected downstream.
//
// The mcp-go client wraps every failure to send a request or read its
// response in *transport.Error (client/client.go sendRequest), while a
// JSON-RPC error response is converted by JSONRPCErrorDetails.AsError and is
// never wrapped that way. A JSON-RPC error is the downstream's own answer and
// the agent needs it, so it passes through unlogged. A transport failure is
// logged here and replaced with a fixed message: the 401 hint for an
// authorization failure (its text holds no URL), otherwise
// errDownstreamCallFailed.
func (g *Gateway) downstreamCallError(ctx context.Context, sessionID, clientName string, cfg config.ClientConfig, method string, err error) error {
	var transportErr *transport.Error
	if !errors.As(err, &transportErr) {
		return err
	}

	// A cancelled request is the caller going away, which is expected.
	log := g.logger.Warn
	if errors.Is(err, context.Canceled) {
		log = g.logger.Debug
	}
	log(ctx, "Downstream request failed",
		zap.String("session_id", sessionID),
		zap.String("client", clientName),
		zap.String("method", method),
		downstreamErrorField(err))

	if errors.Is(err, transport.ErrAuthorizationRequired) {
		return fmt.Errorf("call to downstream server '%s' failed: %s", clientName, authorizationRequiredHint(cfg))
	}
	return errDownstreamCallFailed(clientName)
}

// configuredDownstreamURL returns the URL cfg connects to; url is an alias
// for downstream_url, which config loading normally fills from it.
func configuredDownstreamURL(cfg config.ClientConfig) string {
	if cfg.DownstreamURL != "" {
		return cfg.DownstreamURL
	}
	return cfg.URL
}

// redactedDownstreamURL returns rawURL without userinfo, query string or
// fragment, the parts operators put credentials in, so it can be shown to an
// agent or logged. It returns "" unless rawURL parses as an absolute http or
// https URL with a host: anything else (a scheme-less "user:token@host/mcp"
// parses as scheme "user" with the token in the opaque part) could still
// hold a credential.
func redactedDownstreamURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.Host == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}
