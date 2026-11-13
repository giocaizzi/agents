package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/harness"
	"github.com/giocaizzi/agents/internal/pluginmgmt"
	"github.com/giocaizzi/agents/internal/term"
)

const (
	harnessAntigravity = "antigravity"
	harnessClaude      = "claude"
	harnessCodex       = "codex"
	harnessCopilot     = "copilot"
	harnessOpencode    = "opencode"
	harnessPi          = "pi"
	helpFlag           = "--help"
)

func (d Deps) runPlugins(ctx context.Context, args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == helpFlag || args[0] == helpCmd) {
		return d.explicitHelp(ctx, append([]string{"plugins"}, args[1:]...))
	}
	subcmd := "list"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcmd = args[0]
		rest = args[1:]
	}

	switch subcmd {
	case "list":
		return d.runPluginsList(ctx, rest)
	case verbUpdate:
		return d.runPluginsUpdate(ctx, rest)
	case "prune":
		return d.runPluginsPrune(ctx, rest)
	default:
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%sunknown plugins subcommand: %s%s\n", st.R, subcmd, st.O)
		d.printHelp(d.Stderr, "plugins", nil)
		return 2
	}
}

type pluginsCommonFlags struct {
	*configFlags

	harnessDir string
	harness    string
	dryRun     bool
	force      bool
	jsonOutput bool
}

func (d Deps) parsePluginsFlags(name string, args []string) (pluginsCommonFlags, error) {
	fs := d.newFlags(name)
	cf := addConfigFlags(fs)
	harnessDirFlag := fs.String("harness-dir", "", "registry checkout to use instead of the one under the data directory")
	harnessFlag := fs.String("harness", "", "target harness (antigravity, claude, codex, etc.)")
	dryRunFlag := fs.Bool("n", false, "dry-run mode (inspect only, do not modify)")
	dryRunLong := fs.Bool("dry-run", false, "dry-run mode")
	forceFlag := fs.Bool("f", false, "force mode")
	forceLong := fs.Bool("force", false, "force mode")
	jsonFlag := fs.Bool("json", false, "output JSON format")

	if err := fs.Parse(args); err != nil {
		d.parseFailure(fs, err)
		return pluginsCommonFlags{}, fmt.Errorf("parse %s flags: %w", name, err)
	}
	return pluginsCommonFlags{
		configFlags: cf,
		harnessDir:  *harnessDirFlag,
		harness:     *harnessFlag,
		dryRun:      *dryRunFlag || *dryRunLong,
		force:       *forceFlag || *forceLong,
		jsonOutput:  *jsonFlag,
	}, nil
}

// pluginSetup is the plugin manager with what it was built from.
type pluginSetup struct {
	mgr      pluginmgmt.Manager
	resolved *config.Resolved
	registry string // owner/repo, "" without a registry
	targets  []string
}

// loadPluginSetup builds the plugin manager from the resolved configuration.
// The checkout is the registry directory under the data directory unless
// -harness-dir names another.
func (d Deps) loadPluginSetup(flags pluginsCommonFlags) (pluginSetup, error) {
	r, err := d.loadConfig(flags.configFlags)
	if err != nil {
		return pluginSetup{}, err
	}
	c := &r.Config
	checkout := flags.harnessDir
	if checkout == "" {
		checkout = harness.RegistryDir(c, r.Dirs)
	}

	plugins := make(map[string][]string, len(c.Plugins.Enabled))
	for name := range c.Plugins.Enabled {
		plugins[name] = nil
	}
	for _, h := range r.Harnesses {
		for name, on := range harness.PluginStates(c, h) {
			if on {
				plugins[name] = append(plugins[name], string(h))
			}
		}
	}
	standalone := make(map[string]pluginmgmt.Standalone, len(c.Plugins.Standalone)+len(c.Plugins.External))
	for _, table := range []map[string]config.StandalonePlugin{c.Plugins.Standalone, c.Plugins.External} {
		for name, sp := range table {
			standalone[name] = pluginmgmt.Standalone{Source: sp.Source, Path: sp.Path, Harnesses: sp.Harnesses, Version: sp.Version}
		}
	}

	targets := make([]string, 0, len(r.Harnesses))
	for _, h := range r.Harnesses {
		if slices.Contains(pluginmgmt.DefaultHarnessOrder(), string(h)) {
			targets = append(targets, string(h))
		}
	}
	targets = pluginmgmt.SortHarnesses(targets)
	if flags.harness != "" {
		targets = []string{flags.harness}
	}

	return pluginSetup{
		mgr: pluginmgmt.Manager{
			HarnessDir: checkout,
			HomeDir:    d.home(),
			ConfigHome: d.Getenv("XDG_CONFIG_HOME"),
			Namespace:  harness.MarketplaceName(c),
			Plugins:    plugins,
			Standalone: standalone,
		},
		resolved: r,
		registry: c.Plugins.Registry,
		targets:  targets,
	}, nil
}

func buildPluginRow(p pluginmgmt.PluginInfo, harnesses []string, st term.Style) ([]string, bool, bool) {
	up := p.UpstreamVersion
	if up == "" {
		up = "—"
	}
	nameStr := st.M + p.Name + st.O
	if p.IsStandalone {
		nameStr += st.D + " *" + st.O
	}
	upCell := st.D + up
	if p.UpstreamStale {
		upCell += " ?"
	}
	row := []string{nameStr, upCell + st.O}
	isStale := false
	isMissing := false
	for _, h := range harnesses {
		state := p.State(h)
		v := p.Installed[h]
		switch state {
		case pluginmgmt.StateOK:
			row = append(row, st.G+"✓ "+v+st.O)
		case pluginmgmt.StateStale:
			isStale = true
			row = append(row, st.Y+"↑ "+v+st.O)
		case pluginmgmt.StateMissing:
			isMissing = true
			row = append(row, st.R+"✗ missing"+st.O)
		case pluginmgmt.StateDisabled:
			row = append(row, st.D+"—"+st.O)
		}
	}
	return row, isStale, isMissing
}

func (d Deps) runPluginsList(ctx context.Context, args []string) int {
	st := term.NewStyle(d.Stdout, d.Getenv)
	flags, err := d.parsePluginsFlags("plugins list", args)
	if err != nil {
		return d.flagFail(err)
	}

	setup, err := d.loadPluginSetup(flags)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins: %v\n", err)
		return 1
	}
	mgr := setup.mgr

	list := mgr.ListPlugins(ctx)

	if flags.jsonOutput {
		enc := json.NewEncoder(d.Stdout)
		enc.SetIndent("", "  ")
		if encodeErr := enc.Encode(list); encodeErr != nil {
			fmt.Fprintf(d.Stderr, "json encode: %v\n", encodeErr)
			return 1
		}
		return 0
	}

	fmt.Fprintf(d.Stdout, "%sagents plugins — harness plugin status and lifecycle%s\n\n", st.B, st.O)

	harnesses := setup.targets

	upstreamStale := slices.ContainsFunc(list, func(p pluginmgmt.PluginInfo) bool { return p.UpstreamStale })
	staleCount, missingCount := d.printPluginMatrix(st, list, harnesses)

	if staleCount > 0 || missingCount > 0 {
		st.Warn(d.Stdout, fmt.Sprintf("%d plugin(s) stale or missing: run `agents plugins update`", staleCount+missingCount))
	}
	if upstreamStale {
		st.Warn(d.Stdout, fmt.Sprintf("upstream unreachable: versions read from the local checkout at %s may be out of date", mgr.HarnessDir))
	}
	if staleCount == 0 && missingCount == 0 {
		d.printUpToDate(st, len(list), len(harnesses), upstreamStale)
	}

	return 0
}

// printPluginMatrix renders the plugin by harness table and returns how many
// plugins are stale and how many are missing on at least one harness.
func (d Deps) printPluginMatrix(st term.Style, list []pluginmgmt.PluginInfo, harnesses []string) (stale, missing int) {
	headers := make([]string, 0, 2+len(harnesses))
	headers = append(headers, "PLUGIN", "UPSTREAM")
	for _, h := range harnesses {
		headers = append(headers, strings.ToUpper(h))
	}

	rows := make([][]string, 0, len(list))
	for _, p := range list {
		row, isStale, isMissing := buildPluginRow(p, harnesses, st)
		if isStale {
			stale++
		}
		if isMissing {
			missing++
		}
		rows = append(rows, row)
	}

	st.Table(d.Stdout, headers, rows)
	fmt.Fprintln(d.Stdout)
	if slices.ContainsFunc(list, func(p pluginmgmt.PluginInfo) bool { return p.IsStandalone }) {
		fmt.Fprintf(d.Stdout, "  %s* standalone external plugin%s\n\n", st.D, st.O)
	}
	return stale, missing
}

// printUpToDate reports that nothing is stale or missing. When upstream was
// unreachable it cannot claim the plugins are current, so the line is neutral.
func (d Deps) printUpToDate(st term.Style, plugins, harnesses int, upstreamStale bool) {
	if upstreamStale {
		fmt.Fprintf(d.Stdout, "  %sno newer version found in the local checkout%s\n", st.D, st.O)
		return
	}
	fmt.Fprintf(d.Stdout, "  %s✓%s all %d plugins up to date across %d configured harnesses\n", st.G, st.O, plugins, harnesses)
}

func (d Deps) pruneCaches(st term.Style, mgr pluginmgmt.Manager, dryRun bool) {
	candidates, err := mgr.FindPruneCandidates()
	if err != nil || len(candidates) == 0 {
		return
	}
	if dryRun {
		fmt.Fprintf(d.Stdout, "  %s[dry-run]%s would prune %d obsolete plugin cache directories\n", st.D, st.O, len(candidates))
		return
	}
	pruned, pErr := mgr.Prune(candidates)
	if pErr == nil {
		fmt.Fprintf(d.Stdout, "  %s✓%s pruned %d obsolete plugin cache(s)\n", st.G, st.O, len(pruned))
	}
}

func (d Deps) runPluginsPrune(_ context.Context, args []string) int {
	st := term.NewStyle(d.Stdout, d.Getenv)
	flags, err := d.parsePluginsFlags("plugins prune", args)
	if err != nil {
		return d.flagFail(err)
	}

	setup, err := d.loadPluginSetup(flags)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins: %v\n", err)
		return 1
	}
	mgr := setup.mgr

	candidates, err := mgr.FindPruneCandidates()
	if err != nil {
		fmt.Fprintf(d.Stderr, "prune search: %v\n", err)
		return 1
	}

	fmt.Fprintf(d.Stdout, "%sagents plugins prune — clean obsolete plugin caches%s\n\n", st.B, st.O)

	if len(candidates) == 0 {
		fmt.Fprintf(d.Stdout, "  %s✓%s nothing to prune\n", st.G, st.O)
		return 0
	}

	if flags.dryRun {
		for _, c := range candidates {
			fmt.Fprintf(d.Stdout, "  %s[dry-run]%s would remove %s (%s, %s)\n", st.D, st.O, c.Path, c.Harness, c.Reason)
		}
		fmt.Fprintf(d.Stdout, "\n  %s!%s found %d prune candidate(s) (dry-run, no changes made)\n", st.Y, st.O, len(candidates))
		return 0
	}

	pruned, err := mgr.Prune(candidates)
	if err != nil {
		fmt.Fprintf(d.Stderr, "prune error: %v\n", err)
		return 1
	}

	for _, p := range pruned {
		fmt.Fprintf(d.Stdout, "  %s✓%s removed %s\n", st.G, st.O, p)
	}
	fmt.Fprintf(d.Stdout, "\n  %s✓%s pruned %d obsolete plugin cache(s)\n", st.G, st.O, len(pruned))
	return 0
}
