# Specifications

This directory contains reference documentation for actively-maintained
processes and formats. Design rationale that used to live here as
individual specs has been consolidated into
[ARCHITECTURE.md](../../ARCHITECTURE.md)'s "Design notes" section, since
most of it described features that are now fully implemented and better
understood as part of the overall architecture than as standalone specs.

## Specs by Status

| Spec | Status | Description |
|------|--------|-------------|
| [policy-test-suite](policy-test-suite.md) | Implemented | CLI-based policy test harness: flags, suite/test-case schema, CI integration |
| [release-notes-process](release-notes-process.md) | Implemented | Versioned release notes (`release-notes/v{version}.md`) enforced by CI |

## Creating a New Spec

1. Create a new `.md` file in this directory
2. Start with a `# Title` and `## Status` section
3. Include: Overview, Goals, Non-Goals, and detailed design
4. Update this README to add the spec to the table above
5. Once implemented and stable, consider whether the spec should be trimmed
   to its non-obvious rationale and folded into `ARCHITECTURE.md` instead
   of kept as a standalone file — most specs eventually become either
   self-evident from the code (drop) or genuinely load-bearing context
   (fold into ARCHITECTURE.md), rather than staying a spec forever.
