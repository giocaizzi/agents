package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/cli"
	"github.com/giocaizzi/agents/internal/config"
)

// stdEnv is a hermetic machine: a temp home, no XDG variables, no tools.
type stdEnv struct {
	deps   cli.Deps
	out    *bytes.Buffer
	errOut *bytes.Buffer
	home   string
}

func newStdEnv(t *testing.T, stdin string) *stdEnv {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{"HOME": home}
	e := &stdEnv{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, home: home}
	e.deps = cli.Deps{
		Stdin:    strings.NewReader(stdin),
		Stdout:   e.out,
		Stderr:   e.errOut,
		Getenv:   func(k string) string { return env[k] },
		GOOS:     "linux",
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Hostname: func() (string, error) { return "testhost", nil },
	}
	return e
}

func (e *stdEnv) run(args ...string) int {
	e.out.Reset()
	e.errOut.Reset()
	return e.deps.Run(context.Background(), args)
}

// writeConfig writes a config file under the home and returns its path.
func (e *stdEnv) writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(e.home, "cfg.toml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *stdEnv) userConfig() string {
	return filepath.Join(e.home, ".config", "agents", "config.toml")
}

func TestInit_CreatesTheStarterAndDetectsHarnesses(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if err := os.Mkdir(filepath.Join(e.home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if code := e.run("init"); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, e.errOut)
	}
	data, err := os.ReadFile(e.userConfig())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if first, _, _ := strings.Cut(text, "\n"); first != "#:schema "+config.SchemaID {
		t.Errorf("first line = %q", first)
	}
	if !strings.Contains(text, "schema_version = 1") || !strings.Contains(text, `harnesses = ["claude"]`) {
		t.Errorf("starter lacks schema_version or detected harnesses:\n%s", text)
	}
	if runtime.GOOS != "windows" {
		assertMode(t, e.userConfig(), 0o600)
		assertMode(t, filepath.Dir(e.userConfig()), 0o700)
	}
	var cfg config.Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if fs := config.Validate(cfg); len(fs) > 0 {
		t.Errorf("starter does not validate: %v", fs)
	}
}

func TestInit_IsIdempotentAndForceOverwrites(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if code := e.run("init"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if err := os.WriteFile(e.userConfig(), []byte("schema_version = 1\n# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := e.run("init"); code != 0 || !strings.Contains(e.out.String(), "already exists") {
		t.Fatalf("second init: exit = %d, out = %q", code, e.out)
	}
	if got, _ := os.ReadFile(e.userConfig()); !strings.Contains(string(got), "# mine") {
		t.Errorf("existing file was changed:\n%s", got)
	}
	if code := e.run("init", "-force"); code != 0 {
		t.Fatalf("init -force exit = %d, stderr = %s", code, e.errOut)
	}
	if got, _ := os.ReadFile(e.userConfig()); strings.Contains(string(got), "# mine") {
		t.Errorf("-force did not overwrite:\n%s", got)
	}
}

func TestInit_PrintTouchesNothing(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if code := e.run("init", "-print"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.HasPrefix(e.out.String(), "#:schema "+config.SchemaID+"\n") {
		t.Errorf("stdout = %q", e.out)
	}
	if _, err := os.Stat(e.userConfig()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("-print wrote the file: %v", err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}
