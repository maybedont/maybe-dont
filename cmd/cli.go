package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/maybedont/maybe-dont/internal/cliproxy"
	"github.com/spf13/cobra"
)

// CLI command flags
var (
	cliServer  string
	cliTimeout time.Duration
	cliDryRun  bool
)

// Default values for CLI command
const (
	defaultCLIServer  = "http://localhost:8080"
	defaultCLITimeout = 30 * time.Second
)

var cliCmd = &cobra.Command{
	Use:   "cli [flags] -- <command> [args...]",
	Short: "Validate and execute CLI commands through the gateway",
	Long: `Routes CLI commands through the Maybe Don't gateway for validation.

The -- separator is REQUIRED to separate cli flags from the command to execute.
Commands are validated against the gateway's policy rules before execution.

Fail-open behavior: If the gateway is unreachable, the command will execute
with a warning. This ensures commands don't fail when the gateway is down.

Examples:
  maybe-dont cli -- gh pr comment 123 --body "LGTM"
  maybe-dont cli --server https://gateway:8443 -- aws s3 ls
  maybe-dont cli --dry-run -- kubectl delete pod my-pod

Environment Variables:
  MAYBE_DONT_CLIENT_ID    Client identifier sent with validation requests`,
	// Skip parent's PersistentPreRunE - this command doesn't need full gateway config loaded
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
	RunE: runCLI,
}

func init() {
	cliCmd.Flags().StringVarP(&cliServer, "server", "s", defaultCLIServer,
		"Gateway base URL")
	cliCmd.Flags().DurationVar(&cliTimeout, "timeout", defaultCLITimeout,
		"Validation request timeout")
	cliCmd.Flags().BoolVar(&cliDryRun, "dry-run", false,
		"Validate only, don't execute the command")

	rootCmd.AddCommand(cliCmd)
}

// runCLI validates and optionally executes the command after the -- separator.
func runCLI(cmd *cobra.Command, args []string) error {
	// Find the command and arguments after the -- separator
	command, cmdArgs, err := parseCommandFromArgs()
	if err != nil {
		return err
	}

	// Get working directory
	workingDir, err := os.Getwd()
	if err != nil {
		// Non-fatal - continue with empty working directory
		workingDir = ""
	}

	// Collect client info for audit attribution
	clientInfo := cliproxy.CollectClientInfo(version)

	// Get client ID from environment variable
	clientID := os.Getenv("MAYBE_DONT_CLIENT_ID")

	// Create cliproxy client
	client := cliproxy.NewClient(cliproxy.ClientConfig{
		ServerURL: cliServer,
		Timeout:   cliTimeout,
		ClientID:  clientID,
	})

	// Build validation request
	req := cliproxy.ValidationRequest{
		Command:          command,
		Arguments:        cmdArgs,
		WorkingDirectory: workingDir,
		ClientInfo:       clientInfo,
	}

	// Validate command with gateway
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	resp, err := client.Validate(ctx, req)
	if err != nil {
		// Fail-open: allow command if gateway is unreachable
		fmt.Fprintf(os.Stderr, "Warning: Gateway validation failed (%v), proceeding with command\n", err)
		if cliDryRun {
			fmt.Println("Dry-run: command would execute (gateway unreachable)")
			return nil
		}
		return cliproxy.ExecuteCommand(command, cmdArgs, nil)
	}

	// Handle validation response
	if !resp.Allowed {
		// Policy denied the command - print error and exit
		fmt.Fprintf(os.Stderr, "Error: Command denied by policy\n")
		fmt.Fprintf(os.Stderr, "  Request ID: %s\n", resp.RequestID)
		fmt.Fprintf(os.Stderr, "  Reason: %s\n", resp.Message)

		// Print individual policy results if available
		for _, result := range resp.Results {
			if result.Action == "deny" {
				fmt.Fprintf(os.Stderr, "  Policy: %s (%s) - %s\n",
					result.PolicyName, result.PolicyType, result.Message)
			}
		}

		os.Exit(1)
	}

	// Validation passed
	if cliDryRun {
		fmt.Println("Dry-run: validation passed")
		fmt.Printf("  Request ID: %s\n", resp.RequestID)
		fmt.Printf("  Message: %s\n", resp.Message)
		if resp.ValidationRequired {
			fmt.Printf("  Policies evaluated: %d\n", len(resp.Results))
		} else {
			fmt.Printf("  Validation: not required (command not in validate_commands list)\n")
		}
		return nil
	}

	// Execute the validated command - this replaces the current process
	return cliproxy.ExecuteCommand(command, cmdArgs, nil)
}

// parseCommandFromArgs finds the -- separator in os.Args and returns the command
// and arguments that follow it.
func parseCommandFromArgs() (string, []string, error) {
	// Find the -- separator in os.Args
	separatorIdx := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separatorIdx = i
			break
		}
	}

	if separatorIdx == -1 {
		return "", nil, fmt.Errorf("missing -- separator: usage: maybe-dont cli [flags] -- <command> [args...]")
	}

	// Get everything after the separator
	afterSeparator := os.Args[separatorIdx+1:]
	if len(afterSeparator) == 0 {
		return "", nil, fmt.Errorf("no command specified after --: usage: maybe-dont cli [flags] -- <command> [args...]")
	}

	command := afterSeparator[0]
	var cmdArgs []string
	if len(afterSeparator) > 1 {
		cmdArgs = afterSeparator[1:]
	}

	return command, cmdArgs, nil
}
