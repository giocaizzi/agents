package cli

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// piRevokedFixture is a pi "openai-codex" login whose token carries no user
// id (the account id comes from the store) and outlives the Codex one, next to
// a Codex login of the same account.
func piRevokedFixture(t *testing.T) (store kcStore, piTok, codexTok string) {
	t.Helper()
	exp := usageNow.Add(time.Hour)
	piExp := exp.Add(time.Hour)
	piTok = fakeJWT(t, map[string]any{"exp": piExp.Unix()})
	files, codexTok := codexFiles(t, exp)
	return newStore(withFiles(files, piFile(piOpenAI(piTok, piExp, "acct-1")))), piTok, codexTok
}

func TestLiveFailedLookupIsNotBoundFromCache(t *testing.T) {
	t.Parallel()
	store, piTok, codexTok := piRevokedFixture(t)
	srv := newWhamServer(t)
	srv.answers[piTok] = whamBody("acct-1", "user-1")
	srv.answers[codexTok] = whamBody("acct-1", "user-1")
	fl := newOpenAIFlow(t, srv)

	fl.run(store)
	if diff := cmp.Diff([][]string{{"codex", "pi"}}, harnessSets(fl.snaps(), "openai")); diff != "" {
		t.Fatalf("run 1 did not merge (-want +got):\n%s", diff)
	}

	// The pi token is revoked now: the server rejects it, the Codex one is fine.
	srv.set(piTok, "")
	piBefore, codexBefore := len(srv.sent(piTok)), len(srv.sent(codexTok))
	out := fl.run(store)

	if diff := cmp.Diff([][]string{{"codex"}, {"pi"}}, harnessSets(fl.snaps(), "openai")); diff != "" {
		t.Fatalf("run 2 harness sets (-want +got):\n%s", diff)
	}
	for _, s := range openaiRows(fl.snaps()) {
		switch {
		case slices.Equal(s.Harnesses, []string{"codex"}):
			if s.Status != usg.StatusOK || s.Source != "api" || len(s.Limits) != 2 {
				t.Errorf("codex row = %+v, want fresh limits", s)
			}
		default:
			if s.Status != usg.StatusUnavailable || s.Note != "unauthorized (401)" {
				t.Errorf("pi row = %+v, want unauthorized (401)", s)
			}
		}
	}
	if n := len(srv.sent(piTok)) - piBefore; n != 1 {
		t.Errorf("the pi token was sent %d times in run 2, want 1", n)
	}
	if n := len(srv.sent(codexTok)) - codexBefore; n != 1 {
		t.Errorf("the codex token was sent %d times in run 2, want 1", n)
	}
	fl.noSecrets(out, piTok, codexTok)
}

func TestLiveLookupFailureStaysUnproven(t *testing.T) {
	t.Parallel()
	html := "<html><body>Bad gateway</body></html>"
	tests := []struct {
		name string
		fail whamFail
	}{
		{"403", whamFail{http.StatusForbidden, ""}},
		{"429", whamFail{http.StatusTooManyRequests, ""}},
		{"500", whamFail{http.StatusInternalServerError, ""}},
		{"502 with an HTML body", whamFail{http.StatusBadGateway, html}},
		{"200 with an HTML body", whamFail{http.StatusOK, html}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store, piTok, codexTok := piRevokedFixture(t)
			srv := newWhamServer(t)
			srv.answers[piTok] = whamBody("acct-1", "user-1")
			srv.answers[codexTok] = whamBody("acct-1", "user-1")
			fl := newOpenAIFlow(t, srv)
			fl.run(store)
			if diff := cmp.Diff([][]string{{"codex", "pi"}}, harnessSets(fl.snaps(), "openai")); diff != "" {
				t.Fatalf("run 1 did not merge (-want +got):\n%s", diff)
			}

			srv.mu.Lock()
			delete(srv.answers, piTok)
			srv.fail[piTok] = tt.fail
			srv.mu.Unlock()
			out := fl.run(store)

			if diff := cmp.Diff([][]string{{"codex"}, {"pi"}}, harnessSets(fl.snaps(), "openai")); diff != "" {
				t.Errorf("harness sets (-want +got):\n%s", diff)
			}
			for _, s := range openaiRows(fl.snaps()) {
				if want := slices.Equal(s.Harnesses, []string{"pi"}); s.IdentityUnknown != want {
					t.Errorf("row %v unknown = %v, want %v", s.Harnesses, s.IdentityUnknown, want)
				}
			}
			fl.noSecrets(out, piTok, codexTok)

			assertLookupFailed(t, srv, store, []string{"pi"})
		})
	}
}

// assertLookupFailed checks that resolving the identities of the store's
// accounts against srv flags exactly the accounts of want (harness lists).
func assertLookupFailed(t *testing.T, srv *whamServer, store provider.Store, want []string) {
	t.Helper()
	f := testFetcher(t, srv.Server)
	f.client = newFetcher(t.TempDir()).client
	var failed []string
	for _, a := range f.resolveIdentities(t.Context(), provider.Discover(store)) {
		if a.LookupFailed {
			failed = append(failed, strings.Join(a.Harnesses, ","))
		}
	}
	if diff := cmp.Diff(want, failed); diff != "" {
		t.Errorf("LookupFailed accounts (-want +got):\n%s", diff)
	}
}

func TestOpenAIUsageSentOncePerCredential(t *testing.T) {
	t.Parallel()
	exp := usageNow.Add(time.Hour)
	tests := []struct {
		name       string
		fail       *whamFail
		wantLimits bool
	}{
		{"200", nil, true},
		{"401", &whamFail{http.StatusUnauthorized, ""}, false},
		{"429", &whamFail{http.StatusTooManyRequests, ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry, piTok := piBareOpenAI(t, exp)
			srv := newWhamServer(t)
			if tt.fail != nil {
				srv.fail[piTok] = *tt.fail
			} else {
				srv.answers[piTok] = whamBody("acct-1", "user-1")
			}
			fl := newOpenAIFlow(t, srv)

			out := fl.run(newStore(piFile(entry)))

			if n := len(srv.requests()); n != 1 {
				t.Errorf("requests = %d, want 1 across identity and usage", n)
			}
			s := onlySnap(t, fl.snaps(), "openai")
			if got := len(s.Limits) > 0; got != tt.wantLimits {
				t.Errorf("limits = %+v, want limits: %v", s.Limits, tt.wantLimits)
			}
			fl.noSecrets(out, piTok)
		})
	}
}

func TestUsageMemoSendsOnceConcurrently(t *testing.T) {
	t.Parallel()
	m := newUsageMemo()
	calls := make(chan struct{}, 8)
	done := make(chan struct{})
	for range 8 {
		go func() {
			_, _ = m.do("fp:x", func() ([]byte, error) { calls <- struct{}{}; return nil, nil })
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
	if n := len(calls); n != 1 {
		t.Errorf("fn ran %d times, want 1", n)
	}
	var nilMemo *usageMemo
	n := 0
	for range 2 {
		_, _ = nilMemo.do("fp:x", func() ([]byte, error) { n++; return nil, nil })
	}
	if n != 2 {
		t.Errorf("a nil memo ran fn %d times, want 2", n)
	}
}
