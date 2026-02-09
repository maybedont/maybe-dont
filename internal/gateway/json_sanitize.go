package gateway

import (
	"bytes"
)

// SanitizeJSONEscapes fixes invalid JSON escape sequences in AI model output.
// AI models sometimes produce invalid JSON when their response text contains
// backslash-prefixed characters that aren't valid JSON escapes (e.g., C:\Windows\
// produces \W which is not a legal JSON escape). This function escapes those
// invalid sequences so the JSON can be parsed.
//
// Valid JSON escapes after '\' are: " \ / b f n r t u
// Anything else (e.g., \W, \S, \P) is replaced with the escaped form (\\W, \\S, \\P).
func SanitizeJSONEscapes(data []byte) []byte {
	// Fast path: if no backslashes, return as-is.
	if !bytes.ContainsRune(data, '\\') {
		return data
	}

	result := make([]byte, 0, len(data)+32)
	inString := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		// Track whether we're inside a JSON string value.
		// Only string values need escape sanitization.
		if c == '"' && (i == 0 || data[i-1] != '\\') {
			inString = !inString
			result = append(result, c)
			continue
		}

		if !inString {
			result = append(result, c)
			continue
		}

		// Inside a string: check for backslash escapes
		if c == '\\' && i+1 < len(data) {
			next := data[i+1]
			if isValidJSONEscape(next) {
				// Valid escape — pass through as-is
				result = append(result, c, next)
				i++ // skip the next char since we consumed it
			} else {
				// Invalid escape — double the backslash to make it literal
				result = append(result, '\\', '\\', next)
				i++ // skip the next char since we consumed it
			}
			continue
		}

		result = append(result, c)
	}

	return result
}

// isValidJSONEscape returns true if c is a valid character after '\' in JSON.
func isValidJSONEscape(c byte) bool {
	switch c {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	}
	return false
}
