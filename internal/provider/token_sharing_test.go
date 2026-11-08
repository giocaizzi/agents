package provider

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/usage"
)

// sharingClaims are the claims of a token-sharing access token: the scope, an
// auth claim with no account or user id, and a salted sub.
func sharingClaims(exp time.Time, extra map[string]any) map[string]any {
	c := map[string]any{
		"exp": exp.Unix(),
		"sub": "salted-sub",
		openaiAuthClaim: map[string]any{
			"encrypted_auth_metadata": "fake-meta", "per_user_salt": "fake-salt",
		},
	}
	maps.Copy(c, extra)
	return c
}

func TestOpenAITokenSharingClassification(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	const scope = "chatgpt.tokens.use.direct"
	tests := []struct {
		name  string
		extra map[string]any
		want  bool
	}{
		{"scp array", map[string]any{"scp": []any{"openid", scope}}, true},
		{"scp string", map[string]any{"scp": "openid " + scope}, true},
		{"scope string", map[string]any{"scope": "openid " + scope + " email"}, true},
		{"scope array", map[string]any{"scope": []any{"openid", scope}}, true},
		{"scope array without it", map[string]any{"scope": []any{"openid", 7}}, false},
		{"other scopes", map[string]any{"scp": []any{"openid", "offline_access"}, "scope": "openid"}, false},
		{"scope as a substring only", map[string]any{"scope": "x" + scope}, false},
		{"no scope", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := openaiOAuth("codex", jwt(t, sharingClaims(exp, tt.extra)), "", "")
			if a.TokenSharing != tt.want {
				t.Fatalf("TokenSharing = %v, want %v", a.TokenSharing, tt.want)
			}
			if !tt.want {
				return
			}
			if !a.Cred.Empty() || a.Key != "" || len(a.Fingerprints) != 0 || a.IdentityKnown || a.Label != "" {
				t.Errorf("token-sharing account kept a credential or identity: %+v", views([]Account{a}))
			}
			if a.Auth != AuthOAuth || a.Plan != usage.TokenSharingPlan() || a.Status != usage.StatusNotVisible || a.Note != "usage not visible for this login type" {
				t.Errorf("account = %+v plan %+v", views([]Account{a}), a.Plan)
			}
			if !a.Expires.Equal(time.Unix(exp.Unix(), 0)) {
				t.Errorf("Expires = %v, want the exp claim", a.Expires)
			}
		})
	}
}

func TestPiOpenAIKeyIsTokenSharing(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	f := newFake("linux")
	// The token carries no scope: the store key alone classifies it.
	f.files[f.path(".pi", "agent", "auth.json")] = fmt.Sprintf(
		`{"openai":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d}}`,
		jwt(t, map[string]any{"exp": exp.Unix()}), exp.UnixMilli())
	got := readPi(f)
	if len(got) != 1 {
		t.Fatalf("accounts = %d", len(got))
	}
	a := got[0]
	if !a.TokenSharing || !a.Cred.Empty() || a.Key != "" || a.ID != "pi:openai" || a.Plan != usage.TokenSharingPlan() ||
		a.Status != usage.StatusNotVisible || a.Note != "usage not visible for this login type" {
		t.Errorf("account = %+v id %q", views(got), a.ID)
	}
}

func TestPiOpenAIKeyExpiredTokenSharing(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(-time.Hour)
	f := newFake("linux")
	f.files[f.path(".pi", "agent", "auth.json")] = fmt.Sprintf(
		`{"openai":{"type":"oauth","access":"fake-a","refresh":"fake-r","expires":%d}}`, exp.UnixMilli())
	got := readPi(f)
	if len(got) != 1 || !got[0].TokenSharing || got[0].Status != usage.StatusExpired || got[0].Note != "token expired, open pi to refresh" {
		t.Fatalf("account = %+v", views(got))
	}
}

func TestOpencodeOpenAIKeyIsNotTokenSharing(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	access, _ := chatgptTokens(t, exp, "acct-1")
	f := newFake("linux")
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = []OpencodeRow{
		{ID: "c1", IntegrationID: "openai", Active: true, Value: ocValueJSON("oauth", access, "fake-r", "", exp, "acct-1")},
	}
	got := readOpencode(f)
	if len(got) != 1 || got[0].TokenSharing || got[0].Key != "openai:acct-1:user-1" {
		t.Errorf("opencode openai entry = %+v", views(got))
	}
}

func TestOpencodeTokenSharingClaim(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	access := jwt(t, sharingClaims(exp, map[string]any{"scp": []any{"chatgpt.tokens.use.direct"}}))
	f := newFake("linux")
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = []OpencodeRow{
		{ID: "c1", IntegrationID: "openai", Active: true, Value: ocValueJSON("oauth", access, "fake-r", "", exp, "acct-1")},
	}
	got := readOpencode(f)
	if len(got) != 1 || !got[0].TokenSharing || !got[0].Cred.Empty() || got[0].Key != "" {
		t.Errorf("opencode token-sharing entry = %+v", views(got))
	}
}

func TestTokenSharingNeverMergesWithCodex(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	f := newFake("linux")
	codexAccess, codexID := chatgptTokens(t, exp, "acct-1")
	f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, codexAccess, codexID, "acct-1")
	f.files[f.path(".pi", "agent", "auth.json")] = fmt.Sprintf(
		`{"openai":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d,"accountId":"acct-1"}}`,
		jwt(t, sharingClaims(exp, map[string]any{"scp": "chatgpt.tokens.use.direct"})), exp.UnixMilli())
	got := Discover(f)
	if len(got) != 2 {
		t.Fatalf("accounts = %d, want 2: %+v", len(got), views(got))
	}
	for _, a := range got {
		if a.TokenSharing && !slices.Equal(a.Harnesses, []string{"pi"}) {
			t.Errorf("token-sharing harnesses = %v", a.Harnesses)
		}
	}
}

// threeCredentials is three credentials of one account, in the given order,
// with only the one at index inactive being inactive.
func threeCredentials(inactive int, order [3]int) []Account {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	harnesses := []string{"codex", "opencode", "pi"}
	in := make([]Account, 3)
	for i, o := range order {
		a := Account{
			Provider: OpenAI, Key: "openai:acct:user", ID: harnesses[o], Harnesses: []string{harnesses[o]},
			Expires: base.Add(time.Duration(o) * time.Hour), Note: "n", Inactive: o == inactive,
		}
		if a.Inactive {
			a.Note = "n " + inactiveNote
		}
		in[i] = a
	}
	return in
}

func TestMergeThreeWithOneInactive(t *testing.T) {
	t.Parallel()
	for inactive := range 3 {
		for _, order := range permutations3() {
			got := Merge(threeCredentials(inactive, order))
			if len(got) != 1 {
				t.Fatalf("rows = %d", len(got))
			}
			if got[0].Inactive || strings.Contains(got[0].Note, "(inactive)") {
				t.Errorf("inactive %d order %v: inactive=%v note=%q", inactive, order, got[0].Inactive, got[0].Note)
			}
			if diff := cmp.Diff([]string{"codex", "opencode", "pi"}, got[0].Harnesses); diff != "" {
				t.Errorf("harnesses (-want +got):\n%s", diff)
			}
		}
	}
}

func permutations3() [][3]int {
	return [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
}

func TestMergeLookupFailedNeverWins(t *testing.T) {
	t.Parallel()
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(token string, failed bool, exp time.Time) Account {
		return Account{
			Provider: OpenAI, Key: "openai:acct:user", ID: "x", Expires: exp, LookupFailed: failed,
			Cred: NewCredential(token, ""),
		}
	}
	// The failed credential is the newest of all.
	all := []Account{mk("failed", true, base.Add(2*time.Hour)), mk("ok-1", false, base), mk("ok-2", false, base.Add(time.Hour))}
	for _, order := range permutations3() {
		in := []Account{all[order[0]], all[order[1]], all[order[2]]}
		got := Merge(in)
		if len(got) != 1 {
			t.Fatalf("rows = %d", len(got))
		}
		if got[0].Cred.Token() != "ok-2" || got[0].LookupFailed {
			t.Errorf("order %v: winner = %q failed=%v, want ok-2 not failed", order, got[0].Cred.Token(), got[0].LookupFailed)
		}
	}
	both := Merge([]Account{mk("a", true, base), mk("b", true, base.Add(time.Hour))})
	if len(both) != 1 || !both[0].LookupFailed || both[0].Cred.Token() != "b" {
		t.Errorf("both failed = %+v", both)
	}
}

func TestCredentialFingerprint(t *testing.T) {
	t.Parallel()
	c := NewCredential("fake-tok", "acct-1")
	if got, want := c.Fingerprint(OpenAI), fingerprint(OpenAI, "fake-tok"); got != want || got == "" {
		t.Errorf("Fingerprint = %q, want %q", got, want)
	}
	if got := (Credential{}).Fingerprint(OpenAI); got != "" {
		t.Errorf("empty credential fingerprint = %q", got)
	}
	if strings.Contains(c.Fingerprint(OpenAI), "fake-tok") {
		t.Error("fingerprint leaks the token")
	}
}
