# Maybe Don't CLI Proxy Instructions

## Overview
All CLI commands that interact with external services must be routed through Maybe Don't for validation before execution.

## Command Format

Use this format for ALL shell commands:
```bash
maybe-dont cli -s <server-url> -- <command> [args...]
```

## Important Rules

- **Always** use the `maybe-dont cli` wrapper for external commands
- The `--` separator is **required** between cli flags and the command to execute
- If a command is denied by policy, explain the denial reason to the user and suggest compliant alternatives
- **Never** bypass validation by running commands directly
- Use `--dry-run` to validate a command without executing it

## Optional: Client Identification

For audit attribution, set:
```bash
export MAYBE_DONT_CLIENT_ID="your-email@company.com"
```

## Examples

```bash
# GitHub operations
maybe-dont cli -s http://localhost:8080 -- gh pr create --title "New feature"
maybe-dont cli -s http://localhost:8080 -- gh issue list

# Cloud CLI tools
maybe-dont cli -s http://localhost:8080 -- aws s3 ls
maybe-dont cli -s http://localhost:8080 -- kubectl get pods

# Dry-run mode (validate only)
maybe-dont cli -s http://localhost:8080 --dry-run -- rm -rf /tmp/test
```

## Handling Denials

When a command is denied:
1. Read the denial message to understand the policy violation
2. Explain to the user why the command was blocked
3. Suggest alternative approaches that comply with policy
