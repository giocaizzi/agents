package cli

import (
	"context"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/harness"
	"github.com/giocaizzi/agents/internal/pluginreg"
)

// pluginFindings checks the plugin selection of r against the registry checkout for agents check.
func (d Deps) pluginFindings(_ context.Context, r *config.Resolved) []finding.Finding {
	c := &r.Config
	enabled := make(map[string][]string, len(c.Plugins.Enabled))
	for name, hs := range c.Plugins.Enabled {
		for _, h := range hs {
			enabled[name] = append(enabled[name], string(h))
		}
	}
	valid := make([]string, 0, len(config.All()))
	for _, h := range config.All() {
		valid = append(valid, string(h))
	}
	return pluginreg.Check(pluginreg.Selection{
		Checkout:  harness.RegistryDir(c, r.Dirs),
		Enabled:   enabled,
		Disabled:  c.Plugins.Disabled,
		Harnesses: valid,
	})
}
