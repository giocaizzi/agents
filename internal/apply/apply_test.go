package apply

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func engineIn(dir string) Engine {
	return Engine{StatePath: filepath.Join(dir, "state", "apply.json")}
}

func runOne(t *testing.T, e Engine, tg Target) Result {
	t.Helper()
	res, err := e.Run([]Target{tg})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1", len(res))
	}
	return res[0]
}

func TestRunJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		live       string // empty means the file does not exist
		managed    any
		union      [][]string
		remove     [][]string
		want       string
		wantStatus Status
	}{
		{
			name:       "fresh create",
			managed:    map[string]any{"b": 1, "a": []string{"x"}},
			want:       "{\n  \"a\": [\n    \"x\"\n  ],\n  \"b\": 1\n}\n",
			wantStatus: StatusCreate,
		},
		{
			name:       "ordered managed document appends in its own order",
			managed:    &Object{Keys: []string{"z", "a"}, Values: map[string]any{"z": true, "a": nil}},
			want:       "{\n  \"z\": true,\n  \"a\": null\n}\n",
			wantStatus: StatusCreate,
		},
		{
			name:       "merge keeps unmanaged keys and live order",
			live:       `{"zeta": 1, "alpha": {"keep": true, "mine": 1}, "omega": "x"}`,
			managed:    map[string]any{"alpha": map[string]any{"mine": 2, "new": "n"}, "added": 1},
			want:       "{\n  \"zeta\": 1,\n  \"alpha\": {\n    \"keep\": true,\n    \"mine\": 2,\n    \"new\": \"n\"\n  },\n  \"omega\": \"x\",\n  \"added\": 1\n}\n",
			wantStatus: StatusUpdate,
		},
		{
			name:       "scalars and arrays replace",
			live:       `{"s": "old", "arr": [1, 2, 3], "o": {"a": 1}}`,
			managed:    map[string]any{"s": "new", "arr": []any{"x"}, "o": "scalar"},
			want:       "{\n  \"s\": \"new\",\n  \"arr\": [\n    \"x\"\n  ],\n  \"o\": \"scalar\"\n}\n",
			wantStatus: StatusUpdate,
		},
		{
			name:       "union keeps live elements and appends new ones",
			live:       `{"chat": {"allowedUrls": ["https://mine.example", "https://a.example"]}}`,
			managed:    map[string]any{"chat": map[string]any{"allowedUrls": []any{"https://a.example", "https://b.example"}}},
			union:      [][]string{{"chat", "allowedUrls"}},
			want:       "{\n  \"chat\": {\n    \"allowedUrls\": [\n      \"https://mine.example\",\n      \"https://a.example\",\n      \"https://b.example\"\n    ]\n  }\n}\n",
			wantStatus: StatusUpdate,
		},
		{
			name: "union of objects keeps user entries and dedupes",
			live: `{"permission": [{"tool": "bash", "action": "ask"}, {"action": "allow", "tool": "read"}]}`,
			managed: map[string]any{"permission": []any{
				map[string]any{"tool": "read", "action": "allow"},
				map[string]any{"tool": "edit", "action": "deny"},
			}},
			union:      [][]string{{"permission"}},
			want:       "{\n  \"permission\": [\n    {\n      \"tool\": \"bash\",\n      \"action\": \"ask\"\n    },\n    {\n      \"action\": \"allow\",\n      \"tool\": \"read\"\n    },\n    {\n      \"action\": \"deny\",\n      \"tool\": \"edit\"\n    }\n  ]\n}\n",
			wantStatus: StatusUpdate,
		},
		{
			name:       "union path absent in live is created",
			live:       `{}`,
			managed:    map[string]any{"u": []any{"a"}},
			union:      [][]string{{"u"}},
			want:       "{\n  \"u\": [\n    \"a\"\n  ]\n}\n",
			wantStatus: StatusUpdate,
		},
		{
			name:       "remove paths are deleted first",
			live:       `{"a": {"b": 1, "c": 2}, "d": 3}`,
			managed:    map[string]any{"e": 4},
			remove:     [][]string{{"a", "b"}, {"d"}, {"missing", "path"}},
			want:       "{\n  \"a\": {\n    \"c\": 2\n  },\n  \"e\": 4\n}\n",
			wantStatus: StatusUpdate,
		},
		{
			name:       "remove path can be set again by managed",
			live:       `{"a": 1}`,
			managed:    map[string]any{"a": 2},
			remove:     [][]string{{"a"}},
			want:       "{\n  \"a\": 2\n}\n",
			wantStatus: StatusUpdate,
		},
		{
			name:       "keys with dots are single segments",
			live:       `{"chat.tools.terminal.autoApprove": {"ls": true}}`,
			managed:    map[string]any{"chat.tools.terminal.autoApprove": map[string]any{"git status": true}},
			want:       "{\n  \"chat.tools.terminal.autoApprove\": {\n    \"ls\": true,\n    \"git status\": true\n  }\n}\n",
			wantStatus: StatusUpdate,
		},
		{
			name:       "numbers compare by value",
			live:       `{"i": 1.0, "f": 2.50, "big": 12345678901234567890}`,
			managed:    map[string]any{"i": 1, "f": 2.5, "big": uint64(12345678901234567890)},
			want:       `{"i": 1.0, "f": 2.50, "big": 12345678901234567890}`,
			wantStatus: StatusUnchanged,
		},
		{
			name:       "html is not escaped",
			managed:    map[string]any{"k": "<a&b>"},
			want:       "{\n  \"k\": \"<a&b>\"\n}\n",
			wantStatus: StatusCreate,
		},
		{
			name:       "empty containers",
			managed:    map[string]any{"o": map[string]any{}, "a": []any{}},
			want:       "{\n  \"a\": [],\n  \"o\": {}\n}\n",
			wantStatus: StatusCreate,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkRunJSON(t, tt.live, tt.want, tt.wantStatus, Target{
				Format: JSON, Managed: tt.managed, Union: tt.union, Legacy: tt.remove,
			})
		})
	}
}

func checkRunJSON(t *testing.T, live, want string, wantStatus Status, tg Target) {
	t.Helper()
	dir := t.TempDir()
	tg.Owner = "h"
	tg.Path = filepath.Join(dir, "sub", "settings.json")
	if live != "" {
		writeFileT(t, tg.Path, live)
	}
	res := runOne(t, engineIn(dir), tg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Status != wantStatus {
		t.Errorf("status = %q, want %q", res.Status, wantStatus)
	}
	if diff := cmp.Diff(want, readFileT(t, tg.Path)); diff != "" {
		t.Errorf("file (-want +got):\n%s", diff)
	}
	if (wantStatus == StatusUnchanged) != (res.Diff == "") {
		t.Errorf("status %q with diff %q", res.Status, res.Diff)
	}
}

func TestUnchangedJSONCNotRewritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.jsonc")
	live := "{\n  // keep me\n  \"a\": 1, /* inline */\n  \"b\": [1, 2,],\n}\n"
	writeFileT(t, path, live)
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	res := runOne(t, engineIn(dir), Target{Path: path, Format: JSONC, Managed: map[string]any{"a": 1, "b": []any{1, 2}}})
	if res.Err != nil || res.Status != StatusUnchanged {
		t.Fatalf("result = %+v", res)
	}
	if got := readFileT(t, path); got != live {
		t.Errorf("content changed: %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("mtime = %v, want %v", info.ModTime(), old)
	}
}

func TestJSONCUpdate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.jsonc")
	writeFileT(t, path, "{\n  // note\n  \"url\": \"http://x//y\", /* c */\n  \"list\": [1,],\n}\n")
	res := runOne(t, engineIn(dir), Target{Path: path, Format: JSONC, Managed: map[string]any{"new": true}})
	if res.Err != nil || res.Status != StatusUpdate {
		t.Fatalf("result = %+v", res)
	}
	want := "{\n  \"url\": \"http://x//y\",\n  \"list\": [\n    1\n  ],\n  \"new\": true\n}\n"
	if diff := cmp.Diff(want, readFileT(t, path)); diff != "" {
		t.Errorf("file (-want +got):\n%s", diff)
	}
}

func TestInvalidLive(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, live string }{
		{"broken json", `{"a": `},
		{"array top level", `[1]`},
		{"trailing data", `{} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "s.json")
			writeFileT(t, path, tt.live)
			res := runOne(t, engineIn(dir), Target{Path: path, Format: JSON, Managed: map[string]any{}})
			if res.Err == nil {
				t.Fatal("want an error")
			}
			if got := readFileT(t, path); got != tt.live {
				t.Errorf("file was touched: %q", got)
			}
		})
	}
}

func TestTOMLMerge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFileT(t, path, "model = \"x\"\n\n[mcp_servers.mine]\ncommand = \"a\"\n\n[mcp_servers.shared]\ncommand = \"old\"\nargs = [\"1\"]\n")
	managed := map[string]any{
		"approval": "never",
		"mcp_servers": map[string]any{
			"shared": map[string]any{"command": "new"},
			"added":  map[string]any{"url": "https://x.example", "port": 8080},
		},
	}
	res := runOne(t, engineIn(dir), Target{Path: path, Format: TOML, Managed: managed})
	if res.Err != nil || res.Status != StatusUpdate {
		t.Fatalf("result = %+v", res)
	}
	want := "approval = 'never'\nmodel = 'x'\n\n[mcp_servers]\n[mcp_servers.added]\nport = 8080\nurl = 'https://x.example'\n\n[mcp_servers.mine]\ncommand = 'a'\n\n[mcp_servers.shared]\nargs = ['1']\ncommand = 'new'\n"
	if diff := cmp.Diff(want, readFileT(t, path)); diff != "" {
		t.Errorf("file (-want +got):\n%s", diff)
	}
	// A second run is a no-op.
	res = runOne(t, engineIn(dir), Target{Path: path, Format: TOML, Managed: managed})
	if res.Status != StatusUnchanged {
		t.Errorf("second run status = %q", res.Status)
	}
}

func TestText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	e := engineIn(dir)
	steps := []struct {
		content string
		want    Status
	}{
		{"one\ntwo\n", StatusCreate},
		{"one\ntwo\n", StatusUnchanged},
		{"one\nthree\n", StatusUpdate},
	}
	for _, s := range steps {
		res := runOne(t, e, Target{Path: path, Format: Text, Managed: s.content})
		if res.Err != nil || res.Status != s.want {
			t.Fatalf("content %q: result = %+v, want %q", s.content, res, s.want)
		}
		if got := readFileT(t, path); got != s.content {
			t.Errorf("file = %q, want %q", got, s.content)
		}
	}
	if res := runOne(t, e, Target{Path: path, Format: Text, Managed: 3}); res.Err == nil {
		t.Error("non-string text managed value must fail")
	}
}

func TestSymlinks(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (dir, link, target string) {
		t.Helper()
		dir = t.TempDir()
		target = filepath.Join(dir, "real.json")
		link = filepath.Join(dir, "link.json")
		writeFileT(t, target, "{\"a\": 1}\n")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		return dir, link, target
	}
	t.Run("refused", func(t *testing.T) {
		t.Parallel()
		dir, link, target := setup(t)
		res := runOne(t, engineIn(dir), Target{Owner: "h", Path: link, Format: JSON, Managed: map[string]any{"a": 2}})
		if res.Status != StatusRefused || res.Err == nil {
			t.Fatalf("result = %+v", res)
		}
		if !strings.Contains(res.Err.Error(), link) || !strings.Contains(res.Err.Error(), "symlink") {
			t.Errorf("error = %v", res.Err)
		}
		if got := readFileT(t, target); got != "{\"a\": 1}\n" {
			t.Errorf("real file changed: %q", got)
		}
	})
	t.Run("check only drift", func(t *testing.T) {
		t.Parallel()
		dir, link, target := setup(t)
		res := runOne(t, engineIn(dir), Target{Path: link, Format: JSON, Managed: map[string]any{"a": 2}, CheckOnly: true})
		if res.Err != nil || res.Status != StatusDrift || res.Diff == "" {
			t.Fatalf("result = %+v", res)
		}
		if got := readFileT(t, target); got != "{\"a\": 1}\n" {
			t.Errorf("real file changed: %q", got)
		}
	})
	t.Run("check only unchanged", func(t *testing.T) {
		t.Parallel()
		dir, link, _ := setup(t)
		res := runOne(t, engineIn(dir), Target{Path: link, Format: JSON, Managed: map[string]any{"a": 1}, CheckOnly: true})
		if res.Err != nil || res.Status != StatusUnchanged {
			t.Fatalf("result = %+v", res)
		}
	})
}

func TestCheckOnlyMissingIsDriftAndNotRecorded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	e := engineIn(dir)
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"a": 1}, CheckOnly: true})
	if res.Status != StatusDrift {
		t.Errorf("status = %q", res.Status)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("check-only target was written")
	}
	if _, err := os.Stat(e.StatePath); err == nil {
		t.Error("manifest written for a check-only run")
	}
}

func TestDryRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.json")
	missing := filepath.Join(dir, "deep", "missing.json")
	writeFileT(t, existing, "{\n  \"a\": 1\n}\n")
	e := engineIn(dir)
	e.DryRun = true
	res, err := e.Run([]Target{
		{Path: existing, Format: JSON, Managed: map[string]any{"a": 2}},
		{Path: missing, Format: JSON, Managed: map[string]any{"a": 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Status != StatusUpdate || res[1].Status != StatusCreate {
		t.Errorf("statuses = %q, %q", res[0].Status, res[1].Status)
	}
	if !strings.Contains(res[0].Diff, "-  \"a\": 1") || !strings.Contains(res[0].Diff, "+  \"a\": 2") {
		t.Errorf("diff = %q", res[0].Diff)
	}
	if got := readFileT(t, existing); got != "{\n  \"a\": 1\n}\n" {
		t.Errorf("existing changed: %q", got)
	}
	for _, p := range []string{missing, filepath.Dir(missing), filepath.Dir(e.StatePath)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was created", p)
		}
	}
}

func TestManifestRemovesStaleKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeFileT(t, path, `{"user": 1, "u": ["mine"]}`)
	e := engineIn(dir)
	union := [][]string{{"u"}}
	first := map[string]any{
		"keep":   1,
		"drop":   map[string]any{"deep": map[string]any{"x": 1}},
		"empty":  map[string]any{},
		"scalar": "s",
		"u":      []any{"a", "b"},
	}
	if res := runOne(t, e, Target{Path: path, Format: JSON, Managed: first, Union: union}); res.Err != nil || res.Status != StatusUpdate {
		t.Fatalf("first run: %+v", res)
	}
	second := map[string]any{"keep": 1, "u": []any{"b"}}
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: second, Union: union})
	if res.Err != nil || res.Status != StatusUpdate {
		t.Fatalf("second run: %+v", res)
	}
	want := "{\n  \"user\": 1,\n  \"u\": [\n    \"mine\",\n    \"b\"\n  ],\n  \"keep\": 1\n}\n"
	if diff := cmp.Diff(want, readFileT(t, path)); diff != "" {
		t.Errorf("file (-want +got):\n%s", diff)
	}

	// Dropping the union path entirely removes only managed elements.
	res = runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"keep": 1}, Union: union})
	if res.Err != nil || res.Status != StatusUpdate {
		t.Fatalf("third run: %+v", res)
	}
	want = "{\n  \"user\": 1,\n  \"u\": [\n    \"mine\"\n  ],\n  \"keep\": 1\n}\n"
	if diff := cmp.Diff(want, readFileT(t, path)); diff != "" {
		t.Errorf("file (-want +got):\n%s", diff)
	}

	// Manifest format.
	got := readFileT(t, e.StatePath)
	if !strings.Contains(got, `"version": 1`) || !strings.Contains(got, path) {
		t.Errorf("manifest = %s", got)
	}
	info, statErr := os.Stat(e.StatePath)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("manifest mode = %v", info.Mode().Perm())
	}
}

func TestManifestUnionArrayRemovedWhenEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	e := engineIn(dir)
	union := [][]string{{"u"}}
	runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"u": []any{"a"}}, Union: union})
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"k": 1}, Union: union})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if diff := cmp.Diff("{\n  \"k\": 1\n}\n", readFileT(t, path)); diff != "" {
		t.Errorf("file (-want +got):\n%s", diff)
	}
}

func TestManifestVersionMismatchIsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	e := engineIn(dir)
	writeFileT(t, path, `{"old": 1, "k": 1}`)
	writeFileT(t, e.StatePath, `{"version":99,"files":{"`+path+`":{"keys":[["old"]]}}}`)
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"k": 1}})
	if res.Status != StatusUnchanged {
		t.Errorf("status = %q, want unchanged (old manifest ignored)", res.Status)
	}
}

func TestModes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fresh := filepath.Join(dir, "a", "fresh.json")
	custom := filepath.Join(dir, "custom.json")
	existing := filepath.Join(dir, "existing.json")
	writeFileT(t, existing, "{}")
	if err := os.Chmod(existing, 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := engineIn(dir).Run([]Target{
		{Path: fresh, Format: JSON, Managed: map[string]any{"a": 1}},
		{Path: custom, Format: JSON, Managed: map[string]any{"a": 1}, Mode: 0o644},
		{Path: existing, Format: JSON, Managed: map[string]any{"a": 1}, Mode: 0o644},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{fresh: 0o600, custom: 0o644, existing: 0o640} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v", filepath.Base(path), info.Mode().Perm(), want)
		}
	}
	info, err := os.Stat(filepath.Dir(fresh))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", info.Mode().Perm())
	}
	// No temp files left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if strings.Contains(en.Name(), ".tmp-") {
			t.Errorf("leftover temp file %s", en.Name())
		}
	}
}

func TestRelativePathFails(t *testing.T) {
	t.Parallel()
	res := runOne(t, Engine{}, Target{Path: "rel.json", Format: JSON, Managed: map[string]any{}})
	if res.Err == nil {
		t.Error("want an error for a relative path")
	}
}

func TestUnifiedDiff(t *testing.T) {
	t.Parallel()
	lines := func(n int) string {
		var sb strings.Builder
		for i := 1; i <= n; i++ {
			sb.WriteString("l" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + "\n")
		}
		return sb.String()
	}
	base := lines(30)
	tests := []struct {
		name      string
		old, new  string
		want      string
		wantHunks int // when > 0, only the number of hunks is checked
	}{
		{"equal", "a\n", "a\n", "", 0},
		{
			"create",
			"", "a\nb\n",
			"--- /p\n+++ /p\n@@ -0,0 +1,2 @@\n+a\n+b\n", 0,
		},
		{
			"change in the middle",
			"a\nb\nc\n", "a\nX\nc\n",
			"--- /p\n+++ /p\n@@ -1,3 +1,3 @@\n a\n-b\n+X\n c\n", 0,
		},
		{
			"missing trailing newline",
			"a\nb", "a\nb\n",
			"--- /p\n+++ /p\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n", 0,
		},
		{
			"two far apart hunks",
			base,
			strings.Replace(strings.Replace(base, "lb0\n", "CHANGED\n", 1), "l"+string(rune('a'+29%26))+"1\n", "TAIL\n", 1),
			"", 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := unifiedDiff("/p", tt.old, tt.new)
			if tt.wantHunks > 0 {
				if n := strings.Count(got, "@@ -"); n != tt.wantHunks {
					t.Errorf("hunks = %d, want %d:\n%s", n, tt.wantHunks, got)
				}
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestObject(t *testing.T) {
	t.Parallel()
	o := NewObject()
	o.Set("b", 1)
	o.Set("a", 2)
	o.Set("b", 3)
	if diff := cmp.Diff([]string{"b", "a"}, o.Keys); diff != "" {
		t.Errorf("keys (-want +got):\n%s", diff)
	}
	if v, ok := o.Get("b"); !ok || v != 3 {
		t.Errorf("Get(b) = %v, %v", v, ok)
	}
	if !o.Delete("b") || o.Delete("b") {
		t.Error("Delete result wrong")
	}
	if diff := cmp.Diff([]string{"a"}, o.Keys); diff != "" {
		t.Errorf("keys (-want +got):\n%s", diff)
	}
	var zero Object
	zero.Set("x", 1)
	if len(zero.Keys) != 1 {
		t.Error("Set on zero Object failed")
	}
}

// TestReviewXFail holds reproductions of engine bugs found in review. Each
// subtest is skipped until the source is fixed; drop the t.Skip to verify.
func TestReviewXFail(t *testing.T) {
	t.Parallel()

	t.Run("xfail: an empty managed object recorded as a leaf wipes user entries once a child is managed", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: dropStale deletes the whole object a previous run managed as an empty leaf ({}), including keys the user added under it")
		dir := t.TempDir()
		path := filepath.Join(dir, ".claude.json")
		// The user's own MCP servers (e.g. added with the harness's own CLI).
		writeFileT(t, path, `{"mcpServers": {"mine": {"command": "my-mcp"}}}`)
		e := engineIn(dir)
		// First apply: no MCP servers configured yet, so the managed value is {}.
		if res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"mcpServers": map[string]any{}}}); res.Err != nil {
			t.Fatal(res.Err)
		}
		// Later: one server is configured.
		managed := map[string]any{"mcpServers": map[string]any{"docs": map[string]any{"url": "https://docs.example"}}}
		if res := runOne(t, e, Target{Path: path, Format: JSON, Managed: managed}); res.Err != nil {
			t.Fatal(res.Err)
		}
		if got := readFileT(t, path); !strings.Contains(got, `"mine"`) {
			t.Errorf("the user's server was deleted:\n%s", got)
		}
	})

	t.Run("xfail: an update keeps the comments of a JSONC file", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: JSONC is stripped and re-encoded as plain JSON, so every user comment is lost on the first update")
		dir := t.TempDir()
		path := filepath.Join(dir, "opencode.jsonc")
		writeFileT(t, path, "{\n  // why this theme: contrast\n  \"theme\": \"x\"\n}\n")
		if res := runOne(t, engineIn(dir), Target{Path: path, Format: JSONC, Managed: map[string]any{"new": true}}); res.Err != nil {
			t.Fatal(res.Err)
		}
		if got := readFileT(t, path); !strings.Contains(got, "// why this theme: contrast") {
			t.Errorf("comment lost:\n%s", got)
		}
	})

	t.Run("xfail: an update keeps the live key order and comments of a TOML file", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: TOML goes through map[string]any, so live keys are re-sorted and comments dropped, against the documented 'keeps the live key order'")
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		writeFileT(t, path, "# my codex setup\nzeta = 1\nalpha = 2\n")
		if res := runOne(t, engineIn(dir), Target{Path: path, Format: TOML, Managed: map[string]any{"model": "m"}}); res.Err != nil {
			t.Fatal(res.Err)
		}
		got := readFileT(t, path)
		if !strings.Contains(got, "# my codex setup") {
			t.Errorf("comment lost:\n%s", got)
		}
		if strings.Index(got, "zeta") > strings.Index(got, "alpha") {
			t.Errorf("live key order not kept:\n%s", got)
		}
	})
}

// TestReviewXFailDiffMemory runs alone (not parallel): it measures
// process-wide allocation.
//
//nolint:paralleltest // measures process-wide allocation
func TestReviewXFailDiffMemory(t *testing.T) {
	t.Run("xfail: updating a large file does not allocate a quadratic diff table", func(t *testing.T) {
		t.Skip("xfail: unifiedDiff builds an O(n*m) LCS table for every changed target, even when not a dry run; ~1.2 GB for a 12k-line file changed at both ends")
		dir := t.TempDir()
		path := filepath.Join(dir, "state.json")
		var sb strings.Builder
		sb.WriteString("{\n  \"model\": \"old\",\n  \"projects\": {\n")
		const n = 12000
		for i := range n {
			sep := ","
			if i == n-1 {
				sep = ""
			}
			sb.WriteString("    \"/p/" + strconv.Itoa(i) + "\": " + strconv.Itoa(i) + sep + "\n")
		}
		sb.WriteString("  }\n}\n")
		writeFileT(t, path, sb.String())
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		res := runOne(t, engineIn(dir), Target{Path: path, Format: JSON, Managed: map[string]any{"model": "new", "added": 1}})
		runtime.ReadMemStats(&after)
		if res.Err != nil {
			t.Fatal(res.Err)
		}
		if got := after.TotalAlloc - before.TotalAlloc; got > 64<<20 {
			t.Errorf("allocated %d MiB to update a %d-line file", got>>20, n)
		}
	})
}
