---
name: security-review
description: Use when reviewing code for security vulnerabilities, before committing security-sensitive changes, or when implementing authentication, authorization, or credential handling
---

# Security Review

## Overview

Structured security review for the maybe-dont gateway. This project handles authentication credentials, MCP request validation, and security policies - security review is critical.

## When to Use

- Before committing changes to auth, credential handling, or validation code
- When implementing new API endpoints or handlers
- When modifying pass-through authentication logic
- When adding logging (to avoid leaking secrets)
- Before opening PRs with security-sensitive changes

## Review Checklist

### Credential Handling
- [ ] No credentials logged (Authorization headers, API keys, tokens)
- [ ] No credentials in error messages returned to clients
- [ ] Pass-through credentials not exposed to unintended destinations
- [ ] Credentials cleared from memory when no longer needed

### Input Validation
- [ ] All external input validated before use
- [ ] No path traversal vulnerabilities (`../` in file paths)
- [ ] No command injection (user input in shell commands)
- [ ] No SQL/NoSQL injection if applicable
- [ ] Request size limits enforced

### Authentication/Authorization
- [ ] Auth checks cannot be bypassed
- [ ] Failed auth returns generic error (no user enumeration)
- [ ] Timing-safe comparison for secrets when relevant
- [ ] Session tokens have sufficient entropy

### Logging Safety
- [ ] Tool parameters NOT logged (may contain secrets)
- [ ] Authorization headers NOT logged
- [ ] API keys NOT logged
- [ ] Error messages don't leak internal paths or stack traces to clients

### MCP-Specific
- [ ] CEL policies can't be bypassed via malformed requests
- [ ] Tool name prefixing correctly enforced
- [ ] Pass-through auth headers mapped to correct destinations
- [ ] Audit log captures security-relevant events

## Sensitive Data Patterns

**Never log or expose:**
```go
// BAD - logs Authorization header
logger.Info("Request received", zap.Any("headers", r.Header))

// GOOD - log safe fields only
logger.Info("Request received", zap.String("method", r.Method))
```

**Safe error responses:**
```go
// BAD - exposes internal path
return fmt.Errorf("failed to read /etc/secrets/api-key: %w", err)

// GOOD - generic message
return fmt.Errorf("configuration error: %w", err)
```

## Quick Reference

| Risk | Check |
|------|-------|
| Credential leak | Search for `Authorization`, `api_key`, `token`, `secret` in logs |
| Injection | User input → shell/SQL/file path without sanitization |
| Auth bypass | Unauthenticated paths, missing middleware |
| Info disclosure | Stack traces, internal paths in error responses |

## Integration with PR Process

Before opening a PR with security-sensitive code:

1. Run this checklist against changed files
2. Note any items that don't apply with justification
3. Include "Security Review: Completed" in PR description
4. Flag remaining concerns for reviewer attention
