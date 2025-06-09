# Troubleshooting

This guide helps you diagnose and fix common issues with Maybe Don't.

## Common Issues

### Policy Validation Not Working

#### Symptoms
- Policies not being evaluated
- No validation results in logs
- All requests passing through

#### Checks
1. Verify policy files exist:
```bash
ls -l cel_rules.yaml ai_rules.yaml
```

2. Check policy configuration:
```yaml
policy_validation:
  enabled: true
  rules_file: cel_rules.yaml

ai_validation:
  enabled: true
  rules_file: ai_rules.yaml
```

3. Check policy file format:
```yaml
# CEL rules
rules:
  - name: "block-destructive-actions"
    description: "Block destructive kubectl commands"
    expression: |
      has(params.arguments.command) &&
      get(params.arguments.command, "").contains("delete")

# AI rules
policies:
  - name: "block-destructive-actions"
    description: "Block destructive kubectl commands"
    rules:
      - "Block any kubectl commands that delete resources"
```

4. Check logs for errors:
```bash
tail -f logs/audit.log | grep "error"
```

### Authentication Issues

#### Symptoms
- 401 Unauthorized errors
- Authentication failures in logs
- API key not working

#### Checks
1. Verify API key:
```bash
curl -v -H "X-API-Key: your-api-key" http://localhost:8080/health
```

2. Check authentication configuration:
```yaml
auth:
  type: api_key
  api_key: your-api-key
```

3. Check JWT configuration:
```yaml
auth:
  type: jwt
  jwt:
    jwks_url: https://your-domain/.well-known/jwks.json
    issuer: your-issuer
    audience: ["your-audience"]
```

4. Check mTLS configuration:
```yaml
auth:
  type: mtls
  mtls:
    ca_file: /path/to/ca.pem
    cert_file: /path/to/cert.pem
    key_file: /path/to/key.pem
```

### Performance Issues

#### Symptoms
- Slow response times
- High latency
- Timeout errors

#### Checks
1. Check server configuration:
```yaml
server:
  type: http
  listen_addr: ":8080"
  timeout: 30s
```

2. Check policy timeouts:
```yaml
policy_validation:
  timeout: 5s

ai_validation:
  timeout: 10s
```

3. Monitor resource usage:
```bash
# CPU usage
top -p $(pgrep maybe-dont)

# Memory usage
ps -o pid,ppid,cmd,%mem,%cpu --sort=-%mem | grep maybe-dont

# Disk I/O
iostat -x 1
```

4. Check network:
```bash
# Network connections
netstat -an | grep 8080

# Network latency
ping localhost
```

### Logging Issues

#### Symptoms
- Missing logs
- Incomplete log entries
- Log rotation not working

#### Checks
1. Verify log configuration:
```yaml
audit:
  path: logs/audit.log
  format: json
  max_size: 100MB
  max_backups: 5
  max_age: 30d
```

2. Check log permissions:
```bash
ls -l logs/audit.log
```

3. Check disk space:
```bash
df -h
```

4. Check log rotation:
```bash
ls -l logs/audit.log*
```

## Debug Mode

Enable debug mode for more detailed logging:

```yaml
server:
  log_level: debug
```

## Common Error Messages

### Policy Errors

```
Error: policy validation failed
```
- Check policy file format
- Verify policy expressions
- Check policy configuration

```
Error: AI validation failed
```
- Check OpenAI API key
- Verify AI rules format
- Check network connectivity

### Authentication Errors

```
Error: invalid API key
```
- Verify API key
- Check authentication configuration
- Check API key format

```
Error: JWT validation failed
```
- Check JWT token
- Verify JWT configuration
- Check token expiration

### Server Errors

```
Error: server timeout
```
- Check server configuration
- Verify network connectivity
- Check resource usage

```
Error: rate limit exceeded
```
- Check rate limit configuration
- Monitor request volume
- Implement backoff strategy

## Recovery Procedures

### Policy Recovery

1. Backup current policies:
```bash
cp cel_rules.yaml cel_rules.yaml.bak
cp ai_rules.yaml ai_rules.yaml.bak
```

2. Restore from backup:
```bash
cp cel_rules.yaml.bak cel_rules.yaml
cp ai_rules.yaml.bak ai_rules.yaml
```

3. Restart service:
```bash
systemctl restart maybe-dont
```

### Configuration Recovery

1. Backup configuration:
```bash
cp config.yaml config.yaml.bak
```

2. Restore configuration:
```bash
cp config.yaml.bak config.yaml
```

3. Restart service:
```bash
systemctl restart maybe-dont
```

### Log Recovery

1. Backup logs:
```bash
cp logs/audit.log logs/audit.log.bak
```

2. Clear logs:
```bash
> logs/audit.log
```

3. Restart service:
```bash
systemctl restart maybe-dont
```

## Getting Help

1. Check documentation:
   - [Getting Started](./getting-started.md)
   - [Configuration](./configuration.md)
   - [API Reference](./api-reference.md)

2. Check logs:
```bash
tail -f logs/audit.log
```

3. Enable debug mode:
```yaml
server:
  log_level: debug
```

4. Contact support:
   - GitHub Issues
   - Email support
   - Community forums 