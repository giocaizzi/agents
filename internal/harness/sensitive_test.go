package harness

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

// TestSensitiveValues: a user-level target that holds an expanded value
// carries it in Sensitive; one that holds none carries nothing.
func TestSensitiveValues(t *testing.T) {
	t.Parallel()
	var c config.Config
	c.MCP.Servers = map[string]config.MCPServer{
		"docs": {URL: "https://docs.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${TOKEN}"}},
	}
	// renderFor's environment has TOKEN=tok.
	plan := renderFor(t, c, "linux", config.Claude, config.Pi)
	for _, tg := range plan.Targets {
		var want []string
		switch filepath.Base(tg.Path) {
		case ".claude.json", "mcp.json":
			want = []string{"tok"}
		}
		if diff := cmp.Diff(want, tg.Sensitive); diff != "" {
			t.Errorf("%s Sensitive (-want +got):\n%s", tg.Path, diff)
		}
	}
}

// TestSensitiveSkipsPaths: a variable expanded inside a path is not a secret.
func TestSensitiveSkipsPaths(t *testing.T) {
	t.Parallel()
	var c config.Config
	c.MCP.Workspaces = map[string]config.Workspace{
		"w": {Path: "${WSDIR}/ws", Servers: map[string]config.MCPServer{"s": {URL: "https://s.example.com/mcp"}}},
	}
	// renderFor's environment has WSDIR=wsroot.
	plan := renderFor(t, c, "linux", config.Claude, config.Pi)
	for _, tg := range plan.Targets {
		if len(tg.Sensitive) > 0 {
			t.Errorf("%s Sensitive = %v, want none", tg.Path, tg.Sensitive)
		}
	}
}
