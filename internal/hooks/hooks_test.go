package hooks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Verify all hooks are embedded, non-empty, and well-structured.
func TestHooks(t *testing.T) {
	allHooks := Hooks()
	require.Len(t, allHooks, 5, "Should have 5 hooks")

	expectedNames := []string{"claude-code", "cursor", "gemini-cli", "cline", "copilot"}
	for i, hook := range allHooks {
		t.Run(hook.Name, func(t *testing.T) {
			assert.Equal(t, expectedNames[i], hook.Name)
			assert.NotEmpty(t, hook.Description)
			assert.NotEmpty(t, hook.Script)
			assert.NotEmpty(t, hook.Config)
		})
	}
}

// Verify GetHook returns correct hook by name, nil for unknown.
func TestGetHook(t *testing.T) {
	tests := []struct {
		name     string
		hookName string
		wantNil  bool
	}{
		{"claude-code exists", "claude-code", false},
		{"cursor exists", "cursor", false},
		{"gemini-cli exists", "gemini-cli", false},
		{"cline exists", "cline", false},
		{"copilot exists", "copilot", false},
		{"unknown hook", "unknown", true},
		{"empty name", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hook := GetHook(tc.hookName)
			if tc.wantNil {
				assert.Nil(t, hook)
			} else {
				require.NotNil(t, hook)
				assert.Equal(t, tc.hookName, hook.Name)
			}
		})
	}
}

// Verify HookNames returns all hook names in order.
func TestHookNames(t *testing.T) {
	names := HookNames()
	expected := []string{"claude-code", "cursor", "gemini-cli", "cline", "copilot"}
	assert.Equal(t, expected, names)
}

// Verify all scripts start with a valid bash shebang.
func TestScriptsHaveShebang(t *testing.T) {
	for _, hook := range Hooks() {
		t.Run(hook.Name, func(t *testing.T) {
			assert.True(t, strings.HasPrefix(hook.Script, "#!/usr/bin/env bash"),
				"Script for %s should start with #!/usr/bin/env bash", hook.Name)
		})
	}
}

// Verify all config snippets are valid JSON.
func TestConfigsAreValidJSON(t *testing.T) {
	for _, hook := range Hooks() {
		t.Run(hook.Name, func(t *testing.T) {
			var parsed map[string]any
			err := json.Unmarshal([]byte(hook.Config), &parsed)
			require.NoError(t, err, "Config for %s should be valid JSON", hook.Name)
			assert.NotEmpty(t, parsed, "Config for %s should not be empty", hook.Name)
		})
	}
}

// Verify all scripts contain the core shared functions.
func TestScriptsHaveCoreFunctions(t *testing.T) {
	coreFunctions := []string{"md_check_deps", "md_call_gateway", "md_is_denied", "md_get_reason"}
	for _, hook := range Hooks() {
		t.Run(hook.Name, func(t *testing.T) {
			for _, fn := range coreFunctions {
				assert.Contains(t, hook.Script, fn,
					"Script for %s should contain %s", hook.Name, fn)
			}
		})
	}
}

// Verify scripts call the correct intercept endpoint.
func TestScriptsCallInterceptEndpoint(t *testing.T) {
	for _, hook := range Hooks() {
		t.Run(hook.Name, func(t *testing.T) {
			assert.Contains(t, hook.Script, "/api/v1/intercept",
				"Script for %s should call the intercept endpoint", hook.Name)
		})
	}
}

// Verify scripts reference the MAYBE_DONT_URL environment variable.
func TestScriptsUseGatewayURL(t *testing.T) {
	for _, hook := range Hooks() {
		t.Run(hook.Name, func(t *testing.T) {
			assert.Contains(t, hook.Script, "MAYBE_DONT_URL",
				"Script for %s should reference MAYBE_DONT_URL", hook.Name)
		})
	}
}

// Verify each agent script identifies itself correctly in the principal.id field.
func TestScriptsIdentifyAgent(t *testing.T) {
	agentIDs := map[string]string{
		"claude-code": "claude-code",
		"cursor":      "cursor",
		"gemini-cli":  "gemini-cli",
		"cline":       "cline",
		"copilot":     "copilot",
	}

	for _, hook := range Hooks() {
		t.Run(hook.Name, func(t *testing.T) {
			expectedID := agentIDs[hook.Name]
			assert.Contains(t, hook.Script, expectedID,
				"Script for %s should identify as %s", hook.Name, expectedID)
		})
	}
}
