# Provider-Agnostic AI Validation

## Status
**Draft** - Pending Review


## Overview

Decouple AI validation from the OpenAI SDK by introducing a provider-agnostic client layer. The gateway should be able to validate requests/responses using multiple AI providers (OpenAI, Anthropic, and others) by configuring the endpoint URL and model, while keeping existing OpenAI-based behavior working.

This spec also covers: configuration changes, adapter design, REST vs SDK tradeoffs, backward compatibility, required tests, prompt reliability evaluation options, and audit log updates.

## Goals

1. Support AI validation with multiple providers (OpenAI, Anthropic, and others).
2. Allow selecting the model per provider (e.g., "opus 4.5", "chatgpt 5.2", "5.2 nano", or any vendor model ID).
3. Allow "URL-only" provider switching where the provider is OpenAI-compatible.
4. Provide a clear comparison of REST-only vs provider SDK options.
5. Maintain backward compatibility with the current OpenAI configuration.
6. Define test coverage to avoid functional regressions.
7. Identify common API concepts and their equivalents across providers for consistent prompts/results.
8. Identify options for evaluating prompt reliability (including future CLI-based tool calls).
9. Decide whether audit logs should include provider/model/endpoint metadata.

## Non-Goals

1. Implementing provider-specific advanced features (e.g., tool use schemas beyond current needs).
2. Building a full prompt-evaluation framework (we will define hooks and test strategies).
3. Supporting multiple AI providers simultaneously in a single request (one provider per validation run).
4. Per-rule model or provider overrides (all rules use the same configured provider/model).

## Current State

- AI validation uses the OpenAI SDK (`openai-go`) directly in:
  - `internal/gateway/ai_engine.go`
  - `internal/gateway/ai_response_engine.go`
  - `internal/gateway/audit_report_tool.go`
  - `internal/gateway/ai_client.go`
- Config is centralized under:
  ```yaml
  validation:
    ai:
      endpoint: "https://api.openai.com/v1/chat/completions"
      model: "gpt-4o-mini"
      api_key: "${OPENAI_API_KEY}"
  ```
- The current prompt strategy depends on JSON-schema formatted responses using the OpenAI response format feature.

## Proposed Design

### 1) Provider-Agnostic Client Interface

Introduce a provider-agnostic interface used by request/response validation and audit reporting.
This must not collide with the existing `AIClient` interface in `internal/gateway/ai_client.go`.
Proposed name: `AIProviderClient`.

```
type AIProviderClient interface {
    Generate(ctx context.Context, req AIRequest) (AICompletionResult, error)
}
```

Where `AIRequest` is a vendor-neutral structure:
- `Model string`
- `SystemPrompt string` (optional)
- `UserPrompt string`
- `ResponseSchema JSONSchema` (optional, for structured JSON responses)
- `Temperature *float64` (optional)
- `MaxTokens *int` (optional)
- `Metadata map[string]string` (optional; for audit/correlation)

To avoid colliding with the existing `AIResponse` (the validation schema), the provider response wrapper should be named
`AICompletionResult` (or similar).

`AICompletionResult` includes:
- `RawText string`
- `ParsedJSON json.RawMessage` (optional)
- `ProviderRequestID string` (optional, if provided)

### Type Naming and Collisions

To avoid confusion with existing types:
- Keep `AIResponse` as the validation schema (used in AI rule parsing).
- Name the provider response wrapper `AICompletionResult`.
- Name the new provider interface `AIProviderClient`.
- Keep the existing OpenAI SDK wrapper interface (`AIClient`) for now, but use it only inside the OpenAI adapter.

### 2) Provider Adapters

Implement adapters that translate `AIRequest` into provider-specific APIs and back:

- **OpenAI adapter**:
  - Uses OpenAI-compatible chat completions or the existing SDK.
  - Default if `validation.ai.provider` is unset.

- **Anthropic adapter**:
  - Uses the vendor "messages" style API via REST (SDK optional).

- **OpenAI-compatible REST adapter**:
  - For any provider that supports OpenAI-compatible chat completions.
  - Enables "URL-only" switching by changing `endpoint`.

Adapters should be isolated behind the same interface so validation engines do not depend on any SDK types.

### 3) Configuration Changes

Add optional fields under `validation.ai` while keeping current fields working:

```yaml
validation:
  ai:
    provider: "openai"        # openai | anthropic | openai_compatible | custom (optional)
    endpoint: "https://api.openai.com/v1/chat/completions"
    model: "gpt-4o-mini"
    api_key: "${OPENAI_API_KEY}"
    headers:                  # optional additional headers
      X-Custom-Header: "value"
    auth:
      header: "Authorization" # optional override
      prefix: "Bearer "       # optional override
```

Notes:
- `provider` is optional; default is `openai`.
- If `provider` is `openai_compatible`, only `endpoint`, `model`, and `api_key` are required (plus optional headers).
- For `anthropic`, the adapter can apply default header conventions if only `api_key` is provided; otherwise honor `headers`.
- Keep `endpoint`, `model`, and `api_key` as-is for backward compatibility.
- Preserve environment-variable override behavior (e.g., `MAYBE_DONT_VALIDATION_AI_*`).

### 4) Common API Concepts and Equivalents

We will standardize on a minimal set of concepts that map well across providers:

| Concept | Meaning | OpenAI-style | Anthropic-style | OpenAI-compatible |
|---------|---------|--------------|-----------------|------------------|
| model | Model identifier | `model` | `model` | `model` |
| system prompt | High-level instruction | system message | system field/role | system message |
| user prompt | Input content | user message | user content | user message |
| max tokens | Output limit | `max_tokens` | `max_tokens` | `max_tokens` |
| temperature | Randomness | `temperature` | `temperature` | `temperature` |
| JSON output | Structured response | JSON schema / response format | prompt + schema validation | JSON schema / response format |

For providers lacking native JSON schema support, we will:
1. Embed schema instructions in the prompt.
2. Parse the response as JSON.
3. Validate against the same schema locally (fail with `parse_error` if invalid).

### 5) REST vs SDK: Pros/Cons

**REST-only**
- Pros:
  - Simple dependency graph
  - Uniform behavior across providers
  - Easier to support OpenAI-compatible endpoints
  - Less vendor lock-in
- Cons:
  - Reimplement auth headers, retries, and streaming behavior
  - Less access to provider-specific features or best practices

**SDK per provider**
- Pros:
  - Provider-supported auth, error formats, and advanced features
  - Easier upgrades for new provider features
- Cons:
  - Multiple dependencies, larger binary
  - Harder to unify error handling and telemetry
  - Vendor SDKs can be opinionated or inconsistent

**Recommendation**:
Start with REST adapters for all providers, and optionally keep the OpenAI SDK as a compatibility path. Add SDK support only when REST is insufficient.

### 6) Backward Compatibility

Compatibility strategy:
- If `validation.ai.provider` is unset:
  - Use the OpenAI adapter.
  - Interpret `endpoint`, `model`, and `api_key` exactly as today.
- Keep existing config keys and env vars intact.
- Preserve audit log schema unless an opt-in extension is enabled (see next section).

### 7) Audit Log Updates

Decision needed: Should audit logs include the provider/model/endpoint metadata?

Option A (recommended):
- Add optional fields under `request_validation.ai` and `response_validation.ai`:
  - `provider`
  - `model`
  - `endpoint_host` (host only; no path/query)
  - `request_id` (if provider returns one)

Pros:
- Easier debugging and validation traceability.
Cons:
- Schema update required; potential storage expansion.

Field behavior if adopted:
- `provider`: present when known (explicit config or inferred from adapter), omitted otherwise.
- `model`: present when configured; omitted if empty.
- `endpoint_host`: host (and port if non-default) only; no scheme, path, or query.
- `request_id`: present when provider returns one; omitted otherwise.

Example (snippet only):
```json
{
  "request_validation": {
    "ai": {
      "provider": "anthropic",
      "model": "opus-4.5",
      "endpoint_host": "api.anthropic.com",
      "request_id": "req_123"
    }
  }
}
```

If adopted:
- Update `docs/specs/validation-chain-audit-schema.md`
- Update audit entry tests to include new fields (optional)

### 8) Prompt Reliability Evaluation

We need a repeatable evaluation harness to ensure AI validation responses remain stable across providers and models.

Suggested evaluation layers:
1. **Local golden tests**:
   - Create a corpus of MCP tool calls and expected validation outcomes.
   - Run in CI with a deterministic mock AI client.
2. **External evals (TBD)**:
   - Integrate with third-party or vendor eval services to compare model outputs.
3. **Local model runners (TBD)**:
   - Support running evals against a local model for offline development.

Future CLI tool-call support:
- Extend validation input schema to include `call_type` (`tool_call` or `cli_command`), `command`, and `args`.
- Ensure the prompt template can render both MCP tool calls and CLI commands consistently.

### 9) Tests

**Unit tests**
- `internal/gateway/ai_client_test.go`:
  - Provider adapter selection
  - Header/auth construction rules
  - Schema validation behavior for providers without native JSON schema

**Engine tests**
- `internal/gateway/ai_engine_test.go`
- `internal/gateway/ai_response_engine_test.go`
  - Use mock AI clients to ensure no behavior regression.
  - Validate response parsing and error categories.

**Config tests**
- `internal/config/config_test.go`
  - Ensure new config fields load correctly.
  - Ensure backward compatibility when provider is unset.

**Audit tests**
- `internal/gateway/audit_entry_test.go`
  - If audit metadata added, validate fields are present and sanitized.

**Integration tests**
- Create a local stub server to emulate OpenAI-compatible responses.
- Validate that changing `endpoint` switches providers without code changes.

## Rollout Plan

1. Add provider-agnostic interface and OpenAI adapter that preserves current behavior.
2. Add OpenAI-compatible REST adapter with URL-only switching.
3. Add Anthropic REST adapter.
4. Add configuration docs and migration notes.
5. Optional: add SDK paths if necessary.

## Migration Strategy

1. **Introduce new provider interface**:
   - Add `AIProviderClient` and `AICompletionResult` types (new file).
   - Keep existing `AIClient` (OpenAI SDK wrapper) but stop using it directly in engines.

2. **Adapter layer**:
   - Create OpenAI adapter implementing `AIProviderClient` that wraps the existing OpenAI SDK usage.
   - Add OpenAI-compatible REST adapter and Anthropic REST adapter.

3. **Update engines**:
   - `ai_engine.go`, `ai_response_engine.go`, and `audit_report_tool.go` depend on `AIProviderClient`.
   - Remove SDK types from these files; they should use only provider-agnostic types.

4. **Update mocks/tests**:
   - Replace `MockAIClient` with `MockAIProviderClient` using the new request/result types.
   - Update tests to assert on provider-agnostic request payloads rather than OpenAI SDK structs.

5. **De-risk rollout**:
   - Ship OpenAI adapter first to preserve behavior.
   - Add other providers behind config flags once validated.

## Error Handling and Normalization

Define a standard error shape returned by adapters so the engines can log and classify consistently:

```
type AIProviderError struct {
    Category  string // "api_error", "timeout", "canceled", "parse_error", "no_response", "rate_limited", "auth_error", "invalid_request"
    Message   string
    Retryable bool
}
```

Normalization rules:
- Context deadline exceeded -> `timeout`
- Context canceled -> `canceled`
- HTTP 429 -> `rate_limited` (retryable)
- HTTP 401/403 -> `auth_error` (non-retryable)
- HTTP 400/422 -> `invalid_request` (non-retryable)
- Other 4xx/5xx -> `api_error` (retryable for 5xx only)
- Empty choices/outputs -> `no_response`
- JSON parse/validation errors -> `parse_error`

Engines continue using existing categories; new categories may appear in audit logs but should not break consumers.

## Retry and Backoff Strategy

Adapters should implement retries for retryable errors:
- Retry on: network errors, timeouts, 429, and 5xx.
- No retry on: auth errors, invalid requests, or parse errors.
- Strategy: exponential backoff with jitter (e.g., 200ms, 500ms, 1s), max 2 retries.
- Must respect context deadlines and `max_rule_evaluation_ms` (do not exceed the configured budget).

## System Prompt Handling

Current policies are user-message-only. To preserve behavior:
- `SystemPrompt` defaults to empty and is not required.
- For providers that support explicit system prompts (e.g., Anthropic), pass `SystemPrompt` only if set.
- No schema or rule format change is required in this spec; future enhancement can add optional system prompts in rule definitions.

## Open Questions

1. Do we want "auto" provider detection based on URL, or explicit `provider` only?
2. Should we keep OpenAI SDK support long-term or deprecate after REST is stable?
3. Should audit logs include `provider`, `model`, and `endpoint_host` by default?
4. What external eval tooling should we standardize on (if any)?
