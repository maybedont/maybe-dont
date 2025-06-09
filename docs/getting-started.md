# Getting Started with Maybe Don't

This guide will help you get up and running with Maybe Don't quickly.

## Installation

### Using Docker (Recommended)

```bash
# Pull the image
docker pull ghcr.io/sudermanjr/maybe-dont:latest

# Or build from source
git clone https://github.com/sudermanjr/maybe-dont.git
cd maybe-dont
docker build -t maybe-dont .
```

### From Source

```bash
# Clone the repository
git clone https://github.com/sudermanjr/maybe-dont.git
cd maybe-dont

# Build the binary
go build -o maybe-dont ./cmd/maybe-dont
```

## Quick Start

1. Create a basic configuration file `config.yaml`:

```yaml
server:
  type: http
  listen_addr: ":8080"
  log_level: info
  log_format: json

policy_validation:
  enabled: true
  rules_file: cel_rules.yaml

ai_validation:
  enabled: true
  endpoint: https://api.openai.com/v1
  model: gpt-4-turbo-preview
  rules_file: ai_rules.yaml
  api_key: ${OPENAI_API_KEY}

audit:
  path: logs/audit.log
  format: json
```

2. Start the proxy:

```bash
# Using Docker
docker run -d \
  --name maybe-dont \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/cel_rules.yaml:/app/cel_rules.yaml \
  -v $(pwd)/ai_rules.yaml:/app/ai_rules.yaml \
  -v $(pwd)/logs:/app/logs \
  -e OPENAI_API_KEY=your_api_key \
  maybe-dont

# Or using the binary
./maybe-dont
```

3. Test the proxy:

```bash
# Send a test request
curl -X POST http://localhost:8080/tools/call \
  -H "Content-Type: application/json" \
  -d '{
    "params": {
      "name": "kubectl",
      "arguments": {
        "command": "delete namespace test"
      }
    }
  }'
```

## Basic Configuration

### Server Types

Maybe Don't supports three server types:

1. **HTTP** (default):
```yaml
server:
  type: http
  listen_addr: ":8080"
```

2. **SSE** (Server-Sent Events):
```yaml
server:
  type: sse
  listen_addr: ":8080"
  sse:
    tls:
      enabled: true
      cert_file: /path/to/cert.pem
      key_file: /path/to/key.pem
```

3. **STDIO**:
```yaml
server:
  type: stdio
```

### Authentication

Configure authentication in the `auth` section:

```yaml
auth:
  type: api_key  # api_key, jwt, or mtls
  api_key: your-api-key  # for api_key type
  jwt:
    jwks_url: https://your-domain/.well-known/jwks.json
    issuer: your-issuer
    audience: ["your-audience"]
  mtls:
    ca_file: /path/to/ca.pem
    cert_file: /path/to/cert.pem
    key_file: /path/to/key.pem
```

### Logging

Configure logging in the `server` section:

```yaml
server:
  log_level: info  # debug, info, warn, error
  log_format: json  # json or text
```

### Audit Logging

Configure audit logging in the `audit` section:

```yaml
audit:
  path: logs/audit.log
  format: json  # json or text
```

## Next Steps

- Read the [Architecture](./architecture.md) guide to understand how Maybe Don't works
- Check out the [Policy Engine](./policy-engine.md) documentation to learn about rules
- See the [Built-in Rules](./rules.md) for examples and reference
- Review [Security](./security.md) best practices 