// Package hooks provides embedded hook scripts for AI agent integrations.
package hooks

import (
	_ "embed"
)

// Embedded hook scripts and config snippets for each agent.
//
//go:embed claude-code.sh
var claudeCodeScript string

//go:embed claude-code.config.json
var claudeCodeConfig string

//go:embed cursor.sh
var cursorScript string

//go:embed cursor.config.json
var cursorConfig string

//go:embed gemini-cli.sh
var geminiCLIScript string

//go:embed gemini-cli.config.json
var geminiCLIConfig string

//go:embed cline.sh
var clineScript string

//go:embed cline.config.json
var clineConfig string

//go:embed copilot.sh
var copilotScript string

//go:embed copilot.config.json
var copilotConfig string

// Hook represents an embedded hook script with its configuration snippet.
type Hook struct {
	Name        string // Short name used for lookup (e.g., "claude-code")
	Description string // Brief description shown in hook list
	Script      string // Bash hook script content
	Config      string // Agent config snippet (JSON)
}

// Hooks returns all available embedded hooks.
func Hooks() []Hook {
	return []Hook{
		{
			Name:        "claude-code",
			Description: "Claude Code PreToolUse/PostToolUse hooks",
			Script:      claudeCodeScript,
			Config:      claudeCodeConfig,
		},
		{
			Name:        "cursor",
			Description: "Cursor shell and MCP execution hooks",
			Script:      cursorScript,
			Config:      cursorConfig,
		},
		{
			Name:        "gemini-cli",
			Description: "Gemini CLI BeforeTool/AfterTool hooks",
			Script:      geminiCLIScript,
			Config:      geminiCLIConfig,
		},
		{
			Name:        "cline",
			Description: "Cline PreToolUse/PostToolUse hooks",
			Script:      clineScript,
			Config:      clineConfig,
		},
		{
			Name:        "copilot",
			Description: "GitHub Copilot PreToolUse/PostToolUse hooks",
			Script:      copilotScript,
			Config:      copilotConfig,
		},
	}
}

// GetHook returns a hook by name, or nil if not found.
func GetHook(name string) *Hook {
	for _, h := range Hooks() {
		if h.Name == name {
			return &h
		}
	}
	return nil
}

// HookNames returns the names of all available hooks (for CLI tab completion).
func HookNames() []string {
	hooks := Hooks()
	names := make([]string, len(hooks))
	for i, h := range hooks {
		names[i] = h.Name
	}
	return names
}
