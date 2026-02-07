# Confidence Scoring for AI Policy Responses

> **Issue**: [#90 - Add confidence scoring to AI policy responses](https://github.com/maybedont/maybe-dont/issues/90)
> **Status**: Draft

## Post-Merge Re-Review Checklist

> **Context**: The `degroff/test_matrix` branch has a large pending PR that adds the policy test suite runner, executor, state management, and skills (`ai-policy.md`, `test-case.md`). This spec was written with exploratory reads of that branch but should be re-reviewed against the merged code. After merging `test_matrix` into main and main into this branch, re-review this spec asking:
>
> - **Accuracy**: Do the struct names, field names, and file paths still match? Verify `ActualResult`, `CachedResult`, `TestResult`, `AIResponse`, `AIResponseEvaluation`, and `aiRuleResult` against merged code.
> - **Runtime issues**: Trace the full code path for a request validation and response validation with the merged test runner to confirm no gaps.
> - **Documentation**: Confirm whether maybedont.ai/docs content has changed and what pages need updating.
> - **Migration**: Verify backward compatibility claims against the actual structured output enforcement in merged provider code.
> - **Policy versioning**: Check if `test_matrix` introduced any versioning for rule files.
> - **Assumptions**: Validate the assumptions table (Section 14) against merged code.
> - **Risks/downsides/benefits**: Re-assess with full test suite available.
> - **Required tests**: Cross-reference the test plan (Section 16) against existing test coverage in merged code.
> - **CLI proxy**: Check if `test_matrix` implemented any CLI proxy endpoint code.
> - **Skills**: Verify skill file paths and content on main match what this spec references.
> - **State files**: Confirm state file schema version handling and backward compatibility with merged state management code.
> - **Empirical validation**: Confirm test suite model matrix is available and plan the A/B baseline test described in Section 19.

## Summary

Add a confidence score (0.0-1.0) to AI policy responses alongside the existing binary allow/deny decision. Centralize the AI response format instruction so it is owned by the gateway engine rather than duplicated in every rule prompt. Add configurable thresholds so operators can tune sensitivity, and propagate confidence through the audit trail.

## Motivation

Today every AI policy returns `{"allowed": true/false, "message": "..."}`. This has three problems:

1. **No nuance**: A high-confidence "this is definitely malicious" and a low-confidence "I'm not sure but maybe" both produce the same binary deny. Operators cannot distinguish between the two in audit logs or at decision time.
2. **Boilerplate duplication**: Every rule prompt ends with `Return ONLY JSON in this exact format: { "allowed": true/false, "message": "your message" }`. This wastes tokens, introduces inconsistency risk, and makes format changes require editing every rule. More importantly, the AI response format is business logic that belongs to the gateway engine, not to the policy author. Policy authors should focus on describing what to detect, not how to format the response.
3. **No threshold tuning**: Operators cannot adjust sensitivity without rewriting prompts. There is no way to say "only block if the AI is at least 80% confident."

Confidence scoring addresses all three by:
- Adding a `confidence` field (0.0-1.0) to the AI response schema
- Moving the response format instruction out of rule prompts and into a runtime-injected suffix controlled by the engine
- Making decision thresholds configurable at the global and per-rule level
- Propagating scores through audit entries for forensic analysis and threshold tuning

## Consolidation Note

This spec consolidates and supersedes confidence scoring design scattered across three existing specs:

| Spec | Section | Disposition |
|------|---------|-------------|
| `docs/specs/policy-test-suite/README.md` | "Future: Confidence Scoring", `min_confidence` field | Absorbed into [Test Suite Changes](#10-test-suite-changes) below |
| `docs/specs/runtime-action-interception-architecture.md` | "Batching with Structured Outputs" (confidence per policy) | Absorbed into [AI Response Format](#2-centralized-ai-response-format) below |
| `docs/specs/cli-proxy-for-ai-agents.md` | AI rules response with `confidence: 0.0-1.0` | Absorbed into [AI Response Format](#2-centralized-ai-response-format) below |

Once this spec is finalized, those sections should be updated to reference this spec as the authoritative source.

## Feasibility: Can AI Models Reliably Return Confidence Scores?

Before designing the system, it is worth addressing whether LLMs can consistently produce meaningful confidence scores.

### Evidence For

1. **Structured output enforcement**: Both OpenAI and Anthropic support JSON schema-constrained responses (`response_format` / `output_config`). The gateway already uses this via `GenerateSchema[AIResponse]()` with `strict: true`. Adding a `confidence` float field to the schema is mechanically straightforward and the provider will enforce the field is present and numeric.

2. **Calibration research**: Studies on LLM calibration (e.g., Kadavath et al. 2022 "Language Models (Mostly) Know What They Know") show that LLMs can produce reasonably well-calibrated probability estimates when explicitly prompted to do so, particularly for factual questions. Security classification is more subjective, but the signal is still useful for ranking.

3. **Practical precedent**: Systems like GitHub Copilot's content filtering and various AI-powered moderation APIs (OpenAI Moderation, Perspective API) return confidence/severity scores successfully. The pattern is well-established.

4. **Our use case is favorable**: We are not asking for absolute calibration (i.e., "0.8 means exactly 80% probability"). We are asking for a *relative ordering* signal: is the model more confident about case A than case B? This is a much easier bar to clear and is sufficient for threshold tuning and audit triage.

### Known Limitations

1. **Not perfectly calibrated**: A score of 0.85 does not mean 85% probability of being correct in a frequentist sense. Scores should be treated as relative confidence indicators, not absolute probabilities.

2. **Model-dependent distributions**: Different models (gpt-4o-mini vs claude-sonnet) will produce different score distributions for the same inputs. A threshold of 0.7 may be appropriate for one model but too aggressive for another. This is why per-model threshold tuning (enabled by the test suite model matrix) is important.

3. **Prompt sensitivity**: The confidence score is influenced by how the prompt is worded. Our approach of centralizing the response format instruction and providing clear guidance ("1.0 = absolute certainty, 0.5 = uncertain") helps standardize this.

4. **Temperature matters**: Higher temperature increases score variance. The gateway should recommend temperature 0 for deterministic scoring, and the existing `parameters` config already supports this.

5. **Non-determinism at threshold boundaries**: Two identical requests may receive confidence 0.71 and 0.69 from the same model with temperature 0. If the threshold is 0.7, one blocks and the other doesn't. This is inherent to any continuous scoring system and operators should be aware that near-threshold behavior is probabilistic. The audit trail captures the raw score so these cases can be identified and thresholds adjusted.

6. **Potential impact on primary decision quality**: Asking an LLM to simultaneously classify AND self-assess certainty is a different cognitive task than classification alone. There is a risk that adding confidence scoring changes how the model makes its primary allow/deny decision — for example, the model may hedge on borderline cases where it previously would have committed to a clear deny. This is an open empirical question that must be validated before shipping. See [Section 19: Empirical Validation](#19-empirical-validation-required).

### Recommendation

Confidence scoring is viable and likely valuable. The key is to treat scores as **ordinal signals for ranking and thresholding**, not as calibrated probabilities. However, we must empirically validate that adding confidence does not degrade the quality of the primary allow/deny decision before shipping this feature. The test suite model matrix provides the mechanism to do this.

### Alternative Considered: Categorical Confidence

Instead of a continuous 0.0-1.0 score, we considered asking the AI to return a categorical level (`high`, `medium`, `low`). This has fewer calibration concerns but sacrifices granularity for threshold tuning and makes audit analytics less useful (you cannot compute distributions or percentiles over categories). The continuous score with well-documented semantics is the better choice, and can always be bucketed into categories at the presentation layer.

## Design

### 1. Design Decision: Additive Confidence vs. Response Format Replacement

An earlier revision of this spec proposed replacing the entire response format: changing `allowed` (bool) to `decision` (string enum) and `message` to `reason`. After review, we recommend the **additive approach** instead: keep the existing `allowed` and `message` fields, and add `confidence` alongside them.

#### Option A: Additive (recommended)

```go
type AIResponse struct {
    Allowed    bool    `json:"allowed"`
    Confidence float64 `json:"confidence" jsonschema:"minimum=0,maximum=1"`
    Message    string  `json:"message"`
}
```

**Advantages:**
- **Zero breaking change** to the response schema — purely additive
- **No legacy format detection needed** — old responses just lack `confidence`
- **No prompt regression risk from field renaming** — the AI is still answering the same question (`allowed: true/false`), just adding a self-assessment
- **Existing inversion logic is tested and works** — no need to rewrite decision logic
- **Simpler implementation** — fewer moving parts, fewer tests, lower risk
- **Decouples concerns** — confidence scoring ships without the risk of a response format overhaul

**Disadvantages:**
- The inversion logic remains (deny-rule + `allowed: false` = deny), which is confusing to read in code
- `allowed` is less expressive than a string enum for response validation (no first-class `redact` value)

#### Option B: Full replacement (deferred)

```go
type AIResponse struct {
    Decision   string  `json:"decision" jsonschema:"enum=allow,enum=deny"`
    Confidence float64 `json:"confidence" jsonschema:"minimum=0,maximum=1"`
    Reason     string  `json:"reason"`
}
```

This approach has merits (cleaner API, eliminates inversion logic, first-class `redact` support) but introduces unnecessary risk by bundling two independent changes. The `decision` enum can be revisited as a separate, future enhancement once confidence scoring is stable and validated.

#### Decision

**Go with Option A (additive).** Ship the confidence score alongside the existing binary response. Revisit the response format cleanup separately if desired.

### 2. Centralized AI Response Format (Engine-Owned Prompt Suffix)

**Regardless of whether we keep `allowed`/`message` or switch to `decision`/`reason`**, the response format instruction should be owned by the gateway engine, not duplicated in every policy prompt.

**Current state**: Each rule prompt contains a line like:
```
Return ONLY JSON in this exact format: { "allowed": true/false, "message": "your message" }
```

This is wrong for several reasons:
- The response format is **business logic of the gateway**, not a concern of the policy author
- Policy authors should describe **what to detect**, not how to structure the response
- Duplicating the format instruction in every rule wastes tokens and risks inconsistency
- Changing the format (e.g., adding `confidence`) requires editing every rule
- Asking the user to get the format exactly right is error-prone and unnecessary

**New state**: The response format instruction is removed from rule prompts and injected at runtime by the AI engine. Rule authors write only the analytical prompt.

#### Runtime-injected response instruction

When the AI engine constructs the prompt for a policy evaluation, it appends a standardized suffix after the user's prompt. The suffix differs between request and response validation:

**Request validation suffix:**
```
---
Respond with a JSON object containing exactly these fields:
- "allowed": true if the operation appears safe, false if it appears dangerous.
- "confidence": A number between 0.0 and 1.0 indicating how confident you are in your assessment. Use 1.0 for absolute certainty, 0.7+ for high confidence, 0.5 for uncertain, below 0.3 for very low confidence.
- "message": A brief explanation of your reasoning.
```

**Response validation suffix:**
```
---
Respond with a JSON object containing exactly these fields:
- "allowed": true if the response content is safe, false if it should be blocked or redacted.
- "confidence": A number between 0.0 and 1.0 indicating how confident you are in your assessment. Use 1.0 for absolute certainty, 0.7+ for high confidence, 0.5 for uncertain, below 0.3 for very low confidence.
- "message": A brief explanation of your reasoning.
- "redacted_content": If sensitive content was found that should be sanitized, provide the redacted version. Otherwise leave empty.
```

These suffixes are appended by the engine, not written by the rule author. The JSON schema constraint (`GenerateSchema[AIResponse]()` / `GenerateSchema[AIResponseEvaluation]()`) enforces the structure at the provider level.

#### Migration of existing rules

All shipped default rules in `ai_request_rules.yaml` and `ai_response_rules.yaml` will have their response format boilerplate removed. The `%s` placeholder and analytical content remain unchanged. For example:

**Before:**
```yaml
prompt: |-
  ANALYZE: Does this operation delete multiple files...
  ...
  Tool call: %s
  Return ONLY JSON in this exact format: { "allowed": true/false, "message": "your message" }
```

**After:**
```yaml
prompt: |-
  ANALYZE: Does this operation delete multiple files...
  ...
  Tool call: %s
```

The engine appends the response format instruction automatically.

#### Backward compatibility for user-authored rules

Users who have written custom rules with response format instructions in their prompts will not break. There are two scenarios:

**Providers with structured output support (OpenAI, Anthropic):** The JSON schema constraint forces the AI to return the schema-defined format regardless of what the prompt says. User prompts that include old instructions like `Return JSON: {"allowed": true/false, "message": "..."}` are consistent with the schema (we kept the same field names). The prompt instruction and the engine suffix now say the same thing — the only difference is the engine suffix also asks for `confidence`. The old prompt text becomes partially redundant but entirely harmless.

**Providers without structured output support (some OpenAI-compatible endpoints):** The AI will see both the user's format instruction and the engine suffix. Since both ask for `allowed` and `message`, there is no conflict — the engine suffix simply adds `confidence`. The AI may or may not include `confidence` in its response. If `confidence` is missing from the parsed response, the engine defaults to `1.0` to preserve existing behavior.

### 3. Decision Logic

The existing truth-table inversion logic is unchanged:

| Policy Action | AI `allowed` | Result |
|---------------|-------------|--------|
| deny | true | allow |
| deny | false | deny |
| allow | true | allow |
| allow | false | deny |

#### Confidence threshold application

The confidence threshold is applied **only when the AI's decision would cause the rule to fire** — that is, only when the result would be a blocking action:

```
# Existing logic determines the result
result = apply_truth_table(p.action, aiResp.allowed)

# Confidence threshold applies only to firing (blocking) decisions
rule_would_fire = (result == "deny") or (result == "redact")

if rule_would_fire:
    threshold = p.confidence_threshold ?? global_confidence_threshold
    if aiResp.confidence < threshold:
        effective_result = low_confidence_action  # default: "audit_only"
        confidence_applied = true
    else:
        effective_result = result
        confidence_applied = false
else:
    effective_result = result
    confidence_applied = false
```

**Why directional application matters**: If the AI returns `allowed: true` with 0.4 confidence on a deny-rule, the rule already doesn't fire — the request passes. Applying the threshold here would incorrectly flip the result. The threshold only makes sense when it can *soften* a blocking decision, not when it would *create* one.

This means: if a deny-rule's AI says `allowed: false` with 0.4 confidence and the threshold is 0.7, the deny is downgraded to audit-only (logged but not enforced). But if the AI says `allowed: true` with 0.4 confidence, the allow stands — the low confidence is logged in the audit trail for analysis, but does not change the outcome.

### 4. Configuration Changes

#### Global AI confidence settings

```yaml
validation:
  ai:
    # ... existing fields (endpoint, model, api_key, parameters) ...
    confidence_threshold: 0.7           # Default: 0.7
    low_confidence_action: "audit_only" # Default: "audit_only"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `confidence_threshold` | float64 | 0.7 | Minimum confidence to enforce a blocking AI decision. Below this, `low_confidence_action` applies. |
| `low_confidence_action` | string | `audit_only` | What to do when confidence is below threshold on a blocking decision. Options: `audit_only` (log but allow), `deny` (conservative: treat uncertainty as denial). |

#### Per-rule threshold override

```yaml
rules:
  - name: "Check credential file access"
    action: deny
    confidence_threshold: 0.5  # Override: lower threshold for this high-risk rule
    prompt: |-
      ANALYZE: Does this operation access credential files?
      ...
```

Per-rule `confidence_threshold` overrides the global value. If omitted, the global value applies.

#### CEL rules

CEL rules are deterministic and do not produce confidence scores from an AI. When a CEL rule matches, the system assigns `confidence: 1.0`. When it does not match, no result is produced. This requires no configuration changes for CEL. The `ValidationResult` struct (shared by both engines) needs a `Confidence` field so CEL results can carry the 1.0 value through the validation chain.

#### Validation at startup

- `confidence_threshold` must be in range [0.0, 1.0] (inclusive)
- `low_confidence_action` must be one of: `audit_only`, `deny`
- Per-rule `confidence_threshold` is validated with the same range
- Invalid values cause startup failure with a clear error message

### 5. AIResponse Struct and Schema

```go
// AIResponse is the structured response expected from AI request policy evaluations.
// The JSON schema is generated via GenerateSchema[AIResponse]() and enforced by
// the AI provider's structured output feature.
type AIResponse struct {
    Allowed    bool    `json:"allowed"`
    Confidence float64 `json:"confidence" jsonschema:"minimum=0,maximum=1"`
    Message    string  `json:"message"`
}

// AIResponseEvaluation is the structured response expected from AI response policy evaluations.
// It extends AIResponse with a RedactedContent field for response sanitization.
type AIResponseEvaluation struct {
    Allowed         bool    `json:"allowed"`
    Confidence      float64 `json:"confidence" jsonschema:"minimum=0,maximum=1"`
    Message         string  `json:"message"`
    RedactedContent string  `json:"redacted_content"`
}
```

The `jsonschema` tags ensure provider-level enforcement:
- `confidence` is constrained to [0.0, 1.0]
- `allowed` and `message` are unchanged from current behavior

When `confidence` is missing from the AI response (e.g., non-compliant provider, or pre-existing cached response), it deserializes to `0.0` (Go zero value). The engine treats `0.0` as "no confidence data" and defaults to `1.0` to preserve existing behavior. This is handled explicitly in the parsing code, not by relying on the zero value:

```go
if aiResp.Confidence == 0.0 {
    // Provider did not return confidence — preserve existing behavior
    aiResp.Confidence = 1.0
}
```

**Note**: This means a model cannot express genuine 0.0 confidence. This is acceptable — 0.0 ("I have literally zero signal") is not a meaningful real-world response, and using it as a sentinel for "missing" is safe.

### 6. Internal Result Struct Changes

The `aiRuleResult` internal struct (used to pass results through goroutine channels) must also carry the confidence score:

```go
type aiRuleResult struct {
    rule              string
    action            config.PolicyAction
    mode              config.PolicyMode
    result            string              // "allow", "deny", "redact", or "error"
    message           string
    confidence        float64             // NEW: raw AI confidence score
    confidenceApplied bool                // NEW: true if threshold changed outcome
    evaluationMs      int64
    err               error
    errCategory       string
    providerRequestID string
}
```

This struct is goroutine-safe (written by one goroutine, read by the collector via channel). Adding fields does not introduce concurrency concerns.

### 7. Audit Entry Changes

#### AuditAIRuleResult (per-rule)

```go
type AuditAIRuleResult struct {
    Rule              string  `json:"rule"`
    Action            string  `json:"action"`
    Mode              string  `json:"mode,omitempty"`
    Result            string  `json:"result"`             // "allow", "deny", "redact", or "error"
    Confidence        float64 `json:"confidence"`         // 0.0-1.0 (1.0 for missing, 0.0 for error)
    ConfidenceApplied bool    `json:"confidence_applied"` // true if threshold changed the outcome
    EvaluationMs      int64   `json:"evaluation_ms"`
    Error             string  `json:"error,omitempty"`
}
```

New fields:
- `confidence`: The raw score returned by the AI. Set to `1.0` when confidence was missing from the response (backward compat), `0.0` for errors and budget-exhaustion fail-opens.
- `confidence_applied`: `true` when the confidence was below threshold and the result was changed from the AI's decision to `low_confidence_action`. This makes it easy to find cases in audit logs where the threshold changed the outcome.

#### AuditAIResult (aggregate)

```go
type AuditAIResult struct {
    Action       string              `json:"action"`
    BlockedMs    int64               `json:"blocked_ms"`
    EvaluationMs int64               `json:"evaluation_ms"`
    DecidingRule string              `json:"deciding_rule,omitempty"`
    Reason       string              `json:"reason,omitempty"`
    RequestID    string              `json:"request_id,omitempty"`
    Results      []AuditAIRuleResult `json:"results"`
    // No aggregate confidence — individual rule scores are more useful for analysis
}
```

The aggregate level does not include a rolled-up confidence score because the semantics are unclear (min? mean? of the deciding rule?). The per-rule `confidence` is sufficient. Consumers who want the deciding rule's confidence can look up `results[deciding_rule].confidence`.

### 8. Validation Chain Changes

The `ValidationResult` struct in `tool_validation.go` is shared by both CEL and AI engines and needs a `Confidence` field:

```go
type ValidationResult struct {
    PolicyName string              `json:"policy_name"`
    PolicyType string              `json:"policy_type"` // "cel" or "ai"
    Action     config.PolicyAction `json:"action"`
    Mode       config.PolicyMode   `json:"mode"`
    Message    string              `json:"message,omitempty"`
    Error      string              `json:"error,omitempty"`
    DurationMs int64               `json:"duration_ms"`
    Confidence float64             `json:"confidence"` // NEW: 1.0 for CEL, AI score for AI
}
```

CEL results always set `Confidence: 1.0`. AI results set it from the parsed response.

### 9. Prompt Construction Changes

**Current flow** (`ai_engine.go`):
```go
toolCallStr := fmt.Sprintf("Tool: %s\nArguments: %v", req.Params.Name, req.Params.Arguments)
result, err := e.providerClient.Generate(ctx, AIRequest{
    UserPrompt:     fmt.Sprintf(p.Prompt, toolCallStr),
    ResponseSchema: GenerateSchema[AIResponse](),
})
```

**New flow**:
```go
toolCallStr := fmt.Sprintf("Tool: %s\nArguments: %v", req.Params.Name, req.Params.Arguments)

// Expand the user's prompt template with the tool call
userPrompt := fmt.Sprintf(p.Prompt, toolCallStr)

// Append the centralized response format instruction (owned by the engine)
userPrompt += "\n\n" + aiRequestResponseFormatInstruction

result, err := e.providerClient.Generate(ctx, AIRequest{
    UserPrompt:     userPrompt,
    ResponseSchema: GenerateSchema[AIResponse](),
})
```

The response engine (`ai_response_engine.go`) follows the same pattern with `aiResponseResponseFormatInstruction` and `GenerateSchema[AIResponseEvaluation]()`.

Both instruction constants are package-level:

```go
const aiRequestResponseFormatInstruction = `---
Respond with a JSON object containing exactly these fields:
- "allowed": true if the operation appears safe, false if it appears dangerous.
- "confidence": A number between 0.0 and 1.0 indicating how confident you are in your assessment. Use 1.0 for absolute certainty, 0.7+ for high confidence, 0.5 for uncertain, below 0.3 for very low confidence.
- "message": A brief explanation of your reasoning.`

const aiResponseResponseFormatInstruction = `---
Respond with a JSON object containing exactly these fields:
- "allowed": true if the response content is safe, false if it should be blocked or redacted.
- "confidence": A number between 0.0 and 1.0 indicating how confident you are in your assessment. Use 1.0 for absolute certainty, 0.7+ for high confidence, 0.5 for uncertain, below 0.3 for very low confidence.
- "message": A brief explanation of your reasoning.
- "redacted_content": If sensitive content was found that should be sanitized, provide the redacted version. Otherwise leave empty.`
```

### 10. Test Suite Changes

The policy test suite (on the `degroff/test_matrix` branch) has partial scaffolding for confidence:
- `CachedResult` in `state.go` already has a `Confidence` field
- The runner already writes `result.Actual.Confidence` to cached state
- The custom JSON output format includes `confidence` per result
- The spec defines `overall.min_confidence` in test expectations

**NOTE**: The `ActualResult` struct in `executor.go` does NOT yet have a `Confidence` field — it must be added. The runner code already references it, so the plumbing is in place but the field definition is missing.

This feature completes the integration:

#### Test case expectations

Add optional `min_confidence` to test case expectations:

```yaml
expectations:
  decision: deny
  min_confidence: 0.8  # Optional: require at least 80% confidence
  policies:
    - policy_name: "detect-mass-deletion"
      decision: deny
      min_confidence: 0.7  # Optional: per-policy confidence expectation
```

#### Test runner behavior

- When `min_confidence` is specified and the AI returns a confidence below it, the test fails with a message like: `"confidence 0.52 below minimum 0.80"`
- When `min_confidence` is omitted, any confidence is accepted (only decision is checked)
- CEL test results always have confidence `1.0`

#### State file compatibility

The state file schema (`schema_version: "v1"`) already includes `CachedResult.Confidence`. Old state files missing the confidence field will deserialize with `confidence: 0.0` (Go's zero value). This is functionally correct but has an edge case: a `0.0` confidence looks like a very low-confidence result rather than "no data." This is acceptable — the state will be refreshed on next run.

**Cache invalidation note**: If the confidence calculation logic changes (e.g., threshold semantics) but the policy files don't change, the policy hashes remain the same and cached results are NOT invalidated. This is acceptable for the initial release. If it becomes a problem, a schema version bump in the state file would force a full re-run.

#### Model matrix validation

The test suite's model matrix feature becomes the primary mechanism for validating default thresholds:
- Run the same test suite across all supported models
- If a model consistently produces confidence below the default threshold for tests that should match, the threshold needs adjustment or the prompt needs improvement
- The custom JSON output format already captures per-result confidence, enabling distribution analysis

### 11. Skill Updates

#### `cel-policy-authoring` skill

Add a brief note explaining that CEL rules always produce confidence 1.0 since they are deterministic. No changes to the authoring guidance itself.

#### `ai-policy-authoring` skill (`.claude/skills/` and `internal/skills/ai-policy.md`)

Major updates:
1. **Remove response format from examples**: All example prompts should end at the analytical content. The response format instruction is injected by the engine.
2. **Remove "Specify the response format" from best practices**: Replace with a note that the engine handles response format automatically. Policy authors should focus on describing what to detect, not how to format the response.
3. **Add confidence guidance**: Explain that the AI will be asked to return a confidence score and how thresholds work.
4. **Update the "Expected AI Response Format" section**: Show the format with the added `confidence` field.
5. **Update Common Mistakes table**: Remove "Vague response format" (no longer relevant). Add "Specifying response format in prompt" as a mild anti-pattern (harmless but wasteful of tokens).

#### `policy-test-case` skill (`internal/skills/test-case.md`)

1. **Add `min_confidence` to field reference**: Document the optional field in expectations.
2. **Add examples**: Show test cases with `min_confidence` assertions.
3. **Add guidance**: "Use `min_confidence` to validate that the AI model is sufficiently confident in its decisions. This is especially useful in model matrix testing to identify models that are unreliable for specific policy types."

### 12. Default Rules Migration

All shipped rules in `internal/config/defaults/ai_request_rules.yaml` and `ai_response_rules.yaml` need updating:

1. **Remove response format boilerplate**: Delete the `Return ONLY JSON...` line from every rule prompt.
2. **Remove example JSON responses from prompts**: The EXAMPLES sections currently include the response JSON (e.g., `-> { "allowed": true, "message": "..." }`). These should be simplified to just the classification (e.g., `-> SAFE: Not a deletion operation` or `-> DANGEROUS: Wildcard deletion detected`).
3. **No confidence_threshold per rule** (for defaults): The shipped rules should rely on the global threshold. Per-rule overrides are for user customization.

**Example migration**:

Before:
```yaml
prompt: |-
  ANALYZE: Does this operation delete multiple files...

  EXAMPLES:
  - kubectl get po -> NOT deletion -> { "allowed": true, "message": "Not a deletion operation" }
  - kubectl delete po --all -> IS dangerous -> { "allowed": false, "message": "Wildcard deletion blocked" }

  Tool call: %s

  Return ONLY JSON in this exact format: { "allowed": true/false, "message": "your message" }
```

After:
```yaml
prompt: |-
  ANALYZE: Does this operation delete multiple files...

  EXAMPLES:
  - kubectl get po -> SAFE: Not a deletion operation
  - kubectl delete po --all -> DANGEROUS: Wildcard deletion of all pods

  Tool call: %s
```

### 13. Policy File Versioning

**Current state**: Rule files (`ai_request_rules.yaml`, etc.) have no version field. The config loader unmarshals YAML directly into structs with no schema validation. The test suite's `suite.yaml` has `version: "v1"` but rule files don't follow this pattern.

**This change does not break the rule file schema** — the same YAML fields (`name`, `description`, `prompt`, `action`, `message`, `enabled`, `mode`) continue to work. The only addition is the optional `confidence_threshold` per rule. However, this is a good opportunity to establish a versioning convention for future breaking changes.

**Proposal**: Add an optional `version` field to rule files:

```yaml
version: "1"  # Optional, for forward compatibility
rules:
  - name: "rule-name"
    ...
```

Behavior:
- If `version` is absent, treat as version 1 (backward compatible)
- If `version` is present and unrecognized, fail at startup with a clear error: `"unsupported rule file version: X, supported: 1"`
- Shipped default rules should include `version: "1"`

This does not need to gate the confidence scoring feature — it is a low-cost addition that provides a migration hook for the future.

### 14. Backward Compatibility and Migration Strategy

**User-authored rules do not need modification.** The engine changes are invisible to rule authors:

| Scenario | What happens | User action required |
|----------|-------------|---------------------|
| User rule with old response format boilerplate | Engine suffix adds `confidence` to the same `allowed`/`message` format. No conflict. | None (optional cleanup to save tokens) |
| User rule without response format boilerplate | Engine appends instruction automatically | None |
| User rule with `confidence_threshold` | Per-rule threshold applied | None (new opt-in feature) |
| AI response missing `confidence` field | Engine defaults to confidence 1.0 (preserve existing behavior) | None |

**Shipped default rules WILL be modified** to remove boilerplate and simplify examples. This is safe because defaults are embedded at compile time — users who have copied and modified them are already diverged.

### 15. Assumptions

| Assumption | Basis | Risk if wrong |
|-----------|-------|---------------|
| `ai_response_engine.go` is structurally parallel to `ai_engine.go` | Code exploration confirmed parallel but independent structs | Low — changes need to be applied to both files |
| CLI proxy endpoint is not yet implemented | No handler code found in `server.go` or `gateway.go` | Low — if it landed on `test_matrix`, the re-review will catch it |
| `ActualResult` struct in test executor does NOT have `Confidence` field | Explored on `test_matrix` branch; `CachedResult` has it, `ActualResult` doesn't | Low — easy to add; re-review after merge will confirm |
| Skills exist on `test_matrix` branch only (`internal/skills/ai-policy.md`, `internal/skills/test-case.md`) | Only `cel-policy-authoring` exists on main in `.claude/skills/` | Medium — if skills aren't merged first, Phase 4 targets files that don't exist |
| State file `schema_version: "v1"` has no upgrade logic | Explored state.go; load() does not validate version | Low — old files deserialize cleanly with zero-value defaults |
| `GenerateSchema` supports `jsonschema` tags for min/max | `invopop/jsonschema` library is used; `jsonschema:"required"` tag already used elsewhere in codebase | Low — verify tag syntax against library docs |
| `maybedont__generate_audit_report` tool will handle new audit fields | Tool uses AI to analyze audit entries; new fields change the schema it reads | Medium — may need prompt update to reference confidence |
| Blocking budget exhaustion produces a result that needs confidence | Budget exhaustion triggers fail-open with `result: "allow"` | Low — assign confidence 0.0, same as errors |
| Adding confidence does not degrade primary decision quality | Untested assumption — see Section 19 | **High — must be empirically validated before shipping** |

### 16. CLI Proxy Impact

**The CLI proxy endpoint does not exist yet** — it is spec-only (`docs/specs/cli-proxy-for-ai-agents.md`). When implemented, it will use the same `Gateway.ValidateToolCall()` -> `validationChain.Handle()` path as MCP requests. Confidence scoring will flow through automatically because:

1. The validation chain is shared — CLI and MCP requests use the same handlers
2. The `AIResponse` struct change applies to the shared AI engine
3. Audit entries are generated by the same code path

**Spec update needed**: The CLI proxy spec's REST API response structure should be updated to include `confidence` per result:
```json
{
  "results": [
    {
      "policy_name": "general-safety-check",
      "policy_type": "ai",
      "action": "allow",
      "message": "Command is safe to execute",
      "confidence": 0.95
    }
  ]
}
```

This should be added to Phase 5 (existing spec cleanup).

### 17. Required Tests

#### AI Engine (`ai_engine.go` and `ai_response_engine.go`)

**Response parsing:**
- Parse response with `confidence` field correctly
- Parse response without `confidence` field -> defaults to 1.0
- Confidence 0.0 from AI treated as missing -> defaults to 1.0

**Confidence threshold logic:**
- Deny fires at 0.8 with threshold 0.7 -> blocks (threshold met)
- Deny fires at 0.5 with threshold 0.7 -> audit_only (threshold not met, downgraded)
- Allow at 0.5 on deny-rule -> rule doesn't fire (threshold NOT applied to non-firing decisions)
- Deny fires at 0.5 with threshold 0.7 and `low_confidence_action: "deny"` -> blocks (conservative mode)
- Per-rule threshold overrides global threshold
- Boundary: deny fires at exactly 0.7 with threshold 0.7 -> blocks (>= semantics)

**Error and edge cases:**
- AI call error -> confidence 0.0, result "error"
- Blocking budget exhaustion -> confidence 0.0
- Async completion carries confidence through correctly

**Audit recording:**
- `AuditAIRuleResult.Confidence` populated with raw AI score
- `AuditAIRuleResult.ConfidenceApplied` is true when threshold changed outcome
- `AuditAIRuleResult.ConfidenceApplied` is false when decision was not a firing decision
- `aiRuleResult` carries confidence through goroutine channel

#### Config Validation

- `confidence_threshold` in range [0.0, 1.0] accepted
- `confidence_threshold` < 0.0 -> startup error
- `confidence_threshold` > 1.0 -> startup error
- `low_confidence_action` = "audit_only" accepted
- `low_confidence_action` = "deny" accepted
- `low_confidence_action` = "invalid" -> startup error
- Per-rule `confidence_threshold` validated independently
- Environment variable override: `MAYBE_DONT_VALIDATION_AI_CONFIDENCE_THRESHOLD`
- Default values when not specified (0.7 and "audit_only")

#### Validation Chain

- `ValidationResult.Confidence` set to 1.0 for CEL results
- `ValidationResult.Confidence` set from AI score for AI results
- Combined CEL + AI results preserve per-engine confidence

#### Audit Entries

- JSON serialization includes new fields
- Backward-compatible deserialization: old audit entries without confidence deserialize cleanly

#### Test Suite Runner

- `min_confidence` expectation passes when confidence >= threshold
- `min_confidence` expectation fails when confidence < threshold
- Omitted `min_confidence` accepts any confidence
- CEL test results always have confidence 1.0
- State file compatibility: old state files load with confidence 0.0
- State file correctly caches and restores confidence values
- Model matrix output includes confidence in results

### 18. Documentation Changes

This feature requires updates to external documentation at `https://maybedont.ai/docs`:

- [ ] **Configuration reference**: Document `confidence_threshold` and `low_confidence_action` fields under `validation.ai`
- [ ] **AI rule authoring guide**: Update to reflect that response format is injected by the engine; rule authors should not include it
- [ ] **Audit log schema reference**: Document new `confidence` and `confidence_applied` fields in `AuditAIRuleResult`
- [ ] **Threshold tuning guide**: New page explaining how to interpret confidence scores, adjust thresholds, and use the model matrix to validate
- [ ] **Changelog / migration notes**: Document the addition of confidence scoring, emphasizing that existing rules continue to work unchanged

The `maybe-dont.yaml` example config (shipped with the binary) must also be updated to show the new fields with their defaults.

### 19. Empirical Validation (Required)

**This section describes testing that MUST be completed before this feature ships.** Adding a confidence score to the AI response changes the cognitive task for the model. Instead of just classifying ("is this dangerous?"), the model must now classify AND self-assess ("is this dangerous, and how sure am I?"). This could affect decision quality in several ways:

#### Concern: Decision quality degradation

There is research suggesting that asking LLMs to simultaneously produce a classification and a confidence score can change the primary classification. Potential effects:

1. **Hedging**: The model may become less decisive on borderline cases. Where it previously committed to `allowed: false`, it might now return `allowed: true` with low confidence, because the confidence framing encourages it to express uncertainty rather than commit.

2. **Anchoring on confidence**: The model may anchor on producing a "reasonable-looking" confidence score and let that influence the primary decision. For example, if it thinks the confidence should be around 0.5, it might rationalize an `allowed: true` to match that moderate confidence.

3. **Increased token overhead**: The confidence instruction adds ~80 tokens per call. With 7 rules, that's ~560 extra input tokens per request. This marginally reduces the token budget available for reasoning.

4. **No effect (best case)**: The model treats the confidence as a separate output dimension and the primary classification is unaffected. This is the most likely outcome for well-structured prompts, but must be verified.

#### Validation plan

Use the policy test suite model matrix to compare decision quality with and without confidence scoring:

**Step 1: Baseline (no confidence)**
- Run the full test suite with the current binary format (`allowed`/`message` only)
- Record per-test decisions across all models in the matrix
- This is the ground truth

**Step 2: With confidence**
- Run the same test suite with confidence scoring enabled (`allowed`/`confidence`/`message`)
- Record per-test decisions AND confidence scores across all models

**Step 3: Compare**
- For each model: how many test cases changed their primary `allowed` decision?
- Categorize changes: did the model become more permissive (false -> true) or more restrictive (true -> false)?
- Are the changed cases borderline (expected) or clear-cut (concerning)?
- What is the confidence score distribution? Is there meaningful variance, or does the model cluster around 0.9-1.0?

**Step 4: Decide**
- If decision quality is maintained or improved: ship with confidence
- If decision quality degrades significantly: investigate prompt adjustments, or consider shipping confidence as audit-only (always recorded but not used for thresholding) until prompt tuning resolves the issue
- If confidence scores cluster tightly (e.g., everything is 0.95-1.0): the signal is not useful and thresholding adds complexity without benefit — reconsider whether continuous scoring is worth it vs. just centralizing the prompt

#### Research to conduct

Before or alongside the empirical testing:
- Survey existing literature on LLM confidence calibration in classification tasks
- Review how other AI security/moderation systems handle confidence (OpenAI Moderation API, Perspective API, AWS Comprehend)
- Check if any providers offer native confidence/logprob features that could be used instead of asking the model to self-report

### 20. Tradeoffs: Confidence Scoring vs. Binary Approach

#### Risks of moving to confidence scoring

1. **Potential decision quality degradation**: Adding confidence may change how the model makes its primary allow/deny decision. This is the single biggest risk and must be empirically validated (see Section 19).

2. **Increased latency from prompt suffix**: The response format instruction adds ~80 tokens per AI call. With 7 enabled rules, that's ~560 extra input tokens per request. Small but measurable.

3. **Non-determinism at threshold boundaries**: Two identical requests may get confidence 0.71 and 0.69 from the same model with temperature 0. If the threshold is 0.7, one blocks and one doesn't. The binary approach doesn't have this edge case.

4. **Threshold cliff effect**: A deny at 0.69 becomes audit-only while a deny at 0.71 blocks. There is no buffer zone. Users may not understand why seemingly identical requests are treated differently.

5. **Operational complexity**: Operators must understand confidence thresholds, not just enable/disable and audit_only. The config surface area grows.

6. **Debugging is harder**: "Why was this request allowed?" goes from "the AI said allowed=true" to "the AI said allowed=false with 0.65 confidence, below the 0.7 threshold, downgraded to audit_only." More moving parts.

7. **Model switching becomes riskier**: Changing models could shift the confidence distribution enough to change blocking behavior. With binary, model changes affect accuracy but not threshold logic.

#### Benefits of the previous binary approach

- **Simplicity**: The decision path is short — AI says yes/no, combine with rule action, done.
- **Deterministic behavior**: Same input, same model -> same result (no threshold edge cases).
- **Easier to debug**: Two-step chain from AI response to outcome.
- **No tuning required**: Works out of the box without threshold configuration.
- **Lower cognitive load for operators**: enable/disable and audit_only are the only knobs.
- **No risk to decision quality**: The model does exactly what it does today.

#### Why confidence scoring is still the right direction

The binary approach is fine when you trust the AI's judgment completely. Confidence scoring is better when you want to *observe* the AI's judgment before trusting it — which is exactly the position of operators deploying a security gateway for the first time. The ability to see "this rule denied with 0.45 confidence" vs "this rule denied with 0.98 confidence" fundamentally changes how operators tune and trust the system. The test suite model matrix provides the mechanism to validate thresholds before deployment, and the `audit_only` default for `low_confidence_action` means the system is safe by default.

**Importantly**: even if empirical testing shows that confidence scores are not useful for thresholding (e.g., scores cluster too tightly), the prompt centralization change is independently valuable. Moving the response format out of policy prompts and into the engine is the right separation of concerns regardless of confidence scoring.

## Implementation Plan

### Phase 0: Empirical Validation
1. Establish baseline: run test suite with current binary format, record all decisions
2. Add confidence to schema and prompt suffix (local branch, not shipped)
3. Run test suite with confidence enabled, record decisions and scores
4. Compare: identify any decision changes, analyze confidence distributions
5. Go/no-go decision on confidence thresholding based on results

### Phase 1: Core Infrastructure
6. Add `confidence` field to `AIResponse` and `AIResponseEvaluation` structs
7. Add `aiRuleResult.confidence` and `aiRuleResult.confidenceApplied` fields
8. Add response format instruction constants (request and response variants)
9. Update prompt construction to append response format suffix in both engines
10. Handle missing confidence (default to 1.0)
11. Add config fields (`confidence_threshold`, `low_confidence_action`) with validation
12. Add per-rule `confidence_threshold` to `AIPolicy` and `AIResponsePolicy` config structs
13. Update confidence threshold application in decision logic (directional)
14. Update `AuditAIRuleResult` with `confidence` and `confidence_applied` fields
15. Add `Confidence` field to `ValidationResult` struct
16. Update mock AI client for tests

### Phase 2: Default Rules Migration
17. Strip response format boilerplate from all shipped AI request rules
18. Strip response format boilerplate from all shipped AI response rules
19. Simplify EXAMPLES in shipped rules (remove JSON response examples)
20. Update `maybe-dont.yaml` example config with new fields
21. Add optional `version: "1"` to shipped rule files

### Phase 3: Test Suite Integration
22. Add `Confidence` field to `ActualResult` struct in executor
23. Wire confidence from AI responses through test runner
24. Add `min_confidence` to test case expectation validation
25. Update test output formatters to show confidence

### Phase 4: Skill Updates
26. Update `ai-policy-authoring` skill
27. Update `policy-test-case` skill
28. Add confidence note to `cel-policy-authoring` skill

### Phase 5: Existing Spec and Documentation Cleanup
29. Update `policy-test-suite/README.md` to reference this spec
30. Update `runtime-action-interception-architecture.md` to reference this spec
31. Update `cli-proxy-for-ai-agents.md` to reference this spec (including REST response structure)
32. Create documentation update checklist for `maybedont.ai/docs`

## Open Questions

1. **Default threshold value**: The issue suggests 0.7. Should we run the test suite across models before committing to a default, or ship 0.7 and adjust based on feedback? (Recommendation: ship 0.7 with clear documentation that it should be tuned per-model. Phase 0 validation will inform whether this is reasonable.)

2. **Response format injection point**: Should the response format instruction be appended to the user prompt (as proposed) or provided as a system prompt? System prompts are more semantically appropriate for meta-instructions, but not all providers handle them identically. (Recommendation: user prompt suffix, since it's simpler and the gateway already does not use system prompts for policy evaluation.)

3. **Confidence on error and budget exhaustion**: When the AI call fails (timeout, parse error) or the blocking budget is exhausted, what confidence should be recorded? (Recommendation: 0.0, since we have zero signal. The error field and budget exhaustion flag already indicate the failure mode.)

4. **Future: `decision` enum migration**: If the additive approach proves stable, should we later migrate from `allowed: bool` to `decision: string` for a cleaner API? (Recommendation: defer. Revisit only if the inversion logic causes real confusion or if first-class `redact` support in request validation becomes needed.)
