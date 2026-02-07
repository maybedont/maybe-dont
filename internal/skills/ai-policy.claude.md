# ai-policy-authoring

## Description
Guide for authoring AI (LLM-powered) policy rules for the Maybe Don't gateway. Covers request validation, response validation, and response redaction.

## Instructions

AI policies use large language models to evaluate MCP tool calls, CLI commands, and tool responses for security risks that are difficult to express as deterministic rules. Rules are defined in YAML files with prompts that describe what to detect.

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
      ANALYZE: Does this operation involve dangerous deletion patterns?

      Look for:
      - Patterns indicating dangerous behavior
      - Attempts to access sensitive resources

      EXAMPLES:
      - Reading documentation → SAFE: Normal read operation
      - Deleting production databases → DANGEROUS: Destructive operation on production data
```

### Response Format

The AI response format is enforced automatically by the gateway engine via JSON schema (`GenerateSchema[T]()` with `strict: true`). **Do not include response format instructions in policy prompts.** The engine handles this — policy authors should focus exclusively on describing what to detect.

The engine sends the response schema as a separate API parameter, and all major providers (OpenAI, Anthropic, Google, etc.) enforce it at the decoding level. Including format instructions in prompts wastes tokens and risks conflicting with the engine's schema.

### Operation Context Injection

The engine automatically appends the operation being evaluated to the end of your prompt at runtime. **Do not include a `%s` placeholder or manually reference the operation in your prompt.** Just describe what to detect — the engine handles context injection with a context-appropriate label:

- **MCP tool calls**: Appended as `Tool call:` followed by JSON like `{"type": "mcp_tool", "name": "github__delete_repo", "arguments": {"owner": "org", "repo": "prod-db"}}`
- **CLI commands**: Appended as `CLI command:` followed by JSON like `{"type": "cli", "name": "rm", "arguments": ["-rf", "/etc"]}`
- **Response content**: Appended as `Response content:` followed by the formatted response text

### Expected AI Response Format

The engine enforces these response structures via JSON schema:

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
5. **Do not include `%s` in prompts**: The engine appends operation context automatically — prompts containing `%s` are rejected at load time
6. **Use plain-text classification labels in examples**: Write examples as `input → LABEL: reasoning` (e.g., `→ SAFE: Not a deletion operation`). Do not include JSON in examples — the response format is handled by the engine.
7. **Specify replacement text in redact rules**: Always tell the AI what placeholder to use (e.g., "replace with [PII_REDACTED]"). Without explicit replacement text, the AI may use inconsistent placeholders across calls and models. Use distinct placeholders for different redaction types (e.g., `[PII_REDACTED]`, `[PATH_REDACTED]`, `[CREDENTIAL_REDACTED]`).

### Examples

#### Request Rule: Detect mass deletion attempts

```yaml
rules:
  - name: detect-mass-deletion
    description: Detect operations that could delete large amounts of data
    action: deny
    message: "Mass deletion operations require manual approval"
    prompt: |-
      ANALYZE: Does this operation involve mass deletion risk?

      Look for:
      - Operations that delete multiple items, repos, files, or records
      - Wildcard or recursive deletion patterns
      - Operations targeting production or shared resources

      EXAMPLES:
      - Deleting all files in a directory recursively → DANGEROUS: Recursive mass deletion
      - Dropping a database table → DANGEROUS: Irreversible data loss
      - Removing all members from a team → DANGEROUS: Mass permission change
      - Deleting a single temporary file → SAFE: Single file cleanup
      - Removing one outdated branch → SAFE: Routine maintenance
```

#### Response Rule: Redact credentials from output

```yaml
rules:
  - name: redact-leaked-credentials
    description: Detect and redact credentials in tool responses
    action: redact
    message: "Credentials detected and redacted from response"
    prompt: |-
      ANALYZE: Does this tool response contain leaked credentials?

      Look for:
      - API keys, tokens, or secrets
      - Passwords or connection strings
      - Private keys or certificates
      - AWS access keys, GitHub tokens, etc.

      If credentials are found, provide a redacted version with sensitive
      values replaced by [CREDENTIAL_REDACTED].

      EXAMPLES:
      - "Connected to db on port 5432" → SAFE: No credentials
      - "API_KEY=sk-proj-abc123" → CREDENTIALS DETECTED: API key exposed
```

#### Response Rule: Deny on sensitive data (no redaction)

```yaml
rules:
  - name: block-credential-leakage
    description: Block responses containing raw credentials
    action: deny
    message: "Response blocked: contains raw credentials"
    prompt: |-
      ANALYZE: Does this tool response contain credential leakage?

      Look for:
      - Private keys (RSA, EC, PGP)
      - Database connection strings with embedded passwords
      - Cloud provider credentials (AWS, GCP, Azure)

      EXAMPLES:
      - "Server uptime: 42 days" → SAFE: No credentials
      - "-----BEGIN RSA PRIVATE KEY-----" → DANGEROUS: Private key material
```

### Common Mistakes

| Mistake | Problem | Fix |
|---------|---------|-----|
| Including `%s` in prompt | Engine rejects prompts with `%s` — context is appended automatically | Remove the `%s` placeholder; just describe what to detect |
| Overly broad prompt | High false-positive rate | Be specific about threat patterns |
| No examples in prompt | AI lacks calibration | Include EXAMPLES of both safe and dangerous operations |
| `action: redact` on request rule | Redaction only works on responses | Use `action: deny` for request rules |
| Including response format in prompt | Wastes tokens; format is enforced by the engine's JSON schema | Remove format instructions — the engine handles this automatically |
| Combining multiple concerns | Hard to tune and debug | One focused concern per rule |
| No replacement text in redact rules | AI uses inconsistent placeholders | Specify explicit replacement text (e.g., "replace with [PII_REDACTED]") |

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
