package cli

import (
	"errors"
	"io/fs"
	"maps"
	"slices"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/harness"
)

// modelFindings checks the model tiers and agent assignments of r for agents check.
func (d Deps) modelFindings(r *config.Resolved) []finding.Finding {
	c := &r.Config
	if len(c.Models.Assign) == 0 {
		return nil
	}
	agents, err := harness.InstalledAgents(harness.RegistryDir(c, r.Dirs))
	var out []finding.Finding
	switch {
	case errors.Is(err, fs.ErrNotExist):
		out = append(out, finding.Warn("models.assign",
			"registry checkout not found (run agents plugins update): assignments are not checked against the installed agents"))
	case err != nil:
		out = append(out, finding.Warn("models.assign", "list installed agents: %v", err))
	default:
		for _, key := range slices.Sorted(maps.Keys(c.Models.Assign)) {
			if !slices.ContainsFunc(agents, func(id string) bool { return harness.MatchAgent(key, id) }) {
				out = append(out, finding.Warn("models.assign."+key, "matches no installed agent"))
			}
		}
	}

	assigned := harness.ResolveAssign(c, agents)
	for _, id := range slices.Sorted(maps.Keys(assigned)) {
		tier := c.Models.Tiers[assigned[id]]
		for _, h := range harness.Selected(r.Harnesses) {
			if tier.Model(h) != "" && !harness.SupportsPerAgent(h) {
				out = append(out, finding.Note("models.assign",
					"%s: %s keeps its plugin default (no per-agent setting)", id, h))
			}
		}
	}
	return out
}
