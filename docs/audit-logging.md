# Audit Logging

Maybe Don't provides comprehensive audit logging for all tool calls and policy evaluations.

## Overview

The audit system logs:
- All tool calls
- Policy evaluation results
- Authentication attempts
- System events

## Configuration

### Basic Configuration

```yaml
audit:
  path: logs/audit.log
  format: json  # or text
```

### Advanced Configuration

```yaml
audit:
  path: logs/audit.log
  format: json
  max_size: 100MB
  max_backups: 5
  max_age: 30d
  compress: true
  local_time: true
```

## Log Format

### JSON Format

```json
{
  "timestamp": "2024-03-14T12:00:00Z",
  "level": "info",
  "event": "tool_call",
  "request_id": "123e4567-e89b-12d3-a456-426614174000",
  "tool": {
    "name": "kubectl",
    "arguments": {
      "command": "delete namespace test"
    }
  },
  "validation": {
    "valid": false,
    "message": "Destructive actions are not allowed",
    "policies": [
      {
        "name": "block-destructive-actions",
        "valid": false,
        "message": "Destructive actions are not allowed"
      }
    ]
  },
  "user": {
    "id": "user123",
    "ip": "192.168.1.1"
  }
}
```

### Text Format

```
2024-03-14T12:00:00Z INFO tool_call request_id=123e4567-e89b-12d3-a456-426614174000 tool=kubectl arguments.command="delete namespace test" validation.valid=false validation.message="Destructive actions are not allowed" validation.policies[0].name=block-destructive-actions validation.policies[0].valid=false validation.policies[0].message="Destructive actions are not allowed" user.id=user123 user.ip=192.168.1.1
```

## Event Types

### Tool Call Events

```json
{
  "event": "tool_call",
  "tool": {
    "name": "kubectl",
    "arguments": {
      "command": "delete namespace test"
    }
  },
  "validation": {
    "valid": false,
    "message": "Destructive actions are not allowed",
    "policies": [
      {
        "name": "block-destructive-actions",
        "valid": false,
        "message": "Destructive actions are not allowed"
      }
    ]
  }
}
```

### Authentication Events

```json
{
  "event": "auth",
  "success": true,
  "method": "api_key",
  "user": {
    "id": "user123",
    "ip": "192.168.1.1"
  }
}
```

### System Events

```json
{
  "event": "system",
  "type": "startup",
  "version": "1.0.0",
  "config": {
    "server": {
      "type": "http",
      "listen_addr": ":8080"
    }
  }
}
```

## Log Rotation

The audit system supports log rotation with the following options:

- `max_size`: Maximum size of each log file
- `max_backups`: Number of backup files to keep
- `max_age`: Maximum age of log files
- `compress`: Whether to compress rotated logs

Example:

```yaml
audit:
  path: logs/audit.log
  format: json
  max_size: 100MB
  max_backups: 5
  max_age: 30d
  compress: true
```

## Log Analysis

### Using grep

```bash
# Find all blocked tool calls
grep '"validation.valid":false' logs/audit.log

# Find all kubectl commands
grep '"tool.name":"kubectl"' logs/audit.log

# Find all authentication failures
grep '"event":"auth","success":false' logs/audit.log
```

### Using jq

```bash
# Find all blocked tool calls
jq 'select(.validation.valid == false)' logs/audit.log

# Find all kubectl commands
jq 'select(.tool.name == "kubectl")' logs/audit.log

# Find all authentication failures
jq 'select(.event == "auth" and .success == false)' logs/audit.log
```

## Best Practices

1. **Log Storage**
   - Use separate storage for logs
   - Enable log rotation
   - Compress old logs

2. **Log Security**
   - Restrict log file permissions
   - Encrypt sensitive data
   - Monitor log access

3. **Log Analysis**
   - Set up log aggregation
   - Create alerts for issues
   - Regular log review

4. **Performance**
   - Use async logging
   - Buffer log writes
   - Monitor log size

5. **Compliance**
   - Retain logs as required
   - Include required fields
   - Enable audit trails 