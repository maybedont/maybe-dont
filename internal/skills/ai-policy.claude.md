# ai-policy-authoring

## Description
Guide for authoring AI (LLM-powered) policy rules for Maybe Don't. Covers request validation, response validation, and response redaction.

## Instructions

AI policies use large language models to evaluate tool calls, CLI commands, and tool responses for security risks that are difficult to express as deterministic rules. Rules are defined in YAML files with prompts that describe what to detect.

### Rule YAML Structure

Rules are stored in separate files referenced by the server configuration:
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

### Writing Effective Prompts

The prompt is the core of each AI policy rule. Focus on clearly describing the security concern and providing calibration examples.

1. **Describe the threat clearly**: Start with an ANALYZE section that frames the specific security concern
2. **List specific patterns to detect**: Use a "Look for" section naming the exact behaviors or patterns that indicate risk
3. **Include calibration examples**: Provide EXAMPLES of both safe and dangerous operations so the AI can distinguish routine usage from genuine threats
4. **Use plain-text classification labels**: Write examples as `input → LABEL: reasoning` (e.g., `→ SAFE: Routine cleanup`)
5. **Keep each rule focused on one concern**: A rule about credential detection should not also check for data exfiltration — create separate rules for separate concerns
6. **Specify replacement text in redact rules**: Tell the AI what placeholder to use (e.g., "replace with [PII_REDACTED]") and use distinct placeholders for different redaction types (e.g., `[PII_REDACTED]`, `[CREDENTIAL_REDACTED]`)
7. **Test with `mode: audit_only`**: Start in audit mode to observe rule behavior, then remove the mode to enable blocking

### Operation Context

The policy engine automatically appends the operation being evaluated to the end of your prompt at runtime. The context is labeled appropriately:

- **Tool calls**: Appended as `Tool call:` followed by JSON like `{"type": "mcp_tool", "name": "github__delete_repo", "arguments": {"owner": "org", "repo": "prod-db"}}`
- **CLI commands**: Appended as `CLI command:` followed by JSON like `{"type": "cli", "name": "rm", "arguments": ["-rf", "/etc"]}`
- **Response content**: Appended as `Response content:` followed by the formatted response text

### Actions

| Action  | Request Rules | Response Rules | Behavior |
|---------|:---:|:---:|----------|
| `deny`  | Yes | Yes | Block if AI returns `allowed: false` |
| `redact`| No  | Yes | Replace content with `redacted_content` from AI response |

### AI Response Format

The AI response format is enforced automatically at runtime. The server uses these structures:

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
| Overly broad prompt | High false-positive rate | Be specific about threat patterns |
| No examples in prompt | AI lacks calibration | Include EXAMPLES of both safe and dangerous operations |
| `action: redact` on request rule | Redaction only works on responses | Use `action: deny` for request rules |
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

- AI rules run in parallel with per-rule timeout (`max_rule_evaluation_ms`, default: 45s)
- Total blocking budget across all validation phases is `max_blocking_ms` (default: 90s)
- When all rules are `audit_only`, request returns immediately (fail-open) and audit continues async
- First enabled `deny` match short-circuits evaluation
- AI errors fail open (allow the request) but log at ERROR level
- Prompt quality directly impacts accuracy — invest time in clear, well-structured prompts
