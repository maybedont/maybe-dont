# Release Notes Process

> **Status**: See [README.md](README.md)

## Overview

Each release published to `maybedont/releases` should have curated, user-facing release notes rather than a raw commit log. Release notes are stored as versioned files in `release-notes/` (e.g., `release-notes/v1.1.0.md`) and are enforced by the release workflow.

## Enforcement

Release notes are enforced at two levels:

1. **Local**: `make bump-version` dry-runs `cz bump` to determine the next version, then checks that `release-notes/v{version}.md` exists before creating the tag. This catches missing notes before anything is pushed.
2. **CI**: The release workflow (`releaser.yaml`) validates the file exists before goreleaser runs. If the file is missing, the release fails with a clear error message.

Since branch protection requires PRs with reviews to merge to `main`, the release notes file must have been reviewed before it can land on `main` — and therefore before a tag can successfully release. This ensures release notes are both present and reviewed for every release.

## Pre-Release Checklist

Before running `make bump-version`:

1. **Review changes since last release**: `git log <last-tag>..HEAD --oneline --grep="^feat"`
2. **Create `release-notes/v{version}.md`** with user-facing content:
   - Brief intro paragraph summarizing the release theme
   - Breaking changes called out first (with before/after examples)
   - New features with short descriptions and usage examples
   - Omit: bug fixes, CI/CD changes, refactors, internal details
   - Installation section with Homebrew and GitHub releases links
3. **Open a PR** with the release notes file for review
4. **After merge**, run `make bump-version` and push the tag

## Content Guidelines

Start from `release-notes/TEMPLATE.md` — copy it to `release-notes/v{version}.md` and fill in the sections.

- **Audience**: Developers and operators who use Maybe Don't Gateway
- **Tone**: Concise and scannable, with code examples where they aid understanding
- **Remove empty sections**: Delete any section that doesn't apply to the release

### Standard Sections

| Section | Contents |
|---------|----------|
| **Breaking Changes** | Backward-incompatible changes with before/after examples |
| **New** | New features and capabilities |
| **Changed** | Enhancements to existing functionality |
| **Fixed** | Notable bug fixes worth calling out (omit trivial/internal fixes) |
| **Security** | Security improvements, vulnerability fixes, or hardening |
| **Installation** | Pre-filled — just update the version in the releases URL |

## How It Works

The release workflow in `.github/workflows/releaser.yaml`:

1. Extracts the version from the git tag (e.g., `v1.2.0` → `1.2.0`)
2. Checks that `release-notes/v1.2.0.md` exists — fails the workflow if missing
3. Passes `--release-notes=release-notes/v1.2.0.md` to goreleaser
4. GoReleaser uses the file contents as the GitHub release body

The `release-notes/` directory preserves history across releases. These files can also be used by future workflows to update releases programmatically.
