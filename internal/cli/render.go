package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/harness"
)

// renderPlan renders the native files of r on this machine.
func (d Deps) renderPlan(r *config.Resolved) (harness.Plan, error) {
	plan, err := harness.Render(r, d.harnessEnv(r))
	if err != nil {
		return plan, fmt.Errorf("render: %w", err)
	}
	return plan, nil
}

func (d Deps) runRender(_ context.Context, args []string) int {
	fs := d.newFlags(commandRender)
	cf := addConfigFlags(fs)
	only := fs.String("harness", "", "print only the targets of this harness")
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	r, err := d.loadConfig(cf)
	if err != nil {
		fmt.Fprintln(d.Stderr, err)
		return exitFindings
	}
	plan, err := d.renderPlan(r)
	for _, w := range plan.Warnings {
		fmt.Fprintf(d.Stderr, "warning: %s\n", w)
	}
	for _, n := range plan.Notes {
		fmt.Fprintf(d.Stderr, "note: %s\n", n)
	}
	if err != nil {
		fmt.Fprintln(d.Stderr, err)
		return exitFindings
	}
	want := strings.ToLower(strings.TrimSpace(*only))
	for _, t := range plan.Targets {
		if want != "" && t.Owner != want {
			continue
		}
		body, err := managedText(t)
		if err != nil {
			fmt.Fprintf(d.Stderr, "%s: %v\n", t.Path, err)
			return exitFindings
		}
		body = apply.Redact(body, t.Sensitive)
		note := ""
		if t.CheckOnly {
			note = ", check only"
		}
		fmt.Fprintf(d.Stdout, "# %s (%s, %s%s)\n%s", t.Path, t.Owner, formatName(t.Format), note, body)
		if !strings.HasSuffix(body, "\n") {
			fmt.Fprintln(d.Stdout)
		}
	}
	return exitOK
}

func formatName(f apply.Format) string {
	switch f {
	case apply.JSON:
		return "json"
	case apply.JSONC:
		return "jsonc"
	case apply.TOML:
		return "toml"
	case apply.Text:
		return "text"
	default:
		return "text"
	}
}

// managedText formats the keys agents manages in t.
func managedText(t apply.Target) (string, error) {
	switch t.Format {
	case apply.Text:
		s, ok := t.Managed.(string)
		if !ok {
			return "", errors.New("text target has non-text content")
		}
		return s, nil
	case apply.TOML:
		b, err := toml.Marshal(plainValue(t.Managed))
		if err != nil {
			return "", fmt.Errorf("marshal toml: %w", err)
		}
		return string(b), nil
	case apply.JSON, apply.JSONC:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(plainValue(t.Managed)); err != nil {
			return "", fmt.Errorf("encode json: %w", err)
		}
		return buf.String(), nil
	}
	return "", fmt.Errorf("unknown format %d", t.Format)
}

// plainValue converts ordered objects to plain maps and slices for the encoders.
func plainValue(v any) any {
	switch t := v.(type) {
	case *apply.Object:
		m := make(map[string]any, len(t.Keys))
		for _, k := range t.Keys {
			m[k] = plainValue(t.Values[k])
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = plainValue(e)
		}
		return out
	default:
		return v
	}
}
