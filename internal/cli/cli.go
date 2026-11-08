// Package cli implements the chezmoi-agents command line interface.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/term"
)

// Deps holds the injected dependencies for the CLI.
type Deps struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	GOOS    string
	Version string
}

// Run dispatches subcommands with standard OS dependencies.
func Run(ctx context.Context, args []string, version string) int {
	d := Deps{
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Getenv:  os.Getenv,
		GOOS:    runtime.GOOS,
		Version: version,
	}
	return d.Run(ctx, args)
}

// Run executes the command line application and returns an exit code.
func (d Deps) Run(ctx context.Context, args []string) int {
	if d.GOOS == "" {
		d.GOOS = runtime.GOOS
	}
	if d.Getenv == nil {
		d.Getenv = os.Getenv
	}
	if d.Stdin == nil {
		d.Stdin = os.Stdin
	}
	if d.Stdout == nil {
		d.Stdout = os.Stdout
	}
	if d.Stderr == nil {
		d.Stderr = os.Stderr
	}

	if len(args) == 0 {
		st := term.NewStyle(d.Stdout, d.Getenv)
		fmt.Fprint(d.Stdout, usage(st))
		return 0
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "check":
		return d.runCheck(ctx, rest)
	case "render":
		return d.runRender(ctx, rest)
	case "prompt":
		return d.runPrompt(ctx, rest)
	case "mcp":
		return d.runMCP(ctx, rest)
	case "models":
		return d.runModels(ctx, rest)
	case "usage":
		return d.runUsage(ctx, rest)
	case "doctor":
		return d.runDoctor(ctx, rest)
	case "plugins":
		return d.runPlugins(ctx, rest)
	case "statusline":
		return d.runStatusline(ctx, rest)
	case "init":
		return d.runInit(ctx, rest)
	case "version", "--version", "-v":
		v := d.Version
		if v == "" {
			v = "dev"
		}
		fmt.Fprintf(d.Stdout, "agents %s\n", v)
		return 0
	case helpCmd, helpFlag, "-h":
		st := term.NewStyle(d.Stdout, d.Getenv)
		fmt.Fprint(d.Stdout, usage(st))
		return 0
	default:
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%sunknown command: %s%s\n\n", st.R, cmd, st.O)
		fmt.Fprint(d.Stderr, usage(st))
		return 2
	}
}

func usage(s term.Style) string {
	b := s.B // blue
	m := s.M // mauve
	y := s.Y // yellow
	d := s.D // dim overlay0
	o := s.O // reset

	var sb strings.Builder
	p := func(format string, a ...any) {
		fmt.Fprintf(&sb, format, a...)
	}

	p("%sagents%s %s— Universal AI harness configuration, policy validation, parity and prompt engine%s\n\n", b, o, d, o)

	p("%sUSAGE%s\n", b, o)
	p("  %sagents%s %sinit%s %s[-global]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %sstatusline%s %s[claude|copilot|tmux|omp]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %scheck%s %s[-file <path>] [-harness-dir <path>]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %srender%s %s[-file <path>]%s %s-harness <name>%s %s[-target <file>]%s\n", m, o, m, o, d, o, y, o, d, o)
	p("  %sagents%s %sprompt%s %s[-profile <profile>]%s %s[-check]%s %s[-file <path>]%s\n", m, o, m, o, d, o, d, o, d, o)
	p("  %sagents%s %smcp sync%s %s[path] [-check] [-account <id>]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %smcp add%s %s[flags] <name> <commandOrUrl> [args...]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %smodels%s %s[-file <path>]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %smodels sync%s %s[-file <path>] [-doc <path>]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %smodels render%s %s[-check] [-file <path>] [-doc <path>]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %susage%s %s[-live] [-provider <id>]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %sdoctor%s %s[-live] [-q] [-json] [-harness <list>]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %splugins%s %s[list│update│prune] [-n] [-f] [-file <path>] [-harness-dir <path>]%s\n", m, o, m, o, d, o)
	p("  %sagents%s %sversion%s %s│%s %shelp%s\n\n", m, o, m, o, d, o, m, o)

	p("%sCOMMANDS%s\n", b, o)
	p("  %s%-13s%s %s%-30s%s %s\n", m, "init", o, d, "[-global]", o, "Detect harnesses and scaffold agents.toml")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "statusline", o, d, "[claude|copilot|tmux|omp]", o, "Output fast (<5ms) Catppuccin status line")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "check", o, d, "[-file <path>]", o, "Validate harness policy, 6-way client parity, and plugins")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "render", o, y, "-harness <name>", o, "Render client-native configuration (JSON/TOML)")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "prompt", o, d, "[-profile <profile>]", o, "Compose or validate layered system prompt")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "mcp sync", o, d, "[path]", o, "Sync workspace .mcp.json to native Codex, pi and Antigravity configs")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "mcp add", o, d, "<name> <cmd|url> [args]", o, "Add or update server in workspace .mcp.json and sync configs")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "models", o, d, "[-file <path>]", o, "Print the model tier list with costs and arena ranks")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "models sync", o, d, "[-file <path>]", o, "Refresh model data from models.dev, LMArena and Epoch AI; recompute the tiers")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "models render", o, d, "[-check]", o, "Regenerate docs/MODELS.md offline")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "usage", o, d, "[-live] [-provider <id>]", o, "Show subscription usage per provider account (cached; -live refreshes)")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "doctor", o, d, "[-live] [-q] [-harness <list>]", o, "Advise on configured models, accounts and capacity (cached; -live refreshes)")
	p("  %s%-13s%s %s%-30s%s %s\n", m, "plugins", o, d, "[list|update|prune]", o, "Inspect, update, or prune plugins across harnesses")
	p("  %s%-13s%s %-30s %s\n", m, "version", o, "", "Print agents version information")
	p("  %s%-13s%s %-30s %s\n\n", m, "help", o, "", "Display this help message")

	p("%sFLAGS%s\n", b, o)
	p("  %s-file%s %s<path>%s         Path to harness-policy.toml, harness.toml, models.json, or prompt layer\n", y, o, d, o)
	p("  %s-harness%s %s<name>%s      Target harness (render) or comma-separated harnesses (doctor)\n", y, o, d, o)
	p("  %s-harness-dir%s %s<path>%s  Checkout of giocaizzi/harness (defaults to HARNESS_DIR)\n", y, o, d, o)
	p("  %s-profile%s %s<profile>%s   System prompt profile (personal|work|light)\n", y, o, d, o)
	p("  %s-check%s               Validate layer presence or models doc freshness without printing output\n", y, o)
	p("  %s-doc%s %s<path>%s          Path to docs/MODELS.md\n", y, o, d, o)
	p("  %s-target%s %s<file>%s       Target file to write rendered configuration\n", y, o, d, o)
	p("  %s-live%s                Query every provider and rewrite the usage cache (usage, doctor)\n", y, o)
	p("  %s-provider%s %s<id>%s       Limit usage output to one provider id\n", y, o, d, o)
	p("  %s-q%s                   Print nothing when there is nothing to report (doctor)\n", y, o)
	p("  %s-json%s                Print the doctor findings as JSON\n\n", y, o)

	p("%sEXAMPLES%s\n", b, o)
	p("  %schezmoi-agents check%s                           Validate policy, client parity, and harness plugins\n", m, o)
	p("  %schezmoi-agents check -file .chezmoidata/harness.toml%s Validate plugin registry against upstream harness\n", m, o)
	p("  %schezmoi-agents render -harness pi%s              Render Pi native agent configuration\n", m, o)
	p("  %schezmoi-agents render -harness antigravity%s     Render Antigravity CLI native permissions\n", m, o)
	p("  %schezmoi-agents prompt -profile personal -check%s Verify personal prompt layer assembly\n", m, o)
	p("  %schezmoi-agents prompt -profile work%s            Display composed instructions for work profile\n", m, o)
	p("  %schezmoi-agents doctor -live%s                    Refresh usage and advise on models, accounts and capacity\n\n", m, o)

	p("%sDOCUMENTATION%s\n", b, o)
	p("  %sdocs/OS.md#shared-harness-policy%s %s·%s %sdocs/OS.md#agent-instructions%s %s·%s %sdocs/OS.md#agent-plugins-giocaizzi-harness%s\n", d, o, d, o, d, o, d, o, d, o)
	p("  %sdocs/MODELS.md%s %s·%s %sdocs/OS.md#model-tiers-chezmoi-agents-models%s %s·%s %sdocs/OS.md#doctor-chezmoi-agents-doctor%s\n", d, o, d, o, d, o, d, o, d, o)

	return sb.String()
}

func (d Deps) loadPolicy(explicitPath string) (*config.Policy, string, error) {
	if explicitPath != "" {
		cleaned := filepath.Clean(explicitPath)
		f, err := os.Open(cleaned)
		if err != nil {
			return nil, "", fmt.Errorf("open policy %s: %w", explicitPath, err)
		}
		defer f.Close()
		p, err := config.ParseTOML(f)
		if err != nil {
			return nil, "", fmt.Errorf("parse %s: %w", explicitPath, err)
		}
		return p, explicitPath, nil
	}

	candidates := []string{}
	if cfg := d.Getenv("AGENTS_CONFIG"); cfg != "" {
		candidates = append(candidates, cfg)
	}
	candidates = append(candidates,
		"agents.toml",
		filepath.Join(".agents", "config.toml"),
		filepath.Join(".agents", "agents.toml"),
	)
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".config", "agents", "agents.toml"),
			filepath.Join(home, ".config", "agents", "config.toml"),
		)
	}
	if src := d.Getenv("CHEZMOI_SOURCE_DIR"); src != "" {
		candidates = append(candidates,
			filepath.Join(src, ".chezmoidata", "agents.toml"),
			filepath.Join(src, ".chezmoidata", "harness-policy.toml"),
		)
	}
	candidates = append(candidates,
		filepath.Join("home", ".chezmoidata", "agents.toml"),
		filepath.Join("home", ".chezmoidata", "harness-policy.toml"),
		filepath.Join(".chezmoidata", "agents.toml"),
		filepath.Join(".chezmoidata", "harness-policy.toml"),
		filepath.Join("..", "home", ".chezmoidata", "harness-policy.toml"),
		filepath.Join("..", "..", "home", ".chezmoidata", "harness-policy.toml"),
	)

	for _, cand := range candidates {
		cleaned := filepath.Clean(cand)
		if _, err := os.Stat(cleaned); err == nil {
			f, err := os.Open(cleaned)
			if err != nil {
				return nil, "", fmt.Errorf("open %s: %w", cand, err)
			}
			defer f.Close()
			p, err := config.ParseTOML(f)
			if err != nil {
				return nil, "", fmt.Errorf("parse %s: %w", cand, err)
			}
			return p, cand, nil
		}
	}

	return nil, "", errors.New("harness-policy.toml not found")
}

// findPolicySourceDir returns the chezmoi source directory (the one holding
// .chezmoidata/harness-policy.toml), searched in the order loadPolicy uses.
func (d Deps) findPolicySourceDir() (string, error) {
	var candidates []string
	if src := d.Getenv("CHEZMOI_SOURCE_DIR"); src != "" {
		candidates = append(candidates, src)
	}
	candidates = append(candidates, "home", ".", filepath.Join("..", "home"), filepath.Join("..", "..", "home"))
	for _, cand := range candidates {
		cleaned := filepath.Clean(cand)
		if _, err := os.Stat(filepath.Join(cleaned, ".chezmoidata", "harness-policy.toml")); err == nil {
			return cleaned, nil
		}
	}
	return "", errors.New("chezmoi source directory not found; cannot check native voice settings")
}

func (d Deps) findHarnessToml(explicitPath string) (string, error) {
	if explicitPath != "" {
		cleaned := filepath.Clean(explicitPath)
		if _, err := os.Stat(cleaned); err != nil {
			return "", fmt.Errorf("stat %s: %w", explicitPath, err)
		}
		return cleaned, nil
	}

	candidates := []string{}
	if src := d.Getenv("CHEZMOI_SOURCE_DIR"); src != "" {
		candidates = append(candidates, filepath.Join(src, ".chezmoidata", "harness.toml"))
	}
	candidates = append(candidates,
		filepath.Join("home", ".chezmoidata", "harness.toml"),
		filepath.Join(".chezmoidata", "harness.toml"),
		filepath.Join("..", "home", ".chezmoidata", "harness.toml"),
		filepath.Join("..", "..", "home", ".chezmoidata", "harness.toml"),
	)

	for _, cand := range candidates {
		cleaned := filepath.Clean(cand)
		if _, err := os.Stat(cleaned); err == nil {
			return cleaned, nil
		}
	}

	return "", errors.New("harness.toml not found")
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func formatFindings(fs []finding.Finding) string {
	return finding.Lines(fs)
}
