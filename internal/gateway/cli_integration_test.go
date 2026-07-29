package gateway

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/maybedont/maybe-dont/internal/cliproxy"
	"github.com/maybedont/maybe-dont/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

// cliIntegrationStack holds all wired components for a single integration test.
type cliIntegrationStack struct {
	server      *httptest.Server
	client      *cliproxy.Client
	auditWriter *mockAuditWriter
}

// setupCLIIntegrationTest creates a fully wired test stack:
// CELPolicyEngine → CLIValidationHandler → httptest.Server → cliproxy.Client.
// The caller must call cleanup() when done.
func setupCLIIntegrationTest(t *testing.T, policies []config.Policy, topLevelMode config.PolicyMode, validateCommands []string) *cliIntegrationStack {
	t.Helper()

	logger := zaptest.NewLogger(t)
	sessionLogger := config.NewSessionLogger(logger)

	// Create and load CEL engine with real policies
	celEngine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)

	err = celEngine.LoadPolicies(policies, topLevelMode)
	require.NoError(t, err)

	auditWriter := &mockAuditWriter{}

	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    sessionLogger,
	}

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:               true,
		ValidateCommands:      validateCommands,
		Logger:                sessionLogger,
		Version:               "test-1.0.0",
		AuditWriter:           auditWriter,
		Evaluator:             evaluator,
		IncludeArgumentValues: true,
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := cliproxy.NewClient(cliproxy.ClientConfig{
		ServerURL: server.URL,
		ClientID:  "integration-test-client",
	})

	return &cliIntegrationStack{
		server:      server,
		client:      client,
		auditWriter: auditWriter,
	}
}

// TestCLIValidation_Integration exercises the full round-trip:
// cliproxy.Client → real HTTP → CLIValidationHandler → real CELPolicyEngine → response.
// Each subtest gets a fresh stack to avoid cross-contamination.
func TestCLIValidation_Integration(t *testing.T) {
	tests := []struct {
		name             string
		policies         []config.Policy
		topLevelMode     config.PolicyMode
		validateCommands []string
		request          cliproxy.ValidationRequest
		// Expected response fields
		wantAllowed            bool
		wantValidationRequired bool
		wantResultCount        int
		wantResultAction       string // action of first result, if any
		wantResultPolicyName   string // policy name of first result, if any
		wantMessageContains    string
	}{
		{
			name: "no matching policy allows command",
			policies: []config.Policy{
				{
					Name:          "deny-rm",
					CLIExpression: `cli.command == "rm"`,
					Action:        config.PolicyActionDeny,
					Message:       "rm is blocked",
				},
			},
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "gh",
				Arguments: []string{"pr", "list"},
			},
			wantAllowed:            true,
			wantValidationRequired: true,
			wantResultCount:        0,
		},
		{
			name: "deny policy match blocks command",
			policies: []config.Policy{
				{
					Name:          "deny-rm",
					CLIExpression: `cli.command == "rm"`,
					Action:        config.PolicyActionDeny,
					Message:       "rm is blocked",
				},
			},
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "rm",
				Arguments: []string{"-rf", "/important"},
			},
			wantAllowed:            false,
			wantValidationRequired: true,
			wantResultCount:        1,
			wantResultAction:       "deny",
			wantResultPolicyName:   "deny-rm",
			wantMessageContains:    "rm is blocked",
		},
		{
			name: "explicit allow rule matches",
			policies: []config.Policy{
				{
					Name:          "allow-kubectl-get",
					CLIExpression: `cli.command == "kubectl" && cli.arguments.size() > 0 && cli.arguments[0] == "get"`,
					Action:        config.PolicyActionAllow,
					Message:       "kubectl get is safe",
				},
			},
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "kubectl",
				Arguments: []string{"get", "pods"},
			},
			wantAllowed:            true,
			wantValidationRequired: true,
			wantResultCount:        1,
			wantResultAction:       "allow",
			wantResultPolicyName:   "allow-kubectl-get",
		},
		{
			name: "command not in validate_commands skips validation",
			policies: []config.Policy{
				{
					Name:          "deny-everything",
					CLIExpression: `true`,
					Action:        config.PolicyActionDeny,
					Message:       "should never fire",
				},
			},
			validateCommands: []string{"gh", "aws"},
			request: cliproxy.ValidationRequest{
				Command:   "cat",
				Arguments: []string{"README.md"},
			},
			wantAllowed:            true,
			wantValidationRequired: false,
			wantResultCount:        0,
		},
		{
			name: "multiple policies partial match - allow rule matches",
			policies: []config.Policy{
				{
					Name:          "allow-gh-repo",
					CLIExpression: `cli.command == "gh" && cli.arguments.size() > 0 && cli.arguments[0] == "repo"`,
					Action:        config.PolicyActionAllow,
					Message:       "repo operations allowed",
				},
				{
					Name:          "deny-gh-delete",
					CLIExpression: `cli.command == "gh" && cli.arguments.size() >= 2 && cli.arguments[1] == "delete"`,
					Action:        config.PolicyActionDeny,
					Message:       "delete operations blocked",
				},
			},
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "gh",
				Arguments: []string{"repo", "list"},
			},
			wantAllowed:            true,
			wantValidationRequired: true,
			wantResultCount:        1,
			wantResultAction:       "allow",
			wantResultPolicyName:   "allow-gh-repo",
		},
		{
			name: "deny takes precedence when both match",
			policies: []config.Policy{
				{
					Name:          "allow-gh-repo",
					CLIExpression: `cli.command == "gh" && cli.arguments.size() > 0 && cli.arguments[0] == "repo"`,
					Action:        config.PolicyActionAllow,
					Message:       "repo operations allowed",
				},
				{
					Name:          "deny-gh-delete",
					CLIExpression: `cli.command == "gh" && cli.arguments.size() >= 2 && cli.arguments[1] == "delete"`,
					Action:        config.PolicyActionDeny,
					Message:       "delete operations blocked",
				},
			},
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "gh",
				Arguments: []string{"repo", "delete", "myrepo"},
			},
			wantAllowed:            false,
			wantValidationRequired: true,
			wantResultCount:        2, // Both allow-gh-repo and deny-gh-delete match
			wantMessageContains:    "delete operations blocked",
		},
		{
			name: "top-level audit_only mode allows despite deny match",
			policies: []config.Policy{
				{
					Name:          "deny-rm",
					CLIExpression: `cli.command == "rm"`,
					Action:        config.PolicyActionDeny,
					Message:       "rm would be blocked",
				},
			},
			topLevelMode:     config.PolicyModeAuditOnly,
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "rm",
				Arguments: []string{"-rf", "/"},
			},
			wantAllowed:            true,
			wantValidationRequired: true,
			// Audit-only deny still produces a result in the results list
			wantResultCount:      1,
			wantResultAction:     "deny",
			wantResultPolicyName: "deny-rm",
		},
		{
			name: "per-rule audit_only allows despite deny match",
			policies: []config.Policy{
				{
					Name:          "deny-rm-audit",
					CLIExpression: `cli.command == "rm"`,
					Action:        config.PolicyActionDeny,
					Message:       "rm would be blocked (audit)",
					Mode:          config.PolicyModeAuditOnly,
				},
			},
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "rm",
				Arguments: []string{"-rf", "/"},
			},
			wantAllowed:            true,
			wantValidationRequired: true,
			wantResultCount:        1,
			wantResultAction:       "deny",
			wantResultPolicyName:   "deny-rm-audit",
		},
		{
			name: "client info in CEL expression - hostname match denies",
			policies: []config.Policy{
				{
					Name:          "deny-production-host",
					CLIExpression: `cli.client_info.hostname == "production-server"`,
					Action:        config.PolicyActionDeny,
					Message:       "commands blocked on production hosts",
				},
			},
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "rm",
				Arguments: []string{"-rf", "/tmp/cache"},
				ClientInfo: &cliproxy.ClientInfo{
					Hostname: "production-server",
				},
			},
			wantAllowed:            false,
			wantValidationRequired: true,
			wantResultCount:        1,
			wantResultAction:       "deny",
			wantResultPolicyName:   "deny-production-host",
			wantMessageContains:    "commands blocked on production hosts",
		},
		{
			name: "client info in CEL expression - hostname no match allows",
			policies: []config.Policy{
				{
					Name:          "deny-production-host",
					CLIExpression: `cli.client_info.hostname == "production-server"`,
					Action:        config.PolicyActionDeny,
					Message:       "commands blocked on production hosts",
				},
			},
			validateCommands: []string{"*"},
			request: cliproxy.ValidationRequest{
				Command:   "rm",
				Arguments: []string{"-rf", "/tmp/cache"},
				ClientInfo: &cliproxy.ClientInfo{
					Hostname: "dev-workstation",
				},
			},
			wantAllowed:            true,
			wantValidationRequired: true,
			wantResultCount:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			topLevelMode := tt.topLevelMode
			if topLevelMode == "" {
				topLevelMode = config.PolicyModeEnforce
			}
			stack := setupCLIIntegrationTest(t, tt.policies, topLevelMode, tt.validateCommands)

			resp, err := stack.client.Validate(context.Background(), tt.request)
			require.NoError(t, err)

			// Core assertions
			assert.Equal(t, tt.wantAllowed, resp.Allowed, "Allowed mismatch")
			assert.Equal(t, tt.wantValidationRequired, resp.ValidationRequired, "ValidationRequired mismatch")
			assert.Len(t, resp.Results, tt.wantResultCount, "Results count mismatch")

			// Result detail assertions
			if tt.wantResultCount > 0 && len(resp.Results) > 0 {
				firstResult := resp.Results[0]
				if tt.wantResultAction != "" {
					assert.Equal(t, tt.wantResultAction, firstResult.Action, "Result action mismatch")
				}
				if tt.wantResultPolicyName != "" {
					assert.Equal(t, tt.wantResultPolicyName, firstResult.PolicyName, "Result policy name mismatch")
				}
				assert.Equal(t, "cel", firstResult.PolicyType, "All policies in this test are CEL")
			}

			if tt.wantMessageContains != "" {
				assert.Contains(t, resp.Message, tt.wantMessageContains, "Message mismatch")
			}

			// Cross-boundary assertions that validate serialization round-trip
			assert.NotEmpty(t, resp.RequestID, "RequestID should always be populated")
			assert.Equal(t, "test-1.0.0", resp.ServerVersion, "ServerVersion should be echoed")
		})
	}
}

// TestCLIValidation_Integration_ClientVersionEchoed verifies the cross-boundary
// serialization of client_info.cli_version from cliproxy types through gateway types
// and back.
func TestCLIValidation_Integration_ClientVersionEchoed(t *testing.T) {
	stack := setupCLIIntegrationTest(t, nil, config.PolicyModeEnforce, []string{"*"})

	resp, err := stack.client.Validate(context.Background(), cliproxy.ValidationRequest{
		Command:   "echo",
		Arguments: []string{"hello"},
		ClientInfo: &cliproxy.ClientInfo{
			CLIVersion: "2.5.0",
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "2.5.0", resp.ClientVersion, "ClientVersion should be echoed from request")
}

// TestCLIValidation_Integration_AuditEntry verifies the audit entry is fully
// populated through the integration flow including CLI info, upstream request
// metadata, validation details, and timing.
func TestCLIValidation_Integration_AuditEntry(t *testing.T) {
	policies := []config.Policy{
		{
			Name:          "deny-sudo",
			CLIExpression: `cli.command == "sudo"`,
			Action:        config.PolicyActionDeny,
			Message:       "sudo is blocked",
		},
	}

	stack := setupCLIIntegrationTest(t, policies, config.PolicyModeEnforce, []string{"*"})

	resp, err := stack.client.Validate(context.Background(), cliproxy.ValidationRequest{
		Command:          "sudo",
		Arguments:        []string{"rm", "-rf", "/"},
		WorkingDirectory: "/home/user/project",
		ClientInfo: &cliproxy.ClientInfo{
			Hostname:   "dev-workstation",
			Username:   "developer",
			OS:         "linux",
			Arch:       "amd64",
			Shell:      "/bin/bash",
			CLIVersion: "1.3.0",
		},
	})
	require.NoError(t, err)
	assert.False(t, resp.Allowed, "sudo should be denied")

	// Verify audit entry
	entries := stack.auditWriter.getEntries()
	require.Len(t, entries, 1, "Expected exactly one audit entry")
	entry := entries[0]

	// CLI info
	require.NotNil(t, entry.CLI, "CLI should be populated")
	assert.Nil(t, entry.Tool, "Tool should be nil for CLI validations")
	assert.Equal(t, "sudo", entry.CLI.Command)
	assert.Equal(t, []string{"rm", "-rf", "/"}, entry.CLI.Arguments)
	assert.Equal(t, "/home/user/project", entry.CLI.WorkingDirectory)

	// Client info
	require.NotNil(t, entry.CLI.ClientInfo)
	assert.Equal(t, "dev-workstation", entry.CLI.ClientInfo.Hostname)
	assert.Equal(t, "developer", entry.CLI.ClientInfo.Username)
	assert.Equal(t, "linux", entry.CLI.ClientInfo.OS)
	assert.Equal(t, "amd64", entry.CLI.ClientInfo.Arch)
	assert.Equal(t, "/bin/bash", entry.CLI.ClientInfo.Shell)
	assert.Equal(t, "1.3.0", entry.CLI.ClientInfo.CLIVersion)

	// Upstream request info
	assert.NotEmpty(t, entry.UpstreamRequest.RequestID, "RequestID should be generated")
	assert.Equal(t, "integration-test-client", entry.UpstreamRequest.ClientID, "ClientID from X-Maybe-Dont-Client-ID header")

	// Action and timing
	assert.Equal(t, "deny", entry.Action)
	assert.NotEmpty(t, entry.ValidationStarted)
	assert.NotEmpty(t, entry.CreatedAt)
	assert.GreaterOrEqual(t, entry.DurationMs, int64(0))

	// Validation details (CEL deciding rule)
	require.NotNil(t, entry.RequestValidation, "RequestValidation should be populated for policy-evaluated requests")
	require.NotNil(t, entry.RequestValidation.CEL, "CEL details should be populated")
	assert.Equal(t, "deny", entry.RequestValidation.CEL.Action)
	assert.Equal(t, "deny-sudo", entry.RequestValidation.CEL.DecidingRule)
	assert.Equal(t, "sudo is blocked", entry.RequestValidation.CEL.Reason)
}

// TestCLIValidation_Integration_RequestIDInLogs verifies that the generated request ID
// is propagated to the context used by downstream loggers (CEL engine, AI engine).
// Before the fix, all CLI validation logs had request_id: "-" because the handler
// didn't attach the request ID to the context passed to evaluatePolicies.
func TestCLIValidation_Integration_RequestIDInLogs(t *testing.T) {
	// Use zap/observer to capture log entries so we can inspect fields
	core, observed := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	sessionLogger := config.NewSessionLogger(logger)

	celEngine, err := NewCELPolicyEngine(context.Background(), sessionLogger)
	require.NoError(t, err)

	err = celEngine.LoadPolicies([]config.Policy{
		{
			Name:          "deny-rm",
			CLIExpression: `cli.command == "rm"`,
			Action:        config.PolicyActionDeny,
			Message:       "rm is blocked",
		},
	}, config.PolicyModeEnforce)
	require.NoError(t, err)

	evaluator := &PolicyEvaluator{
		CELEngine: celEngine,
		Logger:    sessionLogger,
	}

	handler := NewCLIValidationHandler(CLIValidationHandlerConfig{
		Enabled:               true,
		ValidateCommands:      []string{"*"},
		Logger:                sessionLogger,
		Version:               "test-1.0.0",
		Evaluator:             evaluator,
		IncludeArgumentValues: true,
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := cliproxy.NewClient(cliproxy.ClientConfig{
		ServerURL: server.URL,
	})

	resp, err := client.Validate(context.Background(), cliproxy.ValidationRequest{
		Command:   "rm",
		Arguments: []string{"-rf", "/tmp/test"},
	})
	require.NoError(t, err)
	assert.False(t, resp.Allowed)

	// The response has the generated request ID — verify it appears in log entries
	requestID := resp.RequestID
	require.NotEmpty(t, requestID)

	// Find CEL evaluation log entries and verify they have the request ID
	var foundCELLog bool
	for _, entry := range observed.All() {
		if entry.Message == "Evaluating CLI command with CEL policies" {
			foundCELLog = true
			ridField, ok := findStringField(entry.ContextMap(), "request_id")
			require.True(t, ok, "request_id field should be present in CEL log entry")
			assert.Equal(t, requestID, ridField,
				"request_id in CEL engine logs should match the response request ID")
		}
	}
	assert.True(t, foundCELLog, "Should have found CEL evaluation log entry")
}

// findStringField extracts a string field from a log entry's context map.
func findStringField(fields map[string]interface{}, key string) (string, bool) {
	val, ok := fields[key]
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}
