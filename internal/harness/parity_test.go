package harness

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// parityCase is one profile and starting point of the parity fixture.
type parityCase struct {
	Profile string // personal or work
	Start   string // fresh or live
}

const goldenHome = "/home/user"

// liveFiles maps a file of testdata/parity/live to the native files it seeds.
var liveFiles = map[string][]string{
	"claude.json":      {".claude/settings.json"},
	"agy.json":         {".gemini/antigravity-cli/settings.json"},
	"codex.toml":       {".codex/config.toml"},
	"copilot.json":     {".copilot/settings.json"},
	"copilot-lsp.json": {".copilot/lsp-config.json"},
	"mcp.json":         {".copilot/mcp-config.json", ".pi/agent/mcp.json", ".gemini/config/mcp_config.json"},
	"opencode.json":    {".config/opencode/opencode.jsonc"},
	"pi.json":          {".pi/agent/settings.json"},
}

// goldenTarget maps a golden path (owner/rel) to the native file under home.
func goldenTarget(home string, dirs config.Dirs, owner, rel string) string {
	switch owner + "/" + rel {
	case "claude/lsp.json":
		return filepath.Join(dirs.Data, "claude-plugins", "lsp", ".lsp.json")
	case "claude/mcp-user.json":
		return filepath.Join(home, ".claude.json")
	}
	base := map[string]string{
		"claude":      ".claude",
		"copilot":     ".copilot",
		"codex":       ".codex",
		"opencode":    ".config/opencode",
		"pi":          ".pi/agent",
		"antigravity": ".gemini/antigravity-cli",
		"ops":         "ops",
	}[owner]
	if owner == "antigravity" && rel == "mcp_config.json" {
		base = ".gemini/config"
	}
	return filepath.Join(home, base, rel)
}

var adjusters = map[string]func(parityCase, string, any) any{
	"claude":      adjustClaude,
	"copilot":     adjustCopilot,
	"vscode":      adjustVSCode,
	"codex":       adjustCodex,
	"opencode":    adjustOpenCode,
	"pi":          adjustPi,
	"antigravity": adjustAntigravity,
	"ops":         adjustWorkspace,
}

// TestParity applies the fixture and compares every native file with the
// output of the dotfiles templates it replaces (testdata/parity/README.md).
func TestParity(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("goldens use Unix paths")
	}
	for _, c := range []parityCase{{"personal", "fresh"}, {"personal", "live"}, {"work", "fresh"}, {"work", "live"}} {
		t.Run(c.Profile+"/"+c.Start, func(t *testing.T) {
			t.Parallel()
			home, dirs, plan := applyFixture(t, c)
			if len(plan.Warnings) > 0 {
				t.Logf("warnings: %q", plan.Warnings)
			}
			root := filepath.Join("testdata", "parity", "golden", c.Profile, c.Start)
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(root, path)
				owner, file, _ := strings.Cut(filepath.ToSlash(rel), "/")
				t.Run(owner+"/"+file, func(t *testing.T) {
					compareGolden(t, c, home, dirs, owner, file, path)
				})
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func compareGolden(t *testing.T, c parityCase, home string, dirs config.Dirs, owner, file, goldenPath string) {
	t.Helper()
	target := goldenTarget(home, dirs, owner, file)
	gotRaw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read rendered %s: %v", target, err)
	}
	wantRaw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, file, strings.ReplaceAll(string(gotRaw), home, goldenHome))
	want := decode(t, file, string(wantRaw))
	if owner+"/"+file == "claude/mcp-user.json" {
		got = userServerURLs(got)
	}
	want = adjusters[owner](c, file, want)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s (-golden +agents):\n%s", file, diff)
	}
}

// userServerURLs reduces ~/.claude.json to the golden mcp-user.json shape: name -> url.
func userServerURLs(v any) any {
	out := map[string]any{}
	m, _ := v.(map[string]any)
	servers, _ := m["mcpServers"].(map[string]any)
	for name, s := range servers {
		sm, _ := s.(map[string]any)
		out[name] = sm["url"]
	}
	return out
}

func decode(t *testing.T, file, text string) any {
	t.Helper()
	var v any
	var err error
	switch filepath.Ext(file) {
	case ".json", ".jsonc":
		err = json.Unmarshal([]byte(text), &v)
	case ".toml":
		var m map[string]any
		err = toml.Unmarshal([]byte(text), &m)
		v = m
	default:
		return text
	}
	if err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	return v
}

// applyFixture loads the fixture for c into a fresh home, renders it and
// applies the plan, seeding the live files first for the live start.
func applyFixture(t *testing.T, c parityCase) (string, config.Dirs, Plan) {
	t.Helper()
	home := t.TempDir()
	fixture, err := filepath.Abs(filepath.Join("testdata", "parity"))
	if err != nil {
		t.Fatal(err)
	}
	copyDir(t, filepath.Join(fixture, "instructions"), filepath.Join(home, ".config", "agents", "instructions"))
	if c.Start == "live" {
		for src, dsts := range liveFiles {
			data, rerr := os.ReadFile(filepath.Join(fixture, "live", src))
			if rerr != nil {
				t.Fatal(rerr)
			}
			for _, dst := range dsts {
				writeFile(t, filepath.Join(home, dst), data)
			}
		}
	}
	env := map[string]string{"HOME": home, "MCP_ACCOUNT": "acct", "MCP_CLIENT_ID": "client-123"}
	if c.Profile == "personal" {
		env["AGENTS_OTEL_ENDPOINT"] = "https://otel.example.com"
	}
	getenv := func(k string) string { return env[k] }
	r, err := config.Load(config.Options{
		Getenv: getenv, GOOS: "linux", Home: home,
		ConfigPath: filepath.Join(fixture, "config.toml"), Profile: c.Profile,
	})
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if fs := config.Validate(r.Config); len(fs) > 0 {
		t.Fatalf("fixture is invalid: %v", fs)
	}
	plan, err := Render(r, Env{
		Home: home, Hostname: "host.example", Arch: "arm64", GOOS: "linux",
		Getenv: getenv, LookPath: fakeLookPath(c.Profile), Dirs: r.Dirs,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	results, err := apply.Engine{StatePath: filepath.Join(r.Dirs.State, "apply.json")}.Run(plan.Targets)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, res := range results {
		if res.Err != nil {
			t.Errorf("apply %s: %v", res.Target.Path, res.Err)
		}
	}
	return home, r.Dirs, plan
}

// fakeLookPath finds every language server, except gopls on the work profile,
// whose machines do not install Go.
func fakeLookPath(profile string) func(string) (string, error) {
	return func(name string) (string, error) {
		if profile == "work" && name == "gopls" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dst, e.Name()), data)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
