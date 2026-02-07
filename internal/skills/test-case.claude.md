# policy-test-case

## Description
Guide for writing policy test cases and configuring test suites for Maybe Don't. Covers suite.yaml configuration, test case YAML structure, and the test runner CLI.

## Instructions

The policy test framework validates CEL and AI policy rules by running structured test cases against them. Tests are organized in a suite directory with a `suite.yaml` configuration and test case files in a `cases/` subdirectory.

### Suite Configuration (`suite.yaml`)

```yaml
version: "v1"                          # Required: schema version
bundle_id: "my-test-suite"             # Required: unique suite identifier
description: "Test suite for X"        # Optional

# Policy file paths (relative to suite directory, at least one required)
policies:
  cel_request_rules: "../cel_request_rules.yaml"
  ai_request_rules: "../ai_request_rules.yaml"
  cel_response_rules: "../cel_response_rules.yaml"
  ai_response_rules: "../ai_response_rules.yaml"

# Provider-level API keys (shared across models)
providers:
  openai:
    api_key: "${OPENAI_API_KEY}"        # Supports env var substitution
    endpoint: "https://api.openai.com/v1/chat/completions"  # Optional override
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"

# Acceptance thresholds
acceptance:
  min_match_rate: 1.0                   # 0.0-1.0, fraction of tests that must pass
  strict_policy_match: true             # Fail if unexpected policies trigger

# Execution settings
execution:
  timeout_ms: 30000                     # Per-test timeout (default: 30000)
  retries: 2                            # Retry attempts on failure
  retry_delay_ms: 1000                  # Delay between retries
  delay_between_requests_ms: 100        # Min delay between consecutive requests
  rate_limit_buffer_ms: 5000            # Extra buffer near rate limit window
  rate_limits:                          # Per-provider rate limiting
    openai:
      requests_per_minute: 60
    anthropic:
      requests_per_minute: 30

# Engine configuration
engines:
  cel:
    enabled: true                       # Enable CEL engine testing
  ai:
    enabled: true                       # Enable AI engine testing
    model_matrix:                       # Models to test against
      - provider: openai                # openai, anthropic, openai_compatible
        model: gpt-5-mini
        enabled: true                   # Default: true
        parameters:                     # Optional provider-specific params
          temperature: 0.7
      - provider: anthropic
        model: claude-sonnet-4-20250514
        api_key: "${ANTHROPIC_API_KEY}" # Optional model-level override

# Default test case filters
filters:
  tags: []                              # Include only cases with ALL these tags
  exclude_tags: []                      # Exclude cases with ANY of these tags
  case_pattern: ""                      # Glob pattern for case IDs
```

### Test Case YAML Structure

Test case files live in the `cases/` subdirectory. Each file contains either a single test case (YAML map) or multiple test cases (YAML array).

```yaml
- case_id: "cel-req-001"               # Required: unique across the suite
  title: "Block github__delete_file"    # Required: human-readable title
  tags:                                 # Optional: for filtering
    - cel
    - request
    - github
  notes:                                # Optional: document the scenario
    - "Tests exact tool name matching against deny rule"
    - "Based on real-world incident 2024-12-15"

  phase: request                        # request, response, both (default: request)
  engine: cel                           # cel, ai, both (default: both)

  request:                              # Required for request-phase tests
    tool_name: "github__delete_file"    # MCP tool name (prefixed)
    arguments:                          # Tool arguments (can be empty {})
      owner: "myorg"
      repo: "myrepo"
      path: "README.md"

  response:                             # Required when phase includes response
    content:
      - type: text                      # Currently only "text" supported
        text: "File deleted successfully"
    is_error: false

  expectations:
    decision: deny                      # Required: allow, deny, or redact
    policies:                           # Optional: specific policy expectations
      - policy_name: "deny-github-delete-file"
        decision: deny
    redacted_content:                   # Optional: expected output for redact tests
      - type: text
        text: "Expected redacted string"
```

### Test Case Fields Reference

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `case_id` | Yes | — | Unique identifier (use descriptive kebab-case) |
| `title` | Yes | — | Human-readable test description |
| `tags` | No | `[]` | Tags for filtering with `--tags`/`--exclude-tags` |
| `notes` | No | `[]` | Documentation strings for the test case |
| `phase` | No | `request` | `request`, `response`, or `both` |
| `engine` | No | `both` | `cel`, `ai`, or `both` |
| `request` | Yes* | — | Request payload (*required when phase includes request) |
| `response` | Yes* | — | Response payload (*required when phase includes response) |
| `expectations.decision` | Yes | — | Expected outcome: `allow`, `deny`, `redact` |
| `expectations.policies` | No | `[]` | Expected triggering policies |
| `expectations.redacted_content` | No | — | Expected content after redaction |

### Running Tests

```bash
maybe-dont test policies --suite-dir <path> [options]
```

#### Engine and Model Selection
```bash
--engine {cel|ai|all}         # Override engine from suite.yaml
--model provider:model        # Run single model (e.g., openai:gpt-5-mini)
--matrix                      # Run all enabled models from suite.yaml
```

#### Filtering
```bash
--tags cel,request            # Run cases with ALL specified tags
--exclude-tags slow,flaky     # Skip cases with ANY specified tags
--case-pattern "cel-req-*"    # Glob pattern for case IDs (comma-separated)
```

#### Incremental Execution
```bash
--incremental                 # Skip unchanged tests, load/save state
--full                        # Run all tests but save state
--retry-failed                # Re-run failed/errored tests even if cached
--state-file <path>           # Custom state file location
--wait                        # Run continuously until all tests complete
```

#### Execution Control
```bash
--validate-only               # Validate suite without running tests
--include-disabled            # Include disabled policies
--timeout <ms>                # Override per-test timeout
--requests-per-minute <n>     # Override all provider rate limits
--max-tests <n>               # Limit tests per model (exit code 5 if more remain)
```

#### Output
```bash
--output <file>               # Write results to file
--format {json|junit}         # Output format (default: json)
--quiet, -q                   # Suppress stdout
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | All tests passed, thresholds met |
| 1 | Test failure (thresholds not met) |
| 2 | Schema validation error (suite.yaml or test case format) |
| 3 | Policy integrity error (referenced policy doesn't exist) |
| 4 | Path resolution error (file/directory not found) |
| 5 | More tests remain (when using `--max-tests`) |

### Examples

#### Deny test case (CEL request)

```yaml
- case_id: "cel-req-deny-delete-file"
  title: "CEL denies github__delete_file"
  tags: [cel, request, github, deny]
  engine: cel
  phase: request
  request:
    tool_name: "github__delete_file"
    arguments:
      owner: "myorg"
      repo: "production"
      path: "config.yaml"
  expectations:
    decision: deny
    policies:
      - policy_name: "deny-github-delete-file"
        decision: deny
```

#### Allow test case (AI request)

```yaml
- case_id: "ai-req-allow-read-docs"
  title: "AI allows reading documentation"
  tags: [ai, request, safe]
  engine: ai
  phase: request
  request:
    tool_name: "github__get_file_contents"
    arguments:
      owner: "myorg"
      repo: "docs"
      path: "README.md"
  expectations:
    decision: allow
```

#### Redact test case (CEL response)

```yaml
- case_id: "cel-resp-redact-passwd"
  title: "CEL redacts /etc/passwd content"
  tags: [cel, response, redact]
  engine: cel
  phase: response
  request:
    tool_name: "server__read_file"
    arguments:
      path: "/etc/passwd"
  response:
    content:
      - type: text
        text: "root:x:0:0:root:/root:/bin/bash"
    is_error: false
  expectations:
    decision: redact
    policies:
      - policy_name: "redact-etc-passwd"
        decision: redact
    redacted_content:
      - type: text
        text: "[REDACTED]"
```

### Conventions

- **Case IDs**: Use descriptive kebab-case with engine and phase prefix: `cel-req-001`, `ai-resp-redact-pii`
- **Tags**: Use consistent tags for filtering: `cel`, `ai`, `request`, `response`, `deny`, `allow`, `redact`, plus domain tags like `github`, `aws`
- **One file per logical group**: Group related test cases in a single file (e.g., all GitHub delete tests)
- **Notes for context**: Use `notes` to document the real-world scenario or incident that motivated the test
- **Test both allow and deny**: For each policy rule, write at least one test that should trigger it and one that should not
