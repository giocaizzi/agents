package harness

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

func claudeBuilder(c *config.Config, env map[string]string) *builder {
	return &builder{
		cfg: c, seen: map[string]bool{},
		env: Env{
			Home: "/home/u", Hostname: "h", Arch: "arm64", GOOS: "linux",
			Getenv:   func(k string) string { return env[k] },
			LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
			Dirs:     config.Dirs{Data: "/data/agents"},
		},
	}
}

func claudeTarget(t *testing.T, b *builder, path string) apply.Target {
	t.Helper()
	for _, tg := range b.targets {
		if tg.Path == path {
			return tg
		}
	}
	t.Fatalf("no target %s in %d targets", path, len(b.targets))
	return apply.Target{}
}

func TestClaudeLSPClaimsExtensionsOnce(t *testing.T) {
	t.Parallel()
	servers := []LSPServer{
		{Name: "a", Command: []string{"a-ls", "--stdio"}, Extensions: map[string]string{".py": "python"}},
		{Name: "b", Command: []string{"b-ls"}, Extensions: map[string]string{".py": "python"}},
		{Name: "c", Command: []string{"c-ls"}, Extensions: map[string]string{".py": "python", ".pyi": "python"}},
	}
	got := claudeLSPConfig(servers)
	want := map[string]any{
		"a": map[string]any{"command": "a-ls", "args": []string{"--stdio"}, "extensionToLanguage": map[string]any{".py": "python"}},
		"c": map[string]any{"command": "c-ls", "args": []string{}, "extensionToLanguage": map[string]any{".pyi": "python"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("lsp config (-want +got):\n%s", diff)
	}
}

func TestClaudeTelemetryOffRemovesKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		enabled   bool
		wantRmEnv bool
	}{
		{"off", false, true},
		{"on", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &config.Config{Telemetry: config.Telemetry{Enabled: tt.enabled, Endpoint: "https://otel.example.com"}}
			b := claudeBuilder(c, nil)
			if err := renderClaude(c, b); err != nil {
				t.Fatal(err)
			}
			st := claudeTarget(t, b, "/home/u/.claude/settings.json")
			has := slices.ContainsFunc(st.Legacy, func(p []string) bool {
				return slices.Equal(p, []string{"env", "OTEL_TRACES_EXPORTER"})
			})
			if has != tt.wantRmEnv {
				t.Errorf("Remove has OTEL_TRACES_EXPORTER = %v, want %v", has, tt.wantRmEnv)
			}
			if !slices.ContainsFunc(st.Legacy, func(p []string) bool { return slices.Equal(p, []string{"enabledPlugins", "lsp@dotfiles"}) }) {
				t.Error("Remove misses the old local marketplace plugin")
			}
		})
	}
}

func TestClaudeSettingsSkipUnsetVariable(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.Harness.Claude.Settings = map[string]any{
		"statusLine": map[string]any{"command": "run ${HOME}/x"},
		"token":      "${MISSING}",
		"list":       []any{"${HOME}", "${MISSING}"},
	}
	b := claudeBuilder(c, map[string]string{"HOME": "/home/u"})
	got := claudeSettings(c, b)
	if diff := cmp.Diff(map[string]any{"command": "run /home/u/x"}, got["statusLine"]); diff != "" {
		t.Errorf("statusLine (-want +got):\n%s", diff)
	}
	if _, ok := got["token"]; ok {
		t.Error("token with an unset variable was kept")
	}
	if diff := cmp.Diff([]any{"/home/u"}, got["list"]); diff != "" {
		t.Errorf("list (-want +got):\n%s", diff)
	}
	if len(b.warnings) != 2 || !strings.Contains(b.warnings[0], "${MISSING}") {
		t.Errorf("warnings = %q, want two naming ${MISSING}", b.warnings)
	}
}

func TestClaudeMCPServers(t *testing.T) {
	t.Parallel()
	got := claudeMCPServers(map[string]config.MCPServer{
		"local": {Command: "npx", Args: []string{"-y", "srv"}, Env: map[string]string{"K": "v"}},
		"remote": {
			URL: "https://x.example.com/mcp", Headers: map[string]string{"A": "b"},
			OAuth: config.OAuth{ClientID: "id", CallbackPort: 9},
		},
	})
	want := map[string]any{
		"local": map[string]any{"type": "stdio", "command": "npx", "args": []string{"-y", "srv"}, "env": map[string]string{"K": "v"}},
		"remote": map[string]any{
			"type": "http", "url": "https://x.example.com/mcp", "headers": map[string]string{"A": "b"},
			"oauth": map[string]any{"clientId": "id", "callbackPort": 9},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mcp servers (-want +got):\n%s", diff)
	}
}
