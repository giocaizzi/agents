package harness

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/mcpsync"
)

func TestServerEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   config.MCPServer
		want func(e mcpsync.ServerEntry) bool
	}{
		{"url infers http", config.MCPServer{URL: "https://a.example.com"}, func(e mcpsync.ServerEntry) bool { return e.Type == "http" && e.OAuth == nil }},
		{"command infers stdio", config.MCPServer{Command: "npx"}, func(e mcpsync.ServerEntry) bool { return e.Type == "stdio" }},
		{
			"oauth client unset public flag is confidential",
			config.MCPServer{URL: "https://a.example.com", OAuth: config.OAuth{ClientID: "c", CallbackPort: 9}},
			func(e mcpsync.ServerEntry) bool {
				return e.OAuth.ClientID == "c" && e.OAuth.CallbackPort == 9 && e.OAuthClientID == "c" &&
					e.OAuthPublicClient != nil && !*e.OAuthPublicClient && e.Auth.RedirectPort == 9
			},
		},
		{
			"public client",
			config.MCPServer{URL: "https://a.example.com", OAuth: config.OAuth{ClientID: "c", PublicClient: new(true)}},
			func(e mcpsync.ServerEntry) bool { return *e.OAuthPublicClient && e.Auth == nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if e := serverEntry(tt.in); !tt.want(e) {
				t.Errorf("unexpected entry %+v", e)
			}
		})
	}
}

func TestRenderWorkspaces(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cfg := &config.Config{MCP: config.MCP{Workspaces: map[string]config.Workspace{
		"ops": {Path: "~/ops", Servers: map[string]config.MCPServer{
			"crm": {URL: "https://crm.example.com/mcp", OAuth: config.OAuth{ClientID: "c"}},
		}},
	}}}
	env := Env{Home: home, Getenv: func(string) string { return "" }}
	b := &builder{cfg: cfg, env: env, seen: map[string]bool{}}
	selected := Selected([]config.Harness{config.Claude, config.Copilot, config.VSCode, config.Codex, config.OpenCode})
	if err := renderWorkspaces(cfg, b, selected); err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, tg := range b.targets {
		rel, _ := filepath.Rel(filepath.Join(home, "ops"), tg.Path)
		got[rel] = tg.Owner + " " + tg.Mode.String()
	}
	want := map[string]string{
		".mcp.json":          "claude -rw-------",
		".codex/config.toml": "codex -rw-------",
		"opencode.json":      "opencode -rw-------",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("targets (-want +got):\n%s", diff)
	}
	wantWarnings := []string{
		`mcp.workspaces.ops: server "crm": OAuth client is not public, so its secret is not synced; add the secret through the harness's own login`,
	}
	if diff := cmp.Diff(wantWarnings, b.warnings); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
}

func TestRenderWorkspacesOnly(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	r := &config.Resolved{
		Harnesses: []config.Harness{config.Claude, config.Codex},
		Config: config.Config{MCP: config.MCP{Workspaces: map[string]config.Workspace{
			"ops": {Path: "~/ops", Servers: map[string]config.MCPServer{"docs": {URL: "https://docs.example.com/mcp"}}},
		}}},
	}
	plan, err := RenderWorkspaces(r, Env{Home: home, Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(plan.Targets))
	for _, tg := range plan.Targets {
		rel, _ := filepath.Rel(home, tg.Path)
		got = append(got, rel)
	}
	want := []string{"ops/.mcp.json", "ops/.codex/config.toml"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("targets (-want +got):\n%s", diff)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("warnings = %q, want none", plan.Warnings)
	}
}

func TestWorkspaceLegacyOpenCode(t *testing.T) {
	t.Parallel()
	d := mcpsync.Doc{RelPath: mcpsync.OpenCodeRelPath, Managed: map[string]any{
		"mcp": map[string]any{"servers": map[string]any{"b": map[string]any{}, "a": map[string]any{}}},
	}}
	if diff := cmp.Diff([][]string{{"mcp", "a"}, {"mcp", "b"}}, workspaceLegacy(d)); diff != "" {
		t.Errorf("legacy (-want +got):\n%s", diff)
	}
	if got := workspaceLegacy(mcpsync.Doc{RelPath: ".mcp.json"}); got != nil {
		t.Errorf("legacy for another file = %v, want none", got)
	}
}
