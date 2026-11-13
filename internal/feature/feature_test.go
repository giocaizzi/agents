package feature_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/feature"
	"github.com/giocaizzi/agents/internal/harness"
)

var update = flag.Bool("update", false, "rewrite docs/features.md")

const docPath = "../../docs/features.md"

// schemaNode is the part of a JSON Schema object the walk needs.
type schemaNode struct {
	Ref        string                     `json:"$ref"`
	Properties map[string]json.RawMessage `json:"properties"`
	Additional json.RawMessage            `json:"additionalProperties"`
}

// schemaWalker lists the leaf keys of the generated config schema.
type schemaWalker struct {
	defs map[string]json.RawMessage
}

// node decodes raw and follows a #/$defs reference. A boolean (for example
// additionalProperties: false) is an empty node.
func (w schemaWalker) node(raw json.RawMessage) schemaNode {
	var n schemaNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return schemaNode{}
	}
	if name, ok := strings.CutPrefix(n.Ref, "#/$defs/"); ok {
		return w.node(w.defs[name])
	}
	return n
}

// walk appends the dotted path of every leaf under n, with "*" for the free
// name of a map of objects.
func (w schemaWalker) walk(n schemaNode, prefix string, out *[]string) {
	for _, name := range slices.Sorted(maps.Keys(n.Properties)) {
		if prefix == "" && (name == "profiles" || name == "os") {
			continue // overlays repeat the same keys
		}
		path := prefix + name
		child := w.node(n.Properties[name])
		switch values := w.node(child.Additional); {
		case len(child.Properties) > 0:
			w.walk(child, path+".", out)
		case len(values.Properties) > 0:
			w.walk(values, path+".*.", out)
		default:
			*out = append(*out, path)
		}
	}
}

func schemaPaths(t *testing.T) []string {
	t.Helper()
	raw, err := config.Schema()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	w := schemaWalker{defs: doc.Defs}
	var out []string
	w.walk(w.node(doc.Defs["Config"]), "", &out)
	return out
}

func TestCompleteness(t *testing.T) {
	t.Parallel()
	paths := schemaPaths(t)
	if len(paths) < 100 {
		t.Fatalf("only %d schema paths: the walker is broken", len(paths))
	}
	hit := map[string]bool{}
	for _, p := range paths {
		f, ok := feature.Lookup(p)
		if !ok {
			t.Errorf("config key %s has no feature; add it to internal/feature/table.go", p)
			continue
		}
		hit[f.Key] = true
	}
	for _, f := range feature.Features {
		if f.Key == "profiles" || f.Key == "os" { // overlays: the walker leaves them out
			continue
		}
		if !hit[f.Key] {
			t.Errorf("feature %s matches no key of the config schema", f.Key)
		}
	}
}

func TestCells(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, f := range feature.Features {
		if seen[f.Key] {
			t.Errorf("duplicate feature %s", f.Key)
		}
		seen[f.Key] = true
		t.Run(f.Key, func(t *testing.T) {
			t.Parallel()
			if f.Summary == "" {
				t.Error("empty summary")
			}
			checkScope(t, f)
			for h, c := range f.Cells {
				if !slices.Contains(feature.HarnessIDs(), h) {
					t.Errorf("cell for unknown harness %q", h)
				}
				checkCell(t, h, c)
			}
		})
	}
}

func checkScope(t *testing.T, f feature.Feature) {
	t.Helper()
	switch f.Scope {
	case feature.ScopeShared:
		if len(f.Cells) != len(feature.HarnessIDs()) {
			t.Errorf("%d cells, want one per harness", len(f.Cells))
		}
	case feature.ScopeHarness:
		parts := strings.Split(f.Key, ".")
		if len(parts) < 3 || parts[0] != "harness" || len(f.Cells) != 1 || f.Cells[parts[1]].Status == "" {
			t.Errorf("a harness key needs exactly the cell of harness.<name>, got %v", slices.Collect(maps.Keys(f.Cells)))
		}
	case feature.ScopeInternal:
		if len(f.Cells) != 0 {
			t.Errorf("an internal key has %d cells", len(f.Cells))
		}
	default:
		t.Errorf("unknown scope %q", f.Scope)
	}
}

func checkCell(t *testing.T, h string, c feature.Cell) {
	t.Helper()
	switch c.Status {
	case feature.StatusRendered:
		if c.Target == "" {
			t.Errorf("%s: a rendered cell needs a target: %+v", h, c)
		}
	case feature.StatusUnsupported, feature.StatusPlanned:
		if c.Evidence == "" || c.Target != "" {
			t.Errorf("%s: a %s cell needs evidence and no target: %+v", h, c.Status, c)
		}
	default:
		t.Errorf("%s: unknown status %q", h, c.Status)
	}
}

func TestHarnessIDsAreConfigs(t *testing.T) {
	t.Parallel()
	want := make([]string, 0, len(config.All()))
	for _, h := range config.All() {
		want = append(want, string(h))
	}
	if diff := cmp.Diff(want, feature.HarnessIDs()); diff != "" {
		t.Errorf("harness ids (-config +feature):\n%s", diff)
	}
	for _, f := range feature.Features {
		if f.Scope == feature.ScopeHarness {
			if h := strings.Split(f.Key, ".")[1]; !config.Harness(h).Valid() {
				t.Errorf("%s names an unknown harness", f.Key)
			}
		}
	}
}

// TestCoverage renders testdata/full.toml for each harness alone and checks
// that every managed key each target writes is declared by a rendered cell of
// that harness.
//
// The granularity is the top-level managed key of a file (TOML table or JSON
// property), because that is what a renderer hands to apply: a cell declaring
// "file: permissions.allow" covers the managed key "permissions" of file. A
// declared key without a file part ("file") covers the whole file, as for
// instruction files and generated plugin files whose keys are free names.
// Passthrough settings (harness.<name>.settings) are declared as "(any key)",
// which matches nothing, and the fixture sets none.
func TestCoverage(t *testing.T) {
	t.Parallel()
	for _, h := range config.All() {
		t.Run(string(h), func(t *testing.T) {
			t.Parallel()
			plan, norm := renderFixture(t, h)
			declared := declaredTargets(h)
			written := 0
			for _, tg := range plan.Targets {
				if tg.Owner != string(h) {
					continue
				}
				written++
				file := norm(tg.Path)
				for _, key := range managedKeys(t, tg) {
					if !covered(declared, file, key) {
						t.Errorf("%s writes %q to %s, which no rendered cell of %s declares", h, key, file, h)
					}
				}
			}
			if written == 0 {
				t.Errorf("%s rendered no target", h)
			}
		})
	}
}

// renderFixture renders testdata/full.toml for h alone. The returned function
// names a path the way the table does.
func renderFixture(t *testing.T, h config.Harness) (harness.Plan, func(string) string) {
	t.Helper()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "agents", "instructions", "core.md"), "hello\n")
	getenv := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	r, err := config.Load(config.Options{
		Getenv: getenv, GOOS: "linux", Home: home,
		ConfigPath: filepath.Join("testdata", "full.toml"), Harnesses: []string{string(h)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fs := config.Validate(r.Config); len(fs) > 0 {
		t.Fatalf("fixture is invalid: %v", fs)
	}
	plan, err := harness.Render(r, harness.Env{
		Home: home, Hostname: "host", Arch: "arm64", GOOS: "linux", Getenv: getenv, Dirs: r.Dirs,
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Agents:   []string{"dev-tools:scout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan, normalizer(home, r.Dirs)
}

// alwaysWritten are managed keys that no config key controls.
var alwaysWritten = []string{"~/.config/opencode/opencode.jsonc: $schema", "<workspace>/opencode.json: $schema"}

// normalizer rewrites an absolute path to the names the table uses.
func normalizer(home string, dirs config.Dirs) func(string) string {
	return func(p string) string {
		p = strings.Replace(p, filepath.Join(home, "ws"), "<workspace>", 1)
		p = strings.Replace(p, dirs.Data, "<data dir>", 1)
		p = strings.Replace(p, filepath.Join(home, ".config", "Code", "User"), "<Code user dir>", 1)
		return filepath.ToSlash(strings.Replace(p, home, "~", 1))
	}
}

type declaredKey struct{ file, key string }

// declaredTargets parses the targets of every rendered cell of harness h.
func declaredTargets(h config.Harness) []declaredKey {
	var out []declaredKey
	for _, f := range feature.Features {
		c, ok := f.Cells[string(h)]
		if !ok || c.Status != feature.StatusRendered {
			continue
		}
		for entry := range strings.SplitSeq(c.Target, "; ") {
			file, key, _ := strings.Cut(entry, ": ")
			out = append(out, declaredKey{file, key})
		}
	}
	return out
}

func covered(declared []declaredKey, file, key string) bool {
	if slices.Contains(alwaysWritten, file+": "+key) {
		return true
	}
	for _, d := range declared {
		if d.file != file {
			continue
		}
		if d.key == "" || d.key == key || strings.HasPrefix(d.key, key+".") {
			return true
		}
	}
	return false
}

// managedKeys lists the top-level managed keys of a target; a text target has
// none, and is covered by a declaration of its file.
func managedKeys(t *testing.T, tg apply.Target) []string {
	t.Helper()
	switch m := tg.Managed.(type) {
	case map[string]any:
		return slices.Sorted(maps.Keys(m))
	case string:
		return []string{""}
	default:
		t.Fatalf("%s: unexpected managed value %T", tg.Path, tg.Managed)
		return nil
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDocsDrift(t *testing.T) {
	t.Parallel()
	got := feature.Markdown()
	if *update {
		if err := os.WriteFile(docPath, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("docs/features.md is stale; run go test ./internal/feature -run TestDocsDrift -update")
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want string // feature key, "" = none
	}{
		{"permissions.deny_read", "permissions.deny_read"},
		{"permissions.deny_read.extra", "permissions.deny_read"},
		{"mcp.servers.docs.url", "mcp.servers"},
		{"mcp.servers.docs.oauth.callback_port", "mcp.servers.*.oauth.callback_port"},
		{"mcp.workspaces.app.servers.docs.url", "mcp.workspaces"},
		{"lsp.servers.gopls.format", "lsp.servers.*.format"},
		{"lsp.servers.gopls.command", "lsp.servers"},
		{"instructions.layers.core.file", "instructions"},
		{"instructions.edit_command", "instructions.edit_command"},
		{"harness.codex.sandbox_mode", "harness.codex.sandbox_mode"},
		{"harness.claude.settings.env.X", "harness.claude.settings"},
		{"nonsense", ""},
		{"permissions", ""},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			f, ok := feature.Lookup(tt.key)
			if got := f.Key; got != tt.want || ok != (tt.want != "") {
				t.Errorf("Lookup(%q) = %q, %v, want %q", tt.key, got, ok, tt.want)
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()
	got := feature.Describe("context.max_tokens")
	for _, want := range []string{"Token limit of one response.", "Written to claude.", "Not supported by copilot, vscode, codex, opencode, pi, antigravity."} {
		if !strings.Contains(got, want) {
			t.Errorf("Describe = %q, missing %q", got, want)
		}
	}
	if got := feature.Describe("schema_version"); strings.Contains(got, "Written") || got == "" {
		t.Errorf("an internal key describes only itself: %q", got)
	}
	if got := feature.Describe("nonsense"); got != "" {
		t.Errorf("Describe(unknown) = %q", got)
	}
}

func TestPlanned(t *testing.T) {
	t.Parallel()
	planned := feature.Planned()
	if len(planned) == 0 {
		t.Fatal("no planned features")
	}
	for _, f := range planned {
		if len(f.Harnesses(feature.StatusPlanned)) == 0 {
			t.Errorf("%s has no planned cell", f.Key)
		}
	}
	if !slices.ContainsFunc(planned, func(f feature.Feature) bool { return f.Key == "autonomy.allow_non_workspace_access" }) {
		t.Error("autonomy.allow_non_workspace_access is planned for claude (ambiguous)")
	}
}
