package cmd

import (
	"fmt"
	"os"

	"github.com/maybedont/maybe-dont/internal/hooks"
	"github.com/spf13/cobra"
)

var (
	hookAgent  string
	hookConfig bool
)

var hooksCmd = &cobra.Command{
	Use:   "hooks <command>",
	Short: "Manage embedded hook scripts for AI agent integrations",
	Long: `Manage embedded hook scripts that integrate AI agents with the Maybe Don't
gateway via the intercept endpoint.

Hook scripts are bash scripts that run as agent hooks (pre-tool / post-tool),
calling the gateway's POST /api/v1/intercept endpoint for policy decisions.`,
}

var hooksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available hook agents",
	Long:  `List all embedded hook scripts available in this build.`,
	Run:   runHooksList,
}

var hooksExportCmd = &cobra.Command{
	Use:   "export --agent <name> [--config]",
	Short: "Output a hook script or config snippet to stdout",
	Long: `Output the specified hook script or config snippet to stdout.

By default, outputs the bash hook script. Use --config to output
the agent's configuration snippet instead.

Examples:
  maybe-dont hooks export --agent claude-code > maybe-dont-hook.sh
  chmod +x maybe-dont-hook.sh
  maybe-dont hooks export --agent claude-code --config
  maybe-dont hooks export --agent cursor --config`,
	Run: runHooksExport,
}

func init() {
	hooksExportCmd.Flags().StringVar(&hookAgent, "agent", "",
		"Agent name (required): claude-code, cursor, gemini-cli, cline, copilot")
	_ = hooksExportCmd.MarkFlagRequired("agent")

	hooksExportCmd.Flags().BoolVar(&hookConfig, "config", false,
		"Output the config snippet instead of the hook script")

	hooksCmd.AddCommand(hooksListCmd)
	hooksCmd.AddCommand(hooksExportCmd)
	rootCmd.AddCommand(hooksCmd)
}

func runHooksList(cmd *cobra.Command, args []string) {
	allHooks := hooks.Hooks()

	fmt.Println("Available hook agents:")
	for _, h := range allHooks {
		fmt.Printf("  %-12s %s\n", h.Name, h.Description)
	}
	fmt.Println()
	fmt.Println("Use 'maybe-dont hooks export --agent <name>' to output a hook script.")
	fmt.Println("Use 'maybe-dont hooks export --agent <name> --config' to output a config snippet.")
}

func runHooksExport(cmd *cobra.Command, args []string) {
	hook := hooks.GetHook(hookAgent)

	if hook == nil {
		fmt.Fprintf(os.Stderr, "Error: agent '%s' not found\n", hookAgent)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Available agents:")
		for _, h := range hooks.Hooks() {
			fmt.Fprintf(os.Stderr, "  %s\n", h.Name)
		}
		os.Exit(1)
	}

	if hookConfig {
		fmt.Print(hook.Config)
	} else {
		fmt.Print(hook.Script)
	}
}
