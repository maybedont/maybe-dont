# Policy Test Suite

> **Status**: See [docs/specs/README.md](README.md) for current status.

## Overview

A CLI-based policy test harness for validating CEL and AI validation policies:
regression testing, cross-provider/model comparison, and cost/performance
optimization (finding the cheapest model that still produces acceptable
results). Used both internally and by customers testing their own policies.

## Scope

### Engines

| Engine | Notes |
|--------|-------|
| CEL request policies | Deterministic, no AI API calls |
| AI request policies | Live API calls, model matrix applies |
| CEL response policies | Deterministic |
| AI response policies | Live API calls, model matrix applies |

### Response Validation Actions

Response policies have different valid actions depending on whether the tool call is read-only or state-changing:

| Tool Call Type | Valid Actions | Rationale |
|----------------|---------------|-----------|
| Read-only (GET, LIST, SEARCH) | allow, deny, redact | Can prevent data exposure before it happens |
| State-changing (PUT, POST, DELETE) | allow, redact | Action already occurred; deny is meaningless |

The test harness does not enforce this distinction — test case authors are responsible for setting appropriate expectations.

## CLI Interface

```bash
maybe-dont test policies --suite-dir <path> [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--suite-dir` | Directory containing `suite.yaml` and `cases/` (required) |
| `--engine` | `cel`, `ai`, or `all` (default: from suite.yaml) |
| `--model` | Override model(s) for AI tests: `provider:model` (comma-separated) |
| `--matrix` | Run the full model matrix from suite.yaml |
| `--format` | Output format for `--output`: `json` or `junit` (default: json) |
| `--output` | Write structured output to file, in addition to stdout |
| `-q, --quiet` | Suppress stdout (use with `--output` for file-only output) |
| `--tags` | Only run cases with these tags (comma-separated) |
| `--exclude-tags` | Skip cases with these tags (comma-separated) |
| `--case-pattern` | Glob pattern for case IDs (default: `*`) |
| `--validate-only` | Validate suite config without running tests |
| `--include-disabled` | Include policies with `enabled: false` |
| `--timeout` | Timeout per test case, ms (default: from suite.yaml) |
| `--requests-per-minute` | Override requests/min for all providers |
| `--max-tests` | Max tests per model per invocation (exit code 5 if more remain) |
| `--wait` | Run continuously until all tests complete (requires `--incremental` or `--full`) |
| `--incremental` | Skip unchanged tests, persist results to state file |
| `--full` | Run all tests, persist results (refreshes cache) |
| `--state-file` | Override state file location (with `--incremental`/`--full`) |
| `--retry-failed` | Re-run failed/errored tests even if cached |
| `--summary-only` | Show summary from cached state without running tests |
| `--history-depth` | Override pass-rate history depth (default: from suite.yaml or 20) |

### Examples

```bash
# Run against the real suite CI uses
maybe-dont test policies --suite-dir internal/config/defaults/tests

# Full model matrix, JUnit output for CI
maybe-dont test policies --suite-dir ./suite --matrix --format junit --output results.xml

# Filter by tags / case pattern
maybe-dont test policies --suite-dir ./suite --tags security --exclude-tags slow
maybe-dont test policies --suite-dir ./suite --case-pattern "req-mcp-*"

# Validate config without running
maybe-dont test policies --suite-dir ./suite --validate-only

# Incremental runs (skip unchanged, persist state), resuming under rate limits
maybe-dont test policies --suite-dir ./suite --incremental --wait
```

## Suite Configuration

### Directory Layout

```
<suite-dir>/
  suite.yaml           # required
  cases/
    *.yaml             # auto-discovered recursively, may be nested in subdirs
```

The live example suite CI runs is `internal/config/defaults/tests/`.

### suite.yaml Schema

```yaml
version: "v1"
bundle_id: "my-policies-2026-02"

policies:                                             # file or directory path
  cel_request_rules: "./rules/cel_request_rules.yaml"
  ai_request_rules: "./rules/ai_request/"              # directory: all .yaml files, recursive
  cel_response_rules: "./rules/cel_response_rules.yaml"
  ai_response_rules: "./rules/ai_response/"

acceptance:
  min_match_rate: 1.0        # 1.0 = strict, all tests must pass

execution:
  timeout_ms: 30000
  retries: 2                 # transient errors only (network/timeout/5xx) — never for wrong decisions or 4xx
  retry_delay_ms: 1000

engines:
  cel:
    enabled: true
  ai:
    enabled: true
    model_matrix: [...]      # see Model Matrix below

filters:
  tags: []
  exclude_tags: []
  case_pattern: "*"
```

### Model Matrix

```yaml
providers:                    # API keys at the provider level, shared across models
  openai:
    api_key: "${OPENAI_API_KEY}"
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
  openai_compatible:
    api_key: "${AZURE_OPENAI_API_KEY}"   # also used for self-hosted/local models (e.g. Ollama)

engines:
  ai:
    model_matrix:
      - provider: "openai"
        model: "gpt-5-mini"
        parameters:
          temperature: 0.0
      - provider: "anthropic"
        model: "claude-sonnet-4-5-20250929"   # dated version, for reproducible results
        parameters:
          max_tokens: 4096
```

| Field | Required | Description |
|-------|----------|-------------|
| `provider` | Yes | `openai`, `openai_compatible`, or `anthropic` |
| `endpoint` | For `openai_compatible` | Full chat-completions URL |
| `model` | Yes | Model identifier |
| `api_key` | No | Per-model override (inherits from `providers` if unset) |
| `parameters` | No | temperature, max_tokens, etc. |
| `enabled` | No | `false` to disable this model (default: true) |

### Execution behavior

- Tests run serially, not in parallel.
- Retries only apply to transient errors (network/timeout/5xx) — never to wrong decisions or 4xx.
- On a 429, the harness stops testing that model for the rest of the run; remaining cases for that model are marked `skipped` (reason `rate_limited`); other models continue. This is deliberate fail-fast, not an oversight.
- `--requests-per-minute` and dynamic rate-limit tracking (reading provider rate-limit response headers) are both implemented — the harness slows down proactively rather than waiting to hit 429s.
- `--incremental`/`--full`/`--wait`/`--state-file` implement result caching keyed by content hash (test case + referenced policy) — changing either invalidates the cache for that case.

## Test Case Schema

**Request validation (deny):**

```yaml
case_id: "req-github-delete-file-deny"
title: "Deny file deletion in protected paths"
tags: ["security", "file-operations"]

phase: "request"              # request | response | both (default: request)
engine: "both"                # cel | ai | both (default: both)

request:
  tool_name: "github__delete_file"
  arguments:
    path: "/etc/passwd"

expectations:
  decision: "deny"            # allow | deny | redact (required)
```

**Response validation (redact):**

```yaml
case_id: "resp-redact-ssn"
phase: "response"
engine: "ai"

request:
  tool_name: "database__query"
  arguments:
    sql: "SELECT * FROM users WHERE id = 123"

response:
  content:
    - type: "text"
      text: "User: John Doe, SSN: 123-45-6789"
  is_error: false

expectations:
  decision: "redact"
  redacted_content:                        # exact string match against actual output
    - type: "text"
      text: "User: John Doe, SSN: [PII_REDACTED]"
```

If `redacted_content` is omitted, only the `redact` decision is checked, not the output.

### Decision precedence

`deny > redact > allow` — if any policy denies, the overall decision is deny; else if any redacts, it's redact; only allow if every policy allows. `expectations.decision` asserts this final combined decision.

### Field reference

| Field | Required | Description |
|-------|----------|-------------|
| `case_id` | Yes | Unique ID, used in filtering/reporting |
| `request.tool_name` | Yes | MCP tool name (with client prefix if applicable) |
| `request.arguments` | Yes | Tool arguments |
| `expectations.decision` | Yes | `allow`, `deny`, or `redact` |
| `tags` | No | For filtering |
| `phase` | No | `request` (default), `response`, or `both` |
| `engine` | No | `cel`, `ai`, or `both` (default) |
| `response.content[].{type,text}` | For response tests | Matches MCP Content structure |
| `expectations.redacted_content` | No | Expected post-redaction content |

## Output

Result status: `passed`, `failed`, `errored`, `skipped`, `rate_limited`. Formats: text (default, human-readable), JUnit XML, JSON — same underlying data model (case ID, expected vs actual decision, per-policy timing, AI reasoning where applicable).

## CI Integration

Real workflow: [`.github/workflows/policy-tests.yaml`](../../.github/workflows/policy-tests.yaml). Triggers on push to `main` when `internal/config/defaults/**` changes, monthly (keeps cached AI-test state artifact alive), or manual dispatch. Default `suite_dir` is `internal/config/defaults/tests`. Jobs: `validate` (schema check via `--validate-only`, gates the rest), `test-cel` (no API calls), `test-ai` (model matrix from suite.yaml, cached/incremental via the state artifact). Requires `OPENAI_API_KEY` and `ANTHROPIC_API_KEY` secrets for AI tests.

## Coverage Reporting

The harness can report which enabled policies have zero test cases referencing them (`policy_name` in `expectations.policies`), to catch untested rules. Disabled policies (`enabled: false`) are excluded from coverage checks by default; pass `--include-disabled` to include them.
