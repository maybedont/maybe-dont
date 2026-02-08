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
	require.Len(t, skills, 4, "Should have 4 skills")

	for _, skill := range skills {
		t.Run(skill.Name, func(t *testing.T) {
			assert.NotEmpty(t, skill.Name)
			assert.NotEmpty(t, skill.Description)
			assert.NotEmpty(t, skill.Formats)
			assert.Len(t, skill.Formats, 4, "Should have 4 formats")
		})
	}
}

func TestGetSkill(t *testing.T) {
	tests := []struct {
		name      string
		skillName string
		wantNil   bool
	}{
		{"cli skill exists", "cli", false},
		{"cel-policy skill exists", "cel-policy", false},
		{"ai-policy skill exists", "ai-policy", false},
		{"test-case skill exists", "test-case", false},
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
	tests := []struct {
		name           string
		skillName      string
		wantHeader     string
		wantSection    string
		wantKeyContent string
	}{
		{"cli", "cli", "# Maybe Don't CLI Proxy Rules", "## Rules", "--dry-run"},
		{"cel-policy", "cel-policy", "# CEL Policy Authoring Rules", "## Rules", "mcp_expression"},
		{"ai-policy", "ai-policy", "# AI Policy Authoring Rules", "## Writing Effective Prompts", "redacted_content"},
		{"test-case", "test-case", "# Policy Test Case Authoring Rules", "## Rules", "case_id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			skill := GetSkill(tc.skillName)
			require.NotNil(t, skill)
			content := skill.Formats[FormatCursor]
			assert.Contains(t, content, tc.wantHeader, "Cursor format should have rules header")
			assert.Contains(t, content, tc.wantSection, "Cursor format should have Rules section")
			assert.Contains(t, content, tc.wantKeyContent, "Cursor format should contain key content")
		})
	}
}

func TestCopilotFormat(t *testing.T) {
	tests := []struct {
		name           string
		skillName      string
		wantHeader     string
		wantOverview   bool
		wantKeyContent string
	}{
		{"cli", "cli", "# Maybe Don't CLI Proxy Instructions", true, "## Handling Denials"},
		{"cel-policy", "cel-policy", "# CEL Policy Authoring Instructions", true, "mcp_expression"},
		{"ai-policy", "ai-policy", "# AI Policy Authoring Instructions", true, "redacted_content"},
		{"test-case", "test-case", "# Policy Test Case Authoring Instructions", true, "case_id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			skill := GetSkill(tc.skillName)
			require.NotNil(t, skill)
			content := skill.Formats[FormatCopilot]
			assert.Contains(t, content, tc.wantHeader, "Copilot format should have instructions header")
			if tc.wantOverview {
				assert.Contains(t, content, "## Overview", "Copilot format should have Overview section")
			}
			assert.Contains(t, content, tc.wantKeyContent, "Copilot format should contain key content")
		})
	}
}

func TestGenericFormat(t *testing.T) {
	tests := []struct {
		name           string
		skillName      string
		wantHeader     string
		wantKeyContent string
	}{
		{"cli", "cli", "# CLI Command Validation Instructions", "## Behavior Guidelines"},
		{"cel-policy", "cel-policy", "# CEL Policy Authoring Instructions", "## Behavior Guidelines"},
		{"ai-policy", "ai-policy", "# AI Policy Authoring Instructions", "## Writing Effective Prompts"},
		{"test-case", "test-case", "# Policy Test Case Authoring Instructions", "## Behavior Guidelines"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			skill := GetSkill(tc.skillName)
			require.NotNil(t, skill)
			content := skill.Formats[FormatGeneric]
			assert.Contains(t, content, tc.wantHeader, "Generic format should have clean header")
			assert.Contains(t, content, "## Purpose", "Generic format should have Purpose section")
			assert.Contains(t, content, tc.wantKeyContent, "Generic format should contain key content")
		})
	}
}
