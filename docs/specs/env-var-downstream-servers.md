# Environment Variable Configuration for Downstream MCP Servers

## Overview

Enable full configuration of `downstream_mcp_servers` via environment variables while preserving the existing map-based YAML structure.

## Goals

1. Allow complete gateway configuration via environment variables (no YAML file required)
2. Keep the map-based YAML structure for ergonomics and Claude Desktop compatibility
3. Support both explicit indexed format and compact format for headers
4. Maintain existing validation behavior
5. Provide helpful error messages that include both YAML path and env var equivalent

## Non-Goals

1. Changing the YAML config structure
2. Supporting list-based YAML configuration (keep map only)

## Configuration File Behavior

**The configuration file is optional.** The gateway should:
- Not error when no config file is found
- Only error when a required value is missing (regardless of source)
- Only error when a configured value is invalid

This allows users to configure the gateway entirely via environment variables without needing a YAML file.

## Environment Variable Format

### Prefix Pattern

All downstream server env vars use the prefix:
```
MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_{CLIENT_NAME}_{FIELD_PATH}
```

### Client Name Extraction

Client names in env vars use underscores, converted to lowercase with underscores replaced by hyphens:
- `GITHUB` → `github`
- `AWS_DOCS` → `aws-docs`
- `MY_CUSTOM_SERVER` → `my-custom-server`

**Limitation:** Client names containing literal underscores (e.g., `aws_docs` in YAML) cannot be configured via environment variables. The underscore-to-hyphen conversion means env vars can only define clients with hyphenated names. This is an acceptable trade-off since:
- Hyphenated names are more common and conventional
- YAML configuration remains available for edge cases requiring underscores
- The conversion is deterministic and well-documented

### Field Path Mapping

After extracting the client name, the remaining suffix maps to `ClientConfig` struct fields via mapstructure tags.

For example: `MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE=http`
- Full env var: `MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE`
- Client name: `GITHUB` → `github`
- Field path: `TYPE` → `type`

| Field Path Suffix | Maps To | Type |
|-------------------|---------|------|
| `TYPE` | `type` | string |
| `URL` | `url` | string |
| `DOWNSTREAM_URL` | `downstream_url` | string |
| `COMMAND` | `command` | string |
| `ARGS` | `args` | []string (comma-separated) |
| `COMMAND_ARGS` | `command_args` | []string (comma-separated) |
| `STARTUP_TIMEOUT_MS` | `startup_timeout_ms` | int |
| `INITIALIZATION_RETRIES` | `initialization_retries` | int |
| `RETRY_DELAY_MS` | `retry_delay_ms` | int |
| `CAPABILITY_DISCOVERY_DELAY_MS` | `capability_discovery_delay_ms` | int |
| `CAPABILITY_DISCOVERY_RETRIES` | `capability_discovery_retries` | int |
| `CAPABILITY_RETRY_DELAY_MS` | `capability_retry_delay_ms` | int |
| `HTTP_HEADERS_{HEADER_NAME}` | `http.headers[header_name]` | string |
| `SSE_HEADERS_{HEADER_NAME}` | `sse.headers[header_name]` | string |
| `AUTH_PASS_THROUGH_ENABLED` | `auth.pass_through.enabled` | bool |
| `AUTH_PASS_THROUGH_HEADERS` | `auth.pass_through.headers` | compact format |
| `AUTH_PASS_THROUGH_HEADERS_{N}_SOURCE_HEADER` | `auth.pass_through.headers[n].source_header` | string |
| `AUTH_PASS_THROUGH_HEADERS_{N}_TARGET_HEADER` | `auth.pass_through.headers[n].target_header` | string |
| `AUTH_PASS_THROUGH_HEADERS_{N}_FORMAT` | `auth.pass_through.headers[n].format` | string |

## Header Configuration Formats

### Indexed Format (YAML-equivalent)

Explicit mapping that mirrors YAML structure:

```bash
MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_SOURCE_HEADER=X-GitHub-Token
MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_TARGET_HEADER=Authorization
MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_FORMAT=Bearer {value}
```

Equivalent YAML:
```yaml
downstream_mcp_servers:
  github:
    auth:
      pass_through:
        headers:
          - source_header: X-GitHub-Token
            target_header: Authorization
            format: "Bearer {value}"
```

**Validation**: Same as YAML - `source_header` and `target_header` required, `format` optional.

### Compact Format

Condensed single-variable format for common cases:

```bash
# Single header: source:target[:format]
MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS=X-GitHub-Token:Authorization:Bearer {value}

# Multiple headers: separated by semicolon
MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS=X-GitHub-Token:Authorization:Bearer {value};X-Other:Other-Header

# Multiple headers example with mixed formats
MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_MYAPI_AUTH_PASS_THROUGH_HEADERS=X-API-Key:Authorization;X-Tenant:X-Downstream-Tenant:Tenant {value}
```

**Format syntax:**
```
header_mapping     = source_header ":" target_header [ ":" format ]
compact_value      = header_mapping { ";" header_mapping }
```

**Parsing rules:**
1. Split by `;` (semicolon) to get individual header mappings
2. For each mapping, split by `:` (colon) into max 3 parts:
   - Part 1: `source_header` (required)
   - Part 2: `target_header` (required)
   - Part 3: `format` (optional, defaults to `{value}` for raw passthrough)

**Validation**: Each header mapping must contain at least one `:` (colon). Error message if missing:
```
Invalid compact header format for client 'github': value must contain at least one colon (format: source_header:target_header[:format])
```

### Format Detection

When processing `AUTH_PASS_THROUGH_HEADERS`:
1. Check if any indexed env vars exist (`_HEADERS_0_SOURCE_HEADER`, etc.)
2. If indexed vars exist → use indexed format
3. If no indexed vars but `_HEADERS` exists → use compact format
4. If both exist → indexed takes precedence (more explicit)

## Complete Example

### Environment Variables Only

```bash
# Server config
export MAYBE_DONT_SERVER_TYPE=http
export MAYBE_DONT_SERVER_LISTEN_ADDR=0.0.0.0:8080

# Validation config
export MAYBE_DONT_REQUEST_VALIDATION_CEL_MODE=enabled
export MAYBE_DONT_REQUEST_VALIDATION_CEL_RULES_FILE=cel_request_rules.yaml
export MAYBE_DONT_REQUEST_VALIDATION_AI_MODE=disabled
export MAYBE_DONT_RESPONSE_VALIDATION_CEL_MODE=disabled
export MAYBE_DONT_RESPONSE_VALIDATION_AI_MODE=disabled

# GitHub client (compact headers)
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_TYPE=http
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL=https://api.githubcopilot.com/mcp/
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_ENABLED=true
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS=X-GitHub-Token:Authorization:Bearer {value}

# AWS docs client (no auth)
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_AWS_DOCS_TYPE=http
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_AWS_DOCS_URL=https://knowledge-mcp.global.api.aws

# STDIO client
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_TOOLS_TYPE=stdio
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_TOOLS_COMMAND=/usr/local/bin/mcp-server
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_TOOLS_ARGS=--verbose,--port=8081
```

### Mixed YAML and Environment Variables

YAML provides base config, env vars override specific values:

```yaml
# maybe-dont.yaml
downstream_mcp_servers:
  github:
    type: http
    url: "https://api.githubcopilot.com/mcp/"
    auth:
      pass_through:
        enabled: true
        headers:
          - source_header: X-GitHub-Token
            target_header: Authorization
            format: "Bearer {value}"
```

```bash
# Override URL for testing
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL=https://staging.githubcopilot.com/mcp/

# Add a new client via env var
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_TYPE=stdio
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_LOCAL_COMMAND=./my-mcp-server
```

## Precedence Rules

1. Environment variables override YAML values for the same field
2. For headers:
   - If env var specifies headers (indexed or compact), it completely replaces YAML headers for that client
   - No merging of individual header entries between YAML and env vars
3. If a client is defined in both YAML and env vars, env vars override individual fields (not the entire client)

## Error Message Format

All configuration validation errors should include both the YAML path and the equivalent environment variable name. This helps users regardless of which configuration method they're using.

**Example error messages:**

```
downstream_mcp_servers[github].url is required when type is http
  (env var: MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL)

validation.ai.api_key is required when AI validation is enabled
  (env var: MAYBE_DONT_VALIDATION_AI_API_KEY)

Invalid compact header format for client 'github': value must contain at least one colon
  (env var: MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS)
  Expected format: source_header:target_header[:format]
```

**Implementation:** Add a helper function to generate the env var name from the YAML config path:

```go
// configPathToEnvVar converts a YAML config path to its environment variable equivalent
// e.g., "downstream_mcp_servers[github].url" -> "MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_URL"
func configPathToEnvVar(path string) string {
    // Implementation
}
```

## Implementation

### Code Changes

#### 1. `internal/config/config.go`

Add new function to parse downstream server env vars:

```go
// parseDownstreamServersFromEnv scans environment variables and builds/updates
// the DownstreamMCPServers map from MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_* vars.
func parseDownstreamServersFromEnv(servers map[string]ClientConfig, envPrefix string) map[string]ClientConfig {
    // Implementation
}

// parseCompactHeaders parses the compact header format: "source:target[:format][;...]"
func parseCompactHeaders(value string) ([]CredentialMapping, error) {
    // Implementation
}

// extractClientNameAndPath extracts client name and remaining field path from env var suffix
// e.g., "GITHUB_AUTH_PASS_THROUGH_ENABLED" -> ("github", "AUTH_PASS_THROUGH_ENABLED")
func extractClientNameAndPath(suffix string, existingClients map[string]ClientConfig) (string, string) {
    // Implementation - needs to handle multi-word client names
}
```

#### 2. Update `LoadConfig()`

Call `parseDownstreamServersFromEnv` after initial config load:

```go
// After unmarshaling and applying standard env overrides...
config.DownstreamMCPServers = parseDownstreamServersFromEnv(
    config.DownstreamMCPServers,
    v.GetEnvPrefix(),
)
```

### Client Name Extraction Algorithm

Challenge: Distinguishing client name from field path when both use underscores.

**Algorithm:**
1. Build list of known field suffixes from `ClientConfig` struct tags
2. For each env var suffix, try progressively longer client name prefixes
3. Stop when remaining suffix matches a known field path
4. Convert client name: lowercase, underscores → hyphens

**Example:**
```
Suffix: AWS_DOCS_AUTH_PASS_THROUGH_ENABLED
Try: "AWS" + "DOCS_AUTH_PASS_THROUGH_ENABLED" -> "DOCS_AUTH_..." not a valid field
Try: "AWS_DOCS" + "AUTH_PASS_THROUGH_ENABLED" -> "AUTH_PASS_THROUGH_ENABLED" is valid!
Result: client="aws-docs", path="AUTH_PASS_THROUGH_ENABLED"
```

### Known Field Paths

Build from `ClientConfig` struct using reflection on mapstructure tags:
- `TYPE`
- `URL`
- `DOWNSTREAM_URL`
- `COMMAND`
- `ARGS`
- `COMMAND_ARGS`
- `STARTUP_TIMEOUT_MS`
- `INITIALIZATION_RETRIES`
- `RETRY_DELAY_MS`
- `CAPABILITY_DISCOVERY_DELAY_MS`
- `CAPABILITY_DISCOVERY_RETRIES`
- `CAPABILITY_RETRY_DELAY_MS`
- `HTTP_HEADERS_*`
- `SSE_HEADERS_*`
- `AUTH_PASS_THROUGH_ENABLED`
- `AUTH_PASS_THROUGH_HEADERS` (compact)
- `AUTH_PASS_THROUGH_HEADERS_*_SOURCE_HEADER` (indexed)
- `AUTH_PASS_THROUGH_HEADERS_*_TARGET_HEADER` (indexed)
- `AUTH_PASS_THROUGH_HEADERS_*_FORMAT` (indexed)

## Testing

### Unit Tests

1. **Basic field parsing**
   - Single client with basic fields (type, url)
   - Multiple clients
   - Client names with underscores → hyphens

2. **Header parsing - indexed format**
   - Single header with all fields
   - Single header without format (optional)
   - Multiple headers (indices 0, 1, 2)
   - Missing source_header → error
   - Missing target_header → error

3. **Header parsing - compact format**
   - `source:target` (no format)
   - `source:target:format`
   - Multiple headers with semicolon separator
   - Missing colon → error with helpful message including env var name
   - Format containing colon (should work, colon only delimits first 3 parts)

4. **Format detection**
   - Only indexed vars → indexed format
   - Only compact var → compact format
   - Both present → indexed takes precedence

5. **Precedence**
   - Env var overrides YAML field
   - Env var headers replace (not merge) YAML headers

6. **Edge cases**
   - Empty env var value
   - Env var with only whitespace
   - Client name that looks like a field name
   - Very long client names
   - Underscore-to-hyphen conversion verified (`AWS_DOCS` → `aws-docs`)
   - YAML client with underscore name cannot be overridden via env var (documented limitation)

7. **Error message format**
   - Validation errors include YAML path
   - Validation errors include env var equivalent
   - Compact format errors include expected format hint

8. **Optional config file**
   - No error when config file is missing
   - Error only when required value is missing
   - Error only when configured value is invalid

### Integration Tests

1. Start gateway with env-var-only configuration (no YAML file)
2. Verify clients are created correctly
3. Verify pass-through auth works with env-var-configured headers

## Documentation Updates

### CLAUDE.md

Add section under "Environment Variables":
```markdown
### Downstream MCP Server Configuration

Configure downstream servers entirely via environment variables:

\`\`\`bash
# Basic client
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_MYSERVER_TYPE=http
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_MYSERVER_URL=https://example.com/mcp/

# With pass-through auth (compact format - single header)
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_ENABLED=true
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS=X-Token:Authorization:Bearer {value}

# With pass-through auth (compact format - multiple headers, semicolon-separated)
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_MYAPI_AUTH_PASS_THROUGH_HEADERS=X-Token:Authorization:Bearer {value};X-Tenant:X-Downstream-Tenant

# With pass-through auth (indexed format - mirrors YAML structure)
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_SOURCE_HEADER=X-Token
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_TARGET_HEADER=Authorization
export MAYBE_DONT_DOWNSTREAM_MCP_SERVERS_GITHUB_AUTH_PASS_THROUGH_HEADERS_0_FORMAT=Bearer {value}
\`\`\`

**Client naming:** Underscores in env vars are converted to hyphens: `AWS_DOCS` → `aws-docs`

**Limitation:** Client names with literal underscores (e.g., `aws_docs`) cannot be configured via env vars. Use YAML for these edge cases.

**Compact header format:** `source:target[:format]` with multiple headers separated by `;`
```

### config/maybe-dont.yaml

Add comments showing env var equivalents for key fields.

## Implementation Checklist

- [ ] Add `parseDownstreamServersFromEnv()` function
- [ ] Add `parseCompactHeaders()` function
- [ ] Add `extractClientNameAndPath()` function
- [ ] Add `configPathToEnvVar()` helper function for error messages
- [ ] Update `LoadConfig()` to call new parsing
- [ ] Update `validateConfigWithOptions()` to include env var names in error messages
- [ ] Ensure no error when config file is missing (only error on missing/invalid values)
- [ ] Add unit tests for basic field parsing
- [ ] Add unit tests for indexed header format
- [ ] Add unit tests for compact header format
- [ ] Add unit tests for format detection
- [ ] Add unit tests for precedence rules
- [ ] Add unit tests for edge cases
- [ ] Add unit tests for error message format (includes env var name)
- [ ] Add unit tests for optional config file behavior
- [ ] Add integration test for env-var-only startup
- [ ] Update CLAUDE.md documentation
- [ ] Update config/maybe-dont.yaml with env var comments
- [ ] Run `make test` and fix any failures
- [ ] Run `make lint` and fix any issues
