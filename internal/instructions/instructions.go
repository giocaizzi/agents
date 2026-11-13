// Package instructions composes and renders layered agent instructions.
//
// Layers are Markdown files rendered with text/template. Each (profile,
// harness) cell of the matrix is the profile's layers, then the harness's,
// then the cell's own, deduplicated keeping the first occurrence.
package instructions

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
)

// Spec declares the layers and which of them each matrix cell uses.
type Spec struct {
	Dir      string                         // absolute directory holding layer files
	Layers   map[string]Layer               // declared layers
	Vars     map[string]any                 // free-form template data, as .vars
	Profiles map[string][]string            // profile -> layers for every harness
	Harness  map[string][]string            // harness -> layers for every profile
	Cells    map[string]map[string][]string // profile -> harness -> layers
}

// Layer declares one layer.
type Layer struct {
	// File overrides <Dir>/<name>.md; relative to Dir unless absolute.
	File string
}

// Context is the per-cell template data.
type Context struct{ Profile, Harness, OS, Hostname string }

// ProblemKind classifies a Problem.
type ProblemKind int

// Problem kinds.
const (
	UnknownLayer ProblemKind = iota + 1
	MissingFile
	RenderError
)

// Problem is one failure while rendering one layer of one cell. It
// implements error and unwraps to its cause, so errors.Is(err,
// fs.ErrNotExist) identifies a MissingFile.
type Problem struct { //nolint:errname // name fixed by the package API
	Profile, Harness, Layer, Path string
	Kind                          ProblemKind
	Err                           error
}

func (p *Problem) Error() string {
	cell := fmt.Sprintf("profile %q, harness %q", p.Profile, p.Harness)
	switch p.Kind {
	case UnknownLayer:
		return fmt.Sprintf("layer %q is not declared (%s)", p.Layer, cell)
	case MissingFile:
		return fmt.Sprintf("layer %q: read %s (%s): %v", p.Layer, p.Path, cell, p.Err)
	case RenderError:
		return fmt.Sprintf("layer %q: render %s (%s): %v", p.Layer, p.Path, cell, p.Err)
	}
	return fmt.Sprintf("layer %q (%s): %v", p.Layer, cell, p.Err)
}

// Unwrap returns the underlying cause.
func (p *Problem) Unwrap() error { return p.Err }

// Row is one matrix cell and its composed layers.
type Row struct {
	Profile, Harness string
	Layers           []string
}

// Compose returns the layer names of a cell: profile, harness and cell lists
// in that order, deduplicated keeping the first occurrence.
func (s Spec) Compose(profile, harness string) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range [][]string{s.Profiles[profile], s.Harness[harness], s.Cells[profile][harness]} {
		for _, name := range list {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// Path returns the file of a layer: its declared File (relative to Dir
// unless absolute), or <Dir>/<name>.md.
func (s Spec) Path(name string) string {
	f := s.Layers[name].File
	switch {
	case f == "":
		return filepath.Join(s.Dir, name+".md")
	case filepath.IsAbs(f):
		return f
	}
	return filepath.Join(s.Dir, f)
}

// Render composes the cell of ctx and renders it: layers are trimmed of
// trailing newlines, joined by a blank line, and the result ends with one
// newline. An empty composition renders "". The error is a *Problem.
func (s Spec) Render(ctx Context, read func(path string) ([]byte, error)) (string, error) {
	var parts []string
	for _, name := range s.Compose(ctx.Profile, ctx.Harness) {
		text, p := s.renderLayer(ctx, name, read)
		if p != nil {
			return "", p
		}
		parts = append(parts, text)
	}
	out := strings.Join(parts, "\n\n")
	if out == "" {
		return "", nil
	}
	return out + "\n", nil
}

func (s Spec) renderLayer(ctx Context, name string, read func(string) ([]byte, error)) (string, *Problem) {
	p := &Problem{Profile: ctx.Profile, Harness: ctx.Harness, Layer: name}
	if _, ok := s.Layers[name]; !ok {
		p.Kind = UnknownLayer
		p.Err = errors.New("undeclared layer")
		return "", p
	}
	p.Path = s.Path(name)
	src, err := read(p.Path)
	if err != nil {
		p.Kind = MissingFile
		p.Err = err
		return "", p
	}
	vars := s.Vars
	if vars == nil {
		vars = map[string]any{}
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(src))
	if err == nil {
		var buf bytes.Buffer
		err = tmpl.Execute(&buf, map[string]any{
			"harness":  ctx.Harness,
			"profile":  ctx.Profile,
			"os":       ctx.OS,
			"hostname": ctx.Hostname,
			"vars":     vars,
		})
		if err == nil {
			return strings.TrimRight(buf.String(), "\n"), nil
		}
	}
	p.Kind = RenderError
	p.Err = err
	return "", p
}

// Check renders every cell of profiles x harnesses and returns all problems,
// each layer problem once per cell.
func (s Spec) Check(profiles, harnesses []string, ctxFor func(profile, harness string) Context, read func(string) ([]byte, error)) []Problem {
	var out []Problem
	for _, row := range s.Matrix(profiles, harnesses) {
		ctx := ctxFor(row.Profile, row.Harness)
		for _, name := range row.Layers {
			if _, p := s.renderLayer(ctx, name, read); p != nil {
				out = append(out, *p)
			}
		}
	}
	return out
}

// Matrix lists every cell, sorted by profile, then in the given harness order.
func (s Spec) Matrix(profiles, harnesses []string) []Row {
	ps := slices.Sorted(slices.Values(profiles))
	rows := make([]Row, 0, len(ps)*len(harnesses))
	for _, p := range ps {
		for _, h := range harnesses {
			rows = append(rows, Row{Profile: p, Harness: h, Layers: s.Compose(p, h)})
		}
	}
	return rows
}
