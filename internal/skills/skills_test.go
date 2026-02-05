package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLISkillEmbedded(t *testing.T) {
	// Verify the CLI skill is embedded and non-empty (backward compatibility)
	require.NotEmpty(t, CLISkill, "CLISkill should be embedded")
	assert.Contains(t, CLISkill, "maybe-dont cli", "Should contain command syntax")
	assert.Contains(t, CLISkill, "maybe-dont-cli", "Should contain skill name")
}

func TestSkills(t *testing.T) {
	skills := Skills()
	require.Len(t, skills, 1, "Should have 1 skill")

	cli := skills[0]
	assert.Equal(t, "cli", cli.Name)
	assert.NotEmpty(t, cli.Description)
	assert.NotEmpty(t, cli.Formats)
	assert.Len(t, cli.Formats, 4, "Should have 4 formats")
}

func TestGetSkill(t *testing.T) {
	tests := []struct {
		name      string
		skillName string
		wantNil   bool
	}{
		{"cli skill exists", "cli", false},
		{"unknown skill", "unknown", true},
		{"empty name", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			skill := GetSkill(tc.skillName)
			if tc.wantNil {
				assert.Nil(t, skill)
			} else {
				require.NotNil(t, skill)
				assert.Equal(t, tc.skillName, skill.Name)
			}
		})
	}
}

func TestCLISkillContent(t *testing.T) {
	skill := GetSkill("cli")
	require.NotNil(t, skill)

	// Verify key sections are present in default (Claude) format
	content := skill.Content()
	assert.True(t, strings.Contains(content, "## Instructions"), "Should have Instructions section")
	assert.True(t, strings.Contains(content, "## Description"), "Should have Description section")
	assert.True(t, strings.Contains(content, "MAYBE_DONT_CLIENT_ID"), "Should mention client ID")
}

func TestSkillFormats(t *testing.T) {
	skill := GetSkill("cli")
	require.NotNil(t, skill)

	// Each format should be present and contain the core command syntax
	formats := []string{FormatClaude, FormatCursor, FormatCopilot, FormatGeneric}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			content, ok := skill.Formats[format]
			require.True(t, ok, "Format %s should exist", format)
			require.NotEmpty(t, content, "Format %s should have content", format)
			assert.Contains(t, content, "maybe-dont cli", "Format %s should contain command syntax", format)
			assert.Contains(t, content, "MAYBE_DONT_CLIENT_ID", "Format %s should mention client ID", format)
		})
	}
}

func TestSkillContentMethod(t *testing.T) {
	// Verify Content() returns the default (Claude) format for backward compatibility
	skill := GetSkill("cli")
	require.NotNil(t, skill)

	content := skill.Content()
	claudeContent := skill.Formats[FormatClaude]
	assert.Equal(t, claudeContent, content, "Content() should return Claude format")
}

func TestAvailableFormats(t *testing.T) {
	formats := AvailableFormats()
	require.Len(t, formats, 4, "Should have 4 available formats")

	expected := []string{FormatClaude, FormatCursor, FormatCopilot, FormatGeneric}
	assert.Equal(t, expected, formats, "Formats should be in expected order")
}

func TestFormatConstants(t *testing.T) {
	// Verify format constants have expected values
	assert.Equal(t, "claude", FormatClaude)
	assert.Equal(t, "cursor", FormatCursor)
	assert.Equal(t, "copilot", FormatCopilot)
	assert.Equal(t, "generic", FormatGeneric)
	assert.Equal(t, FormatClaude, DefaultFormat, "Default format should be Claude")
}

func TestCursorFormat(t *testing.T) {
	skill := GetSkill("cli")
	require.NotNil(t, skill)

	content := skill.Formats[FormatCursor]
	assert.Contains(t, content, "# Maybe Don't CLI Proxy Rules", "Cursor format should have rules header")
	assert.Contains(t, content, "## Rules", "Cursor format should have Rules section")
	assert.Contains(t, content, "--dry-run", "Cursor format should mention dry-run")
}

func TestCopilotFormat(t *testing.T) {
	skill := GetSkill("cli")
	require.NotNil(t, skill)

	content := skill.Formats[FormatCopilot]
	assert.Contains(t, content, "# Maybe Don't CLI Proxy Instructions", "Copilot format should have instructions header")
	assert.Contains(t, content, "## Overview", "Copilot format should have Overview section")
	assert.Contains(t, content, "## Handling Denials", "Copilot format should have Handling Denials section")
}

func TestGenericFormat(t *testing.T) {
	skill := GetSkill("cli")
	require.NotNil(t, skill)

	content := skill.Formats[FormatGeneric]
	assert.Contains(t, content, "# CLI Command Validation Instructions", "Generic format should have clean header")
	assert.Contains(t, content, "## Purpose", "Generic format should have Purpose section")
	assert.Contains(t, content, "## Behavior Guidelines", "Generic format should have Behavior Guidelines section")
}
