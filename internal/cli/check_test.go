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

// A policy copy that disagrees with the real VS Code settings must fail the
// check, because the source files are found through CHEZMOI_SOURCE_DIR and
// not next to the copy.
func TestCLICheck_FileCopyIsComparedWithRealSource(t *testing.T) {
	t.Parallel()
	homeDir := t.TempDir()
	data, readErr := os.ReadFile(filepath.Join("..", "testdata", "harness-policy.toml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".chezmoidata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".chezmoidata", "harness-policy.toml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, "dot_config", "Code", "User"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, "dot_config", "Code", "User", "settings.json"), []byte(`{"dictation.enabled": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".chezmoitemplates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".chezmoitemplates", "claude-settings.json"), []byte(`.agents.voice.enabled .agents.voice.mode .agents.voice.autoSubmit`), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgToml := `[package]
gopls = {}
pyright = {}
vtsls = {}
rust-analyzer = {}
`
	if err := os.WriteFile(filepath.Join(homeDir, ".chezmoidata", "packages.toml"), []byte(pkgToml), 0o600); err != nil {
		t.Fatal(err)
	}
	text := string(data)
	i := strings.Index(text, "# --- D9")
	if i < 0 {
		t.Fatal("policy has no D9 voice section")
	}
	text = text[:i] + "[harnessPolicy.voice]\nenabled = false\nmode = \"hold\"\nlanguage = \"it\"\n"
	copyPath := filepath.Join(t.TempDir(), "harness-policy.toml")
	if err := os.WriteFile(copyPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "CHEZMOI_SOURCE_DIR" {
				return homeDir
			}
			return ""
		},
	}
	code := d.Run(context.Background(), []string{"check", "-file", copyPath})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "dictation.enabled") {
		t.Errorf("stderr should mention dictation.enabled, got %q", stderr.String())
	}
}
