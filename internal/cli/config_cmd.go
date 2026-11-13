package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/config"
)

// runConfig dispatches the config subcommands.
func (d Deps) runConfig(_ context.Context, args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == helpFlag || args[0] == helpCmd) {
		d.printHelp(d.Stdout, commandConfig, nil)
		return exitOK
	}
	if len(args) == 0 || args[0] != commandShow {
		d.printHelp(d.Stderr, commandConfig, nil)
		return exitUsage
	}
	fs := d.newFlags(commandConfig + " " + commandShow)
	cf := addConfigFlags(fs)
	asJSON := fs.Bool("json", false, "print JSON")
	origin := fs.Bool("origin", false, "name the layer that set each key")
	if err := fs.Parse(args[1:]); err != nil {
		return d.parseFailure(fs, err)
	}
	r, err := d.loadConfig(cf)
	if err != nil {
		fmt.Fprintln(d.Stderr, err)
		return exitFindings
	}

	var out string
	switch {
	case *origin:
		out, err = originLines(r)
	case *asJSON:
		var b []byte
		b, err = json.MarshalIndent(r.Config, "", "  ")
		out = string(b) + "\n"
	default:
		var b []byte
		b, err = toml.Marshal(r.Config)
		out = configHeader(r) + string(b)
	}
	if err != nil {
		fmt.Fprintf(d.Stderr, "config show: %v\n", err)
		return exitFindings
	}
	fmt.Fprint(d.Stdout, out)
	return exitOK
}

// configHeader names the inputs of the resolved config as TOML comments.
func configHeader(r *config.Resolved) string {
	var b strings.Builder
	files := "none"
	if len(r.Files) > 0 {
		files = strings.Join(r.Files, ", ")
	}
	profile := r.Profile
	if profile == "" {
		profile = "none"
	}
	names := make([]string, 0, len(r.Harnesses))
	for _, h := range r.Harnesses {
		names = append(names, string(h))
	}
	how := "configured"
	if r.HarnessesDetected {
		how = "detected"
	}
	fmt.Fprintf(&b, "# files: %s\n# profile: %s\n# harnesses (%s): %s\n\n", files, profile, how, strings.Join(names, ", "))
	return b.String()
}

// originLines prints one "key = value  # origin" line per leaf, sorted by key.
func originLines(r *config.Resolved) (string, error) {
	raw, err := json.Marshal(r.Config)
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	var tree map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&tree); err != nil {
		return "", fmt.Errorf("decode config: %w", err)
	}
	if len(r.Config.Harnesses) == 0 {
		names := make([]any, 0, len(r.Harnesses))
		for _, h := range r.Harnesses {
			names = append(names, string(h))
		}
		tree["harnesses"] = names
	}
	var b strings.Builder
	for _, key := range slices.Sorted(maps.Keys(r.Origin)) {
		v, ok := lookup(tree, key)
		if !ok {
			continue
		}
		val, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("format %s: %w", key, err)
		}
		fmt.Fprintf(&b, "%s = %s  # %s\n", key, val, r.Origin[key])
	}
	return b.String(), nil
}

// lookup follows a dotted key through nested maps, joining segments that hold
// dots themselves (the loader's leaf keys are dotted paths).
func lookup(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	for i := range len(key) {
		if key[i] != '.' {
			continue
		}
		sub, ok := m[key[:i]].(map[string]any)
		if !ok {
			continue
		}
		if v, ok := lookup(sub, key[i+1:]); ok {
			return v, true
		}
	}
	return nil, false
}

// runSchema prints the JSON Schema of the configuration.
func (d Deps) runSchema(_ context.Context, args []string) int {
	fs := d.newFlags(commandSchema)
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	b, err := config.Schema()
	if err != nil {
		fmt.Fprintf(d.Stderr, "schema: %v\n", err)
		return exitFindings
	}
	if _, err := d.Stdout.Write(b); err != nil {
		fmt.Fprintf(d.Stderr, "schema: %v\n", err)
		return exitFindings
	}
	return exitOK
}
