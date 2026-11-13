package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/term"
)

func (d Deps) runCheck(ctx context.Context, args []string) int {
	fs := d.newFlags(commandCheck)
	cf := addConfigFlags(fs)
	live := fs.Bool("live", false, "also report native files that differ from what apply would write")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	r, err := d.loadConfig(cf)
	if err != nil {
		fmt.Fprintln(d.Stderr, err)
		return exitFindings
	}

	findings := d.validate(r)
	findings = append(findings, d.pluginFindings(ctx, r)...)
	findings = append(findings, d.modelFindings(r)...)
	findings = append(findings, d.featureFindings(r)...)
	plan, err := d.renderPlan(r)
	if err != nil {
		findings = append(findings, finding.New("render", "%v", err))
	}
	for _, w := range plan.Warnings {
		findings = append(findings, finding.Warn("render", "%s", w))
	}
	if *live {
		findings = append(findings, liveFindings(r, plan.Targets)...)
	}
	sortFindings(findings)

	if *asJSON {
		if err := writeFindingsJSON(d, findings); err != nil {
			fmt.Fprintln(d.Stderr, err)
			return exitFindings
		}
	} else {
		d.printFindings(findings)
	}
	if finding.HasErrors(findings) {
		return exitFindings
	}
	return exitOK
}

// liveFindings compares the native files with what apply would write.
func liveFindings(r *config.Resolved, targets []apply.Target) []finding.Finding {
	results, err := apply.Engine{StatePath: filepath.Join(r.Dirs.State, "apply.json"), DryRun: true}.Run(targets)
	if err != nil {
		return []finding.Finding{finding.New("apply", "%v", err)}
	}
	var out []finding.Finding
	for _, res := range results {
		switch {
		case res.Err != nil:
			out = append(out, finding.New(res.Target.Path, "%v", res.Err))
		case res.Status != apply.StatusUnchanged:
			out = append(out, finding.New(res.Target.Path, "differs from what apply would write (%s)", res.Status))
		}
	}
	return out
}

func sortFindings(fs []finding.Finding) {
	slices.SortStableFunc(fs, func(a, b finding.Finding) int {
		if a.Severity != b.Severity {
			return int(a.Severity) - int(b.Severity)
		}
		return strings.Compare(a.String(), b.String())
	})
}

func (d Deps) printFindings(fs []finding.Finding) {
	st := term.NewStyle(d.Stdout, d.Getenv)
	if len(fs) == 0 {
		fmt.Fprintf(d.Stdout, "%s✓%s config ok\n", st.G, st.O)
		return
	}
	for _, f := range fs {
		color, mark := st.R, "✗"
		switch f.Severity {
		case finding.Warning:
			color, mark = st.Y, "!"
		case finding.Info:
			color, mark = st.D, "·"
		case finding.Error:
		}
		fmt.Fprintf(d.Stdout, "%s%s%s %s\n", color, mark, st.O, f)
	}
}

func writeFindingsJSON(d Deps, fs []finding.Finding) error {
	type jsonFinding struct {
		Where    string `json:"where"`
		Message  string `json:"message"`
		Severity string `json:"severity"`
	}
	out := struct {
		Findings []jsonFinding `json:"findings"`
	}{Findings: []jsonFinding{}}
	for _, f := range fs {
		sev := "error"
		switch f.Severity {
		case finding.Warning:
			sev = "warning"
		case finding.Info:
			sev = "info"
		case finding.Error:
		}
		out.Findings = append(out.Findings, jsonFinding{Where: f.Where, Message: f.Message, Severity: sev})
	}
	enc := json.NewEncoder(d.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
