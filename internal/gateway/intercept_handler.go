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
		h.writeError(w, http.StatusBadRequest, "intercept_disabled", "Intercept endpoint not enabled")
		return
	}

	// Validate Content-Type
	if !hasJSONContentType(r) {
		h.writeError(w, http.StatusBadRequest, "invalid_content_type", "Content-Type must be application/json")
		return
	}

	// Parse request body
	var req InterceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON: "+err.Error())
		return
	}

	// Validate required fields
	if req.Event == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "event field is required")
		return
	}
	if req.Event != "tools/call" {
		h.writeError(w, http.StatusBadRequest, "unsupported_event", "Unsupported event type: only tools/call is supported")
		return
	}
	if req.Phase == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "phase field is required")
		return
	}
	if req.Phase != "request" && req.Phase != "response" {
		h.writeError(w, http.StatusBadRequest, "invalid_phase", "phase must be request or response")
		return
	}
	if req.Payload.Name == "" {
		h.writeError(w, http.StatusBadRequest, "missing_tool_name", "payload.name is required")
		return
	}
	if req.Phase == "response" && req.Payload.Result == nil {
		h.writeError(w, http.StatusBadRequest, "missing_result", "payload.result is required for response phase")
		return
	}

	h.config.Logger.Debug(r.Context(), "Intercept request received",
		zap.String("request_id", ctx.RequestID),
		zap.String("event", req.Event),
		zap.String("phase", req.Phase),
		zap.String("tool", req.Payload.Name),
	)

	// Attach request ID to context so downstream loggers (CEL engine, AI engine) include it
	reqCtx := context.WithValue(r.Context(), config.RequestIDKey, ctx.RequestID)

	var resp *InterceptResponse
	switch req.Phase {
	case "request":
		resp = h.handleRequestPhase(r, reqCtx, ctx, &req, start)
	case "response":
		resp = h.handleResponsePhase(r, reqCtx, ctx, &req, start)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleRequestPhase evaluates a request-phase intercept through the policy engines.
// Shell tools trigger CLI command parsing; everything else is evaluated as an MCP tool call.
func (h *InterceptHandler) handleRequestPhase(
	r *http.Request,
	ctx context.Context,
	valCtx *CLIValidationContext,
	req *InterceptRequest,
	start time.Time,
) *InterceptResponse {
	results := h.evaluateRequest(ctx, req)

	action := "allow"
	if !results.Allowed {
		action = "deny"
	}
	actionReason := DeriveAuditActionReason(results.Allowed, results.AuditModeBypass, results.FailedOpen, ActionReasonRequestPolicy)

	h.writeRequestAuditEntry(r, start, valCtx, req, action, actionReason, &results)

	return h.buildValidationResponse(req, valCtx, results, start)
}

// handleResponsePhase evaluates a response-phase intercept through the response validation chain.
// Redacted responses return type="mutation" with the modified payload; denials return type="validation".
func (h *InterceptHandler) handleResponsePhase(
	r *http.Request,
	ctx context.Context,
	valCtx *CLIValidationContext,
	req *InterceptRequest,
	start time.Time,
) *InterceptResponse {
	if h.config.Evaluator == nil {
		return h.buildResponseResult(req, valCtx, ResponseValidationResults{
			Allowed: true,
			Message: "No response validation configured",
		}, start)
	}

	mcpReq := h.payloadToCallToolRequest(req)
	mcpResult := h.payloadToCallToolResult(req)

	results, err := h.config.Evaluator.EvaluateResponse(ctx, mcpReq, mcpResult)
	if err != nil {
		h.config.Logger.Error(ctx, "Response evaluation failed",
			zap.String("request_id", valCtx.RequestID),
			zap.Error(err),
		)
		// Fail-open: treat evaluation errors as allowed
		results.Allowed = true
		results.Message = "Response evaluation failed, allowing (fail-open)"
	}

	action := "allow"
	if !results.Allowed {
		action = "deny"
	} else if results.RedactedContent != nil {
		action = "redact"
	}
	actionReason := DeriveAuditActionReason(results.Allowed, results.AuditModeBypass, results.FailedOpen, ActionReasonResponsePolicy)

	h.writeResponseAuditEntry(r, start, valCtx, req, action, actionReason, &results)

	return h.buildResponseResult(req, valCtx, results, start)
}

// payloadToCallToolResult converts an InterceptPayload's result to an mcp.CallToolResult.
func (h *InterceptHandler) payloadToCallToolResult(req *InterceptRequest) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}
	if req.Payload.Result == nil {
		return result
	}
	for _, c := range req.Payload.Result.Content {
		if c.Type == "text" {
			result.Content = append(result.Content, mcp.TextContent{
				Type: "text",
				Text: c.Text,
			})
		}
	}
	return result
}

// buildResponseResult maps ResponseValidationResults to an SEP-1763 InterceptResponse.
// Redacted responses produce type="mutation" with modified=true and the redacted payload.
func (h *InterceptHandler) buildResponseResult(
	req *InterceptRequest,
	valCtx *CLIValidationContext,
	results ResponseValidationResults,
	start time.Time,
) *InterceptResponse {
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

	resp := &InterceptResponse{
		Interceptor: "maybe-dont",
		Type:        "validation",
		Phase:       req.Phase,
		Valid:       results.Allowed,
		Severity:    severity,
		Messages:    messages,
		DurationMs:  time.Since(start).Milliseconds(),
		Info: InterceptInfo{
			RequestID:     valCtx.RequestID,
			ServerVersion: h.config.Version,
			Results:       policyResults,
		},
	}

	// Redaction produces a mutation response with the modified payload
	if results.RedactedContent != nil {
		resp.Type = "mutation"
		resp.Modified = true
		resp.Payload = &InterceptPayload{
			Name:      req.Payload.Name,
			Arguments: req.Payload.Arguments,
			Result: &InterceptResult{
				Content: []InterceptContent{
					{Type: "text", Text: *results.RedactedContent},
				},
			},
		}
	}

	return resp
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
// as both a CLI command (cli_expression) and an MCP tool call (mcp_expression)
// under a single shared blocking budget.
func (h *InterceptHandler) evaluateShellCommand(ctx context.Context, req *InterceptRequest) ValidationResults {
	cliReq := h.parseShellCommand(req)
	mcpReq := h.payloadToCallToolRequest(req)

	// Use shared budget so CLI + MCP evaluation together respects MaxBlockingMs
	cliResults, mcpResults := h.config.Evaluator.EvaluateShellCommand(ctx, cliReq, mcpReq)

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
// RulesDetails and AIDetails from both sides are merged by concatenating
// individual rule results so the audit trail is complete.
func (h *InterceptHandler) mergeShellResults(cliResults, mcpResults ValidationResults) ValidationResults {
	merged := ValidationResults{
		Allowed: true,
	}

	// Merge per-policy results and counts from both evaluations
	merged.Results = append(merged.Results, cliResults.Results...)
	merged.Results = append(merged.Results, mcpResults.Results...)
	merged.AllowCount = cliResults.AllowCount + mcpResults.AllowCount
	merged.DenyCount = cliResults.DenyCount + mcpResults.DenyCount

	// Propagate FailedOpen from either side
	if cliResults.FailedOpen || mcpResults.FailedOpen {
		merged.FailedOpen = true
	}

	// Merge AuditModeBypass (set if either side bypassed)
	if cliResults.AuditModeBypass || mcpResults.AuditModeBypass {
		merged.AuditModeBypass = true
	}

	// Merge RulesDetails: concatenate rule results from both evaluations
	merged.RulesDetails = mergeAuditRulesResults(cliResults.RulesDetails, mcpResults.RulesDetails)

	// Merge AIDetails: concatenate rule results from both evaluations
	merged.AIDetails = mergeAuditAIResults(cliResults.AIDetails, mcpResults.AIDetails)

	// Async completion: prefer MCP, fall back to CLI. When both are present,
	// only one is captured — this is acceptable because async completions are
	// for audit_only AI evaluation and losing one follow-up entry is low-risk.
	if mcpResults.AsyncCompletion != nil {
		merged.AsyncCompletion = mcpResults.AsyncCompletion
	} else if cliResults.AsyncCompletion != nil {
		merged.AsyncCompletion = cliResults.AsyncCompletion
	}

	// Determine allowed/denied: if either evaluation denies, the merged result is deny.
	// CLI message takes precedence (first writer wins).
	if !cliResults.Allowed {
		merged.Allowed = false
		merged.Message = cliResults.Message
	}
	if !mcpResults.Allowed {
		merged.Allowed = false
		if merged.Message == "" {
			merged.Message = mcpResults.Message
		}
	}

	// Clear AuditModeBypass if an enforced deny overrides it
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

// mergeAuditRulesResults combines RulesDetails from two evaluations by concatenating
// their rule results. Returns nil if both inputs are nil.
func mergeAuditRulesResults(a, b *AuditRulesResult) *AuditRulesResult {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}

	merged := &AuditRulesResult{
		Results: append(a.Results, b.Results...),
	}

	// Use the most restrictive action (deny > allow)
	if a.Action == "deny" || b.Action == "deny" {
		merged.Action = "deny"
	} else {
		merged.Action = a.Action
	}

	// Use first non-empty deciding rule
	if a.DecidingRule != "" {
		merged.DecidingRule = a.DecidingRule
		merged.Reason = a.Reason
	} else if b.DecidingRule != "" {
		merged.DecidingRule = b.DecidingRule
		merged.Reason = b.Reason
	}

	// Sum timing
	merged.BlockedMs = a.BlockedMs + b.BlockedMs
	merged.EvaluationMs = a.EvaluationMs + b.EvaluationMs

	return merged
}

// mergeAuditAIResults combines AIDetails from two evaluations by concatenating
// their rule results. Returns nil if both inputs are nil.
func mergeAuditAIResults(a, b *AuditAIResult) *AuditAIResult {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}

	merged := &AuditAIResult{
		Results: append(a.Results, b.Results...),
	}

	// Use the most restrictive action (deny > allow)
	if a.Action == "deny" || b.Action == "deny" {
		merged.Action = "deny"
	} else {
		merged.Action = a.Action
	}

	// Use first non-empty deciding rule
	if a.DecidingRule != "" {
		merged.DecidingRule = a.DecidingRule
		merged.Reason = a.Reason
	} else if b.DecidingRule != "" {
		merged.DecidingRule = b.DecidingRule
		merged.Reason = b.Reason
	}

	// Sum timing
	merged.BlockedMs = a.BlockedMs + b.BlockedMs
	merged.EvaluationMs = a.EvaluationMs + b.EvaluationMs

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

// --- Audit Logging ---

// writeRequestAuditEntry creates and writes an audit entry for request phase evaluations.
// Shell tools populate both Tool and CLI fields; non-shell tools populate Tool only.
func (h *InterceptHandler) writeRequestAuditEntry(
	r *http.Request,
	start time.Time,
	valCtx *CLIValidationContext,
	req *InterceptRequest,
	action string,
	actionReason string,
	results *ValidationResults,
) {
	if h.config.AuditWriter == nil {
		return
	}

	now := time.Now().UTC()
	entry := &AuditEntry{
		Source:            "intercept",
		ValidationStarted: start.Format(time.RFC3339Nano),
		CreatedAt:         now.Format(time.RFC3339Nano),
		Tool:              h.buildAuditToolInfo(req),
		UpstreamRequest:   h.buildUpstreamRequestInfo(r, valCtx, req),
		Action:            action,
		ActionReason:      actionReason,
		DurationMs:        now.Sub(start).Milliseconds(),
	}

	// Shell tools also get CLI info
	if h.isShellTool(req.Payload.Name) {
		entry.CLI = h.buildAuditCLIInfo(req)
	}

	// Attach validation details
	if results != nil && (results.RulesDetails != nil || results.AIDetails != nil) {
		entry.RequestValidation = &AuditValidationInfo{
			CEL: results.RulesDetails,
			AI:  results.AIDetails,
		}
	}

	_, _ = h.config.AuditWriter.Write(entry)

	// Handle async AI completion
	if results != nil && results.AsyncCompletion != nil {
		WriteAsyncAuditCompletion(h.config.AuditWriter, h.config.Logger, valCtx.RequestID,
			results.AsyncCompletion, func(completion AsyncCompletion) *AuditEntry {
				return &AuditEntry{
					Source:            "intercept",
					ValidationStarted: start.Format(time.RFC3339Nano),
					CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
					Tool:              h.buildAuditToolInfo(req),
					UpstreamRequest: UpstreamRequestInfo{
						RequestID:  valCtx.RequestID + "-async",
						ExternalID: h.getTraceID(req),
						ClientID:   h.resolveClientID(valCtx, req),
					},
					RequestValidation: &AuditValidationInfo{
						AI: completion.AIDetails,
					},
					Action:       action,
					ActionReason: "async_completion",
					DurationMs:   completion.EvaluationMs,
				}
			})
	}
}

// writeResponseAuditEntry creates and writes an audit entry for response phase evaluations.
func (h *InterceptHandler) writeResponseAuditEntry(
	r *http.Request,
	start time.Time,
	valCtx *CLIValidationContext,
	req *InterceptRequest,
	action string,
	actionReason string,
	results *ResponseValidationResults,
) {
	if h.config.AuditWriter == nil {
		return
	}

	now := time.Now().UTC()
	entry := &AuditEntry{
		Source:            "intercept",
		ValidationStarted: start.Format(time.RFC3339Nano),
		CreatedAt:         now.Format(time.RFC3339Nano),
		Tool:              h.buildAuditToolInfo(req),
		UpstreamRequest:   h.buildUpstreamRequestInfo(r, valCtx, req),
		Action:            action,
		ActionReason:      actionReason,
		DurationMs:        now.Sub(start).Milliseconds(),
	}

	// Attach response validation details
	if results != nil && (results.RulesDetails != nil || results.AIDetails != nil) {
		entry.ResponseValidation = &AuditValidationInfo{
			CEL: results.RulesDetails,
			AI:  results.AIDetails,
		}
	}

	_, _ = h.config.AuditWriter.Write(entry)

	// Handle async AI completion
	if results != nil && results.AsyncCompletion != nil {
		WriteAsyncAuditCompletion(h.config.AuditWriter, h.config.Logger, valCtx.RequestID,
			results.AsyncCompletion, func(completion AsyncCompletion) *AuditEntry {
				return &AuditEntry{
					Source:            "intercept",
					ValidationStarted: start.Format(time.RFC3339Nano),
					CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
					Tool:              h.buildAuditToolInfo(req),
					UpstreamRequest: UpstreamRequestInfo{
						RequestID:  valCtx.RequestID + "-async",
						ExternalID: h.getTraceID(req),
						ClientID:   h.resolveClientID(valCtx, req),
					},
					ResponseValidation: &AuditValidationInfo{
						AI: completion.AIDetails,
					},
					Action:       action,
					ActionReason: "async_completion",
					DurationMs:   completion.EvaluationMs,
				}
			})
	}
}

// buildAuditToolInfo creates the Tool section of an audit entry from the intercept request.
// If the payload name is prefixed (e.g., "github__create_issue"), the bare name is extracted.
func (h *InterceptHandler) buildAuditToolInfo(req *InterceptRequest) *AuditToolInfo {
	var params map[string]any
	if h.config.IncludeArgumentValues {
		params = req.Payload.Arguments
	}

	prefixedName := req.Payload.Name
	bareName := prefixedName
	if _, original, err := ParsePrefixedName(prefixedName); err == nil {
		bareName = original
	}

	return &AuditToolInfo{
		Name:         bareName,
		PrefixedName: prefixedName,
		Params:       params,
	}
}

// parseShellCommand extracts the CLI command, arguments, and working directory
// from an intercept request's payload. Used by both evaluation and audit logging
// to ensure consistency between what is validated and what is recorded.
func (h *InterceptHandler) parseShellCommand(req *InterceptRequest) *CLIValidationRequest {
	commandStr, _ := req.Payload.Arguments["command"].(string)
	parts := strings.Fields(commandStr)

	var command string
	var args []string
	if len(parts) > 0 {
		command = parts[0]
		args = parts[1:]
	}

	var workingDir string
	if req.Config != nil {
		workingDir = req.Config.WorkingDirectory
	}

	return &CLIValidationRequest{
		Command:          command,
		Arguments:        args,
		WorkingDirectory: workingDir,
	}
}

// buildAuditCLIInfo creates the CLI section of an audit entry for shell tool calls.
func (h *InterceptHandler) buildAuditCLIInfo(req *InterceptRequest) *AuditCLIInfo {
	parsed := h.parseShellCommand(req)
	return &AuditCLIInfo{
		Command:          parsed.Command,
		Arguments:        parsed.Arguments,
		WorkingDirectory: parsed.WorkingDirectory,
	}
}

// buildUpstreamRequestInfo creates the UpstreamRequest section of an audit entry.
// Maps intercept context fields to the standard upstream request fields.
func (h *InterceptHandler) buildUpstreamRequestInfo(r *http.Request, valCtx *CLIValidationContext, req *InterceptRequest) UpstreamRequestInfo {
	return UpstreamRequestInfo{
		RequestID:  valCtx.RequestID,
		ExternalID: h.getTraceID(req),
		ClientID:   h.resolveClientID(valCtx, req),
		SessionID:  h.getSessionID(req),
		ClientIP:   r.RemoteAddr,
		UserAgent:  r.Header.Get("User-Agent"),
	}
}

// resolveClientID returns the client ID from headers (preferred) or falls back to principal.id.
func (h *InterceptHandler) resolveClientID(valCtx *CLIValidationContext, req *InterceptRequest) string {
	if valCtx.ClientID != "" {
		return valCtx.ClientID
	}
	if req.Context != nil && req.Context.Principal != nil {
		return req.Context.Principal.ID
	}
	return ""
}

// getTraceID extracts the trace ID from the intercept context.
func (h *InterceptHandler) getTraceID(req *InterceptRequest) string {
	if req.Context != nil {
		return req.Context.TraceID
	}
	return ""
}

// getSessionID extracts the session ID from the intercept context.
func (h *InterceptHandler) getSessionID(req *InterceptRequest) string {
	if req.Context != nil {
		return req.Context.SessionID
	}
	return ""
}

func (h *InterceptHandler) extractContext(r *http.Request) *CLIValidationContext {
	return extractHTTPContext(r, h.config.Logger)
}

// InterceptError is the error response body for the intercept endpoint.
type InterceptError struct {
	// Error is a machine-readable error code.
	// Values: "intercept_disabled", "invalid_content_type", "invalid_request",
	// "unsupported_event", "invalid_phase", "missing_tool_name", "missing_result"
	Error string `json:"error"`

	// Message is a human-readable description of the error.
	Message string `json:"message"`
}

func (h *InterceptHandler) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(InterceptError{
		Error:   code,
		Message: message,
	})
}
