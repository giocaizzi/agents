package cli

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/doctor"
	"github.com/giocaizzi/agents/internal/term"
)

const tierCol = 9

// renderDoctor writes the report. meta is the dim text after the title, e.g.
// "(live)". Global findings come first, then the suggestions collapsed across
// harnesses, then one group per set of harnesses with identical findings.
// With limit > 0 it is the quiet report instead: only the loud lines, at most
// limit of them, then a dim line counting the rest.
func renderDoctor(w io.Writer, st term.Style, findings []doctor.Finding, meta string, limit int) {
	fmt.Fprintf(w, "%sagents doctor — models, accounts and capacity%s   %s%s%s\n\n", st.B, st.O, st.D, sanitize(meta), st.O)
	if len(findings) == 0 {
		fmt.Fprintf(w, "  %s✓%s nothing to report\n", st.G, st.O)
		return
	}

	var global []doctor.Finding
	byHarness := map[string][]doctor.Finding{}
	var names []string
	var sugs []doctor.Finding // suggestions with models, collapsed below
	for _, f := range findings {
		switch {
		case f.Harness == "":
			global = append(global, f)
			continue
		case hasModels(f):
			sugs = append(sugs, f)
			continue
		}
		if _, ok := byHarness[f.Harness]; !ok {
			names = append(names, f.Harness)
		}
		byHarness[f.Harness] = append(byHarness[f.Harness], f)
	}
	collapsed := collapseSuggestions(sugs)

	if limit > 0 {
		renderQuiet(w, st, global, byHarness, orderHarnesses(names), collapsed, limit)
		return
	}

	sections := 0
	gap := func() {
		if sections > 0 {
			fmt.Fprintln(w)
		}
		sections++
	}
	if len(global) > 0 {
		gap()
		for _, f := range global {
			fmt.Fprintln(w, findingLine(st, f, ""))
		}
	}
	if len(collapsed) > 0 {
		gap()
		for _, c := range collapsed {
			fmt.Fprintln(w, suggestLine(st, c.f, c.harnesses))
		}
	}
	for _, g := range harnessGroups(orderHarnesses(names), byHarness) {
		gap()
		heads := make([]string, len(g))
		for j, h := range g {
			heads[j] = sanitize(h)
		}
		fmt.Fprintf(w, "%s%s%s\n", st.B, strings.Join(heads, " · "), st.O)
		for _, f := range byHarness[g[0]] {
			fmt.Fprintln(w, findingLine(st, f, "  "))
		}
	}
}

// renderQuiet writes at most limit loud lines, then a dim line counting the rest.
func renderQuiet(w io.Writer, st term.Style, global []doctor.Finding, byHarness map[string][]doctor.Finding, names []string, collapsed []collapsedSuggestion, limit int) {
	lines := loudLines(st, global, byHarness, names, collapsed)
	for i, l := range lines {
		if i == limit {
			fmt.Fprintf(w, "  %s… %d more: run agents doctor%s\n", st.D, len(lines)-limit, st.O)
			return
		}
		fmt.Fprintln(w, l)
	}
}

// loudLines are the lines quiet mode may print: global warnings, harness
// warnings (prefixed with the harness), the collapsed suggestions, then harness
// suggestions without models.
func loudLines(st term.Style, global []doctor.Finding, byHarness map[string][]doctor.Finding, names []string, collapsed []collapsedSuggestion) []string {
	var lines []string
	for _, f := range global {
		if f.Level == doctor.LevelWarn {
			lines = append(lines, findingLine(st, f, ""))
		}
	}
	lines = append(lines, harnessLines(st, byHarness, names, doctor.LevelWarn)...)
	for _, c := range collapsed {
		lines = append(lines, suggestLine(st, c.f, c.harnesses))
	}
	lines = append(lines, harnessLines(st, byHarness, names, doctor.LevelSuggest)...)
	return lines
}

// harnessLines are the harness findings of one level for quiet mode: the same
// finding in several harnesses is one line led by all of them.
func harnessLines(st term.Style, byHarness map[string][]doctor.Finding, names []string, level doctor.Level) []string {
	type same struct{ text, fix string }
	var order []same
	who := map[same][]string{}
	first := map[same]doctor.Finding{}
	for _, h := range names {
		for _, f := range byHarness[h] {
			k := same{f.Text, f.Fix}
			if f.Level != level || slices.Contains(who[k], h) {
				continue
			}
			if _, ok := who[k]; !ok {
				order = append(order, k)
				first[k] = f
			}
			who[k] = append(who[k], h)
		}
	}
	lines := make([]string, 0, len(order))
	for _, k := range order {
		f := first[k]
		f.Harness = strings.Join(who[k], ", ")
		lines = append(lines, findingLine(st, prefixed(f), ""))
	}
	return lines
}

// prefixed is f with its text led by the harness, unless it already is.
func prefixed(f doctor.Finding) doctor.Finding {
	if !strings.HasPrefix(f.Text, f.Harness+":") {
		f.Text = f.Harness + ": " + f.Text
	}
	return f
}

func hasModels(f doctor.Finding) bool {
	return f.Level == doctor.LevelSuggest && f.Current != nil && f.Suggested != nil
}

// collapsedSuggestion is a suggestion shared by harnesses (in display order).
type collapsedSuggestion struct {
	f         doctor.Finding
	harnesses []string
}

// collapseSuggestions merges suggestions identical in tier, models, reason and
// billing, ordered by tier, then by the first harness of each.
func collapseSuggestions(sugs []doctor.Finding) []collapsedSuggestion {
	var out []collapsedSuggestion
	for _, f := range sugs {
		i := slices.IndexFunc(out, func(c collapsedSuggestion) bool {
			g := c.f
			return g.Tier == f.Tier && g.Current.ID == f.Current.ID && g.Suggested.ID == f.Suggested.ID &&
				g.Reason == f.Reason && g.Billing == f.Billing
		})
		if i < 0 {
			out = append(out, collapsedSuggestion{f: f})
			i = len(out) - 1
		}
		if !slices.Contains(out[i].harnesses, f.Harness) {
			out[i].harnesses = append(out[i].harnesses, f.Harness)
		}
	}
	// Tiers have free names: they sort by first appearance in the findings,
	// which follow the config's order (default tier first, then by name).
	var tiers []string
	for _, f := range sugs {
		if !slices.Contains(tiers, f.Tier) {
			tiers = append(tiers, f.Tier)
		}
	}
	rank := func(t string) int { return slices.Index(tiers, t) }
	for i := range out {
		out[i].harnesses = orderHarnesses(out[i].harnesses)
	}
	first := func(c collapsedSuggestion) int {
		return slices.Index(orderHarnesses(allHarnesses(sugs)), c.harnesses[0])
	}
	slices.SortStableFunc(out, func(a, b collapsedSuggestion) int {
		if c := rank(a.f.Tier) - rank(b.f.Tier); c != 0 {
			return c
		}
		return first(a) - first(b)
	})
	return out
}

func allHarnesses(fs []doctor.Finding) []string {
	var out []string
	for _, f := range fs {
		if !slices.Contains(out, f.Harness) {
			out = append(out, f.Harness)
		}
	}
	return out
}

// harnessGroups merges the harnesses (already in display order) whose
// findings are identical but for the Harness field; a group sits where its
// first harness is.
func harnessGroups(ordered []string, by map[string][]doctor.Finding) [][]string {
	var groups [][]string
	for _, h := range ordered {
		i := slices.IndexFunc(groups, func(g []string) bool { return sameFindings(by[g[0]], by[h]) })
		if i < 0 {
			groups = append(groups, []string{h})
			continue
		}
		groups[i] = append(groups[i], h)
	}
	return groups
}

func sameFindings(a, b []doctor.Finding) bool {
	return slices.EqualFunc(a, b, func(x, y doctor.Finding) bool {
		x.Harness, y.Harness = "", ""
		return reflect.DeepEqual(x, y)
	})
}

func marker(st term.Style, l doctor.Level) string {
	switch l {
	case doctor.LevelWarn:
		return st.Y + "!" + st.O
	case doctor.LevelSuggest:
		return st.G + "↑" + st.O
	case doctor.LevelInfo:
	}
	return st.D + "i" + st.O
}

// findingLine is one finding as a line, indented by indent.
func findingLine(st term.Style, f doctor.Finding, indent string) string {
	if hasModels(f) {
		return indent + suggestLine(st, f, nil)
	}
	line := indent + marker(st, f.Level) + " " + sanitize(f.Text)
	if f.Fix != "" {
		line += "  " + st.D + "· " + sanitize(f.Fix) + st.O
	}
	return line
}

// suggestLine is a suggestion with models; harnesses (if any) end the dim part.
func suggestLine(st term.Style, f doctor.Finding, harnesses []string) string {
	cur, sug := f.Current, f.Suggested
	var parts []string
	if f.Reason != "" {
		parts = append(parts, sanitize(f.Reason))
	}
	if cur.Category != "" {
		parts = append(parts, fmt.Sprintf("%s #%d → #%d", sanitize(cur.Category), cur.Rank, sug.Rank))
	}
	parts = append(parts, fmt.Sprintf("$%s/$%s → $%s/$%s /Mtok", num(cur.In), num(cur.Out), num(sug.In), num(sug.Out)))
	if f.Billing != "" {
		parts = append(parts, sanitize(f.Billing))
	}
	if len(harnesses) > 0 {
		hs := make([]string, len(harnesses))
		for i, h := range harnesses {
			hs[i] = sanitize(h)
		}
		parts = append(parts, strings.Join(hs, ", "))
	}
	return fmt.Sprintf("%s↑%s %s%s%s%s → %s%s%s   %s%s%s",
		st.G, st.O,
		st.M, term.Pad(sanitize(f.Tier), tierCol), st.O,
		sanitize(cur.Name), st.M, sanitize(sug.Name), st.O,
		st.D, strings.Join(parts, " · "), st.O)
}
