---
name: cel-policy-authoring
description: Use when writing, testing, or debugging CEL policy rules for request or response validation in maybe-dont gateway
---

# CEL Policy Authoring

## Overview

Write CEL (Common Expression Language) policies for the maybe-dont gateway. Policies validate MCP requests/responses and can allow, deny, or redact.

## Context Variables

| Variable | Type | Available In | Description |
|----------|------|--------------|-------------|
| `request.method` | string | Request rules | MCP method (e.g., `"tools/call"`) |
| `request.params.name` | string | Request rules | Tool name being called |
| `request.params.arguments` | map | Request rules | Tool arguments |
| `request.params.meta` | map | Request rules | Request metadata |
| `response` | map | Response rules | Response data |
| `auth` | map | Both | Authentication context |

## Safe Field Access

**Always use `get()` for optional fields** to avoid runtime errors on missing data:

```cel
# Safe - returns default if field missing
get(request, "method", "") == "tools/call"
get(request.params, "name", "") == "dangerous_tool"

# Unsafe - fails if field doesn't exist
request.params.nonexistent.nested  # Runtime error!
```

**Use `has()` to check field existence:**
```cel
has(request.params, "arguments") && get(request.params.arguments, "path", "") != ""
```

## Rule Structure

```yaml
rules:
- name: rule-name-with-hyphens    # Unique identifier
  description: Human explanation  # What this rule does
  enabled: true                   # Optional, default true
  expression: |-                  # CEL expression (must return bool)
    get(request, "method", "") == "tools/call" &&
    get(request.params, "name", "") == "target_tool"
  action: deny                    # allow, deny, or redact (response only)
  message: User-facing denial message
```

## Common Patterns

**Deny specific tool:**
```cel
get(request, "method", "") == "tools/call" &&
get(request.params, "name", "") == "github__delete_file"
```

**Deny tool with argument pattern:**
```cel
get(request, "method", "") == "tools/call" &&
get(request.params, "name", "") == "write_file" &&
get(request.params.arguments, "path", "").startsWith("/etc/")
```

**Deny tools by prefix:**
```cel
get(request, "method", "") == "tools/call" &&
get(request.params, "name", "").startsWith("github__delete")
```

## Testing CEL Expressions

1. Write the rule in your rules file
2. Set `mode: audit_only` to test without blocking
3. Trigger the tool call and check audit logs
4. Verify the expression matches expected requests
5. Remove `mode: audit_only` when ready to enforce

## Quick Reference

| Function | Usage | Returns |
|----------|-------|---------|
| `get(map, key, default)` | Safe field access | Value or default |
| `has(map, key)` | Check field exists | bool |
| `.startsWith(prefix)` | String prefix match | bool |
| `.endsWith(suffix)` | String suffix match | bool |
| `.contains(substr)` | String contains | bool |
| `.matches(regex)` | Regex match | bool |

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Direct field access on optional fields | Use `get(map, key, default)` |
| Forgetting method check | Always include `request.method == "tools/call"` |
| Typo in tool name | Tool names use `clientname__toolname` format |
| Missing multiline `\|-` in YAML | Use `\|-` for expressions spanning lines |
