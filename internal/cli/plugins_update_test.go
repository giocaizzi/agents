package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/term"
)

var clock = time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)

// fakeRunner records argv and answers from fail; onRun may create files.
type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]error // keyed by the first two argv words
	onRun func(name string, args []string)
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, line)
	if f.onRun != nil {
		f.onRun(name, args)
	}
	for prefix, err := range f.fail {
		if strings.HasPrefix(line, prefix) {
			return []byte("boom"), err
		}
	}
	return nil, nil
}

func (e pluginEnv) stamp() string {
	return filepath.Join(e.root, "state", "agents", "registries", "acme", "harness.pulled")
}

func (e pluginEnv) setStamp(t *testing.T, age time.Duration) {
	t.Helper()
	writeFile(t, e.stamp(), "")
	mtime := clock.Add(-age)
	if err := os.Chtimes(e.stamp(), mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func (e pluginEnv) sync(dir string, dryRun, force bool, f *fakeRunner) (string, error) {
	var out bytes.Buffer
	d := Deps{Stdout: &out, Stderr: &out, Getenv: func(string) string { return "" }}
	err := d.syncCheckout(context.Background(), term.Style{}, checkoutSync{
		run: f.run, now: func() time.Time { return clock },
		registry: "acme/harness", dir: dir, stamp: e.stamp(), dryRun: dryRun, force: force,
	})
	return out.String(), err
}

func TestSyncCheckout(t *testing.T) {
	t.Parallel()
	pull := func(dir string) string { return "git -C " + dir + " pull --ff-only" }
	tests := []struct {
		name       string
		gitDir     bool // the checkout exists with a .git directory
		plainDir   bool // the checkout exists without .git
		stampAge   time.Duration
		dryRun     bool
		force      bool
		wantCalls  func(dir string) []string
		wantStamp  bool // a stamp exists afterwards
		wantFresh  bool // the stamp was touched to the clock
		wantOutput string
	}{
		{
			name:      "missing checkout is cloned",
			wantCalls: func(dir string) []string { return []string{"git clone https://github.com/acme/harness " + dir} },
			wantStamp: true, wantFresh: true, wantOutput: "cloned",
		},
		{
			name: "missing checkout dry run only prints", dryRun: true,
			wantCalls:  func(string) []string { return nil },
			wantOutput: "[dry-run] would run git clone https://github.com/acme/harness",
		},
		{
			name: "pull when there is no stamp", gitDir: true,
			wantCalls: func(dir string) []string { return []string{pull(dir)} },
			wantStamp: true, wantFresh: true,
		},
		{
			name: "pull throttled within 24h", gitDir: true, stampAge: time.Hour,
			wantCalls:  func(string) []string { return nil },
			wantStamp:  true,
			wantOutput: "less than 24h ago",
		},
		{
			name: "pull after 24h", gitDir: true, stampAge: 25 * time.Hour,
			wantCalls: func(dir string) []string { return []string{pull(dir)} },
			wantStamp: true, wantFresh: true,
		},
		{
			name: "force pulls inside the window", gitDir: true, stampAge: time.Hour, force: true,
			wantCalls: func(dir string) []string { return []string{pull(dir)} },
			wantStamp: true, wantFresh: true,
		},
		{
			name: "dry run pull leaves the stamp", gitDir: true, stampAge: 48 * time.Hour, dryRun: true,
			wantCalls:  func(string) []string { return nil },
			wantStamp:  true,
			wantOutput: "[dry-run] would run git -C",
		},
		{
			name: "plain directory is left alone", plainDir: true,
			wantCalls: func(string) []string { return nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPluginEnv(t, pluginConfig)
			if tt.gitDir {
				mkdir(t, filepath.Join(e.checkout, ".git"))
			}
			if tt.plainDir {
				mkdir(t, e.checkout)
			}
			if tt.stampAge > 0 {
				e.setStamp(t, tt.stampAge)
			}
			f := &fakeRunner{}
			out, err := e.sync(e.checkout, tt.dryRun, tt.force, f)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.wantCalls(e.checkout), f.calls); diff != "" {
				t.Errorf("calls (-want +got):\n%s", diff)
			}
			if !strings.Contains(out, tt.wantOutput) {
				t.Errorf("output %q missing %q", out, tt.wantOutput)
			}
			checkStamp(t, e.stamp(), tt.wantStamp, tt.wantFresh)
		})
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

// checkStamp asserts whether the stamp exists and whether it was touched to the clock.
func checkStamp(t *testing.T, path string, want, fresh bool) {
	t.Helper()
	info, err := os.Stat(path)
	if (err == nil) != want {
		t.Fatalf("stamp exists = %v, want %v (%v)", err == nil, want, err)
	}
	if !want {
		return
	}
	if touched := info.ModTime().Equal(clock); touched != fresh {
		t.Errorf("stamp touched = %v (mtime %v), want %v", touched, info.ModTime(), fresh)
	}
}

func TestSyncCheckoutHarnessDirOverride(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, pluginConfig)
	dev := filepath.Join(e.root, "dev-checkout")
	if err := os.MkdirAll(filepath.Join(dev, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{}
	if _, err := e.sync(dev, false, false, f); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"git -C " + dev + " pull --ff-only"}, f.calls); diff != "" {
		t.Errorf("calls (-want +got):\n%s", diff)
	}
}

func TestSyncCheckoutFailures(t *testing.T) {
	t.Parallel()
	t.Run("failed pull warns and keeps the stamp", func(t *testing.T) {
		t.Parallel()
		e := newPluginEnv(t, pluginConfig)
		if err := os.MkdirAll(filepath.Join(e.checkout, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		e.setStamp(t, 48*time.Hour)
		out, err := e.sync(e.checkout, false, false, &fakeRunner{fail: map[string]error{"git": errors.New("exit 1")}})
		if err != nil || !strings.Contains(out, "pull failed (boom)") {
			t.Errorf("err = %v, output %q", err, out)
		}
		if info, _ := os.Stat(e.stamp()); info.ModTime().Equal(clock) {
			t.Error("stamp touched after a failed pull")
		}
	})
	t.Run("failed clone is an error", func(t *testing.T) {
		t.Parallel()
		e := newPluginEnv(t, pluginConfig)
		_, err := e.sync(e.checkout, false, false, &fakeRunner{fail: map[string]error{"git clone": errors.New("exit 128")}})
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("err = %v, want clone failure with output", err)
		}
		if _, statErr := os.Stat(e.stamp()); statErr == nil {
			t.Error("stamp written after a failed clone")
		}
	})
}

// updateRun runs plugins update with a fake runner and a fake claude on PATH.
func (e pluginEnv) updateRun(t *testing.T, f *fakeRunner, extra ...string) (code int, out string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	d := e.deps(&stdout, &stderr)
	d.LookPath = func(bin string) (string, error) { return "/bin/" + bin, nil }
	code = d.pluginsUpdate(t.Context(), e.args(extra...), f.run, func() time.Time { return clock })
	return code, stdout.String() + stderr.String()
}

func TestPluginsUpdateClonesThenActsOnBehindHarness(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, pluginConfig)
	f := &fakeRunner{onRun: func(name string, args []string) {
		if name == "git" && args[0] == "clone" {
			writePluginVersion(t, e.checkout, "dev-tools", "1.1.0")
			writePluginVersion(t, e.checkout, "foundations", "1.0.0")
		}
	}}
	if err := os.MkdirAll(filepath.Join(e.home, ".claude", "plugins", "cache", "acme-harness", "dev-tools", "1.0.0"), 0o700); err != nil {
		t.Fatal(err)
	}

	code, out := e.updateRun(t, f, "-harness", "claude")
	if code != 0 {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
	want := []string{
		"git clone https://github.com/acme/harness " + e.checkout,
		"claude plugin marketplace update acme-harness",
		"claude plugin update dev-tools@acme-harness",
	}
	if diff := cmp.Diff(want, f.calls); diff != "" {
		t.Errorf("calls (-want +got):\n%s", diff)
	}
	for _, w := range []string{"cloned the registry checkout", "claude: dev-tools 1.0.0 → 1.1.0", "updated 1 harness"} {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
	if _, err := os.Stat(e.stamp()); err != nil {
		t.Errorf("pull stamp not keyed by owner/repo: %v", err)
	}
}

func TestPluginsUpdateDryRunRunsNothing(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, pluginConfig)
	f := &fakeRunner{}
	code, out := e.updateRun(t, f, "-n")
	if code != 0 || len(f.calls) != 0 {
		t.Fatalf("code = %d, calls = %v, output:\n%s", code, f.calls, out)
	}
	if !strings.Contains(out, "[dry-run] would run git clone https://github.com/acme/harness "+e.checkout) {
		t.Errorf("output missing clone preview:\n%s", out)
	}
	if _, err := os.Stat(e.stamp()); err == nil {
		t.Error("dry run wrote the stamp")
	}
}

func TestPluginsUpdateCloneFailure(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, pluginConfig)
	code, out := e.updateRun(t, &fakeRunner{fail: map[string]error{"git clone": errors.New("exit 128")}})
	if code != exitFindings || !strings.Contains(out, "git clone") || !strings.Contains(out, "boom") {
		t.Errorf("code = %d, output:\n%s", code, out)
	}
}

func TestPluginsUpdateNeedsARegistry(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, "[plugins.enabled]\ndev-tools = [\"claude\"]\n")
	f := &fakeRunner{}
	code, out := e.updateRun(t, f)
	if code != exitFindings || !strings.Contains(out, "no plugin registry configured") || len(f.calls) != 0 {
		t.Errorf("code = %d, calls = %v, output:\n%s", code, f.calls, out)
	}
}

func TestPluginsUpdateHarnessDirFeedsInstaller(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, pluginConfig)
	dev := filepath.Join(e.root, "dev-checkout")
	writePluginVersion(t, dev, "dev-tools", "1.1.0")
	writeFile(t, filepath.Join(dev, "scripts", "install.mjs"), "")
	f := &fakeRunner{}
	code, out := e.updateRun(t, f, "-harness-dir", dev, "-harness", "antigravity")
	if code != 0 {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
	want := []string{
		"agy plugin install " + filepath.Join(dev, "plugins", "dev-tools"),
		"agy plugin install " + filepath.Join(dev, "plugins", "foundations"),
		"node " + filepath.Join(dev, "scripts", "install.mjs") + " antigravity",
	}
	for _, w := range want[:1] {
		if !strings.Contains(strings.Join(f.calls, "\n"), w) {
			t.Errorf("calls %v missing %q", f.calls, w)
		}
	}
	if last := f.calls[len(f.calls)-1]; last != want[2] {
		t.Errorf("last call = %q, want %q", last, want[2])
	}
	if _, err := os.Stat(e.checkout); err == nil {
		t.Error("default checkout created despite -harness-dir")
	}
}

func TestPluginsUpdateFailingStepStops(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, pluginConfig)
	writePluginVersion(t, e.checkout, "dev-tools", "1.1.0")
	writePluginVersion(t, e.checkout, "foundations", "1.0.0")
	if err := os.MkdirAll(filepath.Join(e.home, ".claude", "plugins", "cache", "acme-harness", "dev-tools", "1.0.0"), 0o700); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{fail: map[string]error{"claude": errors.New("exit 1")}}
	code, out := e.updateRun(t, f, "-harness", "claude")
	if code != exitFindings {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
	if diff := cmp.Diff([]string{"claude plugin marketplace update acme-harness"}, f.calls); diff != "" {
		t.Errorf("calls (-want +got):\n%s", diff)
	}
	if !strings.Contains(out, "1 harness(es) failed to update") {
		t.Errorf("output missing failure summary:\n%s", out)
	}
}
