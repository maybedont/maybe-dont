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
	CEL *AuditPolicyResult `json:"cel,omitempty"`
	AI  *AuditPolicyResult `json:"ai,omitempty"`
}

// AuditPolicyResult contains the result of a single policy evaluation
type AuditPolicyResult struct {
	Action      string `json:"action"`
	RuleMatched string `json:"rule_matched,omitempty"`
	Reasoning   string `json:"reasoning,omitempty"`
	DurationMs  int64  `json:"duration_ms"`
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

// SetRequestValidationCEL sets CEL request validation result
func (ac *AuditContext) SetRequestValidationCEL(action, ruleMatched string, durationMs int64) {
	if ac.entry.RequestValidation == nil {
		ac.entry.RequestValidation = &AuditValidationInfo{}
	}
	ac.entry.RequestValidation.CEL = &AuditPolicyResult{
		Action:      action,
		RuleMatched: ruleMatched,
		DurationMs:  durationMs,
	}
}

// SetRequestValidationAI sets AI request validation result
func (ac *AuditContext) SetRequestValidationAI(action, reasoning string, durationMs int64) {
	if ac.entry.RequestValidation == nil {
		ac.entry.RequestValidation = &AuditValidationInfo{}
	}
	ac.entry.RequestValidation.AI = &AuditPolicyResult{
		Action:     action,
		Reasoning:  reasoning,
		DurationMs: durationMs,
	}
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

// SetResponseValidationCEL sets CEL response validation result
func (ac *AuditContext) SetResponseValidationCEL(action, ruleMatched string, durationMs int64) {
	if ac.entry.ResponseValidation == nil {
		ac.entry.ResponseValidation = &AuditValidationInfo{}
	}
	ac.entry.ResponseValidation.CEL = &AuditPolicyResult{
		Action:      action,
		RuleMatched: ruleMatched,
		DurationMs:  durationMs,
	}
}

// SetResponseValidationAI sets AI response validation result
func (ac *AuditContext) SetResponseValidationAI(action, reasoning string, durationMs int64) {
	if ac.entry.ResponseValidation == nil {
		ac.entry.ResponseValidation = &AuditValidationInfo{}
	}
	ac.entry.ResponseValidation.AI = &AuditPolicyResult{
		Action:     action,
		Reasoning:  reasoning,
		DurationMs: durationMs,
	}
}

// SetActions sets the recommended and actual actions
func (ac *AuditContext) SetActions(recommended, actual string) {
	ac.entry.RecommendedAction = recommended
	ac.entry.Action = actual
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
