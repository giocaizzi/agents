package harness

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// renderFor renders c for the selected harnesses on goos, with home "/h".
func renderFor(t *testing.T, c config.Config, goos string, hs ...config.Harness) Plan {
	t.Helper()
	env := map[string]string{"TOKEN": "tok"}
	plan, err := Render(&config.Resolved{Config: c, Harnesses: hs}, Env{
		Home: "/h", Hostname: "host", Arch: "arm64", GOOS: goos,
		Getenv:   func(k string) string { return env[k] },
		LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("no layers") },
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return plan
}

func findTarget(t *testing.T, plan Plan, path string) apply.Target {
	t.Helper()
	for _, tg := range plan.Targets {
		if tg.Path == path {
			return tg
		}
	}
	t.Fatalf("no target %s in %v", path, plan.Targets)
	return apply.Target{}
}

func TestCopilotMCPOAuthFlatKeys(t *testing.T) {
	t.Parallel()
	public := true
	var c config.Config
	c.MCP.Servers = map[string]config.MCPServer{
		"crm": {
			URL: "https://crm.example.com/mcp", Headers: map[string]string{"X-Token": "${TOKEN}"},
			OAuth: config.OAuth{ClientID: "client-1", CallbackPort: 8765, PublicClient: &public},
		},
		"plain": {URL: "https://plain.example.com/mcp"},
		"local": {Command: "acme-mcp", Args: []string{"--x"}, Env: map[string]string{"K": "v"}},
	}
	plan := renderFor(t, c, "linux", config.Copilot)
	got := findTarget(t, plan, filepath.Join("/h", ".copilot", "mcp-config.json")).Managed
	want := map[string]any{"mcpServers": map[string]any{
		"crm": map[string]any{
			"type": "http", "url": "https://crm.example.com/mcp",
			"headers":       map[string]string{"X-Token": "tok"},
			"oauthClientId": "client-1", "oauthPublicClient": true,
			"auth": map[string]any{"redirectPort": 8765},
		},
		"plain": map[string]any{"type": "http", "url": "https://plain.example.com/mcp"},
		"local": map[string]any{
			"type": "stdio", "command": "acme-mcp", "args": []string{"--x"},
			"env": map[string]string{"K": "v"},
		},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mcp-config (-want +got):\n%s", diff)
	}
}

func TestCopilotSettings(t *testing.T) {
	t.Parallel()
	var c config.Config
	c.Plugins.Registry = "acme/plugins"
	c.Plugins.Enabled = map[string][]config.Harness{"a": {config.Copilot}, "b": {config.Copilot}, "c": {config.Claude}}
	c.Plugins.Disabled = []string{"b"}
	c.Permissions.AllowedURLs = []string{"https://example.com/**"}
	c.Harness.Copilot.Settings = map[string]any{
		"theme":  "${TOKEN}",
		"gone":   "${UNSET_VAR}",
		"footer": map[string]any{"showBranch": true},
	}
	plan := renderFor(t, c, "linux", config.Copilot)
	tg := findTarget(t, plan, filepath.Join("/h", ".copilot", "settings.json"))
	want := map[string]any{
		"voice":       map[string]any{"enabled": false},
		"allowedUrls": []string{"https://example.com/**"},
		"enabledPlugins": map[string]any{
			"a@acme-plugins": true, "b@acme-plugins": false,
		},
		"extraKnownMarketplaces": map[string]any{"acme-plugins": map[string]any{
			"source": map[string]any{"source": "github", "repo": "acme/plugins"},
		}},
		"theme":  "tok",
		"footer": map[string]any{"showBranch": true},
	}
	if diff := cmp.Diff(want, tg.Managed); diff != "" {
		t.Errorf("settings (-want +got):\n%s", diff)
	}
	if tg.Mode != 0o600 || tg.Format != apply.JSON || tg.CheckOnly {
		t.Errorf("target = %+v", tg)
	}
	if len(plan.Warnings) != 1 {
		t.Errorf("warnings = %q, want one for the unset variable", plan.Warnings)
	}
}

func TestCopilotLSPRemovesUnrenderedServers(t *testing.T) {
	t.Parallel()
	var c config.Config
	c.LSP.Servers = map[string]config.LSPServer{
		"gopls": {Command: []string{"gopls"}, Extensions: map[string]string{".go": "go"}},
	}
	plan := renderFor(t, c, "linux", config.Copilot)
	tg := findTarget(t, plan, filepath.Join("/h", ".copilot", "lsp-config.json"))
	wantRemove := [][]string{
		{"lspServers", "gopls", "command"}, {"lspServers", "gopls", "args"}, {"lspServers", "gopls", "fileExtensions"},
	}
	if diff := cmp.Diff(wantRemove, tg.Remove); diff != "" {
		t.Errorf("Remove (-want +got):\n%s", diff)
	}
}
