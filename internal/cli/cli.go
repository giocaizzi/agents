// Package cli implements the agents command line interface.
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

	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/policy"
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
	// LookPath finds executables (default exec.LookPath).
	LookPath func(string) (string, error)
	// Hostname names this machine (default os.Hostname).
	Hostname func() (string, error)
	// RunCommand runs an external command with the given streams (default
	// os/exec).
	RunCommand func(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) error
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
		d.printHelp(d.Stdout, "", nil)
		return 0
	}

	cmd := args[0]
	rest := args[1:]
	if cmd == helpCmd {
		fs := d.newFlags("help")
		if err := fs.Parse(rest); err != nil {
			return d.parseFailure(fs, err)
		}
		return d.explicitHelp(ctx, fs.Args())
	}

	switch cmd {
	case commandCheck:
		return d.runCheck(ctx, rest)
	case commandConfig:
		return d.runConfig(ctx, rest)
	case commandSchema:
		return d.runSchema(ctx, rest)
	case commandRender:
		return d.runRender(ctx, rest)
	case commandApply:
		return d.runApply(ctx, rest)
	case commandPrompt:
		return d.runPrompt(ctx, rest)
	case commandMCP:
		return d.runMCP(ctx, rest)
	case commandModels:
		return d.runModels(ctx, rest)
	case "usage":
		return d.runUsage(ctx, rest)
	case "doctor":
		return d.runDoctor(rest)
	case commandPlugins:
		return d.runPlugins(ctx, rest)
	case "init":
		return d.runInit(ctx, rest)
	case commandVersion, "--version", "-v":
		fs := d.newFlags(commandVersion)
		if err := fs.Parse(rest); err != nil {
			return d.parseFailure(fs, err)
		}
		v := d.Version
		if v == "" {
			v = "dev"
		}
		fmt.Fprintf(d.Stdout, "agents %s\n", v)
		return 0
	case helpFlag, "-h":
		d.printHelp(d.Stdout, "", nil)
		return 0
	default:
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%sunknown command: %s%s\n\n", st.R, cmd, st.O)
		d.printHelp(d.Stderr, "", nil)
		return 2
	}
}

func (d Deps) loadPolicy(explicitPath string) (*policy.Policy, string, error) {
	if explicitPath != "" {
		cleaned := filepath.Clean(explicitPath)
		f, err := os.Open(cleaned)
		if err != nil {
			return nil, "", fmt.Errorf("open policy %s: %w", explicitPath, err)
		}
		defer f.Close()
		p, err := policy.ParseTOML(f)
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
	if home := d.homeDir(); home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".config", "agents", "agents.toml"),
			filepath.Join(home, ".config", "agents", "config.toml"),
		)
	}
	if src := d.Getenv("CHEZMOI_SOURCE_DIR"); src != "" {
		candidates = append(candidates,
			filepath.Join(src, "home", ".chezmoidata", "agents.toml"),
			filepath.Join(src, ".chezmoidata", "agents.toml"),
			filepath.Join(src, "home", ".chezmoidata", "harness-policy.toml"),
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
	if d.Getenv("CHEZMOI_SOURCE_DIR") == "" {
		if home := d.homeDir(); home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "share", "chezmoi", "home", ".chezmoidata", "agents.toml"),
				filepath.Join(home, ".local", "share", "chezmoi", ".chezmoidata", "agents.toml"),
				filepath.Join(home, ".local", "share", "chezmoi", "home", ".chezmoidata", "harness-policy.toml"),
				filepath.Join(home, ".local", "share", "chezmoi", ".chezmoidata", "harness-policy.toml"),
			)
		}
	}

	for _, cand := range candidates {
		cleaned := filepath.Clean(cand)
		if _, err := os.Stat(cleaned); err == nil {
			f, err := os.Open(cleaned)
			if err != nil {
				return nil, "", fmt.Errorf("open %s: %w", cand, err)
			}
			defer f.Close()
			p, err := policy.ParseTOML(f)
			if err != nil {
				return nil, "", fmt.Errorf("parse %s: %w", cand, err)
			}
			return p, cand, nil
		}
	}

	return nil, "", errors.New("harness-policy.toml not found")
}

// findPolicySourceDir returns the chezmoi source directory (the one holding
// .chezmoidata/agents.toml or harness-policy.toml), searched in the order loadPolicy uses.
func (d Deps) findPolicySourceDir() (string, error) {
	var candidates []string
	if src := d.Getenv("CHEZMOI_SOURCE_DIR"); src != "" {
		candidates = append(candidates, src, filepath.Join(src, "home"))
	}
	candidates = append(candidates, "home", ".", filepath.Join("..", "home"), filepath.Join("..", "..", "home"))
	if d.Getenv("CHEZMOI_SOURCE_DIR") == "" {
		if home := d.homeDir(); home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "share", "chezmoi", "home"),
				filepath.Join(home, ".local", "share", "chezmoi"),
			)
		}
	}
	for _, cand := range candidates {
		cleaned := filepath.Clean(cand)
		if _, err := os.Stat(filepath.Join(cleaned, ".chezmoidata", "agents.toml")); err == nil {
			return cleaned, nil
		}
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
		candidates = append(candidates,
			filepath.Join(src, "home", ".chezmoidata", "harness.toml"),
			filepath.Join(src, ".chezmoidata", "harness.toml"),
		)
	}
	candidates = append(candidates,
		filepath.Join("home", ".chezmoidata", "harness.toml"),
		filepath.Join(".chezmoidata", "harness.toml"),
		filepath.Join("..", "home", ".chezmoidata", "harness.toml"),
		filepath.Join("..", "..", "home", ".chezmoidata", "harness.toml"),
	)
	if d.Getenv("CHEZMOI_SOURCE_DIR") == "" {
		if home := d.homeDir(); home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "share", "chezmoi", "home", ".chezmoidata", "harness.toml"),
				filepath.Join(home, ".local", "share", "chezmoi", ".chezmoidata", "harness.toml"),
			)
		}
	}

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
