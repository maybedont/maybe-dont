# CEL Policy Authoring Instructions

## Overview
CEL policies provide deterministic, fast validation for MCP tool calls, CLI commands, and tool responses in the Maybe Don't gateway. Rules are defined in YAML and evaluated using Google's Common Expression Language (CEL).

## Rule File Format

Request rules go in `cel_request_rules.yaml`, response rules in `cel_response_rules.yaml`.

```yaml
rules:
  - name: rule-name
    description: "Purpose of this rule"
    mcp_expression: |                   # For MCP tool calls
      tool.name == "github__delete_repo"
    cli_expression: |                   # For CLI commands (optional)
      cli.command == "gh" && cli.arguments[1] == "delete"
    action: deny                        # allow, deny, redact (response only)
    message: "User-facing denial message"
    mode: audit_only                    # Optional: log without blocking
    redaction_pattern: "regex"          # Response redact rules only
    redaction_replacement: "[REDACTED]" # Response redact rules only
```

## Important Rules

- **Always** use `mcp_expression` for MCP tool call rules (not the legacy `expression` field)
- Use `cli_expression` for CLI command rules with different context variables
- Expressions **must** return a boolean — non-boolean results cause fail-open
- Use `has(obj, field)` before accessing optional fields to prevent nil errors
- `action: redact` is **only** valid for response rules
- Use Go `regexp` syntax (not PCRE) for patterns and `.matches()`
- Test rules with `mode: audit_only` first, then remove it to enable blocking

## Context Variables

### MCP Request (`mcp_expression`)
- `tool.name` — Prefixed tool name (e.g., `"github__delete_repo"`)
- `tool.arguments` — Tool arguments as map
- `request.method` — MCP method (e.g., `"tools/call"`)

### CLI (`cli_expression`)
- `cli.command` — Command name (e.g., `"gh"`, `"kubectl"`)
- `cli.arguments` — Argument list (`[]string`)
- `cli.working_directory` — Current working directory
- `cli.client_info.hostname`, `.username`, `.os`, `.arch`, `.shell`

### Response (`expression`)
- `response.content` — List of content items with `.type`, `.text`, `.data`, `.mimeType`
- `response.isError` — Boolean error flag
- `request.params.name` — Original tool name

## Handling Actions

### Request Rules
- `allow` — Permit the request
- `deny` — Block the request with message

### Response Rules
- `allow` — Pass through response unchanged
- `deny` — Don't show the response to the AI agent. Use sparingly — only meaningful for **read-only** operations (get, list) where withholding the result makes sense. For mutating operations (create, modify, delete), the action has already completed; denying the response hides the outcome without undoing it.
- `redact` — Don't show parts of the response to the AI agent. Replace matched content using `redaction_pattern` and `redaction_replacement`. Generally preferred over `deny` for response rules.

## Examples

```yaml
# Block mass email operations
- name: no-mass-email
  mcp_expression: |
    tool.name == "email__send" &&
    has(tool.arguments, "recipients") &&
    tool.arguments.recipients.size() > 10
  action: deny
  message: "Sending to more than 10 recipients requires approval"

# Redact sensitive data from responses
- name: redact-passwords
  expression: |
    size(response.content) > 0 &&
    response.content[0].type == "text" &&
    response.content[0].text.matches("(?i)password\\s*[:=]\\s*\\S+")
  action: redact
  redaction_pattern: "(?i)(password\\s*[:=]\\s*)\\S+"
  redaction_replacement: "${1}[REDACTED]"
```
