# ai-policy-authoring

## Description
Guide for authoring AI (LLM-powered) policy rules for the Maybe Don't gateway. Covers request validation, response validation, and response redaction.

## Instructions

AI policies use large language models to evaluate MCP tool calls, CLI commands, and tool responses for security risks that are difficult to express as deterministic rules. Rules are defined in YAML files with prompt templates.

### Rule YAML Structure

Rules are stored in separate files referenced by the gateway configuration:
- **Request rules**: `ai_request_rules.yaml` (when `request_validation.ai.enabled: true`)
- **Response rules**: `ai_response_rules.yaml` (when `response_validation.ai.enabled: true`)

```yaml
rules:
  - name: rule-name                     # Unique identifier (kebab-case)
    description: "What this rule detects" # Human-readable purpose
    enabled: true                       # Default: true
    action: deny                        # deny (request + response), redact (response only)
    message: "Explanation shown to user"

    # Optional: audit_only logs but never blocks
    mode: audit_only

    prompt: |-
      ANALYZE the following operation for security risks:

      %s

      Look for:
      - Patterns indicating dangerous behavior
      - Attempts to access sensitive resources

      EXAMPLES of dangerous operations:
      - Deleting production databases
      - Accessing credential files

      EXAMPLES of safe operations:
      - Reading documentation
      - Listing directory contents

      Respond with JSON: {"allowed": true/false, "message": "explanation"}
```

### Prompt Template

The `%s` placeholder in the prompt is replaced with the operation being validated:

**For request validation (MCP tool calls):**
```json
{"type": "mcp_tool", "name": "github__delete_repo", "arguments": {"owner": "org", "repo": "prod-db"}}
```

**For request validation (CLI commands):**
```json
{"type": "cli", "name": "rm", "arguments": ["-rf", "/etc"]}
```

**For response validation:**
```
IsError: false
Content:
  [text] File contents: root:x:0:0:root:/root:/bin/bash...
```

### Expected AI Response Format

**Request validation:**
```json
{
  "allowed": true,
  "message": "This operation is safe because..."
}
```

**Response validation (deny or redact):**
```json
{
  "allowed": false,
  "message": "Response contains credentials",
  "redacted_content": "Connection established using [CREDENTIALS REDACTED]"
}
```

The `redacted_content` field is only used when `action: redact` and the AI determines content should be sanitized.

### Actions

| Action  | Request Rules | Response Rules | Behavior |
|---------|:---:|:---:|----------|
| `deny`  | Yes | Yes | Block if AI returns `allowed: false` |
| `redact`| No  | Yes | Replace content with `redacted_content` from AI response |

### Prompt Engineering Best Practices

1. **Structure prompts clearly**: Use sections like ANALYZE, Look for, EXAMPLES
2. **Include both positive and negative examples**: Show what should be allowed AND denied
3. **Be specific about the threat model**: Name the exact patterns to detect
4. **Keep prompts focused**: One rule per concern (don't combine credential detection with data exfiltration)
5. **Use the `%s` placeholder exactly once**: It must appear in every prompt
6. **Specify the response format**: Always end with the expected JSON format

### Examples

#### Request Rule: Detect mass deletion attempts

```yaml
rules:
  - name: detect-mass-deletion
    description: Detect operations that could delete large amounts of data
    action: deny
    message: "Mass deletion operations require manual approval"
    prompt: |-
      ANALYZE the following operation for mass deletion risk:

      %s

      Look for:
      - Operations that delete multiple items, repos, files, or records
      - Wildcard or recursive deletion patterns
      - Operations targeting production or shared resources

      EXAMPLES of dangerous operations:
      - Deleting all files in a directory recursively
      - Dropping a database table
      - Removing all members from a team

      EXAMPLES of safe operations:
      - Deleting a single temporary file
      - Removing one outdated branch
      - Cleaning up a personal draft

      Respond with JSON: {"allowed": true/false, "message": "brief explanation"}
```

#### Response Rule: Redact credentials from output

```yaml
rules:
  - name: redact-leaked-credentials
    description: Detect and redact credentials in tool responses
    action: redact
    message: "Credentials detected and redacted from response"
    prompt: |-
      ANALYZE the following tool response for leaked credentials:

      %s

      Look for:
      - API keys, tokens, or secrets
      - Passwords or connection strings
      - Private keys or certificates
      - AWS access keys, GitHub tokens, etc.

      If credentials are found, provide a redacted version with sensitive
      values replaced by [REDACTED].

      If no credentials are found, mark as allowed.

      Respond with JSON:
      {"allowed": true/false, "message": "explanation", "redacted_content": "sanitized version if needed"}
```

#### Response Rule: Deny on sensitive data (no redaction)

```yaml
rules:
  - name: block-credential-leakage
    description: Block responses containing raw credentials
    action: deny
    message: "Response blocked: contains raw credentials"
    prompt: |-
      ANALYZE the following tool response for credential leakage:

      %s

      Look for:
      - Private keys (RSA, EC, PGP)
      - Database connection strings with embedded passwords
      - Cloud provider credentials (AWS, GCP, Azure)

      Respond with JSON: {"allowed": true/false, "message": "explanation"}
```

### Common Mistakes

| Mistake | Problem | Fix |
|---------|---------|-----|
| Missing `%s` placeholder | AI receives no operation context | Include exactly one `%s` in the prompt |
| Overly broad prompt | High false-positive rate | Be specific about threat patterns |
| No examples in prompt | AI lacks calibration | Include EXAMPLES of both safe and dangerous operations |
| `action: redact` on request rule | Redaction only works on responses | Use `action: deny` for request rules |
| Vague response format | AI returns unparseable responses | Always specify the exact JSON format expected |
| Combining multiple concerns | Hard to tune and debug | One focused concern per rule |

### Configuration

AI policies share the centralized AI configuration:

```yaml
validation:
  ai:
    provider: openai
    model: gpt-5-mini
    endpoint: "https://api.openai.com/v1/chat/completions"
    api_key: "${OPENAI_API_KEY}"
```

### Key Notes

- AI rules run in parallel goroutines with per-rule timeout (`max_rule_evaluation_ms`, default: 45s)
- Total blocking budget across all validation phases is `max_blocking_ms` (default: 90s)
- When all rules are `audit_only`, request returns immediately (fail-open) and audit continues async
- First enabled `deny` match short-circuits evaluation
- AI errors fail open (allow the request) but log at ERROR level
- Prompt quality directly impacts accuracy — invest time in clear, well-structured prompts
