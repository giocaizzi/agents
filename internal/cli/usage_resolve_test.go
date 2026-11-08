package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

const (
	ghCLI = "fake-gh-token-cli"
	ghOC  = "fake-gh-token-oc"
	ghPi  = "fake-gh-token-pi"
	ghPi2 = "fake-gh-token-pi-new"
	ghEnt = "fake-gh-token-ent"
	antCC = "fake-ant-token-cc"
	antOC = "fake-ant-token-oc"
)

// idServer answers every endpoint the usage command calls and records what
// it was asked.
type idServer struct {
	*httptest.Server

	mu       sync.Mutex
	hits     []string          // "path"
	hdr      map[string]string // header of the last request per path, "path|Header"
	logins   map[string]string // GitHub token -> login
	profiles map[string]string // Anthropic token -> profile JSON
	failID   bool              // /user and /api/oauth/profile answer 500
	failOAI  bool              // /backend-api/wham/usage answers 500
}

func newIDServer(t *testing.T) *idServer {
	t.Helper()
	s := &idServer{hdr: map[string]string{}, logins: map[string]string{}, profiles: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *idServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.hits = append(s.hits, r.URL.Path)
	for _, h := range []string{"Authorization", "Accept", "X-Github-Api-Version", "Chatgpt-Account-Id", "Anthropic-Beta"} {
		s.hdr[r.URL.Path+"|"+h] = r.Header.Get(h)
	}
	failID, failOAI := s.failID, s.failOAI
	token := bearerToken(r)
	login, loginOK := s.logins[token]
	profile, profileOK := s.profiles[token]
	s.mu.Unlock()

	switch {
	case r.URL.Path == "/user":
		switch {
		case failID:
			w.WriteHeader(http.StatusInternalServerError)
		case !loginOK:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			fmt.Fprintf(w, `{"login":%q,"id":1}`, login)
		}
	case r.URL.Path == "/api/oauth/profile":
		switch {
		case failID:
			w.WriteHeader(http.StatusInternalServerError)
		case !profileOK:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			_, _ = w.Write([]byte(profile))
		}
	case r.URL.Path == "/backend-api/wham/usage":
		if failOAI {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":33,"limit_window_seconds":18000,"reset_at":1793000000}}}`))
	case strings.HasPrefix(r.URL.Path, "/copilot_internal/"):
		_, _ = w.Write([]byte(`{"copilot_plan":"individual","quota_reset_date":"2026-12-01","quota_snapshots":{"premium_interactions":{"entitlement":100,"remaining":69,"percent_remaining":69}}}`))
	case r.URL.Path == "/api/oauth/usage":
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":31,"resets_at":"2026-11-06T14:14:00Z"}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *idServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.hits {
		if p == path {
			n++
		}
	}
	return n
}

func (s *idServer) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hits)
}

func (s *idServer) header(path, name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hdr[path+"|"+name]
}

// kcStore is a rowStore with keychain items.
type kcStore struct {
	rowStore

	secrets map[string]string // service + "\x00" + account
}

func (s kcStore) Secret(service, account string) (string, error) {
	if v, ok := s.secrets[service+"\x00"+account]; ok {
		return v, nil
	}
	return "", os.ErrNotExist
}

// newStore builds a store from files under /h and opencode rows.
func newStore(files map[string]string, rows ...provider.OpencodeRow) kcStore {
	return kcStore{rowStore{fakeStore{home: "/h", files: files}, rows}, nil}
}

func (s kcStore) withCopilotCLI() kcStore {
	const login = "octocat"
	files := maps.Clone(s.files)
	if files == nil {
		files = map[string]string{}
	}
	files["/h/.copilot/config.json"] = fmt.Sprintf(`{"loggedInUsers":[%q]}`, login)
	s.files = files
	s.secrets = map[string]string{copilotService + "\x00" + githubHost + ":" + login + ":github": ghCLI}
	return s
}

const copilotService = "copilot-cli"

func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "none"}) + "." + enc(claims) + ".fake-signature"
}

// chatgptJWT is a fake ChatGPT access token for an account, user and email.
func chatgptJWT(t *testing.T, exp time.Time, accountID, email string) string {
	t.Helper()
	return fakeJWT(t, map[string]any{
		"exp": exp.Unix(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID, "chatgpt_user_id": "user-1", "chatgpt_plan_type": "plus",
		},
		"https://api.openai.com/profile": map[string]any{"email": email},
	})
}

func piOpenAI(access string, exp time.Time, accountID string) string {
	return fmt.Sprintf(`"openai-codex":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d,"accountId":%q}`, access, exp.UnixMilli(), accountID)
}

func piCopilotEntry(token, enterprise string) string {
	extra := ""
	if enterprise != "" {
		extra = fmt.Sprintf(`,"enterpriseUrl":%q`, enterprise)
	}
	return fmt.Sprintf(`"github-copilot":{"type":"oauth","refresh":%q,"access":"fake-api-token","expires":%d%s}`, token, usageNow.Add(time.Hour).UnixMilli(), extra)
}

func piFile(entries ...string) map[string]string {
	return map[string]string{"/h/.pi/agent/auth.json": "{" + strings.Join(entries, ",") + "}"}
}

func ocCopilotRow(token string) provider.OpencodeRow {
	return provider.OpencodeRow{
		ID: "row-gh", IntegrationID: provider.GitHubCopilot, Active: true,
		Value: fmt.Appendf(nil, `{"type":"oauth","refresh":%q,"expires":%d}`, token, usageNow.Add(time.Hour).UnixMilli()),
	}
}

func ocAnthropicRow(token string) provider.OpencodeRow {
	return provider.OpencodeRow{
		ID: "row-ant", IntegrationID: provider.Anthropic, Active: true,
		Value: fmt.Appendf(nil, `{"type":"oauth","access":%q,"expires":%d}`, token, usageNow.Add(time.Hour).UnixMilli()),
	}
}

func claudeCreds(token string) map[string]string {
	return map[string]string{"/h/.claude/.credentials.json": fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"subscriptionType":"max"}}`, token, usageNow.Add(time.Hour).UnixMilli())}
}

func withFiles(base, more map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, base)
	maps.Copy(out, more)
	return out
}

func profileJSON(account, org string) string {
	return fmt.Sprintf(`{"account":{"uuid":%q,"email":"me@example.com"},"organization":{"uuid":%q,"organization_type":"claude_max"}}`, account, org)
}

// flow runs the usage command against one server and cache.
type flow struct {
	t    *testing.T
	d    Deps
	out  *bytes.Buffer
	path string
	srv  *idServer
}

func newFlow(t *testing.T, srv *idServer) *flow {
	t.Helper()
	d, out, _, path := usageDeps(t)
	return &flow{t: t, d: d, out: out, path: path, srv: srv}
}

// run executes one usage run and returns its output.
func (fl *flow) run(store provider.Store, args ...string) string {
	fl.t.Helper()
	fl.out.Reset()
	if code := fl.d.runUsageWith(fl.t.Context(), args, store, testFetcher(fl.t, fl.srv.Server)); code != 0 {
		fl.t.Fatalf("usage exit = %d", code)
	}
	return fl.out.String()
}

func (fl *flow) snaps() []usg.Snapshot {
	fl.t.Helper()
	c, err := readUsageCache(fl.path)
	if err != nil {
		fl.t.Fatal(err)
	}
	return c.Snapshots
}

// noSecrets fails when the output or the cache file holds a token.
func (fl *flow) noSecrets(out string, tokens ...string) {
	fl.t.Helper()
	raw, err := os.ReadFile(fl.path)
	if err != nil && !os.IsNotExist(err) {
		fl.t.Fatal(err)
	}
	for _, tok := range tokens {
		if strings.Contains(out, tok) || bytes.Contains(raw, []byte(tok)) {
			fl.t.Errorf("token %q reached the output or the cache", tok)
		}
	}
	if strings.Contains(out, "fp:") {
		fl.t.Error("a fingerprint was rendered")
	}
}

func rowsOf(out, prov string) []string {
	var rows []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, prov+" ") {
			rows = append(rows, line)
		}
	}
	return rows
}

func harnessSets(snaps []usg.Snapshot, prov string) [][]string {
	var out [][]string
	for _, s := range snaps {
		if s.Provider == prov {
			out = append(out, s.Harnesses)
		}
	}
	slices.SortFunc(out, func(a, b []string) int { return strings.Compare(strings.Join(a, ","), strings.Join(b, ",")) })
	return out
}

func onlySnap(t *testing.T, snaps []usg.Snapshot, prov string) usg.Snapshot {
	t.Helper()
	var got []usg.Snapshot
	for _, s := range snaps {
		if s.Provider == prov {
			got = append(got, s)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%s rows = %d, want 1: %+v", prov, len(got), got)
	}
	return got[0]
}

func TestPiOnlyOpenAILogin(t *testing.T) {
	t.Parallel()
	srv := newIDServer(t)
	exp := usageNow.Add(time.Hour)
	access := chatgptJWT(t, exp, "acct-pi", "me@example.com")
	store := newStore(piFile(piOpenAI(access, exp, "acct-pi")))
	fl := newFlow(t, srv)

	out := fl.run(store, "-live")

	s := onlySnap(t, fl.snaps(), "openai")
	if s.Account != "me@example.com" || !slices.Equal(s.Harnesses, []string{"pi"}) || s.Status != usg.StatusOK || s.IdentityUnknown {
		t.Errorf("row = %+v", s)
	}
	if len(s.Limits) == 0 || s.Limits[0].UsedPct == nil || *s.Limits[0].UsedPct != 33 {
		t.Errorf("limits = %+v, want the server's", s.Limits)
	}
	if got := srv.header("/backend-api/wham/usage", "Chatgpt-Account-Id"); got != "acct-pi" {
		t.Errorf("ChatGPT-Account-Id = %q", got)
	}
	if n := srv.count("/user") + srv.count("/api/oauth/profile"); n != 0 {
		t.Errorf("identity requests for an OpenAI login = %d", n)
	}
	fl.noSecrets(out, access)
}

func TestPiAndCodexOpenAI(t *testing.T) {
	t.Parallel()
	exp := usageNow.Add(time.Hour)
	tests := []struct {
		name      string
		piAccount string
		piEmail   string
		want      [][]string
		labels    []string
	}{
		{"same account", "acct-1", "me@example.com", [][]string{{"codex", "pi"}}, []string{"me@example.com"}},
		{"different accounts", "acct-2", "other@example.com", [][]string{{"codex"}, {"pi"}}, []string{"me@example.com", "other@example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newIDServer(t)
			codexTok := chatgptJWT(t, exp, "acct-1", "me@example.com")
			piTok := chatgptJWT(t, exp, tc.piAccount, tc.piEmail)
			files := withFiles(piFile(piOpenAI(piTok, exp, tc.piAccount)), map[string]string{
				"/h/.codex/auth.json": fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q,"account_id":"acct-1"}}`, codexTok),
			})
			fl := newFlow(t, srv)

			out := fl.run(newStore(files), "-live")

			snaps := fl.snaps()
			if diff := cmp.Diff(tc.want, harnessSets(snaps, "openai")); diff != "" {
				t.Errorf("harness sets (-want +got):\n%s", diff)
			}
			labels := make([]string, 0, 2)
			for _, s := range snaps {
				labels = append(labels, s.Account)
				if s.Status != usg.StatusOK {
					t.Errorf("row %v status = %s", s.Harnesses, s.Status)
				}
			}
			slices.Sort(labels)
			if diff := cmp.Diff(tc.labels, labels); diff != "" {
				t.Errorf("labels (-want +got):\n%s", diff)
			}
			fl.noSecrets(out, codexTok, piTok)
		})
	}
}

func TestCopilotMergeByLogin(t *testing.T) {
	t.Parallel()
	ocRow := ocCopilotRow(ghOC)
	tests := []struct {
		name    string
		store   kcStore
		harness []string
		tokens  []string
	}{
		{
			"cli, opencode and pi",
			newStore(piFile(piCopilotEntry(ghPi, "")), ocRow).withCopilotCLI(),
			[]string{"copilot", "opencode", "pi"},
			[]string{ghCLI, ghOC, ghPi},
		},
		{"only pi", newStore(piFile(piCopilotEntry(ghPi, ""))), []string{"pi"}, []string{ghPi}},
		{"only opencode", newStore(nil, ocRow), []string{"opencode"}, []string{ghOC}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newIDServer(t)
			srv.logins = map[string]string{ghOC: "octocat", ghPi: "octocat"}
			fl := newFlow(t, srv)

			out := fl.run(tc.store, "-live")

			s := onlySnap(t, fl.snaps(), "github-copilot")
			if s.Account != "octocat" || !slices.Equal(s.Harnesses, tc.harness) || s.IdentityUnknown || s.Status != usg.StatusOK || len(s.Limits) == 0 {
				t.Errorf("row = %+v", s)
			}
			if !strings.Contains(s.ID, "octocat") {
				t.Errorf("id = %q, want the proven identity", s.ID)
			}
			// /user is called once per unproven token, never for the proven CLI login.
			wantUser := len(tc.tokens)
			if slices.Contains(tc.tokens, ghCLI) {
				wantUser--
			}
			if n := srv.count("/user"); n != wantUser {
				t.Errorf("/user requests = %d, want %d", n, wantUser)
			}
			if got := srv.header("/user", "X-Github-Api-Version"); got != "2026-03-10" {
				t.Errorf("api version = %q", got)
			}
			if got := srv.header("/user", "Authorization"); !strings.HasPrefix(got, "token fake-gh-token-") {
				t.Errorf("/user auth scheme wrong")
			}
			fl.noSecrets(out, tc.tokens...)
		})
	}
}

func TestIdentityFailureKeepsAccountsApart(t *testing.T) {
	t.Parallel()
	t.Run("copilot", func(t *testing.T) {
		t.Parallel()
		srv := newIDServer(t)
		srv.failID = true
		store := newStore(piFile(piCopilotEntry(ghPi, ""))).withCopilotCLI()
		fl := newFlow(t, srv)

		out := fl.run(store, "-live")

		if diff := cmp.Diff([][]string{{"copilot"}, {"pi"}}, harnessSets(fl.snaps(), "github-copilot")); diff != "" {
			t.Errorf("rows (-want +got):\n%s", diff)
		}
		rows := rowsOf(out, "github-copilot")
		if len(rows) != 2 {
			t.Fatalf("rendered rows = %q", rows)
		}
		if !strings.Contains(strings.Join(rows, "\n"), "same account? unknown") || !strings.Contains(strings.Join(rows, "\n"), "octocat") {
			t.Errorf("rows = %q, want the pi row unknown and the CLI row labelled", rows)
		}
		fl.noSecrets(out, ghPi, ghCLI)
	})
	t.Run("anthropic next to claude code without a profile", func(t *testing.T) {
		t.Parallel()
		srv := newIDServer(t)
		srv.failID = true
		store := newStore(claudeCreds(antCC), ocAnthropicRow(antOC))
		fl := newFlow(t, srv)

		out := fl.run(store, "-live")

		if diff := cmp.Diff([][]string{{"claude"}, {"opencode"}}, harnessSets(fl.snaps(), "anthropic")); diff != "" {
			t.Errorf("rows (-want +got):\n%s", diff)
		}
		if n := strings.Count(out, "same account? unknown"); n != 2 {
			t.Errorf("unknown labels = %d, want 2:\n%s", n, out)
		}
		fl.noSecrets(out, antCC, antOC)
	})
	t.Run("lone unproven account", func(t *testing.T) {
		t.Parallel()
		srv := newIDServer(t)
		srv.failID = true
		fl := newFlow(t, srv)

		out := fl.run(newStore(piFile(piCopilotEntry(ghPi, ""))), "-live")

		if !strings.Contains(out, "identity unknown") || strings.Contains(out, "same account?") {
			t.Errorf("output = %s", out)
		}
	})
}

func TestAnthropicIdentityFromProfile(t *testing.T) {
	t.Parallel()
	claudeJSON := map[string]string{"/h/.claude.json": `{"oauthAccount":{"accountUuid":"acc-1","emailAddress":"me@example.com","organizationUuid":"org-1"}}`}
	tests := []struct {
		name string
		org  string
		want [][]string
	}{
		{"same account and organization", "org-1", [][]string{{"claude", "opencode"}}},
		{"another organization", "org-2", [][]string{{"claude"}, {"opencode"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newIDServer(t)
			srv.profiles = map[string]string{antOC: profileJSON("acc-1", tc.org)}
			store := newStore(withFiles(claudeCreds(antCC), claudeJSON), ocAnthropicRow(antOC))
			fl := newFlow(t, srv)

			out := fl.run(store, "-live")

			if diff := cmp.Diff(tc.want, harnessSets(fl.snaps(), "anthropic")); diff != "" {
				t.Errorf("rows (-want +got):\n%s", diff)
			}
			for _, s := range fl.snaps() {
				if s.IdentityUnknown || s.Status != usg.StatusOK || s.Account != "me@example.com" {
					t.Errorf("row = %+v", s)
				}
			}
			if n := srv.count("/api/oauth/profile"); n != 1 {
				t.Errorf("profile requests = %d, want 1 (Claude Code is proven locally)", n)
			}
			if got := srv.header("/api/oauth/profile", "Anthropic-Beta"); got != "oauth-2025-04-20" {
				t.Errorf("anthropic-beta = %q", got)
			}
			fl.noSecrets(out, antCC, antOC)
		})
	}
}

func TestCachedViewKeepsLiveMergeAndSplitsOnTokenChange(t *testing.T) {
	t.Parallel()
	srv := newIDServer(t)
	srv.logins = map[string]string{ghOC: "octocat", ghPi: "octocat", ghPi2: "hubot"}
	fl := newFlow(t, srv)
	rows := []provider.OpencodeRow{ocCopilotRow(ghOC)}
	merged := newStore(piFile(piCopilotEntry(ghPi, "")), rows...).withCopilotCLI()

	live := fl.run(merged, "-live")
	if diff := cmp.Diff([][]string{{"copilot", "opencode", "pi"}}, harnessSets(fl.snaps(), "github-copilot")); diff != "" {
		t.Fatalf("live rows (-want +got):\n%s", diff)
	}
	raw, err := os.ReadFile(fl.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"fp:`)) {
		t.Error("cache holds no fingerprints")
	}
	fl.noSecrets(live, ghCLI, ghOC, ghPi)
	before := srv.total()

	// Offline, same credentials: bound by fingerprint, one row with numbers.
	out := fl.run(merged)
	got := rowsOf(out, "github-copilot")
	if len(got) != 1 || !strings.Contains(got[0], "copilot, opencode, pi") || !strings.Contains(got[0], "octocat") {
		t.Errorf("cached rows = %q", got)
	}
	if !strings.Contains(out, "%") {
		t.Errorf("cached row lost its numbers:\n%s", out)
	}
	fl.noSecrets(out, ghCLI, ghOC, ghPi)

	// pi logs in again with another token: unproven, shown apart, no stale numbers.
	switched := newStore(piFile(piCopilotEntry(ghPi2, "")), rows...).withCopilotCLI()
	out = fl.run(switched)
	got = rowsOf(out, "github-copilot")
	if len(got) != 2 {
		t.Fatalf("rows after the token change = %q\n%s", got, out)
	}
	var piRow, mergedRow string
	for _, r := range got {
		if strings.HasSuffix(strings.TrimSpace(r), "pi") {
			piRow = r
		} else {
			mergedRow = r
		}
	}
	if !strings.Contains(piRow, "same account? unknown") || strings.Contains(piRow, "octocat") || strings.Contains(piRow, "hubot") {
		t.Errorf("pi row = %q", piRow)
	}
	if !strings.Contains(mergedRow, "copilot, opencode") || !strings.Contains(mergedRow, "octocat") {
		t.Errorf("merged row = %q", mergedRow)
	}
	pi := out[max(strings.Index(out, piRow), 0):]
	if strings.Contains(strings.SplitN(pi, "\n", 3)[1], "Premium") || !strings.Contains(pi, noDataNote) {
		t.Errorf("pi row has stale numbers or no note:\n%s", pi)
	}
	fl.noSecrets(out, ghCLI, ghOC, ghPi, ghPi2)
	if srv.total() != before {
		t.Errorf("the cached view made %d requests", srv.total()-before)
	}
}

func TestExpiredPiOpenAIMakesNoRequest(t *testing.T) {
	t.Parallel()
	srv := newIDServer(t)
	exp := usageNow.Add(-time.Hour)
	access := chatgptJWT(t, exp, "acct-pi", "me@example.com")
	store := newStore(piFile(piOpenAI(access, exp, "acct-pi")))
	fl := newFlow(t, srv)

	for _, args := range [][]string{{"-live"}, nil} {
		out := fl.run(store, args...)
		rows := rowsOf(out, "openai")
		if len(rows) != 1 || !strings.Contains(rows[0], "me@example.com") || !strings.Contains(strings.ToLower(rows[0]), "plus") {
			t.Errorf("rows = %q", rows)
		}
		if !strings.Contains(out, "token expired, open pi to refresh") {
			t.Errorf("no expiry note:\n%s", out)
		}
		fl.noSecrets(out, access)
	}
	if n := srv.total(); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
}

func TestEnterpriseCopilotHostNeverGetsIdentityRequest(t *testing.T) {
	t.Parallel()
	srv := newIDServer(t)
	srv.logins = map[string]string{ghEnt: "octocat"}
	fl := newFlow(t, srv)

	out := fl.run(newStore(piFile(piCopilotEntry(ghEnt, "github.example.com"))), "-live")

	if n := srv.total(); n != 0 {
		t.Errorf("requests = %d, want 0 for an enterprise host (paths %v)", n, srv.hits)
	}
	if s := onlySnap(t, fl.snaps(), "github-copilot"); s.Status != usg.StatusNotVisible || !s.IdentityUnknown {
		t.Errorf("row = %+v", s)
	}
	fl.noSecrets(out, ghEnt)
}

func TestCodexSessionsAreNotUsedForPiOpenAI(t *testing.T) {
	t.Parallel()
	srv := newIDServer(t)
	srv.failOAI = true
	exp := usageNow.Add(time.Hour)
	access := chatgptJWT(t, exp, "acct-pi", "me@example.com")
	piOnly := newStore(piFile(piOpenAI(access, exp, "acct-pi")))
	withCodex := newStore(withFiles(piFile(piOpenAI(access, exp, "acct-pi")), map[string]string{
		"/h/.codex/auth.json": fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q,"account_id":"acct-pi"}}`, access),
	}))

	run := func(store kcStore, args ...string) (*flow, usg.Snapshot) {
		fl := newFlow(t, srv)
		f := testFetcher(t, srv.Server)
		writeCodexSession(t, f.home)
		store = rehome(store, f.home)
		fl.out.Reset()
		if code := fl.d.runUsageWith(t.Context(), args, store, f); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if len(args) == 0 {
			return fl, usg.Snapshot{}
		}
		return fl, onlySnap(t, fl.snaps(), "openai")
	}

	if _, s := run(piOnly, "-live"); s.Source == "codex-session" || s.Status != usg.StatusUnavailable {
		t.Errorf("live pi-only row = %+v, want unavailable without Codex numbers", s)
	}
	fl, _ := run(piOnly)
	if strings.Contains(fl.out.String(), "12%") || !strings.Contains(fl.out.String(), noDataNote) {
		t.Errorf("cached pi-only view used Codex's sessions:\n%s", fl.out.String())
	}
	// A Codex login (alone or merged with pi) may still fall back to them.
	if _, s := run(withCodex, "-live"); s.Source != "codex-session" || s.Status != usg.StatusStale {
		t.Errorf("codex row = %+v, want the session fallback", s)
	}
}

// rehome moves the store's home, and the files under it, to home.
func rehome(s kcStore, home string) kcStore {
	files := make(map[string]string, len(s.files))
	for k, v := range s.files {
		files[strings.Replace(k, s.home, home, 1)] = v
	}
	s.files, s.home = files, home
	return s
}
