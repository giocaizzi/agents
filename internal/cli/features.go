package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/feature"
)

// runFeatures prints the feature matrix: which config keys each harness
// supports.
func (d Deps) runFeatures(_ context.Context, args []string) int {
	fs := d.newFlags(commandFeatures)
	harnesses := fs.String("harnesses", "", "comma-separated harnesses to show (default: all)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	ids := feature.HarnessIDs()
	if *harnesses != "" {
		ids = ids[:0:0]
		for h := range strings.SplitSeq(*harnesses, ",") {
			h = strings.TrimSpace(h)
			if !config.Harness(h).Valid() {
				fmt.Fprintf(d.Stderr, "features: unknown harness %q\n", h)
				return exitUsage
			}
			if !slices.Contains(ids, h) {
				ids = append(ids, h)
			}
		}
	}
	// Show the harnesses in canonical order whatever order they were given in.
	ids = slices.DeleteFunc(slices.Clone(feature.HarnessIDs()), func(h string) bool { return !slices.Contains(ids, h) })
	rows := featureRows(ids)

	if *asJSON {
		out := struct {
			Harnesses []string          `json:"harnesses"`
			Features  []feature.Feature `json:"features"`
		}{ids, rows}
		enc := json.NewEncoder(d.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(d.Stderr, "features: %v\n", err)
			return exitFindings
		}
		return exitOK
	}

	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "KEY\t%s\n", strings.Join(ids, "\t"))
	for _, f := range rows {
		if f.Scope == feature.ScopeInternal {
			continue
		}
		cells := make([]string, len(ids))
		for i, h := range ids {
			cells[i] = featureSymbol(f.Cells, h)
		}
		fmt.Fprintf(tw, "%s\t%s\n", f.Key, strings.Join(cells, "\t"))
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(d.Stderr, "features: %v\n", err)
		return exitFindings
	}
	fmt.Fprintln(d.Stdout, "\n✓ written   - no native setting   planned: the harness has one, agents does not write it yet")
	return exitOK
}

// featureRows is the feature table with the cells of ids only. A harness-scoped
// key of another harness is dropped.
func featureRows(ids []string) []feature.Feature {
	var out []feature.Feature
	for _, f := range feature.Features {
		cells := map[string]feature.Cell{}
		for _, h := range ids {
			if c, ok := f.Cells[h]; ok {
				cells[h] = c
			}
		}
		if f.Scope == feature.ScopeHarness && len(cells) == 0 {
			continue
		}
		f.Cells = cells
		if len(cells) == 0 {
			f.Cells = nil
		}
		out = append(out, f)
	}
	return out
}

func featureSymbol(cells map[string]feature.Cell, h string) string {
	c, ok := cells[h]
	switch {
	case !ok:
		return ""
	case c.Status == feature.StatusRendered:
		return "✓"
	case c.Status == feature.StatusUnsupported:
		return "-"
	}
	return string(feature.StatusPlanned)
}
