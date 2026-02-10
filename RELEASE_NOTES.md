# Maybe Don't Gateway v1.1.0 Release Notes

v1.1.0 extends the gateway beyond MCP tool call validation into CLI command validation, adds support for multiple AI providers, and introduces a declarative policy test suite. This release also restructures the CLI with a breaking change to the command hierarchy.

---

## Breaking Change: Command Restructure

The top-level `start` command has moved under a `gateway` sub-command:

```bash
# Before (v1.0.0)
maybe-dont start

# After (v1.1.0)
maybe-dont gateway start
```

The new command hierarchy is: `gateway`, `cli`, `test`, `skill`, and `version`.

---

## CLI Proxy for AI Agent Command Validation

AI agents can now route shell commands through the gateway for policy validation before execution. This brings the same CEL and AI-powered validation that protects MCP tool calls to arbitrary CLI commands like `gh`, `aws`, and `kubectl`.

```bash
# Validate and execute a command
maybe-dont cli -s http://localhost:8080 -- gh repo delete my-repo

# Validate only, without executing
maybe-dont cli -s http://localhost:8080 --dry-run -- gh repo delete my-repo
```

A REST endpoint is also available for programmatic integration:

```
POST /api/v1/cli/validate
```

CEL rules now support separate expressions for MCP and CLI contexts:

```yaml
- name: no-destructive-github-actions
  mcp_expression: |
    tool.name == "github__delete_repo"
  cli_expression: |
    cli.command == "gh" && cli.arguments[1] == "delete"
  action: deny
  message: "Destructive GitHub operations not permitted"
```

Use `maybe-dont skill list` and `maybe-dont skill view <name>` to export agent integration instructions in Claude, Copilot, Cursor, and generic formats.

---

## Provider-Agnostic AI Validation

AI-powered validation is no longer locked to OpenAI. The gateway now supports OpenAI, Anthropic, and any OpenAI-compatible endpoint (Azure OpenAI, LiteLLM, Ollama, etc.). The provider is auto-detected from the endpoint URL, and all providers benefit from retry logic with exponential backoff.

```yaml
validation:
  ai:
    # OpenAI (default)
    endpoint: "https://api.openai.com/v1/chat/completions"
    model: "gpt-4o"

    # Anthropic
    endpoint: "https://api.anthropic.com/v1/messages"
    model: "claude-sonnet-4-20250514"

    # Azure OpenAI or any compatible endpoint
    endpoint: "https://my-deployment.openai.azure.com/openai/deployments/gpt-4o/chat/completions"
```

---

## Policy Test Suite

A new `maybe-dont test policies` command enables declarative testing of your validation rules. Define test cases in YAML with expected outcomes, then run them against your policy configuration.

```bash
# Run all policy tests
maybe-dont test policies --config-dir ./config

# Filter by engine type
maybe-dont test policies --engine cel

# Compare AI model accuracy across providers
maybe-dont test policies --matrix

# Output results as JUnit XML for CI integration
maybe-dont test policies --output junit
```

The test suite tracks rolling pass rate history per model, reporting a stability percentage that shows how consistently each model performs across runs.

---

## Environment Variable Configuration Improvements

All configuration fields, including deeply nested map fields, can now be overridden via environment variables. The default AI temperature has been set to 0.0 for deterministic policy decisions.

```bash
# Override nested AI parameters
export MAYBE_DONT_VALIDATION_AI_PARAMETERS_TEMPERATURE=0.0

# Configure downstream servers entirely via env vars
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE=http
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL=https://api.githubcopilot.com/mcp/
```

---

## Installation

**Homebrew:**

```bash
brew install maybedont/tap/maybe-dont
```

**GitHub Releases:**

Download pre-built binaries from the [GitHub releases page](https://github.com/maybedont/releases/releases/tag/v1.1.0).
