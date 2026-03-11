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
	return `{"hook_event_name": "PreToolUse", "tool_name": "gh_create_issue", "tool_input": {"title": "test"}, "session_id": "sess-1"}`
}

func copilotPostToolInput() string {
	return `{"hook_event_name": "PostToolUse", "tool_name": "gh_create_issue", "tool_input": {"title": "test"}, "tool_result": "created", "session_id": "sess-1"}`
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
