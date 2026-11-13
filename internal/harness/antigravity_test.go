package harness

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

func TestRenderAntigravityMCPDropsCallbackPort(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.MCP.Servers = map[string]config.MCPServer{
		"crm":   {URL: "https://crm.example.com/mcp", OAuth: config.OAuth{ClientID: "id", CallbackPort: 8765}},
		"local": {Command: "x", Args: []string{"-y"}, Env: map[string]string{"K": "v"}},
	}
	b := newTestBuilder(c)
	if err := renderAntigravity(c, b); err != nil {
		t.Fatal(err)
	}
	got := targetAt(t, b, "/home/u/.gemini/config/mcp_config.json").Managed
	want := map[string]any{"mcpServers": map[string]any{
		"crm":   map[string]any{"url": "https://crm.example.com/mcp", "oauth": map[string]any{"clientId": "id"}},
		"local": map[string]any{"command": "x", "args": []any{"-y"}, "env": map[string]any{"K": "v"}},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mcp_config.json (-want +got):\n%s", diff)
	}
}

func TestAntigravityPermissions(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.Permissions = config.Permissions{
		AllowCommandPatterns: []string{"git push origin *", "a * b"},
		DenyCommandPatterns:  []string{"gh pr merge *", "git push * main", "git push origin main", "git push origin main *"},
		DenyCommandExact:     []string{"env"},
		DenyCommandPrefixes:  []string{"pip", "python -m pip"},
		AskCommandPrefixes:   []string{"bw"},
		DenyRead:             []string{"**/.env"},
		DenyWrite:            []string{"**/.ssh/**"},
	}
	c.Harness.Antigravity.Allow = []string{"mcp(*)"}
	got := antigravityPermissions(c)
	want := map[string]any{
		"allow": []any{"mcp(*)", "command(git push origin)"},
		"ask":   []any{"command(bw)", "command(bw *)"},
		"deny": []any{
			"command(gh pr merge *)", "command(git push origin main)", "command(env)",
			"command(pip)", "command(pip *)", "command(python -m pip *)",
			"read_file(**/.env)", "write_file(**/.ssh/**)",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("permissions (-want +got):\n%s", diff)
	}
}
