package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/pluginmgmt"
	"github.com/giocaizzi/agents/internal/term"
)

const (
	subPlugin   = "plugin"
	verbUpdate  = "update"
	verbInstall = "install"
)

// cmdStep is one command to run.
type cmdStep struct {
	name string
	args []string
}

func (s cmdStep) String() string {
	return strings.Join(append([]string{s.name}, s.args...), " ")
}

// pluginChange is one plugin a harness is behind on; from is empty when the
// plugin is missing.
type pluginChange struct{ plugin, from, to string }

func (c pluginChange) String() string {
	to := c.to
	if to == "" {
		to = "latest"
	}
	from := c.from
	if from == "" {
		from = "missing"
	}
	return fmt.Sprintf("%s %s → %s", c.plugin, from, to)
}

// harnessPlan is what an update would do on one harness. A non-empty skip says
// why nothing runs.
type harnessPlan struct {
	harness string
	changes []pluginChange
	steps   []cmdStep
	skip    string
	note    string // shown after the changes when no command is needed
}

// harnessBinaries maps each supported harness to the CLI that updates it.
var harnessBinaries = map[string]string{ //nolint:gochecknoglobals // immutable lookup table
	harnessAntigravity: "agy",
	harnessClaude:      harnessClaude,
	harnessCodex:       harnessCodex,
	harnessCopilot:     harnessCopilot,
	harnessOpencode:    harnessOpencode,
	harnessPi:          harnessPi,
}

// planUpdates decides, per harness, which commands bring the plugins that are
// behind up to date. It runs nothing.
func planUpdates(mgr pluginmgmt.Manager, list []pluginmgmt.PluginInfo, harnesses []string, lookPath func(string) (string, error)) []harnessPlan {
	plans := make([]harnessPlan, 0, len(harnesses))
	for _, h := range harnesses {
		plans = append(plans, planHarness(mgr, h, behindChanges(list, h), lookPath))
	}
	return plans
}

// behindChanges lists the registry plugins that are stale or missing on harness.
func behindChanges(list []pluginmgmt.PluginInfo, harness string) []pluginChange {
	var changes []pluginChange
	for _, p := range list {
		if p.IsStandalone || !p.Behind(harness) {
			continue
		}
		changes = append(changes, pluginChange{plugin: p.Name, from: p.Installed[harness], to: p.UpstreamVersion})
	}
	slices.SortFunc(changes, func(a, b pluginChange) int { return strings.Compare(a.plugin, b.plugin) })
	return changes
}

func planHarness(mgr pluginmgmt.Manager, harness string, changes []pluginChange, lookPath func(string) (string, error)) harnessPlan {
	plan := harnessPlan{harness: harness, changes: changes}
	bin, known := harnessBinaries[harness]
	switch {
	case len(changes) == 0:
		plan.skip = "up to date"
	case !known:
		plan.skip = "unsupported harness"
	default:
		if _, err := lookPath(bin); err != nil {
			plan.skip = fmt.Sprintf("`%s` not found on PATH", bin)
		} else {
			plan.steps, plan.skip = harnessSteps(mgr, harness, bin, changes)
			if harness == harnessPi && plan.skip == "" && len(plan.steps) == 0 {
				plan.note = "by the checkout pull"
			}
		}
	}
	return plan
}

// harnessSteps returns the commands for a supported harness, or why there are none.
func harnessSteps(mgr pluginmgmt.Manager, harness, bin string, changes []pluginChange) ([]cmdStep, string) {
	ns := mgr.EffectiveNamespace()
	installer := installerStep(mgr, harness)
	switch harness {
	case harnessClaude, harnessCopilot:
		steps := make([]cmdStep, 0, 1+len(changes))
		steps = append(steps, cmdStep{bin, []string{subPlugin, "marketplace", verbUpdate, ns}})
		for _, c := range changes {
			verb := verbUpdate
			if c.from == "" {
				verb = verbInstall
			}
			steps = append(steps, cmdStep{bin, []string{subPlugin, verb, c.plugin + "@" + ns}})
		}
		return steps, ""
	case harnessCodex:
		steps := []cmdStep{{bin, []string{subPlugin, "marketplace", "upgrade", ns}}}
		for _, c := range changes {
			steps = append(steps, cmdStep{bin, []string{subPlugin, "add", c.plugin + "@" + ns}})
		}
		return append(steps, installer...), ""
	case harnessAntigravity:
		var steps []cmdStep
		for _, c := range changes {
			dir := filepath.Join(mgr.HarnessDir, "plugins", c.plugin)
			if _, err := os.Stat(dir); err == nil {
				steps = append(steps, cmdStep{bin, []string{subPlugin, verbInstall, dir}})
			}
		}
		steps = append(steps, installer...)
		if len(steps) == 0 {
			return nil, "no plugin directory in the checkout"
		}
		return steps, ""
	case harnessOpencode:
		if len(installer) == 0 {
			return nil, "no installer in the checkout"
		}
		return installer, ""
	default: // harnessPi
		return piSteps(mgr, bin, changes)
	}
}

// piSteps installs the checkout when a plugin is missing. Stale plugins are live
// in the checkout, which the pull already updated, so they need no command.
func piSteps(mgr pluginmgmt.Manager, bin string, changes []pluginChange) ([]cmdStep, string) {
	if mgr.HarnessDir == "" {
		return nil, "no checkout"
	}
	if !slices.ContainsFunc(changes, func(c pluginChange) bool { return c.from == "" }) {
		return nil, ""
	}
	return []cmdStep{{bin, []string{verbInstall, mgr.HarnessDir}}}, ""
}

// installerStep returns the checkout's install script step for harness, or nothing
// when the script does not exist.
func installerStep(mgr pluginmgmt.Manager, harness string) []cmdStep {
	script := filepath.Join(mgr.HarnessDir, "scripts", "install.mjs")
	if mgr.HarnessDir == "" {
		return nil
	}
	if _, err := os.Stat(script); err != nil {
		return nil
	}
	return []cmdStep{{"node", []string{script, harness}}}
}

func (d Deps) runPluginsUpdate(ctx context.Context, args []string) int {
	st := term.NewStyle(d.Stdout, d.Getenv)
	flags, err := d.parsePluginsFlags("plugins update", args)
	if err != nil {
		return d.flagFail(err)
	}

	mgr, err := d.loadHarnessManager(flags.file, flags.harnessDir)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins: %v\n", err)
		return 1
	}

	fmt.Fprintf(d.Stdout, "%sagents plugins update — sync and lifecycle across harnesses%s\n\n", st.B, st.O)

	d.syncUpstream(ctx, st, mgr.HarnessDir, flags.dryRun)

	list, err := mgr.ListPlugins(ctx)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins list: %v\n", err)
		return 1
	}
	if slices.ContainsFunc(list, func(p pluginmgmt.PluginInfo) bool { return p.UpstreamStale }) {
		st.Warn(d.Stdout, "upstream unreachable: cannot tell which plugins are behind; nothing updated")
		return 1
	}

	targets := pluginmgmt.SortHarnesses(pluginmgmt.EnabledHarnesses(mgr.HarnessConfig))
	if flags.harness != "" {
		targets = []string{flags.harness}
	}
	d.printPluginMatrix(st, list, targets)

	plans := planUpdates(mgr, list, targets, exec.LookPath)
	d.reportSkips(st, plans)
	failed := d.applyPlans(ctx, st, plans, flags.dryRun)

	d.pruneCaches(st, mgr, flags.dryRun)

	return d.printUpdateSummary(st, plans, failed, flags.dryRun)
}

// reportSkips prints one line per harness that has nothing to run.
func (d Deps) reportSkips(st term.Style, plans []harnessPlan) {
	for _, p := range plans {
		if p.skip != "" {
			fmt.Fprintf(d.Stdout, "  %s– %s: %s, skipped%s\n", st.D, p.harness, p.skip, st.O)
		}
	}
}

// applyPlans prints or runs every plan that has steps and returns how many failed.
func (d Deps) applyPlans(ctx context.Context, st term.Style, plans []harnessPlan, dryRun bool) int {
	failed := 0
	for _, p := range plans {
		if p.skip != "" {
			continue
		}
		changes := joinChanges(p.changes)
		if p.note != "" {
			changes += " (" + p.note + ")"
		}
		if dryRun {
			fmt.Fprintf(d.Stdout, "  %s[dry-run]%s %s: would update %s\n", st.D, st.O, p.harness, changes)
			for _, s := range p.steps {
				fmt.Fprintf(d.Stdout, "%s      %s%s\n", st.D, s, st.O)
			}
			continue
		}
		if step, out, err := runSteps(ctx, p.steps); err != nil {
			failed++
			fmt.Fprintf(d.Stdout, "  %s✗%s %s: %s failed: %s\n", st.R, st.O, p.harness, step, failureDetail(out, err))
			continue
		}
		fmt.Fprintf(d.Stdout, "  %s✓%s %s: %s\n", st.G, st.O, p.harness, changes)
	}
	return failed
}

func joinChanges(changes []pluginChange) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = c.String()
	}
	return strings.Join(parts, ", ")
}

// runSteps runs steps in order and stops at the first failure, returning that
// step and its combined output.
func runSteps(ctx context.Context, steps []cmdStep) (cmdStep, string, error) {
	for _, s := range steps {
		out, err := exec.CommandContext(ctx, s.name, s.args...).CombinedOutput()
		if err != nil {
			return s, string(out), err
		}
	}
	return cmdStep{}, "", nil
}

func failureDetail(out string, err error) string {
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		return trimmed
	}
	return err.Error()
}

// printUpdateSummary closes the run and returns the exit code.
func (d Deps) printUpdateSummary(st term.Style, plans []harnessPlan, failed int, dryRun bool) int {
	acting, behindSkipped := 0, 0
	anyChange := false
	for _, p := range plans {
		anyChange = anyChange || len(p.changes) > 0
		switch {
		case p.skip == "":
			acting++
		case len(p.changes) > 0:
			behindSkipped++
		}
	}
	switch {
	case !anyChange:
		fmt.Fprintf(d.Stdout, "\n  %s✓%s nothing to update across %d harness(es)\n", st.G, st.O, len(plans))
	case acting == 0:
		fmt.Fprintf(d.Stdout, "\n  nothing updated\n")
	case dryRun:
		fmt.Fprintf(d.Stdout, "\n  would update %d harness(es)\n", acting)
	default:
		fmt.Fprintf(d.Stdout, "\n  %s✓%s updated %d harness(es)\n", st.G, st.O, acting-failed)
	}
	if behindSkipped > 0 {
		st.Warn(d.Stdout, fmt.Sprintf("%d harness(es) behind but skipped (see above)", behindSkipped))
	}
	if failed > 0 {
		st.Warn(d.Stdout, fmt.Sprintf("%d harness(es) failed to update", failed))
		return 1
	}
	return 0
}
