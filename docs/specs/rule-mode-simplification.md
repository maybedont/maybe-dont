# Rule Mode Simplification Specification

## Overview

Simplify the rule mode system to use separate `enabled` and `mode` fields with clear, independent semantics. This replaces the current single `mode` field approach where per-rule values override top-level settings, which creates confusing interactions.

## Problem Statement

The current mode system has a usability issue:

1. Top-level config sets `mode: enabled | audit_only | disabled`
2. Per-rule config can set `mode: enabled | audit_only | disabled` to override
3. If a user sets `mode: enabled` on a rule "just to be explicit," they lose the ability to quickly toggle all rules to `audit_only` via the top-level config
4. Similarly, `mode: disabled` per-rule permanently disables that rule regardless of top-level

**User scenario:** An issue occurs in production. The user wants to immediately switch all validation to `audit_only` to stop blocking while investigating. With explicit per-rule modes, they must review every rule file to find and remove overrides.

## Goals

1. **Simple mental model**: Each field has one clear purpose
2. **Quick global toggle**: Change top-level `mode: audit_only` to make all enabled rules non-blocking
3. **Per-rule control**: Still allow disabling specific rules or making them always audit-only
4. **Symmetric design**: Same fields at top-level and per-rule

## Design

### Field Semantics

| Field | Values | Purpose |
|-------|--------|---------|
| `enabled` | `true` (default) / `false` | On/off switch - does this rule run? |
| `mode` | `audit_only` or `enforce` | When `audit_only`, never block (log only); when `enforce`, rules can block |

**No `mode: enabled` or `mode: disabled`**. The `enabled` boolean handles on/off. The `mode` field supports `audit_only` or `enforce`. If omitted, defaults to `enforce`.

### Top-Level Config

```yaml
request_validation:
  cel:
    enabled: true          # Whether CEL request validation runs (default: true)
    mode: audit_only       # Optional: when set, all rules become audit_only
    rules_file: "cel_request_rules.yaml"
  ai:
    enabled: true          # Whether AI request validation runs (default: true)
    mode: audit_only       # Optional: when set, all rules become audit_only (this is the default for AI)
    rules_file: "ai_request_rules.yaml"

response_validation:
  cel:
    enabled: false         # Response validation disabled by default
    rules_file: "cel_response_rules.yaml"
  ai:
    enabled: false         # Response validation disabled by default
    rules_file: "ai_response_rules.yaml"
```

### Per-Rule Config

```yaml
rules:
  - name: block_destructive_ops
    description: Block dangerous operations
    expression: "request.method == 'tools/call' && request.params.name.endsWith('__delete')"
    action: deny
    message: "Destructive operations are not allowed"
    # enabled: true (default, can omit)
    # mode: (omitted = follows top-level)

  - name: experimental_check
    description: Testing a new rule
    expression: "..."
    action: deny
    enabled: false         # This rule never runs

  - name: sensitive_audit
    description: Always log but never block
    expression: "..."
    action: deny
    mode: audit_only       # This rule never blocks, even if top-level is enabled
```

### Resolution Logic

```
1. Top-level enabled == false?     → validation phase skipped entirely
2. Rule enabled == false?          → rule skipped
3. Top-level mode == "audit_only"? → rule is audit_only
4. Rule mode == "audit_only"?      → rule is audit_only
5. Otherwise                       → rule is enforce (can block)
```

The key insight: **Top-level `mode: audit_only` applies to ALL enabled rules**. Per-rule `mode: audit_only` is additive - it ensures that specific rule never blocks even when top-level allows blocking.

### Defaults by Validation Type

| Validation Phase | `enabled` default | `mode` default | Effect |
|------------------|-------------------|----------------|--------|
| `request_validation.cel` | `true` | `audit_only` | Rules audit only (default) |
| `request_validation.ai` | `true` | `audit_only` | Rules audit only |
| `response_validation.cel` | `false` | - | Phase disabled |
| `response_validation.ai` | `false` | - | Phase disabled |

These defaults match current behavior.

## Code Changes

### 1. `internal/config/config.go`

#### Update PolicyMode type

```go
// PolicyMode represents the execution mode for a policy.
// Valid values: "audit_only" (log but don't block) or "enforce" (rules can block).
type PolicyMode string

const (
    PolicyModeAuditOnly PolicyMode = "audit_only"
    PolicyModeEnforce   PolicyMode = "enforce"
)

// IsValid returns true if the PolicyMode is a recognized value.
func (m PolicyMode) IsValid() bool {
    return m == PolicyModeAuditOnly || m == PolicyModeEnforce
}
```

#### Update validation config structs

```go
// CELRequestValidationConfig for deterministic CEL-based request validation
type CELRequestValidationConfig struct {
    Enabled   bool       `mapstructure:"enabled"`    // Whether this validation phase runs (default: true)
    Mode      PolicyMode `mapstructure:"mode"`       // "audit_only" or "enforce" (default: enforce)
    RulesFile string     `mapstructure:"rules_file"`
    Rules     []Policy   `mapstructure:"rules"`
}

// AIRequestValidationConfig for AI-powered request validation
type AIRequestValidationConfig struct {
    Enabled   bool       `mapstructure:"enabled"`    // Whether this validation phase runs (default: true)
    Mode      PolicyMode `mapstructure:"mode"`       // "audit_only" or "enforce" (default: audit_only for AI)
    RulesFile string     `mapstructure:"rules_file"`
    Rules     []AIPolicy `mapstructure:"rules"`
}

// Similar updates for CELResponseValidationConfig and AIResponseValidationConfig
```

#### Update policy structs

```go
// Policy represents a single deterministic policy rule
type Policy struct {
    Name        string       `mapstructure:"name"`
    Description string       `mapstructure:"description"`
    Expression  string       `mapstructure:"expression"`
    Action      PolicyAction `mapstructure:"action"`
    Message     string       `mapstructure:"message"`
    Enabled     *bool        `mapstructure:"enabled"` // nil = true (default enabled)
    Mode        PolicyMode   `mapstructure:"mode"`    // "audit_only" or "enforce"
}

// IsEnabled returns whether this policy is enabled (defaults to true if not set)
func (p *Policy) IsEnabled() bool {
    if p.Enabled == nil {
        return true
    }
    return *p.Enabled
}

// Similar updates for AIPolicy, ResponsePolicy, AIResponsePolicy
```

#### Update ResolvePolicyMode

```go
// ResolvePolicyMode determines the effective mode for a rule.
// Top-level audit_only applies to all rules. Per-rule audit_only is additive.
func ResolvePolicyMode(topLevelMode PolicyMode, ruleMode PolicyMode) PolicyMode {
    if topLevelMode == PolicyModeAuditOnly {
        return PolicyModeAuditOnly
    }
    if ruleMode == PolicyModeAuditOnly {
        return PolicyModeAuditOnly
    }
    return PolicyModeEnforce
}
```

#### Remove deprecated code

- Remove `PolicyModeEnabled` and `PolicyModeDisabled` constants
- Remove `ResolveValidationMode` function (no longer needed with bool `enabled`)
- Remove deprecated `Enabled *bool` pointer pattern from validation config structs

#### Update LoadConfig defaults

```go
// In LoadConfig(), set defaults for enabled field
// CEL request: enabled=true, mode="audit_only"
// AI request: enabled=true, mode="audit_only"
// CEL response: enabled=false, mode="audit_only"
// AI response: enabled=false, mode="audit_only"
```

### 2. `internal/gateway/cel_engine.go`

Update `LoadPolicies` to use new fields:

```go
func (e *CELPolicyEngine) LoadPolicies(policies []config.Policy, topLevelMode config.PolicyMode) error {
    for _, policy := range policies {
        // Skip disabled rules
        if !policy.IsEnabled() {
            e.logger.Debug(ctx, "Skipping disabled rule", zap.String("name", policy.Name))
            continue
        }

        // Resolve effective mode
        effectiveMode := config.ResolvePolicyMode(topLevelMode, policy.Mode)

        // ... rest of loading logic
    }
}
```

### 3. `internal/gateway/ai_engine.go`

Same pattern as CEL engine.

### 4. `internal/gateway/cel_response_engine.go` and `ai_response_engine.go`

Same pattern as request engines.

### 5. Config validation

Update `validateConfigWithOptions`:

```go
// Validate mode values - only "audit_only" or empty allowed
if cfg.RequestValidation.CEL.Mode != "" && cfg.RequestValidation.CEL.Mode != config.PolicyModeAuditOnly {
    errors = append(errors, fmt.Sprintf("request_validation.cel.mode: invalid value '%s', must be 'audit_only' or omitted", cfg.RequestValidation.CEL.Mode))
}
// Similar for other validation sections and per-rule modes
```

### 6. Update rule file loading

When loading rules from YAML, validate that:
- `mode` is either empty or `"audit_only"`
- `enabled` is either omitted, `true`, or `false`

## Documentation Updates

### 1. `CLAUDE.md`

Update "Validation Policy Modes" section:

```markdown
### Validation Policy Modes

Each validation type has two configuration options:

- **enabled**: `true` (default) or `false` - whether this validation phase runs
- **mode**: `audit_only` (optional) - when set, all rules log but never block

Default modes:
- `request_validation.cel`: enabled=true, mode=(empty) - rules can block
- `request_validation.ai`: enabled=true, mode=audit_only - rules audit only
- `response_validation.cel`: enabled=false - phase disabled
- `response_validation.ai`: enabled=false - phase disabled

Per-rule configuration:
- `enabled: false` - skip this specific rule
- `mode: audit_only` - this rule never blocks, even if top-level allows blocking
```

### 2. `config/maybe-dont.yaml`

Update example config with new structure.

### 3. Rule file examples

Update `config/cel_request_rules.yaml`, `config/ai_request_rules.yaml`, etc. with examples showing `enabled` and `mode` usage.

## Migration

This is a breaking change. Users must update their config files:

### Config file changes

```yaml
# BEFORE
request_validation:
  cel:
    mode: enabled
    rules_file: "cel_request_rules.yaml"

# AFTER
request_validation:
  cel:
    enabled: true
    # mode: (omit for enabled behavior)
    rules_file: "cel_request_rules.yaml"
```

### Rule file changes

```yaml
# BEFORE
rules:
  - name: my_rule
    mode: disabled  # or mode: enabled
    ...

# AFTER
rules:
  - name: my_rule
    enabled: false  # instead of mode: disabled
    # mode: (omit, or use audit_only)
    ...
```

## Testing

### Unit Tests

1. **Test top-level enabled=false skips phase entirely**
2. **Test per-rule enabled=false skips that rule**
3. **Test top-level mode=audit_only makes all rules audit_only**
4. **Test per-rule mode=audit_only overrides when top-level is empty**
5. **Test default values are applied correctly**
6. **Test invalid mode values are rejected**

### Integration Tests

1. **Test quick toggle scenario**: Start with enabled rules, change top-level to audit_only, verify no rules block
2. **Test mixed configuration**: Some rules with explicit audit_only, others following top-level

## Implementation Checklist

- [ ] Update `PolicyMode` type and constants in `config.go`
- [ ] Update validation config structs with `Enabled` bool field
- [ ] Update policy structs with `Enabled *bool` field and `IsEnabled()` method
- [ ] Update `ResolvePolicyMode` function with new logic
- [ ] Remove deprecated `ResolveValidationMode` function
- [ ] Update `LoadConfig` with new defaults
- [ ] Update config validation for new fields
- [ ] Update `cel_engine.go` `LoadPolicies`
- [ ] Update `ai_engine.go` `LoadPolicies`
- [ ] Update `cel_response_engine.go` `LoadPolicies`
- [ ] Update `ai_response_engine.go` `LoadPolicies`
- [ ] Update `tool_validation.go` to pass correct mode to engines
- [ ] Update `response_validation.go` to pass correct mode to engines
- [ ] Update unit tests in `config_test.go`
- [ ] Update unit tests in engine test files
- [ ] Update `CLAUDE.md` documentation
- [ ] Update `config/maybe-dont.yaml` example
- [ ] Update rule file examples
- [ ] Run `make test` and fix failures
- [ ] Run `make lint` and fix issues
