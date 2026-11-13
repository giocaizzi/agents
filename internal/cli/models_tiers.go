package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"text/tabwriter"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/harness"
)

// How an agent's model reaches a harness.
const (
	modeAssigned      = "assigned"       // written through the harness's per-agent setting
	modePluginDefault = "plugin default" // the tier has no model for the harness
	modeSession       = "session model"  // written as the harness's main session model
	modeUnsupported   = "unsupported"    // no per-agent setting: the plugin default applies
	noModel           = "-"
)

// tiersReport is the data of agents models tiers.
type tiersReport struct {
	Harnesses []string   `json:"harnesses"`
	Default   string     `json:"default,omitempty"`
	Tiers     []tierRow  `json:"tiers"`
	Agents    []agentRow `json:"agents"`
}

// tierRow is the model of one tier per harness.
type tierRow struct {
	Name   string            `json:"name"`
	Models map[string]string `json:"models"`
}

// agentRow is one assigned agent (or the main session) with its tier and,
// per harness, the effective model and how it is applied.
type agentRow struct {
	Agent   string                `json:"agent"`
	Tier    string                `json:"tier"`
	Effects map[string]agentModel `json:"harnesses"`
}

type agentModel struct {
	Model string `json:"model"`
	Mode  string `json:"mode"`
}

// runModelsTiers prints the tier x harness model matrix and the agent assignments.
func (d Deps) runModelsTiers(_ context.Context, args []string) int {
	fs := d.newFlags(commandModels + " " + commandTiers)
	cf := addConfigFlags(fs)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	r, err := d.loadConfig(cf)
	if err != nil {
		fmt.Fprintf(d.Stderr, "models tiers: %v\n", err)
		return exitFindings
	}
	rep := buildTiersReport(r, d.harnessEnv(r).Agents)
	if *asJSON {
		enc := json.NewEncoder(d.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(d.Stderr, "models tiers: %v\n", err)
			return exitFindings
		}
		return exitOK
	}
	printTiers(d, rep)
	return exitOK
}

// buildTiersReport resolves the matrix and the assignments for the selected harnesses.
func buildTiersReport(r *config.Resolved, agents []string) tiersReport {
	c := &r.Config
	selected := harness.Selected(r.Harnesses)
	rep := tiersReport{Default: c.Models.Default, Tiers: []tierRow{}, Agents: []agentRow{}}
	for _, h := range selected {
		rep.Harnesses = append(rep.Harnesses, string(h))
	}
	for _, name := range slices.Sorted(maps.Keys(c.Models.Tiers)) {
		row := tierRow{Name: name, Models: map[string]string{}}
		for _, h := range selected {
			row.Models[string(h)] = orDash(c.Models.Tiers[name].Model(h))
		}
		rep.Tiers = append(rep.Tiers, row)
	}
	if t, ok := harness.DefaultTier(c); ok {
		row := agentRow{Agent: "(session)", Tier: c.Models.Default, Effects: map[string]agentModel{}}
		for _, h := range selected {
			row.Effects[string(h)] = sessionEffect(t, h)
		}
		rep.Agents = append(rep.Agents, row)
	}
	assigned := harness.ResolveAssign(c, agents)
	for _, id := range slices.Sorted(maps.Keys(assigned)) {
		t := c.Models.Tiers[assigned[id]]
		row := agentRow{Agent: id, Tier: assigned[id], Effects: map[string]agentModel{}}
		for _, h := range selected {
			row.Effects[string(h)] = agentEffect(t, h)
		}
		rep.Agents = append(rep.Agents, row)
	}
	return rep
}

func sessionEffect(t config.Tier, h config.Harness) agentModel {
	switch {
	case !harness.SupportsSession(h):
		return agentModel{Model: noModel, Mode: modeUnsupported}
	case t.Model(h) == "":
		return agentModel{Model: noModel, Mode: modePluginDefault}
	}
	return agentModel{Model: t.Model(h), Mode: modeSession}
}

func agentEffect(t config.Tier, h config.Harness) agentModel {
	switch {
	case t.Model(h) == "":
		return agentModel{Model: noModel, Mode: modePluginDefault}
	case !harness.SupportsPerAgent(h):
		return agentModel{Model: noModel, Mode: modeUnsupported}
	}
	return agentModel{Model: t.Model(h), Mode: modeAssigned}
}

func orDash(s string) string {
	if s == "" {
		return noModel
	}
	return s
}

// printTiers writes the report as two tables.
func printTiers(d Deps, rep tiersReport) {
	w := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprint(w, "TIER")
	for _, h := range rep.Harnesses {
		fmt.Fprintf(w, "\t%s", h)
	}
	fmt.Fprintln(w)
	for _, t := range rep.Tiers {
		fmt.Fprint(w, t.Name)
		for _, h := range rep.Harnesses {
			fmt.Fprintf(w, "\t%s", t.Models[h])
		}
		fmt.Fprintln(w)
	}
	if len(rep.Agents) > 0 {
		fmt.Fprintln(w)
		fmt.Fprint(w, "AGENT\tTIER")
		for _, h := range rep.Harnesses {
			fmt.Fprintf(w, "\t%s", h)
		}
		fmt.Fprintln(w)
		for _, a := range rep.Agents {
			fmt.Fprintf(w, "%s\t%s", a.Agent, a.Tier)
			for _, h := range rep.Harnesses {
				e := a.Effects[h]
				switch e.Mode {
				case modeAssigned:
					fmt.Fprintf(w, "\t%s", e.Model)
				case modeSession:
					fmt.Fprintf(w, "\t%s (%s)", e.Model, e.Mode)
				default:
					fmt.Fprintf(w, "\t%s", e.Mode)
				}
			}
			fmt.Fprintln(w)
		}
	}
	_ = w.Flush()
}
