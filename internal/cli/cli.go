// Package cli implements the agents command line interface.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

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
	if d.Stdin == nil {
		d.Stdin = os.Stdin
	}
	if d.Stdout == nil {
		d.Stdout = os.Stdout
	}
	if d.Stderr == nil {
		d.Stderr = os.Stderr
	}
	if d.Getenv == nil {
		fmt.Fprintln(d.Stderr, "agents: internal error: Deps.Getenv is nil")
		return 2
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
	case commandFeatures:
		return d.runFeatures(ctx, rest)
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

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
