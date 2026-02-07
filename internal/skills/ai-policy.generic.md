# AI Policy Authoring Instructions

## Purpose
Author AI (LLM-powered) policy rules for the Maybe Don't gateway. AI policies evaluate MCP tool calls, CLI commands, and tool responses using large language models to detect security risks that are difficult to express as deterministic rules.

## Rule File Structure

Request rules: `ai_request_rules.yaml`
Response rules: `ai_response_rules.yaml`

```yaml
rules:
  - name: rule-name
    description: "What this rule detects"
    action: deny                        # deny (request + response), redact (response only)
    message: "User-facing message"
    mode: audit_only                    # Optional: log without blocking
    prompt: |-
      ANALYZE: Does this operation involve dangerous patterns?

      Look for:
      - Specific dangerous patterns

      EXAMPLES:
      - Safe operation → SAFE: Explanation
      - Dangerous operation → DANGEROUS: Explanation
```

## Response Format

The AI response format is enforced automatically by the gateway engine via JSON schema (`GenerateSchema[T]()` with `strict: true`). **Do not include response format instructions in policy prompts.** The engine handles this — policy authors should focus exclusively on describing what to detect.

## Behavior Guidelines

1. **The engine appends operation context automatically** — do not include `%s` in prompts (prompts containing `%s` are rejected at load time)
2. **Structure prompts clearly** with ANALYZE, Look for, EXAMPLES sections
3. **Include both safe and dangerous examples** to calibrate the AI's judgment
4. **One concern per rule** — do not combine unrelated threat types in a single prompt
5. **`action: redact`** is only valid for response rules
6. **Use plain-text classification labels in examples** — write as `→ SAFE:` / `→ DANGEROUS:`, not JSON
7. **Test with `mode: audit_only`** first, then remove it to enable blocking
8. **Specify replacement text in redact rules** — e.g., "replace with [PII_REDACTED]"

## Operation Context

The engine appends operation context to your prompt automatically with a context-appropriate label:
- **MCP tool calls**: `Tool call:` + JSON `{"type": "mcp_tool", "name": "...", "arguments": {...}}`
- **CLI commands**: `CLI command:` + JSON `{"type": "cli", "name": "...", "arguments": [...]}`
- **Responses**: `Response content:` + formatted text with `IsError:`, `Content:`, `Meta:` sections

## AI Response Format

The engine enforces these structures via JSON schema:

Request validation:
```json
{"allowed": true/false, "message": "explanation"}
```

Response validation (with optional redaction):
```json
{"allowed": true/false, "message": "explanation", "redacted_content": "sanitized content"}
```

## Actions
- `deny` — Block if AI returns `allowed: false` (request and response rules)
- `redact` — Replace content with AI-provided `redacted_content` (response rules only)

## Examples

```yaml
# Request: Block mass deletion attempts
- name: detect-mass-deletion
  action: deny
  message: "Mass deletion requires manual approval"
  prompt: |-
    ANALYZE: Does this operation involve mass deletion risk?

    Look for:
    - Recursive or wildcard deletion patterns
    - Operations targeting shared or production resources

    EXAMPLES:
    - Deleting all files recursively → DANGEROUS: Mass file deletion
    - Dropping database tables → DANGEROUS: Irreversible data loss
    - Removing a single temp file → SAFE: Routine cleanup
    - Cleaning up personal drafts → SAFE: Non-destructive operation

# Response: Redact leaked credentials
- name: redact-credentials
  action: redact
  message: "Credentials redacted from response"
  prompt: |-
    ANALYZE: Does this tool response contain leaked credentials?

    Look for: API keys, tokens, passwords, private keys, connection strings

    If found, provide sanitized version with sensitive values replaced by [CREDENTIAL_REDACTED].

    EXAMPLES:
    - "Connected to db on port 5432" → SAFE: No credentials
    - "API_KEY=sk-proj-abc123" → CREDENTIALS DETECTED: API key exposed
```
