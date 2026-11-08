package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/term"
	usg "github.com/giocaizzi/agents/internal/usage"
)

var update = flag.Bool("update", false, "rewrite testdata/usage_golden.txt")

var renderNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) // a Friday

func sampleSnapshots() []usg.Snapshot {
	fetched := renderNow.Add(-12 * time.Minute)
	return []usg.Snapshot{
		{
			Provider: "openrouter", Account: "", Harnesses: []string{"pi"},
			Plan:   usg.Plan{Kind: usg.KindAPI, Billing: usg.BillingMetered, Payer: usg.PayerUnknown, Name: "API key"},
			Status: usg.StatusNoAPI, Note: "no usage API", FetchedAt: fetched,
		},
		{
			Provider: "openai", Account: "me@example.com", Harnesses: []string{"codex", "opencode", "pi"},
			Plan:   usg.Plan{Kind: usg.KindIndividual, Name: "Plus"},
			Status: usg.StatusOK, Source: "api", FetchedAt: fetched,
			Limits: []usg.Limit{
				{Name: "5h", Kind: usg.LimitWindow, UsedPct: new(33.0), ResetsAt: new(renderNow.Add(3*time.Hour + 2*time.Minute))},
				{Name: "7d", Kind: usg.LimitWindow, UsedPct: new(5.0), ResetsAt: new(time.Date(2026, 11, 9, 18, 30, 0, 0, time.UTC))},
				{Name: "credits", Kind: usg.LimitCredits, Remaining: &usg.Amount{Value: 12.5, Unit: "USD"}},
			},
		},
		{
			Provider: "anthropic", Account: "me@example.com", Harnesses: []string{"claude"},
			Plan:   usg.Plan{Kind: usg.KindEnterprise, Name: "Enterprise seat"},
			Status: usg.StatusOK, Source: "api", FetchedAt: fetched,
			Limits: []usg.Limit{
				{Name: "5h", Kind: usg.LimitWindow, UsedPct: new(31.0), ResetsAt: new(renderNow.Add(2*time.Hour + 14*time.Minute))},
				{Name: "7d", Kind: usg.LimitWindow, UsedPct: new(14.0), ResetsAt: new(time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC))},
				{Name: "spend limit", Kind: usg.LimitSpend, Hidden: true, Note: "managed by org"},
			},
		},
		{
			Provider: "github-copilot", Account: "octocat", Harnesses: []string{"copilot", "opencode", "pi"},
			Plan:   usg.Plan{Kind: usg.KindTeam, Name: "Business seat"},
			Status: usg.StatusOK, Source: "api", FetchedAt: fetched,
			Limits: []usg.Limit{
				{
					Name: "premium", Kind: usg.LimitAllowance, Scope: usg.ScopePool, UsedPct: new(70.0),
					Remaining: &usg.Amount{Value: 90, Unit: "requests"}, Cap: &usg.Amount{Value: 300, Unit: "requests"},
					ResetsAt: new(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)),
				},
				{Name: "beta", Kind: usg.LimitWindow, Disabled: true},
			},
		},
		{
			Provider: "google", Account: "", Harnesses: []string{"antigravity"},
			Plan:   usg.Plan{Kind: usg.KindUnknown, Raw: "antigravity"},
			Status: usg.StatusStale, Source: "agy-log", FetchedAt: renderNow.Add(-3 * time.Hour),
			Limits: []usg.Limit{
				{Name: "Gemini · 7d", Kind: usg.LimitWindow, UsedPct: new(100.0), ResetsAt: new(time.Date(2026, 11, 7, 22, 48, 0, 0, time.UTC))},
			},
		},
		{
			Provider: "anthropic", Account: "other@example.com", Harnesses: []string{"pi"},
			Plan:   usg.Plan{Kind: usg.KindIndividual, Name: "Pro"},
			Status: usg.StatusExpired, Note: "token expired, open pi", FetchedAt: fetched,
		},
	}
}

func TestRenderUsageGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	st := term.NewStyle(&buf, func(string) string { return "" }) // not an *os.File: no colour
	renderUsage(&buf, st, sampleSnapshots(), "(cached 12m ago; -live to refresh)", renderNow)

	golden := filepath.Join("testdata", "usage_golden.txt")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if diff := cmp.Diff(string(want), buf.String()); diff != "" {
		t.Errorf("output differs from golden (-want +got):\n%s", diff)
	}
}

func TestRenderUsageColour(t *testing.T) {
	t.Parallel()
	st := term.Style{B: "<B>", M: "<M>", Y: "<Y>", G: "<G>", D: "<D>", O: "<O>"}
	var buf bytes.Buffer
	renderUsage(&buf, st, sampleSnapshots(), "(live)", renderNow)
	out := buf.String()
	for _, want := range []string{"<B>anthropic<O>", "<M>Enterprise seat · me@example.com<O>", "<Y>!<O> token expired, open pi", "<D>(live)<O>"} {
		if !strings.Contains(out, want) {
			t.Errorf("coloured output lacks %q:\n%s", want, out)
		}
	}
}

func TestRenderUsageSortsByProviderThenAccount(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderUsage(&buf, term.Style{}, sampleSnapshots(), "(live)", renderNow)
	var providers []string
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if line != "" && line[0] != ' ' && !strings.HasPrefix(line, "agents") {
			providers = append(providers, strings.Fields(line)[0])
		}
	}
	want := []string{"anthropic", "anthropic", "github-copilot", "google", "openai", "openrouter"}
	if diff := cmp.Diff(want, providers); diff != "" {
		t.Errorf("provider order (-want +got):\n%s", diff)
	}
}

func TestRenderUnprovenNextToAPIKeyIsIdentityUnknown(t *testing.T) {
	t.Parallel()
	snaps := []usg.Snapshot{
		{Provider: "openai", Plan: usg.Plan{Kind: usg.KindUnknown}, IdentityUnknown: true},
		{Provider: "openai", Plan: usg.Plan{Kind: usg.KindAPI}},
	}
	var buf bytes.Buffer
	renderUsage(&buf, term.Style{}, snaps, "(live)", renderNow)
	out := buf.String()
	if !strings.Contains(out, "identity unknown") || strings.Contains(out, "same account?") {
		t.Errorf("want identity unknown only:\n%s", out)
	}
}

func TestPlanText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s    usg.Snapshot
		want string
	}{
		{usg.Snapshot{Plan: usg.Plan{Name: "Plus"}, Account: "octocat"}, "Plus · octocat"},
		{usg.Snapshot{Plan: usg.Plan{Kind: usg.KindUnknown, Raw: "zeta"}}, "unknown (zeta)"},
		{usg.Snapshot{Plan: usg.Plan{Kind: usg.KindAPI}}, "api"},
	}
	for _, tt := range tests {
		if got := planText(tt.s, false); got != tt.want {
			t.Errorf("planText = %q, want %q", got, tt.want)
		}
	}
}
