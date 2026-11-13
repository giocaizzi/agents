package harness

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/config"
)

// TestWorkspaceKeepsReferences: a workspace file is committed, so a ${VAR} is
// never expanded into it. It stays a reference where the harness expands it,
// and the server is skipped (warned) for a harness that expands none.
func TestWorkspaceKeepsReferences(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cfg := &config.Config{MCP: config.MCP{Workspaces: map[string]config.Workspace{
		"ops": {Path: "~/ops", Servers: map[string]config.MCPServer{
			"docs": {URL: "https://docs.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${DOCS_TOKEN}"}},
			"open": {URL: "https://open.example.com/mcp"},
		}},
	}}}
	env := Env{Home: home, Getenv: func(string) string { return "s3cret" }}
	b := &builder{cfg: cfg, env: env, seen: map[string]bool{}}
	selected := Selected([]config.Harness{config.Claude, config.Codex, config.OpenCode, config.Pi, config.Antigravity})
	if err := renderWorkspaces(cfg, b, selected); err != nil {
		t.Fatal(err)
	}

	servers := func(rel, key string) map[string]any {
		t.Helper()
		for _, tg := range b.targets {
			if r, _ := filepath.Rel(filepath.Join(home, "ops"), tg.Path); r == rel {
				m, _ := tg.Managed.(map[string]any)
				s, _ := m[key].(map[string]any)
				return s
			}
		}
		t.Fatalf("no target %s", rel)
		return nil
	}
	nested := func(rel string) map[string]any {
		s, _ := servers(rel, "mcp")["servers"].(map[string]any)
		return s
	}
	auth := func(servers map[string]any) any {
		docs, _ := servers["docs"].(map[string]any)
		headers, _ := docs["headers"].(map[string]any)
		return headers["Authorization"]
	}
	if got := auth(servers(".mcp.json", "mcpServers")); got != "Bearer ${DOCS_TOKEN}" {
		t.Errorf(".mcp.json Authorization = %v, want the reference kept", got)
	}
	if got := auth(nested("opencode.json")); got != "Bearer {env:DOCS_TOKEN}" {
		t.Errorf("opencode.json Authorization = %v, want {env:DOCS_TOKEN}", got)
	}
	for rel, key := range map[string]string{".codex/config.toml": "mcp_servers", ".pi/mcp.json": "mcpServers", ".agents/mcp_config.json": "mcpServers"} {
		got := servers(rel, key)
		if _, has := got["docs"]; has {
			t.Errorf("%s holds docs, which needs ${DOCS_TOKEN} expanded", rel)
		}
		if _, has := got["open"]; !has {
			t.Errorf("%s misses the server without a reference", rel)
		}
	}
	for _, tg := range b.targets {
		if strings.Contains(fmt.Sprint(tg.Managed), "s3cret") {
			t.Errorf("%s holds an expanded value", tg.Path)
		}
	}
	const want = "mcp.workspaces.ops.servers.docs: skipped for codex, which does not expand ${DOCS_TOKEN} in a project file"
	if !slices.Contains(b.warnings, want) {
		t.Errorf("warnings = %q, want %q", b.warnings, want)
	}
}
