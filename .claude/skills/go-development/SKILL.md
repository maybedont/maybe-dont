---
name: go-development
description: Use when writing, modifying, or reviewing Go code in this repository - covers code navigation with gopls, testing patterns, error handling conventions, and required quality checks
---

# Go Development

## Overview

Go development patterns and quality standards for the maybe-dont gateway. Prefer gopls for semantic code navigation over grep/glob.

## Code Navigation with gopls

**Always prefer gopls over manual searching** for Go code exploration:

```bash
# Jump to definition
gopls definition main.go:21:6

# Find all usages
gopls references main.go:21:6

# Find interface implementations
gopls implementation main.go:50:6

# Show callers/callees
gopls call_hierarchy main.go:21:6

# List symbols in file
gopls symbols main.go

# Search workspace for symbol
gopls workspace_symbol MyFunc
```

Format: `gopls <command> <file>:<line>:<column>`

**Use gopls when you need to:**
- Understand how a function or type is used
- Navigate between related code (callers/callees)
- Find where a struct field or method is defined
- Trace through code paths

Fall back to grep/glob only for non-code patterns (comments, strings, config values).

## Testing Patterns

**Table-driven tests are required.** Structure for extensibility:

```go
func TestSomething(t *testing.T) {
    // Tests that [describe behavior] for various [inputs/conditions].
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {"valid input", "foo", "bar", false},
        {"empty input", "", "", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

**Before writing tests:** Check if coverage already exists.

## Quick Reference

| Task | Command |
|------|---------|
| Run all tests | `make test` |
| Run package tests | `go test -v ./internal/gateway/...` |
| Run specific test | `go test -run TestName -v ./...` |
| Build | `make build` |
| Lint | `make lint` |
| Clean deps | `go mod tidy` |

## Required Quality Checks

Before committing Go code:

1. `go mod tidy` - if dependencies changed
2. `make lint` - must pass golangci-lint
3. `make test` - all tests must pass
4. Code must compile

## Error Handling

- Use `errors.Is()` not `==` for error comparison
- Fail-fast over defensive programming: return errors, don't mask with defaults
- Validate at system boundaries only (user input, external APIs)

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Using grep for Go symbols | Use `gopls definition/references` |
| Single-case tests | Use table-driven pattern |
| `err == ErrFoo` | Use `errors.Is(err, ErrFoo)` |
| Silent fallbacks | Return error with clear message |
| Testing without checking existing coverage | Search tests first |
