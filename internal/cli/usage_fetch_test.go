package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// secret is the fake token every test uses; it must never reach a snapshot.
const secret = "tok-SECRET-0123456789" // gitleaks:allow (fake redaction-test fixture)

var usageNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// seen is what the fake servers keep of a request.
type seen struct {
	Method string
	URL    *url.URL
	Header http.Header
}

type recorder struct {
	mu   sync.Mutex
	reqs []seen
}

func (r *recorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, seen{req.Method, req.URL, req.Header.Clone()})
}

func (r *recorder) all() []seen {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]seen(nil), r.reqs...)
}

func testFetcher(t *testing.T, srv *httptest.Server) usageFetcher {
	t.Helper()
	f := newFetcher(t.TempDir())
	f.now = func() time.Time { return usageNow }
	f.agyPath = func() (string, error) { return "", os.ErrNotExist }
	if srv != nil {
		f.anthropicURL, f.chatgptURL, f.githubURL = srv.URL, srv.URL, srv.URL
		f.client = srv.Client()
	}
	return f
}

func oauthAccount(prov string) provider.Account {
	a := provider.Account{
		Provider: prov, Label: "me@example.com", Harnesses: []string{"h1", "h2"},
		Auth: provider.AuthOAuth, Plan: usg.Plan{Kind: usg.KindIndividual, Name: "Plus"},
		Cred: provider.NewCredential(secret, "acct-42"),
	}
	if prov == provider.GitHubCopilot {
		a.Host = githubHost // as provider.Discover sets it
	}
	return a
}

// assertNoSecret fails when the snapshot, as JSON or text, holds the token.
func assertNoSecret(t *testing.T, s usg.Snapshot) {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) || strings.Contains(s.Note, secret) {
		t.Errorf("snapshot leaks the token: %s", b)
	}
}

// limitByName returns the limit called name, failing the test when absent.
func limitByName(t *testing.T, s usg.Snapshot, name string) usg.Limit {
	t.Helper()
	for _, l := range s.Limits {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no limit %q in %+v", name, s.Limits)
	return usg.Limit{}
}

func TestFetchAnthropicHeaders(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":31,"resets_at":"2026-11-06T14:00:00Z"}}`))
	}))
	defer srv.Close()

	snap := testFetcher(t, srv).fetch(t.Context(), oauthAccount(provider.Anthropic), time.Second)

	reqs := rec.all()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	r := reqs[0]
	if r.URL.Path != "/api/oauth/usage" || r.Method != http.MethodGet {
		t.Errorf("request = %s %s", r.Method, r.URL.Path)
	}
	for k, want := range map[string]string{
		"Authorization":  "Bearer " + secret,
		"Anthropic-Beta": "oauth-2025-04-20",
		"User-Agent":     "agents",
	} {
		if got := r.Header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if snap.Provider != provider.Anthropic || snap.Account != "me@example.com" ||
		!cmp.Equal(snap.Harnesses, []string{"h1", "h2"}) || snap.Plan.Name != "Plus" || !snap.FetchedAt.Equal(usageNow) {
		t.Errorf("snapshot identity wrong: %+v", snap)
	}
	if snap.Status != usg.StatusOK || snap.Source != sourceAPI {
		t.Errorf("status/source = %s/%s", snap.Status, snap.Source)
	}
	if l := limitByName(t, snap, "5h"); l.UsedPct == nil || *l.UsedPct != 31 || l.ResetsAt == nil || !l.ResetsAt.Equal(time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("5h limit = %+v", l)
	}
	assertNoSecret(t, snap)
}

func TestFetchOpenAIHeaders(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		_, _ = w.Write([]byte(`{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":33,"limit_window_seconds":18000,"reset_at":1793000000}},` +
			`"credits":{"has_credits":true,"balance":"12.50"},"spend_control":{}}`))
	}))
	defer srv.Close()

	snap := testFetcher(t, srv).fetch(t.Context(), oauthAccount(provider.OpenAI), time.Second)

	if snap.Status != usg.StatusOK || snap.Source != sourceAPI {
		t.Errorf("status/source = %s/%s", snap.Status, snap.Source)
	}
	if snap.Plan.Raw != "pro" { // the plan in the response wins over the credential's
		t.Errorf("plan = %+v, want the response's pro", snap.Plan)
	}
	if l := limitByName(t, snap, "5h"); l.UsedPct == nil || *l.UsedPct != 33 {
		t.Errorf("5h limit = %+v", l)
	}
	if l := limitByName(t, snap, "credits"); l.Remaining == nil || l.Remaining.Value != 12.5 {
		t.Errorf("credits limit = %+v", l)
	}
	if l := limitByName(t, snap, "spend limit"); !l.Hidden {
		t.Errorf("spend limit = %+v, want hidden", l)
	}

	r := rec.all()[0]
	if r.URL.Path != "/backend-api/wham/usage" {
		t.Errorf("path = %s", r.URL.Path)
	}
	for k, want := range map[string]string{
		"Authorization":      "Bearer " + secret,
		"Chatgpt-Account-Id": "acct-42",
		"Originator":         "codex_cli_rs",
	} {
		if got := r.Header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	assertNoSecret(t, snap)
}

func TestFetchOpenAINoAccountIDHeader(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	acct := oauthAccount(provider.OpenAI)
	acct.Cred = provider.NewCredential(secret, "")

	testFetcher(t, srv).fetch(t.Context(), acct, time.Second)

	if _, ok := rec.all()[0].Header["Chatgpt-Account-Id"]; ok {
		t.Error("ChatGPT-Account-Id sent without an account id")
	}
}

const (
	copilotUserJSON = `{"copilot_plan":"business","organization_login_list":["acme"],"quota_reset_date":"2026-12-01",` +
		`"quota_snapshots":{"premium_interactions":{"entitlement":300,"remaining":90,"percent_remaining":30}}}`
	copilotBillingJSON = `{"timePeriod":{"year":2026,"month":11},"usageItems":[{"product":"Copilot","sku":"Copilot Premium Request","unitType":"requests","grossQuantity":10,"netAmount":0.4}]}`
)

// copilotServer answers the user endpoint and the billing endpoint with billingCode.
func copilotServer(rec *recorder, billingCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if strings.HasPrefix(r.URL.Path, "/users/") {
			w.WriteHeader(billingCode)
			if billingCode == http.StatusOK {
				_, _ = w.Write([]byte(copilotBillingJSON))
			}
			return
		}
		_, _ = w.Write([]byte(copilotUserJSON))
	}))
}

func checkCopilotRequests(t *testing.T, reqs []seen) {
	t.Helper()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want the user request and billing", len(reqs))
	}
	if got := reqs[0].Header.Get("Authorization"); got != "token "+secret {
		t.Errorf("Authorization = %q", got)
	}
	if got := reqs[0].Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
	bill := reqs[1]
	if bill.URL.Path != "/users/octocat/settings/billing/premium_request/usage" ||
		bill.Header.Get("X-Github-Api-Version") != "2026-03-10" ||
		bill.Header.Get("Authorization") != "token "+secret {
		t.Errorf("billing request = %s %v", bill.URL.Path, bill.Header)
	}
}

func TestFetchCopilot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		billingCode int
		wantSpend   func(t *testing.T, snap usg.Snapshot)
	}{
		{"personal billing", http.StatusOK, func(t *testing.T, snap usg.Snapshot) {
			t.Helper()
			if l := limitByName(t, snap, "spend this month"); l.Kind != usg.LimitSpend || l.Hidden {
				t.Errorf("billing limit = %+v", l)
			}
		}},
		{"forbidden on an org plan is managed by org", http.StatusForbidden, func(t *testing.T, snap usg.Snapshot) {
			t.Helper()
			if l := limitByName(t, snap, "spend"); l.Kind != usg.LimitSpend || !l.Hidden || l.Note != "managed by org" {
				t.Errorf("spend limit = %+v", l)
			}
		}},
		{"not found on an org plan is managed by org", http.StatusNotFound, func(t *testing.T, snap usg.Snapshot) {
			t.Helper()
			if l := limitByName(t, snap, "spend"); !l.Hidden || l.Note != "managed by org" {
				t.Errorf("spend limit = %+v", l)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			srv := copilotServer(rec, tt.billingCode)
			defer srv.Close()
			acct := oauthAccount(provider.GitHubCopilot)
			acct.Login = "octocat"

			snap := testFetcher(t, srv).fetch(t.Context(), acct, time.Second)

			checkCopilotRequests(t, rec.all())
			if snap.Status != usg.StatusOK || snap.Source != sourceAPI || snap.Plan.Raw != "business" {
				t.Errorf("status/source/plan = %s/%s/%+v", snap.Status, snap.Source, snap.Plan)
			}
			l := limitByName(t, snap, "premium")
			if l.UsedPct == nil || *l.UsedPct != 70 || l.ResetsAt == nil || !l.ResetsAt.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
				t.Errorf("premium limit = %+v", l)
			}
			tt.wantSpend(t, snap)
			assertNoSecret(t, snap)
		})
	}
}

func TestFetchHTTPErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code int
		note string
	}{
		{http.StatusUnauthorized, "unauthorized (401)"},
		{http.StatusForbidden, "unauthorized (403)"},
		{http.StatusTooManyRequests, "rate limited (429)"},
		{http.StatusInternalServerError, "HTTP 500"},
	}
	for _, tt := range tests {
		t.Run(tt.note, func(t *testing.T) {
			t.Parallel()
			var calls int
			var mu sync.Mutex
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				calls++
				mu.Unlock()
				w.Header().Set("X-Echo", secret)
				w.WriteHeader(tt.code)
				_, _ = w.Write([]byte(`{"error":"` + secret + `"}`))
			}))
			defer srv.Close()

			for _, prov := range []string{provider.Anthropic, provider.OpenAI} {
				snap := testFetcher(t, srv).fetch(t.Context(), oauthAccount(prov), time.Second)
				if snap.Status != usg.StatusUnavailable || snap.Note != tt.note {
					t.Errorf("%s: status/note = %s/%q, want unavailable/%q", prov, snap.Status, snap.Note, tt.note)
				}
				assertNoSecret(t, snap)
			}
			mu.Lock()
			defer mu.Unlock()
			if calls != 2 {
				t.Errorf("calls = %d, want 2 (never retried)", calls)
			}
		})
	}
}

func TestFetchNetworkError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	f := testFetcher(t, srv)
	srv.Close() // connection refused

	snap := f.fetch(t.Context(), oauthAccount(provider.Anthropic), time.Second)

	if snap.Status != usg.StatusUnavailable || !strings.HasPrefix(snap.Note, "unavailable: ") {
		t.Errorf("status/note = %s/%q", snap.Status, snap.Note)
	}
	if strings.Contains(snap.Note, "http://") || strings.Contains(snap.Note, srv.URL) {
		t.Errorf("note carries the URL: %q", snap.Note)
	}
	assertNoSecret(t, snap)
}

func TestFetchTimeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	snap := testFetcher(t, srv).fetch(t.Context(), oauthAccount(provider.Anthropic), 50*time.Millisecond)

	if snap.Status != usg.StatusUnavailable || !strings.HasPrefix(snap.Note, "unavailable: ") {
		t.Errorf("status/note = %s/%q", snap.Status, snap.Note)
	}
}

func TestFetchPresetStatusMakesNoRequest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL.Path)
	}))
	defer srv.Close()

	for _, st := range []usg.Status{usg.StatusExpired, usg.StatusNoAPI, usg.StatusNotVisible, usg.StatusSkipped, usg.StatusNotLoggedIn} {
		acct := oauthAccount(provider.Anthropic)
		acct.Status, acct.Note = st, "note for "+string(st)
		snap := testFetcher(t, srv).fetch(t.Context(), acct, time.Second)
		if snap.Status != st || snap.Note != "note for "+string(st) || len(snap.Limits) != 0 {
			t.Errorf("%s: snapshot = %+v", st, snap)
		}
	}
}

func TestFetchUnsupportedAccountIsNoAPI(t *testing.T) {
	t.Parallel()
	acct := provider.Account{Provider: "openrouter", Label: "k", Auth: provider.AuthAPIKey, Cred: provider.NewCredential(secret, "")}
	snap := testFetcher(t, nil).fetch(t.Context(), acct, time.Second)
	if snap.Status != usg.StatusNoAPI {
		t.Errorf("status = %s, want no-api", snap.Status)
	}
}

func TestFetchBodyLimit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 3<<20)))
	}))
	defer srv.Close()
	body, err := testFetcher(t, srv).get(t.Context(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != maxBody {
		t.Errorf("body = %d bytes, want %d", len(body), maxBody)
	}
}

const agyUsageJSON = `{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[` +
	`{"name":"Gemini Models","buckets":[` +
	`{"id":"gemini-weekly","window":"weekly","remaining_fraction":0,"reset_time":"2026-11-07T21:48:39Z"},` +
	`{"id":"gemini-5h","window":"5h","disabled":true,"remaining_fraction":0.9688}]},` +
	`{"name":"Claude and GPT models","buckets":[` +
	`{"id":"3p-weekly","window":"weekly","remaining_fraction":0.25,"reset_time":"2026-11-08T12:05:47Z"},` +
	`{"id":"3p-5h","window":"5h","remaining_fraction":1,"reset_time":"2026-11-06T17:36:57Z"}]}]}}}`

func writeScript(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "agy")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func googleAccount() provider.Account {
	return provider.Account{
		Provider: provider.Google, Harnesses: []string{"antigravity"}, Auth: provider.AuthCLI,
		Plan: usg.Plan{Kind: usg.KindUnknown, Raw: "antigravity"},
	}
}

func TestFetchAgyRunsUsageCommand(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	dir := t.TempDir()
	script := writeScript(t, dir, `echo "$@" > "`+dir+`/args"
pwd > "`+dir+`/cwd"
stat -c %a . > "`+dir+`/mode" 2>/dev/null || stat -f %Lp . > "`+dir+`/mode"
cat <<'EOF'
`+agyUsageJSON+`
EOF
`)
	f := testFetcher(t, nil)
	f.agyPath = func() (string, error) { return script, nil }

	snap := f.fetch(t.Context(), googleAccount(), 10*time.Second)

	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatalf("fake agy was not run: %v", err)
	}
	if got, want := strings.TrimSpace(string(args)), "-p /usage --output-format json --print-timeout 10s"; got != want {
		t.Errorf("agy args = %q, want %q", got, want)
	}
	cwd, _ := os.ReadFile(filepath.Join(dir, "cwd"))
	gotCwd := strings.TrimSpace(string(cwd))
	if base := filepath.Base(gotCwd); !strings.HasPrefix(base, "agents-agy-") {
		t.Errorf("agy cwd = %q, want an agents-agy-* directory", gotCwd)
	}
	if _, err := os.Stat(gotCwd); !os.IsNotExist(err) {
		t.Errorf("agy cwd %q still exists (err %v)", gotCwd, err)
	}
	if mode, _ := os.ReadFile(filepath.Join(dir, "mode")); strings.TrimSpace(string(mode)) != "700" {
		t.Errorf("agy cwd mode = %q, want 700", mode)
	}
	if snap.Status != usg.StatusOK || snap.Source != "agy" || len(snap.Limits) != 4 {
		t.Fatalf("status/source/limits = %s/%s/%d, want ok/agy/4", snap.Status, snap.Source, len(snap.Limits))
	}
	var disabled, full int
	for _, l := range snap.Limits {
		if l.Disabled {
			disabled++
		}
		if l.UsedPct != nil && *l.UsedPct == 100 {
			full++
		}
	}
	if disabled != 1 || full != 1 {
		t.Errorf("disabled = %d, exhausted = %d, want 1 and 1: %+v", disabled, full, snap.Limits)
	}
}

func TestFetchAgyFailureIsUnavailable(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	for name, body := range map[string]string{
		"exit code": "exit 3\n",
		"garbage":   "echo not json\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			script := writeScript(t, t.TempDir(), body)
			f := testFetcher(t, nil)
			f.agyPath = func() (string, error) { return script, nil }
			snap := f.fetch(t.Context(), googleAccount(), 5*time.Second)
			if snap.Status != usg.StatusUnavailable || snap.Note != agyUnavailable {
				t.Errorf("status/note = %s/%q", snap.Status, snap.Note)
			}
		})
	}
}

func writeAgyLog(t *testing.T, home, name, line string) {
	t.Helper()
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFetchAgyMissingFallsBackToLog(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil) // agyPath: not found
	// The newest log (10:00 on the 6th) wins over the older one.
	writeAgyLog(t, f.home, "cli-20261105_090000.log", "E1105 09:07:11.896026 2480 errorreport.go:224] RESOURCE_EXHAUSTED (code 429): quota. Resets in 1h0m0s.\n")
	writeAgyLog(t, f.home, "cli-20261106_100000.log", "E1106 11:07:11.896026 2480 errorreport.go:224] RESOURCE_EXHAUSTED (code 429): quota. Resets in 4h58m35s.\n")

	snap := f.fetch(t.Context(), googleAccount(), time.Second)

	if snap.Status != usg.StatusStale || snap.Source != "agy-log" || len(snap.Limits) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	want := time.Date(2026, 11, 6, 11, 7, 11, 0, time.UTC).Add(4*time.Hour + 58*time.Minute + 35*time.Second)
	if l := snap.Limits[0]; l.ResetsAt == nil || l.ResetsAt.Sub(want).Abs() > time.Second {
		t.Errorf("reset = %v, want about %v", l.ResetsAt, want)
	}
}

func TestFetchAgyLogAlreadyResetIsUnavailable(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	writeAgyLog(t, f.home, "cli-20261105_090000.log", "E1105 09:07:11.896026 2480 errorreport.go:224] RESOURCE_EXHAUSTED (code 429): quota. Resets in 1h0m0s.\n")

	snap := f.fetch(t.Context(), googleAccount(), time.Second)

	if snap.Status != usg.StatusUnavailable || snap.Note != agyUnavailable || len(snap.Limits) != 0 {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestAgyLogFallbackWithoutLogs(t *testing.T) {
	t.Parallel()
	if _, ok := testFetcher(t, nil).agyLogFallback(googleAccount()); ok {
		t.Error("fallback found a log in an empty home")
	}
}

func TestCodexSessionFallback(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	acct := codexAccount(f.home)
	if _, ok := f.codexSessionFallback(acct); ok {
		t.Fatal("fallback found a session in an empty home")
	}

	writeCodexSession(t, f.home)

	snap, ok := f.codexSessionFallback(acct)
	if !ok {
		t.Fatal("fallback found no session")
	}
	if snap.Status != usg.StatusStale || snap.Source != "codex-session" || snap.Plan.Raw != "plus" {
		t.Errorf("snapshot = %+v", snap)
	}
	if l := limitByName(t, snap, "5h"); l.UsedPct == nil || *l.UsedPct != 12 {
		t.Errorf("5h limit = %+v", l)
	}
	if want := time.Date(2026, 11, 5, 10, 0, 1, 0, time.UTC); !snap.FetchedAt.Equal(want) {
		t.Errorf("FetchedAt = %v, want the session line time %v", snap.FetchedAt, want)
	}
}

// codexAccount is an OAuth OpenAI account logged in from <home>/.codex.
func codexAccount(home string) provider.Account {
	a := oauthAccount(provider.OpenAI)
	a.CodexHome = filepath.Join(home, ".codex")
	return a
}

func writeCodexSession(t *testing.T, home string) {
	t.Helper()
	writeCodexRollout(t, filepath.Join(home, ".codex"), 12)
}

// writeCodexRollout writes a recent rollout file under <codexHome>/sessions
// whose 5h window is pct used.
func writeCodexRollout(t *testing.T, codexHome string, pct int) {
	t.Helper()
	day := filepath.Join(codexHome, "sessions", "2026", "11", "05")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"timestamp":"2026-11-05T10:00:01.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"plan_type":"plus",`+
		`"primary":{"used_percent":%d,"window_minutes":300,"resets_at":1793000000}}}}`+"\n", pct)
	if err := os.WriteFile(filepath.Join(day, "rollout-2026-11-05T10-00-00-abc.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCodexSessionFallbackReadsTheAccountsHomeOnly(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	other := t.TempDir()
	writeCodexSession(t, f.home) // ~/.codex of the fetcher's home
	// An old session far outside the 7-day window is ignored.
	old := filepath.Join(other, "sessions", "2026", "01", "01")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	acct := oauthAccount(provider.OpenAI)
	acct.CodexHome = other
	if _, ok := f.codexSessionFallback(acct); ok {
		t.Error("fallback used a session with no files, or another home's")
	}
	acct.CodexHome = ""
	if _, ok := f.codexSessionFallback(acct); ok {
		t.Error("an account with no Codex home used a session log")
	}
}

func TestOpenAIFailureUsesSessionFallback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := testFetcher(t, srv)
	acct := codexAccount(f.home) // only a Codex login has session logs
	snap := f.fetch(t.Context(), acct, time.Second)
	// No session on disk: the failure stays unavailable.
	if snap.Status != usg.StatusUnavailable || snap.Note != "rate limited (429)" {
		t.Errorf("status/note = %s/%q", snap.Status, snap.Note)
	}

	writeCodexSession(t, f.home)
	snap = f.fetch(t.Context(), acct, time.Second)
	if snap.Status != usg.StatusStale || snap.Source != "codex-session" || snap.Note != "rate limited (429)" {
		t.Errorf("with a session: status/source/note = %s/%s/%q", snap.Status, snap.Source, snap.Note)
	}
	assertNoSecret(t, snap)
}

func TestLiveSnapshotsKeepOrderAndRunInParallel(t *testing.T) {
	t.Parallel()
	var started sync.WaitGroup
	started.Add(3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started.Done()
		started.Wait() // all three requests must be in flight together
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	accts := []provider.Account{oauthAccount(provider.Anthropic), oauthAccount(provider.OpenAI), oauthAccount(provider.GitHubCopilot)}
	snaps := liveSnapshots(t.Context(), accts, testFetcher(t, srv), 5*time.Second)

	got := make([]string, 0, len(snaps))
	for _, s := range snaps {
		got = append(got, s.Provider)
	}
	want := []string{provider.Anthropic, provider.OpenAI, provider.GitHubCopilot}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("order (-want +got):\n%s", diff)
	}
}
