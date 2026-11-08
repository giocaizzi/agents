package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// countingServer answers every request 200 with the Copilot user payload and
// records it.
func countingServer(rec *recorder) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		_, _ = w.Write([]byte(copilotUserJSON))
	}))
}

// secretStore is a fakeStore whose keychain holds the fake secret.
type secretStore struct{ fakeStore }

func (secretStore) Secret(string, string) (string, error) { return secret, nil }

const gheHost = "https://acme.ghe.com"

func TestFetchCopilotEnterpriseHostSendsNothing(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	srv := countingServer(rec)
	defer srv.Close()

	store := secretStore{fakeStore{home: "/h", files: map[string]string{
		"/h/.copilot/config.json": `{"loggedInUsers":[{"login":"octocat","host":"` + gheHost + `"}],"lastLoggedInUser":{"login":"octocat","host":"` + gheHost + `"}}`,
	}}}
	var discovered provider.Account
	for _, a := range provider.Discover(store) {
		if a.Provider == provider.GitHubCopilot {
			discovered = a
		}
	}
	if discovered.Login != "octocat" || discovered.Cred.Token() != secret {
		t.Fatalf("Discover found no octocat copilot account: %+v", discovered)
	}
	if discovered.Host != gheHost {
		t.Fatalf("Discover host = %q, want %q", discovered.Host, gheHost)
	}

	direct := oauthAccount(provider.GitHubCopilot)
	direct.Login, direct.Host = "octocat", gheHost
	noHost := oauthAccount(provider.GitHubCopilot)
	noHost.Login, noHost.Host = "octocat", ""

	f := testFetcher(t, srv)
	for name, acct := range map[string]provider.Account{"discovered": discovered, "direct": direct, "no host": noHost} {
		snap := f.fetch(t.Context(), acct, time.Second)
		wantNote := "GitHub Enterprise host acme.ghe.com: usage not queried"
		if acct.Host == "" {
			wantNote = "unknown GitHub host: usage not queried"
		}
		if snap.Status != usg.StatusNotVisible || snap.Note != wantNote || len(snap.Limits) != 0 {
			t.Errorf("%s: status/note = %s/%q, limits %d", name, snap.Status, snap.Note, len(snap.Limits))
		}
		assertNoSecret(t, snap)
	}
	if n := len(rec.all()); n != 0 {
		t.Errorf("server got %d requests, want 0", n)
	}
}

func TestFetchCopilotGitHubDotComSendsOneRequest(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"https://github.com"} {
		t.Run("host="+host, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			srv := countingServer(rec)
			defer srv.Close()
			acct := oauthAccount(provider.GitHubCopilot) // no Login: no billing call
			acct.Host = host

			snap := testFetcher(t, srv).fetch(t.Context(), acct, time.Second)

			reqs := rec.all()
			if len(reqs) != 1 || reqs[0].Header.Get("Authorization") != "token "+secret {
				t.Fatalf("requests = %+v, want one with token auth", reqs)
			}
			if snap.Status != usg.StatusOK {
				t.Errorf("status = %s", snap.Status)
			}
		})
	}
}

func TestFetchCopilotBillingDeniedWithoutOrgIsNotVisible(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/users/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"copilot_plan":"individual","quota_snapshots":{"premium_interactions":{"entitlement":300,"remaining":90,"percent_remaining":30}}}`))
	}))
	defer srv.Close()
	acct := oauthAccount(provider.GitHubCopilot)
	acct.Login = "octocat"

	snap := testFetcher(t, srv).fetch(t.Context(), acct, time.Second)

	if l := limitByName(t, snap, "spend"); !l.Hidden || l.Note != "not visible (token lacks billing access)" {
		t.Errorf("spend limit = %+v", l)
	}
}

func TestHiddenSpendPooledLimitIsManagedByOrg(t *testing.T) {
	t.Parallel()
	snap := usg.Snapshot{Plan: usg.Plan{Payer: usg.PayerSelf}}
	pooled := []usg.Limit{{Name: "premium", Scope: usg.ScopePool}}
	if l := hiddenSpend(snap, pooled); l.Note != "managed by org" {
		t.Errorf("pooled: note = %q", l.Note)
	}
	if l := hiddenSpend(snap, nil); l.Note == "managed by org" {
		t.Errorf("self payer, no pool: note = %q", l.Note)
	}
}

func TestFetchDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	other := &recorder{}
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { other.add(r) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/elsewhere", http.StatusFound)
	}))
	defer srv.Close()

	f := newFetcher(t.TempDir())
	f.now = func() time.Time { return usageNow }
	f.chatgptURL = srv.URL
	snap := f.fetch(t.Context(), oauthAccount(provider.OpenAI), time.Second)

	if snap.Status != usg.StatusUnavailable || snap.Note != "HTTP 302" {
		t.Errorf("status/note = %s/%q, want unavailable/HTTP 302", snap.Status, snap.Note)
	}
	if n := len(other.all()); n != 0 {
		t.Errorf("redirect target got %d requests, want 0", n)
	}
}

func TestHostNote(t *testing.T) {
	t.Parallel()
	const odd = "unrecognised GitHub host form: usage not queried"
	tests := map[string]string{
		"":                                 "unknown GitHub host: usage not queried",
		"HTTP://GITHUB.COM":                odd,
		"https://github.com:443":           odd,
		"github.com":                       odd,
		"https://www.github.com":           odd,
		"WWW.GitHub.com:8443":              odd,
		gheHost:                            "GitHub Enterprise host acme.ghe.com: usage not queried",
		"https://user:s3cret@acme.ghe.com": "GitHub Enterprise host acme.ghe.com: usage not queried",
		"user@acme.ghe.com":                "GitHub Enterprise host acme.ghe.com: usage not queried",
		"github.com.evil.example":          "GitHub Enterprise host github.com.evil.example: usage not queried",
	}
	for host, want := range tests {
		if got := hostNote(host); got != want {
			t.Errorf("hostNote(%q) = %q, want %q", host, got, want)
		}
		if got := hostNote(host); strings.Contains(got, "user") || strings.Contains(got, "s3cret") {
			t.Errorf("hostNote(%q) = %q leaks userinfo", host, got)
		}
	}
}
