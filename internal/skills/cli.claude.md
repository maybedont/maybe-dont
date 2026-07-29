# maybe-dont-cli

## Description
Routes all CLI commands through Maybe Don't for validation before execution.

## Instructions

All shell/CLI commands must be executed through the Maybe Don't CLI proxy:

```bash
maybe-dont cli -s <server-url> -- <command> [args...]
```

### Required Setup

1. Ensure Maybe Don't is running and accessible
2. Set the server URL via the `-s` flag or configure it in your environment

### Client Identification (Optional)

To identify yourself in audit logs for attribution, set:
```bash
export MAYBE_DONT_CLIENT_ID="your-email@company.com"
```

### Examples

```bash
# GitHub CLI
maybe-dont cli -s http://localhost:8080 -- gh pr create --title "Feature X"

# AWS CLI
maybe-dont cli -s http://localhost:8080 -- aws s3 ls

# kubectl
maybe-dont cli -s http://localhost:8080 -- kubectl get pods

# Dry-run mode (validate without executing)
maybe-dont cli -s http://localhost:8080 --dry-run -- rm -rf /tmp/test
```

### Handling Denials

If a command is denied by policy:
1. The CLI will print the denial reason
2. Explain the denial to the user
3. Suggest alternative approaches that comply with policy
4. Do NOT attempt to bypass validation

### Important Notes

- The `--` separator is REQUIRED between cli flags and the command
- Commands are validated against security policies before execution
- If the server is unreachable, commands will execute with a warning (fail-open)
