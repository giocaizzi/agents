package pluginmgmt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giocaizzi/agents/internal/pluginmgmt"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// registryFixture builds a checkout with three plugins and returns its path.
func registryFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "checkout")
	writeTree(t, dir, map[string]string{
		"plugins/dev-tools/skills/a/SKILL.md":           "skill a",
		"plugins/dev-tools/skills/b/SKILL.md":           "skill b",
		"plugins/dev-tools/.generated/opencode/x.md":    "agent x",
		"plugins/dev-tools/.claude-plugin/plugin.json":  `{"version": "1.2.0"}`,
		"plugins/guard/hooks/opencode.mjs":              "export {}",
		"plugins/guard/.claude-plugin/plugin.json":      `{"version": "2.0.0"}`,
		"plugins/hud/hooks/opencode-tui.mjs":            "export {}",
		"plugins/hud/.claude-plugin/plugin.json":        `{"version": "3.0.0"}`,
		"plugins/bare/commands/c.md":                    "nothing for opencode",
		"plugins/bare/.claude-plugin/plugin.json":       `{"version": "4.0.0"}`,
		"plugins/unversioned/hooks/opencode.mjs":        "export {}",
		"plugins/unversioned/.claude-plugin/other.json": `{}`,
	})
	return dir
}

// installedOn returns what ListPlugins reports for plugin on harness.
func installedOn(t *testing.T, m pluginmgmt.Manager, plugin, harness string) string {
	t.Helper()
	m.Plugins = map[string][]string{plugin: {harness}}
	list := m.ListPlugins(t.Context())
	for _, p := range list {
		if p.Name == plugin {
			return p.Installed[harness]
		}
	}
	t.Fatalf("plugin %s not listed", plugin)
	return ""
}

func TestPiVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		settings func(checkout, home string) string // empty means no settings file
		plugin   string
		want     string
	}{
		{"relative path", func(checkout, home string) string {
			rel, _ := filepath.Rel(filepath.Join(home, ".pi", "agent"), checkout)
			return `{"packages": ["` + filepath.ToSlash(rel) + `"]}`
		}, "dev-tools", "1.2.0"},
		{"absolute path", func(checkout, _ string) string {
			return `{"packages": ["git:example/harness", "` + checkout + `"]}`
		}, "dev-tools", "1.2.0"},
		{"object form", func(checkout, _ string) string {
			return `{"packages": [{"source": "` + checkout + `"}]}`
		}, "guard", "2.0.0"},
		{"home path", func(_, _ string) string {
			return `{"packages": ["~/checkout"]}`
		}, "dev-tools", "1.2.0"},
		{"symlinked path", func(_, home string) string {
			return `{"packages": ["` + filepath.Join(home, "link") + `"]}`
		}, "dev-tools", "1.2.0"},
		{"plugin without version", func(checkout, _ string) string {
			return `{"packages": ["` + checkout + `"]}`
		}, "unversioned", "linked"},
		{"git entry only", func(_, _ string) string {
			return `{"packages": ["git:example/harness", "npm:example"]}`
		}, "dev-tools", ""},
		{"another directory", func(_, home string) string {
			return `{"packages": ["` + filepath.Join(home, "elsewhere") + `"]}`
		}, "dev-tools", ""},
		{"unparsable", func(_, _ string) string { return `{` }, "dev-tools", ""},
		{"no settings file", func(_, _ string) string { return "" }, "dev-tools", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkout := registryFixture(t)
			home := t.TempDir()
			// "~/checkout" and "link" both point at the checkout.
			if err := os.Symlink(checkout, filepath.Join(home, "checkout")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(checkout, filepath.Join(home, "link")); err != nil {
				t.Fatal(err)
			}
			if s := tt.settings(checkout, home); s != "" {
				writeTree(t, home, map[string]string{".pi/agent/settings.json": s})
			}
			m := pluginmgmt.Manager{HarnessDir: checkout, HomeDir: home}
			if got := installedOn(t, m, tt.plugin, "pi"); got != tt.want {
				t.Errorf("pi installed(%s) = %q, want %q", tt.plugin, got, tt.want)
			}
		})
	}
}

func TestOpencodeVersion(t *testing.T) {
	t.Parallel()
	skillA := "skills/a/SKILL.md"
	skillB := "skills/b/SKILL.md"
	agentX := "agents/x.md"
	fullCopies := map[string]string{skillA: "skill a", skillB: "skill b", agentX: "agent x"}

	tests := []struct {
		name        string
		target      map[string]string // files under <config>/opencode
		plugin      string
		homeConfig  bool // use HomeDir/.config instead of ConfigHome
		wantVersion string
	}{
		{name: "identical copies", target: fullCopies, plugin: "dev-tools", wantVersion: "1.2.0"},
		{name: "identical copies under home config", target: fullCopies, plugin: "dev-tools", homeConfig: true, wantVersion: "1.2.0"},
		{
			name: "changed skill", plugin: "dev-tools", wantVersion: pluginmgmt.VersionOutdated,
			target: map[string]string{skillA: "old", skillB: "skill b", agentX: "agent x"},
		},
		{
			name: "extra file in skill dir", plugin: "dev-tools", wantVersion: pluginmgmt.VersionOutdated,
			target: map[string]string{skillA: "skill a", "skills/a/stale.md": "x", skillB: "skill b", agentX: "agent x"},
		},
		{
			name: "skill dir missing", plugin: "dev-tools", wantVersion: pluginmgmt.VersionOutdated,
			target: map[string]string{skillA: "skill a", agentX: "agent x"},
		},
		{
			name: "agent changed", plugin: "dev-tools", wantVersion: pluginmgmt.VersionOutdated,
			target: map[string]string{skillA: "skill a", skillB: "skill b", agentX: "old"},
		},
		{name: "nothing installed", target: map[string]string{"skills/other/SKILL.md": "o"}, plugin: "dev-tools", wantVersion: ""},
		{
			name: "guard stub", plugin: "guard", wantVersion: "2.0.0",
			target: map[string]string{"plugins/guard.js": "import \"file:///any/where/plugins/guard/hooks/opencode.mjs\";"},
		},
		{
			name: "stub of another plugin", plugin: "guard", wantVersion: "",
			target: map[string]string{"plugins/other.js": "import \"file:///any/where/plugins/other/hooks/opencode.mjs\";"},
		},
		{
			name: "hud in tui.json", plugin: "hud", wantVersion: "3.0.0",
			target: map[string]string{"tui.json": `{"plugin": ["file:///any/where/plugins/hud/hooks/opencode-tui.mjs"]}`},
		},
		{
			name: "unversioned stub", plugin: "unversioned", wantVersion: "linked",
			target: map[string]string{"plugins/u.js": "x /plugins/unversioned/hooks/opencode.mjs"},
		},
		{name: "nothing for opencode", target: fullCopies, plugin: "bare", wantVersion: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkout := registryFixture(t)
			home := t.TempDir()
			cfg := filepath.Join(t.TempDir(), "xdg")
			m := pluginmgmt.Manager{HarnessDir: checkout, HomeDir: home, ConfigHome: cfg}
			if tt.homeConfig {
				m.ConfigHome = ""
				cfg = filepath.Join(home, ".config")
			}
			writeTree(t, filepath.Join(cfg, "opencode"), tt.target)
			if got := installedOn(t, m, tt.plugin, "opencode"); got != tt.wantVersion {
				t.Errorf("opencode installed(%s) = %q, want %q", tt.plugin, got, tt.wantVersion)
			}
		})
	}
}
