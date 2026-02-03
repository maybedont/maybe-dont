# Provider-Agnostic AI Validation

## Status
**Ready for Implementation** - All open questions resolved (February 2026)


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
5. Per-phase or per-tool AI configuration (e.g., different endpoints/models for request validation vs response validation vs audit report). All AI-powered features share the single `validation.ai` configuration.

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
- **Bug**: `audit_report_tool.go` ignores `validation.ai.endpoint` and always uses OpenAI's default endpoint, even though config validation requires the endpoint field when audit report is enabled. This will be fixed as part of this work.

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
- `Parameters map[string]any` (provider-specific parameters from config)
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

- **OpenAI adapter** (`provider: openai`):
  - Uses OpenAI chat completions API format.
  - Default if `validation.ai.provider` is unset.
  - Endpoint: `https://api.openai.com/v1/chat/completions`

- **OpenAI-compatible adapter** (`provider: openai_compatible`):
  - Same request/response format as OpenAI, but with configurable endpoint.
  - Use cases:
    - **Google Gemini**: `https://generativelanguage.googleapis.com/v1beta/openai/` (use Gemini 2.5+ for full `response_format` JSON schema support)
    - **LiteLLM**: Route requests through LiteLLM proxy to any backend
    - **Azure OpenAI**: Microsoft's hosted OpenAI models
    - **vLLM / Ollama**: Self-hosted local model servers
    - **OpenRouter**: Multi-provider routing service
    - Any service exposing an OpenAI-compatible `/chat/completions` endpoint
  - Enables "URL-only" switching by changing `endpoint`.
  - **Note**: If deficiencies are found with a provider's OpenAI-compatible mode (e.g., incomplete structured output support), a native adapter can be added in the future.

- **Anthropic adapter** (`provider: anthropic`):
  - Uses Anthropic Messages API format (different from OpenAI).
  - Endpoint: `https://api.anthropic.com/v1/messages`
  - Automatically applies Anthropic-specific conventions (see HTTP Request Formats below).

Adapters should be isolated behind the same interface so validation engines do not depend on any SDK types.

### 3) Configuration Changes

Add optional fields under `validation.ai` while keeping current fields working:

```yaml
validation:
  ai:
    provider: "openai"        # openai | openai_compatible | anthropic (required)
    endpoint: ""              # optional for openai/anthropic (uses default), required for openai_compatible
    model: "gpt-4o-mini"
    api_key: "${OPENAI_API_KEY}"
    parameters:               # provider-specific parameters (see Provider Parameters below)
      max_tokens: 4096
      temperature: 0.0
    query_params:             # optional query parameters (e.g., for Azure api-version)
      api-version: "2024-02-15-preview"
    headers:                  # optional additional headers (passed to SDK via WithHeader options)
      X-Custom-Header: "value"
    auth:
      header: "Authorization" # optional override (default varies by provider)
      prefix: "Bearer "       # optional override (default varies by provider)
```

#### Provider Parameters

The `parameters` field is a generic key-value map for provider-specific API parameters. Each adapter reads the parameters it needs and validates required parameters during config validation (fail-fast).

| Provider | Option | Required? | Default | Description |
|----------|--------|-----------|---------|-------------|
| `anthropic` | `max_tokens` | Yes | 4096 | Maximum tokens in response (Anthropic API requires this) |
| All | `temperature` | No | 0.0 | Sampling temperature |
| All | `max_tokens` | No | None | Maximum tokens (optional for OpenAI, required for Anthropic) |

**Environment variable support:**
```bash
export MAYBE_DONT_VALIDATION_AI_PARAMETERS_MAX_TOKENS=4096
export MAYBE_DONT_VALIDATION_AI_PARAMETERS_TEMPERATURE=0.0
```

**Provider validation:** During config validation, each provider adapter validates that required parameters are present. Missing required parameters cause startup failure with a clear error message (e.g., `provider "anthropic" requires parameter "max_tokens"`). If a required parameter has a sensible default (like `max_tokens: 4096`), the adapter applies the default rather than failing.

#### Default Endpoints by Provider

| Provider | Default Endpoint | Endpoint Required? |
|----------|------------------|-------------------|
| `openai` | `https://api.openai.com/v1` | No (uses default, can override) |
| `anthropic` | `https://api.anthropic.com/v1` | No (uses default, can override) |
| `openai_compatible` | None | **Yes** (must specify full URL) |

#### Endpoint Semantics by Provider

The `endpoint` field has different semantics depending on the provider:

| Provider | Endpoint Type | SDK/REST | Description |
|----------|---------------|----------|-------------|
| `openai` | Base URL | SDK | SDK appends `/chat/completions`. Example: `https://api.openai.com/v1` |
| `anthropic` | Base URL | SDK | SDK appends `/messages`. Example: `https://api.anthropic.com/v1` |
| `openai_compatible` | **Full URL** | REST | Complete URL to the chat completions endpoint. Example: `https://my-proxy.com/v1/chat/completions` |

**Why the difference?** For `openai` and `anthropic`, we use official SDKs that expect a base URL and append their known API paths. For `openai_compatible`, we use direct REST calls to give full control over the URL structure, supporting any OpenAI-compatible endpoint regardless of path conventions.

Notes:
- `provider` is required and explicit (no auto-detection from URL). For backward compatibility, if `provider` is unset, default to `openai` and log a deprecation warning.
- `endpoint` is optional for `openai` and `anthropic` (sensible defaults applied); required for `openai_compatible`.
- `parameters` is a generic map for provider-specific API parameters; each adapter validates its required parameters during startup.
- `query_params` is optional and appended to requests (useful for Azure's `api-version` requirement).
- `headers` are passed to SDK clients via `WithHeader()` options, allowing custom headers for proxies or special requirements.
- For `anthropic`, the adapter applies default header conventions (`x-api-key`, `anthropic-version`) unless overridden via `headers` or `auth`.
- Keep `endpoint`, `model`, and `api_key` as-is for backward compatibility.
- Preserve environment-variable override behavior (e.g., `MAYBE_DONT_VALIDATION_AI_*`, `MAYBE_DONT_VALIDATION_AI_PARAMETERS_*`).

#### Configuration Examples

**OpenAI (default, minimal config):**
```yaml
validation:
  ai:
    provider: "openai"
    model: "gpt-4o-mini"
    api_key: "${OPENAI_API_KEY}"
    # endpoint omitted - uses default https://api.openai.com/v1
```

**OpenAI with custom proxy (base URL):**
```yaml
validation:
  ai:
    provider: "openai"
    endpoint: "https://my-proxy.com/openai/v1"  # SDK appends /chat/completions
    model: "gpt-4o-mini"
    api_key: "${OPENAI_API_KEY}"
```

**Anthropic Claude:**
```yaml
validation:
  ai:
    provider: "anthropic"
    model: "claude-sonnet-4-5-20250929"
    api_key: "${ANTHROPIC_API_KEY}"
    # endpoint omitted - uses default https://api.anthropic.com/v1
    parameters:
      max_tokens: 4096  # required for Anthropic, default applied if omitted
```

**Google Gemini (via OpenAI-compatible endpoint):**
```yaml
validation:
  ai:
    provider: "openai_compatible"
    endpoint: "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"  # Full URL
    model: "gemini-2.5-flash"  # Use 2.5+ for full JSON schema support
    api_key: "${GEMINI_API_KEY}"
```

**Azure OpenAI (with query params):**
```yaml
validation:
  ai:
    provider: "openai_compatible"
    endpoint: "https://my-resource.openai.azure.com/openai/deployments/my-deployment/chat/completions"
    model: "gpt-4o-mini"
    api_key: "${AZURE_OPENAI_API_KEY}"
    query_params:
      api-version: "2024-02-15-preview"
```

**LiteLLM (routing to any backend):**
```yaml
validation:
  ai:
    provider: "openai_compatible"
    endpoint: "http://localhost:4000/v1/chat/completions"  # Full URL
    model: "gpt-4o-mini"  # or any model configured in LiteLLM
    api_key: "${LITELLM_API_KEY}"
```

### 4) HTTP Request Formats by Provider

Each provider has different authentication and request body formats. The adapter layer handles these differences.

#### OpenAI / OpenAI-compatible

**Authentication:**
- Header: `Authorization: Bearer {api_key}`

**Request format:**
```json
{
  "model": "gpt-4o-mini",
  "messages": [
    {"role": "system", "content": "..."},
    {"role": "user", "content": "..."}
  ],
  "temperature": 0.0,
  "max_tokens": 1024,
  "response_format": {
    "type": "json_schema",
    "json_schema": { "name": "...", "schema": {...}, "strict": true }
  }
}
```

**Response format:**
```json
{
  "choices": [{ "message": { "content": "..." } }]
}
```

#### Anthropic

**Authentication:**
- Header: `x-api-key: {api_key}` (not Bearer token)
- Required header: `anthropic-version: 2023-06-01`
- Required header: `content-type: application/json`

**Request format:**
```json
{
  "model": "claude-sonnet-4-5-20250929",
  "system": "...",
  "messages": [
    {"role": "user", "content": "..."}
  ],
  "max_tokens": 1024,
  "temperature": 0.0
}
```

Note: `max_tokens` is **required** for Anthropic (unlike OpenAI where it's optional).

**Response format:**
```json
{
  "content": [{ "type": "text", "text": "..." }]
}
```

**JSON schema support:** Anthropic does not have native JSON schema enforcement like OpenAI's `response_format`. The adapter must embed schema instructions in the prompt and validate the response locally.

### 5) Providers Requiring Native Adapters (Future Work)

The following providers cannot use `openai_compatible` and would require dedicated adapters:

| Provider | Reason | Future Path |
|----------|--------|-------------|
| **Amazon Bedrock** | Requires AWS Signature v4 authentication (complex signing), would need AWS SDK dependency | Add `bedrock` adapter with AWS SDK |
| **Cohere, Mistral, etc.** | Each has unique API format without OpenAI-compatible endpoints | Add adapters as needed, or use via LiteLLM |

**Workaround:** Users can route requests through LiteLLM (which exposes an OpenAI-compatible API) to reach these providers without native adapter support.

### 6) Adding New Provider Adapters (Future)

To add support for a new provider:

1. **Implement the `AIProviderClient` interface** in a new adapter file (e.g., `ai_gemini.go`).
2. **Handle authentication**: Map `api_key` and optional `auth.*` config to the provider's expected headers.
3. **Transform request**: Convert `AIRequest` to the provider's request body format.
4. **Transform response**: Parse the provider's response into `AICompletionResult`.
5. **Handle JSON schema**: If the provider lacks native JSON schema support, embed schema in prompt and validate locally.
6. **Add provider constant** to the config validation and adapter factory.
7. **Document** the provider's endpoint, auth format, and any quirks in this spec.

### 7) SDK vs REST Analysis and Decision

#### Binary Size and Dependency Analysis

Analysis performed to evaluate SDK vs REST trade-offs (February 2026):

**Isolated SDK Size Impact:**

| Test Binary | Size | Delta |
|-------------|------|-------|
| Minimal (stdlib: net/http, encoding/json) | 4.5 MB | baseline |
| + OpenAI SDK (`openai-go` + tidwall libs) | 5.0 MB | +0.5 MB |
| + Anthropic SDK (includes AWS/GCP libs) | 7.4 MB | +2.9 MB |

**Maybe-dont Binary Impact:**

| Configuration | Dependencies | Binary Size | Delta |
|---------------|--------------|-------------|-------|
| Current (with OpenAI SDK) | 72 | 35 MB | baseline |
| + Anthropic SDK | 105 | 39 MB | +33 deps, +4 MB |
| REST-only (estimated) | ~67 | ~34-34.5 MB | -5 deps, -0.5-1 MB |

**OpenAI SDK dependencies** (lightweight, already included):
- `github.com/openai/openai-go`
- `github.com/tidwall/gjson`, `gjson`, `match`, `pretty`, `sjson`

**Anthropic SDK dependencies** (heavy, includes unused features):
- AWS SDK v2 (for Bedrock support - not needed)
- Google Cloud libraries (for Vertex AI support - not needed)
- OpenTelemetry/OpenCensus, gRPC, protocol buffers
- ~40+ transitive dependencies

#### Why Dependencies ≠ Proportional Size Increase

Go's **Dead Code Elimination (DCE)** aggressively excludes unused code from the binary:

| Package Category | In go.mod? | In Binary? | Why? |
|------------------|------------|------------|------|
| OpenAI SDK | ✅ | ✅ | Actually used |
| Tidwall JSON libs | ✅ | ✅ | Used by OpenAI SDK |
| Azure SDK | ✅ | ❌ | Transitive from viper, but unused |
| Testify/testing | ✅ | ❌ | Test-only, excluded from release build |
| gRPC/Protobuf | ✅ | Partial | Only code paths used by CEL included |

**The 72 current dependencies break down as:**
- ~15 core app (CLI, config, logging, MCP) → included
- ~5 OpenAI SDK → included
- ~4 CEL engine → included
- ~7 testing packages → excluded from release binary
- ~6 Azure (transitive from viper) → excluded (unused)
- ~10 stdlib extensions → partially included
- ~25 misc/transitive → varies by usage

**Verification:** Running `go tool nm maybe-dont | grep azure` returns no symbols, confirming Azure SDK code is excluded despite being in go.mod.

**Why Anthropic SDK still adds +4 MB despite DCE:**
The Anthropic SDK's core client code references AWS/GCP types directly, so even with DCE, its fundamental functionality pulls in more code paths than the lightweight OpenAI SDK which only uses simple JSON libraries.

#### Trade-off Summary: Anthropic Implementation

| Approach | Binary Impact | New Dependencies | Code to Write |
|----------|---------------|------------------|---------------|
| Anthropic SDK | +4 MB | +33 | ~10 lines |
| Anthropic REST | +0 MB | +0 | ~100-150 lines |

#### Alternative: Full REST-Only Implementation

For future reference, here is the analysis for removing all SDKs and using REST exclusively:

**Dependencies removed (5):**
- `github.com/openai/openai-go`
- `github.com/tidwall/gjson`
- `github.com/tidwall/match`
- `github.com/tidwall/pretty`
- `github.com/tidwall/sjson`

**Impact:**
- Final dependency count: ~67 (down from 72)
- Binary size: ~34-34.5 MB (down from 35 MB)
- Size reduction: ~0.5-1 MB

**Code required for REST-only:**

| Component | Lines | Description |
|-----------|-------|-------------|
| Provider-agnostic types | ~50 | AIProviderClient interface, AIRequest, AICompletionResult, AIProviderError structs |
| OpenAI REST client | ~150 | Request/response structs, HTTP client, headers, error mapping |
| Anthropic REST client | ~140 | Request/response structs, HTTP client, Anthropic-specific headers |
| Mock client update | ~60 | Update to use new provider-agnostic interface |
| Engine updates | ~80 | Modify ai_engine.go, ai_response_engine.go, audit_report_tool.go |
| **Total new code** | **~340-400** | |
| **Total modified code** | **~80-100** | |

**REST-only pros:**
- 5 fewer dependencies
- ~0.5-1 MB smaller binary
- Full control over HTTP behavior
- Smaller attack surface
- No SDK version upgrades to track

**REST-only cons:**
- ~400 lines of new code to write and maintain
- ~100 lines of existing code to modify
- Need to handle edge cases SDKs already handle

#### Decision: SDK for Known Providers, REST for OpenAI-Compatible

Given that SDK binary impact is modest (+4 MB for Anthropic) and reduces code maintenance burden, we will use **official SDKs for known providers** and **direct REST for openai_compatible**:

| Provider | Approach | Rationale |
|----------|----------|-----------|
| `openai` | **OpenAI SDK** | Already integrated, lightweight (+0.5 MB, 5 deps) |
| `openai_compatible` | **Direct REST** | Full control over URL structure; no path assumptions |
| `anthropic` | **Anthropic SDK** | Official SDK, +4 MB acceptable vs. ~150 lines custom code |

**Why REST for `openai_compatible`?** The OpenAI SDK's `WithBaseURL()` expects a base URL and appends `/chat/completions`. This doesn't work for endpoints with non-standard path structures. Using direct REST calls for `openai_compatible` gives users full control over the URL.

**Final binary impact:**
- Current: 35 MB, 72 dependencies
- After adding Anthropic SDK: 39 MB, 105 dependencies

**Future consideration:** If binary size or dependency count becomes a concern, we can migrate to REST-only (~400 lines of code) to save ~5 MB and 38 dependencies. The provider-agnostic interface design supports this migration path.

#### Provider Implementation Summary

| Provider | Implementation | API Endpoint | Auth |
|----------|----------------|--------------|------|
| `openai` | OpenAI SDK | `https://api.openai.com/v1` (base) | `Authorization: Bearer {key}` |
| `openai_compatible` | Direct REST | Full URL from config | `Authorization: Bearer {key}` |
| `anthropic` | Anthropic SDK | `https://api.anthropic.com/v1` (base) | `x-api-key: {key}` |

#### Implementation Details

**OpenAI** (SDK):
```go
// openai (default endpoint)
client := openai.NewClient(option.WithAPIKey(apiKey))

// openai with custom base URL (e.g., proxy)
client := openai.NewClient(
    option.WithAPIKey(apiKey),
    option.WithBaseURL(endpoint), // base URL only, SDK appends /chat/completions
)

// Custom headers passed via SDK options
client := openai.NewClient(
    option.WithAPIKey(apiKey),
    option.WithHeader("X-Custom-Header", "value"),
)
```

**OpenAI-compatible** (Direct REST):
```go
// Direct HTTP call to the configured full URL
req, _ := http.NewRequest("POST", endpoint, body) // endpoint is full URL
req.Header.Set("Authorization", "Bearer "+apiKey)
req.Header.Set("Content-Type", "application/json")
// Add any custom headers from config
for k, v := range config.Headers {
    req.Header.Set(k, v)
}
// Append query params if configured
if len(config.QueryParams) > 0 {
    q := req.URL.Query()
    for k, v := range config.QueryParams {
        q.Set(k, v)
    }
    req.URL.RawQuery = q.Encode()
}
```

**Anthropic** (SDK):
```go
client := anthropic.NewClient(option.WithAPIKey(apiKey))
// SDK handles x-api-key header and anthropic-version automatically

// Read max_tokens from parameters (required for Anthropic)
maxTokens := 4096 // default
if v, ok := config.Parameters["max_tokens"]; ok {
    maxTokens = v.(int)
}
```

#### Common OpenAI-compatible Endpoints

These are full URLs for use with `provider: openai_compatible`:

| Service | Full Endpoint URL | Notes |
|---------|-------------------|-------|
| Google Gemini | `https://generativelanguage.googleapis.com/v1beta/openai/chat/completions` | Use Gemini 2.5+ for JSON schema |
| Azure OpenAI | `https://{resource}.openai.azure.com/openai/deployments/{deployment}/chat/completions` | Requires `query_params: {api-version: "2024-02-15-preview"}` |
| LiteLLM | `http://localhost:4000/v1/chat/completions` | Port configurable |
| Ollama | `http://localhost:11434/v1/chat/completions` | Local models |
| vLLM | `http://localhost:8000/v1/chat/completions` | Local models |
| OpenRouter | `https://openrouter.ai/api/v1/chat/completions` | Multi-provider routing |

For standard OpenAI, use `provider: openai` (not `openai_compatible`) with the SDK.

### 8) Backward Compatibility

Compatibility strategy:
- `validation.ai.provider` is now required (explicit, no auto-detection).
- For backward compatibility, if `provider` is unset, default to `openai` and log a deprecation warning.
- If `endpoint` is unset for `openai` or `anthropic`, use the default endpoint for that provider.
- Keep existing config keys (`endpoint`, `model`, `api_key`) and env vars intact.
- Audit log schema will be extended with new optional fields (see next section).

### 9) Audit Log Updates

**Decision: Yes**, audit logs will include provider/model/endpoint metadata for debugging and traceability.

#### Design: Top-Level `ai` Field

Since the AI provider configuration is shared across both request and response validation, the metadata belongs at the **top level** of the audit entry (not duplicated in each validation phase).

#### Current Audit Entry Structure (Before)

```json
{
  "validation_started": "2026-02-03T12:00:00.000Z",
  "created_at": "2026-02-03T12:00:00.500Z",
  "tool": { ... },
  "upstream_request": { ... },
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 245,
      "evaluation_ms": 312,
      "deciding_rule": "dangerous-operations",
      "reason": "Request appears safe",
      "results": [ ... ]
    }
  },
  "response_validation": {
    "ai": { ... }
  },
  "action": "allow",
  "duration_ms": 500
}
```

#### New Audit Entry Structure (After)

New top-level `ai` field added (present when AI validation is enabled):

```json
{
  "validation_started": "2026-02-03T12:00:00.000Z",
  "created_at": "2026-02-03T12:00:00.500Z",
  "tool": { ... },
  "upstream_request": { ... },
  "ai": {
    "provider": "anthropic",
    "model": "claude-sonnet-4-5-20250929",
    "endpoint_host": "api.anthropic.com",
    "endpoint_path": "/v1/messages"
  },
  "request_validation": {
    "ai": {
      "action": "allow",
      "blocked_ms": 245,
      "evaluation_ms": 312,
      "deciding_rule": "dangerous-operations",
      "reason": "Request appears safe",
      "request_id": "req_01ABC123XYZ",
      "results": [ ... ]
    }
  },
  "response_validation": {
    "ai": {
      "action": "allow",
      "request_id": "req_02DEF456ABC",
      ...
    }
  },
  "action": "allow",
  "duration_ms": 500
}
```

#### Field Specifications

**Top-level `ai` object** (present when AI validation enabled):

| Field | Type | Presence | Description |
|-------|------|----------|-------------|
| `provider` | string | Always | Provider name: `openai`, `openai_compatible`, or `anthropic` |
| `model` | string | Always | Model identifier from config |
| `endpoint_host` | string | Always | Host (and port if non-default) only; no scheme or query |
| `endpoint_path` | string | Always | URL path only; no scheme, host, or query params (query params may contain secrets) |

**Per-validation `request_id`** (in `request_validation.ai` and `response_validation.ai`):

| Field | Type | Presence | Description |
|-------|------|----------|-------------|
| `request_id` | string | Optional | Provider's request ID for this specific API call; omitted if not returned |

Note: `request_id` stays in the per-validation section because each AI API call (request validation vs response validation) gets its own unique request ID from the provider.

#### Examples by Provider

**OpenAI:**
```json
{
  "ai": {
    "provider": "openai",
    "model": "gpt-4o-mini",
    "endpoint_host": "api.openai.com",
    "endpoint_path": "/v1/chat/completions"
  },
  "request_validation": {
    "ai": {
      "request_id": "chatcmpl-ABC123",
      ...
    }
  }
}
```

**Anthropic:**
```json
{
  "ai": {
    "provider": "anthropic",
    "model": "claude-sonnet-4-5-20250929",
    "endpoint_host": "api.anthropic.com",
    "endpoint_path": "/v1/messages"
  },
  "request_validation": {
    "ai": {
      "request_id": "req_01ABC123XYZ",
      ...
    }
  }
}
```

**OpenAI-compatible (Gemini):**
```json
{
  "ai": {
    "provider": "openai_compatible",
    "model": "gemini-2.5-flash",
    "endpoint_host": "generativelanguage.googleapis.com",
    "endpoint_path": "/v1beta/openai/chat/completions"
  }
}
```

**OpenAI-compatible (Azure with deployment):**
```json
{
  "ai": {
    "provider": "openai_compatible",
    "model": "gpt-4o-mini",
    "endpoint_host": "my-resource.openai.azure.com",
    "endpoint_path": "/openai/deployments/my-deployment/chat/completions"
  }
}
```

#### Implementation Notes

- Add new `AuditAIProvider` struct in `internal/gateway/audit_entry.go` with fields: `Provider`, `Model`, `EndpointHost`, `EndpointPath`
- Add `AIProvider *AuditAIProvider` field to `AuditEntry` struct
- Add `RequestID` field to `AuditAIResult` struct
- When populating `EndpointPath`, strip query parameters to avoid logging secrets
- Update `docs/specs/validation-chain-audit-schema.md` with new fields
- Update audit entry tests to verify new fields are populated

### 10) Prompt Reliability Evaluation

We need a repeatable evaluation harness to ensure AI validation responses remain stable across providers and models.

Suggested evaluation layers:
1. **Local golden tests**:
   - Create a corpus of MCP tool calls and expected validation outcomes.
   - Run in CI with a deterministic mock AI client.
2. **External evals**: Out of scope for this spec - will be addressed in a separate prompt reliability evaluation spec.
3. **Local model runners**: Out of scope for this spec - will be addressed in a separate prompt reliability evaluation spec.

Future CLI tool-call support:
- Extend validation input schema to include `call_type` (`tool_call` or `cli_command`), `command`, and `args`.
- Ensure the prompt template can render both MCP tool calls and CLI commands consistently.

### 11) Tests

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

1. Add provider-agnostic interface (`AIProviderClient`) and types (`AIRequest`, `AICompletionResult`).
2. Create OpenAI adapter wrapping the existing SDK, supporting `WithBaseURL()` for custom endpoints.
3. Create OpenAI-compatible adapter using direct REST calls for full URL control.
4. Add Anthropic adapter using official SDK (`anthropic-sdk-go`).
5. Update configuration to support `provider`, `parameters`, `query_params` fields.
6. Update audit log schema with provider metadata fields (`endpoint_host`, `endpoint_path`).
7. Add configuration docs and migration notes.

## Migration Strategy

1. **Introduce new provider interface**:
   - Add `AIProviderClient` and `AICompletionResult` types (new file: `ai.go`).
   - Keep existing `AIClient` (OpenAI SDK wrapper) but stop using it directly in engines.

2. **Adapter layer**:
   - Create OpenAI adapter implementing `AIProviderClient` that wraps the existing OpenAI SDK.
   - Create OpenAI-compatible adapter using direct REST calls for full URL control.
   - Add Anthropic adapter using official `anthropic-sdk-go` SDK.

3. **Update engines**:
   - `ai_engine.go`, `ai_response_engine.go`, and `audit_report_tool.go` depend on `AIProviderClient`.
   - Remove SDK types from these files; they should use only provider-agnostic types.
   - **Bug fix**: `audit_report_tool.go` must use the configured `validation.ai.endpoint` (currently ignores it and defaults to OpenAI).

4. **Update mocks/tests**:
   - Replace `MockAIClient` with `MockAIProviderClient` using the new request/result types.
   - Update tests to assert on provider-agnostic request payloads rather than OpenAI SDK structs.

5. **De-risk rollout**:
   - Ship OpenAI adapter first to preserve existing behavior.
   - Add OpenAI-compatible adapter (REST-based) next.
   - Add Anthropic adapter once OpenAI adapters are validated.

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

Retry behavior varies by provider implementation:

| Provider | Retry Handling | Behavior |
|----------|----------------|----------|
| `openai` | **SDK handles retries** | SDK retries on 429 (rate limit), 5xx (server errors), and network errors with exponential backoff |
| `anthropic` | **SDK handles retries** | SDK retries on 429, 5xx, and network errors with exponential backoff |
| `openai_compatible` | **No automatic retries** | Direct REST calls without retry logic (see limitation below) |

**SDK retry behavior:**
- SDKs retry on: 429 (rate limited), 5xx (server errors), transient network errors
- SDKs do not retry on: 401/403 (auth errors), 400/422 (invalid requests)
- SDKs use exponential backoff with jitter
- Context deadlines are respected (retries stop when `max_rule_evaluation_ms` budget is exhausted)

**Limitation: `openai_compatible` has no retry logic.** Since we use direct REST calls for `openai_compatible` to support arbitrary URL structures, there is no automatic retry on transient errors. If retry support is needed for `openai_compatible` endpoints, this can be added in a future enhancement. Implementation note: add a code comment indicating this limitation when implementing the REST client.

## System Prompt Handling

Current policies are user-message-only. To preserve behavior:
- `SystemPrompt` defaults to empty and is not required.
- For providers that support explicit system prompts (e.g., Anthropic), pass `SystemPrompt` only if set.
- No schema or rule format change is required in this spec; future enhancement can add optional system prompts in rule definitions.

## Resolved Questions

1. **Provider detection**: Explicit `provider` configuration required (no auto-detection from URL). For backward compatibility, if `provider` is unset, default to `openai` with a deprecation warning. Default endpoints are applied per provider unless overridden.
2. **SDK vs REST**: Using official SDKs for `openai` and `anthropic`. Using direct REST for `openai_compatible` to support arbitrary URL structures. REST-only alternative for all providers documented in Section 7 for future consideration.
3. **Endpoint semantics**: For SDK providers (`openai`, `anthropic`), `endpoint` is a base URL and the SDK appends the API path. For `openai_compatible`, `endpoint` is the full URL to the chat completions endpoint.
4. **Provider-specific parameters**: Added generic `parameters` map for provider-specific API parameters (e.g., `max_tokens`, `temperature`). Each adapter validates required parameters during config validation (fail-fast). Supports env vars like `MAYBE_DONT_VALIDATION_AI_PARAMETERS_MAX_TOKENS=4096`.
5. **Query parameters**: Added `query_params` config field for providers that require URL query parameters (e.g., Azure's `api-version`).
6. **Custom headers**: The `headers` config field is passed to SDK clients via `WithHeader()` options, supporting proxies and custom requirements.
7. **Retry strategy**: SDK handles retries for `openai` and `anthropic` (429, 5xx, network errors with backoff). No automatic retries for `openai_compatible` (documented limitation; can be added later if needed).
8. **Audit log metadata**: Include `provider`, `model`, `endpoint_host`, `endpoint_path` (no query params), and per-call `request_id` in audit logs for debugging.
9. **Prompt reliability evaluation**: Out of scope for this spec. Will be addressed in a separate spec for AI validation testing and evaluation.
