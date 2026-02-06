# Policy Test Case Authoring Instructions

## Overview
Write test cases to validate CEL and AI policy rules in the Maybe Don't gateway. Tests are organized in a suite directory containing `suite.yaml` and a `cases/` subdirectory with test case YAML files.

## Test Case Format

```yaml
- case_id: "cel-req-001"               # Required: unique across suite
  title: "Block github__delete_file"    # Required: human-readable
  tags: [cel, request, github]          # Optional: for filtering
  notes: ["Based on incident 2024-12"]  # Optional: documentation
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
    policies:                           # Optional: specific policies expected
      - policy_name: "deny-delete-file"
        decision: deny
    redacted_content:                   # Optional: for redact tests
      - type: text
        text: "[REDACTED]"
```

## Important Rules

- **Every** `case_id` must be unique across the entire suite
- Use descriptive kebab-case IDs: `cel-req-deny-delete`, `ai-resp-redact-pii`
- `expectations.decision` is **required** — must be `allow`, `deny`, or `redact`
- `request` is required when `phase` includes `request`; `response` when it includes `response`
- For redact tests, **always** provide `expectations.redacted_content`
- Tag cases consistently: `cel`, `ai`, `request`, `response`, `deny`, `allow`, `redact`
- Write **both** allow and deny tests for each policy rule

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
  min_match_rate: 1.0
  strict_policy_match: true
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
        model: gpt-4o-mini
```

## Running Tests

```bash
# All tests
maybe-dont test policies --suite-dir ./tests

# CEL engine only
maybe-dont test policies --suite-dir ./tests --engine cel

# Single AI model
maybe-dont test policies --suite-dir ./tests --model openai:gpt-4o-mini

# All models in matrix
maybe-dont test policies --suite-dir ./tests --matrix

# Filter by tags
maybe-dont test policies --suite-dir ./tests --tags cel,request

# Incremental with retry
maybe-dont test policies --suite-dir ./tests --incremental --retry-failed

# Validate without running
maybe-dont test policies --suite-dir ./tests --validate-only
```

## Handling Test Failures

When tests fail:
1. Check exit code to understand failure type (1=threshold, 2=schema, 3=policy, 4=path)
2. Review the failure messages in output for specific assertion mismatches
3. For AI tests, consider that LLM responses may vary — use `--retry-failed` for transient issues
4. For policy match failures with `strict_policy_match: true`, verify no unexpected policies triggered

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | All passed |
| 1 | Test failure (thresholds not met) |
| 2 | Schema validation error |
| 3 | Policy integrity error |
| 4 | Path resolution error |
| 5 | More tests remain (`--max-tests`) |
