# Maybe Don't Gateway

Maybe Don't is a security gateway for agentic AI. It sits between an AI
agent (or its editor/IDE) and the tools it can call — MCP servers and shell
commands — and validates every call against configurable policies before it
runs.

Policies can be deterministic (CEL expressions) or AI-powered (a second
model judges whether a specific call, in context, looks dangerous). Every
decision is written to an audit log.

## How it works

```
                       ┌─────────────────────────────────────────┐
                       │              Maybe Don't Gateway          │
  Agent / IDE  ──────► │                                           │
                       │  ┌───────────────┐   ┌─────────────────┐ │
  MCP proxy,           │  │ CEL request   │   │  AI request     │ │
  /api/v1/intercept,   │  │ rules         │──►│  rules          │ │
  or /api/v1/cli/      │  └───────────────┘   └────────┬────────┘ │
  validate             │                                │          │
                       │                                ▼          │      Downstream
                       │                    (deny / allow / audit) │ ───► MCP server(s)
                       │                                │          │      or shell
                       │                                ▼          │
                       │  ┌───────────────┐   ┌─────────────────┐ │
                       │  │ CEL response  │◄──│  AI response    │ │
                       │  │ rules         │   │  rules          │ │◄─── response
                       │  └───────┬───────┘   └─────────────────┘ │
                       │          ▼                                │
                       │      Audit log                            │
                       └───────────────────────────────────────────┘
```

The gateway exposes three entry surfaces, all going through the same
validation chain (see [ARCHITECTURE.md](ARCHITECTURE.md) for details):

- **MCP proxy** — the gateway itself speaks MCP and proxies tool calls to
  one or more configured downstream MCP servers.
- **`/api/v1/intercept`** — a REST endpoint for editor/agent hook scripts
  (Claude Code, Cursor, Gemini CLI, Cline, Copilot — see `maybe-dont hooks
  list`) to submit a tool call for a policy decision before or after it runs.
- **`/api/v1/cli/validate`** (and `maybe-dont cli`) — validates raw shell
  commands (`gh`, `aws`, `kubectl`, ...) before an agent executes them.

## Install

**Homebrew:**

```bash
brew install maybedont/tap/maybe-dont
```

**Go:**

```bash
go install github.com/maybedont/maybe-dont@latest
```

**Docker:**

```bash
docker pull ghcr.io/maybedont/maybe-dont:latest
```

**Binary release:** download a tarball from the
[releases page](https://github.com/maybedont/releases/releases) for your
platform.

## Quickstart

Run the gateway once to generate default config files, then edit them. AI validation is on by default, so this needs an AI API key (e.g. `OPENAI_API_KEY`) in your environment to start; without one it'll bootstrap the config files and exit rather than staying up — either way, the files are now written:

```bash
maybe-dont gateway start
# Configuration initialized at ~/.config/maybe-dont
# ^C to stop, if it's still running
```

This writes `maybe-dont.yaml` plus the four rules files
(`cel_request_rules.yaml`, `ai_request_rules.yaml`,
`cel_response_rules.yaml`, `ai_response_rules.yaml`) to
`~/.config/maybe-dont`. Add a downstream MCP server:

```yaml
# ~/.config/maybe-dont/maybe-dont.yaml
downstream_mcp_servers:
  github:
    type: http
    url: "https://api.githubcopilot.com/mcp/"
    auth:
      pass_through:
        enabled: true
        headers:
          - source_header: "X-GitHub-Token"
            target_header: "Authorization"
            format: "Bearer {value}"

server:
  type: http
  listen_addr: "127.0.0.1:8080"

# This example uses only deterministic CEL rules. AI-powered validation and
# the AI-powered audit report tool are on by default and require an API key
# (validation.ai.api_key) — see internal/config/defaults/maybe-dont.yaml for
# that configuration; here they're turned off for a minimal first run.
request_validation:
  cel:
    enabled: true
    mode: enforce
    rules_file: "cel_request_rules.yaml"
  ai:
    enabled: false

native_tools:
  audit_report:
    enabled: false
```

And a CEL rule denying a dangerous call:

```yaml
# ~/.config/maybe-dont/cel_request_rules.yaml
rules:
  - name: deny-github-delete-file
    description: Deny github delete file
    enabled: true
    expression: |-
      get(request, "method", "") == "tools/call" &&
      get(request.params, "name", "") == "github__delete_file"
    action: deny
    message: Denied access to github__delete_file
```

Start the gateway and point your MCP client at `http://127.0.0.1:8080`:

```bash
maybe-dont gateway start
```

Tools from the `github` server are now exposed with a client-name prefix,
e.g. `github__create_issue`, and every call against them is evaluated by
the rules above before it reaches GitHub.

To validate a shell command directly instead of proxying MCP:

```bash
maybe-dont cli -s http://127.0.0.1:8080 -- gh pr comment 123 --body "LGTM"
```

## Configuration

Configuration is loaded in this order, each overriding the last:

1. `maybe-dont.yaml` in the config directory (`--config-dir`,
   `MAYBE_DONT_CONFIG_DIR`, `$XDG_CONFIG_HOME/maybe-dont`, or
   `~/.config/maybe-dont`)
2. Environment variables, prefixed `MAYBE_DONT_`
   (e.g. `MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL`)
3. Command-line flags

`${VAR_NAME}` in any config string is substituted from the environment at
load time (useful for API keys and tokens). See `CLAUDE.md` for the full
environment-variable reference and downstream-server-via-env-vars syntax,
and `internal/config/defaults/` for the shipped default rule files.

## Fail-open and fail-closed behavior

Validation has a bounded time budget (`validation.max_blocking_ms`, default
90s) so a slow or unreachable AI provider can't hang a request forever. When
that budget is exhausted, or when the gateway itself is unreachable (as with
`maybe-dont cli` or an agent hook script), the call **proceeds and is logged**
rather than being blocked — this is fail-open behavior, and it is the default
everywhere in this project. The one exception is CEL and AI rules
explicitly set to `mode: enforce`, which can deny a request outright once
evaluated. Editor-side integrations may impose their own fail-closed
behavior independent of the gateway (for example, Cursor's
`beforeMCPExecution` hook).

## Telemetry

Maybe Don't sends no telemetry. The gateway makes outbound network
connections only to:

- the downstream MCP servers you configure, and
- the AI provider endpoint you configure, if AI-based validation is enabled.

There is no usage reporting, no installation identifier, no version check,
and no crash reporting.

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — how the gateway is built
- [CONTRIBUTING.md](CONTRIBUTING.md) — building, testing, and releasing
- [docs/specs/](docs/specs/) — design specs for individual features
- `maybe-dont skill list` / `maybe-dont hooks list` — embedded agent
  integration material

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Security

See [SECURITY.md](SECURITY.md) to report a vulnerability.
