# Security Policy

## Supported Versions

Only the latest released version of the gateway is supported with security
fixes. Please upgrade to the latest release before reporting an issue.

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Instead, use GitHub's private vulnerability reporting for this repository:
open the **Security** tab → **Report a vulnerability**. This creates a
private advisory visible only to maintainers until a fix is ready.

Include:

- A description of the vulnerability and its potential impact
- Steps to reproduce, including a minimal config/policy if relevant
- The gateway version (`maybe-dont version`)

## Response

We will acknowledge new reports as quickly as we can and keep you updated
as we investigate and develop a fix. Once a fix is released, we will publish
a security advisory and credit the reporter unless anonymity is requested.

## Scope

This policy covers the `maybe-dont` gateway binary and its official Docker
images and Homebrew formula. It does not cover third-party MCP servers or
AI providers you configure the gateway to call.
