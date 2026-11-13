package pluginmgmt_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/pluginmgmt"
)

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v1, v2 string
		want   int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.53.0", "1.49.0", 1},
		{"1.49.0", "1.53.0", -1},
		{"v1.2.0", "1.2.0", 0},
		{"v2.0.0", "v1.9.9", 1},
	}
	for _, tc := range cases {
		got := pluginmgmt.CompareVersions(tc.v1, tc.v2)
		if got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.v1, tc.v2, got, tc.want)
		}
	}
}

func TestStateAndBehind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		enabled   bool
		installed string
		upstream  string
		want      pluginmgmt.PluginState
		behind    bool
	}{
		{"older is stale", true, "1.0.0", "1.1.0", pluginmgmt.StateStale, true},
		{"older with v prefix is stale", true, "v1.0.0", "1.1.0", pluginmgmt.StateStale, true},
		{"equal is ok", true, "1.1.0", "1.1.0", pluginmgmt.StateOK, false},
		{"newer is ok", true, "1.2.0", "1.1.0", pluginmgmt.StateOK, false},
		{"not installed is missing", true, "", "1.1.0", pluginmgmt.StateMissing, true},
		{"linked is ok", true, "linked", "1.1.0", pluginmgmt.StateOK, false},
		{"outdated is stale", true, "outdated", "1.1.0", pluginmgmt.StateStale, true},
		{"installed is ok", true, "installed", "9.9.9", pluginmgmt.StateOK, false},
		{"enabled is ok", true, "enabled", "9.9.9", pluginmgmt.StateOK, false},
		{"no upstream is ok", true, "1.0.0", "", pluginmgmt.StateOK, false},
		{"disabled is not behind", false, "1.0.0", "1.1.0", pluginmgmt.StateDisabled, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := pluginmgmt.PluginInfo{
				Name:            "dev-tools",
				UpstreamVersion: tt.upstream,
				Enabled:         map[string]bool{"claude": tt.enabled},
				Installed:       map[string]string{"claude": tt.installed},
			}
			if got := p.State("claude"); got != tt.want {
				t.Errorf("State = %v, want %v", got, tt.want)
			}
			if got := p.Behind("claude"); got != tt.behind {
				t.Errorf("Behind = %v, want %v", got, tt.behind)
			}
		})
	}
}

func TestReadPluginVersion(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	// Missing
	if v := pluginmgmt.ReadPluginVersion(tmp); v != "" {
		t.Errorf("expected empty for missing plugin.json, got %q", v)
	}

	// plugin.json
	data := []byte(`{"version": "1.2.3"}`)
	if err := os.WriteFile(filepath.Join(tmp, "plugin.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if v := pluginmgmt.ReadPluginVersion(tmp); v != "1.2.3" {
		t.Errorf("got %q, want 1.2.3", v)
	}

	// .antigravity-plugin/plugin.json
	sub := filepath.Join(tmp, "sub")
	if err := os.MkdirAll(filepath.Join(sub, ".antigravity-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, ".antigravity-plugin", "plugin.json"), []byte(`{"version": "2.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := pluginmgmt.ReadPluginVersion(sub); v != "2.0.1" {
		t.Errorf("got %q, want 2.0.1", v)
	}
}

func TestListPlugins(t *testing.T) {
	t.Parallel()
	tmpHarness := t.TempDir()
	tmpHome := t.TempDir()

	// Create upstream plugins
	pDir := filepath.Join(tmpHarness, "plugins")
	devToolsDir := filepath.Join(pDir, "dev-tools")
	if err := os.MkdirAll(devToolsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devToolsDir, "plugin.json"), []byte(`{"version": "1.53.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create antigravity installed
	agyDevTools := filepath.Join(tmpHome, ".gemini", "config", "plugins", "dev-tools")
	if err := os.MkdirAll(agyDevTools, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agyDevTools, "plugin.json"), []byte(`{"version": "1.53.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create claude cache
	claudeDevTools := filepath.Join(tmpHome, ".claude", "plugins", "cache", "harness", "dev-tools", "1.53.0")
	if err := os.MkdirAll(claudeDevTools, 0o700); err != nil {
		t.Fatal(err)
	}

	plugins := map[string][]string{
		"dev-tools": {"antigravity", "claude"},
	}

	mgr := pluginmgmt.Manager{
		HarnessDir: tmpHarness,
		HomeDir:    tmpHome,
		Plugins:    plugins,
	}

	list := mgr.ListPlugins(context.Background())
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}

	p := list[0]
	if p.Name != "dev-tools" || p.UpstreamVersion != "1.53.0" {
		t.Errorf("unexpected plugin info: %+v", p)
	}
	if p.State("antigravity") != pluginmgmt.StateOK {
		t.Errorf("expected antigravity StateOK, got %v", p.State("antigravity"))
	}
	if p.State("claude") != pluginmgmt.StateOK {
		t.Errorf("expected claude StateOK, got %v", p.State("claude"))
	}
	if p.State("codex") != pluginmgmt.StateDisabled {
		t.Errorf("expected codex StateDisabled, got %v", p.State("codex"))
	}
}

func TestFindPruneCandidates(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()

	// Claude cache with 2 versions
	cDir := filepath.Join(tmpHome, ".claude", "plugins", "cache", "harness", "dev-tools")
	if err := os.MkdirAll(filepath.Join(cDir, "1.51.9"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cDir, "1.53.0"), 0o700); err != nil {
		t.Fatal(err)
	}

	// Obsolete api plugin in antigravity
	if err := os.MkdirAll(filepath.Join(tmpHome, ".gemini", "config", "plugins", "api"), 0o700); err != nil {
		t.Fatal(err)
	}

	plugins := map[string][]string{
		"dev-tools": {"claude", "antigravity"},
	}

	mgr := pluginmgmt.Manager{
		HomeDir: tmpHome,
		Plugins: plugins,
	}

	candidates, err := mgr.FindPruneCandidates()
	if err != nil {
		t.Fatalf("FindPruneCandidates: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(candidates), candidates)
	}

	pruned, err := mgr.Prune(candidates)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(pruned) != 2 {
		t.Errorf("expected 2 pruned, got %d", len(pruned))
	}

	// Confirm directories were removed
	if _, err := os.Stat(filepath.Join(cDir, "1.51.9")); !os.IsNotExist(err) {
		t.Errorf("expected 1.51.9 to be removed")
	}
	if _, err := os.Stat(filepath.Join(cDir, "1.53.0")); err != nil {
		t.Errorf("expected 1.53.0 to be preserved")
	}
}

func TestSortHarnesses(t *testing.T) {
	t.Parallel()
	in := []string{"pi", "claude", "antigravity", "codex", "custom"}
	got := pluginmgmt.SortHarnesses(in)
	want := []string{"antigravity", "claude", "codex", "pi", "custom"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("SortHarnesses mismatch (-want +got):\n%s", diff)
	}
}

func TestManagerNamespace(t *testing.T) {
	t.Parallel()

	t.Run("explicit-namespace", func(t *testing.T) {
		t.Parallel()
		mgr := pluginmgmt.Manager{Namespace: "custom-ns"}
		if got := mgr.EffectiveNamespace(); got != "custom-ns" {
			t.Errorf("EffectiveNamespace() = %q, want custom-ns", got)
		}
	})

	t.Run("fallback-default", func(t *testing.T) {
		t.Parallel()
		mgr := pluginmgmt.Manager{}
		if got := mgr.EffectiveNamespace(); got != "harness" {
			t.Errorf("EffectiveNamespace() = %q, want harness", got)
		}
	})
}

func TestCopilotVersion_InstalledPluginsDir(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	tmpHarness := t.TempDir()
	ns := "test-ns"
	plugin := "dev-tools"
	if err := os.MkdirAll(filepath.Join(tmpHarness, "plugins", plugin), 0o755); err != nil {
		t.Fatal(err)
	}
	pDir := filepath.Join(tmpHome, ".copilot", "installed-plugins", ns, plugin)
	if err := os.MkdirAll(pDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pDir, "plugin.json"), []byte(`{"version": "1.2.3"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := pluginmgmt.Manager{
		HarnessDir: tmpHarness,
		HomeDir:    tmpHome,
		Namespace:  ns,
		Plugins:    map[string][]string{plugin: {"copilot"}},
	}

	list := mgr.ListPlugins(context.Background())
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}
	if got := list[0].Installed["copilot"]; got != "1.2.3" {
		t.Errorf("copilot version = %q, want 1.2.3", got)
	}
}

func TestCopilotVersion_ConfigJSON(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	tmpHarness := t.TempDir()
	ns := "test-ns"
	plugin := "guard"
	if err := os.MkdirAll(filepath.Join(tmpHarness, "plugins", plugin), 0o755); err != nil {
		t.Fatal(err)
	}
	copilotDir := filepath.Join(tmpHome, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgJSON := `{
  "installedPlugins": [
    {
      "name": "guard",
      "version": "2.4.0",
      "marketplace": "test-ns",
      "enabled": true
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(copilotDir, "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := pluginmgmt.Manager{
		HarnessDir: tmpHarness,
		HomeDir:    tmpHome,
		Namespace:  ns,
		Plugins:    map[string][]string{plugin: {"copilot"}},
	}

	list := mgr.ListPlugins(context.Background())
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}
	if got := list[0].Installed["copilot"]; got != "2.4.0" {
		t.Errorf("copilot version = %q, want 2.4.0", got)
	}
}

func TestCopilotVersion_VSCodeFallback(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	tmpHarness := t.TempDir()
	ns := "test-ns"
	plugin := "sounds"
	if err := os.MkdirAll(filepath.Join(tmpHarness, "plugins", plugin), 0o755); err != nil {
		t.Fatal(err)
	}
	pDir := filepath.Join(tmpHome, ".vscode", "agent-plugins", ns+"-"+plugin)
	if err := os.MkdirAll(pDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pDir, "plugin.json"), []byte(`{"version": "0.9.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := pluginmgmt.Manager{
		HarnessDir: tmpHarness,
		HomeDir:    tmpHome,
		Namespace:  ns,
		Plugins:    map[string][]string{plugin: {"copilot"}},
	}

	list := mgr.ListPlugins(context.Background())
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}
	if got := list[0].Installed["copilot"]; got != "0.9.1" {
		t.Errorf("copilot version = %q, want 0.9.1", got)
	}
}

func hermeticGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "octocat")
	t.Setenv("GIT_AUTHOR_EMAIL", "octocat@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "octocat")
	t.Setenv("GIT_COMMITTER_EMAIL", "octocat@example.com")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeVersion(t *testing.T, repo, version string) {
	t.Helper()
	manifest := filepath.Join(repo, "plugins", "dev-tools", ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"version": "`+version+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commitVersion(t *testing.T, repo, version string) {
	t.Helper()
	writeVersion(t, repo, version)
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-q", "-m", "release "+version)
	runGit(t, repo, "push", "-q", "origin", "HEAD:main")
}

// newGitRegistry builds a bare origin at 1.1.0 and a clone whose working tree
// still says 1.0.0. It returns the checkout path.
func newGitRegistry(t *testing.T) string {
	t.Helper()
	hermeticGit(t)
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	checkout := filepath.Join(root, "checkout")

	runGit(t, root, "init", "-q", "--bare", "-b", "main", origin)
	runGit(t, root, "init", "-q", "-b", "main", seed)
	runGit(t, seed, "remote", "add", "origin", origin)
	commitVersion(t, seed, "1.0.0")
	runGit(t, root, "clone", "-q", origin, checkout)
	commitVersion(t, seed, "1.1.0")
	return checkout
}

func TestListPluginsUpstreamFromGit(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		wantVer   string
		wantStale bool
	}{
		{
			name:    "fetched remote tip",
			setup:   newGitRegistry,
			wantVer: "1.1.0",
		},
		{
			name: "unreachable origin falls back to working tree",
			setup: func(t *testing.T) string {
				t.Helper()
				checkout := newGitRegistry(t)
				runGit(t, checkout, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
				return checkout
			},
			wantVer:   "1.0.0",
			wantStale: true,
		},
		{
			name: "plain directory is not stale",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeVersion(t, dir, "1.0.0")
				return dir
			},
			wantVer: "1.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)
			mgr := pluginmgmt.Manager{
				HarnessDir: dir,
				HomeDir:    t.TempDir(),
				Plugins:    map[string][]string{"dev-tools": {"claude"}},
			}
			list := mgr.ListPlugins(t.Context())
			if len(list) != 1 {
				t.Fatalf("expected 1 plugin, got %d", len(list))
			}
			if got := list[0].UpstreamVersion; got != tt.wantVer {
				t.Errorf("UpstreamVersion = %q, want %q", got, tt.wantVer)
			}
			if got := list[0].UpstreamStale; got != tt.wantStale {
				t.Errorf("UpstreamStale = %v, want %v", got, tt.wantStale)
			}
			// The working tree must never be modified.
			if got := pluginmgmt.ReadPluginVersion(filepath.Join(dir, "plugins", "dev-tools")); got != "1.0.0" {
				t.Errorf("working tree version = %q, want 1.0.0", got)
			}
		})
	}
}
