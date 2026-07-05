package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/maybedont/maybe-dont/internal/auth"
	"go.uber.org/zap"
)

// handleProtectedResourceMetadata serves RFC 9728 protected resource metadata. It must be
// reachable without authentication so clients can discover how to obtain a token.
func (g *Gateway) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONBytes(w, g.authComponents.prmJSON)
}

// handleAuthorizationServerMetadata serves RFC 8414 authorization server metadata for the
// embedded resource authorization server.
func (g *Gateway) handleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONBytes(w, g.authComponents.asMetaJSON)
}

// handleTokenEndpoint implements the embedded AS token endpoint. It accepts the RFC 7523
// JWT bearer grant with an ID-JAG assertion and issues an audience-restricted access token.
func (g *Gateway) handleTokenEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}

	if r.PostForm.Get("grant_type") != auth.GrantTypeJWTBearer {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only the jwt-bearer grant is supported")
		return
	}

	assertion := r.PostForm.Get("assertion")
	if assertion == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "assertion is required")
		return
	}

	ac := g.authComponents
	grant, err := ac.idjagValidator.Validate(r.Context(), assertion)
	if err != nil {
		// Do not echo the assertion; log at debug for diagnostics.
		g.logger.Debug(r.Context(), "ID-JAG validation failed", zap.Error(err))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the provided assertion is not valid")
		return
	}

	if len(ac.allowedClientIDs) > 0 && !ac.allowedClientIDs[grant.ClientID] {
		writeOAuthError(w, http.StatusBadRequest, "unauthorized_client", "client is not permitted")
		return
	}

	granted := intersectScopes(grant.Scopes, ac.scopesSupported)

	accessToken, expiresIn, err := ac.issuer.Issue(auth.IssueParams{
		Subject:  grant.Subject,
		Email:    grant.Email,
		ClientID: grant.ClientID,
		Audience: ac.resource,
		Scopes:   granted,
		TTL:      ac.accessTokenTTL,
	})
	if err != nil {
		g.logger.Error(r.Context(), "Failed to issue access token", zap.Error(err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue token")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"token_type":   "Bearer",
		"access_token": accessToken,
		"expires_in":   expiresIn,
	}
	if len(granted) > 0 {
		resp["scope"] = joinScopeList(granted)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// intersectScopes returns the requested scopes filtered to those in supported. When
// supported is empty, the requested scopes pass through unchanged (the IdP has already
// enforced scope policy when issuing the ID-JAG).
func intersectScopes(requested, supported []string) []string {
	if len(supported) == 0 {
		return requested
	}
	allowed := make(map[string]bool, len(supported))
	for _, s := range supported {
		allowed[s] = true
	}
	var out []string
	for _, s := range requested {
		if allowed[s] {
			out = append(out, s)
		}
	}
	return out
}

// joinScopeList joins scopes with a single space.
func joinScopeList(scopes []string) string {
	out := ""
	for i, s := range scopes {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}

func writeJSONBytes(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": description,
	})
}
