# AI Policy Authoring Instructions

## Purpose
Author AI (LLM-powered) policy rules for Maybe Don't. AI policies evaluate tool calls, CLI commands, and tool responses using large language models to detect security risks that are difficult to express as deterministic rules.

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

## Writing Effective Prompts

1. **Describe the threat clearly** in an ANALYZE section that frames the specific security concern
2. **List specific patterns to detect** in a "Look for" section naming the exact behaviors that indicate risk
3. **Include calibration examples** of both safe and dangerous operations to help the AI distinguish routine usage from genuine threats
4. **Use plain-text classification labels** — write as `→ SAFE:` / `→ DANGEROUS:`
5. **One concern per rule** — keep rules focused; separate credential detection from data exfiltration
6. **Specify replacement text in redact rules** — e.g., "replace with [PII_REDACTED]"
7. **Test with `mode: audit_only`** first, then remove it to enable blocking
8. **`action: redact`** is only valid for response rules

## Operation Context

The policy engine appends operation context to your prompt automatically with a context-appropriate label:
- **Tool calls**: `Tool call:` + JSON `{"type": "mcp_tool", "name": "...", "arguments": {...}}`
- **CLI commands**: `CLI command:` + JSON `{"type": "cli", "name": "...", "arguments": [...]}`
- **Responses**: `Response content:` + formatted text with `IsError:`, `Content:`, `Meta:` sections

## AI Response Format

The AI response format is enforced automatically at runtime. The server uses these structures:

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
