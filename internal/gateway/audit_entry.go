package gateway

import (
	"time"
)

// AuditEntry represents a single consolidated audit log entry for a tool call
type AuditEntry struct {
	CreatedAt          string               `json:"created_at"`
	Tool               AuditToolInfo        `json:"tool"`
	IncomingRequest    IncomingRequestInfo  `json:"incoming_request"`
	Request            AuditRequestInfo     `json:"request"`
	RequestValidation  *AuditValidationInfo `json:"request_validation,omitempty"`
	Response           *AuditResponseInfo   `json:"response,omitempty"`
	ResponseValidation *AuditValidationInfo `json:"response_validation,omitempty"`
	RecommendedAction  string               `json:"recommended_action"`
	Action             string               `json:"action"`
	DurationMs         int64                `json:"duration_ms"`
	TotalBlockedMs     int64                `json:"total_blocked_ms"` // Cumulative time blocked across all validation phases
}

// AuditToolInfo contains tool identification information
type AuditToolInfo struct {
	Name         string `json:"name"`
	Client       string `json:"client"`
	PrefixedName string `json:"prefixed_name"`
}

// IncomingRequestInfo contains incoming request session and client information
type IncomingRequestInfo struct {
	RequestID string `json:"id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ClientIP  string `json:"client_ip,omitempty"`
}

// AuditRequestInfo contains request details and timing
type AuditRequestInfo struct {
	Params     map[string]interface{} `json:"params,omitempty"`
	DurationMs *int64                 `json:"duration_ms,omitempty"`
}

// AuditResponseInfo contains response details (minimal to avoid logging sensitive data)
type AuditResponseInfo struct {
	ContentItems int  `json:"content_items"`
	IsError      bool `json:"is_error"`
}

// AuditValidationInfo contains validation results for CEL and AI policies
type AuditValidationInfo struct {
	CEL *AuditCELResult `json:"cel,omitempty"`
	AI  *AuditAIResult  `json:"ai,omitempty"`
}

// AuditCELResult contains the result of CEL policy evaluation with detailed timing
type AuditCELResult struct {
	Action       string               `json:"action"`                  // Final decision: "allow", "deny", or "redact"
	BlockedMs    int64                `json:"blocked_ms"`              // Time this phase contributed to blocking
	EvaluationMs int64                `json:"evaluation_ms"`           // Total wall-clock time for all rules to complete
	DecidingRule string               `json:"deciding_rule,omitempty"` // Rule that caused the decision (omitted if none)
	Reason       string               `json:"reason,omitempty"`        // Message from deciding rule
	Results      []AuditCELRuleResult `json:"results"`                 // Per-rule results ordered by evaluation order
}

// AuditCELRuleResult contains the result of a single CEL rule evaluation
type AuditCELRuleResult struct {
	Rule         string `json:"rule"`                    // Rule name from definition
	Action       string `json:"action"`                  // Rule's configured action: "allow", "deny", or "redact"
	Mode         string `json:"mode,omitempty"`          // Only present if "audit_only"
	Result       string `json:"result"`                  // What rule contributed: "allow", "deny", "redact", or "no_match"
	EvaluationMs int64  `json:"evaluation_ms"`           // Time for this rule to complete
	Error        string `json:"error,omitempty"`         // Only present when result is "error"
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
	Rule         string `json:"rule"`                    // Rule name from definition
	Action       string `json:"action"`                  // Rule's configured action: "allow" or "deny"
	Mode         string `json:"mode,omitempty"`          // Only present if "audit_only"
	Result       string `json:"result"`                  // What rule contributed: "allow", "deny", or "error"
	EvaluationMs int64  `json:"evaluation_ms"`           // Time for this rule to complete
	Error        string `json:"error,omitempty"`         // Only present when result is "error"
}

// AuditContext is used to accumulate audit data through the request lifecycle
type AuditContext struct {
	entry     *AuditEntry
	startTime time.Time
}

// NewAuditContext creates a new audit context for a tool call
func NewAuditContext(prefixedToolName, clientName, toolName, sessionID, clientIP, requestID string) *AuditContext {
	now := time.Now().UTC()
	return &AuditContext{
		entry: &AuditEntry{
			CreatedAt: now.Format(time.RFC3339),
			Tool: AuditToolInfo{
				Name:         toolName,
				Client:       clientName,
				PrefixedName: prefixedToolName,
			},
			IncomingRequest: IncomingRequestInfo{
				RequestID: requestID,
				SessionID: sessionID,
				ClientIP:  clientIP,
			},
			Request: AuditRequestInfo{},
		},
		startTime: now,
	}
}

// SetRequestParams sets the request parameters
func (ac *AuditContext) SetRequestParams(params map[string]interface{}) {
	ac.entry.Request.Params = params
}

// SetRequestValidationCEL sets CEL request validation result with detailed timing and per-rule results
func (ac *AuditContext) SetRequestValidationCEL(celResult *AuditCELResult) {
	if ac.entry.RequestValidation == nil {
		ac.entry.RequestValidation = &AuditValidationInfo{}
	}
	ac.entry.RequestValidation.CEL = celResult
}

// SetRequestValidationAI sets AI request validation result with detailed timing and per-rule results
func (ac *AuditContext) SetRequestValidationAI(aiResult *AuditAIResult) {
	if ac.entry.RequestValidation == nil {
		ac.entry.RequestValidation = &AuditValidationInfo{}
	}
	ac.entry.RequestValidation.AI = aiResult
}

// SetRequestDuration sets the duration of the actual tool call (excluding validation)
func (ac *AuditContext) SetRequestDuration(durationMs int64) {
	ac.entry.Request.DurationMs = &durationMs
}

// SetResponse sets the response information
func (ac *AuditContext) SetResponse(contentItems int, isError bool) {
	ac.entry.Response = &AuditResponseInfo{
		ContentItems: contentItems,
		IsError:      isError,
	}
}

// SetResponseValidationCEL sets CEL response validation result with detailed timing and per-rule results
func (ac *AuditContext) SetResponseValidationCEL(celResult *AuditCELResult) {
	if ac.entry.ResponseValidation == nil {
		ac.entry.ResponseValidation = &AuditValidationInfo{}
	}
	ac.entry.ResponseValidation.CEL = celResult
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

// SetTotalBlockedMs sets the cumulative time blocked across all validation phases
func (ac *AuditContext) SetTotalBlockedMs(totalBlockedMs int64) {
	ac.entry.TotalBlockedMs = totalBlockedMs
}

// Finalize calculates the total duration and returns the completed entry
func (ac *AuditContext) Finalize() *AuditEntry {
	ac.entry.DurationMs = time.Since(ac.startTime).Milliseconds()
	return ac.entry
}

// Entry returns the current audit entry (for inspection)
func (ac *AuditContext) Entry() *AuditEntry {
	return ac.entry
}
