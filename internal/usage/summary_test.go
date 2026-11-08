package usage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { t := now.Add(d); return &t }

func TestCacheRoundTrip(t *testing.T) {
	t.Parallel()
	snaps := []Snapshot{
		{
			Provider: "openai", Account: "me@example.com", Harnesses: []string{"codex"}, Plan: OpenAIPlan("plus"), Status: StatusOK, FetchedAt: now,
			Limits: []Limit{{Name: "5h", Kind: LimitWindow, UsedPct: new(10.0), ResetsAt: at(time.Hour)}},
		},
		{Provider: "anthropic", Account: "z@example.com", Status: StatusStale, FetchedAt: now},
		{Provider: "anthropic", Account: "a@example.com", Status: StatusOK, FetchedAt: now},
	}
	b, err := MarshalCache(snaps, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b), "}\n") || !strings.Contains(string(b), "\n  \"schema\": 1") {
		t.Errorf("format:\n%s", b)
	}
	c, err := ParseCache(b)
	if err != nil {
		t.Fatal(err)
	}
	if c.Schema != 1 || !c.WrittenAt.Equal(now) || len(c.Snapshots) != 3 {
		t.Fatalf("%+v", c)
	}
	order := make([]string, 0, len(c.Snapshots))
	for _, s := range c.Snapshots {
		order = append(order, s.Provider+"/"+s.Account)
	}
	if diff := cmp.Diff([]string{"anthropic/a@example.com", "anthropic/z@example.com", "openai/me@example.com"}, order); diff != "" {
		t.Error(diff)
	}
	b2, _ := MarshalCache(c.Snapshots, now)
	if string(b) != string(b2) {
		t.Error("not deterministic")
	}
	if snaps[0].Provider != "openai" {
		t.Error("input mutated")
	}
}

func TestCacheEdge(t *testing.T) {
	t.Parallel()
	b, err := MarshalCache(nil, now)
	if err != nil || !strings.Contains(string(b), `"snapshots": []`) {
		t.Errorf("%s %v", b, err)
	}
	if _, err2 := ParseCache([]byte(`{"schema":2,"snapshots":[]}`)); err2 == nil {
		t.Error("want schema error")
	}
	c, err := ParseCache([]byte(`{"schema":1,"extra":true,"snapshots":[{"provider":"p","future":1}]}`))
	if err != nil || len(c.Snapshots) != 1 {
		t.Errorf("%+v %v", c, err)
	}
}

func TestSummaries(t *testing.T) {
	t.Parallel()
	plus := OpenAIPlan("plus")
	snaps := []Snapshot{
		{Provider: "openai", Account: "b", Plan: plus, Limits: []Limit{
			{Name: "5h", UsedPct: new(80.0), ResetsAt: at(time.Hour)},
			{Name: "7d", UsedPct: new(20.0), ResetsAt: at(time.Hour)},
			{Name: "past", UsedPct: new(100.0), ResetsAt: at(-time.Hour)},
			{Name: "hidden", UsedPct: new(100.0), Hidden: true},
			{Name: "disabled", UsedPct: new(100.0), Disabled: true},
			{Name: "nopct"},
		}},
		{Provider: "openai", Account: "a", Plan: plus, Limits: []Limit{{Name: "5h", UsedPct: new(50.0)}}},
		{Provider: "openai", Account: "c", Plan: OpenAIPlan("team")},
		{Provider: "google", Account: "g", Plan: Plan{Kind: KindUnknown}, Limits: []Limit{{Name: "x", Hidden: true, UsedPct: new(1.0)}}},
	}
	got := Summaries(snaps, now)
	want := []Summary{
		{Provider: "google", Plans: []Plan{{Kind: KindUnknown}}},
		{Provider: "openai", Plans: []Plan{plus, OpenAIPlan("team")}, Headroom: new(0.5)},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error(diff)
	}
	if Summaries(nil, now) != nil {
		t.Error("want nil")
	}
}

func TestHumanDuration(t *testing.T) {
	t.Parallel()
	tests := map[time.Duration]string{
		-time.Hour:                   "<1m",
		30 * time.Second:             "<1m",
		41 * time.Minute:             "41m",
		2*time.Hour + 14*time.Minute: "2h 14m",
		33*time.Hour + 5*time.Minute: "1d 9h",
		9*24*time.Hour + 2*time.Hour: "9d 2h",
	}
	for d, want := range tests {
		if got := HumanDuration(d); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
}

func TestAdvisories(t *testing.T) {
	t.Parallel()
	plus := OpenAIPlan("plus")
	snaps := []Snapshot{
		{Provider: "openai", Plan: plus, Limits: []Limit{
			{Name: "5h", Kind: LimitWindow, UsedPct: new(93.4), ResetsAt: at(2*time.Hour + 14*time.Minute)},
			{Name: "7d", Kind: LimitWindow, UsedPct: new(90.0)},
			{Name: "89", Kind: LimitWindow, UsedPct: new(89.9)},
			{Name: "past", Kind: LimitWindow, UsedPct: new(99.0), ResetsAt: at(-time.Minute)},
			{Name: "hidden", Kind: LimitSpend, UsedPct: new(99.0), Hidden: true},
			{Name: "disabled", Kind: LimitWindow, UsedPct: new(99.0), Disabled: true},
		}},
		{Provider: "github-copilot", Plan: CopilotPlan("pro", false), Limits: []Limit{
			{Name: "premium", Kind: LimitAllowance, UsedPct: new(100.0), Overage: new(true), ResetsAt: at(24 * time.Hour)},
			{Name: "chat", Kind: LimitAllowance, UsedPct: new(100.0), Overage: new(false), ResetsAt: at(41 * time.Minute)},
		}},
		{Provider: "google", Plan: Plan{Kind: KindUnknown}, Limits: []Limit{{Name: "x", UsedPct: new(100.0)}}},
	}
	want := []Advisory{
		{"github-copilot", LevelWarn, "github-copilot chat at 100%, resets in 41m"},
		{"github-copilot", LevelInfo, "github-copilot premium allowance used up, overage on"},
		{"openai", LevelWarn, "openai 5h at 93%, resets in 2h 14m"},
		{"openai", LevelWarn, "openai 7d at 90%"},
	}
	if diff := cmp.Diff(want, Advisories(snaps, now)); diff != "" {
		t.Error(diff)
	}
	if got := Advisories(nil, now); len(got) != 0 {
		t.Errorf("%v", got)
	}
}
