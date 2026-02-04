# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Maybe Don't Gateway is a security middleware service built in Go that acts as a protective gateway between LLM/AI agents and Model Context Protocol (MCP) servers. It intercepts and validates MCP requests using both deterministic rules (CEL-based policies) and AI-powered validation to prevent potentially dangerous operations.

## User Interaction Preferences

- **Wait for responses**: When asking questions or requesting clarification, always wait for the user's response before continuing. Do not assume answers or proceed without explicit input.
- **Challenge ideas**: Be careful to agree just to be agreeable. Be prepared to defend your position, and communicate why a recommendation from the developer may not be a good idea. If the idea sounds good, see if you can find a reason why it is not based upon current conventions, code quality, risk or external specifications.  

## Essential Commands

### Build and Development
- `make build` - Build the binary
- `make test` - Run all tests
- `make lint` - Run golangci-lint (linter must be installed)
- `make clean` - Clean build artifacts
- `go test -v ./internal/gateway/...` - Run tests for specific package
- `go test -run TestName -v ./...` - Run a specific test
- `go mod tidy` - Clean up dependencies after changes
- `make setup` - Install Git hooks for the developer
- `make docker-build` - Build a local dev Docker image
- `make docker-run` - Build a local dev Docker image and then start it

### Running the Gateway
- `./maybe-dont start` - Start the gateway with default config
- `./maybe-dont start --config-dir {some dir}` - Start the gateway with a specific location for the config file
- `./maybe-dont version` - Show version information

### Release Management
- `make bump-version` - Bump version using commitizen
- `make snapshot` - Create a snapshot release (for testing)

## Architecture and Key Components

### Core Structure
The gateway operates as a transparent proxy between MCP clients and multiple downstream MCP servers, implementing a dual-validation approach:

1. **Request Policy Engine** (`internal/gateway/cel_engine.go`) - Evaluates deterministic policy rules using Google's Common Expression Language (CEL)
2. **AI Request Policy Engine** (`internal/gateway/ai_engine.go`) - Uses OpenAI API for context-aware validation of potentially dangerous operations
3. **ClientManager** (`internal/gateway/client_manager.go`) - Manages multiple downstream MCP client connections

### Multi-Client Architecture
The gateway now supports multiple downstream MCP servers simultaneously:
- **ClientManager**: Manages multiple `ClientInfo` instances, each representing a connection to a downstream MCP server
- **Name Prefixing**: Tools, prompts, and resources are automatically prefixed with client names (`{client_name}__{original_name}`)
- **Request Routing**: Incoming requests with prefixed names are parsed and routed to the appropriate downstream client
- **Mixed Transport Types**: Each client can use different transport types (stdio, http, sse) simultaneously

### MCP Transport Support
The gateway supports multiple MCP transport types per client:
- **STDIO** - Spawns processes and communicates via stdin/stdout
- **SSE** - Server-Sent Events for streaming connections
- **HTTP** - Standard HTTP requests/responses

### Configuration Structure
- **Multiple Clients**: Configure multiple downstream servers in the `downstream_mcp_servers` map
- **Claude Desktop Compatibility**: Configuration format matches Claude Desktop's `mcpServers` structure
- **Client Naming**: Map keys serve as unique client names for identification and prefixing

### Configuration Hierarchy
Configuration is loaded in this order (later overrides earlier):
1. YAML config file (`maybe-dont.yaml`)
2. Environment variables (prefix: `MAYBE_DONT_`)
3. Command-line flags

### Validation Policy Configuration
Each validation phase (CEL request, AI request, CEL response, AI response) has two settings:
- **enabled** (bool) - Whether the validation phase runs at all
- **mode** (default: `audit_only`) - When set to `audit_only`, rules log but don't block. Set to empty string to enable blocking.

Per-rule settings allow fine-grained control:
- **enabled** (bool) - Whether this specific rule runs (default: true)
- **mode** (optional, only `audit_only` supported) - When set, overrides top-level for this rule

**Mode Resolution**: Top-level `mode: audit_only` applies to ALL rules in that phase. Per-rule `mode: audit_only` only affects that rule.

Defaults:
- `request_validation.cel.enabled`: true, `mode`: audit_only
- `request_validation.ai.enabled`: true, `mode`: audit_only
- `response_validation.cel.enabled`: false, `mode`: audit_only
- `response_validation.ai.enabled`: false, `mode`: audit_only

### AI Configuration (Centralized)
All AI-powered features share a common configuration under `validation.ai`:
```yaml
validation:
  ai:
    endpoint: "https://api.openai.com/v1/chat/completions"
    model: "gpt-4o-mini"
    api_key: "${OPENAI_API_KEY}"
```
This configuration is used by:
- AI request validation
- AI response validation
- Audit report generation (native tool)

### Blocking Budget
The gateway implements a blocking budget to limit cumulative validation latency:
- **max_blocking_ms** (default: 90000ms) - Maximum cumulative time to block a request waiting for all validation decisions
- **max_rule_evaluation_ms** (default: 45000ms) - Maximum time for any single rule evaluation

When the blocking budget is exhausted, remaining validations continue asynchronously but the request proceeds (fail-open behavior).

### Security Rules
- **CEL Request Rules**: Loaded from external `cel_request_rules.yaml` file when `request_validation.cel.enabled` is true (deterministic CEL-based rules)
- **AI Request Rules**: Loaded from external `ai_request_rules.yaml` file when `request_validation.ai.enabled` is true
- **CEL Response Rules**: Loaded from external `cel_response_rules.yaml` file when `response_validation.cel.enabled` is true (deterministic CEL-based rules)
- **AI Response Rules**: Loaded from external `ai_response_rules.yaml` file when `response_validation.ai.enabled` is true
- **Multi-Client Validation**: Policies can target specific clients using name prefixes
- **Required When Enabled**: Rules files must be specified in config when their corresponding validation phase is enabled

### Native Tools
The gateway provides built-in introspection tools (prefixed with `maybedont__`):
- **maybedont__get_audit_log** - Access audit log entries with filtering and pagination
- **maybedont__generate_audit_report** - AI-powered security analysis of audit logs
- **maybedont__list_downstream_servers** - List configured downstream MCP servers
- **maybedont__list_sessions** - List active client sessions
- **maybedont__discover_tools** - Trigger lazy discovery for pass-through auth clients

Each tool can be enabled/disabled in config under `native_tools`.

### Session Management
- Sessions have configurable idle timeout (`server.session_timeout_minutes`, default: 30)
- Sessions inactive longer than the timeout are cleaned up
- When a session expires, clients need to call `maybedont__discover_tools` to reconnect

## Important Development Notes

### Tool and Plugin Suggestions
If a plugin, MCP server, or skillset could improve performance, accuracy, or efficiency for a task, proactively suggest it. This includes LSP tools, linters, formatters, or other development aids that aren't currently being used but would be beneficial.

### Feature Development Workflow
For feature development or any sizeable code change, follow this spec-driven approach:

1. **Build a spec first**: Before implementing, create a specification document. If not explicitly asked to build a spec, ask if one should be created.
2. **Iterate on the spec**: Collaborate to refine the spec until it's agreed upon, then save it to `docs/specs/`. The goal is to document the plan clearly and minimize context window usage during implementation.
3. **Implement systematically**: Once the spec is finalized, use todo lists derived from the spec to complete work in smaller chunks. This improves efficiency and accuracy.
4. **Leverage sub-agents**: When possible, suggest using sub-agents to parallelize work and improve accuracy.
5. **Version control specs**: Commit specs to source control and keep them updated so they remain useful for future reference.
6. **Check existing specs**: Before starting a new spec or feature, review `docs/specs/` for relevant existing specs. Consider updating an existing spec rather than creating a new one. When modifying an existing spec, you can create a temporary worklist file in `docs/specs/` to track implementation progress.

### Spec Status Management
Spec status is tracked in `docs/specs/README.md` (single source of truth). Valid statuses:
- **Draft** - Work in progress, not ready for implementation
- **Ready for Implementation** - Design approved, ready to build
- **Implemented** - Feature shipped, spec is reference documentation
- **Superseded** - Replaced by another spec (link to replacement)

When creating or updating specs:
1. Add or move the spec to the appropriate section in `docs/specs/README.md`
2. Specs can optionally include a note pointing to README for status: `> **Status**: See [README.md](README.md)`

### Code Navigation with LSP
**Prefer `gopls` over grep/glob** for Go code navigation. See the `go-development` skill for command reference. Fall back to grep/glob only for non-code patterns (comments, strings, config values).

### Go Development Standards
From `.cursor/rules/golang.mdc`:
- Always use the latest version of imports
- Run `go mod tidy` after dependency changes
- Ensure code compiles successfully
- All tests must pass
- Code must pass `golangci-lint run`
- Write unit tests where applicable
- CLI configuration goes in `cmd/` folder

### Code Style Preferences
- **Clean and concise**: Code should be straightforward and avoid unnecessary complexity
- **Meaningful names**: Variable, function, and type names should clearly convey their purpose to make code self-documenting
- **Informational comments**: Comments should explain the "why" and help developers understand the workflow, not just restate what the code does
- **DRY (Don't Repeat Yourself)**: Favor reusable code and avoid duplication. Exception: some duplication in tests is acceptable for clarity and test isolation
- **Always keep code formatted**: Run formatters before committing
- **Consider edge cases**: When writing new code, think through edge cases and ensure test coverage addresses them
- **Error equality**: Favor `errors.Is` instead of the legacy equality checks. When you find the legacy usage, please update it.
- **Fail-fast over defensive programming**: Prefer failing explicitly with clear error messages over masking problems with fallbacks or default values. Silent fallbacks hide bugs and make debugging harder. If a required parameter is missing or a precondition isn't met, return an error rather than proceeding with a "safe" default. This surfaces issues during development rather than hiding them until they cause subtle problems in production.

### Adding Configuration Fields
When adding new configuration fields:
- **Naming consistency**: Follow existing naming conventions in the config structs
- **Sensible defaults**: Provide reasonable defaults to minimize required user configuration
- **Environment variable support**: Ensure the field can be overridden via environment variable (follows `MAYBE_DONT_` prefix pattern)
- **Test coverage**: Add tests to verify the config value is loaded correctly and can be overridden via environment variable
- **Keep example config in sync**: When adding or changing defaults in `internal/config/config.go`, update `config/maybe-dont.yaml` to reflect the actual defaults. The shipped config file should represent what you'd get if you omitted the config file entirely.

### Logging Conventions
**Log level guidelines:**
- **DEBUG**: Use for code path tracing and expected conditions. Debug logs can be verbose (e.g., "discovered 5 tools: [tool1, tool2, ...]"). Use when handling expected errors or showing detailed flow.
- **INFO**: Use sparingly for significant events that won't spam logs. Appropriate for: startup/shutdown, new client connections, session timeouts. Avoid for frequently-executed code paths.
- **ERROR**: Use only for unexpected errors that indicate something went wrong.

### Security
See the `security-review` skill for comprehensive security review checklist. Key principle: **never log sensitive information** (Authorization headers, API keys, tool parameters).

### Commit and PR Guidelines
**Before committing**, perform a brief self-review:
- Check for bugs or logic errors introduced by your changes
- Verify no regression in existing functionality
- Ensure adequate test coverage for new or modified code

**Before opening a PR**, conduct a thorough review as if you had dedicated reviewers:
- **Security review**: Use the `security-review` skill checklist for security-sensitive changes
- **Performance review**: Identify potential bottlenecks, unnecessary allocations, or inefficient patterns
- **Documentation review**: For larger features or behavior changes, ask if the documentation at https://maybedont.ai/docs should be reviewed for needed updates. If documentation changes are needed, create a checklist in the PR description for the developer to review.

### Code Review Configuration
When performing automated code reviews, report issues that score 70 or higher on the confidence scale. Issues directly violating CLAUDE.md guidance should always be flagged regardless of score. The confidence scale is:
- **0-25**: Likely false positive or pre-existing issue
- **50**: Real issue but minor or unlikely to be hit in practice
- **75**: Verified issue that will likely be hit, or directly mentioned in CLAUDE.md
- **100**: Definitely a real issue with clear evidence

### Common Pitfalls
- **Unintended behavior changes**: When modifying existing code, bolster test coverage around the affected areas to catch regressions
- **Naming inconsistencies**: If breaking or changing a naming convention, flag it for review and discussion
- **Missing config validation**: New config fields need validation logic, defaults, and environment variable override support
- **Name prefixing edge cases**: Remember that tools/prompts/resources use `{client_name}__{original_name}` format

## Testing Approach

The project uses Go's standard testing framework with testify for assertions. Key test files:
- `internal/gateway/cel_engine_test.go` - Classic deterministic policy engine tests
- `internal/gateway/ai_engine_test.go` - AI policy engine tests
- `internal/gateway/tool_validation_test.go` - Tool validation chain tests
- `internal/gateway/gateway_test.go` - Main gateway integration tests
- `internal/gateway/session_test.go` - Session management tests
- `internal/gateway/stale_session_test.go` - Session timeout tests
- `internal/gateway/audit_entry_test.go` - Audit entry tests
- `internal/gateway/audit_log_tool_test.go` - Audit log tool tests
- `internal/config/config_test.go` - Configuration loading tests

### Testing Patterns
- **Use table-driven tests**: Structure tests with a slice of test cases for extensibility. See the `go-development` skill for the example pattern.
- **Avoid single-input tests**: Evaluate whether the test structure can accommodate multiple inputs rather than testing only one value.
- **Check for existing coverage**: Before writing a new test, check if the use case is already covered to avoid duplication.
- **Document test purpose**: Add a comment at the top of each test describing the use case and expected result.
- **Test-driven style**: When fixing a bug, write a test first to assert the error condition, watch it fail, then fix the code. This also applies to larger features with well-defined specs.

## Key Dependencies

- `github.com/mark3labs/mcp-go` - MCP protocol implementation
- `github.com/google/cel-go` - CEL expression evaluation
- `github.com/openai/openai-go` - OpenAI API client
- `github.com/spf13/cobra` - CLI framework
- `github.com/spf13/viper` - Configuration management
- `go.uber.org/zap` - Structured logging

## Environment Variables

Key environment variables for configuration:
- `MAYBEDONT_METRICS_OPTOUT` - Set to any value to disable anonymous usage metrics collection
- Environment variables follow the pattern: `MAYBE_DONT_{CONFIG_PATH}` where CONFIG_PATH uses underscores
- Use `${VAR_NAME}` syntax in config files for environment variable substitution (e.g., `api_key: "${OPENAI_API_KEY}"`)

### Directory Resolution

The gateway follows [XDG Base Directory conventions](https://specifications.freedesktop.org/basedir/latest/) for locating configuration and log files.

**Config Directory** (in priority order):
1. `--config-dir` CLI flag
2. `MAYBE_DONT_CONFIG_DIR` environment variable
3. `$XDG_CONFIG_HOME/maybe-dont`
4. `$HOME/.config/maybe-dont` (XDG default)

**Log Directory** (in priority order):
1. `--log-dir` CLI flag
2. `MAYBE_DONT_LOG_DIR` environment variable
3. `$XDG_STATE_HOME/maybe-dont`
4. `$HOME/.local/state/maybe-dont` (XDG default)

### Docker Volume Mount Patterns

The Docker image uses XDG Base Directory conventions. The binary embeds default configuration files and writes them on first run if they don't exist.

**Default XDG Paths in Container:**
- Config: `/home/maybedont/.config/maybe-dont/`
- State/Logs: `/home/maybedont/.local/state/maybe-dont/`

**Basic Usage (XDG defaults):**
```yaml
# docker-compose.yml
services:
  gateway:
    image: ghcr.io/maybedont/maybe-dont:latest
    ports:
      - "8080:8080"
    volumes:
      # Mount config read-only after initial setup
      - ./config:/home/maybedont/.config/maybe-dont:ro
      # Mount state read-write for logs, metrics, installation ID
      - ./state:/home/maybedont/.local/state/maybe-dont
```

**With Read-Only Root Filesystem:**
```yaml
services:
  gateway:
    image: ghcr.io/maybedont/maybe-dont:latest
    read_only: true
    volumes:
      - ./config:/home/maybedont/.config/maybe-dont:ro
      - ./state:/home/maybedont/.local/state/maybe-dont
```

**Using XDG Environment Variables:**
```yaml
services:
  gateway:
    image: ghcr.io/maybedont/maybe-dont:latest
    environment:
      - XDG_CONFIG_HOME=/xdg/config
      - XDG_STATE_HOME=/xdg/state
    volumes:
      - ./config:/xdg/config/maybe-dont:ro
      - ./state:/xdg/state/maybe-dont
```

**Using App-Specific Environment Variables:**
```yaml
services:
  gateway:
    image: ghcr.io/maybedont/maybe-dont:latest
    environment:
      - MAYBE_DONT_CONFIG_DIR=/config
      - MAYBE_DONT_LOG_DIR=/logs
    volumes:
      - ./config:/config:ro
      - ./logs:/logs
```

**First-Run Workflow:**
1. Start container without config volume to generate defaults
2. Copy defaults from container or use `defaults export` command
3. Customize configuration files
4. Restart with config volume mounted read-only

### Downstream MCP Server Configuration via Environment Variables

Configure downstream servers entirely via environment variables (no YAML file required):

```bash
# Basic HTTP client
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE=http
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL=https://api.githubcopilot.com/mcp/

# STDIO client with arguments
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_TYPE=stdio
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_COMMAND=/usr/local/bin/mcp-server
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_ARGS=--verbose,--port=8080

# Pass-through auth (compact format - single header)
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_ENABLED=true
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS=X-Token:Authorization:Bearer {value}

# Pass-through auth (compact format - multiple headers, semicolon-separated)
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_MYAPI_AUTH_PASS_THROUGH_HEADERS=X-Token:Authorization:Bearer {value};X-Tenant:X-Downstream-Tenant

# Pass-through auth (indexed format - mirrors YAML structure)
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_SOURCE_HEADER=X-Token
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_TARGET_HEADER=Authorization
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_FORMAT=Bearer {value}
```

**Client naming:** Underscores in env vars are converted to hyphens: `AWS_DOCS` → `aws-docs`

**Limitation:** Client names with literal underscores (e.g., `aws_docs` in YAML) cannot be configured via env vars. Use YAML for these edge cases.

**Compact header format:** `source:target[:format]` with multiple headers separated by `;`

## Anonymous Usage Metrics

The gateway collects anonymous usage metrics to help improve the product. **No personal data or sensitive information is ever collected or transmitted.**

### What is Collected

The following anonymized metrics are collected:
- **Installation ID**: A randomly generated unique identifier (32-character hex string) that is created on first run and persisted locally
- **Version**: The version of the gateway binary
- **Tool Invocations**: Count of tool calls processed by the gateway
- **Gateway Starts**: Number of times the gateway has been started
- **MCP Server Count**: Number of configured downstream MCP servers
- **Rule Usage Flags**: Boolean flags indicating if AI request rules, request rules, AI response rules, and response rules are enabled

### How It Works

- Metrics state files (`installation-id`, `metrics-state`) are stored in `XDG_STATE_HOME/maybe-dont` (default: `~/.local/state/maybe-dont`)
- Metrics are reported once per day (24-hour interval) to Axiom
- Reporting is done via HTTPS POST to `https://api.axiom.co/v1/datasets/{dataset_name}/ingest`
- The gateway continues to function normally even if metrics reporting fails

### Opting Out

To disable metrics collection entirely, set the `MAYBEDONT_METRICS_OPTOUT` environment variable:

```bash
export MAYBEDONT_METRICS_OPTOUT=1
./maybe-dont start
```

When opted out:
- No installation ID is generated
- No metrics are tracked or stored
- No network requests are made for metrics reporting

### Configuration

Metrics configuration (Axiom dataset and API token) is built into the binary at compile time. This means:
- **Release builds**: Metrics collection is enabled by default (installation ID created on first run)
- **Development builds**: Metrics collection is disabled (no installation ID created) unless you build with `-ldflags "-X main.metricsDataset=... -X main.metricsAPIToken=..."`

## Environment Variable Substitution

The gateway supports environment variable substitution in configuration files using `${VAR_NAME}` syntax. This works for any string field in the configuration, including headers, URLs, and other string values.

### Examples:
```yaml
downstream_mcp_servers:
  github:
    type: http
    url: "${GITHUB_API_URL}"
    http:
      headers:
        Authorization: "Bearer ${GITHUB_TOKEN}"
        X-Custom-Header: "${CUSTOM_VALUE}"
```

This will expand to the values of the `GITHUB_API_URL`, `GITHUB_TOKEN`, and `CUSTOM_VALUE` environment variables at runtime.

## Multi-Client Configuration Example

```yaml
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

  aws-docs:
    type: http
    url: "https://knowledge-mcp.global.api.aws"
```

This configuration results in tools being exposed with prefixed names:
- GitHub tools: `github__create_issue`, `github__search_code`
- AWS tools: `aws-docs__search_documentation`, `aws-docs__read_documentation`

### Pass-Through Authentication
For HTTP/SSE clients, pass-through authentication allows extracting credentials from incoming request headers and forwarding them to downstream servers:
- **source_header**: Header name to extract from incoming requests
- **target_header**: Header name to send to downstream server
- **format**: Optional template for value formatting (e.g., `"Bearer {value}"`)

## Request Flow Architecture

1. **Client Registration**: `ClientManager.InitializeClients()` connects to all configured downstream MCP servers
2. **Capability Discovery**: Each client's tools/prompts/resources are discovered via MCP initialization
3. **Server Registration**: All capabilities are registered with prefixed names (`{client_name}__{original_name}`)
4. **Request Handling**:
   - Incoming requests are validated through the policy chain
   - Prefixed names are parsed to determine target client (`ParsePrefixedName()`)
   - Requests are routed to appropriate client with original (unprefixed) names
   - Responses are returned with prefixed names maintained

## Important Development Considerations

### Multi-Client Testing
- Config tests require at least one client in the `DownstreamMCPServers` map
- Test fixtures use minimal client config: `{"test": {Type: "stdio", Command: "echo"}}`
- Integration tests should verify name prefixing and routing behavior

### Client Lifecycle Management
- All clients are initialized during gateway startup
- Failed client initialization causes gateway startup failure
- Client connections are closed during gateway shutdown
- ClientManager handles thread-safe access to client instances

### Name Prefixing Rules
- Format: `{client_name}__{original_name}` using double underscore separator
- Applied to: Tools, Prompts, Resources, Resource Templates
- URI Templates require special handling due to `uritemplate.Template` type
- Validation policies can target specific clients using name prefixes