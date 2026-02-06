# CLI Command Validation Instructions

## Purpose
Route all CLI commands through the Maybe Don't security gateway for policy validation before execution.

## Command Syntax
```
maybe-dont cli -s <gateway-url> -- <command> [arguments...]
```

**Required elements:**
- `-s <gateway-url>`: The gateway server URL
- `--`: Separator between cli flags and the command (REQUIRED)
- `<command>`: The actual command to validate and execute

## Behavior Guidelines

1. **Always wrap commands**: Use `maybe-dont cli -s <url> --` before any CLI command that interacts with external services
2. **Handle denials gracefully**: If a command is denied, explain the reason and suggest alternatives
3. **Never bypass**: Do not run commands directly to avoid validation
4. **Use dry-run for testing**: Add `--dry-run` flag to validate without executing

## Optional Configuration

Set client ID for audit attribution:
```
export MAYBE_DONT_CLIENT_ID="identifier"
```

## Examples

```
maybe-dont cli -s http://localhost:8080 -- gh pr create --title "Feature"
maybe-dont cli -s http://localhost:8080 -- aws s3 ls
maybe-dont cli -s http://localhost:8080 --dry-run -- kubectl delete pod mypod
```
