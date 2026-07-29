// Package cmd implements the maybe-dont CLI (built with Cobra).
//
// Subcommands:
//
//   - gateway start: runs the MCP proxy and REST validation endpoints.
//   - cli: validates and optionally executes a shell command through a
//     running gateway (internal/cliproxy).
//   - test policies: runs the policy test harness (internal/testsuite).
//   - skill, hooks: print embedded agent-integration material
//     (internal/skills, internal/hooks).
//   - config, version: inspect resolved configuration paths and build info.
package cmd
