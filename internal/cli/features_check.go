package cli

import (
	"maps"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/feature"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/harness"
)

// featureFindings reports the keys set in r that a selected harness ignores:
// an info for a key the harness has no setting for, a warning for one it has
// and agents does not write yet. Each key is reported once per status, naming
// every harness affected.
func (d Deps) featureFindings(r *config.Resolved) []finding.Finding {
	selected := harness.Selected(r.Harnesses)
	set := map[string]feature.Feature{}
	for path := range r.Origin {
		if f, ok := feature.Lookup(path); ok {
			set[f.Key] = f
		}
	}
	var out []finding.Finding
	for _, key := range slices.Sorted(maps.Keys(set)) {
		f := set[key]
		var none, planned []string
		for _, h := range selected {
			c, ok := f.Cells[string(h)]
			switch {
			case !ok:
			case c.Status == feature.StatusUnsupported:
				none = append(none, string(h)+" ("+c.Evidence+")")
			case c.Status == feature.StatusPlanned:
				planned = append(planned, string(h)+" ("+c.Evidence+")")
			case c.Status == feature.StatusRendered:
			}
		}
		if len(none) > 0 {
			out = append(out, finding.Note(key, "no effect on %s", strings.Join(none, "; ")))
		}
		if len(planned) > 0 {
			out = append(out, finding.Warn(key, "no effect on %s", strings.Join(planned, "; ")))
		}
	}
	return out
}
