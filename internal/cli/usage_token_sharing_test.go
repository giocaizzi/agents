package cli

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
	"github.com/giocaizzi/agents/internal/term"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// sharingToken is the access token of a "Sign in with ChatGPT" login for the
// API: the token-sharing scope, no account or user id, a salted sub.
func sharingToken(t *testing.T, exp time.Time, scope map[string]any) string {
	t.Helper()
	claims := map[string]any{
		"exp": exp.Unix(),
		"sub": "salted-sub",
		"https://api.openai.com/auth": map[string]any{
			"encrypted_auth_metadata": "fake-metadata", "per_user_salt": "fake-salt",
		},
	}
	maps.Copy(claims, scope)
	return fakeJWT(t, claims)
}

// piSharingEntry is pi's "openai" login, a token-sharing login.
func piSharingEntry(token string, exp time.Time) string {
	return fmt.Sprintf(`"openai":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d}`, token, exp.UnixMilli())
}

func sharingRowText(t *testing.T, out string) {
	t.Helper()
	rows := rowsOf(out, "openai")
	if len(rows) != 1 {
		t.Fatalf("openai rows = %q", rows)
	}
	if got, want := strings.Join(strings.Fields(rows[0]), " "), "openai ChatGPT sign-in (token sharing) pi"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
	if !strings.Contains(out, "! usage not visible for this login type") {
		t.Errorf("no note line:\n%s", out)
	}
}

func TestTokenSharingRowAndNoRequest(t *testing.T) {
	t.Parallel()
	exp := usageNow.Add(time.Hour)
	tok := sharingToken(t, exp, map[string]any{"scp": []any{"openid", "chatgpt.tokens.use.direct"}})
	srv := newWhamServer(t)
	srv.answers[tok] = whamBody("acct-1", "user-1")
	fl := newOpenAIFlow(t, srv)
	store := newStore(piFile(piSharingEntry(tok, exp)))

	out := fl.run(store, "-live")
	sharingRowText(t, out)
	if n := len(srv.requests()); n != 0 {
		t.Errorf("-live sent %d requests to the usage server, want 0", n)
	}
	if got := fl.snaps(); len(got) != 1 || got[0].Plan.Name != usg.NameTokenSharing || got[0].Status != usg.StatusNotVisible || got[0].IdentityUnknown {
		t.Errorf("cached snapshots = %+v", got)
	}
	fl.noSecrets(out, tok)

	out = fl.run(store)
	sharingRowText(t, out)
	if n := len(srv.requests()); n != 0 {
		t.Errorf("cached run sent %d requests", n)
	}
}

func TestTokenSharingExpired(t *testing.T) {
	t.Parallel()
	exp := usageNow.Add(-time.Hour)
	tok := sharingToken(t, exp, map[string]any{"scope": "openid chatgpt.tokens.use.direct"})
	srv := newWhamServer(t)
	fl := newOpenAIFlow(t, srv)

	out := fl.run(newStore(piFile(piSharingEntry(tok, exp))), "-live")

	if !strings.Contains(out, "ChatGPT sign-in (token sharing)") || !strings.Contains(out, "! token expired, open pi to refresh") {
		t.Errorf("output:\n%s", out)
	}
	if n := len(srv.requests()); n != 0 {
		t.Errorf("requests = %d", n)
	}
}

func TestTokenSharingIgnoresOldCachedSnapshot(t *testing.T) {
	t.Parallel()
	exp := usageNow.Add(time.Hour)
	tok := sharingToken(t, exp, nil)
	srv := newWhamServer(t)
	fl := newOpenAIFlow(t, srv)
	old := usg.Snapshot{
		ID: "pi:openai", Provider: provider.OpenAI, Harnesses: []string{"pi"},
		Plan:   usg.Plan{Kind: usg.KindUnknown, Billing: usg.BillingUnknown, Payer: usg.PayerUnknown},
		Status: usg.StatusUnavailable, Note: "unauthorized (401)", FetchedAt: usageNow,
	}
	if err := writeUsageCache(fl.path, []usg.Snapshot{old}, usageNow); err != nil {
		t.Fatal(err)
	}

	out := fl.run(newStore(piFile(piSharingEntry(tok, exp))))

	sharingRowText(t, out)
	if strings.Contains(out, "unauthorized") {
		t.Errorf("an old 401 row is shown:\n%s", out)
	}
}

func TestTokenSharingNeverMergesWithCodex(t *testing.T) {
	t.Parallel()
	exp := usageNow.Add(time.Hour)
	tok := sharingToken(t, exp, map[string]any{"scp": "chatgpt.tokens.use.direct"})

	tests := []struct {
		name string
		// codex returns the Codex files and its token.
		codex func(t *testing.T) (map[string]string, string)
	}{
		{"proven codex login", func(t *testing.T) (map[string]string, string) {
			t.Helper()
			return codexFiles(t, exp)
		}},
		{"unproven codex login", func(t *testing.T) (map[string]string, string) {
			t.Helper()
			bare := fakeJWT(t, map[string]any{"exp": exp.Unix()})
			return map[string]string{"/h/.codex/auth.json": fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q}}`, bare)}, bare
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files, codexTok := tt.codex(t)
			srv := newWhamServer(t)
			srv.answers[codexTok] = whamBody("acct-1", "user-1")
			srv.answers[tok] = whamBody("acct-1", "user-1")
			fl := newOpenAIFlow(t, srv)

			out := fl.run(newStore(withFiles(files, piFile(piSharingEntry(tok, exp)))), "-live")

			if diff := cmp.Diff([][]string{{"codex"}, {"pi"}}, harnessSets(fl.snaps(), "openai")); diff != "" {
				t.Errorf("harness sets (-want +got):\n%s", diff)
			}
			if n := len(srv.sent(tok)); n != 0 {
				t.Errorf("the token-sharing token was sent %d times", n)
			}
			fl.noSecrets(out, tok, codexTok)
		})
	}
}

func TestTokenSharingIsNotASibling(t *testing.T) {
	t.Parallel()
	exp := usageNow.Add(time.Hour)
	tok := sharingToken(t, exp, nil)
	bare := fakeJWT(t, map[string]any{"exp": exp.Unix()})
	srv := newWhamServer(t) // the Codex token gets a 401: its identity stays unknown
	fl := newOpenAIFlow(t, srv)
	files := map[string]string{"/h/.codex/auth.json": fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q}}`, bare)}

	out := fl.run(newStore(withFiles(files, piFile(piSharingEntry(tok, exp)))), "-live")

	if strings.Contains(out, "same account?") || !strings.Contains(out, "identity unknown") {
		t.Errorf("the token-sharing row counts as a sibling:\n%s", out)
	}
}

func TestRenderTokenSharingRow(t *testing.T) {
	t.Parallel()
	snaps := []usg.Snapshot{{
		Provider: provider.OpenAI, Harnesses: []string{"pi"}, Plan: usg.TokenSharingPlan(),
		Status: usg.StatusNotVisible, Note: usg.NoteSharingNotVisible,
	}, {
		Provider: provider.OpenAI, Harnesses: []string{"codex"}, IdentityUnknown: true,
		Plan:   usg.Plan{Kind: usg.KindUnknown},
		Status: usg.StatusUnavailable, Note: "unauthorized (401)",
	}}
	var out bytes.Buffer
	renderUsage(&out, term.NewStyle(&out, func(string) string { return "" }), snaps, "(live)", usageNow)
	if strings.Contains(out.String(), "same account?") || !strings.Contains(out.String(), "unknown · identity unknown") {
		t.Errorf("render:\n%s", out.String())
	}
	if !slices.ContainsFunc(strings.Split(out.String(), "\n"), func(l string) bool {
		return strings.Join(strings.Fields(l), " ") == "openai ChatGPT sign-in (token sharing) pi"
	}) {
		t.Errorf("no token-sharing row:\n%s", out.String())
	}
}
