package apply

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestPatchInPlace runs an update twice (add/replace, then drop) over live
// text and checks the exact bytes: comments, order and layout outside the
// managed keys must not move.
func TestPatchInPlace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		format  Format
		live    string
		first   map[string]any
		second  map[string]any
		wantOne string
		wantTwo string
	}{
		{
			name:   "jsonc nested insert, replace and removal",
			format: JSONC,
			live:   "{\n  // keep\n  \"mine\": 1, // inline\n  \"servers\": {\n    /* block */\n    \"own\": {\"a\": 1}\n  },\n}\n",
			first: map[string]any{
				"servers": map[string]any{"docs": map[string]any{"url": "u"}},
				"theme":   "dark",
			},
			second:  map[string]any{"theme": "light"},
			wantOne: "{\n  // keep\n  \"mine\": 1, // inline\n  \"servers\": {\n    /* block */\n    \"own\": {\"a\": 1},\n    \"docs\": {\n      \"url\": \"u\"\n    }\n  },\n  \"theme\": \"dark\",\n}\n",
			wantTwo: "{\n  // keep\n  \"mine\": 1, // inline\n  \"servers\": {\n    /* block */\n    \"own\": {\"a\": 1}\n  },\n  \"theme\": \"light\",\n}\n",
		},
		{
			name:    "json keeps four-space indent and tabs unit",
			format:  JSON,
			live:    "{\n    \"mine\": 1\n}\n",
			first:   map[string]any{"a": map[string]any{"b": 1}},
			second:  map[string]any{},
			wantOne: "{\n    \"mine\": 1,\n    \"a\": {\n        \"b\": 1\n    }\n}\n",
			wantTwo: "{\n    \"mine\": 1\n}\n",
		},
		{
			name:    "toml scalar replace, root insert and table insert",
			format:  TOML,
			live:    "# my setup\nzeta = 1 # why\nalpha = 2\n\n[tools]\nx = 'a'\n\n[other]\nk = 1\n",
			first:   map[string]any{"model": "m", "tools": map[string]any{"x": "b", "y": true}},
			second:  map[string]any{"model": "n"},
			wantOne: "# my setup\nzeta = 1 # why\nalpha = 2\nmodel = \"m\"\n\n[tools]\nx = \"b\"\ny = true\n\n[other]\nk = 1\n",
			wantTwo: "# my setup\nzeta = 1 # why\nalpha = 2\nmodel = \"n\"\n\n\n[other]\nk = 1\n",
		},
		{
			name:    "toml removes a whole managed table and keeps comments around",
			format:  TOML,
			live:    "[a]\nu = 1\n\n# note\n[mine]\nz = 1\n",
			first:   map[string]any{"mcp_servers": map[string]any{"docs": map[string]any{"url": "u", "args": []any{"1", "2"}}}},
			second:  map[string]any{},
			wantOne: "[a]\nu = 1\n\n# note\n[mine]\nz = 1\n\n[mcp_servers.docs]\nargs = [\"1\", \"2\"]\nurl = \"u\"\n",
			wantTwo: "[a]\nu = 1\n\n# note\n[mine]\nz = 1\n\n",
		},
		{
			name:    "toml into a file with no trailing newline and quoted keys",
			format:  TOML,
			live:    "a = 1",
			first:   map[string]any{"projects": map[string]any{"/p/x y": map[string]any{"trust": "ok"}}},
			second:  map[string]any{},
			wantOne: "a = 1\n\n[projects.\"/p/x y\"]\ntrust = \"ok\"\n",
			wantTwo: "a = 1\n\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "f")
			writeFileT(t, path, tt.live)
			e := engineIn(dir)
			// A path that was never managed has no manifest entry: both steps run on one.
			if res := runOne(t, e, Target{Path: path, Format: tt.format, Managed: tt.first}); res.Err != nil {
				t.Fatal(res.Err)
			}
			if diff := cmp.Diff(tt.wantOne, readFileT(t, path)); diff != "" {
				t.Errorf("after first run (-want +got):\n%s", diff)
			}
			if res := runOne(t, e, Target{Path: path, Format: tt.format, Managed: tt.second}); res.Err != nil {
				t.Fatal(res.Err)
			}
			if diff := cmp.Diff(tt.wantTwo, readFileT(t, path)); diff != "" {
				t.Errorf("after second run (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTOMLInlineTableRewrittenWhole(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	writeFileT(t, path, "srv = { a = 1, b = 2 } # keep\n")
	managed := map[string]any{"srv": map[string]any{"a": 5}}
	if res := runOne(t, engineIn(dir), Target{Path: path, Format: TOML, Managed: managed}); res.Err != nil {
		t.Fatal(res.Err)
	}
	got := readFileT(t, path)
	if !strings.Contains(got, "a = 5") || !strings.Contains(got, "# keep") {
		t.Errorf("file = %q", got)
	}
}

func TestPatchRefusesToWriteAWrongDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	// A dotted-key parent cannot take a [table] header; the engine must fail, not write.
	live := "a.b = 1\n"
	writeFileT(t, path, live)
	res := runOne(t, engineIn(dir), Target{Path: path, Format: TOML, Managed: map[string]any{"a": map[string]any{"c": 2}}})
	if res.Err == nil {
		t.Fatalf("want an error, file now %q", readFileT(t, path))
	}
	if got := readFileT(t, path); got != live {
		t.Errorf("file was touched: %q", got)
	}
}
