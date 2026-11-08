package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/doctor"
	"github.com/giocaizzi/agents/internal/term"
)

func suggestFinding(harness string) doctor.Finding {
	return doctor.Finding{
		Level: doctor.LevelSuggest, Kind: doctor.KindBetterModel, Harness: harness, Role: doctor.RoleDriver,
		Text:      "driver: a better model is available",
		Current:   &doctor.ModelRef{ID: "acme/old", Name: "Old 4", In: 3, Out: 15, Category: "coding", Rank: 9},
		Suggested: &doctor.ModelRef{ID: "acme/new", Name: "New 5", In: 2.5, Out: 12.5, Category: "coding", Rank: 2},
		Reason:    "better-ranked", Billing: "included in your plan",
	}
}

func sampleFindings() []doctor.Finding {
	warn := func(h string) doctor.Finding {
		return doctor.Finding{Level: doctor.LevelWarn, Kind: doctor.KindUnknownModel, Harness: h, Text: "scout: model ghost is not in the catalog", Fix: "harnessPolicy.models.tiers.scout"}
	}
	return []doctor.Finding{
		{Level: doctor.LevelWarn, Kind: doctor.KindStaleCatalog, Text: "model catalog synced 40 days ago (2026-09-27)", Fix: "agents models sync"},
		{Level: doctor.LevelInfo, Kind: doctor.KindNoUsage, Text: "no usage data: account checks skipped"},
		warn("claude"),
		suggestFinding("claude"),
		warn("codex"),
		suggestFinding("codex"),
		{Level: doctor.LevelSuggest, Kind: doctor.KindNoAccount, Harness: "pi", Text: "pi: no account for provider acme", Fix: "agents usage -live"},
	}
}

const wantPlain = `agents doctor — models, accounts and capacity   (live)

! model catalog synced 40 days ago (2026-09-27)  · agents models sync
i no usage data: account checks skipped

↑ driver   Old 4 → New 5   better-ranked · coding #9 → #2 · $3/$15 → $2.5/$12.5 /Mtok · included in your plan · claude, codex

claude · codex
  ! scout: model ghost is not in the catalog  · harnessPolicy.models.tiers.scout

pi
  ↑ pi: no account for provider acme  · agents usage -live
`

func TestRenderDoctorPlain(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderDoctor(&buf, term.Style{}, sampleFindings(), "(live)", 0)
	if diff := cmp.Diff(wantPlain, buf.String()); diff != "" {
		t.Errorf("render mismatch (-want +got):\n%s", diff)
	}
}

func TestRenderDoctorStyled(t *testing.T) {
	t.Parallel()
	st := term.Style{B: "<B>", M: "<M>", R: "<R>", G: "<G>", Y: "<Y>", C: "<C>", D: "<D>", O: "<O>"}
	var buf bytes.Buffer
	renderDoctor(&buf, st, sampleFindings(), "(live)", 0)
	want := `<B>agents doctor — models, accounts and capacity<O>   <D>(live)<O>

<Y>!<O> model catalog synced 40 days ago (2026-09-27)  <D>· agents models sync<O>
<D>i<O> no usage data: account checks skipped

<G>↑<O> <M>driver   <O>Old 4 → <M>New 5<O>   <D>better-ranked · coding #9 → #2 · $3/$15 → $2.5/$12.5 /Mtok · included in your plan · claude, codex<O>

<B>claude · codex<O>
  <Y>!<O> scout: model ghost is not in the catalog  <D>· harnessPolicy.models.tiers.scout<O>

<B>pi<O>
  <G>↑<O> pi: no account for provider acme  <D>· agents usage -live<O>
`
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Errorf("styled render mismatch (-want +got):\n%s", diff)
	}
}

func TestRenderDoctorNothingToReport(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderDoctor(&buf, term.Style{}, nil, "(no usage cache; -live to refresh)", 0)
	want := "agents doctor — models, accounts and capacity   (no usage cache; -live to refresh)\n\n  ✓ nothing to report\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Errorf("render mismatch (-want +got):\n%s", diff)
	}
}

// Groups of different harnesses stay apart and keep harness order, whatever
// the order of the findings; a group sits where its first harness is.
func TestRenderDoctorGroupOrder(t *testing.T) {
	t.Parallel()
	a := func(h string) doctor.Finding {
		return doctor.Finding{Level: doctor.LevelInfo, Harness: h, Text: "same"}
	}
	findings := []doctor.Finding{a("pi"), {Level: doctor.LevelInfo, Harness: "codex", Text: "other"}, a("claude"), a("zed")}
	var buf bytes.Buffer
	renderDoctor(&buf, term.Style{}, findings, "", 0)
	want := "claude · pi · zed\n  i same\n\ncodex\n  i other\n"
	if !strings.HasSuffix(buf.String(), want) {
		t.Errorf("groups = %q, want suffix %q", buf.String(), want)
	}
}

// A suggest finding without models falls back to its text line.
func TestRenderDoctorSuggestWithoutModels(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderDoctor(&buf, term.Style{}, []doctor.Finding{{Level: doctor.LevelSuggest, Text: "try acme", Fix: "do it"}}, "", 0)
	if want := "↑ try acme  · do it\n"; !strings.HasSuffix(buf.String(), want) {
		t.Errorf("output = %q, want suffix %q", buf.String(), want)
	}
}

// Strings from findings may echo cache data: no control character reaches the terminal.
func TestRenderDoctorSanitizes(t *testing.T) {
	t.Parallel()
	f := suggestFinding("cl\x1b[2Jaude")
	f.Role = "dri\x1bver"
	f.Current.Name = "Old\x07"
	f.Reason = "bet\x1bter"
	findings := []doctor.Finding{
		{Level: doctor.LevelWarn, Text: "bad\x1b[31m text", Fix: "fi\x1bx"},
		f,
	}
	var buf bytes.Buffer
	renderDoctor(&buf, term.Style{}, findings, "(me\x1bta)", 0)
	if strings.ContainsRune(buf.String(), '\x1b') || strings.ContainsRune(buf.String(), '\x07') {
		t.Errorf("control character in output: %q", buf.String())
	}
}

func roleSuggest(harness, role, sug string) doctor.Finding {
	f := suggestFinding(harness)
	f.Role = role
	f.Suggested.ID = sug
	return f
}

// Identical suggestions collapse into one line per role, in role order, with
// their harnesses in harness order; a different model pair stays apart.
func TestRenderDoctorCollapsesSuggestions(t *testing.T) {
	t.Parallel()
	findings := []doctor.Finding{
		roleSuggest("pi", doctor.RoleDriver, "acme/new"),
		roleSuggest("copilot", doctor.RoleScout, "acme/new"),
		roleSuggest("claude", doctor.RoleDriver, "acme/new"),
		roleSuggest("pi", doctor.RoleScout, "acme/new"),
		roleSuggest("codex", doctor.RoleDriver, "acme/other"),
	}
	var buf bytes.Buffer
	renderDoctor(&buf, term.Style{}, findings, "", 0)
	var got []string
	for l := range strings.SplitSeq(buf.String(), "\n") {
		if strings.HasPrefix(l, "↑") {
			got = append(got, l[strings.LastIndex(l, " · ")+len(" · "):])
		}
	}
	want := []string{"copilot, pi", "claude, pi", "codex"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("collapsed harness lists (-want +got):\n%s\n%s", diff, buf.String())
	}
	if strings.Contains(buf.String(), "\nclaude") || strings.Contains(buf.String(), "\npi") {
		t.Errorf("suggestions still repeat in harness groups:\n%s", buf.String())
	}
}

func quietFindings(warns, sugs int) []doctor.Finding {
	fs := make([]doctor.Finding, 0, 1+warns+sugs)
	fs = append(fs, doctor.Finding{Level: doctor.LevelInfo, Text: "info dropped"})
	for i := range warns {
		fs = append(fs, doctor.Finding{Level: doctor.LevelWarn, Kind: doctor.KindCapacity, Harness: "codex", Text: fmt.Sprintf("openai %dh at 95%%", i+1)})
	}
	roles := []string{doctor.RoleScout, doctor.RoleWorker, doctor.RoleDriver, doctor.RoleEscalate}
	for i := range sugs {
		fs = append(fs, roleSuggest("claude", roles[i], "acme/new"))
	}
	return fs
}

func TestRenderDoctorQuietWithinLimit(t *testing.T) {
	t.Parallel()
	findings := append([]doctor.Finding{{Level: doctor.LevelWarn, Text: "global warn"}}, quietFindings(2, 2)...)
	var buf bytes.Buffer
	renderDoctor(&buf, term.Style{}, findings, "(live)", 5)
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2+5 {
		t.Fatalf("lines = %d, want title, blank and 5:\n%s", len(lines), buf.String())
	}
	for i, prefix := range []string{"! global warn", "! codex: openai 1h at 95%", "! codex: openai 2h at 95%", "↑ scout", "↑ worker"} {
		if !strings.HasPrefix(lines[2+i], prefix) {
			t.Errorf("line %d = %q, want prefix %q", i, lines[2+i], prefix)
		}
	}
	if strings.Contains(buf.String(), "more") || strings.Contains(buf.String(), "info dropped") {
		t.Errorf("unexpected content:\n%s", buf.String())
	}
}

func TestRenderDoctorQuietOverLimit(t *testing.T) {
	t.Parallel()
	st := term.Style{D: "<D>", O: "<O>"}
	var buf bytes.Buffer
	renderDoctor(&buf, st, quietFindings(4, 3), "", 5)
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2+5+1 {
		t.Fatalf("lines = %d, want title, blank, 5 and the more line:\n%s", len(lines), buf.String())
	}
	if want := "  <D>… 2 more: run agents doctor<O>"; lines[len(lines)-1] != want {
		t.Errorf("last line = %q, want %q", lines[len(lines)-1], want)
	}
}

func TestRenderDoctorQuietCollapsesHarnesses(t *testing.T) {
	t.Parallel()
	warn := func(h string) doctor.Finding {
		return doctor.Finding{Level: doctor.LevelWarn, Kind: doctor.KindCapacity, Harness: h, Text: "openai 5h at 95%"}
	}
	var buf bytes.Buffer
	renderDoctor(&buf, term.Style{}, []doctor.Finding{warn("codex"), warn("pi")}, "", 5)
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if diff := cmp.Diff([]string{"! codex, pi: openai 5h at 95%"}, lines[2:]); diff != "" {
		t.Errorf("quiet lines (-want +got):\n%s", diff)
	}
}
