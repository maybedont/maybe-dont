# Deployment

This guide covers different ways to deploy Maybe Don't.

## Docker Deployment

### Building the Image

```bash
# Build from source
docker build -t maybe-dont .

# Or pull from registry
docker pull ghcr.io/sudermanjr/maybe-dont:latest
```

### Running the Container

```bash
docker run -d \
  --name maybe-dont \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/cel_rules.yaml:/app/cel_rules.yaml \
  -v $(pwd)/ai_rules.yaml:/app/ai_rules.yaml \
  -v $(pwd)/logs:/app/logs \
  -e OPENAI_API_KEY=your_api_key \
  maybe-dont
```

### Environment Variables

- `OPENAI_API_KEY`: OpenAI API key for AI validation
- `LOG_LEVEL`: Logging level (debug, info, warn, error)
- `CONFIG_FILE`: Path to config file (default: config.yaml)

### Volume Mounts

- `/app/config.yaml`: Main configuration file
- `/app/cel_rules.yaml`: CEL rules file
- `/app/ai_rules.yaml`: AI rules file
- `/app/logs`: Audit log directory

## Kubernetes Deployment

### Deployment Manifest

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: maybe-dont
spec:
  replicas: 1
  selector:
    matchLabels:
      app: maybe-dont
  template:
    metadata:
      labels:
        app: maybe-dont
    spec:
      containers:
      - name: maybe-dont
        image: ghcr.io/sudermanjr/maybe-dont:latest
        ports:
        - containerPort: 8080
        env:
        - name: OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: maybe-dont-secrets
              key: openai-api-key
        volumeMounts:
        - name: config
          mountPath: /app/config.yaml
          subPath: config.yaml
        - name: cel-rules
          mountPath: /app/cel_rules.yaml
          subPath: cel_rules.yaml
        - name: ai-rules
          mountPath: /app/ai_rules.yaml
          subPath: ai_rules.yaml
        - name: logs
          mountPath: /app/logs
      volumes:
      - name: config
        configMap:
          name: maybe-dont-config
      - name: cel-rules
        configMap:
          name: maybe-dont-cel-rules
      - name: ai-rules
        configMap:
          name: maybe-dont-ai-rules
      - name: logs
        persistentVolumeClaim:
          claimName: maybe-dont-logs
```

### Service Manifest

```yaml
apiVersion: v1
kind: Service
metadata:
  name: maybe-dont
spec:
  selector:
    app: maybe-dont
  ports:
  - port: 8080
    targetPort: 8080
  type: ClusterIP
```

### ConfigMap Manifest

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: maybe-dont-config
data:
  config.yaml: |
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

    audit:
      path: logs/audit.log
      format: json
```

### Secret Manifest

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: maybe-dont-secrets
type: Opaque
data:
  openai-api-key: <base64-encoded-api-key>
```

## Local Deployment

### Building from Source

```bash
# Clone the repository
git clone https://github.com/sudermanjr/maybe-dont.git
cd maybe-dont

# Build the binary
go build -o maybe-dont ./cmd/maybe-dont
```

### Running Locally

```bash
# Set environment variables
export OPENAI_API_KEY=your_api_key
export LOG_LEVEL=info

# Run the binary
./maybe-dont
```

## Configuration

### Basic Configuration

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

### Advanced Configuration

```yaml
server:
  type: http
  listen_addr: ":8080"
  log_level: info
  log_format: json
  tls:
    enabled: true
    cert_file: /path/to/cert.pem
    key_file: /path/to/key.pem

auth:
  type: api_key
  api_key: your-api-key

policy_validation:
  enabled: true
  rules_file: cel_rules.yaml
  timeout: 5s

ai_validation:
  enabled: true
  endpoint: https://api.openai.com/v1
  model: gpt-4-turbo-preview
  rules_file: ai_rules.yaml
  api_key: ${OPENAI_API_KEY}
  timeout: 10s
  max_tokens: 1000

audit:
  path: logs/audit.log
  format: json
  max_size: 100MB
  max_backups: 5
  max_age: 30d
```

## Monitoring

### Health Check

```bash
# Check health endpoint
curl http://localhost:8080/health
```

### Metrics

```bash
# Check metrics endpoint
curl http://localhost:8080/metrics
```

### Logs

```bash
# View logs
tail -f logs/audit.log
```

## Security Considerations

1. **API Keys**
   - Store API keys securely
   - Use environment variables
   - Rotate keys regularly

2. **TLS**
   - Enable TLS for all deployments
   - Use valid certificates
   - Configure secure ciphers

3. **Authentication**
   - Enable authentication
   - Use strong API keys
   - Consider mTLS

4. **Network**
   - Use internal networks
   - Limit external access
   - Configure firewalls

5. **Updates**
   - Keep images updated
   - Monitor security advisories
   - Apply patches promptly 