// Package skills provides embedded skill definitions for AI agents.
package skills

import (
	_ "embed"
)

// CLISkill contains the Claude Code skill definition for CLI proxy usage.
// This skill instructs AI agents how to route commands through the gateway.
//
//go:embed cli.md
var CLISkill string

// Skill represents an embedded skill definition.
type Skill struct {
	Name        string // Short name used for lookup (e.g., "cli")
	Description string // Brief description shown in skill list
	Content     string // Full skill content (markdown)
}

// Skills returns all available embedded skills.
func Skills() []Skill {
	return []Skill{
		{
			Name:        "cli",
			Description: "Claude Code skill for CLI command validation",
			Content:     CLISkill,
		},
	}
}

// GetSkill returns a skill by name, or nil if not found.
func GetSkill(name string) *Skill {
	for _, s := range Skills() {
		if s.Name == name {
			return &s
		}
	}
	return nil
}
