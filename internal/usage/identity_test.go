package usage

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestParseAnthropicProfile(t *testing.T) {
	t.Parallel()
	const tier = "default_claude_max_5x"
	prof := func(typ string) string {
		return `{"account":{"uuid":"acc-1","email":"a@example.com","display_name":"A"},"organization":{"uuid":"org-1","organization_type":"` + typ + `","rate_limit_tier":"` + tier + `"}}`
	}
	tests := []struct {
		name     string
		body     string
		wantPlan Plan
		wantErr  bool
	}{
		{name: "max", body: prof("claude_max"), wantPlan: AnthropicPlan("max", tier)},
		{name: "pro", body: prof("claude_pro"), wantPlan: AnthropicPlan("pro", tier)},
		{name: "team", body: prof("claude_team"), wantPlan: AnthropicPlan("team", tier)},
		{name: "enterprise", body: prof("claude_enterprise"), wantPlan: AnthropicPlan("enterprise", tier)},
		{name: "other type", body: prof("something")},
		{name: "empty type", body: prof("")},
		{name: "no uuid", body: `{"account":{"email":"a@example.com"}}`, wantErr: true},
		{name: "malformed", body: `{`, wantErr: true},
		{name: "wrong shape", body: `[]`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAnthropicProfile([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				if strings.Contains(err.Error(), "a@example.com") {
					t.Errorf("error leaks body: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := Profile{AccountID: "acc-1", Email: "a@example.com", OrgID: "org-1", Plan: tt.wantPlan}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestParseGitHubUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		want    Profile
		wantErr bool
	}{
		{name: "ok", body: `{"login":"octo","id":1}`, want: Profile{Login: "octo"}},
		{name: "empty login", body: `{"login":"","id":1}`, wantErr: true},
		{name: "missing", body: `{"id":1}`, wantErr: true},
		{name: "malformed", body: `nope`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseGitHubUser([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func FuzzParseAnthropicProfile(f *testing.F) {
	f.Add([]byte(`{"account":{"uuid":"u","email":"a@example.com","display_name":"A"},"organization":{"uuid":"o","organization_type":"claude_pro","rate_limit_tier":"t"}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"account":null}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ParseAnthropicProfile(b)
		if err == nil && p.AccountID == "" {
			t.Errorf("success with empty AccountID: %q", b)
		}
	})
}

func FuzzParseGitHubUser(f *testing.F) {
	f.Add([]byte(`{"login":"octo","id":1}`))
	f.Add([]byte(`{"login":""}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ParseGitHubUser(b)
		if err == nil && p.Login == "" {
			t.Errorf("success with empty Login: %q", b)
		}
	})
}

func TestCopilotLimitEntitlement(t *testing.T) {
	t.Parallel()
	reset := utc("2026-12-01T00:00:00Z")
	tests := []struct {
		name string
		raw  string
		want Limit
	}{
		{
			name: "zero entitlement",
			raw:  `{"entitlement":0,"remaining":0,"percent_remaining":0,"overage_permitted":true}`,
			want: Limit{Name: "premium", Kind: LimitAllowance, Scope: ScopeAccount, Overage: new(true), Note: "none included"},
		},
		{
			name: "negative entitlement is unlimited",
			raw:  `{"entitlement":-1,"remaining":-1,"percent_remaining":100}`,
			want: Limit{Name: "premium", Kind: LimitAllowance, Scope: ScopeAccount, ResetsAt: reset, Unlimited: true},
		},
		{
			name: "normal",
			raw:  `{"entitlement":100,"remaining":31,"percent_remaining":31}`,
			want: Limit{
				Name: "premium", Kind: LimitAllowance, Scope: ScopeAccount, ResetsAt: reset, UsedPct: new(69.0),
				Cap: &Amount{100, "requests"}, Remaining: &Amount{31, "requests"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := copilotLimit(copilotPremium, []byte(tt.raw), ScopeAccount, reset)
			if !ok {
				t.Fatal("omitted")
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
	t.Run("negative non-premium omitted", func(t *testing.T) {
		t.Parallel()
		if _, ok := copilotLimit("chat", []byte(`{"entitlement":-1}`), ScopeAccount, reset); ok {
			t.Error("want omitted")
		}
	})
}

func TestCacheIdentityRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	with := Snapshot{Provider: "anthropic", Account: "a", IdentityUnknown: true, Fingerprints: []string{"abc123"}, FetchedAt: now}
	without := Snapshot{Provider: "anthropic", Account: "b", FetchedAt: now}
	b, err := MarshalCache([]Snapshot{with, without}, now)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCache(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Snapshots) != 2 {
		t.Fatalf("got %d snapshots", len(c.Snapshots))
	}
	if !c.Snapshots[0].IdentityUnknown || !cmp.Equal(c.Snapshots[0].Fingerprints, []string{"abc123"}) {
		t.Errorf("identity lost: %+v", c.Snapshots[0])
	}
	if c.Snapshots[1].IdentityUnknown || c.Snapshots[1].Fingerprints != nil {
		t.Errorf("unexpected identity: %+v", c.Snapshots[1])
	}
	only, err := MarshalCache([]Snapshot{without}, now)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(only); strings.Contains(s, "identityUnknown") || strings.Contains(s, "fingerprints") {
		t.Errorf("keys not omitted:\n%s", s)
	}
}

func TestParseOpenAIIdentity(t *testing.T) {
	t.Parallel()
	plus, err := os.ReadFile("testdata/openai_plus.json")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		body    string
		want    Profile
		wantErr bool
	}{
		{
			name: "full", body: `{"plan_type":"plus","account_id":"acct-1","user_id":"user-1","rate_limit":{}}`,
			want: Profile{AccountID: "acct-1", UserID: "user-1", Plan: OpenAIPlan("plus")},
		},
		{
			name: "unknown plan", body: `{"plan_type":"mystery","account_id":"acct-1","user_id":"user-1"}`,
			want: Profile{AccountID: "acct-1", UserID: "user-1"},
		},
		{name: "no user id", body: `{"plan_type":"plus","account_id":"acct-1"}`, wantErr: true},
		{name: "no account id", body: `{"plan_type":"plus","user_id":"user-1"}`, wantErr: true},
		{name: "malformed", body: `{"account_id":"secret-body`, wantErr: true},
		{name: "fixture without ids", body: string(plus), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseOpenAIIdentity([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				if strings.Contains(err.Error(), "secret-body") {
					t.Errorf("error leaks the body: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("profile mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
