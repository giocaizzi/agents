package instructions_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/instructions"
)

// vol makes a slash-rooted fixture path absolute on Windows (a drive letter).
var vol = filepath.VolumeName(os.TempDir())

func fakeRead(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		s, ok := files[filepath.ToSlash(path)]
		if !ok {
			return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
		}
		return []byte(s), nil
	}
}

func TestCompose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec instructions.Spec
		want []string
	}{
		{"order", instructions.Spec{
			Profiles: map[string][]string{"p": {"a", "b"}},
			Harness:  map[string][]string{"h": {"c"}},
			Cells:    map[string]map[string][]string{"p": {"h": {"d"}}},
		}, []string{"a", "b", "c", "d"}},
		{"dedup keeps first", instructions.Spec{
			Profiles: map[string][]string{"p": {"a", "b", "a"}},
			Harness:  map[string][]string{"h": {"b", "c"}},
			Cells:    map[string]map[string][]string{"p": {"h": {"a", "d", "c"}}},
		}, []string{"a", "b", "c", "d"}},
		{"profile only", instructions.Spec{Profiles: map[string][]string{"p": {"a"}}}, []string{"a"}},
		{"harness only", instructions.Spec{Harness: map[string][]string{"h": {"a"}}}, []string{"a"}},
		{"cell only", instructions.Spec{Cells: map[string]map[string][]string{"p": {"h": {"a"}}}}, []string{"a"}},
		{"other cell ignored", instructions.Spec{Cells: map[string]map[string][]string{"p": {"x": {"a"}}}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, tt.spec.Compose("p", "h")); diff != "" {
				t.Errorf("Compose mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPath(t *testing.T) {
	t.Parallel()
	s := instructions.Spec{Dir: vol + "/d", Layers: map[string]instructions.Layer{
		"a": {}, "b": {File: "sub/b.txt"}, "c": {File: vol + "/abs/c.md"},
	}}
	for name, want := range map[string]string{"a": vol + "/d/a.md", "b": vol + "/d/sub/b.txt", "c": vol + "/abs/c.md"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := filepath.ToSlash(s.Path(name)); got != want {
				t.Errorf("Path(%q) = %q, want %q", name, got, want)
			}
		})
	}
}

type renderCase struct {
	name     string
	spec     instructions.Spec
	files    map[string]string
	want     string
	wantKind instructions.ProblemKind
	wantErr  error
}

func renderCases() []renderCase {
	layers := func(names ...string) map[string]instructions.Layer {
		m := map[string]instructions.Layer{}
		for _, n := range names {
			m[n] = instructions.Layer{}
		}
		return m
	}
	return []renderCase{
		{
			name: "empty composition",
			spec: instructions.Spec{Dir: vol + "/d"},
			want: "",
		},
		{
			name:  "join and newline rule",
			spec:  instructions.Spec{Dir: vol + "/d", Layers: layers("a", "b"), Profiles: map[string][]string{"p": {"a", "b"}}},
			files: map[string]string{vol + "/d/a.md": "one\n\n\n", vol + "/d/b.md": "two"},
			want:  "one\n\ntwo\n",
		},
		{
			name: "context and vars",
			spec: instructions.Spec{
				Dir: vol + "/d", Layers: layers("a"), Profiles: map[string][]string{"p": {"a"}},
				Vars: map[string]any{"user": map[string]any{"name": "ann"}},
			},
			files: map[string]string{vol + "/d/a.md": "{{.profile}} {{.harness}} {{.os}} {{.hostname}} {{.vars.user.name}}\n"},
			want:  "p h linux box ann\n",
		},
		{
			name:  "relative file override",
			spec:  instructions.Spec{Dir: vol + "/d", Layers: map[string]instructions.Layer{"a": {File: "x/y.md"}}, Profiles: map[string][]string{"p": {"a"}}},
			files: map[string]string{vol + "/d/x/y.md": "rel"},
			want:  "rel\n",
		},
		{
			name:  "absolute file override",
			spec:  instructions.Spec{Dir: vol + "/d", Layers: map[string]instructions.Layer{"a": {File: vol + "/other/z.md"}}, Profiles: map[string][]string{"p": {"a"}}},
			files: map[string]string{vol + "/other/z.md": "abs"},
			want:  "abs\n",
		},
		{
			name:     "undeclared layer",
			spec:     instructions.Spec{Dir: vol + "/d", Profiles: map[string][]string{"p": {"ghost"}}},
			wantKind: instructions.UnknownLayer,
		},
		{
			name:     "missing file",
			spec:     instructions.Spec{Dir: vol + "/d", Layers: layers("a"), Profiles: map[string][]string{"p": {"a"}}},
			wantKind: instructions.MissingFile,
			wantErr:  fs.ErrNotExist,
		},
		{
			name:     "missingkey error",
			spec:     instructions.Spec{Dir: vol + "/d", Layers: layers("a"), Profiles: map[string][]string{"p": {"a"}}},
			files:    map[string]string{vol + "/d/a.md": "{{.vars.nope}}"},
			wantKind: instructions.RenderError,
		},
		{
			name:     "parse error",
			spec:     instructions.Spec{Dir: vol + "/d", Layers: layers("a"), Profiles: map[string][]string{"p": {"a"}}},
			files:    map[string]string{vol + "/d/a.md": "{{"},
			wantKind: instructions.RenderError,
		},
	}
}

func TestRender(t *testing.T) {
	t.Parallel()
	ctx := instructions.Context{Profile: "p", Harness: "h", OS: "linux", Hostname: "box"}
	for _, tt := range renderCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.spec.Render(ctx, fakeRead(tt.files))
			if tt.wantKind == 0 {
				if err != nil {
					t.Fatalf("Render error: %v", err)
				}
				if got != tt.want {
					t.Errorf("Render = %q, want %q", got, tt.want)
				}
				return
			}
			if got != "" {
				t.Errorf("Render = %q on error, want empty", got)
			}
			checkProblem(t, err, tt)
		})
	}
}

func checkProblem(t *testing.T, err error, tt renderCase) {
	t.Helper()
	var p *instructions.Problem
	if !errors.As(err, &p) {
		t.Fatalf("Render error = %v, want *Problem", err)
	}
	if p.Kind != tt.wantKind || p.Profile != "p" || p.Harness != "h" || p.Layer == "" {
		t.Errorf("problem = %+v, want kind %v for p/h with layer", p, tt.wantKind)
	}
	if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
		t.Errorf("errors.Is(%v, %v) = false", err, tt.wantErr)
	}
}

func TestMatrix(t *testing.T) {
	t.Parallel()
	s := instructions.Spec{
		Profiles: map[string][]string{"b": {"x"}},
		Harness:  map[string][]string{"h2": {"y"}},
	}
	got := s.Matrix([]string{"b", "a"}, []string{"h2", "h1"})
	want := []instructions.Row{
		{Profile: "a", Harness: "h2", Layers: []string{"y"}},
		{Profile: "a", Harness: "h1"},
		{Profile: "b", Harness: "h2", Layers: []string{"x", "y"}},
		{Profile: "b", Harness: "h1", Layers: []string{"x"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Matrix mismatch (-want +got):\n%s", diff)
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	s := instructions.Spec{
		Dir:      vol + "/d",
		Layers:   map[string]instructions.Layer{"ok": {}, "gone": {}, "bad": {}},
		Profiles: map[string][]string{"p": {"ok", "ghost"}},
		Harness:  map[string][]string{"h1": {"gone"}, "h2": {"bad", "ok"}},
	}
	files := map[string]string{vol + "/d/ok.md": "fine", vol + "/d/bad.md": "{{.vars.nope}}"}
	ctxFor := func(p, h string) instructions.Context { return instructions.Context{Profile: p, Harness: h} }
	got := s.Check([]string{"p"}, []string{"h1", "h2"}, ctxFor, fakeRead(files))
	type key struct {
		H, L string
		K    instructions.ProblemKind
	}
	keys := make([]key, 0, len(got))
	for _, p := range got {
		if p.Err == nil {
			t.Errorf("problem without Err: %+v", p)
		}
		keys = append(keys, key{p.Harness, p.Layer, p.Kind})
	}
	want := []key{
		{"h1", "ghost", instructions.UnknownLayer},
		{"h1", "gone", instructions.MissingFile},
		{"h2", "ghost", instructions.UnknownLayer},
		{"h2", "bad", instructions.RenderError},
	}
	if diff := cmp.Diff(want, keys); diff != "" {
		t.Errorf("Check mismatch (-want +got):\n%s", diff)
	}
}

func FuzzCompose(f *testing.F) {
	f.Add("a,b,a", "b,c", "c,d")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, p, h, c string) {
		split := func(s string) []string {
			if s == "" {
				return nil
			}
			var out []string
			start := 0
			for i := 0; i <= len(s); i++ {
				if i == len(s) || s[i] == ',' {
					out = append(out, s[start:i])
					start = i + 1
				}
			}
			return out
		}
		pl, hl, cl := split(p), split(h), split(c)
		s := instructions.Spec{
			Profiles: map[string][]string{"p": pl},
			Harness:  map[string][]string{"h": hl},
			Cells:    map[string]map[string][]string{"p": {"h": cl}},
		}
		got := s.Compose("p", "h")
		all := slices.Concat(pl, hl, cl)
		seen := map[string]bool{}
		var want []string
		for _, n := range all {
			if !seen[n] {
				seen[n] = true
				want = append(want, n)
			}
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Compose mismatch (-want +got):\n%s", diff)
		}
	})
}
