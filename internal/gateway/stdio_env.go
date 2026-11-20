package gateway

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"sort"

	"github.com/mark3labs/mcp-go/client/transport"
)

// inheritedStdioEnv lists the gateway environment variables a stdio server
// inherits: enough to find and run a program (npx, uvx, docker) and nothing
// that usually holds secrets. It matches the default of the official MCP
// TypeScript SDK, so configs written for Claude Desktop behave the same.
// Anything else a server needs goes in its env config, where ${VAR} passes a
// gateway variable through explicitly.
func inheritedStdioEnv() []string {
	if runtime.GOOS == "windows" {
		return []string{
			"APPDATA", "HOMEDRIVE", "HOMEPATH", "LOCALAPPDATA", "PATH",
			"PROCESSOR_ARCHITECTURE", "SYSTEMDRIVE", "SYSTEMROOT", "TEMP",
			"USERNAME", "USERPROFILE", "PROGRAMFILES",
		}
	}
	return []string{"HOME", "LOGNAME", "PATH", "SHELL", "TERM", "USER"}
}

// stdioEnv builds a stdio server's environment: the inherited basics that are
// set in the gateway's environment, overridden by the server's configured env.
func stdioEnv(configured map[string]string) []string {
	merged := make(map[string]string, len(configured)+len(inheritedStdioEnv()))
	for _, name := range inheritedStdioEnv() {
		if value, ok := os.LookupEnv(name); ok {
			merged[name] = value
		}
	}
	for name, value := range configured {
		merged[name] = value
	}

	env := make([]string, 0, len(merged))
	for name, value := range merged {
		env = append(env, name+"="+value)
	}
	sort.Strings(env)
	return env
}

// stdioCommandFunc spawns stdio servers with stdioEnv instead of mcp-go's
// default, which appends to the gateway's entire environment and would hand
// every downstream server the gateway's API keys and credentials.
func stdioCommandFunc(configured map[string]string) transport.CommandFunc {
	return func(ctx context.Context, command string, _ []string, args []string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Env = stdioEnv(configured)
		return cmd, nil
	}
}
