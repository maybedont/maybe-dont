# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Maybe Don't Gateway is a security middleware service built in Go that acts as a protective gateway between LLM/AI agents and Model Context Protocol (MCP) servers. It intercepts and validates MCP requests using both CEL-based policies and AI-powered validation to prevent potentially dangerous operations. The gateway supports OAuth 2.0 authentication (RFC 9728), multiple transport types, and concurrent multi-client connections with sophisticated retry logic.

## Essential Commands

### Build and Development
- `make build` - Build the binary
- `make test` - Run all tests
- `make lint` - Run golangci-lint (linter must be installed)
- `make clean` - Clean build artifacts
- `go test -v ./internal/gateway/...` - Run tests for specific package
- `go test -run TestName -v ./...` - Run a specific test
- `go mod tidy` - Clean up dependencies after changes

### Running the Gateway
- `./maybe-dont start` - Start the gateway with default config
- `./maybe-dont start --config gateway-config.yaml` - Start with specific config file
- `./maybe-dont version` - Show version information

### OAuth 2.0 Authentication
The gateway supports OAuth 2.0 authentication for HTTP and SSE servers:
- **Protected Resource Metadata**: Available at `/.well-known/oauth-protected-resource` (RFC 9728)
- **Token Validation**: Bearer tokens validated on `/mcp` endpoint
- **CORS Support**: Configurable CORS headers for .well-known endpoints

### Release Management
- `make bump-version` - Bump version using commitizen
- `make snapshot` - Create a snapshot release (for testing)

## Architecture and Key Components

### Core Structure
The gateway operates as a transparent proxy between MCP clients and multiple downstream MCP servers, implementing a validation chain approach with parallel processing:

1. **Gateway** (`internal/gateway/gateway.go`) - Main gateway orchestration and request handling
2. **Server** (`internal/gateway/server.go`) - Multi-transport server implementations (STDIO, HTTP, SSE)
3. **ClientManager** (`internal/gateway/client_manager.go`) - Manages multiple downstream MCP client connections with retry logic
4. **Tool Validation Chain** (`internal/gateway/tool_validation.go`) - Processes requests through multiple validation handlers
5. **CEL Engine** (`internal/gateway/cel_engine.go`) - Evaluates deterministic policy rules using Google's Common Expression Language
6. **AI Engine** (`internal/gateway/ai_engine.go`) - Uses OpenAI API for context-aware validation with concurrent policy evaluation
7. **OAuth Handler** (`internal/gateway/oauth.go`) - Implements OAuth 2.0 authentication and CORS for HTTP/SSE servers
8. **Audit Logging** (`internal/gateway/audit_logging.go`) - Comprehensive audit trail for all operations

### Multi-Client Architecture
The gateway supports multiple downstream MCP servers simultaneously with enhanced reliability:
- **ClientManager**: Manages multiple `ClientInfo` instances, each representing a connection to a downstream MCP server
- **Name Prefixing**: Tools, prompts, and resources are automatically prefixed with client names (`{client_name}__{original_name}`)
- **Request Routing**: Incoming requests with prefixed names are parsed and routed to the appropriate downstream client
- **Mixed Transport Types**: Each client can use different transport types (stdio, http, sse) simultaneously
- **Retry Logic**: Sophisticated retry mechanisms with exponential backoff for client initialization and capability discovery
- **Capability Discovery**: Delayed and retried capability discovery to handle startup race conditions

### MCP Transport Support
The gateway supports multiple MCP transport types per client:
- **STDIO** - Spawns processes and communicates via stdin/stdout
- **SSE** - Server-Sent Events for streaming connections
- **HTTP** - Standard HTTP requests/responses

### Server Configuration
The gateway supports three server modes:
- **STDIO**: Direct stdin/stdout communication for command-line usage
- **HTTP**: RESTful HTTP server with OAuth 2.0 support and CORS
- **SSE**: Server-Sent Events for streaming connections with TLS support

### OAuth 2.0 Integration
- **RFC 9728 Compliance**: Implements OAuth 2.0 Protected Resource Metadata specification
- **Bearer Token Authentication**: Validates tokens on protected endpoints (`/mcp`)
- **CORS Support**: Configurable CORS headers for `.well-known` endpoints
- **Token Validation**: Placeholder implementation ready for JWT or introspection endpoint integration

### Configuration Structure
- **Multiple Clients**: Configure multiple downstream servers in the `downstream_mcp_servers` map
- **Claude Desktop Compatibility**: Configuration format matches Claude Desktop's `mcpServers` structure
- **Client Naming**: Map keys serve as unique client names for identification and prefixing
- **Environment Variable Substitution**: Supports `${VAR_NAME}` syntax in configuration strings
- **Retry Configuration**: Configurable timeouts, retries, and delays for client initialization

### Configuration Hierarchy
Configuration is loaded in this order (later overrides earlier):
1. Embedded defaults
2. YAML config file (`gateway-config.yaml`)
3. Environment variables (prefix: `MCP_GATEWAY_`)
4. Command-line flags

### Validation Chain Architecture
The gateway implements a sophisticated validation chain that processes requests through multiple handlers:

1. **Tool Logging Handler**: Comprehensive audit logging of all tool calls
2. **CEL Validation Handler**: Rule-based validation using Common Expression Language
3. **AI Validation Handler**: AI-powered validation using OpenAI API

#### CEL Policy Engine
- **Custom Functions**: Includes `has()` and `get()` functions for safe field access
- **Policy Types**: Support for both `allow` and `deny` actions
- **Request Context**: Access to full request structure including method, params, and arguments

#### AI Policy Engine
- **Concurrent Evaluation**: Multiple AI policies evaluated in parallel with goroutines
- **Structured Responses**: Uses OpenAI's structured output with JSON schemas
- **Timeout Handling**: 30-second timeout per policy evaluation
- **Security-Focused Prompts**: Pre-configured policies for common security threats

### Security Rules and Policies
- **CEL Rules**: Embedded in binary, can be overridden via `cel_rules.yaml`
- **AI Rules**: Embedded in binary, can be overridden via `ai_rules.yaml`
- **Multi-Client Validation**: Policies can target specific clients using name prefixes
- **Error Handling**: User-friendly error messages with structured policy denial information

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
- `internal/gateway/cel_engine_test.go` - CEL policy engine tests
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
- `OPENAI_API_KEY` - OpenAI API key for AI validation (overrides config file setting)
- `GITHUB_TOKEN` - GitHub authentication token for GitHub MCP clients
- Environment variables follow the pattern: `MCP_GATEWAY_{CONFIG_PATH}` where CONFIG_PATH uses underscores

## Environment Variable Substitution

The gateway supports environment variable substitution in configuration files using `${VAR_NAME}` syntax. This works for any string field in the configuration, including headers, URLs, and other string values.

### Examples:
```yaml
server:
  type: http
  listen_addr: "0.0.0.0:8080"
  oauth:
    enabled: true
    authorization_server: "${OAUTH_AUTH_SERVER}"
    
downstream_mcp_servers:
  github:
    type: http
    url: "${GITHUB_API_URL}"
    http:
      headers:
        Authorization: "Bearer ${GITHUB_TOKEN}"
  aws-docs:
    type: stdio
    command: "uvx"
    args: ["awslabs.aws-documentation-mcp-server@latest"]
```

This will expand to the values of the environment variables at runtime. The substitution happens after configuration loading but before validation.

## Multi-Client Configuration Example

```yaml
server:
  type: http
  listen_addr: "0.0.0.0:8080"
  oauth:
    enabled: true
    authorization_server: "https://auth.example.com"
    cors:
      enabled: true
      allowed_origins:
        - "*"

downstream_mcp_servers:
  github:
    type: http
    url: "https://api.githubcopilot.com/mcp/"
    startup_timeout_ms: 30000
    initialization_retries: 5
    http:
      headers:
        Authorization: "Bearer ${GITHUB_TOKEN}"
  aws-docs:
    type: stdio
    command: "uvx"
    args: ["awslabs.aws-documentation-mcp-server@latest"]
    capability_discovery_delay_ms: 1000
    capability_discovery_retries: 3
```

This configuration results in tools being exposed with prefixed names:
- GitHub tools: `github__create_issue`, `github__search_code`
- AWS tools: `aws-docs__describe_instance`, `aws-docs__list_buckets`

## Request Flow Architecture

1. **Gateway Startup**: `Gateway.Start()` initializes the complete system
2. **Client Registration**: `ClientManager.InitializeClients()` connects to all configured downstream MCP servers with retry logic
3. **Capability Discovery**: Each client's tools/prompts/resources are discovered via MCP initialization with configurable delays
4. **Server Registration**: All capabilities are registered with prefixed names (`{client_name}__{original_name}`)
5. **Validation Chain Setup**: Tool validation chain is configured with enabled handlers (logging, CEL, AI)
6. **Request Handling**:
   - OAuth token validation (for HTTP/SSE servers)
   - Incoming requests are validated through the complete validation chain
   - Audit logging captures all request details and validation results
   - Prefixed names are parsed to determine target client (`ParsePrefixedName()`)
   - Requests are routed to appropriate client with original (unprefixed) names
   - Responses include validation metadata and are audited

## Important Development Considerations

### OAuth 2.0 Development
- Token validation is currently a placeholder implementation (`internal/gateway/oauth.go:109`)
- Implement JWT parsing or token introspection endpoint integration for production
- CORS configuration affects `.well-known` endpoints only
- OAuth metadata endpoint provides RFC 9728 compliant protected resource information

### Multi-Client Testing
- Config tests require at least one client in the `DownstreamMCPServers` map
- Test fixtures use minimal client config: `{"test": {Type: "stdio", Command: "echo"}}`
- Integration tests should verify name prefixing and routing behavior
- OAuth testing requires proper token setup for HTTP server tests

### Client Lifecycle Management
- All clients are initialized during gateway startup with sophisticated retry logic
- Failed client initialization logs errors but doesn't prevent gateway startup
- Client connections are closed during gateway shutdown
- ClientManager handles thread-safe access to client instances
- Retry parameters are configurable per client

### Validation Chain Development
- Validation handlers process requests in sequence: logging → CEL → AI
- Policy denied errors include structured data for client consumption
- AI policies are evaluated concurrently for performance
- Audit logs capture complete request lifecycle including validation results

### Name Prefixing Rules
- Format: `{client_name}__{original_name}` using double underscore separator
- Applied to: Tools, Prompts, Resources, Resource Templates
- URI Templates require special handling due to `uritemplate.Template` type
- Validation policies can target specific clients using name prefixes
- ParsePrefixedName and PrefixName utility functions handle the conversion