package doctor

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/catalog"
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/usage"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func model(provider, id, name, date string, in, out float64, textRank int) catalog.Model {
	m := catalog.Model{Name: name, Provider: provider, ID: id, ReleaseDate: date}
	if in > 0 {
		m.API = &catalog.Price{In: in, Out: out}
	}
	if textRank > 0 {
		m.Arena = map[string]catalog.Rank{"text": {Rank: textRank}}
	}
	return m
}

func fixtureCatalog() catalog.Catalog {
	models := map[string]catalog.Model{
		"anthropic/claude-haiku-4-5": model("anthropic", "claude-haiku-4-5", "Claude Haiku 4.5", "2025-10-01", 1, 5, 20),
		"anthropic/claude-haiku-5":   model("anthropic", "claude-haiku-5", "Claude Haiku 5", "2026-06-01", 1, 5, 10),
		"google/gemini-flash":        model("google", "gemini-flash", "Gemini Flash", "2026-03-01", 0.1, 0.5, 12),
		"openai/gpt-mini":            model("openai", "gpt-mini", "GPT Mini", "2026-01-01", 0.4, 1.6, 25),
		"anthropic/claude-opus-4-5":  model("anthropic", "claude-opus-4-5", "Claude Opus 4.5", "2025-11-01", 5, 25, 8),
		"openai/gpt-6":               model("openai", "gpt-6", "GPT 6", "2026-08-01", 5, 25, 4),
		"anthropic/claude-nameless":  model("anthropic", "claude-nameless", "Claude Nameless", "2026-01-01", 0, 0, 0),
	}
	return catalog.Catalog{
		Schema:    catalog.Schema,
		SyncedAt:  "2026-11-01",
		Providers: []string{"anthropic", "google", "openai", "xai"},
		Models:    models,
		Tiers:     catalog.ComputeTiers(models, nil),
	}
}

func window(used float64) usage.Limit {
	r := now.Add(2 * time.Hour)
	return usage.Limit{Name: "5h", Kind: usage.LimitWindow, UsedPct: new(used), ResetsAt: &r}
}

var (
	flatPlan     = usage.Plan{Kind: usage.KindIndividual, Billing: usage.BillingFlat, Payer: usage.PayerSelf, Name: "Max"}
	meteredPlan  = usage.Plan{Kind: usage.KindAPI, Billing: usage.BillingMetered, Payer: usage.PayerSelf}
	orgPlan      = usage.Plan{Kind: usage.KindTeam, Billing: usage.BillingFlat, Payer: usage.PayerOrg}
	unknownPlan  = usage.Plan{Kind: usage.KindUnknown, Billing: usage.BillingUnknown, Payer: usage.PayerUnknown}
	scoutHaiku45 = Setting{Harness: "claude", Tier: "scout", Model: "claude-haiku-4-5", Key: "models.tiers.scout.claude"}
)

func snap(provider string, plan usage.Plan, harnesses []string, limits ...usage.Limit) usage.Snapshot {
	return usage.Snapshot{
		Provider: provider, Account: "me@example.com", Harnesses: harnesses,
		Plan: plan, Limits: limits, Status: usage.StatusOK, FetchedAt: now,
	}
}

func kinds(fs []Finding) []Kind {
	out := make([]Kind, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Kind)
	}
	return out
}

func only(fs []Finding, k Kind) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Kind == k {
			out = append(out, f)
		}
	}
	return out
}

func texts(fs []Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Text)
	}
	return out
}

func TestSettings(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Models: config.Models{
		Default: "worker",
		Tiers: map[string]config.Tier{
			"scout":  {Claude: "claude-haiku-4-5", Codex: "gpt-mini"},
			"worker": {Claude: "claude-haiku-5", VSCode: "gpt-6", Pi: "gemini-flash"},
			"driver": {Claude: "gpt-6", Copilot: "claude-opus-4-5", OpenCode: "openai/gpt-mini"},
		},
	}}
	tier := func(h, name, model string) Setting {
		return Setting{Harness: h, Tier: name, Model: model, Key: "models.tiers." + name + "." + h}
	}
	tests := []struct {
		name      string
		cfg       config.Config
		harnesses []string
		want      []Setting
	}{
		{
			name: "default tier first, then by name; unknown harness ignored", cfg: cfg, harnesses: []string{"vscode", "copilot", "claude"},
			want: []Setting{
				tier("claude", "worker", "claude-haiku-5"), tier("claude", "driver", "gpt-6"), tier("claude", "scout", "claude-haiku-4-5"),
				tier("copilot", "driver", "claude-opus-4-5"),
			},
		},
		{
			name: "harness display order", cfg: cfg, harnesses: []string{"pi", "opencode", "codex"},
			want: []Setting{
				tier("codex", "scout", "gpt-mini"),
				tier("opencode", "driver", "openai/gpt-mini"),
				tier("pi", "worker", "gemini-flash"),
			},
		},
		{name: "empty config", cfg: config.Config{}, harnesses: []string{"codex", "claude", "antigravity"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, Settings(tt.cfg, tt.harnesses)); diff != "" {
				t.Errorf("Settings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	c := catalog.Catalog{Models: map[string]catalog.Model{
		"anthropic/claude-opus-4-5": {ID: "claude-opus-4-5", Provider: "anthropic"},
		"openai/gpt-6":              {ID: "gpt-6", Provider: "openai"},
		"azure/gpt-6":               {ID: "gpt-6", Provider: "azure"},
	}}
	tests := []struct {
		name, model, want string
		ok                bool
	}{
		{"exact", "openai/gpt-6", "openai/gpt-6", true},
		{"prefixed", "anthropic/claude-opus-4.5", "anthropic/claude-opus-4-5", true},
		{"prefixed wrong provider", "openai/claude-opus-4-5", "", false},
		{"bare normalised", "claude-opus-4.5", "anthropic/claude-opus-4-5", true},
		{"bare sorted first", "gpt-6", "azure/gpt-6", true},
		{"missing", "claude-ghost", "", false},
		{"missing prefixed", "openai/claude-ghost", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resolve(c, tt.model)
			if got != tt.want || ok != tt.ok {
				t.Errorf("resolve(%q) = %q, %v; want %q, %v", tt.model, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCopilotProviders(t *testing.T) {
	t.Parallel()
	if diff := cmp.Diff([]string{"anthropic", "google", "openai", "xai"}, CopilotProviders()); diff != "" {
		t.Errorf("CopilotProviders mismatch:\n%s", diff)
	}
}

func TestGlobalFindings(t *testing.T) {
	t.Parallel()
	c := fixtureCatalog()
	key := func(role string) string { return "models.tiers." + role + ".claude" }
	set := func(role, m string) Setting { return Setting{Harness: "claude", Tier: role, Model: m, Key: key(role)} }

	t.Run("unknown model once per key and model", func(t *testing.T) {
		t.Parallel()
		in := Input{Catalog: c, Now: now, Settings: []Setting{
			set("scout", "claude-ghost"),
			{Harness: "codex", Tier: "scout", Model: "claude-ghost", Key: key("scout")},
			{Harness: "pi", Tier: "default", Model: "ghost", Key: "models.tiers.default.pi"},
		}}
		got := Check(in)
		want := []Finding{
			{
				Level: LevelWarn, Kind: KindUnknownModel, Tier: "scout",
				Text: "claude-ghost (models.tiers.scout.claude) is not in the model catalog: retired, renamed or not tracked",
				Fix:  "pick a tracked model from agents models",
			},
			{
				Level: LevelWarn, Kind: KindUnknownModel, Tier: "default",
				Text: "ghost (models.tiers.default.pi) is not in the model catalog: retired, renamed or not tracked",
				Fix:  "pick a tracked model from agents models",
			},
			{
				Level: LevelInfo, Kind: KindNoUsage,
				Text: "no usage data yet: account, capacity and model checks skipped", Fix: "agents usage",
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("untiered info", func(t *testing.T) {
		t.Parallel()
		in := Input{Catalog: c, Now: now, Settings: []Setting{
			set("worker", "claude-opus-4-5"), set("scout", "claude-nameless"), set("driver", "gpt-6"),
		}}
		got := only(Check(in), KindTierDrift)
		want := []Finding{{
			Level: LevelInfo, Kind: KindTierDrift, Tier: "scout",
			Text: "Claude Nameless (models.tiers.scout.claude) has no tier: no API price or no ranking",
		}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("no usage skips the rest", func(t *testing.T) {
		t.Parallel()
		in := Input{Catalog: c, Now: now, Harnesses: []string{"claude"}, Settings: []Setting{scoutHaiku45}}
		if diff := cmp.Diff([]Kind{KindNoUsage}, kinds(Check(in))); diff != "" {
			t.Errorf("kinds mismatch:\n%s", diff)
		}
	})

	t.Run("stale catalog first", func(t *testing.T) {
		t.Parallel()
		stale := c
		stale.SyncedAt = ""
		got := kinds(Check(Input{Catalog: stale, Now: now}))
		if diff := cmp.Diff([]Kind{KindStaleCatalog, KindNoUsage}, got); diff != "" {
			t.Errorf("kinds mismatch:\n%s", diff)
		}
	})
}

var claude = []string{"claude"}

func settingFor(h, role, m string) Setting {
	return Setting{Harness: h, Tier: role, Model: m, Key: "models.tiers." + role + ".claude"}
}

func check(h []string, snaps []usage.Snapshot, ss ...Setting) []Finding {
	return Check(Input{Catalog: fixtureCatalog(), Settings: ss, Harnesses: h, Snapshots: snaps, HaveUsage: true, Now: now})
}

func TestAccountFindings(t *testing.T) {
	t.Parallel()

	t.Run("claude with an openai model is silent", func(t *testing.T) {
		t.Parallel()
		got := check(claude, []usage.Snapshot{snap("anthropic", flatPlan, claude)}, settingFor("claude", "driver", "gpt-6"))
		if len(got) != 0 {
			t.Errorf("want no findings, got %v", texts(got))
		}
	})

	t.Run("no account", func(t *testing.T) {
		t.Parallel()
		got := check([]string{"copilot"}, []usage.Snapshot{snap("anthropic", flatPlan, claude)},
			settingFor("copilot", "scout", "claude-haiku-5"), settingFor("copilot", "worker", "claude-haiku-5"), settingFor("copilot", "driver", "gpt-6"))
		want := []Finding{{
			Level: LevelWarn, Kind: KindNoAccount, Harness: "copilot",
			Text: "no github-copilot account found for scout, worker, driver (claude-haiku-5, gpt-6)",
			Fix:  "log in to copilot, then agents usage",
		}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("not logged in is no account", func(t *testing.T) {
		t.Parallel()
		s := snap("anthropic", flatPlan, claude)
		s.Status = usage.StatusNotLoggedIn
		got := check(claude, []usage.Snapshot{s}, scoutHaiku45)
		if diff := cmp.Diff([]Kind{KindNoAccount}, kinds(got)); diff != "" {
			t.Errorf("kinds mismatch:\n%s", diff)
		}
		if want := "no anthropic account found for scout (claude-haiku-4-5)"; got[0].Text != want {
			t.Errorf("text = %q, want %q", got[0].Text, want)
		}
	})
}

func TestCapacityFindings(t *testing.T) {
	t.Parallel()

	t.Run("capacity warn", func(t *testing.T) {
		t.Parallel()
		got := check(claude, []usage.Snapshot{snap("anthropic", flatPlan, claude, window(95))})
		want := []Finding{{
			Level: LevelWarn, Kind: KindCapacity, Harness: "claude",
			Text: "anthropic 5h at 95%, resets in " + usage.HumanDuration(2*time.Hour),
		}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("headroom through another harness and on its own account", func(t *testing.T) {
		t.Parallel()
		hs := []string{"claude", "opencode"}
		snaps := []usage.Snapshot{
			snap("anthropic", flatPlan, hs, window(95)),
			snap("openai", flatPlan, []string{"opencode"}, window(20)),
		}
		got := only(check(hs, snaps), KindHeadroom)
		want := []Finding{
			{Level: LevelInfo, Kind: KindHeadroom, Harness: "claude", Text: "openai has 80% left through opencode"},
			{Level: LevelInfo, Kind: KindHeadroom, Harness: "opencode", Text: "opencode can use its openai account instead (80% left)"},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("no headroom hint when the other account is also low", func(t *testing.T) {
		t.Parallel()
		hs := []string{"claude", "codex"}
		snaps := []usage.Snapshot{
			snap("anthropic", flatPlan, []string{"claude"}, window(95)),
			snap("openai", flatPlan, []string{"codex"}, window(95)),
		}
		if got := only(check(hs, snaps), KindHeadroom); len(got) != 0 {
			t.Errorf("want none, got %v", texts(got))
		}
	})
}

func TestBetterModelFindings(t *testing.T) {
	t.Parallel()

	t.Run("flat picks the stronger model", func(t *testing.T) {
		t.Parallel()
		got := only(check(claude, []usage.Snapshot{snap("anthropic", flatPlan, claude)}, scoutHaiku45), KindBetterModel)
		want := []Finding{{
			Level: LevelSuggest, Kind: KindBetterModel, Harness: "claude", Tier: "scout",
			Text:      "scout: Claude Haiku 4.5 → Claude Haiku 5 (better-ranked; text #20 → #10; $1/$5 → $1/$5 per Mtok; within your flat plan)",
			Current:   &ModelRef{ID: "anthropic/claude-haiku-4-5", Name: "Claude Haiku 4.5", Tier: "lightweight", In: 1, Out: 5, Category: "text", Rank: 20},
			Suggested: &ModelRef{ID: "anthropic/claude-haiku-5", Name: "Claude Haiku 5", Tier: "lightweight", In: 1, Out: 5, Category: "text", Rank: 10},
			Reason:    "better-ranked", Billing: "within your flat plan",
		}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("flat with a nearly full plan makes no capacity claim", func(t *testing.T) {
		t.Parallel()
		got := only(check(claude, []usage.Snapshot{snap("anthropic", flatPlan, claude, window(95))}, scoutHaiku45), KindBetterModel)
		if len(got) != 1 || got[0].Billing != "" || strings.Contains(got[0].Text, "flat plan") {
			t.Errorf("want one suggestion without billing, got %+v", got)
		}
	})

	t.Run("metered and org prefer the cheaper model", func(t *testing.T) {
		t.Parallel()
		hs := []string{"opencode"}
		for _, tt := range []struct {
			name, billing string
			plan          usage.Plan
		}{
			{"metered", "metered billing", meteredPlan},
			{"org", "seat managed by org", orgPlan},
		} {
			snaps := []usage.Snapshot{snap("anthropic", tt.plan, hs), snap("google", tt.plan, hs)}
			got := only(check(hs, snaps, settingFor("opencode", "scout", "anthropic/claude-haiku-4-5")), KindBetterModel)
			want := []Finding{{
				Level: LevelSuggest, Kind: KindBetterModel, Harness: "opencode", Tier: "scout",
				Text:      "scout: Claude Haiku 4.5 → Gemini Flash (cheaper, ranked as well or better; text #20 → #12; $1/$5 → $0.1/$0.5 per Mtok; " + tt.billing + ")",
				Current:   &ModelRef{ID: "anthropic/claude-haiku-4-5", Name: "Claude Haiku 4.5", Tier: "lightweight", In: 1, Out: 5, Category: "text", Rank: 20},
				Suggested: &ModelRef{ID: "google/gemini-flash", Name: "Gemini Flash", Tier: "lightweight", In: 0.1, Out: 0.5, Category: "text", Rank: 12},
				Reason:    "cheaper, ranked as well or better", Billing: tt.billing,
			}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("%s mismatch (-want +got):\n%s", tt.name, diff)
			}
		}
	})

	t.Run("metered and org never fall back to a stronger model", func(t *testing.T) {
		t.Parallel()
		// Only anthropic is available, where Claude Haiku 5 ranks better at
		// the same price: flat suggests it, metered and org stay quiet.
		for _, plan := range []usage.Plan{meteredPlan, orgPlan} {
			got := only(check(claude, []usage.Snapshot{snap("anthropic", plan, claude)}, scoutHaiku45), KindBetterModel)
			if len(got) != 0 {
				t.Errorf("%+v: want no suggestion, got %v", plan, texts(got))
			}
		}
	})

	t.Run("ranking only has no billing claim", func(t *testing.T) {
		t.Parallel()
		got := only(check(claude, []usage.Snapshot{snap("anthropic", unknownPlan, claude)}, scoutHaiku45), KindBetterModel)
		if len(got) != 1 || got[0].Billing != "" || got[0].Reason != "better-ranked" || strings.Contains(got[0].Text, "; within") {
			t.Errorf("want one billing-free better-ranked suggestion, got %+v", got)
		}
	})
}

func TestBetterModelNone(t *testing.T) {
	t.Parallel()

	t.Run("nothing beats the current model", func(t *testing.T) {
		t.Parallel()
		best := settingFor("claude", "scout", "claude-haiku-5")
		got := check(claude, []usage.Snapshot{snap("anthropic", flatPlan, claude)}, best)
		if len(got) != 0 {
			t.Errorf("want none, got %v", texts(got))
		}
	})

	t.Run("a model is compared within its own catalog tier", func(t *testing.T) {
		t.Parallel()
		d := Setting{Harness: "codex", Tier: "default", Model: "gpt-mini", Key: "models.tiers.default.codex"}
		got := only(check([]string{"codex"}, []usage.Snapshot{snap("openai", flatPlan, []string{"codex"})}, d), KindBetterModel)
		if len(got) != 0 { // gpt-mini is the only lightweight openai model
			t.Errorf("want none, got %v", texts(got))
		}
	})

	t.Run("deterministic order", func(t *testing.T) {
		t.Parallel()
		hs := []string{"claude", "opencode"}
		snaps := []usage.Snapshot{
			snap("anthropic", flatPlan, hs, window(95)),
			snap("openai", flatPlan, []string{"opencode"}, window(20)),
		}
		ss := []Setting{
			scoutHaiku45, settingFor("claude", "worker", "claude-haiku-4-5"), settingFor("claude", "driver", "claude-opus-4-5"),
			settingFor("opencode", "scout", "claude-haiku-4-5"), settingFor("opencode", "default", "gpt-6"),
		}
		first := check(hs, snaps, ss...)
		for range 5 {
			if diff := cmp.Diff(first, check(hs, snaps, ss...)); diff != "" {
				t.Fatalf("non-deterministic output:\n%s", diff)
			}
		}
		// per harness findings follow Input.Harnesses order, then check order.
		seen := make([]string, 0, len(first))
		for _, f := range first {
			seen = append(seen, f.Harness+":"+string(f.Kind))
		}
		want := []string{
			"claude:capacity", "claude:headroom", "claude:better-model", "claude:better-model",
			"opencode:capacity", "opencode:headroom", "opencode:better-model",
		}
		if diff := cmp.Diff(want, seen); diff != "" {
			t.Errorf("order mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestBetterModelPayingAccount: billing comes from the account that would pay
// for the suggestion, not the current model's.
func TestBetterModelPayingAccount(t *testing.T) {
	t.Parallel()
	// opencode holds a flat anthropic plan and a metered openai key. The
	// only better-ranked model is a pricier openai one, paid by the
	// metered key: no suggestion, though the current model's plan is flat.
	c := fixtureCatalog()
	delete(c.Models, "anthropic/claude-haiku-5")
	c.Models["openai/gpt-pricey-mini"] = model("openai", "gpt-pricey-mini", "GPT Pricey Mini", "2026-09-01", 1.2, 6, 5)
	c.Tiers = catalog.ComputeTiers(c.Models, nil)
	if c.TierOf("openai/gpt-pricey-mini") != catalog.TierLightweight {
		t.Fatalf("fixture: gpt-pricey-mini in %q, want lightweight", c.TierOf("openai/gpt-pricey-mini"))
	}
	hs := []string{"opencode"}
	snaps := []usage.Snapshot{snap("anthropic", flatPlan, hs), snap("openai", meteredPlan, hs)}
	got := only(Check(Input{
		Catalog: c, Harnesses: hs, Snapshots: snaps, HaveUsage: true, Now: now,
		Settings: []Setting{settingFor("opencode", "scout", "anthropic/claude-haiku-4-5")},
	}), KindBetterModel)
	if len(got) != 0 {
		t.Errorf("want no suggestion, got %v", texts(got))
	}
}
