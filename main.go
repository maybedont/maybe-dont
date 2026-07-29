// Command maybe-dont is an AI security gateway that sits between an LLM
// agent and its MCP servers or shell, validating tool calls and CLI
// commands against configurable policies. See cmd for the subcommands.
package main

import "github.com/maybedont/maybe-dont/cmd"

var (
	// version is set during build
	version = "development"
	// commit is set during build
	commit = "n/a"
	// date is set during build
	date = "n/a"
)

func main() {
	cmd.Execute(version, commit, date)
}
