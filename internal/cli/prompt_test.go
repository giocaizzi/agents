package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// promptEnv is a hermetic home, instructions dir and config file.
type promptEnv struct {
	dir, cfg string
	env      map[string]string
	ran      [][]string
	runErr   error
}

func newPromptEnv(t *testing.T, extra string, files map[string]string) *promptEnv {
	t.Helper()
	root := t.TempDir()
	e := &promptEnv{dir: filepath.Join(root, "instr"), cfg: filepath.Join(root, "agents.toml"), env: map[string]string{"HOME": root}}
	if err := os.MkdirAll(e.dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(e.dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "profile = \"work\"\n[instructions]\ndir = " + quote(e.dir) + "\n" + extra
	if err := os.WriteFile(e.cfg, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

func quote(s string) string { return "'" + s + "'" }

const promptLayers = `
[instructions.vars]
who = "me"
[instructions.layers.core]
[instructions.layers.work]
[instructions.layers.home]
[profiles.work]
instructions = ["core", "work"]
[profiles.home]
instructions = ["core", "home"]
[harness.codex]
instructions = ["core"]
`

func (e *promptEnv) run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	d := Deps{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, GOOS: "linux",
		Getenv:   func(k string) string { return e.env[k] },
		Hostname: func() (string, error) { return "box", nil },
		RunCommand: func(_ context.Context, argv []string, _ io.Reader, _, _ io.Writer) error {
			e.ran = append(e.ran, argv)
			return e.runErr
		},
	}
	args = append(args[:2:2], append([]string{"-file", e.cfg, "-harnesses", "claude,codex"}, args[2:]...)...)
	code = d.Run(t.Context(), args)
	return code, out.String(), errb.String()
}

var allLayerFiles = map[string]string{
	"core.md": "core {{.vars.who}} {{.harness}} {{.profile}} {{.os}} {{.hostname}}\n",
	"work.md": "work\n",
	"home.md": "home\n",
}

func TestPromptGroup(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args []string
		code int
	}{{nil, 0}, {[]string{"-h"}, 0}, {[]string{"bogus"}, 2}} {
		var out, errb bytes.Buffer
		d := Deps{Stdout: &out, Stderr: &errb, Getenv: hermeticGetenv(t, nil)}
		if code := d.Run(t.Context(), append([]string{"prompt"}, tt.args...)); code != tt.code {
			t.Errorf("prompt %v: code=%d, want %d", tt.args, code, tt.code)
		}
		if !strings.Contains(out.String()+errb.String(), "COMMANDS") {
			t.Errorf("prompt %v: no group help", tt.args)
		}
	}
}

func TestPromptEdit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		extra  string
		editor string
		args   []string
		code   int
		want   []string
	}{
		{name: "edit_command", extra: "", args: []string{"core"}, code: 0, want: []string{"ed", "--name", "core", "DIR/core.md"}},
		{name: "editor fallback", editor: "vim -u NONE", args: []string{"work"}, code: 0, want: []string{"vim", "-u", "NONE", "DIR/work.md"}},
		{name: "undeclared layer", editor: "vim", args: []string{"nope"}, code: 1},
		{name: "no editor", args: []string{"core"}, code: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			layers := promptLayers
			if tt.name == "edit_command" {
				layers = strings.Replace(layers, "[instructions.vars]", "edit_command = [\"ed\", \"--name\", \"{name}\", \"{file}\"]\n[instructions.vars]", 1)
			}
			e := newPromptEnv(t, layers, allLayerFiles)
			e.env["EDITOR"] = tt.editor
			code, _, stderr := e.run(t, append([]string{"prompt", "edit"}, tt.args...)...)
			if code != tt.code {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			var want [][]string
			if tt.want != nil {
				argv := make([]string, len(tt.want))
				for i, a := range tt.want {
					argv[i] = strings.ReplaceAll(strings.ReplaceAll(a, "DIR/", e.dir+string(filepath.Separator)), "DIR", e.dir)
				}
				want = [][]string{argv}
			}
			if diff := cmp.Diff(want, e.ran); diff != "" {
				t.Errorf("ran (-want +got):\n%s", diff)
			}
		})
	}
	t.Run("command error", func(t *testing.T) {
		t.Parallel()
		e := newPromptEnv(t, promptLayers, allLayerFiles)
		e.env["EDITOR"] = "vim"
		e.runErr = errors.New("boom")
		if code, _, stderr := e.run(t, "prompt", "edit", "core"); code != 1 || !strings.Contains(stderr, "boom") {
			t.Errorf("code=%d stderr=%q", code, stderr)
		}
	})
}

func TestPromptShow(t *testing.T) {
	t.Parallel()
	e := newPromptEnv(t, promptLayers, allLayerFiles)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"defaults", nil, "core me claude work linux box\n\nwork\n"},
		{"harness and profile", []string{"-profile", "home", "-harness", "codex"}, "core me codex home linux box\n\nhome\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := e.run(t, append([]string{"prompt", "show"}, tt.args...)...)
			if code != 0 || stdout != tt.want {
				t.Errorf("code=%d stdout=%q want %q stderr=%q", code, stdout, tt.want, stderr)
			}
		})
	}
}

func TestPromptMatrix(t *testing.T) {
	t.Parallel()
	e := newPromptEnv(t, promptLayers, allLayerFiles)
	code, stdout, stderr := e.run(t, "prompt", "matrix")
	want := strings.Join([]string{
		"home  claude  core, home",
		"home  codex   core, home",
		"work  claude  core, work",
		"work  codex   core, work",
		"",
	}, "\n")
	if code != 0 || stdout != want {
		t.Errorf("code=%d stdout=%q want %q stderr=%q", code, stdout, want, stderr)
	}
}

func TestPromptCheck(t *testing.T) {
	t.Parallel()
	missingHome := map[string]string{"core.md": allLayerFiles["core.md"], "work.md": "work\n"}
	tests := []struct {
		name       string
		files      map[string]string
		extra      string
		args       []string
		code       int
		stdoutHas  []string
		stderrHas  string
		stdoutNone string
	}{
		{name: "clean", files: allLayerFiles, code: 0, stdoutHas: []string{"✓ home  claude", "✓ work  codex"}},
		{name: "other profile skipped", files: missingHome, code: 0, stdoutHas: []string{"home  claude  home: skipped (not on this machine)", "✓ work  claude"}, stdoutNone: "✓ home"},
		{name: "all makes it an error", files: missingHome, args: []string{"-all"}, code: 1, stderrHas: "home"},
		{name: "active profile missing", files: map[string]string{"core.md": allLayerFiles["core.md"], "home.md": "h"}, code: 1, stderrHas: "work.md"},
		{name: "unknown layer", files: allLayerFiles, extra: "\n[profiles.home2]\ninstructions = [\"ghost\"]\n", code: 1, stderrHas: "ghost"},
		{name: "template error", files: map[string]string{"core.md": "{{.vars.absent}}", "work.md": "w", "home.md": "h"}, code: 1, stderrHas: "absent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPromptEnv(t, promptLayers+tt.extra, tt.files)
			code, stdout, stderr := e.run(t, append([]string{"prompt", "check"}, tt.args...)...)
			if code != tt.code {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			for _, s := range tt.stdoutHas {
				if !strings.Contains(stdout, s) {
					t.Errorf("stdout missing %q: %q", s, stdout)
				}
			}
			if tt.stdoutNone != "" && strings.Contains(stdout, tt.stdoutNone) {
				t.Errorf("stdout has %q: %q", tt.stdoutNone, stdout)
			}
			if !strings.Contains(stderr, tt.stderrHas) {
				t.Errorf("stderr missing %q: %q", tt.stderrHas, stderr)
			}
		})
	}
}
