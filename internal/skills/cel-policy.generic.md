# CEL Policy Authoring Instructions

## Purpose
Author deterministic CEL (Common Expression Language) policy rules for the Maybe Don't gateway. CEL policies validate MCP tool calls, CLI commands, and tool responses using fast, compiled expressions.

## Rule File Structure

Request rules: `cel_request_rules.yaml`
Response rules: `cel_response_rules.yaml`

```yaml
rules:
  - name: rule-name
    description: "What this rule does"
    mcp_expression: |                   # For MCP tool calls
      tool.name == "github__delete_repo"
    cli_expression: |                   # For CLI commands (optional)
      cli.command == "gh" && cli.arguments[1] == "delete"
    action: deny                        # allow, deny, redact (response only)
    message: "Explanation shown to user"
    mode: audit_only                    # Optional: log without blocking
    redaction_pattern: "regex"          # Response redact rules only
    redaction_replacement: "[REDACTED]" # Response redact rules only
```

## Behavior Guidelines

1. **Use `mcp_expression`** for MCP tool call rules (preferred over legacy `expression` field)
2. **Use `cli_expression`** for CLI command rules — CLI and MCP have different context variables
3. **Expressions must return boolean** — non-boolean results cause fail-open behavior
4. **Guard field access** with `has(obj, field)` before accessing optional fields
5. **Use Go regexp syntax** for `redaction_pattern` and `.matches()` — PCRE features are not supported
6. **`action: redact`** is only valid for response rules
7. **Test with `mode: audit_only`** first, then remove it to enable blocking
8. **Response `deny`** means "don't show the response to the AI agent" — only use for read-only operations (get, list). For mutating operations (create, modify, delete), the action already completed; denying the response hides the outcome without undoing it
9. **Response `redact`** means "don't show parts of the response to the AI agent" — generally preferred over `deny` for response rules

## Context Variables

### MCP Tool Calls (`mcp_expression`)
- `tool.name` — Prefixed tool name (e.g., `"github__delete_repo"`)
- `tool.arguments` — Arguments map
- `request.method` — MCP method
- `request.params.meta` — Request metadata

### CLI Commands (`cli_expression`)
- `cli.command` — Command name (e.g., `"rm"`, `"gh"`)
- `cli.arguments` — Argument list (`[]string`)
- `cli.working_directory` — Working directory
- `cli.client_info.hostname`, `.username`, `.os`, `.arch`, `.shell`

### Response Rules (`expression`)
- `response.content` — List with `.type`, `.text`, `.data`, `.mimeType` fields
- `response.isError` — Error flag
- `request.params.name` — Original tool name

## Available Functions
- `has(obj, field)` — Check field existence
- `get(obj, field, default)` — Get with fallback value
- `.contains(s)`, `.startsWith(s)`, `.endsWith(s)` — String checks
- `.matches(regex)` — Regex match (Go syntax)
- `.size()` — Length of list or map
- `.exists(x, expr)`, `.all(x, expr)` — Collection predicates
- `x in [a, b, c]` — Membership test

## Examples

```yaml
# Deny destructive operations (MCP + CLI)
- name: no-destructive-github
  mcp_expression: |
    tool.name in ["github__delete_repo", "github__delete_file"]
  cli_expression: |
    cli.command == "gh" && cli.arguments.exists(a, a == "delete")
  action: deny
  message: "Destructive GitHub operations not permitted"

# Redact API keys from tool responses
- name: redact-api-keys
  expression: |
    size(response.content) > 0 &&
    response.content[0].type == "text" &&
    response.content[0].text.matches("(?i)api[_-]?key\\s*[:=]\\s*\\S+")
  action: redact
  redaction_pattern: "(?i)(api[_-]?key\\s*[:=]\\s*)\\S+"
  redaction_replacement: "${1}[REDACTED]"
```
