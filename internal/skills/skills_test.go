package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLISkillEmbedded(t *testing.T) {
	// Verify the CLI skill is embedded and non-empty
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
	assert.NotEmpty(t, cli.Content)
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

	// Verify key sections are present
	assert.True(t, strings.Contains(skill.Content, "## Instructions"), "Should have Instructions section")
	assert.True(t, strings.Contains(skill.Content, "## Description"), "Should have Description section")
	assert.True(t, strings.Contains(skill.Content, "MAYBE_DONT_CLIENT_ID"), "Should mention client ID")
}
