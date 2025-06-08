# MCP Security Proxy Design Document

## Product Overview

The MCP Security Proxy is a Go-based middleware service that provides enterprise-grade security controls for Model Context Protocol (MCP) communications. It acts as a transparent proxy between MCP clients and servers, enforcing security policies, validating requests, and providing comprehensive audit logging.

### Core Value Proposition

- **Zero-trust security model** for MCP communications
- **Drop-in replacement** for existing MCP servers with no client modifications
- **Policy-as-code** approach to security rules
- **Enterprise-ready** with authentication, authorization, and audit trails
- **Transport agnostic** supporting STDIO and SSE in MVP, with HTTP and WebSocket coming later

### Target Users

1. **Security Teams**: Need visibility and control over AI model interactions
2. **Platform Engineers**: Require centralized policy enforcement
3. **Compliance Officers**: Must maintain audit trails for regulatory requirements
4. **Development Teams**: Want seamless integration without changing existing tools

## Technical Requirements

### Required Dependencies

The implementation MUST use the following libraries:

- **[mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)**: Official Go SDK for MCP protocol implementation
- **[uber-go/zap](https://github.com/uber-go/zap)**: Structured logging with high performance
- **[spf13/cobra](https://github.com/spf13/cobra)**: CLI interface and command structure
- **[spf13/viper](https://github.com/spf13/viper)**: Configuration management with multiple sources

### Architecture Requirements

```
┌─────────────────┐    ┌─────────────────────┐    ┌─────────────────┐
│   MCP Client    │◄──►│  Security Proxy     │◄──►│   MCP Server    │
│                 │    │                     │    │                 │
│ (Unmodified)    │    │ • Authentication    │    │ (Any MCP impl) │
│                 │    │ • Authorization     │    │                 │
│                 │    │ • Validation        │    │                 │
│                 │    │ • Audit Logging     │    │                 │
│                 │    │ • Rate Limiting     │    │                 │
└─────────────────┘    └─────────────────────┘    └─────────────────┘
```

### Configuration Management

The proxy MUST support configuration through three mechanisms with the following precedence:

1. **Command-line flags** (highest priority)
2. **Environment variables** (with `MCP_PROXY_` prefix)
3. **Configuration file** (YAML format)
4. **Default values** (lowest priority)

Configuration search paths:
- Current directory: `./config.yaml`
- User home: `~/.mcp-proxy/config.yaml`
- System-wide: `/etc/mcp-proxy/config.yaml`
- Custom path via `--config` flag

### CLI Design Requirements

#### Primary Commands

- `mcp-security-proxy start` - Launch the proxy server
- `mcp-security-proxy validate` - Validate configuration without starting
- `mcp-security-proxy version` - Display version and build information
- `mcp-security-proxy test` - Test security policies against sample requests

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

### Logging Requirements

#### Structured Logging with Zap

All logs MUST be structured JSON by default with the following requirements:

1. **Standard Fields** (always present):
   - `timestamp` - ISO8601 format
   - `level` - Log level
   - `service` - Always "mcp-security-proxy"
   - `version` - Proxy version
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
   - `policy_violations` - Array of violated policies
   - `access_decision` - allow/deny with reason

#### Log Levels

- **ERROR**: Security violations, authentication failures, downstream errors
- **WARN**: Policy violations, rate limits, suspicious patterns
- **INFO**: Request summaries, connection events, configuration changes
- **DEBUG**: Full payloads (redacted), validation logic, performance metrics

#### Debug Mode Requirements

When `log-level: debug`, the proxy MUST:
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

The proxy MUST support multiple authentication mechanisms:

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

Validation rules that can:
- Whitelist/blacklist specific tools
- Restrict file system paths
- Limit response sizes
- Scan for secrets/sensitive data
- Apply rate limits
- Validate request parameters

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
- Authorization decision and reasoning
- Response summary (success/failure, size)
- Duration and performance metrics

#### Audit Destinations

- Local file (with rotation)
- Syslog
- Stdout/stderr (for container deployments)
- Future: Elasticsearch, Splunk, SIEM integrations

### Performance Requirements

- Latency overhead: <10ms for policy evaluation
- Memory usage: <100MB baseline, linear with concurrent connections
- Connection handling: 1000+ concurrent clients
- Startup time: <1 second to ready state

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
```

### Full Configuration Structure

```yaml
# Proxy listener configuration
proxy:
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
  validation:
    enabled: bool
    rules: []
    
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
3. **Policy Violations**: Detailed error messages with violation reasons
4. **System Errors**: Graceful degradation without data loss

### Security Operations

1. **Key Rotation**: Support hot-reload of authentication keys
2. **Policy Updates**: Apply new rules without restart
3. **Incident Response**: Detailed audit trail for forensics
4. **Rate Limiting**: Prevent DoS with configurable limits

## Future Considerations (Post-MVP)

### Protocol Enhancements

1. **Streaming Support**: Handle long-running MCP operations with chunked responses
2. **Bidirectional Communication**: Server-initiated messages and notifications  
3. **WebSocket Transport**: Real-time communication with connection persistence
4. **Protocol Negotiation**: Support multiple MCP versions simultaneously

### Advanced Security

1. **Multi-Factor Authentication**: Combine multiple auth methods
2. **Dynamic Authorization**: Context-aware permissions (time, location, risk score)
3. **Anomaly Detection**: ML-based detection of unusual patterns
4. **Secret Scanning**: Deep content inspection for leaked credentials

### Enterprise Features

1. **Multi-Tenancy**: Isolated proxy instances with shared infrastructure
2. **High Availability**: Active-active clustering with state synchronization
3. **Compliance Modes**: Pre-configured policies for HIPAA, PCI, SOC2
4. **Integration Ecosystem**: Plugins for SIEM, SOAR, and identity providers

### Performance Optimizations

1. **Connection Pooling**: Reuse downstream connections
2. **Request Caching**: Cache safe, idempotent operations
3. **Zero-Copy Proxying**: Direct memory transfer for large payloads
4. **Hardware Acceleration**: Crypto operations offloading

## Success Metrics

1. **Adoption**: Number of deployments and active users
2. **Security**: Prevented policy violations and blocked attacks
3. **Performance**: P99 latency overhead below 10ms
4. **Reliability**: 99.9% uptime for proxy service
5. **Compliance**: Successful audits with complete trail

## Development Priorities

### Phase 1 (MVP)
- Core proxy functionality with STDIO and SSE transports
- Basic authentication (API key)
- File path validation
- Structured logging with zap
- CLI with cobra/viper

### Phase 2
- HTTP transport support
- JWT authentication
- Advanced validation rules
- Prometheus metrics
- Docker packaging

### Phase 3
- mTLS authentication
- Multi-tenancy support
- WebSocket transport
- SIEM integrations
- HA clustering