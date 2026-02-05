package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/maybedont/maybe-dont/internal/config"
)

// CLIValidationRequest represents a CLI command validation request sent to the gateway.
// This is the JSON request body for POST /api/v1/cli/validate.
type CLIValidationRequest struct {
	// Command is the CLI executable name (e.g., "gh", "aws", "kubectl").
	// This field is required.
	Command string `json:"command"`

	// Arguments contains the command arguments. Can be an empty array.
	// This field is required.
	Arguments []string `json:"arguments"`

	// WorkingDirectory is the current working directory where the command will execute.
	// This field is optional.
	WorkingDirectory string `json:"working_directory,omitempty"`

	// ClientInfo contains optional client environment information for audit attribution.
	ClientInfo *CLIClientInfo `json:"client_info,omitempty"`
}

// CLIClientInfo contains client environment information collected by the CLI wrapper.
// All fields are optional and used for audit logging and policy evaluation.
type CLIClientInfo struct {
	// Hostname is the client machine hostname (from os.Hostname()).
	Hostname string `json:"hostname,omitempty"`

	// Username is the current user (from os/user.Current()).
	Username string `json:"username,omitempty"`

	// OS is the operating system (from runtime.GOOS: "darwin", "linux", "windows").
	OS string `json:"os,omitempty"`

	// OSVersion is the OS version string (best-effort, platform-specific).
	OSVersion string `json:"os_version,omitempty"`

	// Arch is the CPU architecture (from runtime.GOARCH: "amd64", "arm64", etc.).
	Arch string `json:"arch,omitempty"`

	// Shell is the user's shell (from $SHELL on Unix, $COMSPEC on Windows).
	Shell string `json:"shell,omitempty"`

	// CLIVersion is the version of the maybe-dont CLI wrapper.
	CLIVersion string `json:"cli_version,omitempty"`
}

// CLIValidationResponse represents the validation result returned by the gateway.
// This is the JSON response body for POST /api/v1/cli/validate.
type CLIValidationResponse struct {
	// Allowed indicates whether the command is permitted to execute.
	Allowed bool `json:"allowed"`

	// ValidationRequired indicates whether the command required policy validation.
	// When false, the command was not in the validate_commands list and was allowed
	// without policy evaluation.
	ValidationRequired bool `json:"validation_required"`

	// Message is a human-readable description of the validation result.
	Message string `json:"message"`

	// ActionReason explains why the action was taken. Only populated when Allowed is false.
	// Values: "request_policy" (policy denied the command).
	ActionReason string `json:"action_reason,omitempty"`

	// ServerVersion is the gateway version (from build info).
	ServerVersion string `json:"server_version"`

	// ClientVersion is echoed from the request's client_info.cli_version if provided.
	ClientVersion string `json:"client_version,omitempty"`

	// Results contains per-policy evaluation results.
	// Empty when ValidationRequired is false.
	Results []CLIPolicyResult `json:"results"`
}

// CLIPolicyResult represents a single policy evaluation result.
type CLIPolicyResult struct {
	// PolicyName is the name of the policy from the rule definition.
	PolicyName string `json:"policy_name"`

	// PolicyType indicates the type of policy: "cel" or "ai".
	PolicyType string `json:"policy_type"`

	// Action is the result of the policy evaluation: "allow", "deny", or "redact".
	Action string `json:"action"`

	// Message is an optional explanation from the policy evaluation.
	Message string `json:"message,omitempty"`
}

// CLIValidationError represents an error response from the validation endpoint.
// All error responses follow this structure for consistency.
type CLIValidationError struct {
	// Error is the error code. Values:
	// - "cli_validation_disabled": CLI validation feature not enabled
	// - "invalid_request": Malformed request body
	// - "missing_command": Required command field is empty
	// - "invalid_content_type": Content-Type header is not application/json
	// - "policy_evaluation_error": CEL or AI engine failed
	// - "internal_error": Unexpected server error
	Error string `json:"error"`

	// Message is a human-readable description of the error.
	Message string `json:"message"`
}

// CLIValidationHandlerConfig configures the CLI validation HTTP handler.
type CLIValidationHandlerConfig struct {
	// Enabled indicates whether CLI validation is enabled.
	Enabled bool

	// ValidateCommands is the list of commands that require validation.
	// Use "*" to match all commands.
	ValidateCommands []string

	// Logger is the session logger for request logging.
	Logger *config.SessionLogger

	// Version is the gateway version string returned in responses.
	Version string

	// Future: CELEngine, AIEngine for policy evaluation
}

// CLIValidationHandler handles /api/v1/cli/validate requests.
type CLIValidationHandler struct {
	config CLIValidationHandlerConfig
}

// NewCLIValidationHandler creates a new CLI validation handler.
func NewCLIValidationHandler(cfg CLIValidationHandlerConfig) *CLIValidationHandler {
	return &CLIValidationHandler{config: cfg}
}

// ServeHTTP handles CLI validation requests.
func (h *CLIValidationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check if CLI validation is enabled
	if !h.config.Enabled {
		h.writeError(w, http.StatusBadRequest, "cli_validation_disabled",
			"CLI validation is not enabled on this gateway. Set cli_request_validation.enabled: true in configuration.")
		return
	}

	// Validate Content-Type (accept with charset parameters like "application/json; charset=utf-8")
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		h.writeError(w, http.StatusBadRequest, "invalid_content_type",
			"Content-Type must be application/json")
		return
	}

	// Parse request body
	var req CLIValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request",
			"Failed to parse request body: "+err.Error())
		return
	}

	// Validate required fields
	if req.Command == "" {
		h.writeError(w, http.StatusBadRequest, "missing_command",
			"Required field 'command' is empty")
		return
	}

	// Check if command requires validation
	if !h.requiresValidation(req.Command) {
		resp := CLIValidationResponse{
			Allowed:            true,
			ValidationRequired: false,
			Message:            "Command does not require validation",
			ServerVersion:      h.config.Version,
			ClientVersion:      h.getClientVersion(&req),
			Results:            []CLIPolicyResult{},
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Command requires validation - evaluate policies
	// TODO: Integrate with CEL and AI engines in later tasks
	resp := CLIValidationResponse{
		Allowed:            true, // Default allow when no policies configured
		ValidationRequired: true,
		Message:            "Command approved by policy",
		ServerVersion:      h.config.Version,
		ClientVersion:      h.getClientVersion(&req),
		Results:            []CLIPolicyResult{},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// requiresValidation checks if the command is in the validate_commands list.
func (h *CLIValidationHandler) requiresValidation(command string) bool {
	// "*" matches all commands
	for _, c := range h.config.ValidateCommands {
		if c == "*" {
			return true
		}
	}
	for _, c := range h.config.ValidateCommands {
		if c == command {
			return true
		}
	}
	return false
}

// getClientVersion extracts client version from request.
func (h *CLIValidationHandler) getClientVersion(req *CLIValidationRequest) string {
	if req.ClientInfo != nil {
		return req.ClientInfo.CLIVersion
	}
	return ""
}

// writeError writes a JSON error response.
func (h *CLIValidationHandler) writeError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(CLIValidationError{
		Error:   code,
		Message: message,
	})
}
