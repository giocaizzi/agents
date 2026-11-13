package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
				plan.note = piNote(changes)
			}
		}
	}
	return plan
}

// piNote says how pi gets the changes: the checkout pull updates linked
// plugins, and apply links the checkout when a plugin is missing.
func piNote(changes []pluginChange) string {
	if slices.ContainsFunc(changes, func(c pluginChange) bool { return c.from == "" }) {
		return "linked by agents apply"
	}
	return "by the checkout pull"
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
		return piSteps(mgr)
	}
}

// piSteps has no command: pi loads the registry checkout as a package, which
// apply links into the pi settings, and the checkout pull updates it in place.
func piSteps(mgr pluginmgmt.Manager) ([]cmdStep, string) {
	if mgr.HarnessDir == "" {
		return nil, "no checkout"
	}
	return nil, ""
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

// runner runs one command and returns its combined output.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner runs commands for real, with git prompts disabled.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("run %s: %w", name, err)
	}
	return out, nil
}

// pullInterval is how long a successful pull of the registry checkout lasts.
const pullInterval = 24 * time.Hour

func (d Deps) runPluginsUpdate(ctx context.Context, args []string) int {
	return d.pluginsUpdate(ctx, args, execRunner, time.Now)
}

// pluginsUpdate is runPluginsUpdate with the command runner and the clock injected.
func (d Deps) pluginsUpdate(ctx context.Context, args []string, run runner, now func() time.Time) int {
	st := term.NewStyle(d.Stdout, d.Getenv)
	flags, err := d.parsePluginsFlags("plugins update", args)
	if err != nil {
		return d.flagFail(err)
	}

	setup, err := d.loadPluginSetup(flags)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins: %v\n", err)
		return 1
	}
	mgr := setup.mgr
	if mgr.HarnessDir == "" {
		fmt.Fprintln(d.Stderr, "plugins: no plugin registry configured; set [plugins] registry or pass -harness-dir")
		return 1
	}

	fmt.Fprintf(d.Stdout, "%sagents plugins update — sync and lifecycle across harnesses%s\n\n", st.B, st.O)

	stamp := ""
	if setup.registry != "" {
		stamp = filepath.Join(setup.resolved.Dirs.State, "registries", filepath.Base(setup.registry)+".pulled")
	}
	sync := checkoutSync{run: run, now: now, registry: setup.registry, dir: mgr.HarnessDir, stamp: stamp, dryRun: flags.dryRun, force: flags.force}
	if err := d.syncCheckout(ctx, st, sync); err != nil {
		fmt.Fprintf(d.Stderr, "plugins update: %v\n", err)
		return 1
	}

	list := mgr.ListPlugins(ctx)
	if slices.ContainsFunc(list, func(p pluginmgmt.PluginInfo) bool { return p.UpstreamStale }) {
		st.Warn(d.Stdout, "upstream unreachable: cannot tell which plugins are behind; nothing updated")
		return 1
	}

	targets := setup.targets
	d.printPluginMatrix(st, list, targets)

	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	plans := planUpdates(mgr, list, targets, lookPath)
	d.reportSkips(st, plans)
	failed := d.applyPlans(ctx, st, plans, flags.dryRun, run)

	d.pruneCaches(st, mgr, flags.dryRun)

	return d.printUpdateSummary(st, plans, failed, flags.dryRun)
}

// checkoutSync describes how to bring the registry checkout up to date.
type checkoutSync struct {
	run      runner
	now      func() time.Time
	registry string // owner/repo; "" when only a -harness-dir is known
	dir      string
	stamp    string // file whose mtime is the last successful pull; "" without a registry
	dryRun   bool
	force    bool
}

// syncCheckout clones the checkout when it is missing and otherwise pulls it
// when the last successful pull is older than pullInterval (or on force). A
// directory that is not a git checkout is left alone. A failed pull is a
// warning; a failed clone is an error.
func (d Deps) syncCheckout(ctx context.Context, st term.Style, sc checkoutSync) error {
	if _, err := os.Stat(sc.dir); errors.Is(err, fs.ErrNotExist) {
		if sc.registry == "" {
			return nil
		}
		step := cmdStep{"git", []string{"clone", "https://github.com/" + sc.registry, sc.dir}}
		if sc.dryRun {
			fmt.Fprintf(d.Stdout, "  %s[dry-run]%s would run %s\n", st.D, st.O, step)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(sc.dir), 0o750); err != nil {
			return fmt.Errorf("create checkout parent: %w", err)
		}
		if out, err := sc.run(ctx, step.name, step.args...); err != nil {
			return fmt.Errorf("%s failed: %s", step, failureDetail(string(out), err))
		}
		fmt.Fprintf(d.Stdout, "  %s✓%s cloned the registry checkout to %s\n", st.G, st.O, sc.dir)
		return sc.touchStamp()
	}
	if _, err := os.Stat(filepath.Join(sc.dir, ".git")); err != nil {
		return nil //nolint:nilerr // a plain directory is not ours to pull
	}
	if !sc.force && sc.stamp != "" {
		if info, err := os.Stat(sc.stamp); err == nil && sc.now().Sub(info.ModTime()) < pullInterval {
			fmt.Fprintf(d.Stdout, "  %s– registry checkout pulled less than 24h ago, skipped (-force pulls)%s\n", st.D, st.O)
			return nil
		}
	}
	step := cmdStep{"git", []string{"-C", sc.dir, "pull", "--ff-only"}}
	if sc.dryRun {
		fmt.Fprintf(d.Stdout, "  %s[dry-run]%s would run %s\n", st.D, st.O, step)
		return nil
	}
	if out, err := sc.run(ctx, step.name, step.args...); err != nil {
		st.Warn(d.Stdout, fmt.Sprintf("registry checkout pull failed (%s)", failureDetail(string(out), err)))
		return nil
	}
	fmt.Fprintf(d.Stdout, "  %s✓%s registry checkout updated\n", st.G, st.O)
	return sc.touchStamp()
}

// touchStamp records a successful pull now.
func (sc checkoutSync) touchStamp() error {
	if sc.stamp == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(sc.stamp), 0o750); err != nil {
		return fmt.Errorf("create stamp directory: %w", err)
	}
	if err := os.WriteFile(sc.stamp, nil, 0o600); err != nil {
		return fmt.Errorf("write pull stamp: %w", err)
	}
	t := sc.now()
	if err := os.Chtimes(sc.stamp, t, t); err != nil {
		return fmt.Errorf("touch pull stamp: %w", err)
	}
	return nil
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
func (d Deps) applyPlans(ctx context.Context, st term.Style, plans []harnessPlan, dryRun bool, run runner) int {
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
		if step, out, err := runSteps(ctx, run, p.steps); err != nil {
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
func runSteps(ctx context.Context, run runner, steps []cmdStep) (cmdStep, string, error) {
	for _, s := range steps {
		out, err := run(ctx, s.name, s.args...)
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
