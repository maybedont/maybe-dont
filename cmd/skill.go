package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/maybedont/maybe-dont/internal/skills"
	"github.com/spf13/cobra"
)

var skillFormat string

var skillCmd = &cobra.Command{
	Use:   "skill <command>",
	Short: "Manage embedded skill definitions for AI agents",
	Long: `Manage embedded skill definitions that instruct AI agents on how to use
the Maybe Don't CLI proxy.

Skills are markdown files embedded in the binary that can be deployed to
your project or user configuration for various AI coding assistants.`,
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available skills",
	Long:  `List all embedded skill definitions available in this build.`,
	Run:   runSkillList,
}

var skillViewCmd = &cobra.Command{
	Use:   "view <name> --format <format>",
	Short: "Output a skill definition to stdout",
	Long: `Output the specified skill definition to stdout.

Supported formats:
  claude   - Claude Code skill format
  cursor   - Cursor .cursorrules format
  copilot  - GitHub Copilot instructions format
  generic  - Generic markdown for any AI agent

Examples:
  maybe-dont skill view cli --format claude > .claude/skills/maybe-dont-cli.md
  maybe-dont skill view cli --format cursor > .cursorrules
  maybe-dont skill view cli --format copilot > .github/copilot-instructions.md
  maybe-dont skill view cli --format generic > instructions.md`,
	Args: cobra.ExactArgs(1),
	Run:  runSkillView,
}

func init() {
	skillViewCmd.Flags().StringVar(&skillFormat, "format", "",
		"Output format (required): claude, cursor, copilot, generic")
	_ = skillViewCmd.MarkFlagRequired("format")

	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillViewCmd)
	rootCmd.AddCommand(skillCmd)
}

func runSkillList(cmd *cobra.Command, args []string) {
	allSkills := skills.Skills()

	fmt.Println("Available skills:")
	for _, s := range allSkills {
		fmt.Printf("  %-8s %s\n", s.Name, s.Description)
	}
	fmt.Println()
	fmt.Printf("Supported formats: %s\n", strings.Join(skills.AvailableFormats(), ", "))
	fmt.Println()
	fmt.Println("Use 'maybe-dont skill view <name> --format <format>' to output a skill definition.")
}

func runSkillView(cmd *cobra.Command, args []string) {
	name := args[0]
	skill := skills.GetSkill(name)

	if skill == nil {
		fmt.Fprintf(os.Stderr, "Error: skill '%s' not found\n", name)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Available skills:")
		for _, s := range skills.Skills() {
			fmt.Fprintf(os.Stderr, "  %s\n", s.Name)
		}
		os.Exit(1)
	}

	content, ok := skill.Formats[skillFormat]
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: format '%s' not supported\n", skillFormat)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Available formats:")
		for _, f := range skills.AvailableFormats() {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		os.Exit(1)
	}

	// Output skill content to stdout for piping to files
	fmt.Print(content)
}
