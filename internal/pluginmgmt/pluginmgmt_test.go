package pluginmgmt_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/pluginmgmt"
	"github.com/giocaizzi/agents/internal/pluginreg"
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
	claudeDevTools := filepath.Join(tmpHome, ".claude", "plugins", "cache", "giocaizzi-harness", "dev-tools", "1.53.0")
	if err := os.MkdirAll(claudeDevTools, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := pluginreg.HarnessConfig{
		Plugins: map[string][]string{
			"dev-tools": {"antigravity", "claude"},
		},
	}

	mgr := pluginmgmt.Manager{
		HarnessDir:    tmpHarness,
		HomeDir:       tmpHome,
		HarnessConfig: cfg,
	}

	list, err := mgr.ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
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
	cDir := filepath.Join(tmpHome, ".claude", "plugins", "cache", "giocaizzi-harness", "dev-tools")
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

	cfg := pluginreg.HarnessConfig{
		Plugins: map[string][]string{
			"dev-tools": {"claude", "antigravity"},
		},
	}

	mgr := pluginmgmt.Manager{
		HomeDir:       tmpHome,
		HarnessConfig: cfg,
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
