# Confidence Scoring Spec — Phase 1 Implementation Worklist

> **Spec**: `docs/specs/confidence-scoring.md`
> **Branch**: `degroff/confidence_scoring`
> **Worktree**: `.worktrees/degroff/confidence_scoring`

## Phase 1a: Remove Redundant Prompt Text and Fix Redact Logic

### Policy prompt cleanup

- [x] **1. AI request rules** (`internal/config/defaults/ai_request_rules.yaml`)
  - Remove `Return ONLY JSON in this exact format: { "allowed": true/false, "message": "your message" }` from all 7 rules
  - Replace JSON-formatted EXAMPLES with plain-text classification labels
  - Example: `→ { "allowed": true, "message": "Not a deletion operation" }` becomes `→ SAFE: Not a deletion operation`
  - See spec Section 23 for the full pattern

- [x] **2. AI response rules** (`internal/config/defaults/ai_response_rules.yaml`)
  - Remove `Return ONLY JSON...` blocks (one-liner and multi-line variants) from all 4 rules
  - Remove conditional field-mapping instructions (e.g., "If PII is found: Set allowed to true...")
  - Replace with plain-text classification examples
  - Ensure redact rules specify replacement text in prompt (e.g., `[PII_REDACTED]`)
  - See spec Section 23 "Response Validation Rules" subsection for the `detect-pii-in-response` before/after

- [x] **3. AI policy authoring skill** (`internal/skills/ai-policy.md` and any variants in `.claude/skills/`)
  - Remove response format from example prompts
  - Add note: response format is enforced by the API-level schema, should not be in policy prompts
  - Add note: redact rules should specify replacement text (see spec Section 24)
  - Remove "Specify the response format" from best practices if present

### Fix redact rule decision logic (bug fix)

- [x] **4. Fix decision logic** (`internal/gateway/ai_response_engine.go` ~line 256-263)
  - Current (buggy):
    ```go
    if !evaluation.Allowed {
        resultStr = "deny"
    } else if p.Action == config.PolicyActionRedact && evaluation.RedactedContent != "" {
        resultStr = "redact"
    } else {
        resultStr = "allow"
    }
    ```
  - Fixed:
    ```go
    if p.Action == config.PolicyActionRedact {
        if evaluation.RedactedContent != "" {
            resultStr = "redact"
        } else {
            resultStr = "allow"
        }
    } else if !evaluation.Allowed {
        resultStr = "deny"
    } else {
        resultStr = "allow"
    }
    ```

- [x] **5. Add/update tests for redact logic** (`internal/gateway/ai_response_engine_test.go`)
  - Table-driven tests covering all 6 rows of the truth table:
    | Rule Action | `allowed` | `redacted_content` | Expected Result |
    |---|---|---|---|
    | redact | true | present | redact |
    | redact | true | empty | allow |
    | redact | false | present | redact |
    | redact | false | empty | allow |
    | deny | false | n/a | deny |
    | deny | true | n/a | allow |
  - Key regression tests: redact+allowed:false+content → redact (NOT deny), redact+allowed:false+empty → allow (NOT deny)

## Phase 1b: Verification

- [x] **6. `make test`** — all existing unit tests pass
- [x] **7. `make lint`** — no new lint issues
- [ ] **8. Run policy test suite** against modified default rules (if test suite is available on this branch)
- [ ] **9. Sample test matrix** across 2+ models (if test matrix is available; otherwise note as manual follow-up)

## Phase 1c: Skip Response Validation for Empty Responses

- [x] **10. Add empty response guard** (`internal/gateway/gateway.go` ~line 559)
  - Change:
    ```go
    if g.responseValidationChain != nil {
    ```
  - To:
    ```go
    if g.responseValidationChain != nil && result != nil && len(result.Content) > 0 {
    ```
  - Add DEBUG log when response validation is skipped due to empty content

- [x] **11. Add test for empty response skip** (`internal/gateway/response_validation_test.go`)
  - Verify response validation is skipped when `result.Content` is empty
  - Verify response validation still runs for non-empty responses

## Notes

- Skip `version: "1"` in rule files (deferred per discussion)
- No engine-owned prompt suffix — rely purely on API-level schema enforcement
- Response format is enforced by `GenerateSchema[T]()` with `strict: true` sent via `ResponseSchema` parameter
- Replacement text for redact rules is a policy prompt concern, not engine logic (see spec Section 24)
