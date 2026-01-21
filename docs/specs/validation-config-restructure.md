# Validation Config Restructure

## Overview

Restructure the validation configuration to group CEL and AI validation under unified `request_validation` and `response_validation` sections, and rename rules files for consistency.

## Goals

1. Simplify config by reducing 4 top-level validation sections to 2
2. Make file naming consistent across all rules files
3. Maintain environment variable override capability
4. Ensure inline rules continue to work identically

## File Renames

| Current Name | New Name |
|--------------|----------|
| `config/rules.yaml` | `config/cel_request_rules.yaml` |
| `config/ai_rules.yaml` | `config/ai_request_rules.yaml` |
| `config/response_rules.yaml` | `config/cel_response_rules.yaml` |
| `config/ai_response_rules.yaml` | `config/ai_response_rules.yaml` (unchanged) |

## Config Structure Changes

### Current YAML Structure (4 top-level sections)

```yaml
request_validation:
  mode: enabled
  rules_file: "rules.yaml"

ai_request_validation:
  mode: audit_only
  rules_file: "ai_rules.yaml"

response_validation:
  mode: disabled
  rules_file: "response_rules.yaml"

ai_response_validation:
  mode: disabled
  rules_file: "ai_response_rules.yaml"
```

### New YAML Structure (2 top-level sections with nested ai/cel)

```yaml
request_validation:
  cel:
    mode: enabled
    rules_file: "cel_request_rules.yaml"
  ai:
    mode: audit_only
    rules_file: "ai_request_rules.yaml"

response_validation:
  cel:
    mode: disabled
    rules_file: "cel_response_rules.yaml"
  ai:
    mode: disabled
    rules_file: "ai_response_rules.yaml"
```

### Go Struct Changes

```go
// RequestValidation contains all request validation settings
type RequestValidation struct {
    CEL CELRequestValidationConfig `mapstructure:"cel"`
    AI  AIRequestValidationConfig  `mapstructure:"ai"`
}

// ResponseValidation contains all response validation settings
type ResponseValidation struct {
    CEL CELResponseValidationConfig `mapstructure:"cel"`
    AI  AIResponseValidationConfig  `mapstructure:"ai"`
}

// CELRequestValidationConfig for deterministic CEL-based request validation
type CELRequestValidationConfig struct {
    Enabled   *bool      `mapstructure:"enabled"` // Deprecated: use Mode instead
    Mode      PolicyMode `mapstructure:"mode"`
    RulesFile string     `mapstructure:"rules_file"`
    Rules     []Policy   `mapstructure:"rules"`
}

// AIRequestValidationConfig for AI-powered request validation
type AIRequestValidationConfig struct {
    Enabled   *bool      `mapstructure:"enabled"` // Deprecated: use Mode instead
    Mode      PolicyMode `mapstructure:"mode"`
    RulesFile string     `mapstructure:"rules_file"`
    Rules     []AIPolicy `mapstructure:"rules"`
}

// CELResponseValidationConfig for deterministic CEL-based response validation
type CELResponseValidationConfig struct {
    Enabled   *bool            `mapstructure:"enabled"` // Deprecated: use Mode instead
    Mode      PolicyMode       `mapstructure:"mode"`
    RulesFile string           `mapstructure:"rules_file"`
    Rules     []ResponsePolicy `mapstructure:"rules"`
}

// AIResponseValidationConfig for AI-powered response validation
type AIResponseValidationConfig struct {
    Enabled   *bool              `mapstructure:"enabled"` // Deprecated: use Mode instead
    Mode      PolicyMode         `mapstructure:"mode"`
    RulesFile string             `mapstructure:"rules_file"`
    Rules     []AIResponsePolicy `mapstructure:"rules"`
}
```

## Environment Variable Changes

### Current Pattern

```bash
MAYBE_DONT_REQUEST_VALIDATION_MODE=enabled
MAYBE_DONT_REQUEST_VALIDATION_RULES_FILE=rules.yaml
MAYBE_DONT_AI_REQUEST_VALIDATION_MODE=audit_only
MAYBE_DONT_AI_REQUEST_VALIDATION_RULES_FILE=ai_rules.yaml
```

### New Pattern

```bash
MAYBE_DONT_REQUEST_VALIDATION_CEL_MODE=enabled
MAYBE_DONT_REQUEST_VALIDATION_CEL_RULES_FILE=cel_request_rules.yaml
MAYBE_DONT_REQUEST_VALIDATION_AI_MODE=audit_only
MAYBE_DONT_REQUEST_VALIDATION_AI_RULES_FILE=ai_request_rules.yaml
MAYBE_DONT_RESPONSE_VALIDATION_CEL_MODE=disabled
MAYBE_DONT_RESPONSE_VALIDATION_AI_MODE=disabled
```

No code changes needed for env var handling - the existing `applyEnvironmentOverrides` function recursively walks nested structs using `mapstructure` tags.

## Code Changes Required

### 1. `internal/config/config.go`
- Update `Config` struct to use new nested structure
- Update `LoadConfig()` to load rules from new paths
- Update `ResolveValidationMode()` calls for each nested section
- Update validation in `validateConfigWithOptions()` to check nested paths

### 2. `internal/gateway/gateway.go`
- Update references: `cfg.RequestValidation.Mode` → `cfg.RequestValidation.CEL.Mode`
- Update references: `cfg.AIRequestValidation.Mode` → `cfg.RequestValidation.AI.Mode`
- Same pattern for response validation

### 3. `internal/gateway/tool_validation.go`
- Update validation chain setup to use new config paths

### 4. `internal/gateway/cel_engine.go`
- Update any config references for CEL policy loading

### 5. `internal/gateway/ai_engine.go`
- Update any config references for AI policy loading

### 6. `internal/gateway/response_validation.go`
- Update config references for response validation

### 7. Test files
- `internal/config/config_test.go` - Update test fixtures and assertions
- `internal/gateway/*_test.go` - Update test configs

## Documentation Updates

### 1. `CLAUDE.md`
- Update "Validation Policy Modes" section
- Update example config snippets
- Update environment variable examples
- Update "Security Rules" section to use new file names

### 2. `docs/specs/`
- Review and update any specs that reference config structure

### 3. `config/maybedont.yaml`
- Update to new structure with comments

### 4. Example rules files
- Rename files per the file renames table
- Update any internal comments

## Testing Strategy

### Config Loading Tests (`internal/config/config_test.go`)
- Test new nested structure loads correctly from YAML
- Test environment variable overrides work for nested paths
- Test inline rules work for both `request_validation.cel.rules` and `request_validation.ai.rules`
- Test default values are applied correctly for each nested section
- Test validation errors reference correct paths

### Validation Chain Tests (`internal/gateway/tool_validation_test.go`)
- Update test fixtures to use new config structure
- Verify CEL and AI validation still execute in correct order
- Verify mode settings work for each nested section

### Engine Tests
- `cel_engine_test.go` - Update config fixtures
- `ai_engine_test.go` - Update config fixtures

### Specific Test Cases
- Inline rules work for `request_validation.cel.rules`
- Inline rules work for `request_validation.ai.rules`
- Same for response validation
- Mixed: `rules_file` for CEL, inline `rules` for AI (and vice versa)

## Breaking Change

This is a breaking change. Users upgrading will need to:

1. Update their config file structure (nest `cel`/`ai` under `request_validation`/`response_validation`)
2. Rename rules files or update `rules_file` paths in config
3. Update any environment variables to new paths

### Migration Example

```yaml
# BEFORE
request_validation:
  mode: enabled
  rules_file: "rules.yaml"
ai_request_validation:
  mode: audit_only
  rules_file: "ai_rules.yaml"

# AFTER
request_validation:
  cel:
    mode: enabled
    rules_file: "cel_request_rules.yaml"
  ai:
    mode: audit_only
    rules_file: "ai_request_rules.yaml"
```

No backwards compatibility layer will be provided - this is a clean break.

## Default Values

| Config Path | Default Value |
|-------------|---------------|
| `request_validation.cel.mode` | `enabled` |
| `request_validation.ai.mode` | `audit_only` |
| `response_validation.cel.mode` | `disabled` |
| `response_validation.ai.mode` | `disabled` |

## Implementation Checklist

- [x] Rename config files in `config/` directory
- [x] Update `Config` struct in `internal/config/config.go`
- [x] Update `LoadConfig()` function
- [x] Update `validateConfigWithOptions()` function
- [x] Update `internal/gateway/gateway.go`
- [x] Update `internal/gateway/tool_validation.go`
- [x] Update `internal/gateway/cel_engine.go`
- [x] Update `internal/gateway/ai_engine.go`
- [x] Update `internal/gateway/response_validation.go`
- [x] Update `internal/config/config_test.go`
- [x] Update gateway test files
- [x] Update `config/maybedont.yaml`
- [x] Update `CLAUDE.md`
- [x] Review and update specs in `docs/specs/`
- [x] Run `make test` and fix any failures
- [x] Run `make lint` and fix any issues
