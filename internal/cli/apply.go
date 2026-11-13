package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/term"
)

// runApply writes every selected harness's native files.
func (d Deps) runApply(_ context.Context, args []string) int {
	fs := d.newFlags(commandApply)
	cf := addConfigFlags(fs)
	dry := fs.Bool("n", false, "dry run: print the diff and write nothing")
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	r, err := d.loadConfig(cf)
	if err != nil {
		fmt.Fprintln(d.Stderr, err)
		return exitFindings
	}
	if findings := d.validate(r); finding.HasErrors(findings) {
		sortFindings(findings)
		d.printFindings(findings)
		return exitFindings
	}
	plan, err := d.renderPlan(r)
	for _, w := range plan.Warnings {
		fmt.Fprintf(d.Stderr, "warning: %s\n", w)
	}
	for _, n := range plan.Notes {
		fmt.Fprintf(d.Stderr, "note: %s\n", n)
	}
	if err != nil {
		fmt.Fprintln(d.Stderr, err)
		return exitFindings
	}

	results, err := apply.Engine{StatePath: filepath.Join(r.Dirs.State, "apply.json"), DryRun: *dry}.Run(plan.Targets)
	if err != nil {
		fmt.Fprintf(d.Stderr, "apply: %v\n", err)
		return exitFindings
	}
	st := term.NewStyle(d.Stdout, d.Getenv)
	code := exitOK
	for _, res := range results {
		switch {
		case res.Status == apply.StatusRefused:
			fmt.Fprintf(d.Stdout, "%s%s%s %s\n", st.R, res.Status, st.O, res.Target.Path)
		case res.Err != nil:
			fmt.Fprintf(d.Stdout, "%serror%s %s\n", st.R, st.O, res.Target.Path)
		case res.Status == apply.StatusUnchanged:
			fmt.Fprintf(d.Stdout, "%s%s %s%s\n", st.D, res.Status, res.Target.Path, st.O)
		case res.Status == apply.StatusDrift:
			fmt.Fprintf(d.Stdout, "%s%s%s %s\n", st.Y, res.Status, st.O, res.Target.Path)
		default:
			fmt.Fprintf(d.Stdout, "%s%s%s %s\n", st.G, res.Status, st.O, res.Target.Path)
		}
		if res.Err != nil {
			fmt.Fprintf(d.Stderr, "%s: %v\n", res.Target.Path, res.Err)
			code = exitFindings
		}
		if res.Status == apply.StatusRefused {
			code = exitFindings
		}
		if *dry && res.Diff != "" {
			fmt.Fprintln(d.Stdout, res.Diff)
		}
	}
	return code
}
