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
