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

// identTokens returns a fake ChatGPT access token carrying the account, the
// user, the plan and the profile email.
func identTokens(t *testing.T, exp time.Time, accountID, userID string) string {
	t.Helper()
	return jwt(t, map[string]any{
		"exp": exp.Unix(),
		openaiAuthClaim: map[string]any{
			"chatgpt_account_id": accountID, "chatgpt_user_id": userID, "chatgpt_plan_type": "plus",
		},
		openaiProfileClaim: map[string]any{"email": "me@example.com"},
	})
}

func piCodexFile(access string, exp time.Time, accountID string) string {
	return fmt.Sprintf(`{"openai-codex":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d,"accountId":%q}}`,
		access, exp.UnixMilli(), accountID)
}

func TestPiCodexLoginOnly(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	access := identTokens(t, exp, "acct-1", "user-1")
	f := newFake("linux")
	f.files[f.path(".pi", "agent", "auth.json")] = piCodexFile(access, exp, "acct-1")

	got := Discover(f)
	if len(got) != 1 {
		t.Fatalf("got %d accounts, want 1: %+v", len(got), views(got))
	}
	a := got[0]
	const key = "openai:acct-1:user-1"
	if a.Provider != OpenAI || a.Label != "me@example.com" || a.Key != key || a.ID != key || !a.IdentityKnown {
		t.Errorf("account = %+v", views(got))
	}
	if a.Plan != usage.OpenAIPlan("plus") {
		t.Errorf("Plan = %+v", a.Plan)
	}
	if a.Cred.Token() != access || a.Cred.AccountID() != "acct-1" {
		t.Errorf("credential not carried")
	}
}

func TestPiCodexExpired(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(-time.Hour)
	f := newFake("linux")
	f.files[f.path(".pi", "agent", "auth.json")] = piCodexFile(identTokens(t, exp, "acct-1", "user-1"), exp, "acct-1")
	got := readPi(f)
	if len(got) != 1 {
		t.Fatalf("got %d accounts", len(got))
	}
	a := got[0]
	if a.Label != "me@example.com" || a.Key != "openai:acct-1:user-1" || a.Plan != usage.OpenAIPlan("plus") {
		t.Errorf("identity lost on an expired token: %+v", views(got))
	}
	if a.Status != usage.StatusExpired || a.Note != "token expired, open pi to refresh" {
		t.Errorf("status = %q, note = %q", a.Status, a.Note)
	}
}

func TestPiOpenAIKeysDoNotCollideUnproven(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.files[f.path(".pi", "agent", "auth.json")] = `{
	  "openai":{"type":"oauth","access":"fake-a","refresh":"fake-r"},
	  "openai-codex":{"type":"oauth","access":"fake-b","refresh":"fake-r"}
	}`
	ids := make([]string, 0, 2)
	for _, a := range readPi(f) {
		ids = append(ids, a.ID)
	}
	if diff := cmp.Diff([]string{"pi:openai", "pi:openai-codex"}, ids); diff != "" {
		t.Errorf("ids (-want +got):\n%s", diff)
	}
}

func TestDiscoverPiAndCodexSameOrDifferentAccount(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	tests := []struct {
		name          string
		piAcct, piUsr string
		wantAccounts  int
	}{
		{"same account", "acct-1", "user-1", 1},
		{"different account", "acct-2", "user-1", 2},
		{"different user", "acct-1", "user-2", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake("linux")
			codexAccess := identTokens(t, exp, "acct-1", "user-1")
			f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, codexAccess, "", "acct-1")
			f.files[f.path(".pi", "agent", "auth.json")] = piCodexFile(identTokens(t, exp, tt.piAcct, tt.piUsr), exp, tt.piAcct)
			got := Discover(f)
			if len(got) != tt.wantAccounts {
				t.Fatalf("got %d accounts, want %d: %+v", len(got), tt.wantAccounts, views(got))
			}
			if tt.wantAccounts == 1 {
				if diff := cmp.Diff([]string{"codex", "pi"}, got[0].Harnesses); diff != "" {
					t.Errorf("harnesses (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestOpenAIClaimFallbacks(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	tests := []struct {
		name      string
		access    map[string]any
		id        map[string]any
		accountID string
		wantKey   string
		wantLabel string
	}{
		{
			name:      "user_id and id token profile email",
			access:    map[string]any{openaiAuthClaim: map[string]any{"chatgpt_account_id": "a", "user_id": "u"}},
			id:        map[string]any{openaiProfileClaim: map[string]any{"email": "id@example.com"}},
			wantKey:   "openai:a:u",
			wantLabel: "id@example.com",
		},
		{
			name:      "everything from the id token",
			access:    map[string]any{"exp": exp.Unix()},
			id:        map[string]any{"email": "id@example.com", openaiAuthClaim: map[string]any{"chatgpt_account_id": "b", "chatgpt_user_id": "v"}},
			wantKey:   "openai:b:v",
			wantLabel: "id@example.com",
		},
		{
			name:      "account argument wins",
			access:    map[string]any{openaiAuthClaim: map[string]any{"chatgpt_account_id": "a", "chatgpt_user_id": "u"}},
			accountID: "arg",
			wantKey:   "openai:arg:u",
		},
		{
			name:      "email from the access token",
			access:    map[string]any{"email": "acc@example.com", openaiAuthClaim: map[string]any{"user_id": "u2"}},
			accountID: "arg",
			wantKey:   "openai:arg:u2",
			wantLabel: "acc@example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := openaiOAuth("codex", jwt(t, tt.access), jwt(t, tt.id), tt.accountID)
			if a.Key != tt.wantKey || a.ID != tt.wantKey || a.Label != tt.wantLabel || !a.IdentityKnown {
				t.Errorf("got key %q id %q label %q known %v", a.Key, a.ID, a.Label, a.IdentityKnown)
			}
		})
	}

	t.Run("account id without any user claim is unproven", func(t *testing.T) {
		t.Parallel()
		access := jwt(t, map[string]any{"exp": exp.Unix(), openaiAuthClaim: map[string]any{"chatgpt_account_id": "a"}})
		a := openaiOAuth("codex", access, "", "")
		if a.IdentityKnown || a.ID != "codex" || a.Key != fingerprint(OpenAI, access) {
			t.Errorf("got key %q id %q known %v", a.Key, a.ID, a.IdentityKnown)
		}
	})

	t.Run("a sub claim never identifies the user", func(t *testing.T) {
		t.Parallel()
		// pi salts sub per client, so it is no account identity.
		access := jwt(t, map[string]any{"exp": exp.Unix(), "sub": "s1", "email": "acc@example.com", openaiAuthClaim: map[string]any{"chatgpt_account_id": "a"}})
		a := openaiOAuth("codex", access, "", "")
		if a.IdentityKnown || a.Key != fingerprint(OpenAI, access) || a.Label != "acc@example.com" {
			t.Errorf("got key %q label %q known %v", a.Key, a.Label, a.IdentityKnown)
		}
	})

	t.Run("no account id is unproven", func(t *testing.T) {
		t.Parallel()
		access := jwt(t, map[string]any{"exp": exp.Unix()})
		a := openaiOAuth("codex", access, "", "")
		if a.IdentityKnown || a.ID != "codex" || a.Key != fingerprint(OpenAI, access) {
			t.Errorf("got key %q id %q known %v", a.Key, a.ID, a.IdentityKnown)
		}
	})
}

// copilotTenantStore holds the same GitHub login in opencode and pi, with the
// short-lived API token (access) already past its stored expiry.
func copilotTenantStore(past time.Time, piRefresh string) *fakeStore {
	f := newFake("linux")
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = []OpencodeRow{
		{ID: "c1", IntegrationID: "github-copilot", Active: true, Value: ocValueJSON("oauth", "fake-api-token", "fake-gh-token", "", past, "")},
	}
	f.files[f.path(".pi", "agent", "auth.json")] = fmt.Sprintf(
		`{"github-copilot":{"type":"oauth","access":"fake-api-token","refresh":%q,"expires":%d}}`, piRefresh, past.UnixMilli())
	return f
}

func TestTenantCopilotUnproven(t *testing.T) {
	t.Parallel()
	f := copilotTenantStore(testNow.Add(-time.Hour), "fake-gh-token")
	fp := fingerprint(GitHubCopilot, "fake-gh-token")
	oc, pi := readOpencode(f), readPi(f)
	if len(oc) != 1 || len(pi) != 1 {
		t.Fatalf("got %d opencode, %d pi accounts", len(oc), len(pi))
	}
	for _, tt := range []struct {
		a      Account
		wantID string
	}{{oc[0], "opencode:c1"}, {pi[0], "pi:github-copilot"}} {
		a := tt.a
		if a.IdentityKnown || a.Key != fp || a.ID != tt.wantID {
			t.Errorf("%s: known %v key %q id %q", a.Harnesses, a.IdentityKnown, a.Key, a.ID)
		}
		if diff := cmp.Diff([]string{fp}, a.Fingerprints); diff != "" {
			t.Errorf("fingerprints (-want +got):\n%s", diff)
		}
		if !a.Expires.IsZero() || a.Status != "" {
			t.Errorf("%s: the API token expiry must not apply: expires %v status %q", a.Harnesses, a.Expires, a.Status)
		}
		if a.Cred.Token() != "fake-gh-token" {
			t.Errorf("%s: credential is not the GitHub OAuth token", a.Harnesses)
		}
	}
}

func TestPiCopilotAccessFallbackKeepsExpiry(t *testing.T) {
	t.Parallel()
	f := copilotTenantStore(testNow.Add(-time.Hour), "")
	got := readPi(f)
	if len(got) != 1 || got[0].Status != usage.StatusExpired || got[0].Expires.IsZero() {
		t.Errorf("got %+v", views(got))
	}
}

// identifyUnproven proves every account of accs that is not yet proven.
func identifyUnproven(accs []Account, id Identity) []Account {
	out := make([]Account, 0, len(accs))
	for _, a := range accs {
		if !a.IdentityKnown {
			a = Identify(a, id)
		}
		out = append(out, a)
	}
	return out
}

func TestCopilotMergeAcrossHarnesses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		piRefresh string
		wantFPs   []string
	}{
		{"same token", "fake-gh-token", []string{fingerprint(GitHubCopilot, "fake-gh-token")}},
		{"two tokens", "fake-gh-token-2", unionFingerprints(
			[]string{fingerprint(GitHubCopilot, "fake-gh-token")}, []string{fingerprint(GitHubCopilot, "fake-gh-token-2")})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := copilotTenantStore(testNow.Add(-time.Hour), tt.piRefresh)
			f.files[f.path(".copilot", "config.json")] = `{"loggedInUsers":["Octocat"],"lastLoggedInUser":"Octocat"}`
			id := Identity{Key: CopilotKey("https://github.com", "Octocat"), Label: "octocat", Login: "octocat"}
			all := slices.Concat(readOpencode(f), readPi(f), readCopilot(f))
			got := Merge(identifyUnproven(all, id))
			if len(got) != 1 {
				t.Fatalf("got %d accounts, want 1: %+v", len(got), views(got))
			}
			if diff := cmp.Diff([]string{"copilot", "opencode", "pi"}, got[0].Harnesses); diff != "" {
				t.Errorf("harnesses (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantFPs, got[0].Fingerprints); diff != "" {
				t.Errorf("fingerprints (-want +got):\n%s", diff)
			}
			if got[0].Key != id.Key || !got[0].IdentityKnown {
				t.Errorf("key %q known %v", got[0].Key, got[0].IdentityKnown)
			}
		})
	}
}

func TestCopilotProvenKey(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.files[f.path(".copilot", "config.json")] = `{"loggedInUsers":[{"login":"Octocat","host":"GHE.example.com"}]}`
	got := readCopilot(f)
	if len(got) != 1 {
		t.Fatalf("got %d accounts", len(got))
	}
	const want = "copilot:https://ghe.example.com:octocat"
	if got[0].Key != want || got[0].ID != want || !got[0].IdentityKnown || got[0].Label != "Octocat" {
		t.Errorf("got key %q id %q label %q", got[0].Key, got[0].ID, got[0].Label)
	}
}

type claudeProfileCase struct {
	name      string
	env       map[string]string
	files     map[string]string // relative to the fake home unless absolute
	wantKey   string
	wantLabel string
	known     bool
}

func TestClaudeProfile(t *testing.T) {
	t.Parallel()
	profile := `{"oauthAccount":{"accountUuid":"u-1","emailAddress":"me@example.com","organizationUuid":"o-1"},"other":"x"}`
	unprovenKey := fingerprint(Anthropic, "fake-token-1")
	tests := []claudeProfileCase{
		{
			name: "home profile", files: map[string]string{".claude.json": profile},
			wantKey: "anthropic:u-1:o-1", wantLabel: "me@example.com", known: true,
		},
		{
			name: "CLAUDE_CONFIG_DIR profile", env: map[string]string{"CLAUDE_CONFIG_DIR": "/cfg"},
			files: map[string]string{
				"/cfg/.credentials.json": claudeCredsJSON(testNow.Add(time.Hour)),
				"/cfg/.claude.json":      profile,
				".claude.json":           `{"oauthAccount":{"accountUuid":"u-home"}}`,
			},
			wantKey: "anthropic:u-1:o-1", wantLabel: "me@example.com", known: true,
		},
		{name: "missing", wantKey: unprovenKey},
		{name: "malformed", files: map[string]string{".claude.json": `{ broken fake-token-1`}, wantKey: unprovenKey},
		{name: "no account uuid", files: map[string]string{".claude.json": `{"oauthAccount":{"emailAddress":"x@example.com"}}`}, wantKey: unprovenKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkClaudeProfile(t, tt)
		})
	}
}

func checkClaudeProfile(t *testing.T, tt claudeProfileCase) {
	t.Helper()
	f := newFake("linux")
	f.files[f.path(".claude", ".credentials.json")] = claudeCredsJSON(testNow.Add(time.Hour))
	if tt.env["CLAUDE_CONFIG_DIR"] != "" {
		delete(f.files, f.path(".claude", ".credentials.json")) // the env dir alone, not next to a default login
	}
	maps.Copy(f.env, tt.env)
	for p, content := range tt.files {
		if !strings.HasPrefix(p, "/") {
			p = f.path(p)
		}
		f.files[p] = content
	}
	got := readClaude(f)
	if len(got) != 1 {
		t.Fatalf("got %d accounts", len(got))
	}
	a := got[0]
	if a.Key != tt.wantKey || a.Label != tt.wantLabel || a.IdentityKnown != tt.known {
		t.Errorf("key %q label %q known %v", a.Key, a.Label, a.IdentityKnown)
	}
	if tt.known && a.ID != tt.wantKey {
		t.Errorf("ID %q != Key", a.ID)
	}
	if !tt.known && len(a.Fingerprints) != 1 {
		t.Errorf("fingerprints = %v", a.Fingerprints)
	}
}

func TestFingerprint(t *testing.T) {
	t.Parallel()
	const token = "fake-token-1"
	fp := fingerprint(Anthropic, token)
	if strings.Contains(fp, token) || !strings.HasPrefix(fp, "fp:") {
		t.Errorf("fingerprint = %q", fp)
	}
	tests := []struct {
		name     string
		provider string
		token    string
		wantSame bool
		wantZero bool
	}{
		{"same input", Anthropic, token, true, false},
		{"other token", Anthropic, "fake-token-2", false, false},
		{"other provider", OpenAI, token, false, false},
		{"empty token", Anthropic, "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fingerprint(tt.provider, tt.token)
			if tt.wantZero != (got == "") || (!tt.wantZero && (got == fp) != tt.wantSame) {
				t.Errorf("fingerprint(%q, %q) = %q (reference %q)", tt.provider, tt.token, got, fp)
			}
		})
	}
}

func TestAccountFormatNeverLeaksToken(t *testing.T) {
	t.Parallel()
	const secret = "fake-token-1"
	a := Account{Provider: Anthropic, Cred: NewCredential(secret, "acct-1")}
	unproven(&a, secret)
	for _, verb := range []string{"%v", "%+v", "%#v"} {
		if out := fmt.Sprintf(verb, a); strings.Contains(out, secret) {
			t.Errorf("Sprintf(%q, Account) leaks the token: %q", verb, out)
		}
	}
}

func TestSharedWorkspaceKeepsUsersApart(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	exp := testNow.Add(time.Hour)
	mk := func(user, email string) string {
		return jwt(t, map[string]any{
			"exp":              exp.Unix(),
			openaiAuthClaim:    map[string]any{"chatgpt_account_id": "ws-1", "chatgpt_user_id": user},
			openaiProfileClaim: map[string]any{"email": email},
		})
	}
	f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, mk("user-1", "a@example.com"), "", "ws-1")
	f.files[f.path(".pi", "agent", "auth.json")] = fmt.Sprintf(
		`{"openai-codex":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d,"accountId":"ws-1"}}`,
		mk("user-2", "b@example.com"), exp.UnixMilli())
	var keys []string
	for _, a := range Discover(f) {
		if a.Provider == OpenAI {
			keys = append(keys, a.Key)
		}
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want 2 rows", keys)
	}
}
