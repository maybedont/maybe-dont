# cel-policy-authoring

## Description
Guide for authoring CEL (Common Expression Language) deterministic policy rules for Maybe Don't. Covers both request and response validation rules.

## Instructions

CEL policies provide fast, deterministic validation of MCP tool calls, CLI commands, and tool responses. Rules are defined in YAML files and evaluated using Google's Common Expression Language.

### Rule YAML Structure

Rules are stored in separate files referenced by the server configuration:
- **Request rules**: `cel_request_rules.yaml` (when `request_validation.cel.enabled: true`)
- **Response rules**: `cel_response_rules.yaml` (when `response_validation.cel.enabled: true`)

```yaml
rules:
  - name: rule-name                     # Unique identifier (kebab-case)
    description: "What this rule does"  # Human-readable purpose
    enabled: true                       # Default: true

    # For MCP tool calls (use this over legacy 'expression' field)
    mcp_expression: |
      tool.name == "github__delete_repo"

    # For CLI commands (optional, evaluated only for CLI requests)
    cli_expression: |
      cli.command == "gh" && cli.arguments[1] == "delete"

    action: deny                        # allow, deny (request); allow, deny, redact (response)
    message: "Explanation shown to user"

    # Optional: audit_only logs but never blocks
    mode: audit_only

    # Response redaction fields (only with action: redact)
    redaction_pattern: "(?i)password\\s*[:=]\\s*\\S+"
    redaction_replacement: "[REDACTED]"
```

### Context Variables

#### MCP Request Rules (`mcp_expression`)

```cel
request.method              # MCP method (e.g., "tools/call")
request.params.name         # Tool name (prefixed: "github__delete_repo")
request.params.arguments    # Tool arguments as map[string]any
request.params.meta         # Request metadata

# Shorthand equivalents
tool.name                   # Same as request.params.name
tool.arguments              # Same as request.params.arguments
```

#### CLI Rules (`cli_expression`)

```cel
cli.command                 # Command name (e.g., "rm", "gh", "kubectl")
cli.arguments               # List of command arguments ([]string)
cli.working_directory       # Current working directory
cli.client_info.hostname    # Client hostname
cli.client_info.username    # Username running the CLI
cli.client_info.os          # Operating system ("darwin", "linux", etc.)
cli.client_info.arch        # Architecture ("amd64", "arm64", etc.)
cli.client_info.shell       # Shell environment ("bash", "zsh", etc.)
cli.client_info.cli_version # CLI proxy version
```

#### Response Rules (`expression`)

```cel
request.method                # Original request method
request.params.name           # Tool name that was called
request.params.arguments      # Tool arguments used
response.isError              # Boolean: is this an error response?
response.content              # List of content items
response.content[0].type      # "text", "image", or "resource"
response.content[0].text      # Text content (for type == "text")
response.content[0].data      # Image data base64 (for type == "image")
response.content[0].mimeType  # MIME type for images
response.meta                 # Response metadata
```

### Actions

| Action  | Request Rules | Response Rules | Behavior |
|---------|:---:|:---:|----------|
| `allow` | Yes | Yes | Permit the operation |
| `deny`  | Yes | Yes | Block the operation |
| `redact`| No  | Yes | Replace matched content using regex |

### CEL Function Reference

| Function | Example | Description |
|----------|---------|-------------|
| `has(obj, field)` | `has(tool.arguments, "force")` | Check if field exists |
| `get(obj, field, default)` | `get(tool.arguments, "path", "")` | Get with fallback |
| `.contains(s)` | `tool.name.contains("delete")` | Substring check |
| `.startsWith(s)` | `tool.name.startsWith("github__")` | Prefix check |
| `.endsWith(s)` | `tool.name.endsWith("_repo")` | Suffix check |
| `.matches(re)` | `text.matches("(?i)api[_-]?key")` | Regex match |
| `.size()` | `cli.arguments.size() >= 2` | Length of list or map |
| `.exists(x, expr)` | `cli.arguments.exists(a, a == "-rf")` | Any element matches |
| `.all(x, expr)` | `list.all(x, x > 0)` | All elements match |
| `x in [a, b]` | `cli.command in ["rm", "rmdir"]` | Membership test |

### Examples

#### Request Rule: Deny destructive GitHub operations (MCP + CLI)

```yaml
rules:
  - name: no-destructive-github
    description: Block destructive GitHub operations via MCP and CLI
    mcp_expression: |
      tool.name in ["github__delete_repo", "github__delete_file"]
    cli_expression: |
      cli.command == "gh" &&
      cli.arguments.size() >= 2 &&
      cli.arguments.exists(a, a == "delete")
    action: deny
    message: "Destructive GitHub operations are not permitted"
```

#### Request Rule: Block mass operations

```yaml
rules:
  - name: no-mass-email
    description: Prevent sending to more than 10 recipients
    mcp_expression: |
      tool.name == "email__send" &&
      has(tool.arguments, "recipients") &&
      tool.arguments.recipients.size() > 10
    action: deny
    message: "Sending to more than 10 recipients requires approval"
```

#### Response Rule: Redact sensitive patterns

```yaml
rules:
  - name: redact-api-keys
    description: Redact API keys from tool responses
    expression: |
      size(response.content) > 0 &&
      response.content[0].type == "text" &&
      response.content[0].text.matches("(?i)api[_-]?key\\s*[:=]\\s*\\S+")
    action: redact
    redaction_pattern: "(?i)(api[_-]?key\\s*[:=]\\s*)\\S+"
    redaction_replacement: "${1}[REDACTED]"
    message: "API keys redacted from response"
```

### Common Mistakes

| Mistake | Problem | Fix |
|---------|---------|-----|
| Using `expression` for CLI rules | CLI context not available in `expression` | Use `cli_expression` for CLI rules |
| Non-boolean expression result | Rule evaluation fails (fail-open) | Ensure expression returns `true` or `false` |
| Missing `has()` check | Nil access if field missing | Use `has(tool.arguments, "field")` before access |
| Wrong regex syntax | CEL/Go regex, not PCRE | Use Go `regexp` syntax (no lookaheads) |
| `redaction_pattern` on request rule | Redaction only works on responses | Use `action: deny` for request rules |

### Testing Workflow

1. Set `mode: audit_only` on your rule to test without blocking
2. Trigger the rule by sending matching requests
3. Check audit logs via `maybedont__get_audit_log` native tool
4. Remove `mode: audit_only` when confident the rule works correctly

### Key Notes

- CEL expressions compile at startup; invalid expressions prevent startup
- First enabled `deny` match short-circuits evaluation (subsequent rules are skipped)
- Rules with only `mcp_expression` are skipped for CLI commands (and vice versa)
- Redaction uses Go `regexp` syntax (not PCRE)
- If `redaction_pattern` is empty, entire content becomes `[REDACTED]`
