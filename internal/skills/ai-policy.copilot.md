# AI Policy Authoring Instructions

## Overview
AI policies use LLMs to validate tool calls, CLI commands, and tool responses for security risks in Maybe Don't. Rules are defined in YAML with prompts that describe what to detect.

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
      ANALYZE: Does this operation involve dangerous patterns?

      Look for:
      - Dangerous patterns to detect

      EXAMPLES:
      - Safe operation example → SAFE: Explanation
      - Dangerous operation example → DANGEROUS: Explanation
```

## Writing Effective Prompts

- Describe the specific security concern in an ANALYZE section
- List the exact patterns to detect in a "Look for" section
- Include **both** safe and dangerous examples to calibrate the AI's judgment
- Use plain-text classification labels in examples (`→ SAFE:` / `→ DANGEROUS:`)
- Keep each rule focused on **one** concern — separate rules for separate threats
- For redact rules, specify replacement text (e.g., "replace with [PII_REDACTED]")
- Use `mode: audit_only` to test rules without blocking, then remove it when confident
- `action: redact` is **only** valid for response rules

## Operation Context

The policy engine appends operation context to your prompt automatically with a context-appropriate label:
- **Tool calls**: `Tool call:` + JSON `{"type": "mcp_tool", "name": "tool_name", "arguments": {...}}`
- **CLI commands**: `CLI command:` + JSON `{"type": "cli", "name": "command", "arguments": [...]}`
- **Responses**: `Response content:` + formatted text with `IsError:`, `Content:`, `Meta:` sections

## AI Response Format

The AI response format is enforced automatically at runtime. The server uses these structures:

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
    ANALYZE: Does this operation access credential files?

    Look for:
    - Reading .env, .credentials, .pem, .key files
    - Accessing password stores or secret managers

    EXAMPLES:
    - Reading ~/.ssh/id_rsa → DANGEROUS: SSH private key access
    - cat .env → DANGEROUS: Environment file with secrets
    - Reading README.md → SAFE: Documentation file
    - Listing directory → SAFE: Non-sensitive operation

# Response: Redact PII
- name: redact-pii
  action: redact
  message: "PII redacted from response"
  prompt: |-
    ANALYZE: Does this tool response contain PII?

    Look for: email addresses, phone numbers, SSNs, credit card numbers

    If found, provide sanitized version with sensitive data replaced by [PII_REDACTED].

    EXAMPLES:
    - "Contact john@example.com" → PII DETECTED: Email address found
    - "Server returned 200 OK" → SAFE: No PII detected
```
