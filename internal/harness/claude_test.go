package harness

import (
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

func TestClaudeTelemetryEnvAndLegacy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled bool
		wantEnv bool
	}{
		{"off", false, false},
		{"on", true, true},
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
			managed, _ := st.Managed.(map[string]any)
			if _, has := managed["env"]; has != tt.wantEnv {
				t.Errorf("managed env present = %v, want %v", has, tt.wantEnv)
			}
			// Only the old manager's keys migrate; telemetry env keys leave through the manifest.
			if diff := cmp.Diff(claudeLegacy, st.Legacy); diff != "" {
				t.Errorf("Legacy (-want +got):\n%s", diff)
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

func TestClaudeSettingsFromSharedKeys(t *testing.T) {
	t.Parallel()
	c := &config.Config{
		Context: config.Context{AutoCompact: true, MaxTokens: 4096},
		Permissions: config.Permissions{
			AllowCommands:        []string{"git status *"},
			AllowCommandPatterns: []string{"make *"},
			AllowedMCP:           []string{"docs", "my.server", "mcp__acme__search"},
			AllowedURLs: []string{
				"https://example.com/**", "https://*.example.org/", "docs.acme.dev", "https://acme.dev/docs/**",
				"https://*.cdn.acme.dev/**", "https://a.example/x", "https://example.com/*",
			},
		},
	}
	b := claudeBuilder(c, nil)
	s := claudeSettings(c, b)
	if got := s["autoCompactEnabled"]; got != true {
		t.Errorf("autoCompactEnabled = %v, want true", got)
	}
	env, _ := s["env"].(map[string]any)
	if diff := cmp.Diff(map[string]any{"CLAUDE_CODE_MAX_OUTPUT_TOKENS": "4096"}, env); diff != "" {
		t.Errorf("env (-want +got):\n%s", diff)
	}
	perms, _ := s["permissions"].(map[string]any)
	want := []string{
		"Bash(make *)", "Bash(git status *)",
		"WebFetch(domain:example.com)", "WebFetch(domain:example.org)", "WebFetch(domain:docs.acme.dev)", "WebFetch(domain:cdn.acme.dev)",
		"mcp__docs__*", "mcp__my_server__*", "mcp__acme__search",
	}
	if diff := cmp.Diff(want, perms["allow"]); diff != "" {
		t.Errorf("allow (-want +got):\n%s", diff)
	}
	if len(b.warnings) != 2 {
		t.Errorf("warnings = %q, want two for the path-restricted URLs", b.warnings)
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

func TestTelemetryHarnessesGate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		harnesses []config.Harness
		claude    bool
		vscode    bool
	}{
		{"unset means all", nil, true, true},
		{"listed", []config.Harness{config.Codex, config.Claude}, true, false},
		{"not listed", []config.Harness{config.Codex}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &config.Config{Telemetry: config.Telemetry{Enabled: true, Endpoint: "https://otel.example.com", Harnesses: tt.harnesses}}
			b := claudeBuilder(c, nil)
			if _, got := b.telemetry(config.Claude); got != tt.claude {
				t.Errorf("claude telemetry = %v, want %v", got, tt.claude)
			}
			if got := c.Telemetry.Exports(config.VSCode); got != tt.vscode {
				t.Errorf("vscode exports = %v, want %v", got, tt.vscode)
			}
		})
	}
}
