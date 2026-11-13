package harness

import (
	"path/filepath"
	"strings"
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
		".mcp.json":          "claude -rw-r--r--",
		".codex/config.toml": "codex -rw-------",
		"opencode.json":      "opencode -rw-r--r--",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("targets (-want +got):\n%s", diff)
	}
	if len(b.warnings) != 1 || !strings.Contains(b.warnings[0], "mcp.workspaces.ops: server \"crm\": OAuth client is not public") {
		t.Errorf("warnings = %q, want the confidential client warning", b.warnings)
	}
}
