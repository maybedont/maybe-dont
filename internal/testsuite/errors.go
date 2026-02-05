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

// PolicyIntegrityError indicates a test case references a policy that doesn't exist.
type PolicyIntegrityError struct {
	CaseID     string
	PolicyName string
	Message    string
}

func (e *PolicyIntegrityError) Error() string {
	return fmt.Sprintf("policy integrity error in case %q: %s", e.CaseID, e.Message)
}

// PathResolutionError indicates a configured path could not be resolved.
type PathResolutionError struct {
	Path    string
	Message string
}

func (e *PathResolutionError) Error() string {
	return fmt.Sprintf("path resolution error for %q: %s", e.Path, e.Message)
}
