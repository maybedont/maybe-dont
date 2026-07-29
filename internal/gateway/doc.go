// Package gateway implements the Maybe Don't security gateway: an MCP
// proxy and set of REST endpoints that validate tool calls and CLI
// commands against configurable policies before they reach a downstream
// MCP server or shell.
//
// A request passes through an ordered validation chain, in this order:
// CEL request rules (cel_engine.go), AI request rules (ai_engine.go,
// ai_provider*.go), the downstream call itself, then CEL response rules
// (cel_response_engine.go) and AI response rules (ai_response_engine.go).
// Every stage writes a decision to the audit log (audit_entry.go,
// audit_writer.go) regardless of whether it changed the outcome.
//
// The gateway exposes three entry surfaces onto that same chain:
//
//   - The MCP proxy itself (gateway.go, client_manager.go, session.go),
//     which multiplexes one or more downstream MCP servers behind a
//     single MCP endpoint, prefixing each downstream's tools/prompts/
//     resources with "{client_name}__".
//   - POST /api/v1/intercept (intercept_handler.go), used by editor/agent
//     hook scripts (see internal/hooks) to submit a tool call for a
//     decision outside the MCP protocol.
//   - POST /api/v1/cli/validate (cli_validation.go), used by the
//     `maybe-dont cli` command (internal/cliproxy) to validate a raw
//     shell command line before it runs.
//
// See ARCHITECTURE.md at the repository root for the full request
// lifecycle, the blocking budget that bounds validation latency
// (blocking_budget.go), and the project's fail-open/fail-closed contract.
package gateway
