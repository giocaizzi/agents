package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/harness"
	"github.com/giocaizzi/agents/internal/instructions"
	"github.com/giocaizzi/agents/internal/term"
)

const skippedNote = "skipped (not on this machine)"

// runCommand runs argv with the given streams (the default Deps.RunCommand).
func runCommand(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return cmd.Run() //nolint:wrapcheck // the caller adds the command name
}

func (d Deps) runPrompt(ctx context.Context, args []string) int {
	if len(args) == 0 {
		d.printHelp(d.Stdout, commandPrompt, nil)
		return exitOK
	}
	rest := args[1:]
	switch args[0] {
	case "edit":
		return d.runPromptEdit(ctx, rest)
	case commandShow:
		return d.runPromptShow(rest)
	case "matrix":
		return d.runPromptMatrix(rest)
	case commandCheck:
		return d.runPromptCheck(rest)
	case helpCmd, helpFlag, "-h":
		return d.explicitHelp(ctx, append([]string{commandPrompt}, rest...))
	default:
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%sunknown prompt subcommand: %s%s\n\n", st.R, args[0], st.O)
		d.printHelp(d.Stderr, commandPrompt, nil)
		return exitUsage
	}
}

// promptView is the loaded configuration seen as instruction layers.
type promptView struct {
	r        *config.Resolved
	spec     instructions.Spec
	profiles []string // declared profiles, sorted; "" when none
	hosts    []string // selected harnesses
	hostname string
}

// loadPrompt parses fs, requires wantArgs positional arguments, loads the
// config and builds the instruction view. A nil view means the caller must
// return the code.
func (d Deps) loadPrompt(fs *flag.FlagSet, cf *configFlags, args []string, wantArgs int) (*promptView, int) {
	if err := fs.Parse(args); err != nil {
		return nil, d.parseFailure(fs, err)
	}
	if fs.NArg() != wantArgs {
		fmt.Fprintf(d.Stderr, "%s: want %d argument(s), got %d\n\n", fs.Name(), wantArgs, fs.NArg())
		d.printHelp(d.Stderr, fs.Name(), fs)
		return nil, exitUsage
	}
	r, err := d.loadConfig(cf)
	if err != nil {
		fmt.Fprintf(d.Stderr, "%s: %v\n", fs.Name(), err)
		return nil, exitFindings
	}
	dir := r.Config.Instructions.Dir
	if dir == "" {
		dir = filepath.Join(r.Dirs.Config, "instructions")
	}
	dir, unset := config.ExpandPath(dir, d.home(), d.Getenv)
	if len(unset) > 0 {
		fmt.Fprintf(d.Stderr, "%s: instructions.dir: ${%s} is unset\n", fs.Name(), strings.Join(unset, "}, ${"))
		return nil, exitFindings
	}
	v := &promptView{r: r, spec: harness.InstructionSpec(&r.Config, dir), hostname: d.harnessEnv(r).Hostname}
	v.profiles = slices.Sorted(maps.Keys(r.Config.Profiles))
	if len(v.profiles) == 0 {
		v.profiles = []string{""}
	}
	for _, h := range r.Harnesses {
		v.hosts = append(v.hosts, string(h))
	}
	return v, exitOK
}

func (v *promptView) context(d Deps, profile, harnessName string) instructions.Context {
	return instructions.Context{Profile: profile, Harness: harnessName, OS: d.GOOS, Hostname: v.hostname}
}

func (d Deps) runPromptEdit(ctx context.Context, args []string) int {
	fs := d.newFlags("prompt edit")
	cf := addConfigFlags(fs)
	v, code := d.loadPrompt(fs, cf, args, 1)
	if v == nil {
		return code
	}
	name := fs.Arg(0)
	if _, ok := v.spec.Layers[name]; !ok {
		fmt.Fprintf(d.Stderr, "prompt edit: layer %q is not declared (declared: %s)\n", name, strings.Join(slices.Sorted(maps.Keys(v.spec.Layers)), ", "))
		return exitFindings
	}
	file := v.spec.Path(name)
	var argv []string
	if tmpl := v.r.Config.Instructions.EditCommand; len(tmpl) > 0 {
		rep := strings.NewReplacer("{name}", name, "{file}", file)
		for _, a := range tmpl {
			argv = append(argv, rep.Replace(a))
		}
	} else if editor := strings.Fields(d.Getenv("EDITOR")); len(editor) > 0 {
		argv = append(slices.Clone(editor), file)
	} else {
		fmt.Fprintln(d.Stderr, "prompt edit: set instructions.edit_command or $EDITOR")
		return exitFindings
	}
	run := d.RunCommand
	if run == nil {
		run = runCommand
	}
	if err := run(ctx, argv, d.Stdin, d.Stdout, d.Stderr); err != nil {
		fmt.Fprintf(d.Stderr, "prompt edit: run %s: %v\n", argv[0], err)
		return exitFindings
	}
	return exitOK
}

func (d Deps) runPromptShow(args []string) int {
	fs := d.newFlags("prompt show")
	cf := addConfigFlags(fs) // -profile selects the profile of the cell
	hf := fs.String("harness", "", "harness to render (default: the first selected harness)")
	v, code := d.loadPrompt(fs, cf, args, 0)
	if v == nil {
		return code
	}
	h := *hf
	if h == "" {
		if len(v.hosts) == 0 {
			fmt.Fprintln(d.Stderr, "prompt show: no harness selected; pass -harness")
			return exitFindings
		}
		h = v.hosts[0]
	}
	text, err := v.spec.Render(v.context(d, v.r.Profile, h), os.ReadFile)
	if err != nil {
		fmt.Fprintf(d.Stderr, "prompt show: %v\n", err)
		return exitFindings
	}
	fmt.Fprint(d.Stdout, text)
	return exitOK
}

func (d Deps) runPromptMatrix(args []string) int {
	fs := d.newFlags("prompt matrix")
	cf := addConfigFlags(fs)
	v, code := d.loadPrompt(fs, cf, args, 0)
	if v == nil {
		return code
	}
	rows := v.spec.Matrix(v.profiles, v.hosts)
	var pw, hw int
	for _, row := range rows {
		pw, hw = max(pw, len(row.Profile)), max(hw, len(row.Harness))
	}
	for _, row := range rows {
		fmt.Fprintf(d.Stdout, "%-*s  %-*s  %s\n", pw, row.Profile, hw, row.Harness, strings.Join(row.Layers, ", "))
	}
	return exitOK
}

func (d Deps) runPromptCheck(args []string) int {
	fs := d.newFlags("prompt check")
	cf := addConfigFlags(fs)
	all := fs.Bool("all", false, "report missing layer files of other profiles as errors")
	v, code := d.loadPrompt(fs, cf, args, 0)
	if v == nil {
		return code
	}
	ctxFor := func(p, h string) instructions.Context { return v.context(d, p, h) }
	type cell struct{ profile, harness string }
	dirty := map[cell]bool{}
	errCount := 0
	for _, p := range v.spec.Check(v.profiles, v.hosts, ctxFor, os.ReadFile) {
		dirty[cell{p.Profile, p.Harness}] = true
		label := fmt.Sprintf("%s  %s  %s", p.Profile, p.Harness, p.Layer)
		if p.Kind == instructions.MissingFile && p.Profile != v.r.Profile && !*all {
			fmt.Fprintf(d.Stdout, "- %s: %s\n", label, skippedNote)
			continue
		}
		errCount++
		fmt.Fprintf(d.Stderr, "x %s: %v\n", label, &p)
	}
	for _, row := range v.spec.Matrix(v.profiles, v.hosts) {
		if !dirty[cell{row.Profile, row.Harness}] {
			fmt.Fprintf(d.Stdout, "✓ %s  %s\n", row.Profile, row.Harness)
		}
	}
	if errCount > 0 {
		return exitFindings
	}
	return exitOK
}
