// Package pluginreg checks .chezmoidata/harness.toml against the upstream harness repo.
package pluginreg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/finding"
)

// HarnessConfig represents the structure of .chezmoidata/harness.toml.
type HarnessConfig struct {
	PersonalPlugins []string            `toml:"harnessPersonalPlugins"`
	Plugins         map[string][]string `toml:"harnessPlugins"`
	Targets         map[string][]string `toml:"harnessTargets"`
}

// DefaultHarnesses returns the fallback list of valid harnesses if .chezmoi.toml.tmpl is not found.
func DefaultHarnesses() []string {
	return []string{"claude", "copilot", "vscode", "codex", "opencode", "pi", "antigravity"}
}

var harnessList = regexp.MustCompile(`\$all\s*:=\s*list((?:\s+"[a-z]+")+)`)

// ExtractHarnesses parses the allowed harnesses from .chezmoi.toml.tmpl content.
func ExtractHarnesses(tmpl []byte) ([]string, error) {
	m := harnessList.FindSubmatch(tmpl)
	if m == nil {
		return nil, errors.New(`config template: no $all := list harness list`)
	}
	return strings.Fields(strings.ReplaceAll(string(m[1]), `"`, "")), nil
}

// ResolveUpstream finds the list of plugin directory names from a local dir, standard locations, or gh api.
func ResolveUpstream(ctx context.Context, explicitHarnessDir string) ([]string, error) {
	var candidates []string
	if explicitHarnessDir != "" {
		candidates = append(candidates, explicitHarnessDir)
	}
	if env := os.Getenv("HARNESS_DIR"); env != "" {
		candidates = append(candidates, env)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Documents", "github", "harness"),
			filepath.Join(home, ".local", "share", "giocaizzi-harness"),
		)
	}

	for _, cand := range candidates {
		pDir := filepath.Join(cand, "plugins")
		ents, err := os.ReadDir(pDir)
		if err == nil {
			var out []string
			for _, e := range ents {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					out = append(out, e.Name())
				}
			}
			if len(out) > 0 {
				sort.Strings(out)
				return out, nil
			}
		}
	}

	// Fallback to gh api
	cmd := exec.CommandContext(ctx, "gh", "api", "repos/giocaizzi/harness/contents/plugins", "--jq", `.[] | select(.type == "dir") | .name`)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("list harness plugins (set --harness-dir or sign in with gh): %w", err)
	}
	fields := strings.Fields(stdout.String())
	sort.Strings(fields)
	return fields, nil
}

// Check compares the plugins listed here with the plugin directories upstream.
// plugins maps plugin to the harnesses it is enabled for; choices are the
// harnesses a machine can select.
func Check(plugins map[string][]string, personal, choices, upstream []string) []finding.Finding {
	var out []finding.Finding
	listed := make([]string, 0, len(plugins))
	for p, hs := range plugins {
		listed = append(listed, p)
		for _, h := range hs {
			if !slices.Contains(choices, h) {
				out = append(out, finding.New("harnessPlugins."+p, "unknown harness %s", h))
			}
		}
	}
	sort.Strings(listed)
	for _, p := range personal {
		if _, ok := plugins[p]; !ok {
			out = append(out, finding.New("harnessPersonalPlugins", "%s is not in [harnessPlugins]", p))
		}
	}
	if len(upstream) == 0 {
		return append(out, finding.New("", "no plugin directories found upstream"))
	}
	for _, p := range listed {
		if !slices.Contains(upstream, p) {
			out = append(out, finding.New("harnessPlugins."+p, "not a plugin of giocaizzi/harness"))
		}
	}
	for _, p := range upstream {
		if _, ok := plugins[p]; !ok {
			out = append(out, finding.New("harnessPlugins."+p, "plugin of giocaizzi/harness missing here"))
		}
	}
	return out
}

// ValidateFile parses harness.toml at path, resolves upstream plugins, and checks consistency.
func ValidateFile(ctx context.Context, harnessPath, harnessDir string) ([]finding.Finding, error) {
	data, err := os.ReadFile(filepath.Clean(harnessPath))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", harnessPath, err)
	}

	var cfg HarnessConfig
	if unmarshalErr := toml.Unmarshal(data, &cfg); unmarshalErr != nil {
		return []finding.Finding{finding.New(harnessPath, "parse: %v", unmarshalErr)}, nil
	}

	// Try reading choices from .chezmoi.toml.tmpl in surrounding directories
	choices := DefaultHarnesses()
	dir := filepath.Dir(harnessPath)
	for range 4 {
		tmplPath := filepath.Clean(filepath.Join(dir, ".chezmoi.toml.tmpl"))
		if tmplBytes, readErr := os.ReadFile(tmplPath); readErr == nil {
			if parsed, parseErr := ExtractHarnesses(tmplBytes); parseErr == nil && len(parsed) > 0 {
				choices = parsed
				break
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	upstream, err := ResolveUpstream(ctx, harnessDir)
	if err != nil {
		return nil, err
	}

	return Check(cfg.Plugins, cfg.PersonalPlugins, choices, upstream), nil
}
