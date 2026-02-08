# Confidence Scoring for AI Policy Responses

> **Issue**: [#90 - Add confidence scoring to AI policy responses](https://github.com/maybedont/maybe-dont/issues/90)
> **Status**: Draft

## Post-Merge Re-Review (Completed)

> **Reviewed**: 2026-02-06 after merging main (which includes the `degroff/test_matrix` branch) into this branch. Key findings:
>
> - **Accuracy**: All struct names and field names verified. `AIResponse`, `AIResponseEvaluation`, `aiRuleResult`, `ValidationResult`, `AuditAIRuleResult`, `AuditAIResult`, `AIPolicy`, `AIResponsePolicy` all match. `ActualResult` now has a placeholder `Confidence` field (always 1.0). `CachedResult` has `Confidence`.
> - **CLI proxy**: Fully implemented (`internal/gateway/cli_validation.go`, routes in `server.go`). Section 16 updated.
> - **Skills**: All skill files exist on main (`internal/skills/ai-policy.claude.md`, `test-case.claude.md`, `cel-policy.claude.md` plus variants). Phase 4 targets are valid.
> - **Policy versioning**: No versioning introduced for rule files. Spec proposal for optional `version: "1"` field still applies.
> - **State files**: Schema version "v1" with no upgrade/migration logic. Backward compatibility confirmed — old files deserialize with zero-value defaults.
> - **Default rules**: Still contain "Return ONLY JSON" boilerplate. Phase 2 migration still needed.
> - **Test suite expectations**: `ExpectationsConfig` and `PolicyExpectation` have no `min_confidence`. Phase 3 work still needed.
> - **Assumptions table**: Updated in Section 15.
> - **New finding**: `CLIValidationPolicyResult` struct needs `Confidence` field added (Phase 1).

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

> **Note on Sections 2–9**: These sections were written during the original design phase when self-reported confidence scoring was the plan. After research (see Section 21), self-reported confidence was rejected as unreliable. Logprob-based confidence is a potential future route but is not currently planned. **What was actually implemented** from these sections:
> - **Section 2**: The concept of removing redundant prompt boilerplate was implemented, but the engine-owned format suffix constants described here were NOT added. The engine relies purely on API-level schema enforcement (`GenerateSchema[T]()` with `strict: true`).
> - **Section 3**: The decision logic truth table is accurate and unchanged. The `action: allow` gate pattern documentation is accurate.
> - **Section 9**: The operation context injection (`Tool call:`, `CLI command:`, `Response content:` labels) was implemented. The `const` format suffix strings described here were NOT implemented.
> - **Sections 4–8**: The confidence-related struct fields, config changes, and audit entry changes described here were NOT implemented. They are preserved for reference if logprob-based confidence is pursued in the future.

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

All shipped default rules in `ai_request_rules.yaml` and `ai_response_rules.yaml` will have their response format boilerplate removed. The `%s` placeholder is also removed — the engine appends operation context automatically at runtime with a context-appropriate label (`Tool call:`, `CLI command:`, or `Response content:`). Prompts containing `%s` are rejected at load time. For example:

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
```

The engine appends both the operation context and the response format instruction automatically.

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

#### The `action: allow` gate pattern

Most rules use `action: deny` — describe what's dangerous, deny when found. The engine also supports `action: allow`, which acts as a **required gate**: describe what's acceptable, deny when the AI says the operation doesn't match.

Both action types produce the same truth table (the `allowed` boolean drives the result identically), but the prompt framing is inverted:

- **Deny rule**: "Is this operation dangerous?" — AI says `allowed: false` -> deny
- **Allow rule (gate)**: "Does this operation meet requirements X?" — AI says `allowed: false` -> deny

Gate rules are feasible for **narrow, well-defined acceptance criteria**:
- "Only allow read-only operations" (small surface area, clear boundary)
- "Only allow operations targeting the staging environment" (specific, verifiable condition)

Gate rules are **not recommended for broad acceptance patterns** because they are prone to false positives. The AI must confirm an operation matches a potentially broad set of legitimate uses, and anything it doesn't recognize gets denied. Novel but legitimate operations get caught, and the prompt must exhaustively describe what "good" looks like — which is significantly harder than describing specific threats.

The default rules and skill documentation use `action: deny` exclusively. The gate pattern is not documented in the skills to avoid encouraging a pattern that most users would struggle with.

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

**Updated approach (post-research):** The revised implementation plan (see Implementation Plan) uses logprob-derived confidence rather than self-reported confidence. The sentinel value for "no confidence data available" is **`-1.0`**, which is outside the valid 0.0–1.0 range, making it unambiguous. Any consumer (audit log viewer, test suite, future threshold logic) can immediately distinguish "model was X% confident" from "we have no confidence data." This avoids the original design's problem of overloading `0.0` as both "missing" and a potentially valid (if unlikely) score, and avoids silently pretending full confidence by defaulting to `1.0`.

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

**Previous flow** (`ai_engine.go`):
```go
toolCallStr := fmt.Sprintf("Tool: %s\nArguments: %v", req.Params.Name, req.Params.Arguments)
result, err := e.providerClient.Generate(ctx, AIRequest{
    UserPrompt:     fmt.Sprintf(p.Prompt, toolCallStr),
    ResponseSchema: GenerateSchema[AIResponse](),
})
```

**Current flow (implemented)**:
```go
operationStr := formatOperationForAI(req)

// Engine appends operation context with a context-appropriate label
userPrompt := p.Prompt + "\n\nTool call:\n" + operationStr

result, err := e.providerClient.Generate(policyCtx, AIRequest{
    UserPrompt:     userPrompt,
    ResponseSchema: GenerateSchema[AIResponse](),
})
```

For CLI commands, the label is `CLI command:` instead of `Tool call:`. The response engine (`ai_response_engine.go`) follows the same pattern with `Response content:` as the label and `GenerateSchema[AIResponseEvaluation]()`.

**Note (superseded):** An earlier revision of this section proposed package-level `const` prompt suffix strings (`aiRequestResponseFormatInstruction`, `aiResponseResponseFormatInstruction`) that the engine would append to every prompt. These were NOT implemented. The response format is enforced entirely by `GenerateSchema[T]()` with `strict: true` at the API level — no prompt-level format instruction is needed. See the worklist note: "No engine-owned prompt suffix — rely purely on API-level schema enforcement."

### 10. Test Suite Changes

The policy test suite (on the `degroff/test_matrix` branch) has partial scaffolding for confidence:
- `CachedResult` in `state.go` already has a `Confidence` field
- The runner already writes `result.Actual.Confidence` to cached state
- The custom JSON output format includes `confidence` per result
- The spec defines `overall.min_confidence` in test expectations

**NOTE**: The `ActualResult` struct in `executor.go` already has a `Confidence float64` field, but it is a placeholder — currently hardcoded to `1.0` for all results. The `CachedResult` struct also has `Confidence`. The plumbing is in place; this feature wires real confidence values through.

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

#### `ai-policy-authoring` skill (`.claude/skills/` and `internal/skills/ai-policy.claude.md`)

Major updates:
1. **Remove response format from examples**: All example prompts should end at the analytical content. The response format instruction is injected by the engine.
2. **Remove "Specify the response format" from best practices**: Replace with a note that the engine handles response format automatically. Policy authors should focus on describing what to detect, not how to format the response.
3. **Add confidence guidance**: Explain that the AI will be asked to return a confidence score and how thresholds work.
4. **Update the "Expected AI Response Format" section**: Show the format with the added `confidence` field.
5. **Update Common Mistakes table**: Remove "Vague response format" (no longer relevant). Add "Specifying response format in prompt" as a mild anti-pattern (harmless but wasteful of tokens).

#### `policy-test-case` skill (`internal/skills/test-case.claude.md`)

1. **Add `min_confidence` to field reference**: Document the optional field in expectations.
2. **Add examples**: Show test cases with `min_confidence` assertions.
3. **Add guidance**: "Use `min_confidence` to validate that the AI model is sufficiently confident in its decisions. This is especially useful in model matrix testing to identify models that are unreliable for specific policy types."

### 12. Default Rules Migration

All shipped rules in `internal/config/defaults/ai_request_rules.yaml` and `ai_response_rules.yaml` need updating:

1. **Remove response format boilerplate**: Delete the `Return ONLY JSON...` line from every rule prompt.
2. **Remove example JSON responses from prompts**: The EXAMPLES sections currently include the response JSON (e.g., `-> { "allowed": true, "message": "..." }`). These should be simplified to just the classification (e.g., `-> SAFE: Not a deletion operation` or `-> DANGEROUS: Wildcard deletion detected`).
3. **Remove `%s` placeholder**: The `Tool call: %s` and `Response content: %s` lines are removed — the engine appends operation context automatically at runtime. Prompts containing `%s` are rejected at load time.
4. **No confidence_threshold per rule** (for defaults): The shipped rules should rely on the global threshold. Per-rule overrides are for user customization.

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

**User-authored rules require one migration step.** The `%s` placeholder is rejected at load time — prompts containing `%s` will cause a startup error. This is a **breaking change** for users who wrote custom rules following previous documentation that instructed them to include `%s`.

| Scenario | What happens | User action required |
|----------|-------------|---------------------|
| User rule with `%s` placeholder | **Startup error** — `LoadPolicies` rejects the prompt | **Remove `%s`** from the prompt. The engine now appends operation context automatically. |
| User rule with old response format boilerplate | Harmless — the schema enforces the format regardless. The old instruction is redundant but not rejected. | None (optional cleanup to save tokens) |
| User rule without response format boilerplate | Engine handles format via schema enforcement | None |
| User rule with `confidence_threshold` | Per-rule threshold applied | None (new opt-in feature) |
| AI response missing `confidence` field | Engine defaults to confidence 1.0 (preserve existing behavior) | None |

**Shipped default rules WILL be modified** to remove boilerplate, simplify examples, and remove `%s` placeholders. This is safe because defaults are embedded at compile time — users who have copied and modified them are already diverged and will need to remove `%s` from their copies.

### 15. Assumptions

| Assumption | Basis | Risk if wrong | Status |
|-----------|-------|---------------|--------|
| `ai_response_engine.go` is structurally parallel to `ai_engine.go` | Code exploration confirmed parallel but independent structs | Low — changes need to be applied to both files | **Verified** |
| ~~CLI proxy endpoint is not yet implemented~~ | ~~No handler code found~~ | — | **Invalidated** — CLI proxy is fully implemented (`cli_validation.go`, routes in `server.go`). See updated Section 16. |
| ~~`ActualResult` struct does NOT have `Confidence` field~~ | ~~Explored on `test_matrix` branch~~ | — | **Invalidated** — `ActualResult` has `Confidence float64` (placeholder, always 1.0). Phase 3 step 22 is already done. |
| ~~Skills exist on `test_matrix` branch only~~ | ~~Only `cel-policy-authoring` exists on main~~ | — | **Invalidated** — All skills exist on main in `internal/skills/` (ai-policy.md, test-case.md, cel-policy.md plus variants). Phase 4 targets are valid. |
| State file `schema_version: "v1"` has no upgrade logic | Explored state.go; load() does not validate version | Low — old files deserialize cleanly with zero-value defaults | **Verified** |
| `GenerateSchema` supports `jsonschema` tags for min/max | `invopop/jsonschema` library is used; `jsonschema:"required"` tag already used elsewhere in codebase | Low — verify tag syntax against library docs | **Verified** — `GenerateSchema[T]()` at `ai_engine.go:1242` uses `invopop/jsonschema` reflector |
| `maybedont__generate_audit_report` tool will handle new audit fields | Tool uses AI to analyze audit entries; new fields change the schema it reads | Medium — may need prompt update to reference confidence | Open |
| Blocking budget exhaustion produces a result that needs confidence | Budget exhaustion triggers fail-open with `result: "allow"` | Low — assign confidence 0.0, same as errors | Open |
| Adding confidence does not degrade primary decision quality | Untested assumption — see Section 19 | **High — must be empirically validated before shipping** | Open |
| `CLIValidationPolicyResult` needs `Confidence` field | CLI proxy response struct has no confidence field | Low — straightforward addition | **New** — discovered during post-merge review |

### 16. CLI Proxy Impact

The CLI proxy endpoint is fully implemented (`internal/gateway/cli_validation.go`, routes registered in `server.go` at `/api/v1/cli/validate`). It evaluates both CEL and AI policies against CLI commands via `evaluatePolicies()`.

#### How confidence flows through CLI validation

The CLI proxy uses the same AI engine (`providerClient.Generate()`) and the same `AIResponse` / `AIResponseEvaluation` structs as MCP request validation. Confidence scoring will flow through the AI evaluation path automatically. Specifically:

1. `evaluatePolicies()` in `cli_validation.go` calls the AI provider with `GenerateSchema[AIResponse]()` — the schema change adds `confidence` to AI responses for CLI validation too.
2. The AI engine's prompt suffix injection applies to CLI operations identically to MCP tool calls (the `operationStr` differs but the suffix is the same).
3. CEL evaluation for CLI commands uses `cli_expression` — these are deterministic and get `confidence: 1.0`.

#### `CLIValidationPolicyResult` needs a `Confidence` field

The CLI response struct `CLIValidationPolicyResult` currently has `PolicyName`, `PolicyType`, `Action`, `Message`, and `DurationMs` but no confidence. This needs to be added in Phase 1:

```go
type CLIValidationPolicyResult struct {
    PolicyName string `json:"policy_name"`
    PolicyType string `json:"policy_type"`
    Action     string `json:"action"`
    Message    string `json:"message,omitempty"`
    DurationMs int64  `json:"duration_ms"`
    Confidence float64 `json:"confidence"` // NEW: 1.0 for CEL, AI score for AI
}
```

The REST API response then includes confidence per result:
```json
{
  "results": [
    {
      "policy_name": "general-safety-check",
      "policy_type": "ai",
      "action": "allow",
      "message": "Command is safe to execute",
      "duration_ms": 1200,
      "confidence": 0.95
    }
  ]
}
```

**Spec update needed**: The CLI proxy spec (`cli-proxy-for-ai-agents.md`) should be updated to reference this spec for confidence in its response structure. This is tracked in Phase 5.

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

### Phasing Strategy

The implementation is split into two milestones. Milestone 1 is independently shippable. Milestone 2 is exploratory and requires empirical testing before committing to ship.

**Milestone 1 (Phase 1a–1b): Policy Prompt Cleanup** — Remove redundant response format instructions from all policy prompts. The JSON response format is already enforced by the API-level schema (`GenerateSchema[T]()` with `strict: true`), making the prompt-level instructions a no-op that wastes tokens. This milestone also simplifies the EXAMPLES sections in default rules from JSON-formatted examples to plain-text classification labels (see Section 23). Shippable on its own, and independently valuable because:
- Saves 161-350+ wasted input tokens per validation request across default rules
- Policy authors focus on detection logic, not JSON syntax
- Default rules become shorter and more readable
- Skills and documentation reflect the correct separation of concerns
- Changing the response format in the future requires zero policy edits

**Milestone 2 (Phase 2–2b): Logprob-Based Confidence (Exploratory)** — Investigate extracting confidence from API log probabilities rather than asking the model to self-report a confidence number. Logprobs represent the model's actual internal probability for its classification decision and are the superior signal (see Section 21). This milestone is exploratory — logprob availability with structured outputs needs empirical testing before committing. If logprobs are available, propagate them through audit logs and the policy test matrix for informational purposes only. No thresholding, no decision-making changes.

**Future: Threshold-Based Decision Tuning** — If Milestone 2 demonstrates that logprobs are consistently available and provide meaningful signal variance, the gateway could allow operators to define confidence thresholds that tighten or loosen the boundary between allow and deny for specific use cases. This is deferred until there is empirical evidence that the signal is useful.

### Why Not Self-Reported Confidence?

The original design proposed adding a `confidence` field to the AI response schema and asking the model to self-assess its certainty. After research (detailed in Section 21), this approach was rejected for the following reasons:

1. **Self-reported confidence is unreliable.** Research (Jinks 2025, Kadavath et al. 2022) and industry practice show that LLMs produce poorly calibrated self-assessments. Every major production moderation system (OpenAI Moderation API, Google Perspective API, AWS Comprehend) uses model-internal signals (logprobs, logits) — none ask the model to self-report a number.

2. **Risk of degrading primary classification.** Asking the model to simultaneously classify AND self-assess changes the cognitive task. Research suggests models may hedge on borderline cases or anchor on producing a reasonable-looking confidence score, affecting the quality of the primary allow/deny decision.

3. **Threshold cliff effects.** Two identical requests could receive 0.69 and 0.71 confidence from the same model at temperature 0. With a threshold of 0.7, one blocks and one doesn't — introducing non-determinism that the binary approach doesn't have.

4. **Operational complexity without proven benefit.** Adding threshold configuration, per-rule overrides, and low-confidence actions increases the config surface area significantly, but only delivers value if the underlying confidence signal is meaningful — which is unproven for self-reported scores.

The research notes, tradeoff analysis, and detailed reasoning are preserved in Sections 19, 20, and 21 for future reference.

### Structured Output Support Across Providers

Research confirms that structured JSON output with schema enforcement is now standard across all major LLM providers (as of 2026):

| Provider | Supports `response_format` with JSON schema | `strict` mode |
|----------|---------------------------------------------|---------------|
| OpenAI | Yes | Yes |
| Anthropic | Yes (via `output_config`) | Yes |
| Google Gemini | Yes | Yes |
| Groq | Yes | Yes |
| xAI Grok | Yes | Implicit |
| Azure OpenAI | Yes | Yes |
| AWS Bedrock | Yes (GA Feb 2026) | Yes |
| Together AI | Yes | Yes |
| Fireworks AI | Yes | Yes |
| Mistral AI | Yes | Yes |
| Cohere | Yes | Yes |
| LiteLLM (router) | Pass-through | Depends on downstream |
| Ollama | Partial (non-standard `format` param) | No |

The only notable gap is **Ollama**, which uses a non-standard parameter name (`format` instead of `response_format`). Ollama wouldn't work with the `openai_compatible` provider for structured outputs regardless of prompt instructions.

Given this landscape, removing prompt-level format instructions is safe for all production providers. The `openai_compatible` provider already has a comment acknowledging that not all compatible endpoints support structured outputs — this is an existing limitation, not a new one introduced by this change.

---

### Milestone 1: Policy Prompt Cleanup and Engine Fixes

#### Phase 1a: Remove Redundant Prompt Text and Fix Redact Logic

**Policy prompt cleanup:**
1. Strip `Return ONLY JSON...` boilerplate from all shipped AI request rules (`internal/config/defaults/ai_request_rules.yaml`)
2. Strip `Return ONLY JSON...` boilerplate and multi-line JSON format blocks from all shipped AI response rules (`internal/config/defaults/ai_response_rules.yaml`)
3. Remove conditional field-mapping instructions from response rules (e.g., "If PII is found: Set allowed to true...") — the schema and engine handle field semantics
4. Simplify EXAMPLES in shipped rules — replace JSON response examples (e.g., `→ { "allowed": true, "message": "..." }`) with plain-text classification labels (e.g., `→ SAFE: Not a deletion operation`). See Section 23 for detailed before/after examples including response validation rules.
5. Update `ai-policy-authoring` skill (`internal/skills/ai-policy.claude.md` and variants): remove response format from examples, add note that the response format is enforced by the API-level schema and should not be included in policy prompts. Document that redact rules should specify replacement text in the prompt (see Section 24 for details).
6. Add optional `version: "1"` to shipped rule files (low-cost forward compatibility hook)

**Fix redact rule decision logic (bug fix):**

The current logic in `ai_response_engine.go:256-263` uses `allowed` as the primary decision for all rule types, including redact rules. This means a redact rule can produce a "deny" result when the model returns `allowed: false`, which contradicts the policy author's declared intent. A policy author who chooses `action: redact` is saying "sanitize if needed, always pass through" — if they wanted blocking, they'd use `action: deny`.

7. Update the decision logic in `ai_response_engine.go` so that `allowed` is ignored for redact rules. The only meaningful signal for redact rules is whether `redacted_content` was provided:

**Current logic (buggy):**
```go
if !evaluation.Allowed {
    resultStr = "deny"
} else if p.Action == config.PolicyActionRedact && evaluation.RedactedContent != "" {
    resultStr = "redact"
} else {
    resultStr = "allow"
}
```

**Fixed logic:**
```go
if p.Action == config.PolicyActionRedact {
    if evaluation.RedactedContent != "" {
        resultStr = "redact"    // Model provided sanitized content, use it
    } else {
        resultStr = "allow"     // Nothing to redact, pass through original
    }
} else if !evaluation.Allowed {
    resultStr = "deny"
} else {
    resultStr = "allow"
}
```

8. Add/update tests for the redact decision logic covering all cases in the truth table below.

**Response engine decision truth table (after fix):**

| Rule Action | `allowed` | `redacted_content` | Result | Why |
|------------|-----------|-------------------|--------|-----|
| redact | true | present | redact | Model provided sanitized content |
| redact | true | empty | allow | Nothing to redact, pass through original |
| redact | false | present | redact | Model provided sanitized content (`allowed` is irrelevant for redact rules) |
| redact | false | empty | allow | Nothing to redact; redact rules never deny |
| deny | false | n/a | deny | Content flagged as dangerous |
| deny | true | n/a | allow | Content is safe |

Key principle: **redact rules never produce "deny."** The `allowed` field is only meaningful for deny (and allow) rules. For redact rules, the relevant signal is `redacted_content` — did the model provide sanitized content or not?

If a policy author wants "redact if possible, block if too severe to redact," that requires two separate rules: a `redact` rule for sanitizable content and a `deny` rule for content that can't be sanitized.

#### Phase 1b: Verification
9. Run `make test` — all existing unit tests must pass
10. Run `make lint` — no new lint issues
11. Run the policy test suite against the default rules to verify no decision regressions from the prompt changes
12. Sample the policy test matrix across at least 2 models to confirm classification behavior is unchanged

#### Phase 1c: Skip Response Validation for Empty Responses

Currently, response validation runs unconditionally after every tool call, even when the response has no content. The AI engine formats the response via `formatResponseForAI()` which produces just `"IsError: false\n"` for empty responses, then sends this to the AI provider for every enabled response policy. This wastes API calls and adds unnecessary latency.

13. Add a check in `gateway.go` before the response validation chain to skip validation when the response has no content:

```go
if g.responseValidationChain != nil && result != nil && len(result.Content) > 0 {
    // Run response validation
}
```

14. Log at DEBUG level when response validation is skipped due to empty content
15. Add a test verifying that response validation is skipped for empty responses

---

### Milestone 2: Logprob-Based Confidence (Exploratory)

#### Phase 2: Logprob Extraction
11. **Empirical test**: Determine whether OpenAI returns meaningful logprobs when `response_format` with `strict: true` is used. If constrained decoding eliminates alternatives at each token position, logprobs may always be near 0.0 (100% for the chosen token), providing no useful signal. This test gates the rest of Milestone 2.
12. Extend the `AIProviderResponse` struct with an optional `Logprob *float64` field
13. Update the OpenAI provider to request logprobs (`logprobs: true`) and extract the logprob for the `allowed` token (`true`/`false`)
14. Convert the raw logprob to a 0.0–1.0 probability via `exp(logprob)`
15. For providers that do not support logprobs (Anthropic, some OpenAI-compatible), or when logprobs are unavailable, set the value to `-1.0` (sentinel: "no data available"). `-1.0` is outside the valid 0.0–1.0 range, making it unambiguous — any consumer can immediately distinguish "confident" from "no data."
16. Update the Anthropic provider and OpenAI-compatible provider to return `-1.0` when logprobs are not available
17. CEL rule results always set confidence to `1.0` (deterministic match = full confidence)

#### Phase 2b: Audit and Observability Integration
18. Add `Confidence float64` field to `AuditAIRuleResult` — raw logprob-derived score, or `-1.0` for no data
19. Add `Confidence float64` field to `ValidationResult` — `1.0` for CEL, logprob-derived for AI, `-1.0` for no data
20. Add `Confidence float64` field to `CLIValidationPolicyResult` in `cli_validation.go`
21. Add `Confidence float64` field to `aiRuleResult` internal struct (goroutine channel)
22. Wire confidence through the AI engine evaluation flow (`ai_engine.go` and `ai_response_engine.go`)
23. Log confidence at DEBUG level when available (never at INFO — this fires per rule per request)
24. Wire real confidence values through the policy test suite runner (replace the placeholder `1.0` in `ActualResult.Confidence`)
25. Update test output formatters to display confidence when available (show `-1.0` as "N/A" or "no data")
26. Document in test suite output that confidence values are **informational only** and not used for decision-making

**Important**: No threshold logic, no `confidence_threshold` config field, no `low_confidence_action`, no per-rule overrides. Confidence is purely observational in this milestone.

---

### Future: Threshold-Based Decision Tuning (Not Scheduled)

If Milestone 2 demonstrates that logprobs are consistently available across providers and produce meaningful score variance (not clustered at 0.95-1.0), the following could be considered:

- Configurable `confidence_threshold` at global and per-rule level
- `low_confidence_action` setting (e.g., `audit_only` to downgrade low-confidence denials)
- Directional threshold application (only softens firing/blocking decisions, never creates new blocks)
- `min_confidence` expectations in policy test suite for threshold validation across models

The design for these features is preserved in Sections 3, 4, 6, and 10 of this spec for reference. Implementation should only proceed after empirical evidence shows the confidence signal is useful for decision-making — not just for observability.

---

### Documentation and Cleanup (After Milestone 1)

27. Update `policy-test-suite/README.md` to reference this spec for confidence-related design
28. Update `runtime-action-interception-architecture.md` to reference this spec
29. Update `cli-proxy-for-ai-agents.md` to reference this spec
30. Update `policy-test-case` skill (`internal/skills/test-case.claude.md`) if test suite confidence display is added
31. Create documentation update checklist for `maybedont.ai/docs` (configuration reference, AI rule authoring guide)

## 21. Research Findings: Confidence Scoring Cost/Benefit Analysis

This section captures research conducted to evaluate the viability and tradeoffs of self-reported confidence scoring versus the existing binary allow/deny approach.

### Speed Impact

**Negligible.** Adding a `confidence` float field to the JSON schema sent via OpenAI's `response_format` or Anthropic's `output_config` adds approximately 1 extra output token per response. The dominant latency factors are model inference time, network round-trip, and total token count — not one additional JSON field.

The prompt suffix proposed in Section 9 (~80 tokens of confidence guidance per rule) adds up across rules. With 7 enabled rules evaluated in parallel, that's ~560 extra input tokens per request. Input tokens don't affect per-rule latency (rules are evaluated concurrently), but they do marginally increase cost per validation. This is not a blocking concern.

### Accuracy Impact of Self-Reported Confidence

Research suggests self-reported confidence is problematic for decision-making:

**1. Self-reported confidence is unreliable.** Every major production moderation system (OpenAI Moderation API, Google Perspective API, AWS Comprehend) uses model-internal signals (logprobs, logits) for confidence scores — not self-reported numbers. Research from 2025 (Jinks, "Estimating LLM classification confidence with log probabilities") explicitly calls self-reported confidence "highly unreliable" with "the lowest accuracy and highest standard deviation" compared to logprob-based approaches. Models tend to generate a plausible-sounding number rather than meaningfully self-assess.

**2. Confidence may degrade the primary classification.** This aligns with Section 19's concerns. Asking the model to simultaneously classify AND self-assess introduces a different cognitive task. Research shows models may:
- **Hedge** on borderline cases — returning `allowed: true` with low confidence where they'd have committed to `allowed: false` without the confidence framing
- **Anchor** on producing a reasonable-looking confidence number and let it influence the primary decision
- Produce confidence distributions with insufficient variance (clustering at 0.9-1.0), providing zero useful signal for thresholding

**3. Threshold placement matters and 50% is suboptimal for security.** Standard ML practice (see Evidently AI, Google ML Crash Course on classification thresholds) states that when false negatives (allowing a dangerous operation) cost more than false positives (blocking a safe one), the threshold should shift toward the expensive error. For security, that means a *lower* deny threshold (e.g., >0.3), not a midpoint at 0.5. However, this entire optimization is moot if the underlying scores aren't calibrated.

**4. Non-determinism at threshold boundaries.** Two identical requests may receive confidence 0.69 and 0.71 from the same model at temperature 0. If the threshold is 0.7, one blocks and one doesn't. The binary approach has no such non-determinism at the decision boundary — the model either says allow or deny.

### When Confidence Scoring Is Valuable

Despite the above concerns, confidence scores provide legitimate value for **audit enrichment and observability**. Seeing "denied with 0.45 confidence" vs "denied with 0.98 confidence" in audit logs is genuinely useful for operators tuning rules. This is an observability use case, not a decision-making use case. The spec's `low_confidence_action: audit_only` default (Section 4) correctly acknowledges this.

### Logprobs as an Alternative to Self-Reported Confidence

Instead of asking the model to self-report a confidence number, the gateway could extract confidence from **log probabilities** (logprobs) returned by the API. Logprobs represent the model's actual internal probability for each generated token — they are the ground truth for how confident the model was, rather than a post-hoc self-assessment.

**How it would work:**

1. The gateway already asks the model to return `"allowed": true` or `"allowed": false` via structured output
2. Most AI providers can return logprobs alongside the response — the probability the model assigned to each token it generated
3. The logprob of the `true` or `false` token directly after `"allowed":` represents the model's actual confidence in its classification decision
4. Convert the logprob (which is `log(p)`, ranging from 0.0 for 100% confidence to negative infinity for 0%) into a 0.0-1.0 probability via `exp(logprob)`

**Example:** If the model returns `"allowed": false` with a logprob of `-0.105` for the `false` token, the actual confidence is `exp(-0.105) ≈ 0.90` — the model was 90% confident in its deny decision.

**Advantages over self-reported confidence:**
- Reflects the model's actual internal state, not a hallucinated number
- Does not change the classification prompt at all — no risk of degrading the primary decision
- Zero additional prompt tokens (no confidence guidance suffix needed)
- Research consistently shows logprobs are "by far the most accurate technique" for estimating LLM confidence (Jinks 2025)

**Current limitations:**
- **OpenAI**: Supports logprobs via the `logprobs: true` parameter on chat completions. However, logprobs may not be available when `response_format` with `strict: true` is used — this needs empirical testing.
- **Anthropic**: Does not currently expose logprobs in the API.
- **OpenAI-compatible providers**: Support varies by provider.

**Recommendation:** Logprobs are the superior confidence signal when available. The gateway's provider abstraction (`AIProviderClient` interface) could be extended to optionally return logprobs alongside the parsed response. This would require:
- Adding an optional `Logprobs` field to `AIProviderResponse`
- Each provider implementation extracting logprobs if available
- The engine converting the `allowed` token's logprob to a 0.0-1.0 confidence score
- Falling back to self-reported confidence (or 1.0) when logprobs are unavailable

This is a cleaner long-term approach than self-reported confidence, but provider support gaps mean it cannot be the sole mechanism today. Worth tracking as provider APIs evolve.

### Recommendations

| Topic | Recommendation |
|-------|---------------|
| **Milestone A: Prompt centralization (Phases 1-2)** | **Ship it.** Independently valuable, low risk, correct separation of concerns. See Section 22 for why. |
| **Confidence scoring for decision-making (thresholding)** | **Do not ship without empirical validation.** Self-reported confidence is unreliable per research. The Section 19 validation plan must complete first. |
| **Confidence scoring for audit enrichment** | **Reasonable** — but only if it does not degrade primary classification quality. Validate with the test suite model matrix first. Ship as audit-only data initially. |
| **Threshold of ≤0.5 = allow, >0.5 = deny** | **Do not use.** If thresholding is adopted, the threshold must be tuned empirically per model, not set at an arbitrary midpoint. Security cost asymmetry favors a lower deny threshold. |
| **Logprobs as alternative** | **Track for the future.** Superior signal when available. Extend the provider interface to optionally return logprobs. Use as the primary confidence source when supported, fall back to self-reported or 1.0 when not. |
| **Implementation sequence** | Ship Milestone A first. Defer Milestone B until empirical validation is complete. If confidence distributions cluster tightly or degrade decisions, the prompt centralization alone is still valuable. |

### Research Sources

- Kadavath et al. (2022) — "Language Models (Mostly) Know What They Know" ([arXiv:2207.05221](https://arxiv.org/abs/2207.05221))
- Jinks (2025) — "Estimating LLM classification confidence with log probabilities" ([ericjinks.com](https://ericjinks.com/blog/2025/logprobs/))
- STED Framework — "Evaluating LLM Structured Output Reliability" ([arXiv:2512.23712](https://arxiv.org/html/2512.23712))
- Amazon Research — "Label with Confidence: Effective Confidence Calibration and Ensembles in LLM-Powered Classification" ([amazon.science](https://www.amazon.science/publications/label-with-confidence-effective-confidence-calibration-and-ensembles-in-llm-powered-classification))
- OpenAI — "Using logprobs" ([OpenAI Cookbook](https://cookbook.openai.com/examples/using_logprobs))
- Google — "Thresholds and the confusion matrix" ([ML Crash Course](https://developers.google.com/machine-learning/crash-course/classification/thresholding))
- Google Research — "Introducing ASPIRE for selective prediction in LLMs" ([research.google](https://research.google/blog/introducing-aspire-for-selective-prediction-in-llms/))

## 22. Why the Prompt Boilerplate Is Redundant (Detailed Explanation)

This section explains in detail why the `Return ONLY JSON...` instruction in every rule prompt is unnecessary, to support the case for Milestone A.

### The Schema Is Sent Separately from the Prompt

The gateway's AI provider implementation sends the expected response format as a **machine-enforced JSON schema** alongside the prompt — not embedded within it. This is a distinct API parameter that the AI provider uses to constrain the model's output at the decoding level.

**What happens in `ai_engine.go`:**

```go
userPrompt := p.Prompt + "\n\nTool call:\n" + operationStr  // Engine appends context
result, err := e.providerClient.Generate(policyCtx, AIRequest{
    UserPrompt:     userPrompt,                              // The rule prompt + context
    ResponseSchema: GenerateSchema[AIResponse](),            // Schema sent separately
    Parameters:     e.cfg.Validation.AI.Parameters,
})
```

These are two independent inputs to the API call:
1. `UserPrompt` — the rule's analytical prompt (what to detect)
2. `ResponseSchema` — the Go struct reflected into a JSON schema (how to respond)

**What the OpenAI provider sends (`ai_provider_openai.go:177`):**

```go
body["response_format"] = map[string]any{
    "type": "json_schema",
    "json_schema": map[string]any{
        "name":   "response",
        "schema": req.ResponseSchema,
        "strict": true,       // Provider enforces exact schema compliance
    },
}
```

The `strict: true` flag means OpenAI's API will **reject any response that doesn't match the schema**. The model is physically constrained to produce `{"allowed": bool, "message": string}`. It cannot deviate from this format regardless of what the prompt says.

**The Anthropic provider does the same** via `output_config.format` with `json_schema`.

### What This Means for the Prompt Instruction

Every default rule currently ends with a line like:

```
Return ONLY JSON in this exact format: { "allowed": true/false, "message": "your message" }
```

This instruction is telling the model to do something it is **already forced to do** by the schema constraint. It is redundant — the model would return `{"allowed": ..., "message": ...}` even if the prompt said nothing about format, because the API-level schema enforcement leaves no alternative.

Removing this line from each rule saves ~23-50 tokens per rule (161-350 tokens across 7 default request rules) with zero behavioral change.

### Why No Engine Suffix Was Implemented

An earlier revision proposed engine-owned prompt suffix constants (see Section 9). After implementation, the decision was to rely purely on API-level schema enforcement (`GenerateSchema[T]()` with `strict: true`) without any prompt-level format instruction. The schema is sufficient because all production providers support structured outputs (see the provider support table in the Implementation Plan). For the `openai_compatible` provider, the existing comment already acknowledges that not all compatible endpoints support structured outputs — this is a pre-existing limitation, not one introduced by removing prompt boilerplate.

## 23. Migrating from JSON-Formatted Examples to Plain-Text Classification

This section explains the rationale and mechanics of simplifying the EXAMPLES sections in default rules.

### What Changes

The examples in each rule prompt are classification demonstrations — they teach the model the decision boundary for the rule. Currently, each example encodes both the classification AND the response format:

**Before (current):**
```
EXAMPLES:
- kubectl get po → NOT deletion → { "allowed": true, "message": "Not a deletion operation" }
- kubectl delete po --all → IS dangerous → { "allowed": false, "message": "Wildcard deletion blocked" }
- DELETE FROM users → IS dangerous (no WHERE) → { "allowed": false, "message": "Mass database deletion blocked" }
```

**After (proposed):**
```
EXAMPLES:
- kubectl get po → SAFE: Not a deletion operation
- kubectl delete po --all → DANGEROUS: Wildcard deletion of all pods
- DELETE FROM users → DANGEROUS: Mass database deletion with no WHERE clause
```

### What Does NOT Change

The model's response format is unchanged. The model still returns:
```json
{"allowed": true, "message": "Not a deletion operation"}
```

This is enforced by the JSON schema sent via the API's structured output feature (see Section 22). The examples are not teaching the model how to format its response — the schema does that. The examples are teaching the model **what to classify as safe vs dangerous**.

### Why This Is Better

**1. Separates classification from formatting.** The examples should teach the decision boundary, not JSON syntax. When examples include `{ "allowed": true, "message": "..." }`, they conflate two concerns: "what should be blocked?" and "how should I format the answer?" The model already knows the format from the schema.

**2. Saves tokens.** Each JSON example adds ~15-20 tokens of format overhead (`{ "allowed": true/false, "message": "..." }`). With 3-8 examples per rule and 7 rules, this is 300-1100 wasted tokens per request across all rules.

**3. More natural classification examples.** LLMs are better at few-shot classification when examples are written as natural-language labels rather than structured output. The pattern `input → LABEL: reasoning` is the standard few-shot classification format used in prompting research. Asking the model to map examples to JSON structure is an unnecessary cognitive step when the structure is already enforced externally.

**4. Reduces risk of conflicting instructions.** If the engine suffix describes the response format and the examples also embed a response format, there are two sources of truth. If they ever diverge (e.g., when adding `confidence`), the model receives conflicting signals. Plain-text examples eliminate this risk entirely.

### Response Validation Rules: Same Principle, More Fields

The response validation rules use a larger struct (`AIResponseEvaluation`) that includes `redacted_content` alongside `allowed` and `message`. The same schema enforcement applies — at `ai_response_engine.go:223`, the engine sends `GenerateSchema[AIResponseEvaluation]()` which produces a JSON schema with all three fields. The provider enforces this schema identically to the request validation case.

This means the multi-line JSON format block in response rules like `detect-pii-in-response` is also fully redundant:

**Before (current `detect-pii-in-response`):**
```yaml
prompt: |-
  ANALYZE: Does this response contain PII (personally identifiable information)?

  Look for: email addresses, phone numbers, social security numbers, physical addresses,
  credit card numbers, passport numbers, driver's license numbers, or other identifying information.

  Response content: %s

  If PII is found:
  - Set "allowed" to true (we'll redact, not block)
  - Provide the response with PII replaced by [PII_REDACTED]
  - Explain what was redacted in the message

  If no PII is found:
  - Set "allowed" to true
  - Leave "redacted_content" empty or null
  - Set message to "No PII detected"

  Return ONLY JSON in this exact format:
  {
    "allowed": true/false,
    "message": "explanation of what was found/redacted",
    "redacted_content": "sanitized version with [PII_REDACTED] replacing sensitive data (only if PII found)"
  }
```

**After (proposed):**
```yaml
prompt: |-
  ANALYZE: Does this response contain PII (personally identifiable information)?

  Look for: email addresses, phone numbers, social security numbers, physical addresses,
  credit card numbers, passport numbers, driver's license numbers, or other identifying information.

  If PII is found, provide a redacted version with sensitive data replaced by [PII_REDACTED].

  EXAMPLES:
  - "Contact John at john@example.com" → PII DETECTED: Email address found, redact to "[PII_REDACTED]"
  - "The server returned 200 OK" → SAFE: No PII detected
  - "User SSN: 123-45-6789" → PII DETECTED: Social security number found
```

**What changed:**
1. The `Return ONLY JSON...` block with the three-field JSON template is removed entirely. The engine-owned suffix (Section 9) and the `GenerateSchema[AIResponseEvaluation]()` schema handle the response format.
2. The conditional instructions ("If PII is found: Set allowed to true...") are removed. These were teaching the model how to map its classification to JSON field values — that mapping is now handled by the engine suffix which explains the semantics of each field.
3. Simple classification examples are added instead, following the same `input → LABEL: reasoning` pattern as request rules.

**What the model still returns** (enforced by schema):
```json
{
  "allowed": true,
  "message": "Email address found and redacted",
  "redacted_content": "Contact John at [PII_REDACTED]"
}
```

The response format instruction that the engine appends for response validation (Section 9) covers the `redacted_content` field:
```
- "redacted_content": If sensitive content was found that should be sanitized, provide the redacted version. Otherwise leave empty.
```

This is the single place where the `redacted_content` field's semantics are explained — not duplicated across every response rule.

### Not Changing the AI's Decision Logic

To be clear: the model still returns `{"allowed": true/false, "message": "..."}` (or `{"allowed": ..., "message": "...", "redacted_content": "..."}` for response rules) in JSON. The migration only changes the *examples within the prompt* from showing JSON to showing plain-text classification labels. The actual response is structured JSON, enforced by the schema — not by the examples.

## 24. Replacement Text in Redact Policies

### Who Specifies Replacement Text?

The replacement text for redacted content is specified **in the policy prompt**, not in the engine. The engine has no default replacement text — it uses whatever the model returns in `redacted_content` verbatim.

Current default rules demonstrate this:
- `detect-pii-in-response`: "Provide the response with PII replaced by [PII_REDACTED]"
- `redact-internal-paths`: "Provide redacted content with sensitive parts replaced by [PATH_REDACTED] or [HOST_REDACTED]"

### What Happens When Replacement Text Is Omitted?

If a policy prompt says "redact sensitive content" without specifying what to replace it with, the behavior is **non-deterministic**. The AI model will improvise — likely choosing something like `[REDACTED]`, `***`, or `<removed>`, but there is no guarantee of consistency across calls or models. This is acceptable for a quick start but not recommended for production use.

### Empty `redacted_content` Semantics

The engine treats `redacted_content` as follows:
- **Non-empty string** → content was redacted, use this version (result: "redact")
- **Empty string (`""`)** → nothing to redact, pass through the original response unchanged (result: "allow")

This means a fully-redacted response should NOT return an empty string. If the entire response is sensitive, the model should return something like `"[FULLY_REDACTED]"` or `"[PII_REDACTED]"` — not `""`. The policy prompt's replacement text instructions ensure this: if the prompt says "replace with [PII_REDACTED]", a fully-redacted response contains `"[PII_REDACTED]"`, which is non-empty.

### Best Practices (for skill documentation)

1. **Always specify replacement text** in redact policy prompts. Example: "Replace sensitive content with [PII_REDACTED]"
2. **Use distinct placeholder strings** for different types of redaction so operators can identify what was redacted in audit logs (e.g., `[PII_REDACTED]`, `[PATH_REDACTED]`, `[CREDENTIAL_REDACTED]`)
3. **Do not rely on the AI to choose replacement text** — different models and temperatures will produce different placeholders, making audit logs inconsistent

## Open Questions

1. **Default threshold value**: The issue suggests 0.7. Should we run the test suite across models before committing to a default, or ship 0.7 and adjust based on feedback? (Recommendation: ship 0.7 with clear documentation that it should be tuned per-model. Phase 0 validation will inform whether this is reasonable.)

2. **Response format injection point**: Should the response format instruction be appended to the user prompt (as proposed) or provided as a system prompt? System prompts are more semantically appropriate for meta-instructions, but not all providers handle them identically. (Recommendation: user prompt suffix, since it's simpler and the gateway already does not use system prompts for policy evaluation.) This will be resolved in Milestone A (Phase 1).

3. **Confidence on error and budget exhaustion**: When the AI call fails (timeout, parse error) or the blocking budget is exhausted, what confidence should be recorded? (Recommendation: 0.0, since we have zero signal. The error field and budget exhaustion flag already indicate the failure mode.)

4. **Future: `decision` enum migration**: If the additive approach proves stable, should we later migrate from `allowed: bool` to `decision: string` for a cleaner API? (Recommendation: defer. Revisit only if the inversion logic causes real confusion or if first-class `redact` support in request validation becomes needed.)

5. **Logprobs feasibility with structured outputs**: Does OpenAI return logprobs when `response_format` with `strict: true` is used? This needs empirical testing before committing to a logprob-based confidence strategy. If logprobs are not available with structured outputs, self-reported confidence may be the only option for providers that support structured outputs but not logprobs alongside them.

6. **Confidence as audit-only vs decision-making**: Should confidence scoring ship initially as audit-only enrichment (always logged, never used for thresholding), with thresholding gated behind the empirical validation in Section 19? This would decouple the observability value from the decision-quality risk.
