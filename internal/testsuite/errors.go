// Package testsuite provides a test harness for validating CEL and AI policies.
package testsuite

import "fmt"

// SchemaValidationError indicates a suite or test case schema validation failure.
type SchemaValidationError struct {
	Message string
	Details []string
}

func (e *SchemaValidationError) Error() string {
	if len(e.Details) == 0 {
		return fmt.Sprintf("schema validation error: %s", e.Message)
	}
	return fmt.Sprintf("schema validation error: %s\n%v", e.Message, e.Details)
}

// PolicyIntegrityError indicates test cases reference policies that don't exist.
type PolicyIntegrityError struct {
	Message string
	Details string
}

func (e *PolicyIntegrityError) Error() string {
	if e.Details == "" {
		return fmt.Sprintf("policy integrity error: %s", e.Message)
	}
	return fmt.Sprintf("policy integrity error: %s\n\n%s", e.Message, e.Details)
}

// PathResolutionError indicates a configured path could not be resolved.
type PathResolutionError struct {
	Path    string
	Message string
}

func (e *PathResolutionError) Error() string {
	return fmt.Sprintf("path resolution error for %q: %s", e.Path, e.Message)
}
