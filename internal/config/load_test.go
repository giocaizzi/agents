package config_test

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

// fakeFS serves files and directories from memory.
type fakeFS struct {
	files map[string]string
	dirs  map[string]bool
}

func newFS(files map[string]string, dirs ...string) *fakeFS {
	f := &fakeFS{files: map[string]string{}, dirs: map[string]bool{}}
	for k, v := range files {
		f.files[filepath.Clean(k)] = v
	}
	for _, d := range dirs {
		f.dirs[filepath.Clean(d)] = true
	}
	return f
}

type fakeInfo struct {
	name string
	dir  bool
}

func (i fakeInfo) Name() string     { return i.name }
func (fakeInfo) Size() int64        { return 0 }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool      { return i.dir }
func (fakeInfo) Sys() any           { return nil }

func (f *fakeFS) Stat(p string) (fs.FileInfo, error) {
	p = filepath.Clean(p)
	if _, ok := f.files[p]; ok {
		return fakeInfo{name: filepath.Base(p)}, nil
	}
	if f.dirs[p] {
		return fakeInfo{name: filepath.Base(p), dir: true}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

func (f *fakeFS) ReadFile(p string) ([]byte, error) {
	s, ok := f.files[filepath.Clean(p)]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	return []byte(s), nil
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func p(parts ...string) string {
	return filepath.Join(append([]string{string(filepath.Separator)}, parts...)...)
}

const userCfg = `
schema_version = 1
profile = "dev"

[ui]
theme = "dark"
progress_bar = true

[permissions]
allow_commands = ["ls", "cat"]
deny_read = ["a"]

[instructions.layers.core]
file = "core.md"

[harness.claude]
default_mode = "plan"
instructions = ["core"]

[os.windows.ui]
theme = "win"

[os.linux.ui]
theme = "linux"

[profiles.dev]
instructions = ["core"]

[profiles.dev.ui]
theme = "profile"
output_style = "terse"

[profiles.dev.harness.claude]
instructions = ["core", "extra"]
default_mode = "auto"

[profiles.other.ui]
theme = "other"
`

func load(t *testing.T, fsys *fakeFS, mut func(*config.Options), envs map[string]string) (*config.Resolved, error) {
	t.Helper()
	o := config.Options{
		Getenv:   env(envs),
		GOOS:     "darwin",
		Home:     p("home", "u"),
		Stat:     fsys.Stat,
		ReadFile: fsys.ReadFile,
	}
	if mut != nil {
		mut(&o)
	}
	return config.Load(o)
}

func standardFS() *fakeFS {
	return newFS(map[string]string{
		p("home", "u", ".config", "agents", "config.toml"): userCfg,
	}, p("repo", ".git"))
}

func TestLoadPrecedenceChain(t *testing.T) {
	t.Parallel()
	user := p("home", "u", ".config", "agents", "config.toml")

	r, err := load(t, standardFS(), func(o *config.Options) { o.GOOS = "linux" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Files, []string{user}; !slices.Equal(got, want) {
		t.Errorf("Files = %v, want %v", got, want)
	}
	if r.Profile != "dev" {
		t.Errorf("Profile = %q, want dev", r.Profile)
	}
	// defaults < user < os linux < profile dev
	want := map[string]string{
		"schema_version":              user,
		"instructions.dir":            "default",
		"ui.theme":                    "profile dev",
		"ui.output_style":             "profile dev",
		"ui.progress_bar":             user,
		"permissions.allow_commands":  user,
		"permissions.deny_read":       user,
		"harness.claude.default_mode": "profile dev",
		"profile":                     user,
	}
	for k, v := range want {
		if got := r.Origin[k]; got != v {
			t.Errorf("Origin[%s] = %q, want %q", k, got, v)
		}
	}
	c := r.Config
	if c.UI.Theme != "profile" || c.UI.OutputStyle != "terse" || !c.UI.ProgressBar {
		t.Errorf("ui = %+v", c.UI)
	}
	if got, want := c.Permissions.AllowCommands, []string{"ls", "cat"}; !slices.Equal(got, want) {
		t.Errorf("allow_commands = %v, want %v", got, want)
	}
	if c.Instructions.Dir != filepath.Join(r.Dirs.Config, "instructions") {
		t.Errorf("instructions.dir = %q", c.Instructions.Dir)
	}
}

func TestLoadOSOverlayOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, goos, profile, wantTheme, wantOrigin string
	}{
		{"linux overlay loses to profile", "linux", "", "profile", "profile dev"},
		{"windows overlay", "windows", "other", "other", "profile other"},
		{"no overlay for darwin", "darwin", "", "profile", "profile dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := load(t, standardFS(), func(o *config.Options) {
				o.GOOS = tt.goos
				o.Profile = tt.profile
				o.ConfigPath = p("home", "u", ".config", "agents", "config.toml")
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if r.Config.UI.Theme != tt.wantTheme || r.Origin["ui.theme"] != tt.wantOrigin {
				t.Errorf("theme = %q from %q, want %q from %q", r.Config.UI.Theme, r.Origin["ui.theme"], tt.wantTheme, tt.wantOrigin)
			}
		})
	}
}

func TestLoadOSOverlayWithoutProfile(t *testing.T) {
	t.Parallel()
	{
		fsys := newFS(map[string]string{p("c.toml"): `
[os.windows.ui]
theme = "win"
[os.windows.harness.claude]
instructions = ["x"]
default_mode = "plan"
[os.linux.ui]
theme = "linux"
[ui]
theme = "base"
`})
		for goos, want := range map[string]string{"windows": "win", "linux": "linux", "darwin": "base"} {
			r, err := load(t, fsys, func(o *config.Options) { o.GOOS = goos; o.ConfigPath = p("c.toml") }, nil)
			if err != nil {
				t.Fatal(err)
			}
			if r.Config.UI.Theme != want {
				t.Errorf("%s: theme = %q, want %q", goos, r.Config.UI.Theme, want)
			}
			if goos == "windows" {
				if got := r.Origin["ui.theme"]; got != "os windows" {
					t.Errorf("origin = %q, want os windows", got)
				}
				if r.Config.Harness.Claude.DefaultMode != "plan" || len(r.Config.Harness.Claude.Instructions) != 0 {
					t.Errorf("os overlay harness = %+v (instructions must be stripped)", r.Config.Harness.Claude)
				}
			}
		}
	}
}

func TestLoadProfileOverlayStripsInstructions(t *testing.T) {
	t.Parallel()
	r, err := load(t, standardFS(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Config.Harness.Claude.Instructions, []string{"core"}; !slices.Equal(got, want) {
		t.Errorf("harness.claude.instructions = %v, want the base list %v", got, want)
	}
	if got, want := r.Config.Profiles["dev"].Instructions, []string{"core"}; !slices.Equal(got, want) {
		t.Errorf("profiles.dev.instructions = %v, want %v (kept as data)", got, want)
	}
	if got, want := r.Config.Profiles["dev"].Harness.Claude.Instructions, []string{"core", "extra"}; !slices.Equal(got, want) {
		t.Errorf("profiles.dev.harness.claude.instructions = %v, want %v", got, want)
	}
	if r.Config.Harness.Claude.DefaultMode != "auto" {
		t.Errorf("overlay scalar not applied: %q", r.Config.Harness.Claude.DefaultMode)
	}
}

func TestLoadEnvAndFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		env           map[string]string
		mut           func(*config.Options)
		wantProfile   string
		wantTheme     string
		wantHarnesses []config.Harness
		wantOrigin    map[string]string
	}{
		{
			name:          "env profile and harnesses",
			env:           map[string]string{"AGENTS_PROFILE": "other", "AGENTS_HARNESSES": "claude, codex,"},
			wantProfile:   "other",
			wantTheme:     "other",
			wantHarnesses: []config.Harness{config.Claude, config.Codex},
			wantOrigin:    map[string]string{"profile": "env AGENTS_PROFILE", "harnesses": "env AGENTS_HARNESSES"},
		},
		{
			name:          "flags beat env",
			env:           map[string]string{"AGENTS_PROFILE": "other", "AGENTS_HARNESSES": "claude"},
			mut:           func(o *config.Options) { o.Profile = "dev"; o.Harnesses = []string{"pi", "vscode"} },
			wantProfile:   "dev",
			wantTheme:     "profile",
			wantHarnesses: []config.Harness{config.Pi, config.VSCode},
			wantOrigin:    map[string]string{"profile": "flag -profile", "harnesses": "flag -harnesses"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := load(t, standardFS(), tt.mut, tt.env)
			if err != nil {
				t.Fatal(err)
			}
			if r.Profile != tt.wantProfile || r.Config.Profile != tt.wantProfile || r.Config.UI.Theme != tt.wantTheme {
				t.Errorf("profile = %q/%q theme = %q, want %q/%q", r.Profile, r.Config.Profile, r.Config.UI.Theme, tt.wantProfile, tt.wantTheme)
			}
			if !slices.Equal(r.Harnesses, tt.wantHarnesses) || r.HarnessesDetected {
				t.Errorf("harnesses = %v detected=%v, want %v", r.Harnesses, r.HarnessesDetected, tt.wantHarnesses)
			}
			if diff := cmp.Diff(tt.wantOrigin, map[string]string{"profile": r.Origin["profile"], "harnesses": r.Origin["harnesses"]}); diff != "" {
				t.Errorf("origin (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLoadIgnoresProjectFile(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		p("repo", "agents.toml"):        "[ui]\ntheme = \"root\"\n",
		p("repo", "sub", "agents.toml"): "[ui]\ntheme = \"here\"\n",
	}
	r, err := load(t, newFS(files, p("repo", ".git")), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Config.UI.Theme != "" || len(r.Files) != 0 {
		t.Errorf("theme = %q, files = %v: an agents.toml in the working directory must be ignored", r.Config.UI.Theme, r.Files)
	}
}

func TestLoadMergeRule(t *testing.T) {
	t.Parallel()
	fsys := newFS(map[string]string{
		p("a.toml"): "profile = \"p\"\n[models.tiers.fast]\nclaude = \"m1\"\neffort = \"low\"\n[models.catalog]\nproviders = [\"x\", \"y\"]\n[harness.claude.settings]\nk = 1\n" +
			"[profiles.p.models.tiers.fast]\nclaude = \"m2\"\n[profiles.p.models.tiers.deep]\nclaude = \"m3\"\n[profiles.p.models.catalog]\nproviders = [\"z\"]\n[profiles.p.harness.claude.settings]\nj = \"v\"\n",
	})
	r, err := load(t, fsys, func(o *config.Options) { o.ConfigPath = p("a.toml") }, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]config.Tier{"fast": {Claude: "m2", Effort: "low"}, "deep": {Claude: "m3"}}
	if diff := cmp.Diff(want, r.Config.Models.Tiers); diff != "" {
		t.Errorf("tables merge (-want +got):\n%s", diff)
	}
	if got := r.Config.Models.Catalog.Providers; !slices.Equal(got, []string{"z"}) {
		t.Errorf("arrays replace: providers = %v", got)
	}
	if diff := cmp.Diff(map[string]any{"k": int64(1), "j": "v"}, r.Config.Harness.Claude.Settings); diff != "" {
		t.Errorf("free maps merge (-want +got):\n%s", diff)
	}
	if r.Origin["models.tiers.fast.effort"] != p("a.toml") || r.Origin["models.tiers.fast.claude"] != "profile p" {
		t.Errorf("origin = %v", r.Origin)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	user := p("home", "u", ".config", "agents", "config.toml")
	tests := []struct {
		name    string
		fsys    *fakeFS
		mut     func(*config.Options)
		env     map[string]string
		wantErr string
	}{
		{"missing explicit path", newFS(nil), func(o *config.Options) { o.ConfigPath = p("nope.toml") }, nil, "nope.toml"},
		{"missing AGENTS_CONFIG", newFS(nil), nil, map[string]string{"AGENTS_CONFIG": p("nope2.toml")}, "nope2.toml"},
		{"unknown profile", standardFS(), func(o *config.Options) { o.Profile = "ghost" }, nil, `unknown profile "ghost"`},
		{"unknown profile from env", standardFS(), nil, map[string]string{"AGENTS_PROFILE": "ghost"}, `unknown profile "ghost"`},
		{"unknown key", newFS(map[string]string{user: "schema_version = 1\n\n[ui]\ntheme = \"x\"\nthemee = \"y\"\n"}), nil, nil, user + `:5: unknown key "ui.themee"`},
		{"unknown nested key in profile", newFS(map[string]string{user: "[profiles.a.ui]\nbogus = 1\n"}), nil, nil, `:2: unknown key "profiles.a.ui.bogus"`},
		{"profile cannot hold os", newFS(map[string]string{user: "[profiles.a.os.linux.ui]\ntheme = \"x\"\n"}), nil, nil, `unknown key "profiles.a.os`},
		{"type error names the line", newFS(map[string]string{user: "[ui]\ntheme = 3\n"}), nil, nil, user + ":2"},
		{"syntax error", newFS(map[string]string{user: "[ui\n"}), nil, nil, user},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := load(t, tt.fsys, tt.mut, tt.env)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	t.Run("nil Getenv", func(t *testing.T) {
		t.Parallel()
		if _, err := config.Load(config.Options{GOOS: "linux", Home: "/h"}); err == nil {
			t.Error("want an error when Getenv is nil")
		}
	})
	t.Run("missing default file is fine", func(t *testing.T) {
		t.Parallel()
		r, err := load(t, newFS(nil), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Files) != 0 || r.Config.SchemaVersion != 1 || r.Origin["schema_version"] != "default" {
			t.Errorf("files=%v version=%d origin=%v", r.Files, r.Config.SchemaVersion, r.Origin)
		}
	})
	t.Run("AGENTS_CONFIG replaces the default path", func(t *testing.T) {
		t.Parallel()
		fsys := newFS(map[string]string{p("x.toml"): "[ui]\ntheme = \"x\"\n", user: "[ui]\ntheme = \"user\"\n"})
		r, err := load(t, fsys, nil, map[string]string{"AGENTS_CONFIG": p("x.toml")})
		if err != nil {
			t.Fatal(err)
		}
		if r.Config.UI.Theme != "x" {
			t.Errorf("theme = %q", r.Config.UI.Theme)
		}
	})
}

func TestLoadDetectsHarnesses(t *testing.T) {
	t.Parallel()
	home := p("home", "u")
	tests := []struct {
		name string
		goos string
		env  map[string]string
		dirs []string
		want []config.Harness
	}{
		{"none", "linux", nil, nil, nil},
		{"homes", "linux", nil, []string{
			filepath.Join(home, ".claude"), filepath.Join(home, ".copilot"), filepath.Join(home, ".codex"),
			filepath.Join(home, ".config", "opencode"), filepath.Join(home, ".pi"), filepath.Join(home, ".gemini", "antigravity-cli"),
		}, []config.Harness{config.Claude, config.Copilot, config.Codex, config.OpenCode, config.Pi, config.Antigravity}},
		{"vscode linux", "linux", nil, []string{filepath.Join(home, ".config", "Code", "User")}, []config.Harness{config.VSCode}},
		{"vscode darwin", "darwin", nil, []string{filepath.Join(home, "Library", "Application Support", "Code", "User")}, []config.Harness{config.VSCode}},
		{"vscode windows", "windows", map[string]string{"APPDATA": p("appdata")}, []string{filepath.Join(p("appdata"), "Code", "User")}, []config.Harness{config.VSCode}},
		{"vscode ignores XDG", "linux", map[string]string{"XDG_CONFIG_HOME": p("xdg")}, []string{filepath.Join(p("xdg"), "Code", "User")}, nil},
		{"wrong os dir", "darwin", nil, []string{filepath.Join(home, ".config", "Code", "User")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := load(t, newFS(nil, tt.dirs...), func(o *config.Options) { o.GOOS = tt.goos }, tt.env)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(r.Harnesses, tt.want) || !r.HarnessesDetected || r.Origin["harnesses"] != "auto-detect" {
				t.Errorf("harnesses = %v detected=%v origin=%q, want %v", r.Harnesses, r.HarnessesDetected, r.Origin["harnesses"], tt.want)
			}
		})
	}

	t.Run("explicit harnesses skip detection", func(t *testing.T) {
		t.Parallel()
		fsys := newFS(map[string]string{p("c.toml"): "harnesses = [\"pi\"]\n"}, filepath.Join(home, ".claude"))
		r, err := load(t, fsys, func(o *config.Options) { o.ConfigPath = p("c.toml") }, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(r.Harnesses, []config.Harness{config.Pi}) || r.HarnessesDetected {
			t.Errorf("harnesses = %v detected=%v", r.Harnesses, r.HarnessesDetected)
		}
	})
}

func TestLoadTestdata(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"full.toml", "os-windows.toml"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, err := config.Load(config.Options{
				Getenv:     env(nil),
				GOOS:       "windows",
				Home:       t.TempDir(),
				ConfigPath: filepath.Join("testdata", "valid", name),
			})
			if err != nil {
				t.Fatal(err)
			}
			if fs := config.Validate(r.Config); len(fs) != 0 {
				t.Errorf("findings: %v", fs)
			}
			if name == "os-windows.toml" {
				if got := r.Config.Harness.Claude.Settings["defaultShell"]; got != "powershell" {
					t.Errorf("windows overlay not applied: %v", got)
				}
			}
		})
	}
}

func TestDirs(t *testing.T) {
	t.Parallel()
	j := filepath.Join
	tests := []struct {
		name string
		goos string
		env  map[string]string
		want config.Dirs
	}{
		{"linux default", "linux", nil, config.Dirs{
			Config: j("/h", ".config", "agents"), Cache: j("/h", ".cache", "agents"),
			State: j("/h", ".local", "state", "agents"), Data: j("/h", ".local", "share", "agents"),
		}},
		{"macOS uses XDG defaults", "darwin", nil, config.Dirs{
			Config: j("/h", ".config", "agents"), Cache: j("/h", ".cache", "agents"),
			State: j("/h", ".local", "state", "agents"), Data: j("/h", ".local", "share", "agents"),
		}},
		{"XDG on any OS", "darwin", map[string]string{
			"XDG_CONFIG_HOME": "/x/c", "XDG_CACHE_HOME": "/x/k", "XDG_STATE_HOME": "/x/s", "XDG_DATA_HOME": "/x/d",
		}, config.Dirs{Config: j("/x/c", "agents"), Cache: j("/x/k", "agents"), State: j("/x/s", "agents"), Data: j("/x/d", "agents")}},
		{"windows", "windows", map[string]string{"APPDATA": "/ad", "LOCALAPPDATA": "/lad"}, config.Dirs{
			Config: j("/ad", "agents"), Cache: j("/lad", "agents", "cache"),
			State: j("/lad", "agents", "state"), Data: j("/lad", "agents", "data"),
		}},
		{"windows XDG wins", "windows", map[string]string{"XDG_CONFIG_HOME": "/x", "LOCALAPPDATA": "/lad"}, config.Dirs{
			Config: j("/x", "agents"), Cache: j("/lad", "agents", "cache"),
			State: j("/lad", "agents", "state"), Data: j("/lad", "agents", "data"),
		}},
		{"windows without env", "windows", nil, config.Dirs{
			Config: j("/h", "AppData", "Roaming", "agents"), Cache: j("/h", "AppData", "Local", "agents", "cache"),
			State: j("/h", "AppData", "Local", "agents", "state"), Data: j("/h", "AppData", "Local", "agents", "data"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, config.ResolveDirs(env(tt.env), tt.goos, "/h")); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()
	getenv := env(map[string]string{"A": "1", "B": "two", "HOME": "/h/u"})
	tests := []struct {
		in        string
		want      string
		wantUnset []string
	}{
		{"plain", "plain", nil},
		{"${A}", "1", nil},
		{"x${A}y${B}z", "x1ytwoz", nil},
		{"${MISSING}", "${MISSING}", []string{"MISSING"}},
		{"${M1}-${A}-${M2}-${M1}", "${M1}-1-${M2}-${M1}", []string{"M1", "M2"}},
		{"$A", "$A", nil},
		{"$$A", "$$A", nil},
		{"$${A}", "${A}", nil},
		{"x$${A}y${A}", "x${A}y1", nil},
		{"$${MISSING}", "${MISSING}", nil},
		{"${", "${", nil},
		{"${A", "${A", nil},
		{"${}", "${}", nil},
		{"${A B}", "${A B}", nil},
		{"${1A}", "${1A}", nil},
		{"${${A}}", "${${A}}", nil},
		{"", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, unset := config.Expand(tt.in, getenv)
			if got != tt.want || !slices.Equal(unset, tt.wantUnset) {
				t.Errorf("Expand(%q) = %q, %v; want %q, %v", tt.in, got, unset, tt.want, tt.wantUnset)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	t.Parallel()
	getenv := env(map[string]string{"D": "dir"})
	j := filepath.Join
	tests := []struct {
		in        string
		want      string
		wantUnset []string
	}{
		{"~/a/${D}", j("/h", "a", "dir"), nil},
		{"~", "/h", nil},
		{`~\a`, j("/h", "a"), nil},
		{`~\${NOPE}`, j("/h", "${NOPE}"), []string{"NOPE"}},
		{"~/$${D}", j("/h", "${D}"), nil},
		{"~/${NOPE}/x", j("/h", "${NOPE}", "x"), []string{"NOPE"}},
		{"~user/a", "~user/a", nil},
		{"/abs/${D}", "/abs/dir", nil},
		{"a/~/b", "a/~/b", nil},
		{"${NOPE}", "${NOPE}", []string{"NOPE"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, unset := config.ExpandPath(tt.in, "/h", getenv)
			if got != tt.want || !slices.Equal(unset, tt.wantUnset) {
				t.Errorf("ExpandPath(%q) = %q, %v; want %q, %v", tt.in, got, unset, tt.want, tt.wantUnset)
			}
		})
	}
}

func FuzzExpand(f *testing.F) {
	for _, s := range []string{"", "${A}", "${", "}${", "a${A}b${B", "${${A}}", "$$", "~/${A}"} {
		f.Add(s)
	}
	getenv := env(map[string]string{"A": "x"})
	f.Fuzz(func(t *testing.T, s string) {
		out, _ := config.Expand(s, getenv)
		if !strings.Contains(s, "${") && out != s {
			t.Errorf("Expand(%q) = %q, want the input unchanged", s, out)
		}
		config.ExpandPath(s, "/h", getenv)
	})
}

func TestValidate(t *testing.T) {
	t.Parallel()
	ok := func() config.Config {
		return config.Config{
			SchemaVersion: 1,
			Instructions:  config.Instructions{Layers: map[string]config.InstructionLayer{"core": {File: "core.md"}}},
		}
	}
	server := func(s config.MCPServer) map[string]config.MCPServer { return map[string]config.MCPServer{"acme": s} }
	tests := []struct {
		name string
		mut  func(*config.Config)
		want string // substring of the only expected finding, "" = none
	}{
		{"valid", func(*config.Config) {}, ""},
		{"schema_version 2", func(c *config.Config) { c.SchemaVersion = 2 }, "schema_version"},
		{"schema_version unset", func(c *config.Config) { c.SchemaVersion = 0 }, "schema_version"},
		{"bad harness", func(c *config.Config) { c.Harnesses = []config.Harness{"emacs"} }, `unknown harness "emacs"`},
		{"bad telemetry harness", func(c *config.Config) { c.Telemetry.Harnesses = []config.Harness{"emacs"} }, `unknown harness "emacs"`},
		{"bad plugin harness", func(c *config.Config) { c.Plugins.Enabled = map[string][]config.Harness{"p": {"emacs"}} }, "plugins.enabled.p"},
		{"profile missing", func(c *config.Config) { c.Profile = "ghost" }, "ghost"},
		{"profile declared", func(c *config.Config) {
			c.Profile = "dev"
			c.Profiles = map[string]config.Profile{"dev": {Instructions: []string{"core"}}}
		}, ""},
		{"undeclared profile layer", func(c *config.Config) {
			c.Profiles = map[string]config.Profile{"dev": {Instructions: []string{"core", "ghost"}}}
		}, "profiles.dev.instructions"},
		{"undeclared harness layer", func(c *config.Config) { c.Harness.Pi.Instructions = []string{"ghost"} }, "harness.pi.instructions"},
		{"undeclared profile harness layer", func(c *config.Config) {
			c.Profiles = map[string]config.Profile{"dev": {Harness: config.HarnessTable{Codex: config.CodexSettings{Instructions: []string{"ghost"}}}}}
		}, "profiles.dev.harness.codex.instructions"},
		{"lsp empty command", func(c *config.Config) { c.LSP.Servers = map[string]config.LSPServer{"gopls": {}} }, "lsp.servers.gopls.command"},
		{"mcp neither", func(c *config.Config) { c.MCP.Servers = server(config.MCPServer{}) }, "exactly one"},
		{"mcp both", func(c *config.Config) { c.MCP.Servers = server(config.MCPServer{URL: "u", Command: "c"}) }, "exactly one"},
		{"mcp url ok", func(c *config.Config) { c.MCP.Servers = server(config.MCPServer{URL: "u"}) }, ""},
		{"oauth on stdio", func(c *config.Config) {
			c.MCP.Servers = server(config.MCPServer{Command: "c", OAuth: config.OAuth{ClientID: "id"}})
		}, "only valid on a url server"},
		{"callback port high", func(c *config.Config) {
			c.MCP.Servers = server(config.MCPServer{URL: "u", OAuth: config.OAuth{CallbackPort: 70000}})
		}, "callback_port"},
		{"callback port negative", func(c *config.Config) {
			c.MCP.Servers = server(config.MCPServer{URL: "u", OAuth: config.OAuth{CallbackPort: -1}})
		}, "callback_port"},
		{"callback port ok", func(c *config.Config) {
			c.MCP.Servers = server(config.MCPServer{URL: "u", OAuth: config.OAuth{CallbackPort: 65535}})
		}, ""},
		{"client id control char", func(c *config.Config) {
			c.MCP.Servers = server(config.MCPServer{URL: "u", OAuth: config.OAuth{ClientID: "a\x07b"}})
		}, "client_id"},
		{"client id quote", func(c *config.Config) {
			c.MCP.Servers = server(config.MCPServer{URL: "u", OAuth: config.OAuth{ClientID: `a"b`}})
		}, "client_id"},
		{"client id backslash", func(c *config.Config) {
			c.MCP.Servers = server(config.MCPServer{URL: "u", OAuth: config.OAuth{ClientID: `a\b`}})
		}, "client_id"},
		{"client id with variable ok", func(c *config.Config) {
			c.MCP.Servers = server(config.MCPServer{URL: "u", OAuth: config.OAuth{ClientID: "${ACME_ID}"}})
		}, ""},
		{"workspace path empty", func(c *config.Config) {
			c.MCP.Workspaces = map[string]config.Workspace{"app": {Servers: server(config.MCPServer{URL: "u"})}}
		}, "mcp.workspaces.app.path"},
		{"workspace server invalid", func(c *config.Config) {
			c.MCP.Workspaces = map[string]config.Workspace{"app": {Path: "/x", Servers: server(config.MCPServer{})}}
		}, "mcp.workspaces.app.servers.acme"},
		{"reason for a listed pattern", func(c *config.Config) {
			c.Permissions = config.Permissions{DenyCommandPrefixes: []string{"pip"}, Reasons: map[string]string{"pip": "use uv"}}
		}, ""},
		{"reason for an unlisted pattern", func(c *config.Config) {
			c.Permissions = config.Permissions{DenyCommandPrefixes: []string{"pip"}, Reasons: map[string]string{"npm": "use pnpm"}}
		}, "permissions.reasons.npm"},
		{"execution mode ok", func(c *config.Config) { c.Autonomy.ExecutionMode = "full" }, ""},
		{"execution mode free name", func(c *config.Config) { c.Autonomy.ExecutionMode = "bypass" }, "autonomy.execution_mode"},
		{"execution mode lists the allowed values", func(c *config.Config) { c.Autonomy.ExecutionMode = "x" }, "ask, auto, full"},
		{"sandbox mode ok", func(c *config.Config) { c.Autonomy.SandboxMode = "read-only" }, ""},
		{"sandbox mode free name", func(c *config.Config) { c.Autonomy.SandboxMode = "unrestricted" }, "autonomy.sandbox_mode"},
		{"sandbox mode lists the allowed values", func(c *config.Config) { c.Autonomy.SandboxMode = "x" }, "off, workspace, read-only"},
		{"profile execution mode", func(c *config.Config) {
			c.Profiles = map[string]config.Profile{"dev": {Autonomy: config.Autonomy{ExecutionMode: "x"}}}
		}, "profiles.dev.autonomy.execution_mode"},
		{"os sandbox mode", func(c *config.Config) { c.OS.Linux.Autonomy.SandboxMode = "x" }, "os.linux.autonomy.sandbox_mode"},
		{"profile overlay server may be partial", func(c *config.Config) {
			c.Profiles = map[string]config.Profile{"dev": {MCP: config.MCP{Servers: server(config.MCPServer{Env: map[string]string{"A": "b"}})}}}
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := ok()
			tt.mut(&c)
			got := config.Validate(c)
			switch {
			case tt.want == "" && len(got) != 0:
				t.Errorf("unexpected findings: %v", got)
			case tt.want != "" && (len(got) != 1 || !strings.Contains(got[0].String(), tt.want)):
				t.Errorf("findings = %v, want exactly one containing %q", got, tt.want)
			}
		})
	}
}

func TestHarness(t *testing.T) {
	t.Parallel()
	want := []config.Harness{"claude", "copilot", "vscode", "codex", "opencode", "pi", "antigravity"}
	if diff := cmp.Diff(want, config.All()); diff != "" {
		t.Errorf("All (-want +got):\n%s", diff)
	}
	for _, h := range want {
		if !h.Valid() {
			t.Errorf("%s not valid", h)
		}
	}
	if config.Harness("emacs").Valid() || config.Harness("").Valid() {
		t.Error("unknown harness reported valid")
	}
}
