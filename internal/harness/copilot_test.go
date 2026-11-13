package harness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// applyIn renders c for hs with home as the home directory and applies the
// plan with the manifest under home, failing on any target error.
func applyIn(t *testing.T, home string, c config.Config, layers map[string]string, hs ...config.Harness) {
	t.Helper()
	plan, err := Render(&config.Resolved{Config: c, Harnesses: hs}, Env{
		Home: home, Hostname: "host", Arch: "arm64", GOOS: "linux",
		Getenv:   func(string) string { return "" },
		LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
		ReadFile: func(p string) ([]byte, error) {
			if s, ok := layers[filepath.Base(p)]; ok {
				return []byte(s), nil
			}
			return nil, errors.New("no layer " + p)
		},
		Dirs: config.Dirs{State: filepath.Join(home, "state"), Data: filepath.Join(home, "data")},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	res, err := apply.Engine{StatePath: filepath.Join(home, "state", "apply.json")}.Run(plan.Targets)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Err != nil {
			t.Fatalf("%s: %v", r.Target.Path, r.Err)
		}
	}
}

// TestCopilotReviewXFail holds reproductions of bugs found in review; drop
// the t.Skip to verify a fix.
func TestCopilotReviewXFail(t *testing.T) {
	t.Parallel()

	t.Run("xfail: removing the last MCP server removes it from mcp-config.json", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: the mcp-config.json target is only emitted while servers exist, so the manifest never drops the last server")
		home := t.TempDir()
		var c config.Config
		c.MCP.Servers = map[string]config.MCPServer{"a": {Command: "a-mcp"}}
		applyIn(t, home, c, nil, config.Copilot)
		applyIn(t, home, config.Config{}, nil, config.Copilot)
		data, err := os.ReadFile(filepath.Join(home, ".copilot", "mcp-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "a-mcp") {
			t.Errorf("server dropped from the config is still written:\n%s", data)
		}
	})

	t.Run("xfail: apply keeps the user's own statusLine in settings.json", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: the Copilot settings target always removes statusLine, a key agents never manages")
		home := t.TempDir()
		path := filepath.Join(home, ".copilot", "settings.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"statusLine": {"type": "command", "command": "my-status"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		applyIn(t, home, config.Config{}, nil, config.Copilot)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "my-status") {
			t.Errorf("user statusLine deleted:\n%s", data)
		}
	})

	t.Run("xfail: a new instruction file is private (0600)", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: instruction targets are created 0644, but layers may hold private content")
		home := t.TempDir()
		var c config.Config
		c.Instructions.Dir = filepath.Join(home, "layers")
		c.Instructions.Layers = map[string]config.InstructionLayer{"personal": {}}
		c.Harness.Copilot.Instructions = []string{"personal"}
		applyIn(t, home, c, map[string]string{"personal.md": "private notes"}, config.Copilot)
		info, err := os.Stat(filepath.Join(home, ".copilot", "copilot-instructions.md"))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("mode = %v, want 0600", got)
		}
	})
}
