# Specifications

This directory contains design specifications for Maybe Don't Gateway features.

## Spec Status

Each spec should have a `## Status` section near the top with one of these values:

| Status | Description |
|--------|-------------|
| **Draft** | Work in progress, not ready for implementation |
| **Ready for Implementation** | Design approved, ready to build |
| **Implemented** | Feature shipped, spec is reference documentation |
| **Superseded** | Replaced by another spec (link to replacement) |

## Specs by Status

### Implemented

| Spec | Description |
|------|-------------|
| [policy-test-suite](policy-test-suite.md) | CLI-based policy test harness with model matrix |
| [xdg-base-directory-support](xdg-base-directory-support.md) | XDG Base Directory conventions for config/state files |
| [test-pass-rate-history](test-pass-rate-history.md) | Rolling pass rate tracking, policy change markers, and stability reporting for the policy test suite |
| [release-notes-process](release-notes-process.md) | Versioned release notes (release-notes/v{version}.md) enforced by CI |
| [openhands-integration](openhands-integration.md) | OpenHands security analyzer integration via `POST /api/v1/action/validate` endpoint |
| [agent-hook-and-interceptor-integration](agent-hook-and-interceptor-integration.md) | Agent hooks and `POST /api/v1/intercept` endpoint — endpoint shipped, hook scripts in progress (#131) |

### Ready for Implementation

| Spec | Description |
|------|-------------|
| [ai-validation-provider-agnostic](ai-validation-provider-agnostic.md) | Multi-provider AI validation (OpenAI, Anthropic, OpenAI-compatible) |
| [mcp-sub-command](mcp-sub-command.md) | Move gateway server to `gateway` sub-command, update help text |
| [policy-evaluation-improvements](policy-evaluation-improvements.md) | Analysis of policy/test suite efficacy and phased improvement plan (Phases 1-2 complete, Phases 3-5 remaining) |

### Draft

| Spec | Description |
|------|-------------|
| [test-failure-categorization](test-failure-categorization.md) | Separate extra-policy-only failures from decision failures in model comparison table |
| [prompt-injection-considerations](prompt-injection-considerations.md) | Prompt injection threat model, product positioning, and optimization strategies |
| [api-token-obfuscation](api-token-obfuscation.md) | Obfuscate sensitive tokens in logs and audit entries |
| [confidence-scoring](confidence-scoring.md) | Confidence scoring for AI policy responses with configurable thresholds |
| [audit-report-timeout-optimization](audit-report-timeout-optimization.md) | Optimize audit report generation timeouts |
| [cli-proxy-for-ai-agents](cli-proxy-for-ai-agents.md) | CLI proxy mode for AI agent integrations |
| [response-validation-state-changes](response-validation-state-changes.md) | Response validation action semantics |
| [runtime-action-interception-architecture](runtime-action-interception-architecture.md) | Architecture for intercepting runtime actions |

### No Status (Legacy)

These specs predate the status convention and need review:

| Spec | Description |
|------|-------------|
| [ai-engine-error-logging](ai-engine-error-logging.md) | Error logging improvements for AI engines |
| [audit-action-reason](audit-action-reason.md) | Audit log action reason field |
| [dedicated-audit-log-writer](dedicated-audit-log-writer.md) | Dedicated writer for audit logs |
| [env-var-downstream-servers](env-var-downstream-servers.md) | Environment variable config for downstream servers |
| [gateway-auth-header-design](gateway-auth-header-design.md) | Gateway authentication header design |
| [govulncheck-github-action](govulncheck-github-action.md) | GitHub Action for vulnerability scanning |
| [lazy-discovery-synchronization](lazy-discovery-synchronization.md) | Lazy tool discovery synchronization |
| [rule-mode-simplification](rule-mode-simplification.md) | Simplify rule mode configuration |
| [validation-chain-audit-schema](validation-chain-audit-schema.md) | Audit entry schema for validation chain |
| [validation-config-restructure](validation-config-restructure.md) | Restructure validation configuration |

## Subdirectories

| Directory | Description |
|-----------|-------------|
| [policy-test-suite/](policy-test-suite/) | Test cases and suite configuration for policy testing |

## Creating a New Spec

1. Create a new `.md` file in this directory
2. Start with a `# Title` and `## Status` section
3. Include: Overview, Goals, Non-Goals, and detailed design
4. Update this README to add the spec to the appropriate status section

## Maintaining This Index

When changing a spec's status:
1. Update the `## Status` line in the spec itself
2. Move the spec to the appropriate section in this README
