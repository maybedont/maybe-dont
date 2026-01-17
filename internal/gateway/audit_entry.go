package gateway

import (
	"time"
)

// AuditEntry represents a single consolidated audit log entry for a tool call
type AuditEntry struct {
	// Temporal fields - all in RFC3339Nano format
	ValidationStarted string `json:"validation_started"` // When we received the tool call and began validation
	CreatedAt         string `json:"created_at"`         // When this audit entry was finalized and written

	// Tool call information (identity + execution details)
	Tool AuditToolInfo `json:"tool"`

	// Upstream request metadata (about the incoming request, not the tool call)
	UpstreamRequest UpstreamRequestInfo `json:"upstream_request"`

	// Validation results
	RequestValidation  *AuditValidationInfo `json:"request_validation,omitempty"`
	Response           *AuditResponseInfo   `json:"response,omitempty"`
	ResponseValidation *AuditValidationInfo `json:"response_validation,omitempty"`

	// Actions
	RecommendedAction string `json:"recommended_action"`
	Action            string `json:"action"`

	// Timing
	DurationMs     int64 `json:"duration_ms"`       // Total wall-clock time from validation_started to created_at
	TotalBlockedMs int64 `json:"total_blocked_ms"`  // Time caller was blocked (validation + tool call)
}

// AuditToolInfo contains tool identification and execution details
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

// UpstreamRequestInfo contains metadata about the incoming request
type UpstreamRequestInfo struct {
	RequestID string `json:"id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ClientIP  string `json:"client_ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"` // User-Agent header from incoming request
}

// AuditResponseInfo contains response details (minimal to avoid logging sensitive data)
type AuditResponseInfo struct {
	ContentItems int  `json:"content_items"`
	IsError      bool `json:"is_error"`
}

// AuditValidationInfo contains validation results for rules and AI policies
type AuditValidationInfo struct {
	Rules *AuditRulesResult `json:"rules,omitempty"` // Deterministic rule evaluation (was "cel")
	AI    *AuditAIResult    `json:"ai,omitempty"`    // AI-powered validation
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
	Rule         string `json:"rule"`           // Rule name from definition
	Action       string `json:"action"`         // Rule's configured action: "allow", "deny", or "redact"
	Mode         string `json:"mode,omitempty"` // Only present if "audit_only"
	Result       string `json:"result"`         // What rule contributed: "allow", "deny", "redact", or "no_match"
	EvaluationMs int64  `json:"evaluation_ms"`  // Time for this rule to complete
	Error        string `json:"error,omitempty"` // Only present when result is "error"
}

// AuditAIResult contains the result of AI policy evaluation with detailed timing
type AuditAIResult struct {
	Action       string              `json:"action"`                  // Final decision: "allow" or "deny"
	BlockedMs    int64               `json:"blocked_ms"`              // Time request was blocked waiting for decision
	EvaluationMs int64               `json:"evaluation_ms"`           // Total wall-clock time for all rules to complete
	DecidingRule string              `json:"deciding_rule,omitempty"` // Rule that caused the decision (omitted if none)
	Reason       string              `json:"reason,omitempty"`        // Message from deciding rule's AI response
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
}

// NewAuditContext creates a new audit context for a tool call
func NewAuditContext(prefixedToolName, clientName, toolName, sessionID, clientIP, requestID string) *AuditContext {
	now := time.Now().UTC()
	return &AuditContext{
		entry: &AuditEntry{
			ValidationStarted: now.Format(time.RFC3339Nano),
			Tool: AuditToolInfo{
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

// SetToolCalledAt records when the downstream tool was invoked
func (ac *AuditContext) SetToolCalledAt() {
	ac.entry.Tool.CalledAt = time.Now().UTC().Format(time.RFC3339Nano)
}

// SetToolDuration sets the duration of the downstream tool call
func (ac *AuditContext) SetToolDuration(durationMs int64) {
	ac.entry.Tool.DurationMs = &durationMs
}

// SetRequestValidationRules sets deterministic rules request validation result
func (ac *AuditContext) SetRequestValidationRules(rulesResult *AuditRulesResult) {
	if ac.entry.RequestValidation == nil {
		ac.entry.RequestValidation = &AuditValidationInfo{}
	}
	ac.entry.RequestValidation.Rules = rulesResult
}

// SetRequestValidationAI sets AI request validation result with detailed timing and per-rule results
func (ac *AuditContext) SetRequestValidationAI(aiResult *AuditAIResult) {
	if ac.entry.RequestValidation == nil {
		ac.entry.RequestValidation = &AuditValidationInfo{}
	}
	ac.entry.RequestValidation.AI = aiResult
}

// SetResponse sets the response information
func (ac *AuditContext) SetResponse(contentItems int, isError bool) {
	ac.entry.Response = &AuditResponseInfo{
		ContentItems: contentItems,
		IsError:      isError,
	}
}

// SetResponseValidationRules sets deterministic rules response validation result
func (ac *AuditContext) SetResponseValidationRules(rulesResult *AuditRulesResult) {
	if ac.entry.ResponseValidation == nil {
		ac.entry.ResponseValidation = &AuditValidationInfo{}
	}
	ac.entry.ResponseValidation.Rules = rulesResult
}

// SetResponseValidationAI sets AI response validation result with detailed timing and per-rule results
func (ac *AuditContext) SetResponseValidationAI(aiResult *AuditAIResult) {
	if ac.entry.ResponseValidation == nil {
		ac.entry.ResponseValidation = &AuditValidationInfo{}
	}
	ac.entry.ResponseValidation.AI = aiResult
}

// SetActions sets the recommended and actual actions
func (ac *AuditContext) SetActions(recommended, actual string) {
	ac.entry.RecommendedAction = recommended
	ac.entry.Action = actual
}

// SetTotalBlockedMs sets the cumulative time blocked (validation + tool call)
func (ac *AuditContext) SetTotalBlockedMs(totalBlockedMs int64) {
	ac.entry.TotalBlockedMs = totalBlockedMs
}

// Finalize calculates duration and sets created_at timestamp
func (ac *AuditContext) Finalize() *AuditEntry {
	now := time.Now().UTC()
	ac.entry.CreatedAt = now.Format(time.RFC3339Nano)
	ac.entry.DurationMs = now.Sub(ac.validationStart).Milliseconds()
	return ac.entry
}

// Entry returns the current audit entry (for inspection)
func (ac *AuditContext) Entry() *AuditEntry {
	return ac.entry
}

// Deprecated method aliases for backwards compatibility during migration
// These will be removed in a future version

// SetRequestValidationCEL is deprecated: use SetRequestValidationRules instead
func (ac *AuditContext) SetRequestValidationCEL(celResult *AuditRulesResult) {
	ac.SetRequestValidationRules(celResult)
}

// SetResponseValidationCEL is deprecated: use SetResponseValidationRules instead
func (ac *AuditContext) SetResponseValidationCEL(celResult *AuditRulesResult) {
	ac.SetResponseValidationRules(celResult)
}

// SetRequestParams is deprecated: use SetToolParams instead
func (ac *AuditContext) SetRequestParams(params map[string]interface{}) {
	ac.SetToolParams(params)
}

// SetRequestDuration is deprecated: use SetToolDuration instead
func (ac *AuditContext) SetRequestDuration(durationMs int64) {
	ac.SetToolDuration(durationMs)
}
