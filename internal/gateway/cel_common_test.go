package gateway

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

// TestBuildToolVariable verifies that the CEL "tool" variable carries the
// caller's prefixed tool name and arguments unchanged, and that missing
// arguments (untyped or typed nil) become an empty, non-nil map.
func TestBuildToolVariable(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		arguments     any
		wantArguments any
	}{
		{
			name:          "map arguments pass through",
			toolName:      "github__delete_file",
			arguments:     map[string]any{"path": "README.md"},
			wantArguments: map[string]any{"path": "README.md"},
		},
		{
			name:          "nil arguments become empty map",
			toolName:      "github__list_issues",
			arguments:     nil,
			wantArguments: map[string]any{},
		},
		{
			name:          "typed nil map becomes empty map",
			toolName:      "github__list_issues",
			arguments:     map[string]any(nil),
			wantArguments: map[string]any{},
		},
		{
			name:          "empty map stays empty",
			toolName:      "aws-docs__search_documentation",
			arguments:     map[string]any{},
			wantArguments: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tt.toolName, Arguments: tt.arguments}}

			got := buildToolVariable(req)

			assert.Equal(t, tt.toolName, got["name"])
			assert.Equal(t, tt.wantArguments, got["arguments"])
			assert.NotNil(t, got["arguments"])
		})
	}
}
