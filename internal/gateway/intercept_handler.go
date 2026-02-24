package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
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

	var resp *InterceptResponse
	switch req.Phase {
	case "request":
		resp = h.handleRequestPhase(r.Context(), ctx, &req, start)
	case "response":
		resp = h.handleResponsePhase(r.Context(), ctx, &req, start)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleRequestPhase evaluates a request-phase intercept through the policy engines.
// Shell tools trigger CLI command parsing; everything else is evaluated as an MCP tool call.
func (h *InterceptHandler) handleRequestPhase(
	ctx context.Context,
	valCtx *CLIValidationContext,
	req *InterceptRequest,
	start time.Time,
) *InterceptResponse {
	results := h.evaluateRequest(ctx, req)
	return h.buildValidationResponse(req, valCtx, results, start)
}

// handleResponsePhase evaluates a response-phase intercept through the response validation chain.
// Placeholder — full implementation in Task 7.
func (h *InterceptHandler) handleResponsePhase(
	_ context.Context,
	valCtx *CLIValidationContext,
	req *InterceptRequest,
	start time.Time,
) *InterceptResponse {
	// Placeholder — returns allowed until Task 7
	return &InterceptResponse{
		Interceptor: "maybe-dont",
		Type:        "validation",
		Phase:       req.Phase,
		Valid:       true,
		Severity:    "info",
		Messages:    []InterceptMessage{},
		DurationMs:  time.Since(start).Milliseconds(),
		Info: InterceptInfo{
			RequestID:     valCtx.RequestID,
			ServerVersion: h.config.Version,
			Results:       []InterceptPolicyResult{},
		},
	}
}

// evaluateRequest routes request evaluation based on whether the tool is a shell tool.
func (h *InterceptHandler) evaluateRequest(ctx context.Context, req *InterceptRequest) ValidationResults {
	if h.config.Evaluator == nil {
		return ValidationResults{
			Results: []ValidationResult{},
			Allowed: true,
			Message: "No validation policies configured",
		}
	}

	// Shell tools get evaluated as both CLI commands and MCP tool calls.
	// The evaluator merges results from both expression types.
	if h.isShellTool(req.Payload.Name) {
		return h.evaluateShellCommand(ctx, req)
	}

	return h.evaluateToolCall(ctx, req)
}

// isShellTool checks if the tool name is in the configured shell tool names.
func (h *InterceptHandler) isShellTool(name string) bool {
	return slices.Contains(h.config.ShellToolNames, name)
}

// evaluateShellCommand parses a shell tool's command string and evaluates it
// as both a CLI command (cli_expression) and an MCP tool call (mcp_expression).
func (h *InterceptHandler) evaluateShellCommand(ctx context.Context, req *InterceptRequest) ValidationResults {
	// Parse command from arguments
	commandStr, _ := req.Payload.Arguments["command"].(string)
	parts := strings.Fields(commandStr)

	var command string
	var args []string
	if len(parts) > 0 {
		command = parts[0]
		args = parts[1:]
	}

	// Extract working directory from request config
	var workingDir string
	if req.Config != nil {
		workingDir = req.Config.WorkingDirectory
	}

	cliReq := &CLIValidationRequest{
		Command:          command,
		Arguments:        args,
		WorkingDirectory: workingDir,
	}

	// Evaluate CLI expressions
	cliResults := h.config.Evaluator.EvaluateCLICommand(ctx, cliReq)

	// Also evaluate MCP expressions against the tool call
	mcpResults := h.config.Evaluator.EvaluateToolCall(ctx, h.payloadToCallToolRequest(req))

	// Merge: if either denies, the final result is deny
	return h.mergeShellResults(cliResults, mcpResults)
}

// evaluateToolCall converts the payload to an MCP tool call request and evaluates it.
func (h *InterceptHandler) evaluateToolCall(ctx context.Context, req *InterceptRequest) ValidationResults {
	mcpReq := h.payloadToCallToolRequest(req)
	return h.config.Evaluator.EvaluateToolCall(ctx, mcpReq)
}

// payloadToCallToolRequest converts an InterceptPayload to an mcp.CallToolRequest.
func (h *InterceptHandler) payloadToCallToolRequest(req *InterceptRequest) mcp.CallToolRequest {
	var mcpReq mcp.CallToolRequest
	mcpReq.Params.Name = req.Payload.Name
	if req.Payload.Arguments != nil {
		mcpReq.Params.Arguments = req.Payload.Arguments
	}
	return mcpReq
}

// mergeShellResults combines CLI and MCP evaluation results.
// If either evaluation denies, the merged result is a deny.
func (h *InterceptHandler) mergeShellResults(cliResults, mcpResults ValidationResults) ValidationResults {
	merged := ValidationResults{
		Allowed: true,
	}

	// Merge CLI results
	merged.Results = append(merged.Results, cliResults.Results...)
	merged.AllowCount += cliResults.AllowCount
	merged.DenyCount += cliResults.DenyCount
	if cliResults.RulesDetails != nil {
		merged.RulesDetails = cliResults.RulesDetails
	}
	if cliResults.AIDetails != nil {
		merged.AIDetails = cliResults.AIDetails
	}
	if cliResults.AsyncCompletion != nil {
		merged.AsyncCompletion = cliResults.AsyncCompletion
	}
	if cliResults.AuditModeBypass {
		merged.AuditModeBypass = true
	}
	if !cliResults.Allowed {
		merged.Allowed = false
		merged.Message = cliResults.Message
	}

	// Merge MCP results
	merged.Results = append(merged.Results, mcpResults.Results...)
	merged.AllowCount += mcpResults.AllowCount
	merged.DenyCount += mcpResults.DenyCount
	if mcpResults.RulesDetails != nil {
		merged.RulesDetails = mcpResults.RulesDetails
	}
	if mcpResults.AIDetails != nil {
		merged.AIDetails = mcpResults.AIDetails
	}
	if mcpResults.AsyncCompletion != nil {
		merged.AsyncCompletion = mcpResults.AsyncCompletion
	}
	if mcpResults.AuditModeBypass {
		merged.AuditModeBypass = true
	}
	if !mcpResults.Allowed {
		merged.Allowed = false
		if merged.Message == "" {
			merged.Message = mcpResults.Message
		}
	}

	// Clear AuditModeBypass if enforced deny overrides it
	if !merged.Allowed {
		merged.AuditModeBypass = false
	}

	// Set default message
	if merged.Message == "" {
		if merged.Allowed {
			merged.Message = "Tool call approved by policy"
		} else {
			merged.Message = "Tool call denied by policy"
		}
	}

	return merged
}

// buildValidationResponse maps ValidationResults to an SEP-1763 InterceptResponse.
func (h *InterceptHandler) buildValidationResponse(
	req *InterceptRequest,
	ctx *CLIValidationContext,
	results ValidationResults,
	start time.Time,
) *InterceptResponse {
	valid := results.Allowed
	severity := "info"
	if !results.Allowed {
		severity = "error"
	} else if results.AuditModeBypass {
		severity = "warn"
	}

	var messages []InterceptMessage
	if !results.Allowed || results.AuditModeBypass {
		messages = append(messages, InterceptMessage{
			Message:  results.Message,
			Severity: severity,
		})
	}
	if messages == nil {
		messages = []InterceptMessage{}
	}

	policyResults := make([]InterceptPolicyResult, 0, len(results.Results))
	for _, r := range results.Results {
		policyResults = append(policyResults, InterceptPolicyResult{
			PolicyName: r.PolicyName,
			PolicyType: r.PolicyType,
			Action:     string(r.Action),
			Message:    r.Message,
		})
	}

	return &InterceptResponse{
		Interceptor: "maybe-dont",
		Type:        "validation",
		Phase:       req.Phase,
		Valid:       valid,
		Severity:    severity,
		Messages:    messages,
		DurationMs:  time.Since(start).Milliseconds(),
		Info: InterceptInfo{
			RequestID:     ctx.RequestID,
			ServerVersion: h.config.Version,
			Results:       policyResults,
		},
	}
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
