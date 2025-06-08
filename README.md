# MCP Security Proxy

A Go-based middleware service that provides enterprise-grade security controls for Model Context Protocol (MCP) communications. The proxy acts as a transparent intermediary between MCP clients and servers, enforcing security policies, validating requests, and providing comprehensive audit logging.

## Features

- **Zero-trust security model** for MCP communications
- **Drop-in replacement** for existing MCP servers with no client modifications
- **Policy-as-code** approach using industry-standard CEL (Common Expression Language)
- **Enterprise-ready** with authentication, authorization, and audit trails
- **Transport agnostic** supporting STDIO and SSE in MVP, with HTTP and WebSocket coming later
- **Familiar policy language** - same as Kubernetes, Istio, and other cloud-native tools

## Installation

```bash
# Build from source
go build -o maybe-dont

# Or use the pre-built binary
# Download from releases page
```

## Configuration

The proxy supports multiple configuration sources with the following precedence:

1. Command-line flags (highest priority)
2. Environment variables (with `MCP_PROXY_` prefix)
3. Configuration file (YAML format) - This is the recommended method for any non-secrets.
4. Default values (lowest priority)

### Configuration File Locations

The proxy searches for `config.yaml` in the following locations:
- User home: `~/.maybe-dont/config.yaml`
- System-wide: `/etc/maybe-dont/config.yaml`
- Custom path via `--config` flag

### Example Configuration

See [config.yaml](./config.yaml) for a complete example with comment notes.

## Usage

### Basic Usage

```bash
# Start with config file
./maybe-dont start --config /path/to/config.yaml

# Start with debug logging
./maybe-dont start --log-level debug
```

### Command Line Options

Use the `--help` flag on any command to see the usage.

### Policy Testing

```bash
# Test policies against a request file
./maybe-dont test --config config.yaml --request request.json

# Test policies interactively
./maybe-dont test --config config.yaml --interactive

# Test with specific auth context
./maybe-dont test --config config.yaml --auth-context '{"client_id": "test", "roles": ["user"]}'
```

## Security Features

### Authentication Methods
- API Key Authentication
- JWT Authentication
- mTLS Authentication
- OAuth2 (Future)

### Authorization
- Role-based access control (RBAC)
- Tool-level permissions
- Resource-level permissions
- Rate limiting per role
- Default-deny option for zero-trust model

### Policy Engine
The proxy uses Common Expression Language (CEL) for policy rules, providing:
- Expressive policies with type safety
- High-performance evaluation
- Familiar syntax for cloud-native users
- Extensible with custom functions

## Transport Options

### MVP Transports
1. **STDIO**: Process spawning with bidirectional communication
2. **SSE**: Server-sent events for streaming

### Phase 2 Transport
3. **HTTP**: REST-style request/response

## Audit Logging

The proxy provides comprehensive audit logging with:
- Timestamp (nanosecond precision)
- Client identification
- Request details
- Policy evaluation results
- Structured JSON output

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting pull requests.

## License

[License information to be added] 