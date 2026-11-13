package cli_test

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, "schema_version = 1\nharnesses = [\"claude\", \"codex\"]\n")

	if code := e.run("render", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	all := e.out.String()
	if !strings.Contains(all, "(claude, json") || !strings.Contains(all, "(codex, toml") {
		t.Fatalf("expected claude and codex targets:\n%s", all)
	}

	if code := e.run("render", "-harness", "codex", "-file", p); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	only := e.out.String()
	if strings.Contains(only, "(claude,") || !strings.Contains(only, "(codex,") {
		t.Errorf("-harness codex did not filter:\n%s", only)
	}
	if !strings.HasPrefix(only, "# "+e.home) {
		t.Errorf("header does not name an absolute path under home:\n%s", only)
	}
}

func TestRenderLoadError(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, "bogus = 1\n")
	if code := e.run("render", "-file", p); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}
