package cli_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/cli"
)

func setupPluginsFixture(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	harnessDir := filepath.Join(tmpDir, "harness")

	// Create harness.toml
	harnessToml := filepath.Join(tmpDir, "harness.toml")
	content := `
[harnessPlugins]
dev-tools = ["antigravity", "claude"]
foundations = ["antigravity"]
`
	if err := os.WriteFile(harnessToml, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create harness upstream plugins
	pDir := filepath.Join(harnessDir, "plugins")
	devToolsDir := filepath.Join(pDir, "dev-tools")
	if err := os.MkdirAll(devToolsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devToolsDir, "plugin.json"), []byte(`{"version": "1.53.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	foundationsDir := filepath.Join(pDir, "foundations")
	if err := os.MkdirAll(foundationsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foundationsDir, "plugin.json"), []byte(`{"version": "1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create antigravity installed plugin
	agyDevTools := filepath.Join(homeDir, ".gemini", "config", "plugins", "dev-tools")
	if err := os.MkdirAll(agyDevTools, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agyDevTools, "plugin.json"), []byte(`{"version": "1.53.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	return harnessToml, harnessDir
}

func TestPluginsList(t *testing.T) {
	t.Parallel()
	harnessToml, harnessDir := setupPluginsFixture(t)

	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "HOME" {
				return filepath.Dir(harnessToml)
			}
			return ""
		},
	}

	code := d.Run(context.Background(), []string{
		"plugins", "list",
		"-file", harnessToml,
		"-harness-dir", harnessDir,
	})

	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "agents plugins") {
		t.Errorf("expected header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "dev-tools") {
		t.Errorf("expected dev-tools in output, got:\n%s", out)
	}
	if !strings.Contains(out, "1.53.0") {
		t.Errorf("expected version 1.53.0 in output, got:\n%s", out)
	}
}

func TestPluginsListJSON(t *testing.T) {
	t.Parallel()
	harnessToml, harnessDir := setupPluginsFixture(t)

	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "HOME" {
				return filepath.Dir(harnessToml)
			}
			return ""
		},
	}

	code := d.Run(context.Background(), []string{
		"plugins", "list",
		"-file", harnessToml,
		"-harness-dir", harnessDir,
		"-json",
	})

	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"dev-tools"`) || !strings.Contains(out, `"1.53.0"`) {
		t.Errorf("expected JSON output with dev-tools, got:\n%s", out)
	}
}

func TestPluginsUpdateDryRun(t *testing.T) {
	t.Parallel()
	harnessToml, harnessDir := setupPluginsFixture(t)

	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "HOME" {
				return filepath.Dir(harnessToml)
			}
			return ""
		},
	}

	code := d.Run(context.Background(), []string{
		"plugins", "update",
		"-file", harnessToml,
		"-harness-dir", harnessDir,
		"-n",
	})

	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "plugins update") {
		t.Errorf("expected update header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "harness(es)") {
		t.Errorf("expected update summary, got:\n%s", out)
	}
}

func TestPluginsPruneDryRun(t *testing.T) {
	t.Parallel()
	harnessToml, _ := setupPluginsFixture(t)

	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "HOME" {
				return filepath.Dir(harnessToml)
			}
			return ""
		},
	}

	code := d.Run(context.Background(), []string{
		"plugins", "prune",
		"-file", harnessToml,
		"-n",
	})

	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "plugins prune") {
		t.Errorf("expected prune header, got:\n%s", out)
	}
}

func TestPluginsListPrefersManagedCustomRegistryAndHonorsOverrides(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	harnessToml := filepath.Join(tmpDir, "harness.toml")
	if err := os.WriteFile(harnessToml, []byte(`
harnessRegistry = "octocat/custom-registry"

[harnessPlugins]
dev-tools = ["claude"]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	writePlugin := func(dir, version string) {
		t.Helper()
		pluginDir := filepath.Join(dir, "plugins", "dev-tools")
		if err := os.MkdirAll(pluginDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"version":"`+version+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	managed := filepath.Join(homeDir, ".local", "share", "octocat-custom-registry")
	developer := filepath.Join(homeDir, "Documents", "custom-registry")
	environment := filepath.Join(tmpDir, "environment")
	explicit := filepath.Join(tmpDir, "explicit")
	writePlugin(managed, "1.0.0")
	writePlugin(developer, "2.0.0")
	writePlugin(environment, "3.0.0")
	writePlugin(explicit, "4.0.0")

	tests := []struct {
		name       string
		args       []string
		harnessDir string
		want       string
	}{
		{"managed custom registry precedes Documents checkout", nil, "", "1.0.0"},
		{"HARNESS_DIR precedes managed registry", nil, environment, "3.0.0"},
		{"harness-dir precedes HARNESS_DIR", []string{"-harness-dir", explicit}, environment, "4.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			d := cli.Deps{
				Stdout: &stdout,
				Stderr: &stderr,
				Getenv: func(k string) string {
					switch k {
					case "HOME":
						return homeDir
					case "HARNESS_DIR":
						return tt.harnessDir
					default:
						return ""
					}
				},
			}

			args := append([]string{"plugins", "list", "-file", harnessToml}, tt.args...)
			if code := d.Run(context.Background(), args); code != 0 {
				t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Errorf("plugins list output = %q, want version %q", stdout.String(), tt.want)
			}
		})
	}
}

func TestPluginsListAutoDiscoverHarnessDirAndNamespace(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")

	// Create harness.toml without harnessRegistry
	harnessToml := filepath.Join(tmpDir, "harness.toml")
	content := `
[harnessPlugins]
dev-tools = ["claude", "copilot"]
`
	if err := os.WriteFile(harnessToml, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create harness repo in ~/src/harness
	harnessDir := filepath.Join(homeDir, "src", "harness")
	pDir := filepath.Join(harnessDir, "plugins", "dev-tools")
	if err := os.MkdirAll(pDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pDir, "plugin.json"), []byte(`{"version": "1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create .claude-plugin/marketplace.json with custom name
	claudePluginDir := filepath.Join(harnessDir, ".claude-plugin")
	if err := os.MkdirAll(claudePluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marketplaceJSON := `{"name": "discovered-marketplace"}`
	if err := os.WriteFile(filepath.Join(claudePluginDir, "marketplace.json"), []byte(marketplaceJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "HOME" {
				return homeDir
			}
			return ""
		},
	}

	// Do NOT pass -harness-dir: discovery should find ~/src/harness and discovered-marketplace namespace
	code := d.Run(context.Background(), []string{
		"plugins", "list",
		"-file", harnessToml,
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "dev-tools") {
		t.Errorf("expected dev-tools in output, got:\n%s", out)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commitPluginVersion(t *testing.T, repo, version string) {
	t.Helper()
	manifest := filepath.Join(repo, "plugins", "dev-tools", ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"version": "`+version+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "release "+version)
	gitIn(t, repo, "push", "-q", "origin", "HEAD:main")
}

// setupGitPluginsFixture returns harness.toml, a registry checkout whose working
// tree says 1.0.0 while origin is at 1.1.0, and a HOME with claude at 1.0.0.
func setupGitPluginsFixture(t *testing.T) (harnessToml, checkout, homeDir string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "octocat")
	t.Setenv("GIT_AUTHOR_EMAIL", "octocat@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "octocat")
	t.Setenv("GIT_COMMITTER_EMAIL", "octocat@example.com")

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	checkout = filepath.Join(root, "checkout")
	homeDir = filepath.Join(root, "home")

	gitIn(t, root, "init", "-q", "--bare", "-b", "main", origin)
	gitIn(t, root, "init", "-q", "-b", "main", seed)
	gitIn(t, seed, "remote", "add", "origin", origin)
	commitPluginVersion(t, seed, "1.0.0")
	gitIn(t, root, "clone", "-q", origin, checkout)
	commitPluginVersion(t, seed, "1.1.0")

	// Pin the claude cache namespace through an untracked marketplace file.
	marketDir := filepath.Join(checkout, ".claude-plugin")
	if err := os.MkdirAll(marketDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marketDir, "marketplace.json"), []byte(`{"name": "example-harness"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(homeDir, ".claude", "plugins", "cache", "example-harness", "dev-tools", "1.0.0"), 0o700); err != nil {
		t.Fatal(err)
	}
	harnessToml = filepath.Join(root, "harness.toml")
	if err := os.WriteFile(harnessToml, []byte("[harnessPlugins]\ndev-tools = [\"claude\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return harnessToml, checkout, homeDir
}

func listGitPlugins(t *testing.T, harnessToml, checkout, homeDir string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "HOME" {
				return homeDir
			}
			return ""
		},
	}
	code := d.Run(t.Context(), []string{
		"plugins", "list",
		"-file", harnessToml,
		"-harness-dir", checkout,
		"-harness", "claude",
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr.String())
	}
	return stdout.String()
}

func TestPluginsListUsesFetchedRemoteTip(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	harnessToml, checkout, homeDir := setupGitPluginsFixture(t)
	out := listGitPlugins(t, harnessToml, checkout, homeDir)

	if !strings.Contains(out, "1.1.0") {
		t.Errorf("expected fetched upstream 1.1.0 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "↑") {
		t.Errorf("expected stale marker in output, got:\n%s", out)
	}
	if !strings.Contains(out, "stale or missing") {
		t.Errorf("expected stale-or-missing warning, got:\n%s", out)
	}
	if strings.Contains(out, "upstream unreachable") {
		t.Errorf("unexpected unreachable warning, got:\n%s", out)
	}
}

func TestPluginsListUpstreamUnreachable(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	harnessToml, checkout, homeDir := setupGitPluginsFixture(t)
	gitIn(t, checkout, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	// Claude has 1.0.0, matching the stale working tree: no row is stale or missing.
	out := listGitPlugins(t, harnessToml, checkout, homeDir)

	if !strings.Contains(out, "upstream unreachable") {
		t.Errorf("expected unreachable warning, got:\n%s", out)
	}
	if !strings.Contains(out, "1.0.0 ?") {
		t.Errorf("expected ' ?' marker after upstream version, got:\n%s", out)
	}
	if strings.Contains(out, "up to date") {
		t.Errorf("must not claim up to date when upstream is unreachable, got:\n%s", out)
	}
	if !strings.Contains(out, "no newer version found in the local checkout") {
		t.Errorf("expected neutral line, got:\n%s", out)
	}
}
