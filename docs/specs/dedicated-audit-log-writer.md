# Dedicated Audit Log Writer Specification

## Overview

This specification defines a dedicated audit log writer that replaces the current zap-based audit logging approach. The new writer produces clean JSONL output with a well-defined schema, independent of operational log levels, with support for log rotation/compression and future SIEM integration.

## Motivation

The current implementation writes audit logs through zap's standard logging infrastructure:

```go
g.auditLogger.Info(ctx, "Tool call audit", zap.Any("audit", entry))
```

This produces wrapped output:
```json
{"level":"info","ts":1705320600.123,"msg":"Tool call audit","audit":{...actual audit entry...}}
```

### Problems with Current Approach

1. **Redundant wrapper fields**: `level`, `ts`, and `msg` are noise for audit logs
2. **Nested schema**: Actual audit data is nested under `"audit"` key
3. **Conceptual mismatch**: Audit logs are structured security records, not "log messages at info level"
4. **Format coupling**: Tied to zap's production format which could change
5. **Ambiguous timestamps**: Only `created_at` exists, but timing of validation start and tool call are unclear

## Goals

1. **Clean JSONL output**: Write `AuditEntry` directly as root JSON object, one per line
2. **Log rotation and compression**: Support size-based rotation with optional gzip compression
3. **Clear temporal semantics**: Distinguish when validation started, when tool was called, and when log was written
4. **Independence from log levels**: Audit entries are always written regardless of operational log configuration
5. **SIEM-ready design**: Schema and architecture that supports future webhook/direct SIEM integration

## Non-Goals

1. Real-time SIEM streaming (future scope)
2. Multiple output destinations simultaneously (future scope)
3. Custom output formats beyond JSON (future scope)

## Schema Changes

### Updated AuditEntry Structure

```go
type AuditEntry struct {
    // Temporal fields - all in RFC3339Nano format
    ValidationStarted string `json:"validation_started"` // When we received the tool call and began validation
    CreatedAt         string `json:"created_at"`         // When this audit entry was finalized and written

    // Tool call information (identity + execution details)
    Tool AuditToolInfo `json:"tool"`

    // Upstream request metadata (about the incoming request, not the tool call)
    UpstreamRequest UpstreamRequestInfo `json:"upstream_request"`

    // Validation results
    RequestValidation  *AuditValidationInfo `json:"request_validation,omitempty"`
    Response           *AuditResponseInfo   `json:"response,omitempty"`
    ResponseValidation *AuditValidationInfo `json:"response_validation,omitempty"`

    // Actions
    RecommendedAction string `json:"recommended_action"`
    Action            string `json:"action"`

    // Timing
    DurationMs     int64 `json:"duration_ms"`       // Total wall-clock time from validation_started to created_at
    TotalBlockedMs int64 `json:"total_blocked_ms"`  // Time caller was blocked (validation + tool call)
}

// AuditToolInfo contains tool identification and execution details
type AuditToolInfo struct {
    // Identity
    Name         string `json:"name"`
    Client       string `json:"client"`
    PrefixedName string `json:"prefixed_name"`

    // Execution details
    Params     map[string]interface{} `json:"params,omitempty"`
    CalledAt   string                 `json:"called_at,omitempty"`   // When downstream tool was invoked (omitted if denied)
    DurationMs *int64                 `json:"duration_ms,omitempty"` // Downstream call duration (omitted if denied)
}

// UpstreamRequestInfo contains metadata about the incoming request
type UpstreamRequestInfo struct {
    RequestID string `json:"id,omitempty"`
    SessionID string `json:"session_id,omitempty"`
    ClientIP  string `json:"client_ip,omitempty"`
    UserAgent string `json:"user_agent,omitempty"` // User-Agent header from incoming request
}

// AuditValidationInfo contains validation results for rules and AI policies
type AuditValidationInfo struct {
    Rules *AuditRulesResult `json:"rules,omitempty"` // Deterministic rule evaluation (was "cel")
    AI    *AuditAIResult    `json:"ai,omitempty"`    // AI-powered validation
}
```

### Field Semantics

#### Temporal Fields

| Field | When Set | Description |
|-------|----------|-------------|
| `validation_started` | Request received | Timestamp when the gateway received the tool call and began validation. Start time for `duration_ms`. |
| `tool.called_at` | Before downstream call | Timestamp when the gateway invoked the downstream MCP server. Omitted if request was denied. |
| `created_at` | Entry finalization | Timestamp when the audit entry was finalized and written. |

#### Timing Fields

| Field | Description | How to Calculate Gateway Overhead |
|-------|-------------|-----------------------------------|
| `duration_ms` | Total wall-clock time from receiving request to returning response | - |
| `total_blocked_ms` | Time the caller was blocked waiting (includes validation blocking + tool call) | `total_blocked_ms - tool.duration_ms` = gateway overhead |
| `tool.duration_ms` | Time spent waiting for downstream MCP server (omitted if denied) | - |
| `request_validation.cel.blocked_ms` | Time blocked waiting for deterministic rules (may be < `evaluation_ms` if short-circuited) | - |
| `request_validation.cel.evaluation_ms` | Total time for all deterministic rules to complete | - |
| `request_validation.ai.blocked_ms` | Time blocked waiting for AI rules (may be < `evaluation_ms` if short-circuited or budget exhausted) | - |
| `request_validation.ai.evaluation_ms` | Total time for all AI rules to complete | - |
| `response_validation.*` | Same structure as request_validation | - |
| `*.results[].evaluation_ms` | Time for each individual rule to complete | - |

**Key timing relationships:**
- `total_blocked_ms` = request_validation.blocked + `tool.duration_ms` + response_validation.blocked
- Gateway overhead = `total_blocked_ms` - `tool.duration_ms`
- If validation short-circuits: `blocked_ms` < `evaluation_ms`
- If budget exhausted: remaining validation runs async, `blocked_ms` stops accumulating

### Validation Result Naming

The validation blocks use `cel` for deterministic CEL rule evaluation results:

```go
type AuditValidationInfo struct {
    CEL *AuditRulesResult `json:"cel,omitempty"` // Deterministic CEL rule evaluation
    AI  *AuditAIResult    `json:"ai,omitempty"`  // AI-powered validation
}

// AuditRulesResult contains the result of deterministic rule evaluation
type AuditRulesResult struct {
    Action       string                 `json:"action"`
    BlockedMs    int64                  `json:"blocked_ms"`
    EvaluationMs int64                  `json:"evaluation_ms"`
    DecidingRule string                 `json:"deciding_rule,omitempty"`
    Reason       string                 `json:"reason,omitempty"`
    Results      []AuditRulesRuleResult `json:"results"`
}

// AuditRulesRuleResult contains the result of a single deterministic rule
type AuditRulesRuleResult struct {
    Rule         string `json:"rule"`
    Action       string `json:"action"`
    Mode         string `json:"mode,omitempty"`
    Result       string `json:"result"`
    EvaluationMs int64  `json:"evaluation_ms"`
    Error        string `json:"error,omitempty"`
}
```

### Example Output

**Successful tool call:**
```json
{
  "validation_started": "2026-01-15T10:30:00.000000000Z",
  "created_at": "2026-01-15T10:30:01.100000000Z",
  "tool": {
    "name": "create_issue",
    "client": "github",
    "prefixed_name": "github__create_issue",
    "params": {"title": "Bug fix", "body": "Description"},
    "called_at": "2026-01-15T10:30:00.850000000Z",
    "duration_ms": 150
  },
  "upstream_request": {
    "id": "req-abc123",
    "session_id": "sess-xyz789",
    "client_ip": "127.0.0.1",
    "user_agent": "claude-code/1.0.0"
  },
  "request_validation": {
    "cel": {
      "action": "allow",
      "blocked_ms": 5,
      "evaluation_ms": 5,
      "results": [
        {"rule": "allow_github_tools", "action": "allow", "result": "allow", "evaluation_ms": 5}
      ]
    },
    "ai": {
      "action": "allow",
      "blocked_ms": 695,
      "evaluation_ms": 1200,
      "results": [
        {"rule": "check_safe_operation", "action": "deny", "result": "allow", "evaluation_ms": 695},
        {"rule": "audit_access", "action": "deny", "mode": "audit_only", "result": "allow", "evaluation_ms": 1200}
      ]
    }
  },
  "response": {
    "content_items": 1,
    "is_error": false
  },
  "recommended_action": "allow",
  "action": "allow",
  "duration_ms": 1100,
  "total_blocked_ms": 850
}
```

In this example:
- `duration_ms` (1100ms) = total wall-clock time
- `total_blocked_ms` (850ms) = cel.blocked (5) + ai.blocked (695) + tool.duration (150)
- Gateway overhead = 850 - 150 = 700ms

**Denied before tool call:**
```json
{
  "validation_started": "2026-01-15T10:31:00.000000000Z",
  "created_at": "2026-01-15T10:31:00.010000000Z",
  "tool": {
    "name": "delete_repo",
    "client": "github",
    "prefixed_name": "github__delete_repo",
    "params": {"owner": "acme", "repo": "prod-db"}
  },
  "upstream_request": {
    "id": "req-def456",
    "session_id": "sess-xyz789",
    "client_ip": "127.0.0.1",
    "user_agent": "claude-code/1.0.0"
  },
  "request_validation": {
    "cel": {
      "action": "deny",
      "blocked_ms": 2,
      "evaluation_ms": 2,
      "deciding_rule": "block_destructive",
      "reason": "Destructive operations blocked",
      "results": [
        {"rule": "block_destructive", "action": "deny", "result": "deny", "evaluation_ms": 2}
      ]
    }
  },
  "recommended_action": "deny",
  "action": "deny",
  "duration_ms": 10,
  "total_blocked_ms": 2
}
```

Note: `tool.called_at` and `tool.duration_ms` are omitted since the downstream tool was never invoked.

## Configuration

### Config Naming Change

The `logging` config section will be renamed to `logger` for consistency with the `audit` section naming pattern.

### Logger Configuration (Application Logs)

```yaml
logger:
  level: info              # debug, info, warn, error (default: info)
  path: stderr             # stdout, stderr, or filename (default: stderr)

  # Log rotation settings (only applicable when path is a filename)
  rotation:
    max_size_mb: 100       # Max size in MB before rotation (default: 100)
    max_backups: 5         # Max number of rotated files to keep (default: 5)
    max_age_days: 180      # Max days before deleting rotated files (default: 180, 0 = no limit)
    compress: true         # Gzip compress rotated files (default: true)
```

### Audit Configuration (Audit Logs)

```yaml
audit:
  # Output destination: "stdout", "stderr", or filename
  # If filename, resolved relative to log_dir
  path: "maybedont-audit.log"  # default

  # Filtering: which tool calls to audit
  # - "all": Audit every tool call (default)
  # - "deny_only": Only audit tool calls that result in a deny action
  filter: all

  # Log rotation settings (only applicable when path is a filename)
  rotation:
    max_size_mb: 100       # Max size in MB before rotation (default: 100)
    max_backups: 5         # Max number of rotated files to keep (default: 5)
    max_age_days: 180      # Max days before deleting rotated files (default: 180, 0 = no limit)
    compress: true         # Gzip compress rotated files (default: true)
```

### Default Behavior

**Logger:**
- `level`: `"info"`
- `path`: `"stderr"`
- `rotation.*`: Only applies when path is a filename

**Audit:**
- `path`: `"maybedont-audit.log"` (written to `log_dir`)
- `filter`: `"all"`
- `rotation.max_size_mb`: 100
- `rotation.max_backups`: 5
- `rotation.max_age_days`: 180 (rotated files deleted after 180 days)
- `rotation.compress`: true

### Environment Variable Overrides

Following existing patterns:

**Logger:**
- `MAYBE_DONT_LOGGER_LEVEL`
- `MAYBE_DONT_LOGGER_PATH`
- `MAYBE_DONT_LOGGER_ROTATION_MAX_SIZE_MB`
- `MAYBE_DONT_LOGGER_ROTATION_MAX_BACKUPS`
- `MAYBE_DONT_LOGGER_ROTATION_MAX_AGE_DAYS`
- `MAYBE_DONT_LOGGER_ROTATION_COMPRESS`

**Audit:**
- `MAYBE_DONT_AUDIT_PATH`
- `MAYBE_DONT_AUDIT_FILTER`
- `MAYBE_DONT_AUDIT_ROTATION_MAX_SIZE_MB`
- `MAYBE_DONT_AUDIT_ROTATION_MAX_BACKUPS`
- `MAYBE_DONT_AUDIT_ROTATION_MAX_AGE_DAYS`
- `MAYBE_DONT_AUDIT_ROTATION_COMPRESS`

## Implementation

### AuditWriter Interface

```go
// AuditWriter writes audit entries to the configured destination
type AuditWriter interface {
    // Write serializes and writes an audit entry
    // Returns error only for write failures (not serialization - that should panic)
    Write(entry *AuditEntry) error

    // Close flushes and closes the writer
    Close() error
}
```

### JSONLAuditWriter Implementation

```go
type JSONLAuditWriter struct {
    writer io.WriteCloser
    mu     sync.Mutex
}

func NewJSONLAuditWriter(cfg *config.Config, logDir string) (*JSONLAuditWriter, error) {
    // Determine output based on cfg.Audit.Path
    // - "stdout" -> os.Stdout
    // - "stderr" -> os.Stderr
    // - filename -> lumberjack.Logger with rotation config
}

func (w *JSONLAuditWriter) Write(entry *AuditEntry) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    // Marshal entry directly (no wrapper)
    data, err := json.Marshal(entry)
    if err != nil {
        // This indicates a bug in AuditEntry - should never happen
        return fmt.Errorf("failed to marshal audit entry: %w", err)
    }

    // Write JSON followed by newline
    if _, err := w.writer.Write(data); err != nil {
        return err
    }
    _, err = w.writer.Write([]byte("\n"))
    return err
}
```

### Log Rotation with Lumberjack

For file outputs, use [lumberjack](https://github.com/natefinch/lumberjack) for rotation:

```go
import "gopkg.in/natefinch/lumberjack.v2"

func newFileWriter(path string, rotationCfg RotationConfig) io.WriteCloser {
    return &lumberjack.Logger{
        Filename:   path,
        MaxSize:    rotationCfg.MaxSizeMB,    // megabytes
        MaxBackups: rotationCfg.MaxBackups,
        MaxAge:     rotationCfg.MaxAgeDays,   // days
        Compress:   rotationCfg.Compress,
    }
}
```

Lumberjack provides:
- Size-based rotation (rotates when file exceeds `MaxSize`)
- Backup retention (keeps up to `MaxBackups` old files)
- Age-based cleanup (deletes files older than `MaxAge` days)
- Optional gzip compression of rotated files

### AuditContext Updates

Update `AuditContext` to track the new schema:

```go
type AuditContext struct {
    entry           *AuditEntry
    validationStart time.Time
}

func NewAuditContext(prefixedToolName, clientName, toolName, sessionID, clientIP, requestID string) *AuditContext {
    now := time.Now().UTC()
    return &AuditContext{
        entry: &AuditEntry{
            ValidationStarted: now.Format(time.RFC3339Nano),
            Tool: AuditToolInfo{
                Name:         toolName,
                Client:       clientName,
                PrefixedName: prefixedToolName,
            },
            UpstreamRequest: UpstreamRequestInfo{
                RequestID: requestID,
                SessionID: sessionID,
                ClientIP:  clientIP,
            },
        },
        validationStart: now,
    }
}

// SetToolParams sets the tool call parameters
func (ac *AuditContext) SetToolParams(params map[string]interface{}) {
    ac.entry.Tool.Params = params
}

// SetToolCalledAt records when the downstream tool was invoked
func (ac *AuditContext) SetToolCalledAt() {
    ac.entry.Tool.CalledAt = time.Now().UTC().Format(time.RFC3339Nano)
}

// SetToolDuration sets the duration of the downstream tool call
func (ac *AuditContext) SetToolDuration(durationMs int64) {
    ac.entry.Tool.DurationMs = &durationMs
}

// Finalize calculates duration and sets created_at timestamp
func (ac *AuditContext) Finalize() *AuditEntry {
    now := time.Now().UTC()
    ac.entry.CreatedAt = now.Format(time.RFC3339Nano)
    ac.entry.DurationMs = now.Sub(ac.validationStart).Milliseconds()
    return ac.entry
}
```

### Gateway Integration

Replace zap-based audit logging:

```go
// In NewGateway
auditWriter, err := NewJSONLAuditWriter(cfg, logDir)
if err != nil {
    return nil, fmt.Errorf("failed to create audit writer: %w", err)
}

// In tool call handler
writeAuditLog := func() {
    // total_blocked_ms = validation blocking + tool call duration
    toolDuration := int64(0)
    if ac.entry.Tool.DurationMs != nil {
        toolDuration = *ac.entry.Tool.DurationMs
    }
    audit.SetTotalBlockedMs(blockingBudget.TotalBlockedMs() + toolDuration)

    entry := audit.Finalize()

    // Apply audit filter
    if !g.shouldAudit(entry) {
        return
    }

    if err := g.auditWriter.Write(entry); err != nil {
        // Log error to operational logger, but don't fail the request
        g.logger.Error(ctx, "Failed to write audit log", zap.Error(err))
    }
}

// shouldAudit determines whether an entry should be written based on audit.filter config
func (g *Gateway) shouldAudit(entry *AuditEntry) bool {
    switch g.config.Audit.Filter {
    case "deny_only":
        return entry.Action == "deny"
    case "all":
        fallthrough
    default:
        return true
    }
}

// Before downstream tool call (after request validation passes)
audit.SetToolCalledAt()
callStart := time.Now()
result, err := client.CallTool(ctx, toolName, req.Params.Arguments)
audit.SetToolDuration(time.Since(callStart).Milliseconds())
```

## Migration

### Breaking Changes

1. **Output format**: Audit log output changes from zap-wrapped JSON to clean JSONL
2. **Schema changes**:
   - `evaluation_started` renamed to `validation_started`
   - `incoming_request` renamed to `upstream_request`
   - `request` object merged into `tool` (params, called_at, duration_ms now under `tool`)
   - `total_blocked_ms` now includes tool call duration
   - New `user_agent` field in `upstream_request`
3. **Config changes**:
   - `logging` section renamed to `logger`
   - New `rotation` config block for both `logger` and `audit`
   - New `audit.filter` option
4. **Timestamp format**: Using RFC3339Nano for millisecond precision (all timing fields are at least ms precision)

### Backwards Compatibility Policy

**We are not providing backwards compatibility for these changes:**
- Old config files using `logging` will need to be updated to use `logger`
- Old audit log entries will not be automatically migrated to the new schema
- Users should archive existing audit logs before upgrade if needed for historical analysis

### Native Tool Handling of Old Format

The `get_audit_log` and `generate_audit_report` native tools should gracefully handle old audit log entries:
- **Do not throw exceptions** when encountering old format entries
- **Skip/ignore** entries that don't match the expected schema
- Log a debug message when skipping malformed entries
- Continue processing remaining entries

This ensures the gateway doesn't fail if the audit log file contains a mix of old and new format entries during the upgrade transition.

## Future Considerations

### SIEM Integration (Future Scope)

The architecture supports future SIEM integration through:

1. **Webhook output**: Add `webhook` destination type that POSTs entries in real-time
   ```yaml
   audit:
     siem:
       enabled: true
       endpoint: "https://siem.example.com/api/ingest"
       headers:
         Authorization: "Bearer ${SIEM_TOKEN}"
       batch_size: 10
       flush_interval_ms: 5000
   ```

2. **Multi-destination**: Write to both file and webhook simultaneously
   ```go
   type MultiAuditWriter struct {
       writers []AuditWriter
   }
   ```

3. **Format adapters**: Transform JSON to CEF, LEEF, or other SIEM formats
   ```yaml
   audit:
     siem:
       format: "cef"  # Common Event Format
   ```

### Potential Enhancements

- **Async writes**: Buffer entries and write in background to reduce latency impact
- **Sampling**: Write a percentage of allow entries to reduce volume (filtering by action is already supported via `audit.filter`)
- **Encryption**: Encrypt audit logs at rest

## Testing

### Unit Tests

1. `TestJSONLAuditWriter_Write` - Verify clean JSONL output without wrapper
2. `TestJSONLAuditWriter_Rotation` - Verify rotation triggers at size threshold
3. `TestJSONLAuditWriter_Compression` - Verify rotated files are gzipped
4. `TestAuditContext_TemporalFields` - Verify all timestamps set correctly
5. `TestAuditContext_ToolCalledAtOmitted` - Verify `tool.called_at` omitted when denied
6. `TestAuditEntry_TotalBlockedIncludesToolCall` - Verify `total_blocked_ms` includes tool duration

### Integration Tests

1. End-to-end test verifying audit entry written with correct timestamps
2. Test rotation behavior under load
3. Test stdout/stderr output modes

## Dependencies

### New Dependency

Add `gopkg.in/natefinch/lumberjack.v2` for log rotation:

```bash
go get gopkg.in/natefinch/lumberjack.v2
```

This is a mature, widely-used library for log rotation in Go applications.

## Implementation Checklist

### Config Changes
- [ ] Rename `Logging` config section to `Logger`
- [ ] Add `Rotation` struct to both `Logger` and `Audit` config sections
- [ ] Add `Filter` field to `Audit` config (values: "all", "deny_only")
- [ ] Add `lumberjack` dependency
- [ ] Update app logger to use lumberjack when writing to file
- [ ] Update environment variable handling for new config structure

### Audit Schema Changes
- [x] Rename `AuditCELResult` to `AuditRulesResult` (JSON tag remains `cel`)
- [ ] Rename `IncomingRequestInfo` to `UpstreamRequestInfo` and update JSON tag
- [ ] Add `UserAgent` field to `UpstreamRequestInfo`
- [ ] Merge `AuditRequestInfo` into `AuditToolInfo` (add `Params`, `CalledAt`, `DurationMs`)
- [ ] Add `ValidationStarted` field to `AuditEntry`
- [ ] Update `AuditContext` methods for new schema
- [ ] Update `total_blocked_ms` calculation to include tool call duration
- [ ] Capture User-Agent header in gateway request handling

### Audit Writer Implementation
- [ ] Create `AuditWriter` interface
- [ ] Implement `JSONLAuditWriter` with lumberjack integration
- [ ] Implement audit filtering based on `audit.filter` config
- [ ] Update gateway to use new writer

### Native Tools
- [ ] Update `get_audit_log` to gracefully skip malformed/old entries
- [ ] Update `generate_audit_report` to gracefully skip malformed/old entries
- [ ] Update both tools for new schema field names

### Testing & Documentation
- [ ] Add tests for rotation behavior
- [ ] Add tests for filtering behavior
- [ ] Add tests for graceful handling of old format entries
- [ ] Update `validation-chain-audit-schema.md` spec with matching changes
- [ ] Update documentation
