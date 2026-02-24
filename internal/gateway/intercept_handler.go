package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/maybedont/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// --- Request Types (SEP-1763 aligned) ---

// InterceptRequest is the JSON body for POST /api/v1/intercept.
type InterceptRequest struct {
	Event   string            `json:"event"`
	Phase   string            `json:"phase"`
	Payload InterceptPayload  `json:"payload"`
	Context *InterceptContext `json:"context,omitempty"`
	Config  *InterceptReqConf `json:"config,omitempty"`
}

// InterceptPayload contains the tool call details.
type InterceptPayload struct {
	Name      string           `json:"name"`
	Arguments map[string]any   `json:"arguments,omitempty"`
	Result    *InterceptResult `json:"result,omitempty"`
}

// InterceptResult contains the tool execution result (response phase only).
type InterceptResult struct {
	Content []InterceptContent `json:"content"`
}

// InterceptContent represents a single content item in the tool result.
type InterceptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// InterceptContext carries trace and identity information from the caller.
type InterceptContext struct {
	Principal *InterceptPrincipal `json:"principal,omitempty"`
	TraceID   string              `json:"traceId,omitempty"`
	SpanID    string              `json:"spanId,omitempty"`
	Timestamp string              `json:"timestamp,omitempty"`
	SessionID string              `json:"sessionId,omitempty"`
}

// InterceptPrincipal identifies the actor performing the tool call.
type InterceptPrincipal struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
}

// InterceptReqConf carries per-request configuration from the caller.
type InterceptReqConf struct {
	WorkingDirectory string `json:"working_directory,omitempty"`
}

// --- Response Types (SEP-1763 aligned) ---

// InterceptResponse is the JSON response for POST /api/v1/intercept.
type InterceptResponse struct {
	Interceptor string             `json:"interceptor"`
	Type        string             `json:"type"`
	Phase       string             `json:"phase"`
	Valid       bool               `json:"valid"`
	Severity    string             `json:"severity"`
	Messages    []InterceptMessage `json:"messages"`
	Modified    bool               `json:"modified,omitempty"`
	Payload     *InterceptPayload  `json:"payload,omitempty"`
	DurationMs  int64              `json:"durationMs"`
	Info        InterceptInfo      `json:"info"`
}

// InterceptMessage represents a single validation message.
type InterceptMessage struct {
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

// InterceptInfo contains metadata about the validation.
type InterceptInfo struct {
	RequestID     string                  `json:"request_id"`
	ServerVersion string                  `json:"server_version"`
	Results       []InterceptPolicyResult `json:"results"`
}

// InterceptPolicyResult represents a single policy evaluation result.
type InterceptPolicyResult struct {
	PolicyName string `json:"policy_name"`
	PolicyType string `json:"policy_type"`
	Action     string `json:"action"`
	Message    string `json:"message,omitempty"`
}

// --- Handler ---

// InterceptHandlerConfig configures the intercept HTTP handler.
type InterceptHandlerConfig struct {
	// Enabled controls whether the intercept endpoint is active.
	Enabled bool

	// ShellToolNames lists tool names that represent shell/CLI execution.
	ShellToolNames []string

	// Logger is the session logger for request logging.
	Logger *config.SessionLogger

	// Version is the gateway version string returned in responses.
	Version string

	// AuditWriter is used to write audit log entries.
	AuditWriter AuditWriter

	// Evaluator is the shared policy evaluation engine.
	Evaluator *PolicyEvaluator

	// IncludeArgumentValues controls whether full argument values are included in audit entries.
	IncludeArgumentValues bool
}

// InterceptHandler handles POST /api/v1/intercept requests.
type InterceptHandler struct {
	config InterceptHandlerConfig
}

// NewInterceptHandler creates a new intercept handler.
func NewInterceptHandler(cfg InterceptHandlerConfig) *InterceptHandler {
	return &InterceptHandler{config: cfg}
}

// ServeHTTP handles the intercept request.
func (h *InterceptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Extract request context (request ID, client ID from headers)
	ctx := h.extractContext(r)

	if !h.config.Enabled {
		h.writeError(w, http.StatusBadRequest, "Intercept endpoint not enabled")
		return
	}

	// Validate Content-Type
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		h.writeError(w, http.StatusBadRequest, "Content-Type must be application/json")
		return
	}

	// Parse request body
	var req InterceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	// Validate required fields
	if req.Event == "" {
		h.writeError(w, http.StatusBadRequest, "event field is required")
		return
	}
	if req.Event != "tools/call" {
		h.writeError(w, http.StatusBadRequest, "Unsupported event type: only tools/call is supported")
		return
	}
	if req.Phase == "" {
		h.writeError(w, http.StatusBadRequest, "phase field is required")
		return
	}
	if req.Phase != "request" && req.Phase != "response" {
		h.writeError(w, http.StatusBadRequest, "phase must be request or response")
		return
	}
	if req.Payload.Name == "" {
		h.writeError(w, http.StatusBadRequest, "payload.name is required")
		return
	}
	if req.Phase == "response" && req.Payload.Result == nil {
		h.writeError(w, http.StatusBadRequest, "payload.result is required for response phase")
		return
	}

	h.config.Logger.Debug(r.Context(), "Intercept request received",
		zap.String("request_id", ctx.RequestID),
		zap.String("event", req.Event),
		zap.String("phase", req.Phase),
		zap.String("tool", req.Payload.Name),
	)

	// Build placeholder response (evaluation logic added in Task 6)
	resp := &InterceptResponse{
		Interceptor: "maybe-dont",
		Type:        "validation",
		Phase:       req.Phase,
		Valid:       true,
		Severity:    "info",
		Messages:    []InterceptMessage{},
		DurationMs:  time.Since(start).Milliseconds(),
		Info: InterceptInfo{
			RequestID:     ctx.RequestID,
			ServerVersion: h.config.Version,
			Results:       []InterceptPolicyResult{},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *InterceptHandler) extractContext(r *http.Request) *CLIValidationContext {
	ctx := &CLIValidationContext{
		RequestID: r.Header.Get("X-Request-ID"),
		ClientID:  r.Header.Get("X-Maybe-Dont-Client-ID"),
	}

	if ctx.RequestID == "" {
		id, err := GenerateRequestID()
		if err != nil {
			h.config.Logger.Logger().Warn("failed to generate request ID, using fallback",
				zap.Error(err))
			ctx.RequestID = "00000000000000000000000000000000"
		} else {
			ctx.RequestID = id
		}
	}

	return ctx
}

func (h *InterceptHandler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
