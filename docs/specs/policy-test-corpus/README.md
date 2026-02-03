# Policy Test Corpus (Draft)

This directory contains a draft starter corpus for validating policy behavior in CI or locally. It is designed to be vendor‑agnostic and to work with both deterministic (CEL/compiled) and AI policies.

## Layout

```
docs/specs/policy-test-corpus/
  corpus.yaml
  cases/
    *.yaml
```

## Conventions

- `policy_id` should match the **rule name** from the loaded policy bundle (there is no separate ID today).
- Case IDs should be unique, short, and stable (prefer `req-*` and `resp-*` prefixes).
- If a rule is **disabled** or **audit_only**, mention it explicitly in `notes`.
- Keep inputs minimal but representative; avoid real secrets.

## Running the Harness (Proposed CLI)

```bash
maybe-dont test policies --corpus docs/specs/policy-test-corpus
```

Examples:

```bash
maybe-dont test policies --corpus docs/specs/policy-test-corpus --engine ai --model openai:gpt-4o-mini
maybe-dont test policies --corpus docs/specs/policy-test-corpus --engine ai --matrix
```

## Corpus Validation (Proposed)

Before running tests, the harness should validate the corpus and fail fast on schema issues:
- `corpus.yaml` must include: `version`, `bundle_id`, `acceptance`, `engines`.
- Each case must include: `case_id`, `title`, `action_envelope`, `expectations`.
- `action_envelope` must include: `action_type`, `target`, `parameters`, `request_id`.
- `expectations.overall.decision` must be one of: `allow`, `deny`, `redact`, `require_preflight`, `needs_review`.
- `expectations.policies[*].policy_id` must be non-empty.

Optional but recommended:
- `response_sample` for response policies.
- `estimated_impact` and `data_classification`.
- `min_confidence` on overall expectations.

## Coverage Checklist (Proposed)

The harness should emit a coverage report:
- **Missing coverage**: Policies in the loaded bundle with zero matching cases.
- **Orphaned cases**: Cases referencing policy IDs that do not exist in the loaded bundle.
- **Disabled/audit_only**: Cases that target disabled or audit-only rules (report separately).
- **Engine gaps**: Cases that require AI evaluation but run with `--engine cel` only.

Suggested policy coverage targets:
- At least **1 case per policy**.
- At least **1 positive and 1 negative case** for high-risk policies.
- At least **1 response case** for each response rule.

## Policy Source of Truth

The harness should load the **exact shipped policies** from `internal/config/defaults` by default. Custom policy sources can be selected by flags (e.g., `--rules-dir` or explicit file overrides).

## Notes

This corpus is intentionally small and should grow with new policies. Add or update cases when policies are added, removed, or changed.
