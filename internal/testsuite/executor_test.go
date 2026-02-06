package testsuite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareResults(t *testing.T) {
	tests := []struct {
		name            string
		expected        ExpectedResult
		actual          ActualResult
		expectedFailures int
		wantContains    []string
	}{
		{
			name: "decisions match - allow",
			expected: ExpectedResult{
				Decision: "allow",
			},
			actual: ActualResult{
				Decision: "allow",
			},
			expectedFailures: 0,
		},
		{
			name: "decisions match - deny",
			expected: ExpectedResult{
				Decision: "deny",
			},
			actual: ActualResult{
				Decision: "deny",
			},
			expectedFailures: 0,
		},
		{
			name: "decision mismatch",
			expected: ExpectedResult{
				Decision: "deny",
			},
			actual: ActualResult{
				Decision: "allow",
			},
			expectedFailures: 1,
			wantContains:    []string{"expected decision \"deny\" but actual \"allow\""},
		},
		{
			name: "policy decision match",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-dangerous", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-dangerous", Decision: "deny"},
				},
			},
			expectedFailures: 0,
		},
		{
			name: "policy decision mismatch",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "block-dangerous", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "block-dangerous", Decision: "allow"},
				},
			},
			expectedFailures: 1,
			wantContains:    []string{"expected policy \"block-dangerous\" to return \"deny\" but actual \"allow\""},
		},
		{
			name: "expected policy not executed",
			expected: ExpectedResult{
				Decision: "deny",
				Policies: []PolicyExpectation{
					{PolicyName: "missing-policy", Decision: "deny"},
				},
			},
			actual: ActualResult{
				Decision: "deny",
				PoliciesExecuted: []PolicyResult{
					{PolicyName: "other-policy", Decision: "deny"},
				},
			},
			expectedFailures: 1,
			wantContains:    []string{"expected policy \"missing-policy\" to execute but it did not"},
		},
		{
			name: "redacted content matches",
			expected: ExpectedResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: [REDACTED]",
			},
			actual: ActualResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: [REDACTED]",
			},
			expectedFailures: 0,
		},
		{
			name: "redacted content mismatch",
			expected: ExpectedResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: [REDACTED]",
			},
			actual: ActualResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: 123-45-6789",
			},
			expectedFailures: 1,
			wantContains:    []string{"redacted content mismatch"},
		},
		{
			name: "expected redacted content but none returned",
			expected: ExpectedResult{
				Decision:        "redact",
				RedactedContent: "User: John, SSN: [REDACTED]",
			},
			actual: ActualResult{
				Decision:        "redact",
				RedactedContent: "",
			},
			expectedFailures: 1,
			wantContains:    []string{"expected redacted content but none was returned"},
		},
		{
			name: "no expected redacted content - skip validation",
			expected: ExpectedResult{
				Decision:        "redact",
				RedactedContent: "", // Not specified
			},
			actual: ActualResult{
				Decision:        "redact",
				RedactedContent: "Some redacted content",
			},
			expectedFailures: 0, // No failure because redacted content wasn't specified in expectations
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures := compareResults(tt.expected, tt.actual)
			assert.Len(t, failures, tt.expectedFailures)

			for _, want := range tt.wantContains {
				found := false
				for _, f := range failures {
					if containsSubstring(f, want) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected failure message containing %q, got %v", want, failures)
			}
		})
	}
}

func TestExtractExpectedRedactedContent(t *testing.T) {
	tests := []struct {
		name     string
		items    []ContentItem
		expected string
	}{
		{
			name:     "empty items",
			items:    nil,
			expected: "",
		},
		{
			name:     "single text item",
			items:    []ContentItem{{Type: "text", Text: "redacted content"}},
			expected: "redacted content",
		},
		{
			name: "multiple text items",
			items: []ContentItem{
				{Type: "text", Text: "line 1"},
				{Type: "text", Text: "line 2"},
			},
			expected: "line 1\nline 2",
		},
		{
			name: "mixed types - only text extracted",
			items: []ContentItem{
				{Type: "text", Text: "text content"},
				{Type: "image", Text: "ignored"},
			},
			expected: "text content",
		},
		{
			name: "empty text items filtered",
			items: []ContentItem{
				{Type: "text", Text: ""},
				{Type: "text", Text: "actual content"},
			},
			expected: "actual content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractExpectedRedactedContent(tt.items)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// containsSubstring checks if s contains substr
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && contains(s, substr)))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
