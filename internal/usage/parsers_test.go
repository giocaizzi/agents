package usage

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func utc(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestAnthropicPlan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sub, tier string
		want      Plan
	}{
		{"pro", "", Plan{KindIndividual, BillingFlat, PayerSelf, "Pro", "pro"}},
		{"max", "default_claude_max_5x", Plan{KindIndividual, BillingFlat, PayerSelf, "Max 5x", "max"}},
		{"max", "default_claude_max_20x", Plan{KindIndividual, BillingFlat, PayerSelf, "Max 20x", "max"}},
		{"max", "other", Plan{KindIndividual, BillingFlat, PayerSelf, "Max", "max"}},
		{"team", "", Plan{KindTeam, BillingFlat, PayerOrg, "Team seat", "team"}},
		{"enterprise", "", Plan{KindEnterprise, BillingUnknown, PayerOrg, "Enterprise seat", "enterprise"}},
		{"", "", Plan{KindUnknown, BillingUnknown, PayerUnknown, "", ""}},
		{"zzz", "", Plan{KindUnknown, BillingUnknown, PayerUnknown, "", "zzz"}},
	}
	for _, tt := range tests {
		t.Run(tt.sub+"/"+tt.tier, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, AnthropicPlan(tt.sub, tt.tier)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestParseAnthropic(t *testing.T) {
	t.Parallel()
	maxPlan := AnthropicPlan("max", "default_claude_max_5x")
	team := AnthropicPlan("team", "")
	tests := []struct {
		name string
		body []byte
		plan Plan
		want []Limit
	}{
		{"max", fixture(t, "anthropic_max.json"), maxPlan, []Limit{
			{Name: "5h", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(31.0), ResetsAt: utc("2026-11-06T18:00:00.123456Z")},
			{Name: "7d", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(14.5), ResetsAt: utc("2026-11-09T09:00:00Z")},
			{Name: "7d cowork", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(7.0), ResetsAt: utc("2026-11-09T09:00:00Z")},
			{Name: "7d sonnet", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(2.0)},
			{
				Name: "extra usage", Kind: LimitSpend, Scope: ScopeAccount, UsedPct: new(25.01), Overage: new(true),
				Used: &Amount{1250.5, "credits"}, Cap: &Amount{5000, "credits"},
			},
		}},
		{"team null windows", fixture(t, "anthropic_team.json"), team, []Limit{
			{Name: "5h", Kind: LimitWindow, Scope: ScopeSeat, Note: noSeatWindow},
			{Name: "7d", Kind: LimitWindow, Scope: ScopeSeat, Note: noSeatWindow},
			{Name: "7d opus", Kind: LimitWindow, Scope: ScopeSeat, Note: noSeatWindow},
		}},
		{"individual null windows omitted", fixture(t, "anthropic_team.json"), maxPlan, nil},
		{"empty object", []byte(`{}`), maxPlan, nil},
		{"null body", []byte(`null`), maxPlan, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAnthropic(tt.body, tt.plan)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestMalformedJSON(t *testing.T) {
	t.Parallel()
	fns := map[string]func([]byte) error{
		"anthropic": func(b []byte) error { _, err := ParseAnthropic(b, Plan{}); return err },
		"openai":    func(b []byte) error { _, _, err := ParseOpenAI(b); return err },
		"copilot":   func(b []byte) error { _, _, err := ParseCopilotUser(b); return err },
		"billing":   func(b []byte) error { _, err := ParseCopilotBilling(b); return err },
		"agy":       func(b []byte) error { _, err := ParseAgyUsage(b); return err },
		"cache":     func(b []byte) error { _, err := ParseCache(b); return err },
	}
	for name, fn := range fns {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, b := range [][]byte{[]byte(`{"oops`), []byte(`[1,2]`), nil} {
				if err := fn(b); err == nil {
					t.Errorf("want error for %q", b)
				}
			}
		})
	}
}

func TestOpenAIPlan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in        string
		kind      Kind
		billing   Billing
		payer     Payer
		name      string
		checkName bool
	}{
		{"free", KindFree, BillingFlat, PayerSelf, "Free", true},
		{"go", KindIndividual, BillingFlat, PayerSelf, "Go", true},
		{"plus", KindIndividual, BillingFlat, PayerSelf, "Plus", true},
		{"pro", KindIndividual, BillingFlat, PayerSelf, "Pro", true},
		{"prolite", KindIndividual, BillingFlat, PayerSelf, "Prolite", true},
		{"promax", KindIndividual, BillingFlat, PayerSelf, "Promax", true},
		{"team", KindTeam, BillingFlat, PayerOrg, "Team seat", true},
		{"business", KindTeam, BillingFlat, PayerOrg, "Business seat", true},
		{"self_serve_business_prolite", KindTeam, BillingFlat, PayerOrg, "", false},
		{"self_serve_business_usage_based", KindTeam, BillingMetered, PayerOrg, "", false},
		{"team_usage_based", KindTeam, BillingMetered, PayerOrg, "", false},
		{"enterprise", KindEnterprise, BillingFlat, PayerOrg, "", false},
		{"ent26", KindEnterprise, BillingFlat, PayerOrg, "", false},
		{"enterprise_cbp_automation", KindEnterprise, BillingFlat, PayerOrg, "", false},
		{"enterprise_usage_based", KindEnterprise, BillingMetered, PayerOrg, "", false},
		{"edu", KindEnterprise, BillingFlat, PayerOrg, "", false},
		{"edu_plus", KindEnterprise, BillingFlat, PayerOrg, "", false},
		{"edu_pro", KindEnterprise, BillingFlat, PayerOrg, "", false},
		{"sci", KindEnterprise, BillingFlat, PayerOrg, "", false},
		{"sci_plus", KindEnterprise, BillingFlat, PayerOrg, "", false},
		{"Plus", KindIndividual, BillingFlat, PayerSelf, "Plus", true},
		{"mystery", KindUnknown, BillingUnknown, PayerUnknown, "", true},
		{"", KindUnknown, BillingUnknown, PayerUnknown, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got := OpenAIPlan(tt.in)
			if got.Kind != tt.kind || got.Billing != tt.billing || got.Payer != tt.payer || got.Raw != tt.in {
				t.Errorf("got %+v", got)
			}
			if tt.checkName && got.Name != tt.name {
				t.Errorf("name %q, want %q", got.Name, tt.name)
			}
		})
	}
}

func TestParseOpenAI(t *testing.T) {
	t.Parallel()
	t.Run("plus", func(t *testing.T) {
		t.Parallel()
		plan, got, err := ParseOpenAI(fixture(t, "openai_plus.json"))
		if err != nil {
			t.Fatal(err)
		}
		if plan.Name != "Plus" {
			t.Errorf("plan %+v", plan)
		}
		want := []Limit{
			{Name: "5h", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(33.0), ResetsAt: new(time.Unix(1793000000, 0).UTC())},
			{Name: "7d", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(5.5), ResetsAt: new(time.Unix(1793500000, 0).UTC())},
			{Name: "Codex Spark · 3h", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(1.0), ResetsAt: new(time.Unix(1793000100, 0).UTC())},
			{Name: "credits", Kind: LimitCredits, Scope: ScopeAccount, Remaining: &Amount{12.5, "credits"}},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("business metered, unlimited credits, hidden spend", func(t *testing.T) {
		t.Parallel()
		plan, got, err := ParseOpenAI(fixture(t, "openai_business.json"))
		if err != nil {
			t.Fatal(err)
		}
		if plan.Kind != KindTeam || plan.Billing != BillingMetered {
			t.Errorf("plan %+v", plan)
		}
		want := []Limit{
			{Name: "5h", Kind: LimitWindow, Scope: ScopeSeat, UsedPct: new(10.0), ResetsAt: new(time.Unix(1793000000, 0).UTC())},
			{Name: "credits", Kind: LimitCredits, Scope: ScopeSeat, Unlimited: true, Remaining: &Amount{0, "credits"}},
			{Name: "spend limit", Kind: LimitSpend, Scope: ScopeSeat, Hidden: true, Note: "managed by org"},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("unknown plan, nothing else", func(t *testing.T) {
		t.Parallel()
		plan, got, err := ParseOpenAI([]byte(`{"plan_type":"weird"}`))
		if err != nil || len(got) != 0 || plan.Kind != KindUnknown || plan.Raw != "weird" {
			t.Errorf("%+v %v %v", plan, got, err)
		}
	})
}

func TestParseCodexSession(t *testing.T) {
	t.Parallel()
	t.Run("last snapshot", func(t *testing.T) {
		t.Parallel()
		plan, got, at, err := ParseCodexSession(strings.NewReader(string(fixture(t, "codex_session.jsonl"))))
		if err != nil {
			t.Fatal(err)
		}
		if plan.Name != "Plus" {
			t.Errorf("plan %+v", plan)
		}
		want := []Limit{
			{Name: "5h", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(12.5), ResetsAt: new(time.Unix(1000, 0).UTC())},
			{Name: "7d", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(40.0), ResetsAt: new(time.Unix(1793500000, 0).UTC())},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Error(diff)
		}
		if !at.Equal(*utc("2026-11-05T11:00:00Z")) {
			t.Errorf("at %v", at)
		}
	})
	t.Run("long line and no trailing newline", func(t *testing.T) {
		t.Parallel()
		long := `{"type":"event_msg","payload":{"type":"agent_message","text":"` + strings.Repeat("x", 3<<20) + `"}}` + "\n" +
			`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"plan_type":"team","primary":{"used_percent":1,"window_minutes":300,"resets_at":2000000000}}}}`
		plan, got, at, err := ParseCodexSession(strings.NewReader(long))
		if err != nil || len(got) != 1 || plan.Kind != KindTeam || !at.IsZero() {
			t.Fatalf("%+v %+v %v %v", plan, got, at, err)
		}
		if got[0].Scope != ScopeSeat {
			t.Errorf("scope %v", got[0].Scope)
		}
	})
	t.Run("no snapshot", func(t *testing.T) {
		t.Parallel()
		for _, in := range []string{"", "not json\n", `{"type":"event_msg","payload":{"type":"token_count","rate_limits":null}}`} {
			if _, _, _, err := ParseCodexSession(strings.NewReader(in)); err == nil {
				t.Errorf("want error for %q", in)
			}
		}
	})
}

func TestCopilotPlan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		org  bool
		want Plan
	}{
		{"free", false, Plan{KindFree, BillingFlat, PayerSelf, "Free", "free"}},
		{"individual", false, Plan{KindIndividual, BillingMixed, PayerSelf, "Pro", "individual"}},
		{"pro", false, Plan{KindIndividual, BillingMixed, PayerSelf, "Pro", "pro"}},
		{"pro_plus", false, Plan{KindIndividual, BillingMixed, PayerSelf, "Pro+", "pro_plus"}},
		{"max", false, Plan{KindIndividual, BillingMixed, PayerSelf, "Max", "max"}},
		{"business", true, Plan{KindTeam, BillingMixed, PayerOrg, "Business seat", "business"}},
		{"enterprise", true, Plan{KindEnterprise, BillingMixed, PayerOrg, "Enterprise seat", "enterprise"}},
		{"pro", true, Plan{KindIndividual, BillingMixed, PayerOrg, "Pro", "pro"}},
		{"new_plan", false, Plan{KindUnknown, BillingUnknown, PayerUnknown, "", "new_plan"}},
		{"new_plan", true, Plan{KindUnknown, BillingUnknown, PayerOrg, "", "new_plan"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, CopilotPlan(tt.in, tt.org)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestParseCopilotUser(t *testing.T) {
	t.Parallel()
	t.Run("org seat", func(t *testing.T) {
		t.Parallel()
		plan, got, err := ParseCopilotUser(fixture(t, "copilot_user_org.json"))
		if err != nil {
			t.Fatal(err)
		}
		if plan.Payer != PayerOrg || plan.Kind != KindIndividual {
			t.Errorf("plan %+v", plan)
		}
		reset := utc("2026-12-01T00:00:00Z")
		want := []Limit{
			{
				Name: "premium", Kind: LimitAllowance, Scope: ScopePool, UsedPct: new(70.0), ResetsAt: reset, Overage: new(true),
				Cap: &Amount{300, "requests"}, Remaining: &Amount{90, "requests"},
			},
			{
				Name: "future quota", Kind: LimitAllowance, Scope: ScopePool, UsedPct: new(50.0), ResetsAt: reset,
				Cap: &Amount{10, "requests"}, Remaining: &Amount{5, "requests"},
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("personal chat limited", func(t *testing.T) {
		t.Parallel()
		_, got, err := ParseCopilotUser([]byte(`{"copilot_plan":"free","quota_snapshots":{"chat":{"entitlement":50,"remaining":40},"completions":{"unlimited":false,"entitlement":2000,"remaining":2000,"percent_remaining":100}}}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Name != "chat" || got[0].Scope != ScopeAccount || *got[0].UsedPct != 20 || got[1].Name != "completions" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("unlimited premium and bad date", func(t *testing.T) {
		t.Parallel()
		_, got, err := ParseCopilotUser([]byte(`{"quota_reset_date":"bad","quota_snapshots":{"premium_interactions":{"unlimited":true},"x":"y"}}`))
		if err != nil || len(got) != 1 || !got[0].Unlimited || got[0].ResetsAt != nil {
			t.Errorf("%+v %v", got, err)
		}
	})
}

func TestParseCopilotBilling(t *testing.T) {
	t.Parallel()
	got, err := ParseCopilotBilling(fixture(t, "copilot_billing.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := Limit{Name: "spend this month", Kind: LimitSpend, Scope: ScopeAccount, Used: &Amount{1.5, "USD"}, Note: "billed to you"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error(diff)
	}
	empty, err := ParseCopilotBilling([]byte(`{}`))
	if err != nil || empty.Used.Value != 0 {
		t.Errorf("%+v %v", empty, err)
	}
}

func TestParseAgyUsage(t *testing.T) {
	t.Parallel()
	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		got, err := ParseAgyUsage(fixture(t, "agy_usage.json"))
		if err != nil {
			t.Fatal(err)
		}
		want := []Limit{
			{Name: "Gemini · 7d", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(100.0), ResetsAt: utc("2026-11-07T21:48:39Z")},
			{Name: "Gemini · 5h", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(100 * (1 - 0.9688)), Disabled: true},
			{Name: "Claude and GPT · 7d", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(100 * (1 - 0.2337)), ResetsAt: utc("2026-11-08T12:05:47Z")},
			{Name: "Claude and GPT · 5h", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(0.0), ResetsAt: utc("2026-11-06T17:36:57Z")},
		}
		if diff := cmp.Diff(want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("rejected", func(t *testing.T) {
		t.Parallel()
		for name, body := range map[string]string{
			"wrong command": `{"status":"SUCCESS","num_turns":0,"command":{"name":"status"}}`,
			"model turn":    `{"status":"SUCCESS","num_turns":1,"command":{"name":"usage","data":{}}}`,
			"no command":    `{"status":"SUCCESS","num_turns":0}`,
			"failed":        `{"status":"ERROR","num_turns":0,"command":{"name":"usage"}}`,
		} {
			_, err := ParseAgyUsage([]byte(body))
			if err == nil || !strings.Contains(err.Error(), "agy did not answer /usage") {
				t.Errorf("%s: err %v", name, err)
			}
		}
	})
}

func TestParseAgyLog(t *testing.T) {
	t.Parallel()
	t.Run("last line", func(t *testing.T) {
		t.Parallel()
		start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		got, at, err := ParseAgyLog(strings.NewReader(string(fixture(t, "agy.log"))), start)
		if err != nil {
			t.Fatal(err)
		}
		wantAt := time.Date(2026, 11, 1, 13, 9, 11, 0, time.UTC)
		reset := wantAt.Add(4*time.Hour + 58*time.Minute + 35*time.Second)
		want := []Limit{{Name: "quota", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(100.0), ResetsAt: &reset, Note: "quota reached (from agy log)"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Error(diff)
		}
		if !at.Equal(wantAt) {
			t.Errorf("at %v", at)
		}
	})
	t.Run("year rollover", func(t *testing.T) {
		t.Parallel()
		start := time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC)
		line := "E0101 00:30:00.000000    1 errorreport.go:2] RESOURCE_EXHAUSTED (code 429): quota. Resets in 33h5m2s.\n"
		got, at, err := ParseAgyLog(strings.NewReader(line), start)
		if err != nil || len(got) != 1 {
			t.Fatal(got, err)
		}
		if want := time.Date(2027, 1, 1, 0, 30, 0, 0, time.UTC); !at.Equal(want) {
			t.Errorf("at %v", at)
		}
		if want := time.Date(2027, 1, 2, 9, 35, 2, 0, time.UTC); !got[0].ResetsAt.Equal(want) {
			t.Errorf("reset %v", got[0].ResetsAt)
		}
	})
	t.Run("none", func(t *testing.T) {
		t.Parallel()
		got, at, err := ParseAgyLog(strings.NewReader("I1101 13:00:00.000000 1 a.go:1] hello\n"), time.Now())
		if err != nil || got != nil || !at.IsZero() {
			t.Errorf("%v %v %v", got, at, err)
		}
	})
}
