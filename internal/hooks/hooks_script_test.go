package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptResult captures the output of running a hook script.
type scriptResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// interceptRequest captures the JSON body sent to the mock gateway.
type interceptRequest struct {
	Event   string `json:"event"`
	Phase   string `json:"phase"`
	Payload struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
		Result    *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result,omitempty"`
	} `json:"payload"`
	Context struct {
		Principal struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"principal"`
		SessionID string `json:"sessionId,omitempty"`
		Timestamp string `json:"timestamp,omitempty"`
	} `json:"context"`
	Config *struct {
		WorkingDirectory string `json:"working_directory,omitempty"`
	} `json:"config,omitempty"`
}

// mockGateway creates an httptest server that returns the given response body
// and captures the request body for inspection.
func mockGateway(t *testing.T, statusCode int, responseBody string) (*httptest.Server, *[]interceptRequest) {
	t.Helper()
	var captured []interceptRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var req interceptRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("Failed to parse request body: %v", err)
		} else {
			captured = append(captured, req)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = fmt.Fprint(w, responseBody)
	}))
	t.Cleanup(server.Close)

	return server, &captured
}

// runScript writes a hook script to a temp file, runs it with the given stdin
// and environment, and returns the result.
func runScript(t *testing.T, script, stdin string, env map[string]string) scriptResult {
	t.Helper()

	// Check dependencies are available.
	for _, dep := range []string{"bash", "jq", "curl"} {
		if _, err := exec.LookPath(dep); err != nil {
			t.Skipf("Skipping: %s not found on PATH", dep)
		}
	}

	tmpfile, err := os.CreateTemp(t.TempDir(), "hook-*.sh")
	require.NoError(t, err)

	_, err = tmpfile.WriteString(script)
	require.NoError(t, err)
	require.NoError(t, tmpfile.Close())
	require.NoError(t, os.Chmod(tmpfile.Name(), 0o755))

	cmd := exec.Command("bash", tmpfile.Name())
	cmd.Stdin = strings.NewReader(stdin)

	// Build environment: inherit minimal PATH, add test-specific vars.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("Failed to run script: %v", err)
		}
	}

	return scriptResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// --- Gateway response fixtures ---

const (
	allowResponse         = `{"valid": true}`
	denyResponse          = `{"valid": false, "messages": [{"message": "Blocked by policy rule: no-destructive-ops"}]}`
	denyNoMessagesReponse = `{"valid": false}`
)

// --- Per-agent stdin fixtures ---

func claudeCodePreToolInput() string {
	return `{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": {"command": "rm -rf /"}, "session_id": "sess-1", "cwd": "/workspace"}`
}

func claudeCodePostToolInput() string {
	return `{"hook_event_name": "PostToolUse", "tool_name": "Bash", "tool_input": {"command": "ls"}, "tool_result": "file1.txt\nfile2.txt", "session_id": "sess-1", "cwd": "/workspace"}`
}

func copilotPreToolInput() string {
	// VS Code Copilot uses camelCase sessionId (Copilot CLI doesn't provide it at all).
	return `{"hook_event_name": "PreToolUse", "tool_name": "gh_create_issue", "tool_input": {"title": "test"}, "sessionId": "sess-1"}`
}

func copilotPostToolInput() string {
	return `{"hook_event_name": "PostToolUse", "tool_name": "gh_create_issue", "tool_input": {"title": "test"}, "tool_result": "created", "sessionId": "sess-1"}`
}

func clinePreToolInput() string {
	return `{"taskId": "task-1", "workspacePath": "/workspace", "timestamp": "1736654400000", "preToolUse": {"tool": "execute_command", "parameters": {"command": "rm -rf /"}}}`
}

func clinePostToolInput() string {
	return `{"taskId": "task-1", "workspacePath": "/workspace", "timestamp": "1736654400000", "postToolUse": {"tool": "execute_command", "parameters": {"command": "ls"}, "result": "file1.txt"}}`
}

func geminiPreToolInput() string {
	return `{"hook_event_name": "BeforeTool", "tool_name": "read_file", "tool_input": {"path": "/etc/passwd"}, "session_id": "sess-1", "cwd": "/workspace"}`
}

func geminiPostToolInput() string {
	return `{"hook_event_name": "AfterTool", "tool_name": "read_file", "tool_input": {"path": "/etc/passwd"}, "tool_result": {"content": "data"}, "session_id": "sess-1", "cwd": "/workspace"}`
}

func cursorShellInput() string {
	return `{"command": "rm -rf /"}`
}

func cursorMCPInput() string {
	return `{"serverName": "github", "toolName": "delete_repo", "arguments": {"repo": "test"}}`
}

func cursorAfterMCPInput() string {
	return `{"serverName": "github", "toolName": "list_repos", "arguments": {}, "output": "repo1\nrepo2"}`
}

// =============================================================================
// Pre-tool deny tests — verify each agent outputs correct deny JSON
// =============================================================================

// TestPreToolDeny verifies that each agent script produces the correct
// deny JSON output when the gateway returns a deny response.
func TestPreToolDeny(t *testing.T) {
	tests := []struct {
		name      string
		hookName  string
		stdin     string
		assertOut func(t *testing.T, stdout string)
	}{
		{
			name:     "claude-code denies with hookSpecificOutput",
			hookName: "claude-code",
			stdin:    claudeCodePreToolInput(),
			assertOut: func(t *testing.T, stdout string) {
				var out map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &out))
				hso, ok := out["hookSpecificOutput"].(map[string]any)
				require.True(t, ok, "Expected hookSpecificOutput object")
				assert.Equal(t, "deny", hso["permissionDecision"])
				assert.Contains(t, hso["permissionDecisionReason"], "no-destructive-ops")
			},
		},
		{
			name:     "copilot denies with hookSpecificOutput",
			hookName: "copilot",
			stdin:    copilotPreToolInput(),
			assertOut: func(t *testing.T, stdout string) {
				var out map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &out))
				hso, ok := out["hookSpecificOutput"].(map[string]any)
				require.True(t, ok, "Expected hookSpecificOutput object")
				assert.Equal(t, "deny", hso["permissionDecision"])
				assert.Contains(t, hso["permissionDecisionReason"], "no-destructive-ops")
			},
		},
		{
			name:     "cline denies with cancel and errorMessage",
			hookName: "cline",
			stdin:    clinePreToolInput(),
			assertOut: func(t *testing.T, stdout string) {
				var out map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &out))
				assert.Equal(t, true, out["cancel"])
				assert.Contains(t, out["errorMessage"], "no-destructive-ops")
			},
		},
		{
			name:     "gemini-cli denies with decision and reason",
			hookName: "gemini-cli",
			stdin:    geminiPreToolInput(),
			assertOut: func(t *testing.T, stdout string) {
				var out map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &out))
				assert.Equal(t, "deny", out["decision"])
				assert.Contains(t, out["reason"], "no-destructive-ops")
			},
		},
		{
			name:     "cursor shell denies with permission deny",
			hookName: "cursor",
			stdin:    cursorShellInput(),
			assertOut: func(t *testing.T, stdout string) {
				var out map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &out))
				assert.Equal(t, "deny", out["permission"])
			},
		},
		{
			name:     "cursor MCP denies with permission deny",
			hookName: "cursor",
			stdin:    cursorMCPInput(),
			assertOut: func(t *testing.T, stdout string) {
				var out map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &out))
				assert.Equal(t, "deny", out["permission"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := mockGateway(t, http.StatusOK, denyResponse)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode, "Hook should exit 0 even on deny")
			tc.assertOut(t, strings.TrimSpace(result.Stdout))
		})
	}
}

// =============================================================================
// Pre-tool allow tests — verify no deny output on allow
// =============================================================================

// TestPreToolAllow verifies that each agent script produces no deny output
// (or empty JSON for Cline) when the gateway allows the request.
func TestPreToolAllow(t *testing.T) {
	tests := []struct {
		name     string
		hookName string
		stdin    string
		wantOut  string // expected stdout (empty or "{}" for Cline)
	}{
		{"claude-code allow", "claude-code", claudeCodePreToolInput(), ""},
		{"copilot allow", "copilot", copilotPreToolInput(), ""},
		{"cline allow", "cline", clinePreToolInput(), "{}"},
		{"gemini-cli allow", "gemini-cli", geminiPreToolInput(), ""},
		{"cursor shell allow", "cursor", cursorShellInput(), ""},
		{"cursor MCP allow", "cursor", cursorMCPInput(), ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := mockGateway(t, http.StatusOK, allowResponse)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode)
			assert.Equal(t, tc.wantOut, strings.TrimSpace(result.Stdout))
		})
	}
}

// =============================================================================
// Fail-open tests — verify scripts allow when gateway is unreachable
// =============================================================================

// TestFailOpenGatewayUnreachable verifies that scripts fail open (exit 0,
// no deny output) when the gateway is unreachable.
func TestFailOpenGatewayUnreachable(t *testing.T) {
	tests := []struct {
		name     string
		hookName string
		stdin    string
	}{
		{"claude-code unreachable", "claude-code", claudeCodePreToolInput()},
		{"copilot unreachable", "copilot", copilotPreToolInput()},
		{"cline unreachable", "cline", clinePreToolInput()},
		{"gemini-cli unreachable", "gemini-cli", geminiPreToolInput()},
		{"cursor shell unreachable", "cursor", cursorShellInput()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			// Point to a port that is not listening.
			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": "http://127.0.0.1:1",
			})

			assert.Equal(t, 0, result.ExitCode, "Should exit 0 (fail-open)")
			// Stdout should not contain any deny JSON.
			stdout := strings.TrimSpace(result.Stdout)
			assert.True(t, stdout == "" || stdout == "{}", "Should not produce deny output, got: %s", stdout)
			assert.Contains(t, result.Stderr, "maybe-dont", "Should log a warning to stderr")
		})
	}
}

// TestFailOpenHTTPError verifies that scripts fail open when the gateway
// returns a non-2xx HTTP status code.
func TestFailOpenHTTPError(t *testing.T) {
	tests := []struct {
		name     string
		hookName string
		stdin    string
	}{
		{"claude-code HTTP 500", "claude-code", claudeCodePreToolInput()},
		{"copilot HTTP 500", "copilot", copilotPreToolInput()},
		{"cline HTTP 500", "cline", clinePreToolInput()},
		{"gemini-cli HTTP 500", "gemini-cli", geminiPreToolInput()},
		{"cursor shell HTTP 500", "cursor", cursorShellInput()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := mockGateway(t, http.StatusInternalServerError, `{"error": "internal"}`)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode, "Should exit 0 (fail-open)")
			stdout := strings.TrimSpace(result.Stdout)
			assert.True(t, stdout == "" || stdout == "{}", "Should not produce deny output on HTTP 500, got: %s", stdout)
			assert.Contains(t, result.Stderr, "500", "Should log HTTP status in warning")
		})
	}
}

// =============================================================================
// Fail-open: missing MAYBE_DONT_URL
// =============================================================================

// TestFailOpenMissingURL verifies that scripts fail open when MAYBE_DONT_URL
// is not set.
func TestFailOpenMissingURL(t *testing.T) {
	for _, hookName := range HookNames() {
		t.Run(hookName, func(t *testing.T) {
			hook := GetHook(hookName)
			require.NotNil(t, hook)

			// Run without MAYBE_DONT_URL set.
			result := runScript(t, hook.Script, `{}`, map[string]string{})

			assert.Equal(t, 0, result.ExitCode, "Should exit 0 (fail-open)")
			assert.Contains(t, result.Stderr, "MAYBE_DONT_URL")
		})
	}
}

// =============================================================================
// Post-tool observability tests — verify post-tool hooks never produce deny
// =============================================================================

// TestPostToolObservabilityOnly verifies that post-tool hooks never produce
// deny output to stdout, even when the gateway returns a deny response.
func TestPostToolObservabilityOnly(t *testing.T) {
	tests := []struct {
		name     string
		hookName string
		stdin    string
	}{
		{"claude-code PostToolUse", "claude-code", claudeCodePostToolInput()},
		{"copilot PostToolUse", "copilot", copilotPostToolInput()},
		{"cline postToolUse", "cline", clinePostToolInput()},
		{"gemini-cli AfterTool", "gemini-cli", geminiPostToolInput()},
		{"cursor afterShellExecution", "cursor", `{"command": "ls", "output": "file1.txt"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := mockGateway(t, http.StatusOK, denyResponse)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode, "Post-tool hooks must always exit 0")
			stdout := strings.TrimSpace(result.Stdout)
			// Post-tool hooks should not output deny JSON — observability only.
			if stdout != "" {
				var parsed map[string]any
				if json.Unmarshal([]byte(stdout), &parsed) == nil {
					assert.NotContains(t, stdout, "deny",
						"Post-tool hook should not output deny decision")
				}
			}
			// Should log a warning to stderr about the violation.
			assert.Contains(t, result.Stderr, "maybe-dont",
				"Should log policy violation warning to stderr")
		})
	}
}

// =============================================================================
// Deny with no messages — verify fallback reason
// =============================================================================

// TestDenyFallbackReason verifies that when the gateway returns a deny
// with no messages, the scripts produce a non-empty fallback reason.
func TestDenyFallbackReason(t *testing.T) {
	tests := []struct {
		name     string
		hookName string
		stdin    string
		// reasonField is the JSON path to the reason in the deny output.
		reasonField string
	}{
		{"claude-code fallback", "claude-code", claudeCodePreToolInput(), "hookSpecificOutput.permissionDecisionReason"},
		{"copilot fallback", "copilot", copilotPreToolInput(), "hookSpecificOutput.permissionDecisionReason"},
		{"cline fallback", "cline", clinePreToolInput(), "errorMessage"},
		{"gemini-cli fallback", "gemini-cli", geminiPreToolInput(), "reason"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := mockGateway(t, http.StatusOK, denyNoMessagesReponse)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode)
			stdout := strings.TrimSpace(result.Stdout)
			require.NotEmpty(t, stdout, "Should produce deny output")

			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &parsed))

			// Navigate to the reason field.
			reason := extractNestedField(parsed, tc.reasonField)
			reasonStr, ok := reason.(string)
			require.True(t, ok, "Reason field should be a string, got %T", reason)
			assert.Equal(t, "Blocked by policy", reasonStr,
				"Fallback reason should be 'Blocked by policy'")
		})
	}
}

// =============================================================================
// Intercept request format — verify the request body sent to the gateway
// =============================================================================

// TestInterceptRequestFormat verifies that the intercept request body sent
// to the gateway has the correct structure.
func TestInterceptRequestFormat(t *testing.T) {
	tests := []struct {
		name        string
		hookName    string
		stdin       string
		wantEvent   string
		wantPhase   string
		wantTool    string
		wantAgentID string
	}{
		{
			name: "claude-code pre-tool request", hookName: "claude-code",
			stdin:     claudeCodePreToolInput(),
			wantEvent: "tools/call", wantPhase: "request",
			wantTool: "Bash", wantAgentID: "claude-code",
		},
		{
			name: "copilot pre-tool request", hookName: "copilot",
			stdin:     copilotPreToolInput(),
			wantEvent: "tools/call", wantPhase: "request",
			wantTool: "gh_create_issue", wantAgentID: "copilot",
		},
		{
			name: "cline pre-tool request", hookName: "cline",
			stdin:     clinePreToolInput(),
			wantEvent: "tools/call", wantPhase: "request",
			wantTool: "execute_command", wantAgentID: "cline",
		},
		{
			name: "gemini-cli pre-tool request", hookName: "gemini-cli",
			stdin:     geminiPreToolInput(),
			wantEvent: "tools/call", wantPhase: "request",
			wantTool: "read_file", wantAgentID: "gemini-cli",
		},
		{
			name: "cursor shell pre-tool request", hookName: "cursor",
			stdin:     cursorShellInput(),
			wantEvent: "tools/call", wantPhase: "request",
			wantTool: "Bash", wantAgentID: "cursor",
		},
		{
			name: "cursor MCP pre-tool request", hookName: "cursor",
			stdin:     cursorMCPInput(),
			wantEvent: "tools/call", wantPhase: "request",
			wantTool: "github__delete_repo", wantAgentID: "cursor",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, captured := mockGateway(t, http.StatusOK, allowResponse)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode)
			require.Len(t, *captured, 1, "Should send exactly one request to gateway")

			req := (*captured)[0]
			assert.Equal(t, tc.wantEvent, req.Event)
			assert.Equal(t, tc.wantPhase, req.Phase)
			assert.Equal(t, tc.wantTool, req.Payload.Name)
			assert.Equal(t, "service", req.Context.Principal.Type)
			assert.Equal(t, tc.wantAgentID, req.Context.Principal.ID)
		})
	}
}

// =============================================================================
// Cline-specific: timestamp validation
// =============================================================================

// TestClineTimestampValidation verifies that the cline script handles
// non-numeric timestamps safely (no crash, no injection).
func TestClineTimestampValidation(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
	}{
		{"valid epoch ms", "1736654400000"},
		{"float timestamp", "1736654400000.5"},
		{"non-numeric string", "not-a-number"},
		{"empty timestamp", ""},
		{"injection attempt", "x[$(echo INJECTED >&2)]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := mockGateway(t, http.StatusOK, allowResponse)
			hook := GetHook("cline")
			require.NotNil(t, hook)

			input := fmt.Sprintf(`{"taskId": "task-1", "workspacePath": "/workspace", "timestamp": "%s", "preToolUse": {"tool": "read_file", "parameters": {"path": "/tmp/test"}}}`, tc.timestamp)

			result := runScript(t, hook.Script, input, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode,
				"Script should not crash on timestamp %q", tc.timestamp)
			assert.NotContains(t, result.Stderr, "INJECTED",
				"Injection attempt should not execute")
		})
	}
}

// =============================================================================
// Cursor afterMCPExecution mutation — verify redaction output
// =============================================================================

// TestCursorAfterMCPMutation verifies that the cursor script returns
// updated_mcp_tool_output when the gateway returns a mutation response.
func TestCursorAfterMCPMutation(t *testing.T) {
	mutationResponse := `{"valid": true, "type": "mutation", "modified": true, "payload": {"result": {"content": [{"type": "text", "text": "[REDACTED]"}]}}}`

	server, _ := mockGateway(t, http.StatusOK, mutationResponse)
	hook := GetHook("cursor")
	require.NotNil(t, hook)

	result := runScript(t, hook.Script, cursorAfterMCPInput(), map[string]string{
		"MAYBE_DONT_URL": server.URL,
	})

	assert.Equal(t, 0, result.ExitCode)
	stdout := strings.TrimSpace(result.Stdout)
	require.NotEmpty(t, stdout, "Should produce mutation output")

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &out))
	assert.Equal(t, "[REDACTED]", out["updated_mcp_tool_output"],
		"Should return redacted text")
}

// =============================================================================
// Context passthrough — verify sessionId, timestamp, working_directory, arguments
// =============================================================================

// TestContextPassthrough verifies that each agent script correctly forwards
// session ID, timestamp, working directory, and tool arguments into the
// intercept request's context and config fields.
func TestContextPassthrough(t *testing.T) {
	tests := []struct {
		name             string
		hookName         string
		stdin            string
		wantSessionID    string
		wantWorkDir      string
		wantArgKey       string // one argument key to verify passthrough
		wantArgValue     any    // expected value for that argument key
		sessionIDPresent bool   // whether sessionId should be present (vs null)
		hasTimestamp     bool   // whether the script generates a timestamp
	}{
		{
			name:     "claude-code forwards session_id, cwd, arguments",
			hookName: "claude-code", stdin: claudeCodePreToolInput(),
			wantSessionID: "sess-1", wantWorkDir: "/workspace",
			wantArgKey: "command", wantArgValue: "rm -rf /",
			sessionIDPresent: true, hasTimestamp: true,
		},
		{
			name:     "copilot forwards sessionId, arguments",
			hookName: "copilot", stdin: copilotPreToolInput(),
			wantSessionID: "sess-1", wantWorkDir: "",
			wantArgKey: "title", wantArgValue: "test",
			sessionIDPresent: true, hasTimestamp: true,
		},
		{
			name:     "gemini-cli forwards session_id, cwd, arguments",
			hookName: "gemini-cli", stdin: geminiPreToolInput(),
			wantSessionID: "sess-1", wantWorkDir: "/workspace",
			wantArgKey: "path", wantArgValue: "/etc/passwd",
			sessionIDPresent: true, hasTimestamp: true,
		},
		{
			name:     "cline forwards taskId as sessionId, workspacePath, arguments",
			hookName: "cline", stdin: clinePreToolInput(),
			wantSessionID: "task-1", wantWorkDir: "/workspace",
			wantArgKey: "command", wantArgValue: "rm -rf /",
			sessionIDPresent: true, hasTimestamp: true,
		},
		{
			name:     "cursor shell forwards command as argument",
			hookName: "cursor", stdin: cursorShellInput(),
			wantSessionID: "", wantWorkDir: "",
			wantArgKey: "command", wantArgValue: "rm -rf /",
			sessionIDPresent: false, hasTimestamp: true,
		},
		{
			name:     "cursor MCP forwards arguments",
			hookName: "cursor", stdin: cursorMCPInput(),
			wantSessionID: "", wantWorkDir: "",
			wantArgKey: "repo", wantArgValue: "test",
			sessionIDPresent: false, hasTimestamp: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, captured := mockGateway(t, http.StatusOK, allowResponse)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode)
			require.Len(t, *captured, 1)
			req := (*captured)[0]

			// Verify timestamp when the script generates one.
			if tc.hasTimestamp {
				assert.NotEmpty(t, req.Context.Timestamp, "Timestamp should be set")
				assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`, req.Context.Timestamp,
					"Timestamp should be ISO 8601 format")
			}

			// Verify session ID passthrough.
			if tc.sessionIDPresent {
				assert.Equal(t, tc.wantSessionID, req.Context.SessionID,
					"SessionID should be forwarded from agent input")
			}

			// Verify working directory passthrough.
			if tc.wantWorkDir != "" {
				require.NotNil(t, req.Config, "Config should be present when working directory is set")
				assert.Equal(t, tc.wantWorkDir, req.Config.WorkingDirectory,
					"Working directory should be forwarded from agent input")
			}

			// Verify at least one argument is correctly passed through.
			argVal, ok := req.Payload.Arguments[tc.wantArgKey]
			require.True(t, ok, "Argument %q should be present in payload", tc.wantArgKey)
			assert.Equal(t, tc.wantArgValue, argVal,
				"Argument %q should have correct value", tc.wantArgKey)
		})
	}
}

// =============================================================================
// Post-tool intercept request format — verify phase="response" + result payload
// =============================================================================

// TestPostToolInterceptRequestFormat verifies that post-tool hooks send
// phase="response" and include a result payload to the gateway.
func TestPostToolInterceptRequestFormat(t *testing.T) {
	tests := []struct {
		name        string
		hookName    string
		stdin       string
		wantTool    string
		wantAgentID string
		wantResult  string // expected substring in result.content[0].text
	}{
		{
			name: "claude-code PostToolUse request", hookName: "claude-code",
			stdin:    claudeCodePostToolInput(),
			wantTool: "Bash", wantAgentID: "claude-code",
			wantResult: "file1.txt",
		},
		{
			name: "copilot PostToolUse request", hookName: "copilot",
			stdin:    copilotPostToolInput(),
			wantTool: "gh_create_issue", wantAgentID: "copilot",
			wantResult: "created",
		},
		{
			name: "cline postToolUse request", hookName: "cline",
			stdin:    clinePostToolInput(),
			wantTool: "execute_command", wantAgentID: "cline",
			wantResult: "file1.txt",
		},
		{
			name: "gemini-cli AfterTool request", hookName: "gemini-cli",
			stdin:    geminiPostToolInput(),
			wantTool: "read_file", wantAgentID: "gemini-cli",
			wantResult: "content", // JSON object serialized to string
		},
		{
			name: "cursor afterShellExecution request", hookName: "cursor",
			stdin:    `{"command": "ls", "output": "file1.txt"}`,
			wantTool: "Bash", wantAgentID: "cursor",
			wantResult: "file1.txt",
		},
		{
			name: "cursor afterMCPExecution request", hookName: "cursor",
			stdin:    cursorAfterMCPInput(),
			wantTool: "github__list_repos", wantAgentID: "cursor",
			wantResult: "repo1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, captured := mockGateway(t, http.StatusOK, allowResponse)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode)
			require.Len(t, *captured, 1, "Should send exactly one request to gateway")

			req := (*captured)[0]
			assert.Equal(t, "tools/call", req.Event)
			assert.Equal(t, "response", req.Phase, "Post-tool must send phase=response")
			assert.Equal(t, tc.wantTool, req.Payload.Name)
			assert.Equal(t, "service", req.Context.Principal.Type)
			assert.Equal(t, tc.wantAgentID, req.Context.Principal.ID)

			// Verify result payload is present with content.
			require.NotNil(t, req.Payload.Result, "Post-tool request must include result")
			require.NotEmpty(t, req.Payload.Result.Content, "Result must have content entries")
			assert.Equal(t, "text", req.Payload.Result.Content[0].Type)
			assert.Contains(t, req.Payload.Result.Content[0].Text, tc.wantResult,
				"Result text should contain tool output")
		})
	}
}

// =============================================================================
// Cursor afterMCPExecution deny (non-mutation) — observability only
// =============================================================================

// TestCursorAfterMCPDenyObservability verifies that cursor afterMCPExecution
// logs a deny to stderr but produces no deny output to stdout when the gateway
// returns a plain deny (not a mutation response).
func TestCursorAfterMCPDenyObservability(t *testing.T) {
	server, _ := mockGateway(t, http.StatusOK, denyResponse)
	hook := GetHook("cursor")
	require.NotNil(t, hook)

	result := runScript(t, hook.Script, cursorAfterMCPInput(), map[string]string{
		"MAYBE_DONT_URL": server.URL,
	})

	assert.Equal(t, 0, result.ExitCode, "afterMCPExecution must always exit 0")
	stdout := strings.TrimSpace(result.Stdout)
	assert.Empty(t, stdout, "Should not produce any stdout on deny (observability only)")
	assert.Contains(t, result.Stderr, "maybe-dont",
		"Should log policy violation warning to stderr")
	assert.Contains(t, result.Stderr, "no-destructive-ops",
		"Should include the deny reason in stderr warning")
}

// =============================================================================
// Cursor deny with no messages — verify no extraneous reason field
// =============================================================================

// TestCursorDenyNoMessages verifies that cursor produces a clean deny output
// with no reason field when the gateway returns a deny with no messages.
// Cursor's deny format is just {"permission": "deny"} — no reason field.
func TestCursorDenyNoMessages(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
	}{
		{"cursor shell deny no messages", cursorShellInput()},
		{"cursor MCP deny no messages", cursorMCPInput()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := mockGateway(t, http.StatusOK, denyNoMessagesReponse)
			hook := GetHook("cursor")
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode)
			stdout := strings.TrimSpace(result.Stdout)
			require.NotEmpty(t, stdout, "Should produce deny output")

			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &parsed))
			assert.Equal(t, "deny", parsed["permission"],
				"Should output permission deny")
			// Cursor's deny format has no reason field — only permission.
			assert.Len(t, parsed, 1,
				"Cursor deny output should only contain 'permission' field")
		})
	}
}

// =============================================================================
// Multiple deny messages — verify "; " joining
// =============================================================================

// TestMultipleMessagesJoined verifies that when the gateway returns multiple
// deny messages, they are joined with "; " in the deny output.
func TestMultipleMessagesJoined(t *testing.T) {
	multiMessageDeny := `{"valid": false, "messages": [{"message": "Rule A violated"}, {"message": "Rule B violated"}]}`

	tests := []struct {
		name        string
		hookName    string
		stdin       string
		reasonField string
	}{
		{"claude-code multi-message", "claude-code", claudeCodePreToolInput(), "hookSpecificOutput.permissionDecisionReason"},
		{"copilot multi-message", "copilot", copilotPreToolInput(), "hookSpecificOutput.permissionDecisionReason"},
		{"cline multi-message", "cline", clinePreToolInput(), "errorMessage"},
		{"gemini-cli multi-message", "gemini-cli", geminiPreToolInput(), "reason"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := mockGateway(t, http.StatusOK, multiMessageDeny)
			hook := GetHook(tc.hookName)
			require.NotNil(t, hook)

			result := runScript(t, hook.Script, tc.stdin, map[string]string{
				"MAYBE_DONT_URL": server.URL,
			})

			assert.Equal(t, 0, result.ExitCode)
			stdout := strings.TrimSpace(result.Stdout)
			require.NotEmpty(t, stdout)

			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &parsed))

			reason := extractNestedField(parsed, tc.reasonField)
			reasonStr, ok := reason.(string)
			require.True(t, ok, "Reason field should be a string")
			assert.Equal(t, "Rule A violated; Rule B violated", reasonStr,
				"Multiple messages should be joined with '; '")
		})
	}
}

// =============================================================================
// Cross-hook equivalence — same input produces same gateway request
// =============================================================================

// TestCrossHookRequestEquivalence verifies that agents receiving equivalent
// tool call input produce structurally identical intercept request payloads
// (differing only in principal.id and context fields the agent may/may not have).
func TestCrossHookRequestEquivalence(t *testing.T) {
	// Claude Code and Gemini CLI both receive tool_name + tool_input in the same
	// format (with hook_event_name, session_id, cwd). Their gateway requests for
	// the same tool call should have identical event, phase, payload.name, and
	// payload.arguments — only principal.id differs.
	claudeInput := `{"hook_event_name": "PreToolUse", "tool_name": "read_file", "tool_input": {"path": "/tmp/test"}, "session_id": "shared-sess", "cwd": "/project"}`
	geminiInput := `{"hook_event_name": "BeforeTool", "tool_name": "read_file", "tool_input": {"path": "/tmp/test"}, "session_id": "shared-sess", "cwd": "/project"}`

	// Run claude-code
	ccServer, ccCaptured := mockGateway(t, http.StatusOK, allowResponse)
	ccHook := GetHook("claude-code")
	require.NotNil(t, ccHook)
	ccResult := runScript(t, ccHook.Script, claudeInput, map[string]string{
		"MAYBE_DONT_URL": ccServer.URL,
	})
	assert.Equal(t, 0, ccResult.ExitCode)
	require.Len(t, *ccCaptured, 1)
	ccReq := (*ccCaptured)[0]

	// Run gemini-cli
	gemServer, gemCaptured := mockGateway(t, http.StatusOK, allowResponse)
	gemHook := GetHook("gemini-cli")
	require.NotNil(t, gemHook)
	gemResult := runScript(t, gemHook.Script, geminiInput, map[string]string{
		"MAYBE_DONT_URL": gemServer.URL,
	})
	assert.Equal(t, 0, gemResult.ExitCode)
	require.Len(t, *gemCaptured, 1)
	gemReq := (*gemCaptured)[0]

	// Verify structural equivalence (everything except principal.id).
	assert.Equal(t, ccReq.Event, gemReq.Event, "Event should match across hooks")
	assert.Equal(t, ccReq.Phase, gemReq.Phase, "Phase should match across hooks")
	assert.Equal(t, ccReq.Payload.Name, gemReq.Payload.Name,
		"Payload name should match across hooks")
	assert.Equal(t, ccReq.Payload.Arguments, gemReq.Payload.Arguments,
		"Payload arguments should match across hooks")
	assert.Equal(t, ccReq.Context.SessionID, gemReq.Context.SessionID,
		"SessionID should match across hooks given same input")
	assert.Equal(t, ccReq.Context.Timestamp, gemReq.Context.Timestamp,
		"Timestamp should both be ISO 8601 (may differ by seconds)")

	// principal.id must differ
	assert.Equal(t, "claude-code", ccReq.Context.Principal.ID)
	assert.Equal(t, "gemini-cli", gemReq.Context.Principal.ID)

	// Working directory should match
	require.NotNil(t, ccReq.Config)
	require.NotNil(t, gemReq.Config)
	assert.Equal(t, ccReq.Config.WorkingDirectory, gemReq.Config.WorkingDirectory,
		"Working directory should match across hooks given same input")
}

// =============================================================================
// Helpers
// =============================================================================

// extractNestedField navigates a dotted path like "a.b.c" through nested maps.
func extractNestedField(m map[string]any, path string) any {
	parts := strings.Split(path, ".")
	var current any = m
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[part]
	}
	return current
}
