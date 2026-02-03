# Spec: Audit Report Timeout and Optimization

## Status
**Draft** - Pending Review


## Problem Statement

The `maybedont__generate_audit_report` tool has a hardcoded 60-second timeout that may be insufficient for:
1. Large audit logs (up to 1000 entries)
2. Complex AI analysis with our expanded recommendations prompt
3. Slow or loaded AI endpoints

Additionally, there are potential connection-level risks where MCP clients may timeout waiting for a response before the report completes.

## Goals

1. Make audit report timeout configurable with a sensible default
2. Optimize the data sent to the AI to reduce processing time
3. Mitigate connection timeout risks at the MCP layer
4. Maintain backward compatibility

## Current Architecture

### Audit Report Generation Flow

```
handleGenerateAuditReport()
    │
    ├── getEntriesForReport()         # Read up to MaxEntries (default 1000)
    │   └── readAuditLogEntries()     # File I/O
    │
    ├── generateAIReport()            # 60s hardcoded timeout
    │   ├── prepareEntrySummary()     # Build text summary
    │   ├── buildReportPrompt()       # Construct full prompt
    │   └── OpenAI API call           # Actual AI request
    │
    └── formatReportAsMarkdown()      # Format response
```

### Current Timeout Configuration

| Component | Timeout | Configurable |
|-----------|---------|--------------|
| Audit report AI call | 60s | No (hardcoded) |
| `max_blocking_ms` | 90s | Yes |
| `max_rule_evaluation_ms` | 45s | Yes |
| HTTP server request | Go default (~no limit) | No |
| SSE connection | None (long-lived) | N/A |

### Data Sent to AI

Currently, `prepareEntrySummary()` builds a text summary including:
- Aggregate statistics (total requests, success/denied/error counts)
- Policy effectiveness metrics (successful denials, audit-only, timeouts)
- Top tools by usage (all tools, no limit)
- Denied policies breakdown (all policies)
- Sample denied requests (5 hardcoded)
- Sample audit-only denials (5 hardcoded)
- Sample timeout failures (5 hardcoded)

**Issues:**
- No token counting or truncation
- Map iteration (TopTools, DeniedByPolicy) includes ALL entries
- No prioritization of most relevant data

## Proposed Changes

### 1. Configurable Timeout (Required)

Add a timeout configuration to the AuditReport config.

#### Option A: Integer with explicit units (Current codebase pattern)

```go
// internal/config/config.go
AuditReport struct {
    Enabled        bool   `mapstructure:"enabled"`
    MaxEntries     int    `mapstructure:"max_entries"`
    TimeoutSeconds int    `mapstructure:"timeout_seconds"` // NEW
    SystemPrompt   string `mapstructure:"system_prompt"`
}
```

```yaml
native_tools:
  audit_report:
    timeout_seconds: 120
```

**Pros:**
- Consistent with existing patterns (`max_blocking_ms`, `max_rule_evaluation_ms`, `startup_timeout_ms`)
- Simple validation (integer range check)
- No parsing complexity

**Cons:**
- Less flexible (locked to one unit)
- Field name must encode the unit

#### Option B: Freeform duration string (Go idiomatic)

```go
// internal/config/config.go
AuditReport struct {
    Enabled      bool   `mapstructure:"enabled"`
    MaxEntries   int    `mapstructure:"max_entries"`
    Timeout      string `mapstructure:"timeout"` // NEW: "2m", "90s", "1m30s"
    SystemPrompt string `mapstructure:"system_prompt"`
}
```

```yaml
native_tools:
  audit_report:
    timeout: "2m"      # or "120s", "1m30s", etc.
```

**Pros:**
- More readable for humans ("2m" vs "120")
- Flexible unit choice (user picks what makes sense)
- Aligns with Go's `time.ParseDuration` standard
- Already have `ParseTimeRange` function that extends this pattern

**Cons:**
- Inconsistent with existing config patterns in this codebase
- Requires parsing and validation of string format
- Error messages need to explain valid formats

#### Option C: Support both (with migration path)

Accept either integer (interpreted as seconds) or string (parsed as duration):

```go
// In config loading:
func parseTimeout(value interface{}) (time.Duration, error) {
    switch v := value.(type) {
    case int:
        return time.Duration(v) * time.Second, nil
    case string:
        return time.ParseDuration(v)
    default:
        return 0, fmt.Errorf("invalid timeout type")
    }
}
```

**Recommendation:** Option A (integer with `_seconds` suffix) for consistency with the existing codebase. The codebase already uses `_ms` and `_seconds` suffixes consistently, and introducing a different pattern for one field would be inconsistent.

**Default:** 180 seconds (increased from 60)

**Validation:** 30-300 seconds (30s minimum to be useful, 5min maximum to avoid indefinite hangs)

**Usage in audit_report_tool.go:**
```go
timeout := time.Duration(h.config.NativeTools.AuditReport.TimeoutSeconds) * time.Second
aiCtx, cancel := context.WithTimeout(ctx, timeout)
```

**Status:** ✅ Implemented

### 2. Optimize Data Summary (Required)

The goal is to reduce the data volume sent to the AI to:
1. Decrease AI processing time (fewer tokens to analyze)
2. Stay within token limits for smaller context models
3. Focus AI attention on the most relevant data

#### Current Data Volume Analysis

**Fixed components:**
- Instruction prompt template: ~3,500 characters
- Default system prompt: ~500 characters
- **Subtotal:** ~4,000 characters (~1,000 tokens)

**Variable components (worst case with 1000 entries, 100 unique tools, 50 policies):**
- Aggregate statistics: ~200 characters
- Policy effectiveness metrics: ~200 characters
- Top tools (ALL, unlimited): 100 tools × ~50 chars = **5,000 characters**
- Denied policies (ALL, unlimited): 50 policies × ~40 chars = **2,000 characters**
- Sample denied requests (5): 5 × ~300 chars = 1,500 characters
- Sample audit-only denials (5): 5 × ~350 chars = 1,750 characters
- Sample timeout failures (5): 5 × ~300 chars = 1,500 characters
- **Subtotal:** ~12,000+ characters (~3,000 tokens)

**Total worst case:** ~16,000 characters (~4,000-5,000 tokens)

This leaves room for the AI's response within most context windows (8K-128K), but:
- Larger summaries = more processing time
- More noise = less focused analysis
- Token costs scale with input size

#### 2a. Limit Top Tools/Policies to Top N

**Current behavior:** Iterates ALL entries in `TopTools` and `DeniedByPolicy` maps.

**Proposed change:** Limit to top 10 by count.

```go
// Add helper function
func topNByCount(m map[string]int, n int) []struct{ Name string; Count int } {
    // Sort by count descending, return top N
}

// In prepareEntrySummary:
summary += "Top 10 Tools by Usage:\n"
for _, item := range topNByCount(stats.TopTools, 10) {
    summary += fmt.Sprintf("  - %s: %d calls\n", item.Name, item.Count)
}
```

| Aspect | Analysis |
|--------|----------|
| **Expected benefit** | Reduces 5,000-7,000 chars to ~500-700 chars in worst case (10x reduction) |
| **Pros** | Significant size reduction; focuses AI on most active tools; deterministic ordering |
| **Cons** | May miss long-tail patterns; loses visibility into rarely-used tools |
| **Risk** | Low - top tools are most relevant for security analysis |
| **Complexity** | Low - simple sort and slice |

**Alternative considered:** Make N configurable. Rejected because 10 is a reasonable default and adding another config option adds complexity for marginal benefit.

#### 2b. Configurable Sample Limits

**Current behavior:** Hardcoded to 5 samples per category (denied, audit-only, timeout).

**Proposed change:** Make configurable, default to 5.

```go
AuditReport struct {
    // ... existing fields ...
    MaxSamplesPerCategory int `mapstructure:"max_samples_per_category"` // default: 5
}
```

| Aspect | Analysis |
|--------|----------|
| **Expected benefit** | Allows tuning for specific needs; more samples = richer context, fewer = faster |
| **Pros** | User control; can reduce for speed or increase for thoroughness |
| **Cons** | Another config option to document/maintain; most users won't change it |
| **Risk** | Low - bounded by MaxEntries anyway |
| **Complexity** | Low - replace hardcoded 5 with config value |

**Recommendation:** Include but keep as optional enhancement. Default of 5 works well.

#### 2c. Truncate Long Arguments

**Current behavior:** JSON-marshals entire tool arguments with no limit.

```go
args, _ := json.Marshal(entry.Audit.Tool.Params)
```

Tool arguments can be very large (file contents, long queries, base64 data).

**Proposed change:** Truncate to 200 characters.

```go
args, _ := json.Marshal(entry.Audit.Tool.Params)
if len(args) > 200 {
    args = append(args[:197], []byte("...")...)
}
```

| Aspect | Analysis |
|--------|----------|
| **Expected benefit** | Prevents single entry from dominating the summary; bounds worst case |
| **Pros** | Prevents massive prompts from large arguments; consistent entry sizes |
| **Cons** | Loses detail that might be relevant; truncation point is arbitrary |
| **Risk** | Medium - may truncate useful context for understanding the request |
| **Complexity** | Low - simple length check and slice |

**Alternative considered:**
- Configurable limit: Adds complexity for edge case.
- Smart truncation (show beginning and end): More complex, marginal benefit.
- Truncate specific fields only: Requires knowledge of argument structure.

**Recommendation:** Include with 200-char limit. If truncation proves problematic, can adjust later.

#### 2d. Summary of Optimization Impact

| Optimization | Worst Case Reduction | Implementation Effort |
|--------------|---------------------|----------------------|
| Limit tools to top 10 | ~5,000 chars → ~500 chars | Low |
| Limit policies to top 10 | ~2,000 chars → ~400 chars | Low |
| Truncate arguments | Unbounded → bounded at 3,000 chars (15 samples × 200) | Low |
| **Combined** | ~12,000+ chars → ~4,000-5,000 chars | Low |

**Expected overall benefit:** 50-70% reduction in variable summary size, leading to:
- Faster AI processing (fewer tokens to analyze)
- Lower token costs
- More focused analysis on high-signal data

### 3. Connection Timeout Mitigation (Required)

#### 3a. Document MCP Client Timeout Expectations

The MCP protocol uses:
- **STDIO**: No connection timeout (pipes stay open)
- **SSE**: Long-lived connections by design, no request timeout
- **HTTP**: Uses Go's default http.Server (no read/write timeout)

**Risk Assessment:**

| Transport | Risk | Mitigation |
|-----------|------|------------|
| STDIO | Low | Process stays alive, no timeout |
| SSE | Low | Long-lived connection, events stream as ready |
| HTTP | Medium | Go default may timeout on very slow responses |

#### 3b. Add HTTP Server Timeouts (Recommended)

For HTTP transport, add configurable timeouts:

```go
// internal/config/config.go
Server struct {
    // ... existing fields ...
    ReadTimeoutSeconds  int `mapstructure:"read_timeout_seconds"`  // default: 0 (no timeout)
    WriteTimeoutSeconds int `mapstructure:"write_timeout_seconds"` // default: 0 (no timeout)
}
```

**Note:** Setting these too low would break audit reports. Default to 0 (disabled) for backward compatibility, but allow users to configure if needed.

#### 3c. Progress Indication (Future Enhancement)

For very long reports, we could send progress updates. However, this requires:
- MCP protocol support for streaming tool responses
- Changes to the tool response format

**Recommendation:** Defer to future work. For now, rely on adequate timeouts.

### 4. Alternative: Chunked Analysis (Not Recommended)

**Considered but rejected:** Breaking the analysis into multiple AI calls and summarizing.

#### How It Would Work

1. Split audit entries into chunks (e.g., 200 entries each)
2. Send each chunk to AI for individual analysis
3. Collect all chunk analyses
4. Send final summarization request to combine chunk analyses

#### Data Size and Request Limit Analysis

**HTTP POST request limits:**
- OpenAI API: ~4-5MB request body limit
- Most HTTP clients/servers: 10-100MB typical limits
- **Our worst-case prompt:** ~16KB (well under any limit)

**Conclusion:** HTTP POST size is NOT a concern. The real constraints are:

**Token limits by model:**
| Model | Context Window | Our Worst Case (~5K tokens) |
|-------|---------------|---------------------------|
| GPT-4o-mini | 128K tokens | ✅ ~4% of limit |
| GPT-4o | 128K tokens | ✅ ~4% of limit |
| GPT-4 Turbo | 128K tokens | ✅ ~4% of limit |
| GPT-4 (original) | 8K tokens | ⚠️ ~60% of limit (leaves room for response) |
| GPT-3.5 Turbo | 16K tokens | ✅ ~30% of limit |

**With optimizations from Section 2:** Prompt drops to ~2-3K tokens, safe for all models.

#### Why Chunking Doesn't Help

| Aspect | Single Request | Chunked Approach |
|--------|---------------|------------------|
| **Latency** | 1 API call | N+1 API calls (N chunks + summary) |
| **Token usage** | ~5K input + ~2K output | N × ~2K + summary overhead |
| **Context** | AI sees full picture | AI loses cross-chunk patterns |
| **Complexity** | Simple | Significant (chunking logic, aggregation, error handling) |
| **Cost** | Lower | Higher (more total tokens processed) |

**Specific problems with chunking:**
1. **Pattern loss:** A tool called 3 times in chunk 1 and 2 times in chunk 2 won't be recognized as a top-5 tool in either chunk
2. **Correlation loss:** Related events split across chunks lose their relationship
3. **Summary degradation:** "Summarizing summaries" loses nuance and specificity
4. **Latency multiplication:** 5 chunks × 20s each + 30s summary = 130s vs 60-90s single call

#### When Chunking Would Be Appropriate

Chunking would only make sense if:
- Audit logs exceeded 100K+ tokens (not our case)
- We needed real-time streaming progress (different architecture needed)
- Token costs were the primary concern over quality (not our case)

**Recommendation:** Reject chunking. The optimizations in Section 2 are sufficient to keep prompts well within limits, and single-request analysis produces better results.

### 5. Default Timeout Increase (Required)

Change default from 60s to 180s:

```go
// internal/config/config.go (in setDefaults or load function)
v.SetDefault("native_tools.audit_report.timeout_seconds", 180)
```

**Status:** ✅ Implemented

## Implementation Checklist

### Phase 1: Timeout Configuration (Required)
- [x] Add `TimeoutSeconds` field to `AuditReport` config struct
- [x] Add default value (180 seconds) in config loading
- [x] Add validation for `TimeoutSeconds` (30-300 range)
- [x] Update `audit_report_tool.go` to use configurable timeout instead of hardcoded 60s
- [x] Add improved error logging with timeout vs API error distinction
- [ ] Add unit tests for new config field loading and validation
- [ ] Update example config file (`config/maybe-dont.yaml`)

### Phase 2: Data Optimization (Required)
- [ ] Add `topNByCount` helper function for sorting maps by value
- [ ] Limit `TopTools` to top 10 in `prepareEntrySummary`
- [ ] Limit `DeniedByPolicy` to top 10 in `prepareEntrySummary`
- [ ] Add argument truncation (200 char limit) in sample collection functions
- [ ] Add unit tests for `topNByCount` helper
- [ ] Add unit tests verifying summary size reduction

### Phase 3: Optional Enhancements
- [ ] Add `MaxSamplesPerCategory` config field (optional - evaluate if needed)
- [ ] Update CLAUDE.md with new config options

### Phase 4: Documentation
- [ ] Document timeout configuration in user-facing docs
- [ ] Add troubleshooting guidance for timeout errors

## Configuration Example

```yaml
native_tools:
  audit_report:
    enabled: true
    max_entries: 1000
    timeout_seconds: 180  # default 180, range 30-300
    # max_samples_per_category: 5  # Optional, if implemented
```

## Risk Analysis

### MCP Client Timeout Risk

**Question:** Will AI agents timeout waiting for the tool response?

**Analysis:**
1. **Claude Desktop / Claude.ai**: Uses SSE transport. SSE connections are long-lived and designed for streaming. The connection stays open while waiting for tool responses. No known timeout for individual tool calls.

2. **Custom MCP Clients**: Depends on implementation. Well-designed clients should:
   - Use appropriate transport (SSE for long operations)
   - Have configurable or generous timeouts for tool calls
   - Handle the async nature of tool execution

3. **HTTP Transport**: Most at risk. Go's default http.Server has no read/write timeout, but load balancers or proxies in front may timeout.

**Recommendation:**
- Default 120s should be safe for most deployments
- Document that users behind aggressive proxies may need to:
  - Increase proxy timeouts
  - Use SSE transport instead of HTTP
  - Reduce `max_entries` to speed up analysis

### Backward Compatibility

- All new fields have defaults matching or improving current behavior
- No breaking changes to existing configs
- Timeout increase (60s → 120s) only affects edge cases that were already failing

## Success Criteria

1. Audit reports with 1000 entries complete successfully
2. Configurable timeout allows users to tune for their environment
3. Reduced prompt size decreases AI processing time
4. No connection timeouts observed in standard deployments (STDIO, SSE)

## Future Considerations

1. **Streaming responses**: If MCP adds support for streaming tool responses, we could send progress updates during long analyses.

2. **Caching**: Cache AI analysis results for identical audit log snapshots.

3. **Background processing**: For very large analyses, queue the job and notify when complete (requires significant architecture changes).
