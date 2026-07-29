package gateway

import (
	"time"
)

// AuditAIProvider contains AI provider metadata for audit logging.
// This is populated when AI validation is enabled and provides traceability
// for debugging and monitoring AI-powered validation.
type AuditAIProvider struct {
	Provider     string `json:"provider"`                // Provider name: "openai", "anthropic", "openai_compatible"
	Model        string `json:"model"`                   // Model identifier used for validation
	EndpointHost string `json:"endpoint_host"`           // API endpoint host (e.g., "api.openai.com")
	EndpointPath string `json:"endpoint_path,omitempty"` // API endpoint path (query params stripped for security)
}

// NewAuditAIProvider creates an AuditAIProvider from provider info.
// The AIProviderInfo already contains sanitized endpoint host and path
// (query params stripped for security, as they may contain secrets).
func NewAuditAIProvider(info AIProviderInfo) *AuditAIProvider {
	return &AuditAIProvider{
		Provider:     info.Provider,
		Model:        info.Model,
		EndpointHost: info.EndpointHost,
		EndpointPath: info.EndpointPath,
	}
}

// AuditEntry represents a single consolidated audit log entry for a tool call, CLI validation, or action validation
type AuditEntry struct {
	// Source identifies which validation path produced this entry: "mcp", "cli", or "action"
	Source string `json:"source,omitempty"`

	// Temporal fields - all in RFC3339Nano format
	ValidationStarted string `json:"validation_started"` // When we received the tool call and began validation
	CreatedAt         string `json:"created_at"`         // When this audit entry was finalized and written

	// Tool is populated for MCP tool calls and action validations (nil for CLI validations).
	Tool *AuditToolInfo `json:"tool,omitempty"`

	// CLI is populated for CLI command validations (nil for MCP tool calls and action validations).
	CLI *AuditCLIInfo `json:"cli,omitempty"`

	// Upstream request metadata (about the incoming request, not the tool call)
	UpstreamRequest UpstreamRequestInfo `json:"upstream_request"`

	// AI provider metadata (populated when AI validation is enabled)
	AI *AuditAIProvider `json:"ai,omitempty"`

	// Validation results
	RequestValidation  *AuditValidationInfo `json:"request_validation,omitempty"`
	ResponseValidation *AuditValidationInfo `json:"response_validation,omitempty"`

	// Actions
	RecommendedAction string `json:"recommended_action,omitempty"` // Omitted when fail-open prevents complete evaluation
	Action            string `json:"action"`
	ActionReason      string `json:"action_reason,omitempty"` // request_policy, response_policy, audit_mode, fail_open

	// Timing
	DurationMs     int64 `json:"duration_ms"`      // Total wall-clock time from validation_started to created_at
	TotalBlockedMs int64 `json:"total_blocked_ms"` // Time caller was blocked (validation + tool call)
}

// AuditToolInfo contains tool identification and execution details.
// Populated for MCP tool calls, nil for CLI validations.
type AuditToolInfo struct {
	// Identity
	Name         string `json:"name"`
	Client       string `json:"client"`
	PrefixedName string `json:"prefixed_name"`

	// Execution details
	Params     map[string]interface{} `json:"params,omitempty"`
	CalledAt   string                 `json:"called_at,omitempty"`   // When downstream tool was invoked (omitted if denied)
	DurationMs *int64                 `json:"duration_ms,omitempty"` // Downstream call duration (omitted if denied)
}

// AuditCLIInfo contains information about a CLI command validation.
// Populated for CLI validations, nil for MCP tool calls.
type AuditCLIInfo struct {
	Command          string         `json:"command"`
	Arguments        []string       `json:"arguments"`
	WorkingDirectory string         `json:"working_directory,omitempty"`
	ClientInfo       *CLIClientInfo `json:"client_info,omitempty"`
}

// UpstreamRequestInfo contains metadata about the incoming request
type UpstreamRequestInfo struct {
	RequestID  string `json:"id,omitempty"`
	ExternalID string `json:"external_id,omitempty"` // Caller-provided correlation ID (e.g., OpenHands action.id)
	ClientID   string `json:"client_id,omitempty"`   // Caller identifier for audit attribution (from X-Maybe-Dont-Client-ID header)
	SessionID  string `json:"session_id,omitempty"`
	ClientIP   string `json:"client_ip,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"` // User-Agent header from incoming request
}

// AuditValidationInfo contains validation results for CEL and AI policies
type AuditValidationInfo struct {
	CEL *AuditRulesResult `json:"cel,omitempty"` // Deterministic CEL rule evaluation
	AI  *AuditAIResult    `json:"ai,omitempty"`  // AI-powered validation
}

// AuditRulesResult contains the result of deterministic rule evaluation
type AuditRulesResult struct {
	Action       string                 `json:"action"`                  // Final decision: "allow", "deny", or "redact"
	BlockedMs    int64                  `json:"blocked_ms"`              // Time this phase contributed to blocking
	EvaluationMs int64                  `json:"evaluation_ms"`           // Total wall-clock time for all rules to complete
	DecidingRule string                 `json:"deciding_rule,omitempty"` // Rule that caused the decision (omitted if none)
	Reason       string                 `json:"reason,omitempty"`        // Message from deciding rule
	Results      []AuditRulesRuleResult `json:"results"`                 // Per-rule results ordered by evaluation order
}

// AuditRulesRuleResult contains the result of a single deterministic rule
type AuditRulesRuleResult struct {
	Rule         string `json:"rule"`            // Rule name from definition
	Action       string `json:"action"`          // Rule's configured action: "allow", "deny", or "redact"
	Mode         string `json:"mode,omitempty"`  // Only present if "audit_only"
	Result       string `json:"result"`          // Effective decision: "allow", "deny", or "redact"
	EvaluationMs int64  `json:"evaluation_ms"`   // Time for this rule to complete
	Error        string `json:"error,omitempty"` // Only present when result is "error"
}

// AuditAIResult contains the result of AI policy evaluation with detailed timing
type AuditAIResult struct {
	Action       string              `json:"action"`                  // Final decision: "allow" or "deny"
	BlockedMs    int64               `json:"blocked_ms"`              // Time request was blocked waiting for decision
	EvaluationMs int64               `json:"evaluation_ms"`           // Total wall-clock time for all rules to complete
	DecidingRule string              `json:"deciding_rule,omitempty"` // Rule that caused the decision (omitted if none)
	Reason       string              `json:"reason,omitempty"`        // Message from deciding rule's AI response
	RequestID    string              `json:"request_id,omitempty"`    // Provider request ID for debugging/tracing
	Results      []AuditAIRuleResult `json:"results"`                 // Per-rule results ordered by completion time
}

// AuditAIRuleResult contains the result of a single AI rule evaluation
type AuditAIRuleResult struct {
	Rule         string `json:"rule"`            // Rule name from definition
	Action       string `json:"action"`          // Rule's configured action: "allow" or "deny"
	Mode         string `json:"mode,omitempty"`  // Only present if "audit_only"
	Result       string `json:"result"`          // What rule contributed: "allow", "deny", or "error"
	EvaluationMs int64  `json:"evaluation_ms"`   // Time for this rule to complete
	Error        string `json:"error,omitempty"` // Only present when result is "error"
}

// AuditContext is used to accumulate audit data through the request lifecycle
type AuditContext struct {
	entry           *AuditEntry
	validationStart time.Time

	// Async AI validation support
	requestAICompletion  <-chan AsyncCompletion // For async request AI validation
	responseAICompletion <-chan AsyncCompletion // For async response AI validation
}

// NewAuditContext creates a new audit context for an MCP tool call
func NewAuditContext(prefixedToolName, clientName, toolName, sessionID, clientIP, requestID string) *AuditContext {
	now := time.Now().UTC()
	return &AuditContext{
		entry: &AuditEntry{
			Source:            "mcp",
			ValidationStarted: now.Format(time.RFC3339Nano),
			Tool: &AuditToolInfo{
				Name:         toolName,
				Client:       clientName,
				PrefixedName: prefixedToolName,
			},
			UpstreamRequest: UpstreamRequestInfo{
				RequestID: requestID,
				SessionID: sessionID,
				ClientIP:  clientIP,
			},
		},
		validationStart: now,
	}
}

// SetToolParams sets the tool call parameters
func (ac *AuditContext) SetToolParams(params map[string]interface{}) {
	ac.entry.Tool.Params = params
}

// SetUserAgent sets the User-Agent header from the incoming request
func (ac *AuditContext) SetUserAgent(userAgent string) {
	ac.entry.UpstreamRequest.UserAgent = userAgent
}

// SetAIProvider sets the AI provider metadata for the audit entry.
// This should be called when AI validation is enabled.
func (ac *AuditContext) SetAIProvider(provider *AuditAIProvider) {
	ac.entry.AI = provider
}

// SetToolCalledAt records when the downstream tool was invoked
func (ac *AuditContext) SetToolCalledAt() {
	ac.entry.Tool.CalledAt = time.Now().UTC().Format(time.RFC3339Nano)
}

// SetToolDuration sets the duration of the downstream tool call
func (ac *AuditContext) SetToolDuration(durationMs int64) {
	ac.entry.Tool.DurationMs = &durationMs
}

// SetRequestValidationRules sets deterministic CEL rules request validation result
func (ac *AuditContext) SetRequestValidationRules(rulesResult *AuditRulesResult) {
	if ac.entry.RequestValidation == nil {
		ac.entry.RequestValidation = &AuditValidationInfo{}
	}
	ac.entry.RequestValidation.CEL = rulesResult
}

// SetRequestValidationAI sets AI request validation result with detailed timing and per-rule results
func (ac *AuditContext) SetRequestValidationAI(aiResult *AuditAIResult) {
	if ac.entry.RequestValidation == nil {
		ac.entry.RequestValidation = &AuditValidationInfo{}
	}
	ac.entry.RequestValidation.AI = aiResult
}

// SetResponseValidationRules sets deterministic CEL rules response validation result
func (ac *AuditContext) SetResponseValidationRules(rulesResult *AuditRulesResult) {
	if ac.entry.ResponseValidation == nil {
		ac.entry.ResponseValidation = &AuditValidationInfo{}
	}
	ac.entry.ResponseValidation.CEL = rulesResult
}

// SetResponseValidationAI sets AI response validation result with detailed timing and per-rule results
func (ac *AuditContext) SetResponseValidationAI(aiResult *AuditAIResult) {
	if ac.entry.ResponseValidation == nil {
		ac.entry.ResponseValidation = &AuditValidationInfo{}
	}
	ac.entry.ResponseValidation.AI = aiResult
}

// SetActions sets the recommended and actual actions with an optional reason.
// The reason explains why the action was taken (request_policy, response_policy, audit_mode, fail_open).
// Pass empty ActionReason when action == recommended_action with no special circumstances.
func (ac *AuditContext) SetActions(recommended, actual string, reason ActionReason) {
	ac.entry.RecommendedAction = recommended
	ac.entry.Action = actual
	ac.entry.ActionReason = string(reason)
}

// SetTotalBlockedMs sets the cumulative time blocked (validation + tool call)
func (ac *AuditContext) SetTotalBlockedMs(totalBlockedMs int64) {
	ac.entry.TotalBlockedMs = totalBlockedMs
}

// SetRequestAIResultsAsync registers a completion channel for async request AI validation.
// The channel will be read during FinalizeAsync to get the final AI results.
func (ac *AuditContext) SetRequestAIResultsAsync(completion <-chan AsyncCompletion) {
	ac.requestAICompletion = completion
}

// SetResponseAIResultsAsync registers a completion channel for async response AI validation.
// The channel will be read during FinalizeAsync to get the final AI results.
func (ac *AuditContext) SetResponseAIResultsAsync(completion <-chan AsyncCompletion) {
	ac.responseAICompletion = completion
}

// HasAsyncWork returns true if there are any pending async completions to wait for.
func (ac *AuditContext) HasAsyncWork() bool {
	return ac.requestAICompletion != nil || ac.responseAICompletion != nil
}

// Finalize calculates duration and sets created_at timestamp.
// This should only be called when there is no async work pending.
// For async workflows, use FinalizeAsync instead.
func (ac *AuditContext) Finalize() *AuditEntry {
	now := time.Now().UTC()
	ac.entry.CreatedAt = now.Format(time.RFC3339Nano)
	ac.entry.DurationMs = now.Sub(ac.validationStart).Milliseconds()
	return ac.entry
}

// FinalizeAsync waits for all async AI results before finalizing the audit entry.
// This should be called in a goroutine to avoid blocking the response.
// It waits for both request and response AI completions if they are registered,
// then calculates the final duration and returns the completed entry.
func (ac *AuditContext) FinalizeAsync() *AuditEntry {
	// Wait for async request AI results
	if ac.requestAICompletion != nil {
		completion := <-ac.requestAICompletion
		if completion.AIDetails != nil {
			ac.SetRequestValidationAI(completion.AIDetails)
		}
	}

	// Wait for async response AI results
	if ac.responseAICompletion != nil {
		completion := <-ac.responseAICompletion
		if completion.AIDetails != nil {
			ac.SetResponseValidationAI(completion.AIDetails)
		}
	}

	// Finalize with the updated results
	return ac.Finalize()
}

// Entry returns the current audit entry (for inspection)
func (ac *AuditContext) Entry() *AuditEntry {
	return ac.entry
}

// Deprecated: use SetToolDuration instead.
func (ac *AuditContext) SetRequestDuration(durationMs int64) {
	ac.SetToolDuration(durationMs)
}
