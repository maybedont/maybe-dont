package gateway

import (
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/maybedont/maybe-dont/internal/config"
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
// must log err themselves. Recognized failures get a fixed message that says
// what to check; everything else gets errDownstreamConnectFailed.
func safeDownstreamConnectError(clientName string, cfg config.ClientConfig, err error) error {
	var detail string
	switch {
	case errors.Is(err, errInitializeRejected4xx):
		detail = errInitializeRejected4xx.Error()
	case errors.Is(err, transport.ErrAuthorizationRequired) && cfg.Auth.PassThrough.Enabled:
		detail = msgPassThroughAuthorizationRequired
	case errors.Is(err, transport.ErrAuthorizationRequired):
		detail = msgAuthorizationRequired
	default:
		return errDownstreamConnectFailed(clientName)
	}
	return fmt.Errorf("failed to connect to downstream server '%s': %s", clientName, detail)
}
