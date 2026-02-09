package gateway

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSanitizeJSONEscapes verifies that invalid JSON escape sequences are fixed
// while valid escapes and non-string content are left untouched.
func TestSanitizeJSONEscapes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no backslashes passes through unchanged",
			input:    `{"allowed":true,"message":"all good"}`,
			expected: `{"allowed":true,"message":"all good"}`,
		},
		{
			name:     "valid escapes preserved",
			input:    `{"message":"line1\nline2\ttab"}`,
			expected: `{"message":"line1\nline2\ttab"}`,
		},
		{
			name:     "escaped quote preserved",
			input:    `{"message":"said \"hello\""}`,
			expected: `{"message":"said \"hello\""}`,
		},
		{
			name:     "escaped backslash preserved",
			input:    `{"message":"path: C:\\Windows"}`,
			expected: `{"message":"path: C:\\Windows"}`,
		},
		{
			name:     "invalid \\W escape fixed",
			input:    `{"message":"path C:\Windows\System32"}`,
			expected: `{"message":"path C:\\Windows\\System32"}`,
		},
		{
			name:     "invalid \\P escape fixed",
			input:    `{"message":"C:\Program Files\app"}`,
			expected: `{"message":"C:\\Program Files\\app"}`,
		},
		{
			name:     "mixed valid and invalid escapes",
			input:    `{"message":"newline\n then C:\Windows\zoo"}`,
			expected: `{"message":"newline\n then C:\\Windows\\zoo"}`,
		},
		{
			name:     "unicode escape preserved",
			input:    `{"message":"\u0048ello"}`,
			expected: `{"message":"\u0048ello"}`,
		},
		{
			name:     "backslashes outside strings untouched",
			input:    `{"key":"value"}`,
			expected: `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeJSONEscapes([]byte(tt.input))
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

// TestSanitizeJSONEscapes_ProducesValidJSON verifies that sanitized output
// containing Windows paths can be successfully parsed as JSON.
func TestSanitizeJSONEscapes_ProducesValidJSON(t *testing.T) {
	// This is the kind of invalid JSON an AI model might produce when
	// echoing Windows paths from a policy prompt.
	input := `{"allowed":false,"message":"DENIED: path C:\Windows\System32 is a dangerous system directory"}`

	// Raw input should fail to parse
	var raw map[string]any
	err := json.Unmarshal([]byte(input), &raw)
	require.Error(t, err, "raw input with invalid escapes should fail to parse")

	// Sanitized input should parse successfully
	sanitized := SanitizeJSONEscapes([]byte(input))
	var parsed map[string]any
	err = json.Unmarshal(sanitized, &parsed)
	require.NoError(t, err, "sanitized input should parse as valid JSON")
	assert.Equal(t, false, parsed["allowed"])
	assert.Contains(t, parsed["message"], "Windows")
}
