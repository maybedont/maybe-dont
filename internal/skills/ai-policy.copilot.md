# AI Policy Authoring Instructions

## Overview
AI policies use LLMs to validate MCP tool calls, CLI commands, and tool responses for security risks in the Maybe Don't gateway. Rules are defined in YAML with prompt templates that guide the AI's evaluation.

## Rule File Format

Request rules go in `ai_request_rules.yaml`, response rules in `ai_response_rules.yaml`.

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
      - Dangerous patterns to detect

      EXAMPLES of dangerous operations:
      - Example 1
      EXAMPLES of safe operations:
      - Example 1

      Respond with JSON: {"allowed": true/false, "message": "explanation"}
```

## Important Rules

- **Always** include exactly one `%s` placeholder in the prompt — it is replaced with the operation context
- Structure prompts with clear sections: ANALYZE, Look for, EXAMPLES, response format
- Include **both** safe and dangerous examples in every prompt
- Keep each rule focused on **one** concern — do not combine unrelated threat types
- `action: redact` is **only** valid for response rules
- Always end prompts with the expected JSON response format
- Use `mode: audit_only` to test rules without blocking

## Prompt Substitution

The `%s` placeholder receives:
- **MCP tool calls**: `{"type": "mcp_tool", "name": "tool_name", "arguments": {...}}`
- **CLI commands**: `{"type": "cli", "name": "command", "arguments": [...]}`
- **Responses**: Formatted text with `IsError:`, `Content:`, `Meta:` sections

## AI Response Format

Request validation:
```json
{"allowed": true, "message": "This operation is safe"}
```

Response validation with redaction:
```json
{"allowed": false, "message": "Credentials found", "redacted_content": "sanitized content"}
```

## Handling Actions

### Request Rules
- `deny` — Block if AI returns `allowed: false`, with the AI's message as the reason

### Response Rules
- `deny` — Block the response entirely
- `redact` — Replace response content with `redacted_content` from the AI response

## Examples

```yaml
# Request: Detect credential access
- name: detect-credential-access
  action: deny
  message: "Credential file access requires approval"
  prompt: |-
    ANALYZE the following operation for credential file access:

    %s

    Look for:
    - Reading .env, .credentials, .pem, .key files
    - Accessing password stores or secret managers

    EXAMPLES of dangerous: reading ~/.ssh/id_rsa, cat .env
    EXAMPLES of safe: reading README.md, listing directory

    Respond with JSON: {"allowed": true/false, "message": "explanation"}

# Response: Redact PII
- name: redact-pii
  action: redact
  message: "PII redacted from response"
  prompt: |-
    ANALYZE the following tool response for PII:

    %s

    Look for: email addresses, phone numbers, SSNs, credit card numbers

    If found, provide sanitized version with [REDACTED] replacements.

    Respond with JSON: {"allowed": true/false, "message": "explanation", "redacted_content": "sanitized if needed"}
```
