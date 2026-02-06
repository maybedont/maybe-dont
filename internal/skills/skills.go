// Package skills provides embedded skill definitions for AI agents.
package skills

import (
	_ "embed"
)

// Embedded skill content for each format
//
//go:embed cli.md
var cliSkillClaude string

//go:embed cli.cursorrules
var cliSkillCursor string

//go:embed cli.copilot.md
var cliSkillCopilot string

//go:embed cli.generic.md
var cliSkillGeneric string

// CLISkill is kept for backward compatibility.
// New code should use GetSkill("cli") with the desired format.
var CLISkill = cliSkillClaude

// Format constants for skill output
const (
	FormatClaude  = "claude"
	FormatCursor  = "cursor"
	FormatCopilot = "copilot"
	FormatGeneric = "generic"
)

// DefaultFormat is the default output format
const DefaultFormat = FormatClaude

// Skill represents an embedded skill definition with multiple format options.
type Skill struct {
	Name        string            // Short name used for lookup (e.g., "cli")
	Description string            // Brief description shown in skill list
	Formats     map[string]string // format name -> content
}

// Content returns the skill content in the default (Claude) format.
// This maintains backward compatibility with code expecting a single Content field.
func (s *Skill) Content() string {
	return s.Formats[DefaultFormat]
}

// Skills returns all available embedded skills.
func Skills() []Skill {
	return []Skill{
		{
			Name:        "cli",
			Description: "CLI command validation skill for AI agents",
			Formats: map[string]string{
				FormatClaude:  cliSkillClaude,
				FormatCursor:  cliSkillCursor,
				FormatCopilot: cliSkillCopilot,
				FormatGeneric: cliSkillGeneric,
			},
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

// AvailableFormats returns the list of supported format names.
func AvailableFormats() []string {
	return []string{FormatClaude, FormatCursor, FormatCopilot, FormatGeneric}
}
