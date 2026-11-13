package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApply_WritesThenIsUnchanged(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	if code := e.run("apply", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	settings := filepath.Join(e.home, ".claude", "settings.json")
	if !strings.Contains(e.out.String(), "create "+settings) {
		t.Errorf("out = %s", e.out)
	}
	if _, err := os.Stat(settings); err != nil {
		t.Fatal(err)
	}
	if code := e.run("apply", "-file", p); code != 0 {
		t.Fatalf("second exit = %d", code)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(e.out.String()), "\n") {
		if !strings.HasPrefix(line, "unchanged ") {
			t.Errorf("second run line %q is not unchanged", line)
		}
	}
}

func TestApply_DryRunWritesNothingAndPrintsADiff(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	if code := e.run("apply", "-n", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".claude")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry run created files: %v", err)
	}
	if !strings.Contains(e.out.String(), "create ") || !strings.Contains(e.out.String(), "+") {
		t.Errorf("no diff printed:\n%s", e.out)
	}
}

func TestApply_RefusedSymlinkFails(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	target := filepath.Join(e.home, "target.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(e.home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(e.home, ".claude", "settings.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if code := e.run("apply", "-file", p); code != 1 {
		t.Fatalf("exit = %d, out = %s", code, e.out)
	}
	if !strings.Contains(e.out.String(), "refused ") {
		t.Errorf("out = %s", e.out)
	}
}
