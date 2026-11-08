package catalog

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func pm(provider, id, name, canonical, date string, in, out float64) ProviderModel {
	return ProviderModel{
		Provider: provider, ID: id, Name: name, CanonicalID: canonical, ReleaseDate: date,
		Context: 1000, Price: &Price{In: in, Out: out}, ToolCall: true, TextOutput: true,
	}
}

func baseCatalog() Catalog {
	return Catalog{
		Schema:    Schema,
		Since:     "2026-01-01",
		Providers: []string{"anthropic", "openai", "google"},
		Pin:       map[string]string{"anthropic/claude-opus-5-5": "powerful"},
		Ignore:    []string{"openai/gpt-realtime-2.1"},
		Models:    map[string]Model{},
	}
}

func TestApplyKeepRules(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Pin["anthropic/claude-haiku-3"] = "lightweight"
	c.Pin["openai/gpt-realtime-2.1"] = "extra"
	c.Ignore = append(c.Ignore, "openai/gpt-ignored")
	noText := pm("openai", "gpt-image", "GPT Image", "openai/gpt-image", "2026-07-06", 4, 24)
	noText.TextOutput = false
	deprecated := pm("google", "gemini-old", "Gemini Old", "google/gemini-old", "2026-05-01", 1, 2)
	deprecated.Deprecated = true
	noPrice := pm("google", "gemma-4", "Gemma 4", "google/gemma-4", "2026-06-01", 0, 0)
	noPrice.Price = nil
	src := Sources{Today: "2026-11-06", ModelsDev: []ProviderModel{
		pm("anthropic", "claude-opus-5-5", "Claude Opus 5.5", "anthropic/claude-opus-5-5", "2026-09-22", 4, 20),
		// untracked provider: ignored entirely
		pm("reseller", "claude-opus-5.5", "Claude Opus 5.5 (reseller)", "anthropic/claude-opus-5-5", "2026-09-23", 9, 9),
		pm("reseller", "only-here", "Only Here", "reseller/only-here", "2026-07-21", 1, 1),
		// non-first-party entry under a tracked provider: ignored
		pm("openai", "claude-clone", "Claude Clone", "anthropic/claude-clone", "2026-09-23", 1, 1),
		pm("anthropic", "claude-3-opus", "Claude 3 Opus", "anthropic/claude-3-opus", "2024-02-29", 15, 75),
		pm("anthropic", "claude-haiku-3", "Claude Haiku 3", "anthropic/claude-haiku-3", "2024-03-07", 0.25, 1.25),
		pm("openai", "gpt-6-luna", "GPT-6 Luna", "openai/gpt-6-luna", "2026-09-22", 0.1, 0.5),
		pm("openai", "gpt-realtime-2.1", "GPT-Realtime-2.1", "openai/gpt-realtime-2.1", "2026-07-06", 4, 24),
		noText,
		pm("openai", "gpt-ignored", "Ignored", "openai/gpt-ignored", "2026-07-06", 1, 1),
		deprecated,
		noPrice,
	}}

	got, rep := Apply(c, src)

	wantIDs := []string{
		"anthropic/claude-haiku-3", "anthropic/claude-opus-5-5", "google/gemma-4",
		"openai/gpt-6-luna", "openai/gpt-realtime-2.1",
	}
	if diff := cmp.Diff(wantIDs, sortedKeys(got.Models)); diff != "" {
		t.Errorf("model ids (-want +got):\n%s", diff)
	}
	wantOpus := Model{
		Name: "Claude Opus 5.5", Provider: "anthropic", ID: "claude-opus-5-5", ReleaseDate: "2026-09-22",
		Context: 1000, API: &Price{In: 4, Out: 20},
	}
	if diff := cmp.Diff(wantOpus, got.Models["anthropic/claude-opus-5-5"]); diff != "" {
		t.Errorf("opus (-want +got):\n%s", diff)
	}
	if m := got.Models["google/gemma-4"]; m.API != nil || m.Provider != "google" {
		t.Errorf("gemma = %+v, want provider google and no API price", m)
	}
	if got.SyncedAt != "2026-11-06" || got.Schema != Schema || got.Since != c.Since {
		t.Errorf("header = %q %d %q", got.SyncedAt, got.Schema, got.Since)
	}
	wantUntiered := []string{"google/gemma-4", "openai/gpt-6-luna"}
	if diff := cmp.Diff(wantUntiered, rep.Untiered); diff != "" {
		t.Errorf("untiered (-want +got):\n%s", diff)
	}
}

func TestApplyCanonicalIDPartWins(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Pin["openai/gpt-5.6-sol"] = "extra"
	alias := pm("openai", "gpt-5.6", "GPT-5.6", "openai/gpt-5.6-sol", "2026-08-01", 4, 20)
	sol := pm("openai", "gpt-5.6-sol", "GPT-5.6 Sol", "openai/gpt-5.6-sol", "2026-08-02", 5, 25)
	for _, entries := range [][]ProviderModel{{alias, sol}, {sol, alias}} {
		got, _ := Apply(c, Sources{ModelsDev: entries})
		m := got.Models["openai/gpt-5.6-sol"]
		if m.ID != "gpt-5.6-sol" || m.Name != "GPT-5.6 Sol" || m.API.In != 5 {
			t.Errorf("model = %+v, want the gpt-5.6-sol entry", m)
		}
	}
}

func TestApplyFirstEntryByIDWithoutCanonicalPart(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Pin["openai/gpt-x"] = "extra"
	b := pm("openai", "gpt-x-b", "B", "openai/gpt-x", "2026-08-01", 1, 1)
	a := pm("openai", "gpt-x-a", "A", "openai/gpt-x", "2026-08-01", 2, 2)
	got, _ := Apply(c, Sources{ModelsDev: []ProviderModel{b, a}})
	if m := got.Models["openai/gpt-x"]; m.ID != "gpt-x-a" {
		t.Errorf("model = %+v, want the first entry by id", m)
	}
}

func TestApplyStaleAndMissing(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Pin["openai/gone"] = "versatile"
	c.Pin["openai/never-seen"] = "versatile"
	c.Models["openai/gone"] = Model{Name: "Gone", Provider: "openai", ID: "gone", API: &Price{In: 1, Out: 2}}
	got, rep := Apply(c, Sources{Today: "2026-11-06"})
	if diff := cmp.Diff([]string{"anthropic/claude-opus-5-5", "openai/never-seen"}, rep.Missing); diff != "" {
		t.Errorf("missing (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"openai/gone"}, rep.Stale); diff != "" {
		t.Errorf("stale (-want +got):\n%s", diff)
	}
	if got.Models["openai/gone"].Name != "Gone" {
		t.Errorf("stale entry not kept: %+v", got.Models)
	}
	if _, ok := got.Models["openai/never-seen"]; ok {
		t.Error("missing id must not appear in models")
	}
}

func TestApplyKeepsSyncedAtWhenDataIsUnchanged(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	src := Sources{Today: "2026-11-06", ModelsDev: []ProviderModel{
		pm("anthropic", "claude-opus-5-5", "Claude Opus 5.5", "anthropic/claude-opus-5-5", "2026-09-22", 4, 20),
	}}
	first, _ := Apply(c, src)
	if first.SyncedAt != "2026-11-06" {
		t.Fatalf("first sync SyncedAt = %q, want today", first.SyncedAt)
	}

	src.Today = "2026-11-13"
	same, _ := Apply(first, src)
	if same.SyncedAt != "2026-11-06" {
		t.Errorf("same data: SyncedAt = %q, want the old date", same.SyncedAt)
	}

	src.ModelsDev = []ProviderModel{
		pm("anthropic", "claude-opus-5-5", "Claude Opus 5.5", "anthropic/claude-opus-5-5", "2026-09-22", 5, 25),
	}
	changed, _ := Apply(first, src)
	if changed.SyncedAt != "2026-11-13" {
		t.Errorf("changed price: SyncedAt = %q, want today", changed.SyncedAt)
	}
}

func TestApplyAmbiguousKeyMatchesNoModel(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Providers = append(c.Providers, "xai")
	c.Aliases.Arena = map[string]string{"Grok 4.20 (Aliased)": "xai/grok-4.20-0309-reasoning"}
	src := arenaSrc(
		ArenaRow{Board: ArenaBoard, Category: "overall", ModelName: "Grok 4.20 (Reasoning)", Org: "xai", Rank: 3, PublishDate: "d"},
		ArenaRow{Board: ArenaBoard, Category: "overall", ModelName: "Grok 4.20 (Aliased)", Org: "xai", Rank: 4, PublishDate: "d"},
	)
	src.Epoch = []EpochRow{{Model: "Grok 4.20", Benchmark: "eci", Score: 150}}
	src.ModelsDev = []ProviderModel{
		pm("xai", "grok-4.20-0309-reasoning", "Grok 4.20 (Reasoning)", "xai/grok-4.20-0309-reasoning", "2026-03-09", 2, 6),
		pm("xai", "grok-4.20-0309-non-reasoning", "Grok 4.20 (Non-Reasoning)", "xai/grok-4.20-0309-non-reasoning", "2026-03-09", 2, 6),
	}
	got, rep := Apply(c, src)

	re := got.Models["xai/grok-4.20-0309-reasoning"]
	non := got.Models["xai/grok-4.20-0309-non-reasoning"]
	if re.Arena["text"].Rank != 4 {
		t.Errorf("reasoning arena = %+v, want only the aliased row (rank 4)", re.Arena)
	}
	if len(non.Arena) != 0 || non.External.ECI != 0 || re.External.ECI != 0 {
		t.Errorf("non-reasoning = %+v, reasoning ECI = %v: ambiguous key must match none", non, re.External.ECI)
	}
	want := []string{
		"arena: Grok 4.20 (ambiguous: xai/grok-4.20-0309-non-reasoning, xai/grok-4.20-0309-reasoning)",
		"epoch: Grok 4.20 (ambiguous: xai/grok-4.20-0309-non-reasoning, xai/grok-4.20-0309-reasoning)",
	}
	if diff := cmp.Diff(want, rep.Unmatched); diff != "" {
		t.Errorf("unmatched (-want +got):\n%s", diff)
	}
}

func arenaSrc(rows ...ArenaRow) Sources {
	return Sources{
		Today: "2026-11-06",
		ModelsDev: []ProviderModel{
			pm("anthropic", "claude-opus-5-5", "Claude Opus 5.5", "anthropic/claude-opus-5-5", "2026-09-22", 4, 20),
			pm("openai", "gpt-6-1-sol", "GPT-6.1 Sol", "openai/gpt-6.1-sol", "2026-09-29", 5, 30),
		},
		Arena: rows,
	}
}

func TestApplyArena(t *testing.T) {
	t.Parallel()
	text := func(name, cat string, rank int, rating float64, date string) ArenaRow {
		return ArenaRow{
			Board: ArenaBoard, Category: cat, ModelName: name, Org: "anthropic",
			Rank: rank, Rating: rating, Votes: 100, PublishDate: date,
		}
	}
	src := arenaSrc(
		text("Claude Opus 5.5 (Max)", "overall", 5, 1490, "2026-11-03"),
		text("Claude Opus 5.5 (High)", "overall", 3, 1508, "2026-11-04"),
		text("Claude Opus 5.5", "coding", 7, 1400, "2026-11-04"),
		text("Claude Opus 5.5 (High)", "hard_prompts", 2, 1450, "2026-11-04"),
		text("Claude Opus 5.5 (High)", "languages", 1, 1, "2026-11-04"),
		ArenaRow{Board: "webdev", Category: "overall", ModelName: "Claude Opus 5.5 (Max)", Org: "anthropic", Rank: 4, Rating: 1300, PublishDate: "2026-12-01"},
		ArenaRow{Board: "agent", Category: "overall", ModelName: "Claude Opus 5.5 (Max)", Org: "anthropic", Rank: 1, Rating: 0.2, Votes: 14169, PublishDate: "2026-11-04"},
	)
	c := baseCatalog()
	c.Sources.Arena.PublishDate = "old"
	got, rep := Apply(c, src)
	want := map[string]Rank{
		"text":        {Rank: 3, Rating: 1508, Votes: 100, Variant: "High"},
		"coding":      {Rank: 7, Rating: 1400, Votes: 100},
		"hardPrompts": {Rank: 2, Rating: 1450, Votes: 100, Variant: "High"},
		"webdev":      {Rank: 4, Rating: 1300, Variant: "Max"},
		"agent":       {Rank: 1, Rating: 0.2, Votes: 14169, Variant: "Max"},
	}
	if diff := cmp.Diff(want, got.Models["anthropic/claude-opus-5-5"].Arena); diff != "" {
		t.Errorf("arena (-want +got):\n%s", diff)
	}
	if got.Sources.Arena != (ArenaInfo{Board: ArenaBoard, PublishDate: "2026-11-04"}) {
		t.Errorf("sources = %+v", got.Sources.Arena)
	}
	if len(rep.Unmatched) != 0 {
		t.Errorf("unmatched = %v", rep.Unmatched)
	}
}

func TestApplyArenaPublishDateKeptWithoutTextRows(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Sources.Arena.PublishDate = "2026-10-01"
	got, _ := Apply(c, arenaSrc())
	if got.Sources.Arena != (ArenaInfo{Board: ArenaBoard, PublishDate: "2026-10-01"}) {
		t.Errorf("sources = %+v", got.Sources.Arena)
	}
}

func TestApplyArenaAliasWinsOverKey(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Aliases.Arena = map[string]string{"Claude Opus 5.5 (High)": "openai/gpt-6.1-sol"}
	src := arenaSrc(ArenaRow{
		Board: ArenaBoard, Category: "overall", ModelName: "Claude Opus 5.5 (High)", Org: "anthropic",
		Rank: 1, Rating: 1500, PublishDate: "2026-11-04",
	})
	got, _ := Apply(c, src)
	if len(got.Models["anthropic/claude-opus-5-5"].Arena) != 0 {
		t.Errorf("key match must lose to the alias: %+v", got.Models["anthropic/claude-opus-5-5"].Arena)
	}
	if r := got.Models["openai/gpt-6.1-sol"].Arena["text"]; r.Rank != 1 || r.Variant != "High" {
		t.Errorf("alias target rank = %+v", r)
	}
}

func TestApplyArenaMatchesByNameAndAPIID(t *testing.T) {
	t.Parallel()
	src := arenaSrc(
		ArenaRow{Board: ArenaBoard, Category: "overall", ModelName: "GPT-6.1 Sol (Max)", Org: "openai", Rank: 2, Rating: 1, PublishDate: "d"},
		ArenaRow{Board: ArenaBoard, Category: "math", ModelName: "gpt-6-1-sol", Org: "openai", Rank: 4, Rating: 1, PublishDate: "d"},
	)
	got, _ := Apply(baseCatalog(), src)
	arena := got.Models["openai/gpt-6.1-sol"].Arena
	if arena["text"].Rank != 2 || arena["math"].Rank != 4 {
		t.Errorf("arena = %+v", arena)
	}
}

func TestApplyUnmatchedLimitedToTrackedOrgs(t *testing.T) {
	t.Parallel()
	row := func(name, org string) ArenaRow {
		return ArenaRow{Board: ArenaBoard, Category: "overall", ModelName: name, Org: org, Rank: 1, PublishDate: "d"}
	}
	src := arenaSrc(
		row("Claude Mystery (High)", "Anthropic"),
		row("Claude Mystery (Max)", "anthropic"),
		row("Other Lab Model", "otherlab"),
		ArenaRow{Board: ArenaBoard, Category: "languages", ModelName: "Claude Skipped", Org: "anthropic"},
		ArenaRow{Board: ArenaBoard, Category: "overall", ModelName: "Claude Legacy", Org: "anthropic", Rank: unmatchedTopRank + 1},
	)
	src.Epoch = []EpochRow{
		{Benchmark: "eci", Model: "Claude Mystery", Score: 1},
		{Benchmark: "terminalBench", Model: "claude-mystery_max", Score: 1},
		{Benchmark: "eci", Model: "Qwen 9", Score: 1},
		{Benchmark: "eci", Model: "GPT-9 Zeta", Score: 1},
	}
	_, rep := Apply(baseCatalog(), src)
	want := []string{"arena: Claude Mystery"} // epoch rows are never reported, low ranks neither
	if diff := cmp.Diff(want, rep.Unmatched); diff != "" {
		t.Errorf("unmatched (-want +got):\n%s", diff)
	}
}

func TestApplyEpochReportsAmbiguousNamesOnce(t *testing.T) {
	t.Parallel()
	grok := []ProviderModel{
		pm("xai", "grok-4.20-0309-reasoning", "Grok 4.20 (Reasoning)", "xai/grok-4.20-0309-reasoning", "2026-03-09", 2, 6),
		pm("xai", "grok-4.20-0309-non-reasoning", "Grok 4.20 (Non-Reasoning)", "xai/grok-4.20-0309-non-reasoning", "2026-03-09", 2, 6),
	}
	rows := []EpochRow{
		{Model: "Grok 4.20", Benchmark: "eci", Score: 150},
		{Model: "Grok 4.20", Benchmark: "osworld", Score: 0.3},
		{Model: "Grok 4.20", Benchmark: "terminalBench", Score: 0.5},
		{Model: "Claude Opus 5.5", Benchmark: "eci", Score: 168},
		{Model: "Legacy Model 1", Benchmark: "eci", Score: 90},
	}
	tests := []struct {
		name  string
		alias map[string]string
		want  []string
	}{
		{"ambiguous name once across rows", nil, []string{
			"epoch: Grok 4.20 (ambiguous: xai/grok-4.20-0309-non-reasoning, xai/grok-4.20-0309-reasoning)",
		}},
		{"alias resolves it", map[string]string{"Grok 4.20": "xai/grok-4.20-0309-reasoning"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := baseCatalog()
			c.Providers = append(c.Providers, "xai")
			c.Aliases.Epoch = tt.alias
			src := arenaSrc()
			src.ModelsDev = append(src.ModelsDev, grok...)
			src.Epoch = rows
			_, rep := Apply(c, src)
			if diff := cmp.Diff(tt.want, rep.Unmatched); diff != "" {
				t.Errorf("unmatched (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApplyExternal(t *testing.T) {
	t.Parallel()
	old := External{ECI: 100, OSWorld: 0.1}
	c := baseCatalog()
	c.Models["anthropic/claude-opus-5-5"] = Model{Name: "Claude Opus 5.5", External: old}
	c.Aliases.Epoch = map[string]string{"weird-name": "openai/gpt-6.1-sol"}

	src := arenaSrc()
	got, _ := Apply(c, src)
	if got.Models["anthropic/claude-opus-5-5"].External != (External{}) {
		t.Errorf("external is rebuilt from epoch, got %+v", got.Models["anthropic/claude-opus-5-5"].External)
	}

	src.Epoch = []EpochRow{
		{Benchmark: "eci", Model: "Claude Opus 5.5", Score: 168},
		{Benchmark: "osworld", Model: "claude-opus-5-5_max", Score: 0.31},
		{Benchmark: "osworld", Model: "claude-opus-5-5_high", Score: 0.29},
		{Benchmark: "terminalBench", Model: "weird-name", Score: 0.8},
	}
	got, _ = Apply(c, src)
	want := External{ECI: 168, OSWorld: 0.31}
	if diff := cmp.Diff(want, got.Models["anthropic/claude-opus-5-5"].External); diff != "" {
		t.Errorf("opus external (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(External{TerminalBench: 0.8}, got.Models["openai/gpt-6.1-sol"].External); diff != "" {
		t.Errorf("alias external (-want +got):\n%s", diff)
	}
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Models["anthropic/claude-opus-5-5"] = Model{
		Name:     "Old",
		ID:       "old",
		API:      &Price{In: 1},
		Arena:    map[string]Rank{"text": {Rank: 9}},
		External: External{ECI: 1},
	}
	c.Models["openai/gone"] = Model{Name: "Gone", Arena: map[string]Rank{"text": {Rank: 8}}}
	c.Pin["openai/gone"] = "versatile"
	want := Catalog{
		Schema: c.Schema, Since: c.Since, Providers: c.Providers,
		Pin:    map[string]string{"anthropic/claude-opus-5-5": "powerful", "openai/gone": "versatile"},
		Ignore: []string{"openai/gpt-realtime-2.1"},
		Models: map[string]Model{
			"anthropic/claude-opus-5-5": {
				Name:     "Old",
				ID:       "old",
				API:      &Price{In: 1},
				Arena:    map[string]Rank{"text": {Rank: 9}},
				External: External{ECI: 1},
			},
			"openai/gone": {Name: "Gone", Arena: map[string]Rank{"text": {Rank: 8}}},
		},
	}
	src := arenaSrc(ArenaRow{Board: ArenaBoard, Category: "overall", ModelName: "Claude Opus 5.5", Org: "anthropic", Rank: 1, PublishDate: "d"})
	src.Epoch = []EpochRow{{Benchmark: "eci", Model: "Claude Opus 5.5", Score: 5}}
	Apply(c, src)
	if diff := cmp.Diff(want, c); diff != "" {
		t.Errorf("input changed (-want +got):\n%s", diff)
	}
}

func TestApplyKeepsManualKeys(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Aliases = Aliases{Arena: map[string]string{"a": "b"}, Epoch: map[string]string{"c": "d"}}
	got, _ := Apply(c, arenaSrc())
	if diff := cmp.Diff(c.Pin, got.Pin); diff != "" {
		t.Errorf("pin: %s", diff)
	}
	if diff := cmp.Diff(c.Ignore, got.Ignore); diff != "" {
		t.Errorf("ignore: %s", diff)
	}
	if diff := cmp.Diff(c.Aliases, got.Aliases); diff != "" {
		t.Errorf("aliases: %s", diff)
	}
	if got.Since != c.Since {
		t.Errorf("since = %q", got.Since)
	}
}

func TestApplyReportsDanglingAliases(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Aliases = Aliases{Epoch: map[string]string{"Typo Model": "openai/does-not-exist"}}
	_, rep := Apply(c, arenaSrc())
	want := []string{"alias epoch: Typo Model -> openai/does-not-exist (not in models)"}
	if diff := cmp.Diff(want, rep.Unmatched); diff != "" {
		t.Errorf("unmatched (-want +got):\n%s", diff)
	}
}
