package harness

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

func TestVSCodeSettingsPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		goos string
		want string
	}{
		{"linux", filepath.Join("/h", ".config", "Code", "User", "settings.json")},
		{"darwin", filepath.Join("/h", "Library", "Application Support", "Code", "User", "settings.json")},
		{"windows", filepath.Join("/h", "AppData", "Roaming", "Code", "User", "settings.json")},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			t.Parallel()
			plan := renderFor(t, config.Config{}, tt.goos, config.VSCode)
			tg := findTarget(t, plan, tt.want)
			if tg.Format != apply.JSONC || !tg.CheckOnly || tg.Owner != "vscode" {
				t.Errorf("target = %+v, want JSONC, CheckOnly, owner vscode", tg)
			}
		})
	}
}

func TestVSCodeSettingsAppData(t *testing.T) {
	t.Parallel()
	b := &builder{env: Env{Home: "/h", GOOS: "windows", Getenv: func(string) string { return filepath.Join("/x", "Roaming") }}}
	want := filepath.Join("/x", "Roaming", "Code", "User", "settings.json")
	if got := vscodeSettingsPath(b); got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestVSCodeManagedKeys(t *testing.T) {
	t.Parallel()
	var c config.Config
	c.Permissions = config.Permissions{
		AllowCommands: []string{"git *"}, AllowCommandPatterns: []string{"git push *"},
		DenyCommandExact: []string{"env"}, DenyCommandPatterns: []string{"rm -rf *"},
		AskCommandPrefixes: []string{"bw"}, AllowedURLs: []string{"https://example.com/**"},
	}
	c.Voice = config.Voice{Enabled: true, Language: "en-US"}
	c.Harness.VSCode.Settings = map[string]any{"editor.fontSize": int64(14)}
	plan := renderFor(t, c, "linux", config.VSCode)
	got := findTarget(t, plan, filepath.Join("/h", ".config", "Code", "User", "settings.json")).Managed
	want := map[string]any{
		"dictation.enabled": true,
		"chat.tools.terminal.autoApprove": map[string]any{
			"git *": true, "git push *": true, "env": false, "rm -rf *": false, "bw": false,
		},
		"chat.tools.urls.autoApprove": map[string]any{"https://example.com/**": true},
		"workbench.colorTheme":        "Catppuccin Mocha",
		"agents.voice.language":       "en-US",
		"editor.fontSize":             int64(14),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("settings (-want +got):\n%s", diff)
	}
}

func copilotPaths() []string {
	out := make([]string, 0, 3)
	for _, f := range []string{"settings.json", "lsp-config.json", "mcp-config.json"} {
		out = append(out, filepath.Join("/h", ".copilot", f))
	}
	return out
}

func TestVSCodeRendersCopilotFiles(t *testing.T) {
	t.Parallel()
	c := copilotFixture()
	for _, tt := range []struct {
		name      string
		hs        []config.Harness
		wantOwner string
	}{
		{"vscode alone", []config.Harness{config.VSCode}, "vscode"},
		{"copilot and vscode", []config.Harness{config.VSCode, config.Copilot}, "copilot"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := renderFor(t, c, "linux", tt.hs...)
			count := map[string]int{}
			for _, tg := range plan.Targets {
				count[tg.Path]++
			}
			for _, p := range copilotPaths() {
				if count[p] != 1 {
					t.Errorf("%s rendered %d times, want once", p, count[p])
				}
				if got := findTarget(t, plan, p).Owner; got != tt.wantOwner {
					t.Errorf("%s owner = %q, want %q", p, got, tt.wantOwner)
				}
			}
		})
	}
}

func copilotFixture() config.Config {
	var c config.Config
	c.LSP = config.LSP{Enabled: true, Servers: map[string]config.LSPServer{
		"gopls": {Command: []string{"gopls"}, Extensions: map[string]string{".go": "go"}},
	}}
	c.MCP.Servers = map[string]config.MCPServer{"docs": {URL: "https://docs.example.com/mcp"}}
	return c
}
