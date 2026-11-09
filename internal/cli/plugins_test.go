package cli_test

import (
	"bytes"
	"context"
	"os"
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
	if !strings.Contains(out, "plugin update complete") {
		t.Errorf("expected completion message, got:\n%s", out)
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
