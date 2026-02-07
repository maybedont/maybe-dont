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
      ANALYZE the following operation:

      %s

      Look for:
      - Specific dangerous patterns

      EXAMPLES of dangerous operations:
      - Example 1
      EXAMPLES of safe operations:
      - Example 1

      Respond with JSON: {"allowed": true/false, "message": "explanation"}
```

## Behavior Guidelines

1. **Include exactly one `%s` placeholder** in every prompt — it is replaced with the operation context
2. **Structure prompts clearly** with ANALYZE, Look for, EXAMPLES, and response format sections
3. **Include both safe and dangerous examples** to calibrate the AI's judgment
4. **One concern per rule** — do not combine unrelated threat types in a single prompt
5. **`action: redact`** is only valid for response rules
6. **Always specify the JSON format** the AI should return
7. **Test with `mode: audit_only`** first, then remove it to enable blocking

## Prompt Substitution

The `%s` placeholder receives:
- **MCP tool calls**: JSON `{"type": "mcp_tool", "name": "...", "arguments": {...}}`
- **CLI commands**: JSON `{"type": "cli", "name": "...", "arguments": [...]}`
- **Responses**: Formatted text with `IsError:`, `Content:`, `Meta:` sections

## AI Response Format

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

## Writing Response Rules

**Performance**: AI response rules can be slow because the tool or CLI response content is not known ahead of time and may be large, requiring the LLM to process significant payloads.

**Understanding response actions:**
- `deny` means "don't show the response to the AI agent." Use sparingly — only meaningful for read-only operations (get, list) where withholding the result makes sense.
- `redact` means "don't show parts of the response to the AI agent." Generally preferred over `deny`.
- Avoid `deny` on mutating operations (create, modify, delete): the action has already completed, so hiding the result is misleading — the agent won't know the operation succeeded.

## Examples

```yaml
# Request: Block mass deletion attempts
- name: detect-mass-deletion
  action: deny
  message: "Mass deletion requires manual approval"
  prompt: |-
    ANALYZE the following operation for mass deletion risk:

    %s

    Look for:
    - Recursive or wildcard deletion patterns
    - Operations targeting shared or production resources

    EXAMPLES of dangerous: deleting all files recursively, dropping tables
    EXAMPLES of safe: removing a single temp file, cleaning drafts

    Respond with JSON: {"allowed": true/false, "message": "explanation"}

# Response: Redact leaked credentials
- name: redact-credentials
  action: redact
  message: "Credentials redacted from response"
  prompt: |-
    ANALYZE the following tool response for leaked credentials:

    %s

    Look for: API keys, tokens, passwords, private keys, connection strings

    If found, provide sanitized version with [REDACTED] replacements.

    Respond with JSON: {"allowed": true/false, "message": "explanation", "redacted_content": "sanitized if needed"}
```
