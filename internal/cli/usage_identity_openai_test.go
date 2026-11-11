package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// whamRequest is what one ChatGPT usage request carried.
type whamRequest struct{ token, accountID string }

// whamServer answers GET /backend-api/wham/usage by bearer token and records
// every request. A token without an answer gets a 401; a token in redirect
// gets a 302 to the target.
type whamServer struct {
	*httptest.Server

	mu       sync.Mutex
	reqs     []whamRequest
	answers  map[string]string // token -> JSON body
	redirect map[string]string // token -> redirect target URL
	fail     map[string]whamFail
}

// whamFail is a non-200 answer (or a 200 with an unusable body) for a token.
type whamFail struct {
	status int
	body   string
}

// set changes the answer for a token between runs: body "" drops it (401).
func (s *whamServer) set(token, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if body == "" {
		delete(s.answers, token)
		return
	}
	s.answers[token] = body
}

func newWhamServer(t *testing.T) *whamServer {
	t.Helper()
	s := &whamServer{answers: map[string]string{}, redirect: map[string]string{}, fail: map[string]whamFail{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		s.mu.Lock()
		s.reqs = append(s.reqs, whamRequest{tok, r.Header.Get("Chatgpt-Account-Id")})
		body, ok := s.answers[tok]
		target := s.redirect[tok]
		fail, failed := s.fail[tok]
		s.mu.Unlock()
		switch {
		case r.URL.Path != "/backend-api/wham/usage":
			w.WriteHeader(http.StatusNotFound)
		case failed:
			w.WriteHeader(fail.status)
			_, _ = w.Write([]byte(fail.body))
		case target != "":
			http.Redirect(w, r, target, http.StatusFound)
		case !ok:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *whamServer) requests() []whamRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reqs)
}

// sent returns the requests that carried token.
func (s *whamServer) sent(token string) []whamRequest {
	var out []whamRequest
	for _, r := range s.requests() {
		if r.token == token {
			out = append(out, r)
		}
	}
	return out
}

// whamBody is the documented usage answer; an empty id is left out.
func whamBody(accountID, userID string) string {
	ids := ""
	if accountID != "" {
		ids += fmt.Sprintf(`"account_id":%q,`, accountID)
	}
	if userID != "" {
		ids += fmt.Sprintf(`"user_id":%q,`, userID)
	}
	return `{"plan_type":"plus",` + ids +
		`"rate_limit":{"primary_window":{"used_percent":33,"limit_window_seconds":18000,"reset_at":1793000000},` +
		`"secondary_window":{"used_percent":12,"limit_window_seconds":604800,"reset_at":1793500000}}}`
}

// piBareOpenAI is a pi "openai-codex" login whose token carries no identity claim.
func piBareOpenAI(t *testing.T, exp time.Time) (entry, token string) {
	t.Helper()
	token = fakeJWT(t, map[string]any{
		"exp": exp.Unix(),
		"sub": "sub-1",
		"https://api.openai.com/auth": map[string]any{
			"encrypted_auth_metadata": "fake-metadata", "per_user_salt": "fake-salt",
		},
	})
	entry = fmt.Sprintf(`"openai-codex":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d,"clientId":"fake-client","scopes":["openid"]}`,
		token, exp.UnixMilli())
	return entry, token
}

// codexFiles is a Codex login of acct-1 / user-1.
func codexFiles(t *testing.T, exp time.Time) (files map[string]string, token string) {
	t.Helper()
	token = chatgptJWT(t, exp, "acct-1", "me@example.com")
	return map[string]string{
		"/h/.codex/auth.json": fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q,"account_id":"acct-1"}}`, token),
	}, token
}

// openaiFlow runs the usage command against a whamServer and one cache.
type openaiFlow struct {
	*flow

	srv *whamServer
}

func newOpenAIFlow(t *testing.T, srv *whamServer) openaiFlow {
	t.Helper()
	return openaiFlow{newFlow(t, &idServer{Server: srv.Server}), srv}
}

// run is flow.run with the production HTTP client, which follows no redirect.
func (fl openaiFlow) run(store provider.Store) string {
	fl.t.Helper()
	f := testFetcher(fl.t, fl.srv.Server)
	f.client = newFetcher(fl.t.TempDir()).client
	fl.out.Reset()
	if code := fl.d.runUsageWith(fl.t.Context(), nil, store, f); code != 0 {
		fl.t.Fatalf("usage exit = %d", code)
	}
	return fl.out.String()
}

func openaiRows(snaps []usg.Snapshot) []usg.Snapshot {
	var out []usg.Snapshot
	for _, s := range snaps {
		if s.Provider == provider.OpenAI {
			out = append(out, s)
		}
	}
	return out
}

func TestOpenAIIdentityPiOnly(t *testing.T) {
	exp := usageNow.Add(time.Hour)
	t.Parallel()
	entry, piTok := piBareOpenAI(t, exp)
	srv := newWhamServer(t)
	srv.answers[piTok] = whamBody("acct-1", "user-1")
	fl := newOpenAIFlow(t, srv)

	out := fl.run(newStore(piFile(entry)))

	s := onlySnap(t, fl.snaps(), "openai")
	if s.IdentityUnknown || s.Status != usg.StatusOK || !slices.Equal(s.Harnesses, []string{"pi"}) || s.Plan.Name != "Plus" {
		t.Errorf("row = %+v", s)
	}
	if len(s.Limits) != 2 || *s.Limits[0].UsedPct != 33 || *s.Limits[1].UsedPct != 12 {
		t.Errorf("limits = %+v, want the server's two windows", s.Limits)
	}
	if strings.Contains(out, "unknown") {
		t.Errorf("output reads unknown:\n%s", out)
	}
	reqs := srv.requests()
	// One request serves the identity and the usage: the identity request has
	// no account to name.
	if want := []whamRequest{{piTok, ""}}; !slices.Equal(reqs, want) {
		t.Errorf("requests = %+v, want one with the pi token and no ChatGPT-Account-Id", reqs)
	}
	fl.noSecrets(out, piTok)
}

func TestOpenAIIdentityMergesCodexAndPi(t *testing.T) {
	exp := usageNow.Add(time.Hour)
	t.Parallel()
	entry, piTok := piBareOpenAI(t, exp)
	files, codexTok := codexFiles(t, exp)
	srv := newWhamServer(t)
	srv.answers[piTok] = whamBody("acct-1", "user-1")
	srv.answers[codexTok] = whamBody("acct-1", "user-1")
	fl := newOpenAIFlow(t, srv)

	out := fl.run(newStore(withFiles(files, piFile(entry))))

	s := onlySnap(t, fl.snaps(), "openai")
	if s.Account != "me@example.com" || !slices.Equal(s.Harnesses, []string{"codex", "pi"}) || s.IdentityUnknown {
		t.Errorf("row = %+v", s)
	}
	fl.noSecrets(out, piTok, codexTok)
}

func TestOpenAIIdentitySplitsAnotherPerson(t *testing.T) {
	exp := usageNow.Add(time.Hour)
	t.Parallel()
	entry, piTok := piBareOpenAI(t, exp)
	files, codexTok := codexFiles(t, exp)
	srv := newWhamServer(t)
	srv.answers[piTok] = whamBody("acct-2", "user-9")
	srv.answers[codexTok] = whamBody("acct-1", "user-1")
	fl := newOpenAIFlow(t, srv)

	out := fl.run(newStore(withFiles(files, piFile(entry))))

	snaps := fl.snaps()
	if diff := cmp.Diff([][]string{{"codex"}, {"pi"}}, harnessSets(snaps, "openai")); diff != "" {
		t.Errorf("harness sets (-want +got):\n%s", diff)
	}
	for _, s := range snaps {
		if s.IdentityUnknown || s.Status != usg.StatusOK || s.Plan.Name != "Plus" || len(s.Limits) != 2 {
			t.Errorf("row %v = %+v, want proven with its own plan and limits", s.Harnesses, s)
		}
	}
	if strings.Contains(out, "same account?") {
		t.Errorf("output reads unknown:\n%s", out)
	}
	fl.noSecrets(out, piTok, codexTok)
}

func TestOpenAIIdentityExpiredTokenNeverSent(t *testing.T) {
	exp := usageNow.Add(time.Hour)
	t.Parallel()
	entry, piTok := piBareOpenAI(t, usageNow.Add(-time.Hour))
	files, codexTok := codexFiles(t, exp)
	srv := newWhamServer(t)
	srv.answers[piTok] = whamBody("acct-1", "user-1")
	srv.answers[codexTok] = whamBody("acct-1", "user-1")
	fl := newOpenAIFlow(t, srv)

	out := fl.run(newStore(withFiles(files, piFile(entry))))

	if n := len(srv.sent(piTok)); n != 0 {
		t.Errorf("the expired token was sent %d times", n)
	}
	if !strings.Contains(out, "token expired, open pi to refresh") {
		t.Errorf("no expiry note:\n%s", out)
	}
	if n := strings.Count(out, "same account? unknown"); n != 1 {
		t.Errorf("unknown labels = %d, want 1 (the pi row):\n%s", n, out)
	}
	if diff := cmp.Diff([][]string{{"codex"}, {"pi"}}, harnessSets(fl.snaps(), "openai")); diff != "" {
		t.Errorf("harness sets (-want +got):\n%s", diff)
	}
	fl.noSecrets(out, piTok, codexTok)
}

func TestOpenAIIdentityCachedProofKeepsExpiredMerged(t *testing.T) {
	exp := usageNow.Add(time.Hour)
	t.Parallel()
	entry, piTok := piBareOpenAI(t, exp)
	files, codexTok := codexFiles(t, exp)
	srv := newWhamServer(t)
	srv.answers[piTok] = whamBody("acct-1", "user-1")
	srv.answers[codexTok] = whamBody("acct-1", "user-1")
	fl := newOpenAIFlow(t, srv)
	fl.run(newStore(withFiles(files, piFile(entry))))
	if diff := cmp.Diff([][]string{{"codex", "pi"}}, harnessSets(fl.snaps(), "openai")); diff != "" {
		t.Fatalf("first run did not merge (-want +got):\n%s", diff)
	}

	// Same token, now past its expiry: it is not sent, the cache vouches for it.
	expired := strings.Replace(entry, fmt.Sprintf(`"expires":%d`, exp.UnixMilli()), fmt.Sprintf(`"expires":%d`, usageNow.Add(-time.Hour).UnixMilli()), 1)
	before := len(srv.sent(piTok))
	out := fl.run(newStore(withFiles(files, piFile(expired))))

	s := onlySnap(t, fl.snaps(), "openai")
	if !slices.Equal(s.Harnesses, []string{"codex", "pi"}) || s.IdentityUnknown {
		t.Errorf("row = %+v, want one merged row", s)
	}
	if n := len(srv.sent(piTok)); n != before {
		t.Errorf("the expired token was sent %d more times", n-before)
	}
	fl.noSecrets(out, piTok, codexTok)
}

func TestOpenAIIdentityStaysUnproven(t *testing.T) {
	t.Parallel()
	exp := usageNow.Add(time.Hour)

	tests := []struct {
		name string
		// piAccount, when set, is the account id stored next to a token
		// without a sub claim (the account id alone proves nothing).
		piAccount string
		answer    func(tok string, srv, other *whamServer)
	}{
		{"no user id", "", func(tok string, srv, _ *whamServer) { srv.answers[tok] = whamBody("acct-1", "") }},
		{"unauthorized", "", func(string, *whamServer, *whamServer) {}},
		{"redirect", "", func(tok string, srv, other *whamServer) { srv.redirect[tok] = other.URL + "/elsewhere" }},
		{"account id differs from the one sent", "acct-1", func(tok string, srv, _ *whamServer) { srv.answers[tok] = whamBody("acct-2", "user-1") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry, piTok := piBareOpenAI(t, exp)
			if tc.piAccount != "" {
				piTok = fakeJWT(t, map[string]any{"exp": exp.Unix()})
				entry = fmt.Sprintf(`"openai-codex":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d,"accountId":%q}`,
					piTok, exp.UnixMilli(), tc.piAccount)
			}
			files, codexTok := codexFiles(t, exp)
			srv, other := newWhamServer(t), newWhamServer(t)
			srv.answers[codexTok] = whamBody("acct-1", "user-1")
			tc.answer(piTok, srv, other)
			fl := newOpenAIFlow(t, srv)

			out := fl.run(newStore(withFiles(files, piFile(entry))))

			if diff := cmp.Diff([][]string{{"codex"}, {"pi"}}, harnessSets(fl.snaps(), "openai")); diff != "" {
				t.Errorf("harness sets (-want +got):\n%s", diff)
			}
			for _, s := range openaiRows(fl.snaps()) {
				if want := slices.Equal(s.Harnesses, []string{"pi"}); s.IdentityUnknown != want {
					t.Errorf("row %v unknown = %v, want %v", s.Harnesses, s.IdentityUnknown, want)
				}
			}
			if !strings.Contains(out, "same account? unknown") {
				t.Errorf("no unknown label:\n%s", out)
			}
			if n := len(other.requests()); n != 0 {
				t.Errorf("redirect target got %d requests", n)
			}
			fl.noSecrets(out, piTok, codexTok)
		})
	}
}
