# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Maybe Don't Gateway is a security middleware service built in Go that acts as a protective gateway between LLM/AI agents and Model Context Protocol (MCP) servers. It intercepts and validates MCP requests using both deterministic rules (CEL-based policies) and AI-powered validation to prevent potentially dangerous operations.

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
1. YAML config file (`gateway-config.yaml`)
2. Environment variables (prefix: `MAYBE_DONT_`)
3. Command-line flags

### Security Rules
- **Request Rules**: Loaded from external `rules.yaml` file when `request_validation` is enabled (deterministic CEL-based rules)
- **AI Request Rules**: Loaded from external `ai_rules.yaml` file when `ai_request_validation` is enabled
- **Response Rules**: Loaded from external `response_rules.yaml` file when `response_validation` is enabled (deterministic CEL-based rules)
- **AI Response Rules**: Loaded from external `ai_response_rules.yaml` file when `ai_response_validation` is enabled
- **Multi-Client Validation**: Policies can target specific clients using name prefixes
- **Required When Enabled**: Rules files must be specified in config when their corresponding validation feature is enabled

## Important Development Notes

From `.cursor/rules/golang.mdc`:
- Always use the latest version of imports
- Run `go mod tidy` after dependency changes
- Ensure code compiles successfully
- All tests must pass
- Code must pass `golangci-lint run`
- Write unit tests where applicable
- CLI configuration goes in `cmd/` folder

## Testing Approach

The project uses Go's standard testing framework with testify for assertions. Key test files:
- `internal/gateway/cel_engine_test.go` - Request policy engine tests
- `internal/gateway/tool_validation_test.go` - Tool validation logic tests
- `internal/gateway/gateway_test.go` - Main gateway integration tests
- `internal/config/config_test.go` - Configuration loading tests

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

- Metrics are stored locally in `~/.maybe-dont/.maybedont-metrics` (or `./.maybedont-metrics` if home directory is unavailable)
- Metrics are reported once per day (24-hour interval) to Axiom if configured
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

Metrics configuration is built into the binary at compile time and cannot be modified by users. This ensures consistent metrics collection across all installations.

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
    http:
      headers:
        Authorization: "${GITHUB_TOKEN}"

  aws:
    type: stdio
    command: "uvx"
    args: ["awslabs.aws-documentation-mcp-server@latest"]
```

This configuration results in tools being exposed with prefixed names:
- GitHub tools: `github__create_issue`, `github__search_code`
- AWS tools: `aws__describe_instance`, `aws__list_buckets`

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