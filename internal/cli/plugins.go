package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/pluginmgmt"
	"github.com/giocaizzi/agents/internal/pluginreg"
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
	subcmd := "list"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcmd = args[0]
		rest = args[1:]
	}

	switch subcmd {
	case "list":
		return d.runPluginsList(ctx, rest)
	case "update":
		return d.runPluginsUpdate(ctx, rest)
	case "prune":
		return d.runPluginsPrune(ctx, rest)
	case helpCmd, helpFlag, "-h":
		st := term.NewStyle(d.Stdout, d.Getenv)
		fmt.Fprintf(d.Stdout, "%sUSAGE%s\n", st.B, st.O)
		fmt.Fprintf(d.Stdout, "  agents plugins [list] [-file <path>] [-harness-dir <path>] [-harness <name>]\n")
		fmt.Fprintf(d.Stdout, "  agents plugins update [-n] [-f] [-file <path>] [-harness-dir <path>] [-harness <name>]\n")
		fmt.Fprintf(d.Stdout, "  agents plugins prune  [-n] [-file <path>]\n")
		return 0
	default:
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%sunknown plugins subcommand: %s%s\n", st.R, subcmd, st.O)
		return 2
	}
}

type pluginsCommonFlags struct {
	file       string
	harnessDir string
	harness    string
	dryRun     bool
	force      bool
	jsonOutput bool
}

func parsePluginsFlags(name string, args []string, getenv func(string) string) (pluginsCommonFlags, error) {
	fs := newFlags(name)
	fileFlag := fs.String("file", "", "path to harness.toml")
	harnessDirFlag := fs.String("harness-dir", getenv("HARNESS_DIR"), "path to plugin registry repository checkout")
	harnessFlag := fs.String("harness", "", "target harness (antigravity, claude, codex, etc.)")
	dryRunFlag := fs.Bool("n", false, "dry-run mode (inspect only, do not modify)")
	dryRunLong := fs.Bool("dry-run", false, "dry-run mode")
	forceFlag := fs.Bool("f", false, "force mode")
	forceLong := fs.Bool("force", false, "force mode")
	jsonFlag := fs.Bool("json", false, "output JSON format")

	if err := fs.Parse(args); err != nil {
		return pluginsCommonFlags{}, fmt.Errorf("parse %s flags: %w", name, err)
	}
	return pluginsCommonFlags{
		file:       *fileFlag,
		harnessDir: *harnessDirFlag,
		harness:    *harnessFlag,
		dryRun:     *dryRunFlag || *dryRunLong,
		force:      *forceFlag || *forceLong,
		jsonOutput: *jsonFlag,
	}, nil
}

func (d Deps) loadHarnessManager(explicitFile, explicitHarnessDir string) (pluginmgmt.Manager, error) {
	tomlPath, err := d.findHarnessToml(explicitFile)
	if err != nil {
		return pluginmgmt.Manager{}, fmt.Errorf("locate harness.toml: %w", err)
	}

	data, err := os.ReadFile(filepath.Clean(tomlPath))
	if err != nil {
		return pluginmgmt.Manager{}, fmt.Errorf("read %s: %w", tomlPath, err)
	}

	var cfg pluginreg.HarnessConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return pluginmgmt.Manager{}, fmt.Errorf("parse %s: %w", tomlPath, err)
	}

	pluginreg.MergeAgentsConfig(&cfg, data)

	harnessDir := explicitHarnessDir
	if harnessDir == "" {
		candidates := pluginreg.CandidateDirs(d.Getenv("HARNESS_DIR"), cfg.Registry, d.homeDir())
		for _, c := range candidates {
			if c != "" {
				if _, statErr := os.Stat(filepath.Join(c, "plugins")); statErr == nil {
					harnessDir = c
					break
				}
			}
		}
	}

	ns := ""
	if cfg.Registry != "" {
		ns = pluginreg.ResolveNamespace(cfg.Registry)
	} else if harnessDir != "" {
		ns = pluginreg.DiscoverMarketplaceNamespace(harnessDir)
		if cfg.Registry == "" {
			cfg.Registry = pluginreg.DiscoverRegistry(harnessDir)
		}
	}
	if ns == "" {
		ns = "harness"
	}

	return pluginmgmt.Manager{
		HarnessDir:    harnessDir,
		HomeDir:       d.homeDir(),
		Registry:      cfg.Registry,
		Namespace:     ns,
		HarnessConfig: cfg,
	}, nil
}

func (d Deps) homeDir() string {
	if h := d.Getenv("HOME"); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err == nil {
		return h
	}
	return ""
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
	row := []string{nameStr, st.D + up + st.O}
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
	flags, err := parsePluginsFlags("plugins list", args, d.Getenv)
	if err != nil {
		fmt.Fprintf(d.Stderr, "flags: %v\n", err)
		return 2
	}

	mgr, err := d.loadHarnessManager(flags.file, flags.harnessDir)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins: %v\n", err)
		return 1
	}

	list, err := mgr.ListPlugins(ctx)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins list: %v\n", err)
		return 1
	}

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

	harnesses := pluginmgmt.SortHarnesses(pluginmgmt.EnabledHarnesses(mgr.HarnessConfig))
	if flags.harness != "" {
		harnesses = []string{flags.harness}
	}

	headers := []string{"PLUGIN", "UPSTREAM"}
	for _, h := range harnesses {
		headers = append(headers, strings.ToUpper(h))
	}

	var rows [][]string
	staleCount := 0
	missingCount := 0
	hasStandalone := false

	for _, p := range list {
		if p.IsStandalone {
			hasStandalone = true
		}
		row, isStale, isMissing := buildPluginRow(p, harnesses, st)
		if isStale {
			staleCount++
		}
		if isMissing {
			missingCount++
		}
		rows = append(rows, row)
	}

	st.Table(d.Stdout, headers, rows)
	fmt.Fprintln(d.Stdout)
	if hasStandalone {
		fmt.Fprintf(d.Stdout, "  %s* standalone external plugin%s\n\n", st.D, st.O)
	}

	if staleCount > 0 || missingCount > 0 {
		st.Warn(d.Stdout, fmt.Sprintf("%d plugin(s) stale or missing: run `agents plugins update`", staleCount+missingCount))
	} else {
		fmt.Fprintf(d.Stdout, "  %s✓%s all %d plugins up to date across %d configured harnesses\n", st.G, st.O, len(list), len(harnesses))
	}

	return 0
}

func (d Deps) runPluginsUpdate(ctx context.Context, args []string) int {
	st := term.NewStyle(d.Stdout, d.Getenv)
	flags, err := parsePluginsFlags("plugins update", args, d.Getenv)
	if err != nil {
		fmt.Fprintf(d.Stderr, "flags: %v\n", err)
		return 2
	}

	mgr, err := d.loadHarnessManager(flags.file, flags.harnessDir)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins: %v\n", err)
		return 1
	}

	fmt.Fprintf(d.Stdout, "%sagents plugins update — sync and lifecycle across harnesses%s\n\n", st.B, st.O)

	d.syncUpstream(ctx, st, mgr.HarnessDir, flags.dryRun)

	targetHarnesses := pluginmgmt.SortHarnesses(pluginmgmt.EnabledHarnesses(mgr.HarnessConfig))
	if flags.harness != "" {
		targetHarnesses = []string{flags.harness}
	}
	d.updateHarnesses(ctx, st, mgr, targetHarnesses, flags.dryRun)

	d.pruneCaches(st, mgr, flags.dryRun)

	fmt.Fprintf(d.Stdout, "\n  %s✓%s plugin update complete\n", st.G, st.O)
	return 0
}

func (d Deps) syncUpstream(ctx context.Context, st term.Style, harnessDir string, dryRun bool) {
	if harnessDir == "" {
		return
	}
	gitDir := filepath.Join(harnessDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return
	}
	if dryRun {
		fmt.Fprintf(d.Stdout, "  %s[dry-run]%s would git pull upstream checkout at %s\n", st.D, st.O, harnessDir)
		return
	}
	cmd := exec.CommandContext(ctx, "git", "-C", harnessDir, "pull", "--ff-only")
	if out, err := cmd.CombinedOutput(); err != nil {
		st.Warn(d.Stdout, fmt.Sprintf("upstream git pull failed (%s): %v", strings.TrimSpace(string(out)), err))
	} else {
		fmt.Fprintf(d.Stdout, "  %s✓%s upstream harness checkout updated\n", st.G, st.O)
	}
}

func (d Deps) updateHarnesses(ctx context.Context, st term.Style, mgr pluginmgmt.Manager, harnesses []string, dryRun bool) {
	for _, h := range harnesses {
		switch h {
		case harnessAntigravity:
			d.updateAntigravity(ctx, st, mgr, dryRun)
		case harnessCodex:
			d.updateCodex(ctx, st, mgr, dryRun)
		case harnessClaude:
			d.updateClaude(ctx, st, mgr, dryRun)
		case harnessCopilot:
			d.updateCopilot(ctx, st, mgr, dryRun)
		case harnessOpencode:
			d.updateOpencode(ctx, st, mgr, dryRun)
		case harnessPi:
			d.updatePi(ctx, st, mgr, dryRun)
		}
	}
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

func (d Deps) updateAntigravity(ctx context.Context, st term.Style, mgr pluginmgmt.Manager, dryRun bool) {
	if _, err := exec.LookPath("agy"); err != nil {
		return
	}
	pluginsDir := filepath.Join(mgr.HarnessDir, "plugins")
	var installed []string

	for p, hs := range mgr.HarnessConfig.Plugins {
		if !slices.Contains(hs, harnessAntigravity) {
			continue
		}
		pPath := filepath.Join(pluginsDir, p)
		if _, err := os.Stat(pPath); err != nil {
			continue
		}
		if dryRun {
			fmt.Fprintf(d.Stdout, "  %s[dry-run]%s agy plugin install %s\n", st.D, st.O, pPath)
		} else {
			cmd := exec.CommandContext(ctx, "agy", "plugin", "install", pPath)
			if err := cmd.Run(); err == nil {
				installed = append(installed, p)
			}
		}
	}

	installScript := filepath.Join(mgr.HarnessDir, "scripts", "install.mjs")
	if _, err := os.Stat(installScript); err == nil && !dryRun {
		_ = exec.CommandContext(ctx, "node", installScript, harnessAntigravity).Run()
	}

	if dryRun {
		fmt.Fprintf(d.Stdout, "  %s✓%s antigravity: would update plugins and sync agents/hooks\n", st.G, st.O)
	} else {
		fmt.Fprintf(d.Stdout, "  %s✓%s antigravity: %d plugin(s) installed/updated\n", st.G, st.O, len(installed))
	}
}

func (d Deps) updateCodex(ctx context.Context, st term.Style, mgr pluginmgmt.Manager, dryRun bool) {
	if _, err := exec.LookPath("codex"); err != nil {
		return
	}
	ns := mgr.EffectiveNamespace()
	if dryRun {
		fmt.Fprintf(d.Stdout, "  %s[dry-run]%s codex plugin marketplace upgrade %s\n", st.D, st.O, ns)
		return
	}
	_ = exec.CommandContext(ctx, "codex", "plugin", "marketplace", "upgrade", ns).Run()
	count := 0
	for p, hs := range mgr.HarnessConfig.Plugins {
		if slices.Contains(hs, harnessCodex) {
			if err := exec.CommandContext(ctx, "codex", "plugin", "add", p+"@"+ns).Run(); err == nil {
				count++
			}
		}
	}
	installScript := filepath.Join(mgr.HarnessDir, "scripts", "install.mjs")
	if _, err := os.Stat(installScript); err == nil {
		_ = exec.CommandContext(ctx, "node", installScript, harnessCodex).Run()
	}
	fmt.Fprintf(d.Stdout, "  %s✓%s codex: updated marketplace and %d plugin(s)\n", st.G, st.O, count)
}

func (d Deps) updateMarketplaceHarness(ctx context.Context, st term.Style, mgr pluginmgmt.Manager, harnessName, command string, dryRun bool) {
	if _, err := exec.LookPath(command); err != nil {
		return
	}
	ns := mgr.EffectiveNamespace()
	if dryRun {
		fmt.Fprintf(d.Stdout, "  %s[dry-run]%s %s plugin marketplace update %s\n", st.D, st.O, command, ns)
		return
	}
	_ = exec.CommandContext(ctx, command, "plugin", "marketplace", "update", ns).Run()
	count := 0
	for p, hs := range mgr.HarnessConfig.Plugins {
		if slices.Contains(hs, harnessName) {
			if err := exec.CommandContext(ctx, command, "plugin", "update", p+"@"+ns).Run(); err == nil {
				count++
			}
		}
	}
	fmt.Fprintf(d.Stdout, "  %s✓%s %s: updated marketplace and %d plugin(s)\n", st.G, st.O, harnessName, count)
}

func (d Deps) updateClaude(ctx context.Context, st term.Style, mgr pluginmgmt.Manager, dryRun bool) {
	d.updateMarketplaceHarness(ctx, st, mgr, harnessClaude, "claude", dryRun)
}

func (d Deps) updateCopilot(ctx context.Context, st term.Style, mgr pluginmgmt.Manager, dryRun bool) {
	d.updateMarketplaceHarness(ctx, st, mgr, harnessCopilot, "copilot", dryRun)
}

func (d Deps) updateOpencode(ctx context.Context, st term.Style, mgr pluginmgmt.Manager, dryRun bool) {
	if _, err := exec.LookPath("opencode"); err != nil {
		return
	}
	installScript := filepath.Join(mgr.HarnessDir, "scripts", "install.mjs")
	if _, err := os.Stat(installScript); err == nil {
		if dryRun {
			fmt.Fprintf(d.Stdout, "  %s[dry-run]%s node %s opencode\n", st.D, st.O, installScript)
		} else {
			_ = exec.CommandContext(ctx, "node", installScript, harnessOpencode).Run()
			fmt.Fprintf(d.Stdout, "  %s✓%s opencode: synchronized skills and agents from checkout\n", st.G, st.O)
		}
	}
}

func (d Deps) updatePi(ctx context.Context, st term.Style, mgr pluginmgmt.Manager, dryRun bool) {
	if _, err := exec.LookPath("pi"); err != nil {
		return
	}
	if mgr.HarnessDir != "" {
		if dryRun {
			fmt.Fprintf(d.Stdout, "  %s[dry-run]%s pi install %s\n", st.D, st.O, mgr.HarnessDir)
		} else {
			_ = exec.CommandContext(ctx, "pi", "install", mgr.HarnessDir).Run()
			fmt.Fprintf(d.Stdout, "  %s✓%s pi: package link up to date\n", st.G, st.O)
		}
	}
}

func (d Deps) runPluginsPrune(_ context.Context, args []string) int {
	st := term.NewStyle(d.Stdout, d.Getenv)
	flags, err := parsePluginsFlags("plugins prune", args, d.Getenv)
	if err != nil {
		fmt.Fprintf(d.Stderr, "flags: %v\n", err)
		return 2
	}

	mgr, err := d.loadHarnessManager(flags.file, flags.harnessDir)
	if err != nil {
		fmt.Fprintf(d.Stderr, "plugins: %v\n", err)
		return 1
	}

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
