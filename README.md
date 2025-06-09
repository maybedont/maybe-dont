# Maybe Don't

A security proxy for MCP that enforces policy-based validation of tool calls using CEL and AI-based rules.

## Features

- CEL-based policy validation
- AI-based policy validation using OpenAI
- Parallel policy evaluation
- Detailed audit logging
- Support for HTTP, SSE, and STDIO transports

## Built-in Rules

### CEL Rules

The following CEL rules are included by default in `cel_rules.yaml`:

```yaml
rules:
- name: deny-kubectl-delete-namespace
  description: Deny kubectl delete namespace
  expression: |-
    get(request, "method", "") == "tools/call" && 
    get(request.params, "name", "") == "kubectl" && 
    has(request.params, "arguments") && 
    has(request.params.arguments, "command") && 
    request.params.arguments.command.contains("delete") && 
    (
        request.params.arguments.command.contains("namespace") || 
        request.params.arguments.command.contains("ns")
    )
  action: deny
  message: Denied access to kubectl delete namespace

- name: deny-kubectl-delete-pod
  description: Deny kubectl delete pod
  expression: |-
    get(request, "method", "") == "tools/call" && 
    get(request.params, "name", "") == "kubectl" && 
    has(request.params, "arguments") && 
    has(request.params.arguments, "command") && 
    request.params.arguments.command.contains("delete") && 
    request.params.arguments.command.contains("pod")
  action: deny
  message: Denied access to kubectl delete pod

- name: deny-kubectl-delete-deployment
  description: Deny kubectl delete deployment
  expression: |-
    get(request, "method", "") == "tools/call" && 
    get(request.params, "name", "") == "kubectl" && 
    has(request.params, "arguments") && 
    has(request.params.arguments, "command") && 
    request.params.arguments.command.contains("delete") && 
    request.params.arguments.command.contains("deployment")
  action: deny
  message: Denied access to kubectl delete deployment

- name: deny-kubectl-delete-service
  description: Deny kubectl delete service
  expression: |-
    get(request, "method", "") == "tools/call" && 
    get(request.params, "name", "") == "kubectl" && 
    has(request.params, "arguments") && 
    has(request.params.arguments, "command") && 
    request.params.arguments.command.contains("delete") && 
    request.params.arguments.command.contains("service")
  action: deny
  message: Denied access to kubectl delete service

- name: deny-kubectl-delete-configmap
  description: Deny kubectl delete configmap
  expression: |-
    get(request, "method", "") == "tools/call" && 
    get(request.params, "name", "") == "kubectl" && 
    has(request.params, "arguments") && 
    has(request.params.arguments, "command") && 
    request.params.arguments.command.contains("delete") && 
    request.params.arguments.command.contains("configmap")
  action: deny
  message: Denied access to kubectl delete configmap

- name: deny-kubectl-delete-secret
  description: Deny kubectl delete secret
  expression: |-
    get(request, "method", "") == "tools/call" && 
    get(request.params, "name", "") == "kubectl" && 
    has(request.params, "arguments") && 
    has(request.params.arguments, "command") && 
    request.params.arguments.command.contains("delete") && 
    request.params.arguments.command.contains("secret")
  action: deny
  message: Denied access to kubectl delete secret
```

### AI Rules

The following AI rules are included by default in `ai_rules.yaml`:

```yaml
rules:
- name: block-destructive-actions
  description: Block destructive actions like rm -rf, format, etc.
  prompt: |
    Analyze the following tool call and determine if it contains any destructive actions.
    Destructive actions include but are not limited to:
    - Deleting files or directories (rm, rm -rf)
    - Formatting disks or partitions
    - Dropping databases
    - Uninstalling critical software
    - Modifying system files
    - Changing system configurations
    - Executing shell commands with sudo or root privileges
    
    Tool call:
    %s
    
    Respond with a JSON object containing:
    {
      "allowed": boolean,
      "message": "string explaining the decision"
    }
  message: Blocked potentially destructive action

- name: block-system-file-modification
  description: Block modifications to system files
  prompt: |
    Analyze the following tool call and determine if it attempts to modify system files.
    System files include but are not limited to:
    - Files in /etc/
    - Files in /usr/
    - Files in /var/
    - Files in /opt/
    - Files in /bin/
    - Files in /sbin/
    - Files in /lib/
    - Files in /lib64/
    
    Tool call:
    %s
    
    Respond with a JSON object containing:
    {
      "allowed": boolean,
      "message": "string explaining the decision"
    }
  message: Blocked system file modification

- name: block-path-traversal
  description: Block path traversal attacks
  prompt: |
    Analyze the following tool call and determine if it contains any path traversal attempts.
    Path traversal attempts include but are not limited to:
    - Using ../ to navigate up directories
    - Using absolute paths
    - Using symbolic links
    - Using environment variables in paths
    - Using special characters in paths
    
    Tool call:
    %s
    
    Respond with a JSON object containing:
    {
      "allowed": boolean,
      "message": "string explaining the decision"
    }
  message: Blocked path traversal attempt
```

## Configuration

The proxy can be configured using a YAML configuration file. Here's an example:

```yaml
server:
  type: http  # stdio, http, or sse
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

## Running with Docker

```bash
# Build the image
docker build -t mcp-proxy .

# Run the container
docker run -d \
  --name mcp-proxy \
  -p 8080:8080 \
  -v $(pwd)/logs:/app/logs \
  -e OPENAI_API_KEY=your_api_key \
  mcp-proxy
```

## Custom Rules

You can add your own rules by:

1. Adding CEL rules to `cel_rules.yaml`:
```yaml
rules:
- name: your-rule-name
  description: Your rule description
  expression: |
    # Your CEL expression here
  action: deny  # or allow
  message: Your message
```

2. Adding AI rules to `ai_rules.yaml`:
```yaml
rules:
- name: your-rule-name
  description: Your rule description
  prompt: |
    # Your prompt template here
    # Use %s to insert the tool call
  message: Your message
```

## CEL Functions

The proxy provides the following custom CEL functions:

- `has(obj, field)`: Safely check if a field exists
- `get(obj, field, default)`: Safely get a field value with a default

Example usage:
```cel
has(request.params, "arguments") && 
get(request.params.arguments, "command", "").contains("delete")
```
