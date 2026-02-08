# Spec: Three New Agent Skills (CEL Policy, AI Policy, Test Case)

> **Status**: See [README.md](README.md)

## Overview

Create three new embedded skills following the same pattern as the existing `cli` skill:
1. **cel-policy** — Guide for authoring CEL (deterministic) policies
2. **ai-policy** — Guide for authoring AI (LLM-powered) policies
3. **test-case** — Guide for writing test cases and configuring test suites

Each skill gets 4 format files (claude, cursor, copilot, generic) and is registered in `skills.go`.

## Motivation

AI agents working with the Maybe Don't gateway need guidance on:
- Writing CEL and AI policy rules (correct YAML structure, context variables, actions, common mistakes)
- Writing test cases for those policies and configuring test suites (suite.yaml schema, test case YAML structure, CLI flags)

These skills are served via `maybe-dont skill view <name>` and can be injected into agent system prompts.

## Files to Create (12 new skill files)

### CEL Policy Skill
| File | Format |
|------|--------|
| `internal/skills/cel-policy.md` | Claude |
| `internal/skills/cel-policy.cursorrules` | Cursor |
| `internal/skills/cel-policy.copilot.md` | Copilot |
| `internal/skills/cel-policy.generic.md` | Generic |

### AI Policy Skill
| File | Format |
|------|--------|
| `internal/skills/ai-policy.md` | Claude |
| `internal/skills/ai-policy.cursorrules` | Cursor |
| `internal/skills/ai-policy.copilot.md` | Copilot |
| `internal/skills/ai-policy.generic.md` | Generic |

### Test Case Skill
| File | Format |
|------|--------|
| `internal/skills/test-case.md` | Claude |
| `internal/skills/test-case.cursorrules` | Cursor |
| `internal/skills/test-case.copilot.md` | Copilot |
| `internal/skills/test-case.generic.md` | Generic |

## Files to Modify

### `internal/skills/skills.go`
- Add `//go:embed` directives for all 12 new files
- Add 3 new entries to the `Skills()` function return slice
- Keep existing `CLISkill` backward compat unchanged

### `internal/skills/skills_test.go`
- Update `TestSkills` assertion: `require.Len(t, skills, 1, ...)` → `4`
- Add test cases to `TestGetSkill` table for: `cel-policy`, `ai-policy`, `test-case`
- Add format/content tests for each new skill (following `TestCursorFormat`, `TestCopilotFormat`, `TestGenericFormat` patterns)

## Skill Content Design

### CEL Policy Skill Content

Covers both request and response CEL policies, including:

**Rule YAML structure:**
- Fields: `name`, `description`, `enabled`, `mcp_expression`, `cli_expression`, `expression` (legacy), `action`, `message`, `mode`, `redaction_pattern`, `redaction_replacement`

**Context variables for request rules (`mcp_expression`):**
- `request.method`, `request.params.name`, `request.params.arguments`, `request.params.meta`
- Shorthands: `tool.name`, `tool.arguments`

**Context variables for CLI rules (`cli_expression`):**
- `cli.command`, `cli.arguments`, `cli.working_directory`
- `cli.client_info.hostname`, `.username`, `.os`, `.arch`, `.shell`, `.cli_version`

**Context variables for response rules (`expression`):**
- `response.content` (list with `.type`, `.text`, `.data`, `.mimeType`), `response.isError`, `response.meta`
- `request.params.name`, `request.params.arguments` (original request context)

**Actions:** `allow`, `deny`, `redact` (response only)

**CEL function reference table:**
- `has()`, `get()`, `.contains()`, `.startsWith()`, `.endsWith()`, `.matches()`, `.size()`, `.exists()`, `.all()`, `in`

**Complete examples:** request deny, response redact, dual MCP/CLI rule

**Common mistakes table:** non-boolean expressions, missing `has()` guards, PCRE vs Go regex, redact on request rules

**Testing workflow:** `mode: audit_only` → check audit logs → remove mode

### AI Policy Skill Content

Covers AI-powered policy rules for request and response validation:

**Rule YAML structure:**
- Fields: `name`, `description`, `enabled`, `action`, `message`, `mode`, `prompt`

**Operation context injection:**
- The engine automatically appends operation context to prompts at runtime — do not include `%s` in prompts (rejected at load time)
- MCP tool calls: appended as `Tool call:` + JSON `{"type": "mcp_tool", "name": "...", "arguments": {...}}`
- CLI commands: appended as `CLI command:` + JSON `{"type": "cli", "name": "...", "arguments": [...]}`
- Responses: appended as `Response content:` + formatted text with `IsError:`, `Content:`, `Meta:` sections

**Expected AI response format:**
- Request: `{"allowed": true/false, "message": "..."}`
- Response: `{"allowed": true/false, "message": "...", "redacted_content": "..."}`

**Actions:** `deny` (request + response), `redact` (response only)

**Prompt engineering best practices:**
- ANALYZE / Look for / EXAMPLES structure
- Include both safe and dangerous examples
- One focused concern per rule
- Do not include `%s` in prompts — engine appends operation context automatically

**Complete examples:** request deny (mass deletion), response redact (credentials), response deny (sensitive data)

**Common mistakes:** including `%s` in prompt (rejected at load time), overly broad prompts, no examples, combining concerns, including response format in prompt

### Test Case Skill Content

Covers both `suite.yaml` configuration and test case authoring:

**Suite configuration (`suite.yaml`):**
```yaml
version: "v1"                          # Required
bundle_id: "unique-id"                 # Required
description: "..."                     # Optional
policies:                              # Required (at least one)
  cel_request_rules: "path"
  ai_request_rules: "path"
  cel_response_rules: "path"
  ai_response_rules: "path"
providers:                             # Optional: provider-level API keys
  openai:
    api_key: "${OPENAI_API_KEY}"
    endpoint: "..."
acceptance:
  min_match_rate: 1.0                  # 0.0-1.0
  strict_policy_match: true
execution:
  timeout_ms: 30000
  retries: 2
  retry_delay_ms: 1000
  # Proactive pacing - applied to every request regardless of API headers.
  # The runner also adapts to 429 responses using retry-after headers.
  delay_between_requests_ms: 100
  rate_limit_buffer_ms: 5000
  rate_limits:
    openai:
      requests_per_minute: 60
engines:
  cel:
    enabled: true
  ai:
    enabled: true
    model_matrix:
      - provider: openai
        model: gpt-4o-mini
        enabled: true
        parameters: {}
filters:
  tags: []
  exclude_tags: []
  case_pattern: ""
```

**Test case YAML structure:**
```yaml
- case_id: "cel-req-001"              # Required: unique ID
  title: "Block delete_file"          # Required
  tags: [cel, request, github]        # Optional: filtering
  notes: ["Real-world scenario"]      # Optional: documentation
  phase: request                      # request, response, both (default: request)
  engine: cel                         # cel, ai, both (default: both)
  request:
    tool_name: "github__delete_file"
    arguments: {owner: "org"}
  response:                           # Required when phase includes response
    content:
      - type: text
        text: "File contents"
    is_error: false
  expectations:
    decision: deny                    # allow, deny, redact
    policies:                         # Optional: specific policy expectations
      - policy_name: "deny-delete"
        decision: deny
    redacted_content:                 # Optional: for redact tests
      - type: text
        text: "Expected redacted output"
```

**Running tests — CLI command and key flags:**
- `maybe-dont test policies --suite-dir <dir>`
- `--engine {cel|ai|all}`, `--model provider:model`, `--matrix`
- `--tags`, `--exclude-tags`, `--case-pattern`
- `--incremental`, `--retry-failed`, `--full`, `--state-file`
- `--max-tests`, `--wait`, `--timeout`, `--rpm`
- `--validate-only`, `--include-disabled`
- `--output <file>`, `--format {json|junit}`, `--quiet`

**Exit codes:**
| Code | Meaning |
|------|---------|
| 0 | All tests passed, thresholds met |
| 1 | Test failure (thresholds not met) |
| 2 | Schema validation error |
| 3 | Policy integrity error |
| 4 | Path resolution error |
| 5 | More tests remain (`--max-tests`) |

## Format Conventions Per Platform

Following the CLI skill pattern exactly:

| Format | File Extension | Header Style | Section Style |
|--------|---------------|-------------|---------------|
| Claude | `.md` | `# skill-name` + `## Description` + `## Instructions` | Structured markdown with H2/H3, most detailed |
| Cursor | `.cursorrules` | `# Title Rules` + `## Rules` | Numbered rules, compact |
| Copilot | `.copilot.md` | `# Title Instructions` + `## Overview` | `## Important Rules` + `## Handling ...` |
| Generic | `.generic.md` | `# Title Instructions` + `## Purpose` | `## Behavior Guidelines`, clean/universal |

## Implementation Order

1. Write 4 CEL policy skill files
2. Write 4 AI policy skill files
3. Write 4 test case skill files
4. Update `skills.go` with embeds and registrations
5. Update `skills_test.go` with new tests

## Verification

1. `go build ./...` — Ensure embeds compile
2. `go test ./internal/skills/...` — All skill tests pass
3. `go test ./...` — Full test suite passes
4. `./maybe-dont skill list` — Shows all 4 skills
5. `./maybe-dont skill view cel-policy --format claude` — Outputs CEL policy skill
6. `./maybe-dont skill view ai-policy --format cursor` — Outputs AI policy skill
7. `./maybe-dont skill view test-case --format generic` — Outputs test case skill
