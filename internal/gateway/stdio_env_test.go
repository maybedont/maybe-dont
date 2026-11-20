package gateway

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStdioCommandFunc_Environment verifies that a spawned stdio server gets
// the inherited basics plus its configured env, and none of the gateway's
// other environment variables, by running a real process that prints its
// environment.
func TestStdioCommandFunc_Environment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /usr/bin/env")
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/home/gateway")
	t.Setenv("OPENAI_API_KEY", "gateway-secret")
	t.Setenv("GITHUB_TOKEN", "gateway-github-token")

	tests := []struct {
		name       string
		configured map[string]string
		want       []string
		wantAbsent []string
	}{
		{
			name:       "no configured env inherits only the basics",
			want:       []string{"PATH=/usr/bin:/bin", "HOME=/home/gateway"},
			wantAbsent: []string{"OPENAI_API_KEY", "GITHUB_TOKEN"},
		},
		{
			name:       "configured env is added with names as written",
			configured: map[string]string{"GITHUB_TOKEN": "server-token", "http_proxy": "http://proxy:3128"},
			want:       []string{"PATH=/usr/bin:/bin", "GITHUB_TOKEN=server-token", "http_proxy=http://proxy:3128"},
			wantAbsent: []string{"OPENAI_API_KEY", "gateway-github-token"},
		},
		{
			name:       "configured env overrides an inherited basic",
			configured: map[string]string{"HOME": "/srv/mcp"},
			want:       []string{"HOME=/srv/mcp"},
			wantAbsent: []string{"HOME=/home/gateway"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := stdioCommandFunc(tt.configured)(context.Background(), "/usr/bin/env", nil, nil)
			require.NoError(t, err)
			out, err := cmd.Output()
			require.NoError(t, err)
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")

			for _, want := range tt.want {
				require.Contains(t, lines, want)
			}
			for _, absent := range tt.wantAbsent {
				require.NotContains(t, string(out), absent)
			}
		})
	}
}
