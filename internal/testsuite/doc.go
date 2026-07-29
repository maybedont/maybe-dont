// Package testsuite implements the `maybe-dont test policies` harness: it
// runs a YAML-defined suite of request/response test cases against the CEL
// and AI validation engines and reports pass/fail, optionally across a
// matrix of AI models (runner.go, executor.go, ai_executor.go).
//
// A suite lives in a directory containing suite.yaml (types.go) and a
// cases/ directory of test case files, auto-discovered recursively.
// Results can be written as text, JUnit, or JSON (output.go). When a
// state file is configured (state.go), results are persisted across runs
// to support incremental execution (skip unchanged cases) and rolling
// pass-rate history per case/model.
//
// See docs/specs/policy-test-suite.md for the suite and test case format.
package testsuite
