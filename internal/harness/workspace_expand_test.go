package harness

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/config"
)

// TestWorkspaceExpandEnv: expand_env writes the expanded values for every
// harness, keeps the files private, records them as sensitive and does not
// warn about skipped servers.
func TestWorkspaceExpandEnv(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cfg := &config.Config{MCP: config.MCP{Workspaces: map[string]config.Workspace{
		"ops": {Path: "~/ops", ExpandEnv: true, Servers: map[string]config.MCPServer{
			"docs": {URL: "https://docs.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${DOCS_TOKEN}"}},
		}},
	}}}
	env := Env{Home: home, Getenv: func(k string) string {
		if k == "DOCS_TOKEN" {
			return "s3cret"
		}
		return ""
	}}
	b := &builder{cfg: cfg, env: env, seen: map[string]bool{}}
	all := Selected([]config.Harness{config.Claude, config.Copilot, config.VSCode, config.Codex, config.OpenCode, config.Pi, config.Antigravity})
	if err := renderWorkspaces(cfg, b, all); err != nil {
		t.Fatal(err)
	}
	if len(b.warnings) != 0 {
		t.Errorf("warnings = %q, want none", b.warnings)
	}
	owners := map[string]bool{}
	for _, tg := range b.targets {
		text := fmt.Sprint(tg.Managed)
		if !strings.Contains(text, "Bearer s3cret") {
			t.Errorf("%s (%s) does not hold the expanded value: %v", tg.Path, tg.Owner, tg.Managed)
		}
		if strings.Contains(text, "${") || strings.Contains(text, "{env:") {
			t.Errorf("%s still holds a reference", tg.Path)
		}
		if tg.Mode != 0o600 {
			t.Errorf("%s mode = %o, want 600", tg.Path, tg.Mode)
		}
		if !slices.Contains(tg.Sensitive, "s3cret") {
			t.Errorf("%s Sensitive = %q, want the expanded value", tg.Path, tg.Sensitive)
		}
		owners[tg.Owner] = true
	}
	for _, h := range []string{"claude", "codex", "opencode", "pi", "antigravity"} {
		if !owners[h] {
			t.Errorf("no workspace file for %s", h)
		}
	}
}
