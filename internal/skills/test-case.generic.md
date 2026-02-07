# Policy Test Case Authoring Instructions

## Purpose
Write and configure policy test cases for the Maybe Don't gateway. The test framework validates CEL and AI policy rules by running structured test cases defined in YAML files.

## Directory Structure

```
suite-dir/
  suite.yaml            # Suite configuration
  cases/
    *.yaml              # Test case files
```

## Test Case Structure

```yaml
- case_id: "cel-req-001"               # Required: unique across suite
  title: "Block github__delete_file"    # Required: human-readable title
  tags: [cel, request, github]          # Optional: filtering tags
  notes: ["Scenario documentation"]     # Optional: context notes
  phase: request                        # request, response, both (default: request)
  engine: cel                           # cel, ai, both (default: both)
  request:
    tool_name: "github__delete_file"
    arguments: {owner: "org", repo: "prod"}
  response:                             # Required when phase includes response
    content:
      - type: text
        text: "Response content"
    is_error: false
  expectations:
    decision: deny                      # allow, deny, redact
    policies:                           # Optional: expected triggering policies
      - policy_name: "deny-delete-file"
        decision: deny
    redacted_content:                   # Optional: for redact tests
      - type: text
        text: "[REDACTED]"
```

## Behavior Guidelines

1. **Unique case IDs** — every `case_id` must be unique across the entire suite
2. **Descriptive IDs** — use kebab-case with engine and phase prefix: `cel-req-001`, `ai-resp-redact-pii`
3. **Required expectations** — `expectations.decision` must always be set to `allow`, `deny`, or `redact`
4. **Phase-appropriate payloads** — provide `request` when phase includes request, `response` when it includes response
5. **Redaction content** — for `redact` tests, always include `expectations.redacted_content`
6. **Consistent tagging** — use standard tags: `cel`, `ai`, `request`, `response`, `deny`, `allow`, `redact`
7. **Balanced coverage** — write both allow and deny tests for each policy rule

## Suite Configuration (`suite.yaml`)

```yaml
version: "v1"
bundle_id: "my-test-suite"
policies:
  cel_request_rules: "../cel_request_rules.yaml"
  ai_request_rules: "../ai_request_rules.yaml"
providers:
  openai:
    api_key: "${OPENAI_API_KEY}"
acceptance:
  min_match_rate: 1.0                   # 0.0-1.0 pass rate threshold
  strict_policy_match: true             # Fail on unexpected policy triggers
execution:
  timeout_ms: 30000
  retries: 2
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
        model: gpt-5-mini
```

## Running Tests

```bash
# All tests
maybe-dont test policies --suite-dir ./tests

# CEL engine only
maybe-dont test policies --suite-dir ./tests --engine cel

# Single AI model
maybe-dont test policies --suite-dir ./tests --model openai:gpt-5-mini

# Filter by tags
maybe-dont test policies --suite-dir ./tests --tags cel,request

# Incremental with retry
maybe-dont test policies --suite-dir ./tests --incremental --retry-failed

# Validate configuration only
maybe-dont test policies --suite-dir ./tests --validate-only
```

## Exit Codes

- `0` — All tests passed, thresholds met
- `1` — Test failure (thresholds not met)
- `2` — Schema validation error
- `3` — Policy integrity error (referenced policy missing)
- `4` — Path resolution error
- `5` — More tests remain (when using `--max-tests`)
