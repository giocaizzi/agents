package cli

import (
	"testing"
	"time"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

func switchAccount(id, prov, label string, kind usg.Kind) provider.Account {
	return provider.Account{
		Provider: prov, ID: id, Label: label, Harnesses: []string{id},
		Auth: provider.AuthOAuth, Plan: usg.Plan{Kind: kind}, IdentityKnown: true,
	}
}

func switchCached(id, prov, label string, kind usg.Kind) usg.Snapshot {
	return usg.Snapshot{
		ID: id, Provider: prov, Account: label, Harnesses: []string{id},
		Plan: usg.Plan{Kind: kind}, Status: usg.StatusOK, Source: "api",
		Limits: []usg.Limit{{Name: "5h", UsedPct: new(77.0)}},
	}
}

// TestCachedSnapshotMissesOnAccountOrAuthChange covers a cached snapshot whose
// id still matches while the account behind it changed.
func TestCachedSnapshotMissesOnAccountOrAuthChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		cached usg.Snapshot
		acct   provider.Account
		hit    bool
	}{
		{
			"codex alice to bob",
			switchCached("codex", provider.OpenAI, "alice@example.com", usg.KindIndividual),
			switchAccount("codex", provider.OpenAI, "bob@example.com", usg.KindIndividual), false,
		},
		{
			"codex oauth to api key",
			switchCached("codex", provider.OpenAI, "alice@example.com", usg.KindIndividual),
			switchAccount("codex", provider.OpenAI, "", usg.KindAPI), false,
		},
		{
			"claude oauth to api key, no labels",
			switchCached("claude", "anthropic", "", usg.KindIndividual),
			switchAccount("claude", "anthropic", "", usg.KindAPI), false,
		},
		{
			"claude oauth to cloud, no labels",
			switchCached("claude", "anthropic", "", usg.KindIndividual),
			switchAccount("claude", "anthropic", "", usg.KindCloud), false,
		},
		{
			"same account, same label",
			switchCached("codex", provider.OpenAI, "alice@example.com", usg.KindIndividual),
			switchAccount("codex", provider.OpenAI, "alice@example.com", usg.KindIndividual), true,
		},
		{
			"same unlabelled api account",
			switchCached("claude", "anthropic", "", usg.KindAPI),
			switchAccount("claude", "anthropic", "", usg.KindAPI), true,
		},
		{
			"snapshot without id never matches",
			func() usg.Snapshot {
				s := switchCached("codex", provider.OpenAI, "alice@example.com", usg.KindIndividual)
				s.ID = ""
				return s
			}(),
			switchAccount("codex", provider.OpenAI, "alice@example.com", usg.KindIndividual), false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := testFetcher(t, nil)
			failed := unavailable(f.base(tc.acct), "HTTP 500")
			got := keepStale([]usg.Snapshot{failed}, []usg.Snapshot{tc.cached})
			checkSwitchStale(t, got, tc.acct, tc.hit)
		})
	}
}

func checkSwitchStale(t *testing.T, got []usg.Snapshot, acct provider.Account, hit bool) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("keepStale rows = %d", len(got))
	}
	s := got[0]
	if hit {
		if s.Status != usg.StatusStale || len(s.Limits) != 1 {
			t.Errorf("keepStale dropped a valid snapshot: %+v", s)
		}
		return
	}
	if s.Status != usg.StatusUnavailable || len(s.Limits) != 0 || s.Account != acct.Label {
		t.Errorf("keepStale = %+v, want the failure snapshot for %q", s, acct.Label)
	}
}

// TestLiveFailureDoesNotRewriteOtherAccountIntoCache runs the failure
// path after Codex switched from alice to bob.
func TestLiveFailureDoesNotRewriteOtherAccountIntoCache(t *testing.T) {
	t.Parallel()
	srv := failServer()
	t.Cleanup(srv.Close)
	f := testFetcher(t, srv)
	bob := switchAccount("codex", provider.OpenAI, "bob@example.com", usg.KindIndividual)
	bob.Cred = provider.NewCredential(tokB, "acct-bob")
	cached := []usg.Snapshot{switchCached("codex", provider.OpenAI, "alice@example.com", usg.KindIndividual)}

	live := liveSnapshots(t.Context(), []provider.Account{bob}, f, time.Second)
	got := keepStale(live, cached)

	if len(got) != 1 || got[0].Account != "bob@example.com" || len(got[0].Limits) != 0 || got[0].Status != usg.StatusUnavailable {
		t.Errorf("rows = %+v, want bob's failure only", got)
	}
}
