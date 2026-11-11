//go:build unix

package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/cli"
)

// updateEnv is a registry checkout with origin at 1.1.0, dev-tools enabled for
// claude and codex, and fake claude and codex executables logging their calls.
type updateEnv struct {
	harnessToml, checkout, homeDir, log string
}

func writeFake(t *testing.T, binDir, name, log, exitCode string) {
	t.Helper()
	script := "#!/bin/sh\necho \"" + name + " $*\" >> '" + log + "'\nexit " + exitCode + "\n"
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func setupUpdateEnv(t *testing.T, claudeVer, claudeExit string) updateEnv {
	t.Helper()
	harnessToml, checkout, homeDir := setupGitPluginsFixture(t)
	if err := os.WriteFile(harnessToml, []byte("[harnessPlugins]\ndev-tools = [\"claude\", \"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for dir, ver := range map[string]string{
		filepath.Join(homeDir, ".claude", "plugins", "cache", "example-harness", "dev-tools"): claudeVer,
		filepath.Join(homeDir, ".codex", "plugins", "cache", "example-harness", "dev-tools"):  "1.1.0",
	} {
		if err := os.MkdirAll(filepath.Join(dir, ver), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	binDir := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls.log")
	writeFake(t, binDir, "claude", log, claudeExit)
	writeFake(t, binDir, "codex", log, "0")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return updateEnv{harnessToml: harnessToml, checkout: checkout, homeDir: homeDir, log: log}
}

func (e updateEnv) run(t *testing.T, extra ...string) (code int, out, calls string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "HOME" {
				return e.homeDir
			}
			return ""
		},
	}
	args := append([]string{"plugins", "update", "-file", e.harnessToml, "-harness-dir", e.checkout}, extra...)
	code = d.Run(t.Context(), args)
	data, err := os.ReadFile(e.log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return code, stdout.String() + stderr.String(), string(data)
}

func TestPluginsUpdateActsOnBehindHarnessOnly(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	env := setupUpdateEnv(t, "1.0.0", "0")
	code, out, calls := env.run(t)

	if code != 0 {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
	wantCalls := "claude plugin marketplace update example-harness\nclaude plugin update dev-tools@example-harness\n"
	if calls != wantCalls {
		t.Errorf("calls = %q, want %q", calls, wantCalls)
	}
	for _, want := range []string{"codex: up to date, skipped", "claude: dev-tools 1.0.0 → 1.1.0", "updated 1 harness"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPluginsUpdateNothingBehind(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	env := setupUpdateEnv(t, "1.1.0", "0")
	code, out, calls := env.run(t)

	if code != 0 {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
	if calls != "" {
		t.Errorf("expected no commands, got %q", calls)
	}
	if !strings.Contains(out, "nothing to update") {
		t.Errorf("expected 'nothing to update', got:\n%s", out)
	}
}

func TestPluginsUpdateDryRunListsCommands(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	env := setupUpdateEnv(t, "1.0.0", "0")
	code, out, calls := env.run(t, "-n")

	if code != 0 {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
	if calls != "" {
		t.Errorf("dry-run must not run commands, got %q", calls)
	}
	for _, want := range []string{
		"      claude plugin marketplace update example-harness\n",
		"      claude plugin update dev-tools@example-harness\n",
		"would update 1 harness",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPluginsUpdateHarnessFlagDoesNotForce(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	env := setupUpdateEnv(t, "1.0.0", "0")
	code, out, calls := env.run(t, "-harness", "codex")

	if code != 0 {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
	if calls != "" {
		t.Errorf("expected no commands, got %q", calls)
	}
	if !strings.Contains(out, "nothing to update") {
		t.Errorf("expected 'nothing to update', got:\n%s", out)
	}
}

func TestPluginsUpdateUpstreamUnreachable(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	env := setupUpdateEnv(t, "1.0.0", "0")
	gitIn(t, env.checkout, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	code, out, calls := env.run(t)

	if code != 1 {
		t.Fatalf("code = %d, want 1, output:\n%s", code, out)
	}
	if calls != "" {
		t.Errorf("expected no commands, got %q", calls)
	}
	if !strings.Contains(out, "upstream unreachable") {
		t.Errorf("expected 'upstream unreachable', got:\n%s", out)
	}
}

func TestPluginsUpdateFailingCommand(t *testing.T) { //nolint:paralleltest // t.Setenv forbids parallel
	env := setupUpdateEnv(t, "1.0.0", "1")
	code, out, calls := env.run(t)

	if code != 1 {
		t.Fatalf("code = %d, want 1, output:\n%s", code, out)
	}
	if calls != "claude plugin marketplace update example-harness\n" {
		t.Errorf("expected to stop at the first failing step, got %q", calls)
	}
	for _, want := range []string{"claude: claude plugin marketplace update example-harness failed", "1 harness(es) failed to update"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
