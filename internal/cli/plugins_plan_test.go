package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/pluginmgmt"
	"github.com/giocaizzi/agents/internal/term"
)

func fakeLookPath(missing ...string) func(string) (string, error) {
	return func(bin string) (string, error) {
		if slices.Contains(missing, bin) {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + bin, nil
	}
}

func planInfo(name, upstream string, installed map[string]string) pluginmgmt.PluginInfo {
	enabled := make(map[string]bool, len(installed))
	for h := range installed {
		enabled[h] = true
	}
	return pluginmgmt.PluginInfo{Name: name, UpstreamVersion: upstream, Enabled: enabled, Installed: installed}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// project reduces a plan to comparable strings.
func project(p harnessPlan) (changes, steps []string, skip string) {
	for _, c := range p.changes {
		changes = append(changes, c.String())
	}
	for _, s := range p.steps {
		steps = append(steps, s.String())
	}
	return changes, steps, p.skip
}

func TestPlanUpdates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	touch(t, filepath.Join(dir, "scripts", "install.mjs"))
	touch(t, filepath.Join(dir, "plugins", "dev-tools", "plugin.json"))
	bare := t.TempDir() // checkout without installer or plugin dirs
	mgr := pluginmgmt.Manager{HarnessDir: dir, Namespace: "ns"}
	bareMgr := pluginmgmt.Manager{HarnessDir: bare, Namespace: "ns"}
	install := filepath.Join(dir, "scripts", "install.mjs")

	behind := []pluginmgmt.PluginInfo{
		planInfo("guard", "1.4.0", map[string]string{"claude": ""}),
		planInfo("dev-tools", "1.56.0", map[string]string{"claude": "1.55.0", "codex": "1.55.0", "antigravity": "1.0.0", "opencode": "linked", "pi": "linked"}),
		{Name: "ext", UpstreamVersion: "2.0.0", IsStandalone: true, Enabled: map[string]bool{"claude": true}, Installed: map[string]string{"claude": "1.0.0"}},
	}
	current := []pluginmgmt.PluginInfo{
		planInfo("dev-tools", "1.56.0", map[string]string{"claude": "1.56.0", "codex": "1.57.0"}),
	}

	tests := []struct {
		name      string
		mgr       pluginmgmt.Manager
		list      []pluginmgmt.PluginInfo
		harness   string
		lookPath  func(string) (string, error)
		wantChg   []string
		wantSteps []string
		wantSkip  string
		wantNote  string
	}{
		{
			name: "claude installs missing and updates stale", mgr: mgr, list: behind, harness: "claude", lookPath: fakeLookPath(),
			wantChg: []string{"dev-tools 1.55.0 → 1.56.0", "guard missing → 1.4.0"},
			wantSteps: []string{
				"claude plugin marketplace update ns",
				"claude plugin update dev-tools@ns",
				"claude plugin install guard@ns",
			},
		},
		{
			name: "copilot mirrors claude shape", mgr: mgr, list: []pluginmgmt.PluginInfo{planInfo("dev-tools", "2.0.0", map[string]string{"copilot": "1.0.0"})},
			harness: "copilot", lookPath: fakeLookPath(),
			wantChg:   []string{"dev-tools 1.0.0 → 2.0.0"},
			wantSteps: []string{"copilot plugin marketplace update ns", "copilot plugin update dev-tools@ns"},
		},
		{
			name: "codex upgrades marketplace, adds, runs installer", mgr: mgr, list: behind, harness: "codex", lookPath: fakeLookPath(),
			wantChg:   []string{"dev-tools 1.55.0 → 1.56.0"},
			wantSteps: []string{"codex plugin marketplace upgrade ns", "codex plugin add dev-tools@ns", "node " + install + " codex"},
		},
		{
			name: "codex without installer", mgr: bareMgr, list: behind, harness: "codex", lookPath: fakeLookPath(),
			wantChg:   []string{"dev-tools 1.55.0 → 1.56.0"},
			wantSteps: []string{"codex plugin marketplace upgrade ns", "codex plugin add dev-tools@ns"},
		},
		{
			name: "antigravity installs existing plugin dirs only", mgr: mgr, list: behind, harness: "antigravity", lookPath: fakeLookPath(),
			wantChg:   []string{"dev-tools 1.0.0 → 1.56.0"},
			wantSteps: []string{"agy plugin install " + filepath.Join(dir, "plugins", "dev-tools"), "node " + install + " antigravity"},
		},
		{
			name: "antigravity with nothing to install", mgr: bareMgr, list: behind, harness: "antigravity", lookPath: fakeLookPath(),
			wantChg:  []string{"dev-tools 1.0.0 → 1.56.0"},
			wantSkip: "no plugin directory in the checkout",
		},
		{
			name: "opencode runs installer", mgr: mgr, list: []pluginmgmt.PluginInfo{planInfo("dev-tools", "1.56.0", map[string]string{"opencode": ""})},
			harness: "opencode", lookPath: fakeLookPath(),
			wantChg:   []string{"dev-tools missing → 1.56.0"},
			wantSteps: []string{"node " + install + " opencode"},
		},
		{
			name: "opencode without installer", mgr: bareMgr, list: []pluginmgmt.PluginInfo{planInfo("dev-tools", "1.56.0", map[string]string{"opencode": ""})},
			harness: "opencode", lookPath: fakeLookPath(),
			wantChg:  []string{"dev-tools missing → 1.56.0"},
			wantSkip: "no installer in the checkout",
		},
		{
			name: "pi installs the checkout", mgr: mgr, list: []pluginmgmt.PluginInfo{planInfo("dev-tools", "1.56.0", map[string]string{"pi": ""})},
			harness: "pi", lookPath: fakeLookPath(),
			wantChg:   []string{"dev-tools missing → 1.56.0"},
			wantSteps: []string{"pi install " + dir},
		},
		{
			name: "pi without checkout", mgr: pluginmgmt.Manager{Namespace: "ns"}, list: []pluginmgmt.PluginInfo{planInfo("dev-tools", "1.56.0", map[string]string{"pi": ""})},
			harness: "pi", lookPath: fakeLookPath(),
			wantChg:  []string{"dev-tools missing → 1.56.0"},
			wantSkip: "no checkout",
		},
		{
			name: "pi all stale needs no command", mgr: mgr,
			list: []pluginmgmt.PluginInfo{
				planInfo("dev-tools", "1.56.0", map[string]string{"pi": "1.55.0"}),
				planInfo("guard", "1.4.0", map[string]string{"pi": "1.3.0"}),
			},
			harness: "pi", lookPath: fakeLookPath(),
			wantChg:  []string{"dev-tools 1.55.0 → 1.56.0", "guard 1.3.0 → 1.4.0"},
			wantNote: "by the checkout pull",
		},
		{
			name: "pi missing and stale installs the checkout", mgr: mgr,
			list: []pluginmgmt.PluginInfo{
				planInfo("dev-tools", "1.56.0", map[string]string{"pi": "1.55.0"}),
				planInfo("guard", "1.4.0", map[string]string{"pi": ""}),
			},
			harness: "pi", lookPath: fakeLookPath(),
			wantChg:   []string{"dev-tools 1.55.0 → 1.56.0", "guard missing → 1.4.0"},
			wantSteps: []string{"pi install " + dir},
		},
		{
			name: "opencode outdated runs installer", mgr: mgr,
			list:    []pluginmgmt.PluginInfo{planInfo("dev-tools", "1.56.0", map[string]string{"opencode": "outdated"})},
			harness: "opencode", lookPath: fakeLookPath(),
			wantChg:   []string{"dev-tools outdated → 1.56.0"},
			wantSteps: []string{"node " + install + " opencode"},
		},
		{
			name: "opencode outdated without installer", mgr: bareMgr,
			list:    []pluginmgmt.PluginInfo{planInfo("dev-tools", "1.56.0", map[string]string{"opencode": "outdated"})},
			harness: "opencode", lookPath: fakeLookPath(),
			wantChg:  []string{"dev-tools outdated → 1.56.0"},
			wantSkip: "no installer in the checkout",
		},
		{name: "up to date harness is skipped", mgr: mgr, list: current, harness: "codex", lookPath: fakeLookPath(), wantSkip: "up to date"},
		{
			name: "missing binary", mgr: mgr, list: behind, harness: "claude", lookPath: fakeLookPath("claude"),
			wantChg:  []string{"dev-tools 1.55.0 → 1.56.0", "guard missing → 1.4.0"},
			wantSkip: "`claude` not found on PATH",
		},
		{
			name: "antigravity binary is agy", mgr: mgr, list: behind, harness: "antigravity", lookPath: fakeLookPath("agy"),
			wantChg:  []string{"dev-tools 1.0.0 → 1.56.0"},
			wantSkip: "`agy` not found on PATH",
		},
		{
			name: "unsupported harness", mgr: mgr, list: []pluginmgmt.PluginInfo{planInfo("dev-tools", "1.56.0", map[string]string{"example-harness": ""})},
			harness: "example-harness", lookPath: fakeLookPath(),
			wantChg:  []string{"dev-tools missing → 1.56.0"},
			wantSkip: "unsupported harness",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plans := planUpdates(tt.mgr, tt.list, []string{tt.harness}, tt.lookPath)
			if len(plans) != 1 {
				t.Fatalf("got %d plans, want 1", len(plans))
			}
			chg, steps, skip := project(plans[0])
			if diff := cmp.Diff(tt.wantChg, chg); diff != "" {
				t.Errorf("changes (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSteps, steps); diff != "" {
				t.Errorf("steps (-want +got):\n%s", diff)
			}
			if skip != tt.wantSkip {
				t.Errorf("skip = %q, want %q", skip, tt.wantSkip)
			}
			if plans[0].note != tt.wantNote {
				t.Errorf("note = %q, want %q", plans[0].note, tt.wantNote)
			}
		})
	}
}

func TestPlanUpdatesOnlyBehindHarness(t *testing.T) {
	t.Parallel()
	list := []pluginmgmt.PluginInfo{
		planInfo("dev-tools", "1.1.0", map[string]string{"claude": "1.0.0", "codex": "1.1.0"}),
	}
	plans := planUpdates(pluginmgmt.Manager{Namespace: "ns"}, list, []string{"claude", "codex"}, fakeLookPath())
	if len(plans) != 2 {
		t.Fatalf("got %d plans, want 2", len(plans))
	}
	if plans[0].skip != "" || len(plans[0].steps) == 0 {
		t.Errorf("claude should have steps, got %+v", plans[0])
	}
	if plans[1].skip != "up to date" || len(plans[1].steps) != 0 {
		t.Errorf("codex should be skipped as up to date, got %+v", plans[1])
	}
}

func TestPrintUpdateSummary(t *testing.T) {
	t.Parallel()
	behind := []pluginChange{{plugin: "dev-tools", from: "1.0.0", to: "1.1.0"}}
	acting := harnessPlan{harness: "claude", changes: behind}
	current := harnessPlan{harness: "codex", skip: "up to date"}
	noBinary := harnessPlan{harness: "copilot", changes: behind, skip: "`copilot` not found on PATH"}
	tests := []struct {
		name     string
		plans    []harnessPlan
		failed   int
		dryRun   bool
		wantCode int
		want     []string
		notWant  []string
	}{
		{name: "nothing behind", plans: []harnessPlan{current}, want: []string{"nothing to update across 1 harness(es)"}},
		{name: "updated", plans: []harnessPlan{acting, current}, want: []string{"updated 1 harness(es)"}, notWant: []string{"skipped"}},
		{name: "dry run", plans: []harnessPlan{acting}, dryRun: true, want: []string{"would update 1 harness(es)"}},
		{name: "failed", plans: []harnessPlan{acting}, failed: 1, wantCode: 1, want: []string{"updated 0 harness(es)", "1 harness(es) failed to update"}},
		{name: "behind but skipped", plans: []harnessPlan{noBinary, current}, want: []string{"nothing updated", "1 harness(es) behind but skipped"}, notWant: []string{"updated 0"}},
		{name: "partly skipped", plans: []harnessPlan{acting, noBinary}, want: []string{"updated 1 harness(es)", "1 harness(es) behind but skipped"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			d := Deps{Stdout: &out}
			if got := d.printUpdateSummary(term.Style{}, tt.plans, tt.failed, tt.dryRun); got != tt.wantCode {
				t.Errorf("printUpdateSummary() = %d, want %d", got, tt.wantCode)
			}
			for _, w := range tt.want {
				if !strings.Contains(out.String(), w) {
					t.Errorf("output missing %q:\n%s", w, out.String())
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(out.String(), w) {
					t.Errorf("output has %q:\n%s", w, out.String())
				}
			}
		})
	}
}
