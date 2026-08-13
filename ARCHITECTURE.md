# Architecture

This describes the gateway as it exists today. For aspirational/rejected
ideas from before the first implementation, see [DESIGN.md](DESIGN.md).

## Package layout

- `main.go`, `cmd/` — CLI entry point (Cobra). Subcommands: `gateway start`,
  `cli`, `test policies`, `skill`, `hooks`, `config`, `version`.
- `internal/config` — config loading (YAML + env + flags), defaults
  embedding (`internal/config/defaults/`), XDG path resolution.
- `internal/gateway` — the gateway itself: MCP proxy, REST endpoints,
  validation engines, audit pipeline, session management. The largest
  package in the repo (~35k lines across ~30 files).
- `internal/cliproxy` — client side of `maybe-dont cli`: talks to a running
  gateway's `/api/v1/cli/validate` endpoint.
- `internal/hooks` — embedded shell scripts and config snippets that wire
  editor/agent hooks (Claude Code, Cursor, Gemini CLI, Cline, Copilot) to
  the gateway's `/api/v1/intercept` endpoint.
- `internal/skills` — embedded Markdown "skill" definitions that instruct
  AI agents how to work with the gateway (CEL policy authoring, test case
  authoring, CLI usage).
- `internal/testsuite` — the `maybe-dont test policies` harness: runs a
  suite of test cases against the CEL and AI engines and reports pass/fail,
  optionally across a matrix of AI models, with historical state tracking.

## Request lifecycle

A tool call — whether it arrives via the MCP proxy, `/api/v1/intercept`, or
`/api/v1/cli/validate` — passes through the same ordered chain
(`internal/gateway/tool_validation.go`, `response_validation.go`):

1. **CEL request rules** (`cel_engine.go`) — deterministic CEL expressions
   evaluated against the request. Fast, no network call.
2. **AI request rules** (`ai_engine.go`, `ai_provider*.go`) — the request is
   sent to a configured AI provider (OpenAI, Anthropic, or an
   OpenAI-compatible endpoint) which returns a judgment. Runs only if CEL
   didn't already deny with a rule in `enforce` mode.
3. The call proceeds to the downstream MCP server or shell, if not denied.
4. **CEL response rules** (`cel_response_engine.go`) and **AI response
   rules** (`ai_response_engine.go`) — same two-stage evaluation, run
   against the response before it's returned to the caller. Can redact or
   deny.
5. **Audit write** (`audit_entry.go`, `audit_writer.go`) — every decision at
   every stage is recorded, whether or not it changed the outcome.

Each phase can be `enabled`/`disabled` and set to `mode: audit_only` (log
only) or `mode: enforce` (can actually deny/redact) independently, at both
the phase level and per-rule.

## Blocking budget and fail-open

`blocking_budget.go` tracks cumulative time spent waiting on validation
across all phases for a single request, capped by
`validation.max_blocking_ms` (default 90s), with
`validation.max_rule_evaluation_ms` (default 45s) capping any single rule.
Once the budget is exhausted, remaining validations continue asynchronously
in the background — for audit purposes — but the request itself proceeds.
This is deliberate fail-open behavior: a slow or unreachable AI provider
degrades to "audit only" rather than hanging or blocking real traffic.
`maybe-dont cli` and the agent hook scripts apply the same principle at a
higher level — if the gateway itself is unreachable, the command executes
with a warning rather than failing. See the README's "Fail-open and
fail-closed behavior" section for the single authoritative statement of
this contract.

## Entry surfaces

- **MCP proxy** (`gateway.go`, `client_manager.go`, `session.go`) — the
  gateway itself implements the MCP server protocol and proxies to one or
  more configured downstream MCP servers (`ClientManager`). Tools, prompts,
  and resources from each downstream are exposed with a
  `{client_name}__{original_name}` prefix so multiple servers can be
  combined without name collisions. Native introspection tools
  (`native_tools.go`, `audit_log_tool.go`, `audit_report_tool.go`,
  `list_servers_tool.go`, `list_sessions_tool.go`) are exposed under a
  `maybedont__` prefix.
- **`/api/v1/intercept`** (`intercept_handler.go`) — a REST endpoint for
  editor/agent hook scripts to submit a tool call (or its result) for a
  policy decision outside the MCP protocol.
- **`/api/v1/cli/validate`** (`cli_validation.go`) — validates a raw shell
  command line before an agent executes it; used by `maybe-dont cli` and by
  the intercept path when a tool call represents shell execution
  (`intercept.shell_tool_names` in config).

`server.go` wires these into the HTTP mux alongside `auth_middleware.go`
(optional caller-identity header enforcement) and
`trusted_proxy.go` (client IP extraction).

## Policy language

Deterministic rules are written in [CEL](https://github.com/google/cel-go)
against a request/response object. Rules support a plain `expression` (or
the more specific `mcp_expression`) for MCP tool calls and a separate
`cli_expression` for CLI commands, so one rule can cover both surfaces. See
`internal/config/defaults/cel_request_rules.yaml` for shipped examples and
[Design notes](#design-notes) below for rationale behind individual
features.

## Testing

- `go test ./...` — unit and integration tests per package.
- `maybe-dont test policies` (`internal/testsuite`) — end-to-end policy
  behavior tests against a YAML-defined suite of request/response cases,
  independent of `go test`. See
  [docs/specs/policy-test-suite.md](docs/specs/policy-test-suite.md).

## Design notes

Rationale behind decisions that aren't obvious from reading the code alone.
Consolidated from what used to be a larger set of individual `docs/specs/`
files, most of which described features that are now fully implemented and
self-evident from the code — this section keeps only the "why," not the
original design-process narrative.

### Validation & policy

- Rule config is `enabled: true/false` + `mode: audit_only` (only) — a
  deliberate simplification from an earlier 3-state
  `enabled`/`audit_only`/`disabled` enum. Don't reintroduce the 3-state
  form.
- Deterministic CEL rules are preferred over AI-evaluated rules where
  possible. AI model match rates on the policy test suite range roughly
  52-68% across models tested, versus 100% for CEL. Use AI evaluation only
  where CEL can't express the check.
- Audit entries distinguish `recommended_action` (what validation decided)
  from `action` (what actually happened) via an `action_reason` field
  (`request_policy`, `response_policy`, `audit_mode`, `fail_open`) — needed
  because audit-only mode and fail-open behavior mean the two can
  legitimately diverge.
- Test suite results distinguish real decision failures from cases where
  only *extra* policies fired unexpectedly (`ExtraPolicyOnly` in
  `internal/testsuite`) — the latter isn't a regression, the decision was
  still correct.

### Integration surfaces

- Three ways to integrate: the MCP gateway proxy (intercepts all tool
  calls), the CLI proxy (`maybe-dont cli -- <command>`, validates via REST
  before exec), and agent-native hooks (deterministic, fire regardless of
  LLM compliance — unlike relying on a skill/prompt alone).
- The CLI proxy has no response validation: commands run via `syscall.Exec`,
  so output can't be inspected after the fact. Known limitation, not an
  oversight.
- Agent hook integration (Claude Code, Cursor, etc.) is **partially
  shipped**: the intercept endpoint and hook scripts for most agents are
  done; Cursor's response-mutation path and shell-level tests are still
  open (tracked upstream as issue #131).
- `/api/v1/action/validate` exists specifically for OpenHands'
  security-analyzer integration and maps directly to OpenHands' `SecurityRisk`
  enum values — it's not a general-purpose endpoint.
- The AI provider abstraction supports `openai`, `anthropic`, and
  `openai_compatible` — the compatible mode exists so self-hosted/local
  models work without a first-class integration.

### Config & state

- Config/log directories follow XDG Base Directory conventions
  (`~/.config/maybe-dont`, `~/.local/state/maybe-dont`) instead of
  `~/.maybe-dont`, for consistency with dotfile-management tools and
  container environments. Resolution still falls back through:
  `--config-dir`/env var → `./config` → legacy `$HOME/.maybe-dont/config`
  (back-compat for existing installs) → cwd.
- `downstream_mcp_servers` (normally YAML, map-keyed) can be fully
  configured via `MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_<NAME>_<FIELD>` env
  vars instead — needed because Claude Desktop doesn't support mounting a
  YAML config file.
- The optional required-header check
  (`MAYBE_DONT_REQUIRED_HEADER_NAME`/`_VALUE`) is a caller identifier for
  audit logging, not an auth secret.
- Audit logging uses a dedicated `JSONLAuditWriter` (rotation-aware) rather
  than inline logging, to keep audit I/O off the hot request path.
- Discovery requests are deduplicated per session/client via a
  `singleflight.Group` (key: `sessionID/clientName`) to avoid redundant
  concurrent tool-discovery calls.
