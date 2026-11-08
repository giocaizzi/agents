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
	homeDir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "home"))
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(homeDir); statErr != nil {
		t.Skip("skipping test in standalone repo: home directory not present")
	}
	policyPath := filepath.Join(homeDir, ".chezmoidata", "harness-policy.toml")
	data, readErr := os.ReadFile(policyPath)
	if readErr != nil {
		var fallbackErr error
		data, fallbackErr = os.ReadFile(filepath.Join("..", "testdata", "harness-policy.toml"))
		if fallbackErr != nil {
			t.Fatal(fallbackErr)
		}
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
