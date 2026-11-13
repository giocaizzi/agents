package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/pluginmgmt"
)

// pluginEnv is a hermetic machine: every directory lives under one temp root,
// and the registry acme/harness resolves to a checkout under the data directory.
type pluginEnv struct {
	root, home, checkout, cfg string
}

const pluginConfig = `
[plugins]
registry = "acme/harness"

[plugins.enabled]
dev-tools = ["antigravity", "claude"]
foundations = ["antigravity"]
`

func newPluginEnv(t *testing.T, config string) pluginEnv {
	t.Helper()
	root := t.TempDir()
	e := pluginEnv{
		root:     root,
		home:     filepath.Join(root, "home"),
		checkout: filepath.Join(root, "data", "agents", "registries", "harness"),
		cfg:      filepath.Join(root, "config.toml"),
	}
	if err := os.WriteFile(e.cfg, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e pluginEnv) getenv(k string) string {
	switch k {
	case "HOME":
		return e.home
	case "XDG_DATA_HOME":
		return filepath.Join(e.root, "data")
	case "XDG_STATE_HOME":
		return filepath.Join(e.root, "state")
	case "XDG_CONFIG_HOME":
		return filepath.Join(e.root, "config")
	case "XDG_CACHE_HOME":
		return filepath.Join(e.root, "cache")
	}
	return ""
}

func (e pluginEnv) deps(stdout, stderr *bytes.Buffer) Deps {
	return Deps{Stdout: stdout, Stderr: stderr, Getenv: e.getenv, GOOS: "linux"}
}

// args builds the argv after "plugins", selecting claude and antigravity from this config.
func (e pluginEnv) args(args ...string) []string {
	return append(args, "-file", e.cfg, "-harnesses", "antigravity,claude")
}

func (e pluginEnv) run(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code = e.deps(&stdout, &stderr).Run(t.Context(), append([]string{"plugins"}, e.args(args...)...))
	return code, stdout.String(), stderr.String()
}

func writePluginVersion(t *testing.T, checkout, name, version string) {
	t.Helper()
	writeFile(t, filepath.Join(checkout, "plugins", name, "plugin.json"), `{"version": "`+version+`"}`)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// installedFixture has the registry at 1.53.0 and antigravity current on dev-tools.
func installedFixture(t *testing.T) pluginEnv {
	t.Helper()
	e := newPluginEnv(t, pluginConfig)
	writePluginVersion(t, e.checkout, "dev-tools", "1.53.0")
	writePluginVersion(t, e.checkout, "foundations", "1.0.0")
	writeFile(t, filepath.Join(e.home, ".gemini", "config", "plugins", "dev-tools", "plugin.json"), `{"version": "1.53.0"}`)
	return e
}

func TestPluginsList(t *testing.T) {
	t.Parallel()
	e := installedFixture(t)
	code, out, errOut := e.run(t, "list")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, errOut)
	}
	for _, want := range []string{"agents plugins", "dev-tools", "1.53.0", "ANTIGRAVITY", "CLAUDE"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPluginsListJSON(t *testing.T) {
	t.Parallel()
	e := installedFixture(t)
	code, out, errOut := e.run(t, "list", "-json")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, errOut)
	}
	var list []pluginmgmt.PluginInfo
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if len(list) != 2 || list[0].Name != "dev-tools" || list[0].UpstreamVersion != "1.53.0" {
		t.Errorf("list = %+v", list)
	}
}

func TestPluginsListMarksStaleAndDisabled(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, `
[plugins]
registry = "acme/harness"
disabled = ["foundations"]

[plugins.enabled]
dev-tools = ["claude"]
foundations = ["claude"]
`)
	writePluginVersion(t, e.checkout, "dev-tools", "1.2.0")
	writePluginVersion(t, e.checkout, "foundations", "1.0.0")
	if err := os.MkdirAll(filepath.Join(e.home, ".claude", "plugins", "cache", "acme-harness", "dev-tools", "1.1.0"), 0o700); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := e.run(t, "list", "-harness", "claude")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, errOut)
	}
	for _, want := range []string{"↑ 1.1.0", "stale or missing"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "✗ missing") {
		t.Errorf("disabled plugin reported missing:\n%s", out)
	}
}

func TestPluginsListWithoutCheckoutShowsConfiguredPlugins(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, pluginConfig)
	code, out, errOut := e.run(t, "list")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, errOut)
	}
	for _, want := range []string{"dev-tools", "foundations"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPluginsListHarnessDirOverridesCheckout(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, pluginConfig)
	writePluginVersion(t, e.checkout, "dev-tools", "1.0.0")
	dev := filepath.Join(e.root, "dev-checkout")
	writePluginVersion(t, dev, "dev-tools", "4.0.0")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"default checkout", nil, "1.0.0"},
		{"harness-dir", []string{"-harness-dir", dev}, "4.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, out, errOut := e.run(t, append([]string{"list"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("code = %d, stderr: %s", code, errOut)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("output missing %q:\n%s", tt.want, out)
			}
		})
	}
}

func TestPluginsListConfigError(t *testing.T) {
	t.Parallel()
	e := newPluginEnv(t, "not = [valid")
	if code, _, errOut := e.run(t, "list"); code != exitFindings || !strings.Contains(errOut, "plugins:") {
		t.Errorf("code = %d, stderr = %q; want exit 1 with a plugins error", code, errOut)
	}
}

func TestPluginsPrune(t *testing.T) {
	t.Parallel()
	e := installedFixture(t)
	cache := filepath.Join(e.home, ".claude", "plugins", "cache", "acme-harness", "dev-tools")
	for _, v := range []string{"1.0.0", "1.1.0"} {
		if err := os.MkdirAll(filepath.Join(cache, v), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	code, out, errOut := e.run(t, "prune", "-n")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "plugins prune") || !strings.Contains(out, "would remove "+filepath.Join(cache, "1.0.0")) {
		t.Errorf("dry run output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(cache, "1.0.0")); err != nil {
		t.Errorf("dry run removed the cache: %v", err)
	}

	if code, _, errOut := e.run(t, "prune"); code != 0 {
		t.Fatalf("prune code = %d, stderr: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(cache, "1.0.0")); !os.IsNotExist(err) {
		t.Errorf("superseded cache still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "1.1.0")); err != nil {
		t.Errorf("newest cache removed: %v", err)
	}
}
