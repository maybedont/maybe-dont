# MCP Security Gateway Design Document

## Product Overview

The MCP Security Gateway is a Go-based middleware service that provides enterprise-grade security controls for Model Context Protocol (MCP) communications. It acts as a transparent gateway between MCP clients and servers, enforcing security policies, validating requests, and providing comprehensive audit logging.

### Core Value Proposition

- **Zero-trust security model** for MCP communications
- **Drop-in replacement** for existing MCP servers with no client modifications
- **Policy-as-code** approach using industry-standard CEL (Common Expression Language)
- **Enterprise-ready** with authentication, authorization, and audit trails
- **Transport agnostic** supporting STDIO and SSE in MVP, with HTTP and WebSocket coming later
- **Familiar policy language** - same as Kubernetes, Istio, and other cloud-native tools

### Target Users

1. **Security Teams**: Need visibility and control over AI model interactions with familiar CEL policy language
2. **Platform Engineers**: Require centralized policy enforcement using cloud-native standards
3. **Compliance Officers**: Must maintain audit trails for regulatory requirements
4. **Development Teams**: Want seamless integration without changing existing tools

## Technical Requirements

### Required Dependencies

The implementation MUST use the following libraries:

- **[mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)**: Official Go SDK for MCP protocol implementation
- **[uber-go/zap](https://github.com/uber-go/zap)**: Structured logging with high performance
- **[spf13/cobra](https://github.com/spf13/cobra)**: CLI interface and command structure
- **[spf13/viper](https://github.com/spf13/viper)**: Configuration management with multiple sources
- **[google/cel-go](https://github.com/google/cel-go)**: Common Expression Language for policy evaluation

### Architecture Requirements

```
┌─────────────────┐    ┌─────────────────────┐    ┌─────────────────┐
│   MCP Client    │◄──►│  Security Gateway   │◄──►│   MCP Server    │
│                 │    │                     │    │                 │
│ (Unmodified)    │    │ • Authentication    │    │ (Any MCP impl)  │
│                 │    │ • Authorization     │    │                 │
│                 │    │ • Validation        │    │                 │
│                 │    │ • Audit Logging     │    │                 │
│                 │    │ • Rate Limiting     │    │                 │
└─────────────────┘    └─────────────────────┘    └─────────────────┘
```

### Configuration Management

The gateway MUST support configuration through three mechanisms with the following precedence:

1. **Command-line flags** (highest priority)
2. **Environment variables** (with `MCP_PROXY_` prefix)
3. **Configuration file** (YAML format)
4. **Default values** (lowest priority)

Configuration search paths:
- Current directory: `./config.yaml`
- User home: `~/.mcp-gateway/config.yaml`
- System-wide: `/etc/mcp-gateway/config.yaml`
- Custom path via `--config` flag

### CLI Design Requirements

#### Primary Commands

- `mcp-security-gateway start` - Launch the gateway server
- `mcp-security-gateway validate` - Validate configuration and compile CEL policies
- `mcp-security-gateway version` - Display version and build information
- `mcp-security-gateway test` - Test CEL policies against sample requests

#### Global Flags

- `--config, -c` - Configuration file path
- `--log-level, -l` - Logging verbosity (debug, info, warn, error)
- `--log-format` - Output format (json, text)
- `--verbose, -v` - Enable verbose output

#### Start Command Flags

- `--dry-run` - Validate configuration and exit
- `--listen-addr` - Override listen address
- `--downstream-url` - Override downstream server URL
- `--auth-type` - Override authentication type

#### Test Command

The `test` command allows validation of CEL policies without running the gateway:

```bash
# Test policies against a request file
mcp-security-gateway test --config config.yaml --request request.json

# Test policies interactively
mcp-security-gateway test --config config.yaml --interactive

# Test with specific auth context
mcp-security-gateway test --config config.yaml --auth-context '{"client_id": "test", "roles": ["user"]}'
```

The test command must:
- Load and compile all CEL expressions
- Validate expression syntax and types
- Execute policies against test requests
- Show which rules match and their outcomes
- Measure policy evaluation performance
- Support exporting test cases for CI/CD pipelines
- Allow importing policy test suites

### Logging Requirements

#### Structured Logging with Zap

All logs MUST be structured JSON by default with the following requirements:

1. **Standard Fields** (always present):
   - `timestamp` - ISO8601 format
   - `level` - Log level
   - `service` - Always "mcp-security-gateway"
   - `version` - Gateway version
   - `request_id` - Unique request identifier
   - `client_id` - Authenticated client identifier

2. **Contextual Fields** (when applicable):
   - `method` - MCP method being called
   - `tool` - Tool name for tool calls
   - `resource` - Resource URI for resource access
   - `duration_ms` - Operation duration
   - `error` - Error details if applicable

3. **Security Fields**:
   - `auth_type` - Authentication method used
   - `policy_results` - Array of CEL policy evaluations
   - `policy_denied` - Name of denying policy (if any)
   - `access_decision` - allow/deny with reason

#### Log Levels

- **ERROR**: Security violations, authentication failures, downstream errors
- **WARN**: Policy violations, rate limits, suspicious patterns
- **INFO**: Request summaries, connection events, configuration changes
- **DEBUG**: Full payloads (redacted), validation logic, performance metrics

#### Debug Mode Requirements

When `log-level: debug`, the gateway MUST:
- Log complete request/response payloads with sensitive data redacted
- Include all validation decisions with reasoning
- Show authentication/authorization flow
- Display configuration resolution from all sources
- Include performance timing for each component

#### Sensitive Data Handling

The logger MUST automatically redact:
- Passwords, tokens, API keys
- Personally identifiable information (PII)
- File contents containing secrets
- Custom patterns defined in configuration

### Security Requirements

#### Authentication

The gateway MUST support multiple authentication mechanisms:

1. **API Key Authentication**
   - Header-based or query parameter
   - Support for multiple keys with different permissions
   - Key rotation without downtime

2. **JWT Authentication**
   - JWKS endpoint support for key discovery
   - Token validation with issuer and audience checks
   - Custom claim mapping to roles

3. **mTLS Authentication**
   - Client certificate validation
   - Certificate attribute extraction for authorization
   - CRL/OCSP support for revocation

4. **OAuth2 (Future)**
   - Support major providers (Google, GitHub, Azure AD)
   - Custom OAuth2 providers
   - Token introspection

#### Authorization

Role-based access control (RBAC) with:
- Tool-level permissions (allow/deny specific MCP tools)
- Resource-level permissions (path-based access control)
- Rate limiting per role
- Default-deny option for zero-trust model

#### Policy Engine

The gateway MUST use **Common Expression Language (CEL)** for policy rules, providing:

- **Expressive policies**: Write complex conditions in a familiar, type-safe language
- **Performance**: CEL compiles to efficient evaluation programs
- **Type safety**: Compile-time type checking prevents runtime errors
- **Ecosystem**: Leverage existing CEL tooling and knowledge
- **Standard language**: Same policy language as Kubernetes, Envoy, and other infrastructure
- **Extensibility**: Add custom functions and macros for domain-specific logic

Example policy capabilities with CEL:
- Tool allowlisting: `request.method == "tools/call" && request.params.name in ["read_file", "list_directory"]`
- Path restrictions: `request.params.uri.startsWith("/safe/") || request.params.uri.startsWith("/public/")`
- Rate limiting: `rateLimit("client:" + auth.client_id, 100, "1m")`
- Content filtering: `size(response.content) < 10 * 1024 * 1024 && !hasSecrets(response.content)`
- Time-based access: `now().getHours() >= 9 && now().getHours() <= 17`
- Role-based access: `"admin" in auth.roles || (request.method == "resources/read" && "reader" in auth.roles)`

Policy evaluation context must include:
- `request`: The incoming MCP request object
- `auth`: Authentication context (client_id, roles, metadata)
- `response`: The MCP response (for post-processing rules)
- `now()`: Current timestamp function
- Custom functions: `hasSecrets()`, `rateLimit()`, `matchesPattern()`
- Environment info: `env.hostname`, `env.region`, `env.deployment`

### Transport Requirements

#### MVP Transports

1. **STDIO**: Process spawning with bidirectional communication
2. **SSE**: Server-sent events for streaming

#### Phase 2 Transport

3. **HTTP**: REST-style request/response

#### Configuration by Transport

- STDIO: Command path, arguments, environment variables
- SSE: URL, headers, TLS settings, timeouts
- HTTP (Phase 2): URL, headers, TLS settings, timeouts

### Audit Requirements

#### Audit Log Contents

Every request MUST generate an audit entry with:
- Timestamp (nanosecond precision)
- Client identification (ID, IP, authentication method)
- Request details (method, parameters)
- Policy evaluation results:
  - CEL expressions evaluated
  - Expression results (allow/deny)
  - Evaluation time per expression
  - Variables used in evaluation
- Authorization decision and reasoning
- Response summary (success/failure, size)
- Duration and performance metrics

#### Audit Destinations

- Local file (with rotation)
- Syslog
- Stdout/stderr (for container deployments)
- Future: Elasticsearch, Splunk, SIEM integrations

### Performance Requirements

- Latency overhead: <10ms for policy evaluation (including CEL expression execution)
- CEL compilation: Expressions compiled once at startup/reload
- CEL optimization: Use CEL's built-in optimization for frequently evaluated expressions
- Memory usage: <100MB baseline, linear with concurrent connections
- Connection handling: 1000+ concurrent clients
- Startup time: <1 second to ready state (including CEL compilation)

### Deployment Requirements

#### Deployment Modes

1. **CLI Tool**: Direct execution by developers
2. **Systemd Service**: Production Linux deployments
3. **Container**: Docker/Kubernetes ready
4. **Sidecar**: Alongside MCP servers in pods

#### Zero Downtime Operations

- Configuration hot-reload via SIGHUP
- Graceful shutdown with connection draining
- Health check endpoints
- Prometheus metrics endpoint

## Configuration Schema

### Minimal Configuration

```yaml
server:
  transport: stdio
  command: mcp-server-filesystem
  args: ["--root", "/safe-directory"]

# Optional: Add basic security policy
policies:
  rules:
    - name: "allow-read-only"
      expression: 'request.method in ["resources/read", "resources/list"]'
      action: allow
    - name: "deny-all-others"
      expression: "true"
      action: deny
```

### Full Configuration Structure

```yaml
# Gateway listener configuration
gateway:
  listen:
    transport: stdio|sse    # http in Phase 2
    address: ":8080"       # For SSE (and HTTP in Phase 2)
    
  health:
    enabled: true
    endpoint: /health
    
  metrics:
    enabled: true
    endpoint: /metrics

# Downstream MCP server
server:
  transport: stdio|sse      # http in Phase 2
  # For stdio transport
  command: string
  args: []string
  env: map[string]string
  # For SSE transport (and HTTP in Phase 2)
  url: string
  headers: map[string]string
  timeout: duration

# Security configuration  
security:
  auth:
    type: none|apikey|jwt|mtls
    required: bool
    config: {}  # Type-specific configuration
    
  tls:
    enabled: bool
    cert_file: string
    key_file: string
    ca_file: string
    
  permissions:
    default_deny: bool
    roles: []
      
# Policy configuration
policies:
  # CEL language version (for future compatibility)
  cel_version: "v0.20.0"
  
  # CEL policies evaluated in order, first deny wins
  rules:
    - name: "deny-dangerous-tools"
      description: "Block potentially dangerous tool calls"
      expression: |
        request.method == "tools/call" && 
        request.params.name in ["execute_command", "write_file", "delete_file"]
      action: deny
      message: "Tool {{request.params.name}} is not allowed"
      
    - name: "restrict-file-paths"
      description: "Limit file access to safe directories"
      expression: |
        request.method in ["resources/read", "tools/call"] &&
        has(request.params.uri) &&
        !(request.params.uri.startsWith("/safe/") || 
          request.params.uri.startsWith("/public/"))
      action: deny
      message: "Access to {{request.params.uri}} is not permitted"
      
    - name: "rate-limit-by-client"
      description: "Limit requests per client"
      expression: |
        !rateLimit("client:" + auth.client_id, 100, duration("1m"))
      action: deny
      message: "Rate limit exceeded"
      
    - name: "business-hours-only"
      description: "Allow access only during business hours"
      expression: |
        auth.roles.contains("contractor") &&
        (now().getHours() < 9 || now().getHours() > 17)
      action: deny
      message: "Access permitted only during business hours"
      
    - name: "response-size-limit"
      description: "Limit response sizes"
      expression: |
        has(response.size) && response.size > 10 * 1024 * 1024
      phase: response  # Evaluate after response
      action: deny
      message: "Response too large: {{response.size}} bytes"
      
  # Custom CEL functions available in expressions
  functions:
    - name: "rateLimit"
      description: "Check rate limit for a key"
    - name: "hasSecrets" 
      description: "Scan content for potential secrets"
    - name: "matchesPattern"
      description: "Regex pattern matching"
    
# Logging configuration
logging:
  level: debug|info|warn|error
  format: json|text
  output: stdout|stderr|file
  file: string
  
# Audit configuration  
audit:
  enabled: bool
  output: file|syslog|stdout
  file:
    path: string
    rotate: bool
    max_size: string
    max_age: duration
```

## Operational Requirements

### Monitoring & Observability

1. **Health Checks**
   - `/health` endpoint with downstream connectivity status
   - Readiness vs liveness probes for Kubernetes

2. **Metrics (Prometheus format)**
   - Request rate by method, client, and result
   - Request duration histograms
   - Active connection gauge
   - Policy violation counters
   - Authentication failure rate

3. **Distributed Tracing (Future)**
   - OpenTelemetry support
   - Trace context propagation
   - Span creation for each validation step

### Error Handling

1. **Client Errors**: Return MCP-compliant error responses
2. **Downstream Errors**: Circuit breaker pattern with fallback
3. **Policy Violations**: Return custom message from CEL rule with violation details
4. **System Errors**: Graceful degradation without data loss

### Security Operations

1. **Key Rotation**: Support hot-reload of authentication keys
2. **Policy Updates**: Hot-reload CEL policies with syntax validation
3. **Incident Response**: Detailed audit trail with policy decisions
4. **Rate Limiting**: CEL-based dynamic rate limits

## Future Considerations (Post-MVP)

### Protocol Enhancements

1. **Streaming Support**: Handle long-running MCP operations with chunked responses
2. **Bidirectional Communication**: Server-initiated messages and notifications  
3. **WebSocket Transport**: Real-time communication with connection persistence
4. **Protocol Negotiation**: Support multiple MCP versions simultaneously

### Advanced Security

1. **Multi-Factor Authentication**: Combine multiple auth methods
2. **Dynamic Authorization**: Context-aware CEL expressions with external data sources
3. **Anomaly Detection**: CEL expressions analyzing request patterns
4. **Secret Scanning**: Deep content inspection via CEL custom functions
5. **Policy Composition**: Hierarchical CEL policies with inheritance

### Enterprise Features

1. **Multi-Tenancy**: Isolated gateway instances with shared infrastructure
2. **High Availability**: Active-active clustering with state synchronization
3. **Compliance Modes**: Pre-configured policies for HIPAA, PCI, SOC2
4. **Integration Ecosystem**: Plugins for SIEM, SOAR, and identity providers

### Performance Optimizations

1. **Connection Pooling**: Reuse downstream connections
2. **Request Caching**: Cache safe, idempotent operations
3. **Zero-Copy Gatewaying**: Direct memory transfer for large payloads
4. **Hardware Acceleration**: Crypto operations offloading

## Success Metrics

1. **Adoption**: Number of deployments and active users
2. **Security**: Policy violations caught, attacks prevented
3. **Performance**: P99 latency overhead below 10ms (including CEL evaluation)
4. **Reliability**: 99.9% uptime for gateway service
5. **Compliance**: Successful audits with complete trail
6. **Policy Effectiveness**: Percentage of requests evaluated, false positive rate

## Development Priorities

### Phase 1 (MVP)
- Core gateway functionality with STDIO and SSE transports
- Basic authentication (API key)
- CEL-based policy engine with common security rules
- File path validation via CEL expressions
- Structured logging with zap
- CLI with cobra/viper
- Policy testing framework

### Phase 2
- HTTP transport support
- JWT authentication
- Advanced CEL features (macros, external data sources)
- Prometheus metrics with CEL policy performance
- Docker packaging
- Policy library and templates

### Phase 3
- mTLS authentication
- Multi-tenancy support
- WebSocket transport
- SIEM integrations
- HA clustering
