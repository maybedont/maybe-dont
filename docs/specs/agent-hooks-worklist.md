# Agent Hook Scripts — Implementation Worklist

> Tracks implementation progress for [#131](https://github.com/maybedont/maybe-dont/issues/131).
> Design spec: [agent-hook-and-interceptor-integration.md](agent-hook-and-interceptor-integration.md)

## Phase 1: Core Infrastructure

- [ ] **1.1 — `internal/hooks/` package** — Create embedded hooks package mirroring `internal/skills/`. Go API: `ListHooks()`, `GetHook(name)`, `GetConfig(name)`. Embed `.sh` and `.config.json` files.
- [ ] **1.2 — `cmd/hooks.go` CLI command** — Add `maybe-dont hooks list` and `maybe-dont hooks export --agent <name> [--config]` subcommands. Mirrors `cmd/skill.go` pattern.
- [ ] **1.3 — Core hook functions** — Write the shared bash functions that get inlined into each agent script:
  - `md_check_deps()` — verify `jq` and `curl` are available
  - `md_call_gateway()` — POST to `$MAYBE_DONT_URL/api/v1/intercept`, 30s timeout, fail-open on error/timeout
  - `md_is_denied()` — check `valid` field in response
  - `md_get_reason()` — extract `messages[].message` from response
  - `md_get_redacted_payload()` — extract mutated payload for Cursor response hooks

## Phase 2: Pre-Tool Hook Scripts

- [ ] **2.1 — Claude Code hook** (`claude-code.sh`) — Reads `PreToolUse`/`PostToolUse` stdin. Extracts `tool_name` + `tool_input`. Returns exit 0 (allow) or JSON `permissionDecision: deny`.
- [ ] **2.2 — Claude Code config** (`claude-code.config.json`) — `.claude/settings.json` snippet. Default: `Bash` matcher only (recommended "Both" pattern). Commented-out: broad matcher for "hooks only" pattern.
- [ ] **2.3 — Cursor hook** (`cursor.sh`) — Handles all 4 events: `beforeShellExecution`, `afterShellExecution`, `beforeMCPExecution`, `afterMCPExecution`. Detects event from payload shape. Returns JSON `permission: deny`.
- [ ] **2.4 — Cursor config** (`cursor.config.json`) — `.cursor/hooks/` config. Default: `beforeShellExecution` only. Commented-out: include `beforeMCPExecution` for "hooks only".
- [ ] **2.5 — Gemini CLI hook** (`gemini-cli.sh`) — Reads `BeforeTool`/`AfterTool` stdin. Extracts `tool_name` + `tool_input` + `mcp_context`. Returns exit 0 or JSON `decision: deny`.
- [ ] **2.6 — Gemini CLI config** (`gemini-cli.config.json`) — `settings.json` snippet with shell tool matchers.
- [ ] **2.7 — Cline hook** (`cline.sh`) — Reads `preToolUse`/`postToolUse` stdin. Extracts `tool` + `parameters`. Returns JSON `cancel: true, errorMessage: "..."`.
- [ ] **2.8 — Cline config** (`cline.config.json`) — `.clinerules/hooks/` config.
- [ ] **2.9 — Copilot hook** (`copilot.sh`) — Reads `PreToolUse`/`PostToolUse` stdin. Returns JSON `hookSpecificOutput.permissionDecision: deny`.
- [ ] **2.10 — Copilot config** (`copilot.config.json`) — `.github/hooks/` config. Note: same hooks work for Cody.

## Phase 3: Post-Tool Hook Scripts

Post-tool logic is included in the same script files from Phase 2. This phase covers the response-specific behavior:

- [ ] **3.1 — Post-tool: observability path** — For Claude Code, Gemini CLI, Cline, Copilot: detect post-tool phase, send `phase: "response"` with tool result to gateway, log warnings to stderr. Cannot modify output.
- [ ] **3.2 — Post-tool: Cursor mutation path** — For Cursor `afterMCPExecution` only: when gateway returns `type: "mutation"` with `modified: true`, return `updated_mcp_tool_output` with the redacted payload. This applies gateway redaction rules to actual tool output.

## Phase 4: Testing

- [ ] **4.1 — Go unit tests** — `internal/hooks/hooks_test.go`: verify all hooks are embedded, can be listed, retrieved by name, and config snippets parse as valid JSON.
- [ ] **4.2 — Hook script tests** — Shell-based tests for each agent script. Mock the gateway with a simple HTTP server (or `nc`), feed agent-specific JSON to stdin, verify:
  - Correct `/api/v1/intercept` request body sent
  - Correct deny output format per agent
  - Correct exit code per agent
  - Fail-open behavior when gateway unreachable
  - Pre vs post phase detection
  - Context passthrough (session_id, cwd)
- [ ] **4.3 — End-to-end test** — Start gateway with a test CEL deny rule, run a hook script with a known-deny tool call, verify the tool is blocked with the correct agent-specific output.

## Phase 5: Documentation (maybedont.ai/docs — separate repo)

- [ ] **5.1 — Intercept API docs page** — Add `/docs/api/intercept/` documenting `POST /api/v1/intercept` (currently undocumented on the site).
- [ ] **5.2 — Hooks guide** — New docs page explaining hooks integration: when to use hooks vs MCP gateway vs CLI skill, recommended deployment patterns, per-agent setup instructions.
- [ ] **5.3 — Update Claude Code page** — Add "Hooks Integration" section to `/docs/mcp-gateway/examples/connecting-claude-code/`.
- [ ] **5.4 — Update Cursor page** — Add "Hooks Integration" section. Highlight unique `afterMCPExecution` mutation capability.
- [ ] **5.5 — Update Copilot page** — Add "Hooks Integration" section. Add note that Cody uses the same hook format.
- [ ] **5.6 — Update Gemini page** — Add "Hooks Integration" section. Distinguish Gemini CLI hooks from Gemini Code Assist IDE integration.
- [ ] **5.7 — Add Cline page** — New page at `/docs/mcp-gateway/examples/connecting-cline/` covering MCP gateway + hooks setup.
- [ ] **5.8 — De-emphasize CLI proxy** — Update CLI gateway docs and agent pages to position CLI proxy as a last-resort option for agents that don't support hooks. Hooks are the recommended approach for CLI command validation.
- [ ] **5.9 — Cody page** — Add note redirecting to Copilot hooks. Consider removing standalone page or reducing to a short "use Copilot hooks" note.

## Phase 6: Cleanup

- [ ] **6.1 — Update spec status** — Mark `agent-hook-and-interceptor-integration.md` as Implemented in spec and README.
- [ ] **6.2 — Close issue** — Close #131 with summary of what shipped.
