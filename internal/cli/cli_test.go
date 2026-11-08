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

func TestCLIRun_Help(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	code := d.Run(context.Background(), []string{"help"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "agents") || !strings.Contains(stdout.String(), "USAGE") {
		t.Errorf("expected usage output, got %q", stdout.String())
	}
}

func TestCLIRun_Version(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  &stderr,
		Version: "0.2.0",
	}

	code := d.Run(context.Background(), []string{"version"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "agents 0.2.0") {
		t.Errorf("expected version output, got %q", stdout.String())
	}
}

func TestCLIRun_CheckAndRender(t *testing.T) {
	t.Parallel()
	homeDir := filepath.Join("..", "..", "..", "..", "home")
	if _, err := os.Stat(homeDir); err != nil {
		t.Skip("skipping in standalone repo")
	}
	policyPath := filepath.Join(homeDir, ".chezmoidata", "harness-policy.toml")
	harnessTomlPath := filepath.Join(homeDir, ".chezmoidata", "harness.toml")
	if _, err := os.Stat(policyPath); err != nil {
		t.Skip("skipping test; harness-policy.toml not accessible at relative path")
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

	// 1. Check policy file explicitly
	code := d.Run(context.Background(), []string{"check", "-file", policyPath})
	if code != 0 {
		t.Fatalf("expected check policy exit 0, got %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "✓ policy & harness parity") {
		t.Errorf("expected check success output, got %s", stdout.String())
	}

	// 2. Check plugins file explicitly (if harness upstream accessible)
	if _, err := os.Stat(harnessTomlPath); err == nil {
		stdout.Reset()
		stderr.Reset()
		code = d.Run(context.Background(), []string{"check", "-file", harnessTomlPath})
		if code == 0 && !strings.Contains(stdout.String(), "✓ harness plugins") {
			t.Errorf("expected harness plugins success, got %s", stdout.String())
		}
	}

	// 3. Render pi
	stdout.Reset()
	stderr.Reset()
	code = d.Run(context.Background(), []string{"render", "-file", policyPath, "-harness", "pi"})
	if code != 0 {
		t.Fatalf("expected render exit 0, got %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "defaultProvider") {
		t.Errorf("expected pi output, got %s", stdout.String())
	}

	// 4. Prompt checks
	stdout.Reset()
	stderr.Reset()
	code = d.Run(context.Background(), []string{"prompt", "-profile", "personal", "-check"})
	if code != 0 {
		t.Fatalf("expected prompt check exit 0, got %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "✓ prompt") {
		t.Errorf("expected prompt check success output, got %s", stdout.String())
	}

	// 5. Prompt check with -file
	stdout.Reset()
	stderr.Reset()
	corePath := filepath.Join(homeDir, ".chezmoitemplates", "agents", "core.md")
	code = d.Run(context.Background(), []string{"prompt", "-profile", "personal", "-check", "-file", corePath})
	if code != 0 {
		t.Fatalf("expected prompt check -file exit 0, got %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "✓ prompt") {
		t.Errorf("expected prompt check success output, got %s", stdout.String())
	}
}
