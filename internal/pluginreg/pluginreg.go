// Package pluginreg checks harness plugin registries and configs.
package pluginreg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
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

// DefaultHarnessRegistry is the default upstream plugin repository.
const DefaultHarnessRegistry = "giocaizzi/harness"

// StandalonePlugin describes an individual plugin external to any registry.
type StandalonePlugin struct {
	Source    string   `toml:"source"`
	Path      string   `toml:"path"`
	Harnesses []string `toml:"harnesses"`
	Version   string   `toml:"version"`
}

// HarnessConfig represents the structure of .chezmoidata/harness.toml or agents.toml plugin configs.
type HarnessConfig struct {
	Registry        string                      `toml:"harnessRegistry"`
	PersonalPlugins []string                    `toml:"harnessPersonalPlugins"`
	Plugins         map[string][]string         `toml:"harnessPlugins"`
	Standalone      map[string]StandalonePlugin `toml:"harnessStandalone"`
	Targets         map[string][]string         `toml:"harnessTargets"`
}

// ResolveNamespace converts a registry repo or URL into a filesystem/cache-safe namespace string.
func ResolveNamespace(registry string) string {
	if registry == "" {
		return "harness"
	}
	r := strings.TrimPrefix(registry, "https://github.com/")
	r = strings.TrimPrefix(r, "http://github.com/")
	r = strings.TrimPrefix(r, "github.com/")
	r = strings.TrimSuffix(r, ".git")
	r = strings.TrimSuffix(r, "/")
	return strings.ReplaceAll(r, "/", "-")
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
	return ResolveUpstreamRegistry(ctx, explicitHarnessDir, "")
}

func candidateDirs(explicitDir, registry string) []string {
	repoName := registry
	if idx := strings.LastIndex(repoName, "/"); idx >= 0 {
		repoName = repoName[idx+1:]
	}

	var candidates []string
	if explicitDir != "" {
		candidates = append(candidates, explicitDir)
	}
	if env := os.Getenv("HARNESS_DIR"); env != "" {
		candidates = append(candidates, env)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Documents", "github", repoName),
			filepath.Join(home, ".local", "share", ResolveNamespace(registry)),
			filepath.Join(home, "Documents", "github", "harness"),
			filepath.Join(home, ".local", "share", "giocaizzi-harness"),
		)
	}
	return candidates
}

// ResolveUpstreamRegistry finds plugin directory names for the specified registry repository.
func ResolveUpstreamRegistry(ctx context.Context, explicitHarnessDir, registry string) ([]string, error) {
	if registry == "" {
		if env := os.Getenv("HARNESS_REGISTRY"); env != "" {
			registry = env
		} else if env := os.Getenv("HARNESS_REPO"); env != "" {
			registry = env
		} else {
			registry = DefaultHarnessRegistry
		}
	}

	for _, cand := range candidateDirs(explicitHarnessDir, registry) {
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

	regSlug := strings.TrimPrefix(registry, "https://github.com/")
	regSlug = strings.TrimPrefix(regSlug, "github.com/")
	regSlug = strings.TrimSuffix(regSlug, ".git")

	//nolint:gosec // regSlug is constructed from registry configuration
	cmd := exec.CommandContext(ctx, "gh", "api", fmt.Sprintf("repos/%s/contents/plugins", regSlug), "--jq", `.[] | select(.type == "dir") | .name`)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("list %s plugins (set --harness-dir or sign in with gh): %w", regSlug, err)
	}
	fields := strings.Fields(stdout.String())
	sort.Strings(fields)
	return fields, nil
}

// Check compares the plugins listed here with the plugin directories upstream.
func Check(plugins map[string][]string, personal, choices, upstream []string) []finding.Finding {
	return CheckRegistry(plugins, personal, choices, upstream, DefaultHarnessRegistry)
}

// CheckRegistry compares the plugins with upstream for a specific registry name.
func CheckRegistry(plugins map[string][]string, personal, choices, upstream []string, registry string) []finding.Finding {
	if registry == "" {
		registry = DefaultHarnessRegistry
	}
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
			out = append(out, finding.New("harnessPlugins."+p, "not a plugin of %s", registry))
		}
	}
	for _, p := range upstream {
		if _, ok := plugins[p]; !ok {
			out = append(out, finding.New("harnessPlugins."+p, "plugin of %s missing here", registry))
		}
	}
	return out
}

// MergeAgentsConfig merges configuration from agents.toml [plugins] table into cfg.
func MergeAgentsConfig(cfg *HarnessConfig, data []byte) {
	var agentsCfg struct {
		Plugins struct {
			Registry   string                      `toml:"registry"`
			Enabled    map[string][]string         `toml:"enabled"`
			Personal   []string                    `toml:"personal"`
			Standalone map[string]StandalonePlugin `toml:"standalone"`
			External   map[string]StandalonePlugin `toml:"external"`
		} `toml:"plugins"`
	}
	if aErr := toml.Unmarshal(data, &agentsCfg); aErr != nil {
		return
	}
	if agentsCfg.Plugins.Registry != "" && cfg.Registry == "" {
		cfg.Registry = agentsCfg.Plugins.Registry
	}
	if len(agentsCfg.Plugins.Enabled) > 0 && len(cfg.Plugins) == 0 {
		cfg.Plugins = agentsCfg.Plugins.Enabled
	}
	if len(agentsCfg.Plugins.Personal) > 0 && len(cfg.PersonalPlugins) == 0 {
		cfg.PersonalPlugins = agentsCfg.Plugins.Personal
	}
	if len(agentsCfg.Plugins.Standalone) > 0 {
		if cfg.Standalone == nil {
			cfg.Standalone = make(map[string]StandalonePlugin)
		}
		maps.Copy(cfg.Standalone, agentsCfg.Plugins.Standalone)
	}
	if len(agentsCfg.Plugins.External) > 0 {
		if cfg.Standalone == nil {
			cfg.Standalone = make(map[string]StandalonePlugin)
		}
		maps.Copy(cfg.Standalone, agentsCfg.Plugins.External)
	}
}

func findChoices(startPath string) []string {
	choices := DefaultHarnesses()
	dir := filepath.Dir(startPath)
	for range 4 {
		tmplPath := filepath.Clean(filepath.Join(dir, ".chezmoi.toml.tmpl"))
		if tmplBytes, readErr := os.ReadFile(tmplPath); readErr == nil {
			if parsed, parseErr := ExtractHarnesses(tmplBytes); parseErr == nil && len(parsed) > 0 {
				return parsed
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return choices
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

	MergeAgentsConfig(&cfg, data)
	choices := findChoices(harnessPath)

	reg := cfg.Registry
	if reg == "" {
		reg = DefaultHarnessRegistry
	}

	upstream, err := ResolveUpstreamRegistry(ctx, harnessDir, reg)
	if err != nil {
		return nil, err
	}

	return CheckRegistry(cfg.Plugins, cfg.PersonalPlugins, choices, upstream, reg), nil
}
