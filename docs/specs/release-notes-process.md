# Release Notes Process

> **Status**: See [README.md](README.md)

## Overview

Each release published to `maybedont/releases` should have curated, user-facing release notes rather than a raw commit log. GoReleaser is configured to use `RELEASE_NOTES.md` at the repo root as the release body.

## Pre-Release Checklist

Before running `make bump-version`:

1. **Review changes since last release**: `git log <last-tag>..HEAD --oneline --grep="^feat"`
2. **Update `RELEASE_NOTES.md`** with user-facing content:
   - Brief intro paragraph summarizing the release theme
   - Breaking changes called out first (with before/after examples)
   - New features with short descriptions and usage examples
   - Omit: bug fixes, CI/CD changes, refactors, internal details
3. **Verify the release URL** in the installation section points to the correct tag
4. **Commit `RELEASE_NOTES.md`** as part of the version bump or in a preceding commit

## Content Guidelines

- **Audience**: Developers and operators who use Maybe Don't Gateway
- **Tone**: Concise and scannable, with code examples where they aid understanding
- **Structure**: Intro paragraph, then feature sections separated by horizontal rules
- **Breaking changes**: Always document with before/after examples
- **Installation section**: Include Homebrew and GitHub releases links at the end

## How It Works

GoReleaser is configured with `release.release_notes: RELEASE_NOTES.md` in `.goreleaser.yaml`. When a tag is pushed and the release workflow runs, GoReleaser reads this file and uses its contents as the GitHub release body instead of generating a changelog from commit messages.

## Post-Release

After the release is published, `RELEASE_NOTES.md` remains in the repo as a record of the last release. It gets overwritten before the next release.
