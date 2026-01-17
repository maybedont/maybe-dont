# Govulncheck GitHub Action Specification

## Overview

This specification defines a GitHub Action workflow that runs `govulncheck` on pull requests to detect known vulnerabilities in Go dependencies. The check gates PR merges until either zero vulnerabilities are reported, or a justification is provided for merging with known vulnerabilities.

## Goals

1. **Automated vulnerability scanning**: Run `govulncheck` automatically on every PR
2. **PR gating**: Block merges when vulnerabilities are detected
3. **Justification workflow**: Allow merges with documented justification for known vulnerabilities
4. **Clear reporting**: Provide actionable vulnerability information in PR comments
5. **Minimal friction**: Don't block PRs unnecessarily (only flag vulnerabilities actually affecting the codebase)

## Background: govulncheck

`govulncheck` is the official Go vulnerability checker that:
- Analyzes call graphs to find vulnerabilities actually used in code (not just present in dependencies)
- Reports CVEs from the Go vulnerability database
- Provides severity levels and fix recommendations
- Can output in text, JSON, or SARIF formats

### Installation

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

### Basic Usage

```bash
# Check all packages
govulncheck ./...

# JSON output for parsing
govulncheck -json ./...

# Exit codes:
# 0 = no vulnerabilities
# 3 = vulnerabilities found
# 1 = other errors
```

## Workflow Design

### Workflow File: `.github/workflows/govulncheck.yaml`

```yaml
name: govulncheck

on:
  pull_request:
  push:
    branches:
      - main

permissions:
  contents: read
  pull-requests: write  # Required for posting comments

jobs:
  govulncheck:
    name: vulnerability scan
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: stable

      - name: Install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest

      - name: Run govulncheck
        id: vulncheck
        run: |
          set +e
          OUTPUT=$(govulncheck -json ./... 2>&1)
          EXIT_CODE=$?
          echo "exit_code=$EXIT_CODE" >> $GITHUB_OUTPUT

          # Save output for later steps
          echo "$OUTPUT" > vuln-report.json

          # Parse vulnerability count
          VULN_COUNT=$(echo "$OUTPUT" | jq -r '[.finding // empty] | length')
          echo "vuln_count=$VULN_COUNT" >> $GITHUB_OUTPUT

          exit 0  # Don't fail yet, let justification check run

      - name: Check for justification
        id: justification
        if: steps.vulncheck.outputs.vuln_count != '0'
        uses: actions/github-script@v7
        with:
          script: |
            // Check PR description and comments for justification
            const prNumber = context.payload.pull_request?.number;
            if (!prNumber) {
              core.setOutput('has_justification', 'false');
              return;
            }

            // Check PR body for justification marker
            const prBody = context.payload.pull_request.body || '';
            const hasJustificationInBody = prBody.includes('<!-- govulncheck-justification -->') ||
                                           prBody.toLowerCase().includes('vulnerability justification:');

            // Check comments for justification
            const comments = await github.rest.issues.listComments({
              owner: context.repo.owner,
              repo: context.repo.repo,
              issue_number: prNumber
            });

            const hasJustificationInComments = comments.data.some(comment =>
              comment.body.includes('<!-- govulncheck-justification -->') ||
              comment.body.toLowerCase().includes('vulnerability justification:')
            );

            const hasJustification = hasJustificationInBody || hasJustificationInComments;
            core.setOutput('has_justification', hasJustification.toString());

      - name: Post vulnerability report
        if: steps.vulncheck.outputs.vuln_count != '0'
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const vulnReport = JSON.parse(fs.readFileSync('vuln-report.json', 'utf8'));

            // Parse findings
            const findings = [];
            const lines = fs.readFileSync('vuln-report.json', 'utf8').split('\n');
            for (const line of lines) {
              if (!line.trim()) continue;
              try {
                const obj = JSON.parse(line);
                if (obj.finding) {
                  findings.push(obj.finding);
                }
              } catch (e) {}
            }

            // Build report
            let report = '## Vulnerability Scan Results\n\n';
            report += `Found **${findings.length}** vulnerabilities:\n\n`;

            for (const finding of findings) {
              const osv = finding.osv;
              report += `### ${osv}\n`;
              report += `- **Module**: ${finding.trace?.[0]?.module || 'unknown'}\n`;
              report += `- **Package**: ${finding.trace?.[0]?.package || 'unknown'}\n`;
              report += `- **Details**: https://pkg.go.dev/vuln/${osv}\n\n`;
            }

            report += '---\n\n';
            report += '### How to resolve\n\n';
            report += '**Option 1**: Fix the vulnerabilities\n';
            report += '- Update affected dependencies to patched versions\n';
            report += '- Run `go get -u <module>@<version>` and `go mod tidy`\n\n';
            report += '**Option 2**: Provide justification to merge with known vulnerabilities\n\n';
            report += 'If you have reviewed the vulnerabilities and determined they are acceptable for this PR, ';
            report += 'add a comment with the following format (copy and customize):\n\n';
            report += '```\n';
            report += '<!-- govulncheck-justification -->\n';
            report += 'Vulnerability Justification:\n';
            // Generate pre-filled template with actual vulnerability IDs
            for (const finding of findings) {
              const osv = finding.osv;
              report += `- ${osv}: [Explain why this is acceptable]\n`;
            }
            report += '```\n\n';
            report += 'After adding the justification comment, re-run this check.\n';

            // Post or update comment
            const prNumber = context.payload.pull_request?.number;
            if (prNumber) {
              const comments = await github.rest.issues.listComments({
                owner: context.repo.owner,
                repo: context.repo.repo,
                issue_number: prNumber
              });

              const botComment = comments.data.find(c =>
                c.user.type === 'Bot' && c.body.includes('## Vulnerability Scan Results')
              );

              if (botComment) {
                await github.rest.issues.updateComment({
                  owner: context.repo.owner,
                  repo: context.repo.repo,
                  comment_id: botComment.id,
                  body: report
                });
              } else {
                await github.rest.issues.createComment({
                  owner: context.repo.owner,
                  repo: context.repo.repo,
                  issue_number: prNumber,
                  body: report
                });
              }
            }

      - name: Fail if vulnerabilities without justification
        if: steps.vulncheck.outputs.vuln_count != '0' && steps.justification.outputs.has_justification != 'true'
        run: |
          echo "::error::Vulnerabilities detected without justification. See PR comment for details."
          exit 1

      - name: Pass with justification warning
        if: steps.vulncheck.outputs.vuln_count != '0' && steps.justification.outputs.has_justification == 'true'
        run: |
          echo "::warning::Vulnerabilities detected but justification provided. Proceeding with merge."
```

## Justification Format

### In PR Description

```markdown
## Summary
...

## Vulnerability Justification
<!-- govulncheck-justification -->
- GO-2024-XXXX: This vulnerability requires network access which our deployment doesn't expose
- GO-2024-YYYY: Fixed in next release, tracked in issue #123
```

### In PR Comment

```markdown
<!-- govulncheck-justification -->
Vulnerability Justification:
- GO-2024-XXXX: The vulnerable code path is not reachable in our usage. The affected function `foo.Bar()` is only called with trusted input from our internal services.
- GO-2024-YYYY: Accepting risk for 2 weeks while upstream fix is released. Mitigation: rate limiting is in place.
```

## PR Gating Logic

```
┌─────────────────────────┐
│   govulncheck runs      │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│ Vulnerabilities found?  │
└───────────┬─────────────┘
            │
     ┌──────┴──────┐
     │             │
     ▼ No          ▼ Yes
┌─────────┐  ┌─────────────────────┐
│  PASS   │  │ Check justification │
└─────────┘  └──────────┬──────────┘
                        │
                 ┌──────┴──────┐
                 │             │
                 ▼ No          ▼ Yes
           ┌─────────┐   ┌─────────────────┐
           │  FAIL   │   │ PASS (warning)  │
           └─────────┘   └─────────────────┘
```

## Branch Protection Configuration

To enforce the check, configure branch protection rules:

1. Go to **Settings** > **Branches** > **Branch protection rules**
2. Add rule for `main` branch
3. Enable **Require status checks to pass before merging**
4. Add `govulncheck / vulnerability scan` to required checks

## Configuration Options

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GOVULNCHECK_FAIL_ON_VULN` | `true` | Whether to fail the check when vulnerabilities are found |
| `GOVULNCHECK_ALLOW_JUSTIFICATION` | `true` | Whether to allow justification to bypass failures |

### Workflow Inputs (for reusable workflow)

```yaml
inputs:
  go-version:
    description: 'Go version to use'
    default: 'stable'
  fail-on-vulnerability:
    description: 'Fail check when vulnerabilities found'
    default: 'true'
  allow-justification:
    description: 'Allow justification comments to bypass'
    default: 'true'
```

## Output Examples

### Clean Scan (No Vulnerabilities)

```
✓ govulncheck / vulnerability scan
  No vulnerabilities found
```

### Vulnerabilities Found (No Justification)

When vulnerabilities are detected, the check fails and posts a detailed PR comment that:
1. Lists all detected vulnerabilities with links to details
2. Provides clear instructions for fixing the vulnerabilities
3. **Provides a copy-paste example for justification** so the user knows exactly how to proceed if they need to merge with known vulnerabilities

```
✗ govulncheck / vulnerability scan
  Error: Vulnerabilities detected without justification. See PR comment for details.
```

PR Comment (automatically posted):
```markdown
## Vulnerability Scan Results

Found **2** vulnerabilities:

### GO-2024-2687
- **Module**: golang.org/x/net
- **Package**: golang.org/x/net/http2
- **Details**: https://pkg.go.dev/vuln/GO-2024-2687

### GO-2024-2611
- **Module**: google.golang.org/grpc
- **Package**: google.golang.org/grpc
- **Details**: https://pkg.go.dev/vuln/GO-2024-2611

---

### How to resolve

**Option 1**: Fix the vulnerabilities
- Update affected dependencies to patched versions
- Run `go get -u <module>@<version>` and `go mod tidy`

**Option 2**: Provide justification to merge with known vulnerabilities

If you have reviewed the vulnerabilities and determined they are acceptable for this PR, add a comment with the following format (copy and customize):

<!-- govulncheck-justification -->
Vulnerability Justification:
- GO-2024-2687: [Explain why this is acceptable, e.g., "The vulnerable code path in http2 is not reachable - we only use http1.1"]
- GO-2024-2611: [Explain why this is acceptable, e.g., "Fixed in next sprint, tracked in issue #456"]

After adding the justification comment, re-run this check.
```

**Key UX requirement**: The PR comment MUST include a pre-filled justification template that lists the actual CVE/GO IDs found, so the user can simply copy, paste, and fill in their reasoning without having to look up the vulnerability IDs.

### Vulnerabilities Found (With Justification)

```
⚠ govulncheck / vulnerability scan
  Warning: Vulnerabilities detected but justification provided. Proceeding with merge.
```

## Implementation Checklist

- [ ] Create `.github/workflows/govulncheck.yaml` with workflow definition
- [ ] Test workflow on a PR with known vulnerable dependency
- [ ] Test justification workflow (comment-based bypass)
- [ ] Configure branch protection to require the check
- [ ] Update CLAUDE.md to document the new check
- [ ] Add to developer onboarding documentation

## Security Considerations

1. **Justification audit trail**: All justifications are recorded in PR history
2. **No silent bypasses**: Justifications must be explicit and visible
3. **Warning on bypass**: Even with justification, a warning is logged
4. **Main branch protection**: Vulnerabilities on main trigger alerts via push event

## Future Enhancements

1. **Severity filtering**: Only fail on HIGH/CRITICAL vulnerabilities
2. **Allowlist file**: `.govulncheck-ignore.yaml` for persistent exceptions
3. **Slack/email notifications**: Alert security team when justification is used
4. **SARIF upload**: Integrate with GitHub Security tab
5. **Scheduled scans**: Daily scans of main branch for new vulnerabilities

## References

- [govulncheck documentation](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)
- [Go Vulnerability Database](https://vuln.go.dev/)
- [GitHub Actions documentation](https://docs.github.com/en/actions)
- [Branch protection rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
