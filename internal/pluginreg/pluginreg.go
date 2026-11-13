// Package pluginreg checks the plugin selection of the configuration against
// the registry checkout.
package pluginreg

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/giocaizzi/agents/internal/finding"
)

// Selection is what the configuration says about plugins.
type Selection struct {
	// Checkout is the registry checkout directory; "" when no registry is configured.
	Checkout string
	// Enabled maps each plugin to the harnesses it is enabled for, as written.
	Enabled map[string][]string
	// Disabled lists the plugins that stay off.
	Disabled []string
	// Harnesses are the valid harness names.
	Harnesses []string
}

// Names lists the plugin directories of the registry checkout at dir, sorted.
func Names(dir string) []string {
	if dir == "" {
		return nil
	}
	ents, err := os.ReadDir(filepath.Join(dir, "plugins"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Check validates s: unknown harness names are errors; an enabled plugin
// missing from the checkout is an error, while a missing checkout and a
// disabled plugin that is not enabled anywhere are warnings.
func Check(s Selection) []finding.Finding {
	var out []finding.Finding
	names := make([]string, 0, len(s.Enabled))
	for p := range s.Enabled {
		names = append(names, p)
	}
	sort.Strings(names)

	for _, p := range names {
		for _, h := range s.Enabled[p] {
			if !slices.Contains(s.Harnesses, h) {
				out = append(out, finding.New("plugins.enabled."+p, "unknown harness %s", h))
			}
		}
	}
	for _, p := range s.Disabled {
		if len(s.Enabled[p]) == 0 {
			out = append(out, finding.Warn("plugins.disabled", "%s is not enabled for any harness", p))
		}
	}
	if len(names) == 0 {
		return out
	}
	if s.Checkout == "" {
		return append(out, finding.Warn("plugins", "registry checkout missing; run agents plugins update"))
	}
	if _, err := os.Stat(filepath.Join(s.Checkout, "plugins")); err != nil {
		return append(out, finding.Warn("plugins", "registry checkout missing; run agents plugins update"))
	}
	for _, p := range names {
		if _, err := os.Stat(filepath.Join(s.Checkout, "plugins", p)); err != nil {
			out = append(out, finding.New("plugins.enabled."+p, "not a plugin of the registry checkout"))
		}
	}
	return out
}

// ValidateFile is a placeholder for the removed harness.toml check; the check
// command stops calling it when it reads the configuration instead.
func ValidateFile(_ context.Context, _, _ string) ([]finding.Finding, error) {
	return nil, nil
}
