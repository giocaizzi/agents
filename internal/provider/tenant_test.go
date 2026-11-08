package provider

import (
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"
)

func ocValueJSON(typ, access, refresh, key string, expires time.Time, accountID string) []byte {
	meta := "{}"
	if accountID != "" {
		meta = fmt.Sprintf(`{"accountId":%q}`, accountID)
	}
	return fmt.Appendf(nil, `{"type":%q,"access":%q,"refresh":%q,"key":%q,"expires":%d,"metadata":%s}`,
		typ, access, refresh, key, expires.UnixMilli(), meta)
}

func TestOpencodeDB(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	access, _ := chatgptTokens(t, exp, "acct-1")
	f := newFake("linux")
	db := f.path(".local", "share", "opencode", "opencode.db")
	f.rows[db] = []OpencodeRow{
		{ID: "cred_1", IntegrationID: "openai", Active: true, Value: ocValueJSON("oauth", access, "fake-refresh-1", "", exp, "acct-1")},
		{ID: "cred_2", IntegrationID: "github-copilot", Active: true, Value: ocValueJSON("oauth", "fake-gh-access", "fake-gh-token", "", exp, "")},
		{ID: "cred_3", IntegrationID: "openrouter", Label: "work", Active: true, Value: ocValueJSON("key", "", "", "fake-key-1", time.Time{}, "")},
		{ID: "cred_4", IntegrationID: "openai", Active: false, Value: ocValueJSON("oauth", "fake-other", "fake-r", "", exp, "acct-2")},
		{ID: "cred_5", IntegrationID: "corp", Label: "sso", Active: true, Value: []byte(`{"type":"external"}`)},
		{ID: "mcp_notion", IntegrationID: "notion", Active: true, Value: ocValueJSON("oauth", "fake-mcp", "", "", exp, "")},
		{ID: "cred_6", IntegrationID: "mcp-linear", Active: true, Value: ocValueJSON("oauth", "fake-mcp2", "", "", exp, "")},
	}
	exp0 := time.UnixMilli(exp.UnixMilli())
	assertViews(t, readOpencode(f), []view{
		{Provider: "openai", Key: "openai:acct-1:user-1", Harnesses: []string{"opencode"}, Auth: AuthOAuth, Token: access, AccountID: "acct-1", Expires: exp0, Source: "sqlite"},
		{Provider: "github-copilot", Key: fingerprint(GitHubCopilot, "fake-gh-token"), Harnesses: []string{"opencode"}, Auth: AuthOAuth, Token: "fake-gh-token", Source: "sqlite"},
		{Provider: "openrouter", Label: "work", Harnesses: []string{"opencode"}, Auth: AuthAPIKey, Status: "no-api", Note: "no usage API for API keys", Source: "sqlite"},
		{Provider: "openai", Key: fingerprint(OpenAI, "fake-other"), Harnesses: []string{"opencode"}, Auth: AuthOAuth, Token: "fake-other", AccountID: "acct-2", Expires: exp0, Note: "(inactive)", Source: "sqlite"},
		{Provider: "corp", Label: "sso", Harnesses: []string{"opencode"}, Auth: AuthAPIKey, Status: "not-visible", Note: "external credential", Source: "sqlite"},
	})
}

func TestOpencodeOpenAIAccountFromJWT(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	access, _ := chatgptTokens(t, exp, "acct-7")
	f := newFake("linux")
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = []OpencodeRow{
		{ID: "cred_1", IntegrationID: "openai", Active: true, Value: fmt.Appendf(nil, `{"type":"oauth","access":%q}`, access)},
	}
	got := readOpencode(f)
	if len(got) != 1 || got[0].Key != "openai:acct-7:user-1" || got[0].Cred.AccountID() != "acct-7" {
		t.Errorf("got %+v", views(got))
	}
}

func TestOpencodeLocations(t *testing.T) {
	t.Parallel()
	row := []OpencodeRow{{ID: "c", IntegrationID: "openrouter", Active: true, Value: []byte(`{"type":"key","key":"fake-key-1"}`)}}
	for name, tc := range map[string]struct {
		goos string
		env  map[string]string
		db   string
	}{
		"OPENCODE_DB":     {"linux", map[string]string{"OPENCODE_DB": "/x/o.db"}, "/x/o.db"},
		"XDG_DATA_HOME":   {"linux", map[string]string{"XDG_DATA_HOME": "/xdg"}, "/xdg/opencode/opencode.db"},
		"windows LOCALAP": {"windows", map[string]string{"LOCALAPPDATA": "/local"}, "/local/opencode/opencode.db"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFake(tc.goos)
			maps.Copy(f.env, tc.env)
			f.rows[filepathFromSlash(tc.db)] = row
			if got := readOpencode(f); len(got) != 1 {
				t.Errorf("got %d accounts, want 1", len(got))
			}
		})
	}
}

func TestOpencodeLegacyWhenNoDB(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	f := newFake("linux")
	f.files[f.path(".local", "share", "opencode", "auth.json")] = fmt.Sprintf(`{
	  "github-copilot":{"type":"oauth","access":"fake-a","refresh":"fake-gh-token","expires":%d},
	  "anthropic":{"type":"api","key":"fake-key-1"},
	  "corp":{"type":"wellknown","key":"fake-k","token":"fake-t"}
	}`, exp.UnixMilli())
	got := readOpencode(f)
	assertViews(t, got, []view{
		{Provider: "anthropic", Harnesses: []string{"opencode"}, Auth: AuthAPIKey, Status: "no-api", Note: "no usage API for API keys", Source: "file"},
		{Provider: "corp", Harnesses: []string{"opencode"}, Auth: AuthAPIKey, Status: "not-visible", Note: "external credential", Source: "file"},
		{Provider: "github-copilot", Key: fingerprint(GitHubCopilot, "fake-gh-token"), Harnesses: []string{"opencode"}, Auth: AuthOAuth, Token: "fake-gh-token", Source: "file"},
	})
}

func TestOpencodeDBPreferredOverLegacy(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.files[f.path(".local", "share", "opencode", "auth.json")] = `{"anthropic":{"type":"api","key":"fake-key-1"}}`
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = nil
	if got := readOpencode(f); len(got) != 0 {
		t.Errorf("legacy store read although the database exists: %+v", views(got))
	}
}

func TestOpencodeUnreadableDB(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.rowsErr = errors.New("database is locked")
	assertViews(t, readOpencode(f), []view{{Provider: "opencode", Harnesses: []string{"opencode"}, Auth: AuthNone, Status: "unavailable", Note: "credentials unreadable"}})
}

func TestPi(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	access, _ := chatgptTokens(t, exp, "acct-1")
	f := newFake("linux")
	f.files[f.path(".pi", "agent", "auth.json")] = fmt.Sprintf(`{
	  "openai-codex":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d},
	  "github-copilot":{"type":"oauth","access":"fake-a","refresh":"fake-gh-token","expires":%d},
	  "openrouter":{"type":"api_key","key":"fake-key-1"},
	  "deepseek":{"type":"api_key","key":"!op read secret"},
	  "anthropic":{"type":"oauth","access":"fake-claude","refresh":"fake-r","expires":%d}
	}`, access, exp.UnixMilli(), exp.UnixMilli(), testNow.Add(-time.Hour).UnixMilli())
	e0 := time.UnixMilli(exp.UnixMilli())
	assertViews(t, readPi(f), []view{
		{Provider: "anthropic", Key: fingerprint(Anthropic, "fake-claude"), Harnesses: []string{"pi"}, Auth: AuthOAuth, Token: "fake-claude", Expires: time.UnixMilli(testNow.Add(-time.Hour).UnixMilli()), Status: "expired", Note: "token expired, open pi to refresh", Source: "file"},
		{Provider: "deepseek", Harnesses: []string{"pi"}, Auth: AuthCommand, Status: "skipped", Note: "key from a command, not run", Source: "file"},
		{Provider: "github-copilot", Key: fingerprint(GitHubCopilot, "fake-gh-token"), Harnesses: []string{"pi"}, Auth: AuthOAuth, Token: "fake-gh-token", Source: "file"},
		{Provider: "openai", Key: "openai:acct-1:user-1", Harnesses: []string{"pi"}, Auth: AuthOAuth, Token: access, AccountID: "acct-1", Expires: e0, Source: "file"},
		{Provider: "openrouter", Harnesses: []string{"pi"}, Auth: AuthAPIKey, Status: "no-api", Note: "no usage API for API keys", Source: "file"},
	})
}

func TestTenantCopilotHost(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)

	f := newFake("linux")
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = []OpencodeRow{
		{ID: "c1", IntegrationID: "github-copilot", Active: true, Value: []byte(`{"type":"oauth","access":"fake-a","refresh":"fake-gh-1","metadata":{"enterpriseUrl":"acme.ghe.com"}}`)},
		{ID: "c2", IntegrationID: "github-copilot", Active: true, Value: []byte(`{"type":"oauth","access":"fake-a","refresh":"fake-gh-2","metadata":{"enterpriseUrl":"https://ghe.example.com/"}}`)},
		{ID: "c3", IntegrationID: "github-copilot", Active: true, Value: []byte(`{"type":"oauth","access":"fake-a","refresh":"fake-gh-3"}`)},
	}
	hosts := make([]string, 0, 3)
	for _, a := range readOpencode(f) {
		hosts = append(hosts, a.Host)
	}
	if want := []string{"https://acme.ghe.com", "https://ghe.example.com", "https://github.com"}; fmt.Sprint(hosts) != fmt.Sprint(want) {
		t.Errorf("opencode hosts = %v, want %v", hosts, want)
	}

	legacy := newFake("linux")
	legacy.files[legacy.path(".local", "share", "opencode", "auth.json")] = fmt.Sprintf(
		`{"github-copilot":{"type":"oauth","access":"fake-a","refresh":"fake-gh","expires":%d,"enterpriseUrl":"acme.ghe.com"}}`, exp.UnixMilli())
	if got := readOpencode(legacy); len(got) != 1 || got[0].Host != "https://acme.ghe.com" {
		t.Errorf("legacy opencode: %+v", views(got))
	}

	for name, entry := range map[string]string{
		"enterpriseUrl":    `{"type":"oauth","access":"fake-a","refresh":"fake-gh","enterpriseUrl":"acme.ghe.com"}`,
		"enterpriseDomain": `{"type":"oauth","access":"fake-a","refresh":"fake-gh","enterpriseDomain":"https://acme.ghe.com"}`,
	} {
		p := newFake("linux")
		p.files[p.path(".pi", "agent", "auth.json")] = `{"github-copilot":` + entry + `}`
		if got := readPi(p); len(got) != 1 || got[0].Host != "https://acme.ghe.com" {
			t.Errorf("pi %s: %+v", name, views(got))
		}
	}
	p := newFake("linux")
	p.files[p.path(".pi", "agent", "auth.json")] = `{"github-copilot":{"type":"oauth","access":"fake-a","refresh":"fake-gh"}}`
	if got := readPi(p); len(got) != 1 || got[0].Host != "https://github.com" {
		t.Errorf("pi default: %+v", views(got))
	}
}

func TestAnthropicTenantDoesNotMergeWithClaude(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.files[f.path(".claude", ".credentials.json")] = claudeCredsJSON(testNow.Add(time.Hour))
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = []OpencodeRow{
		{ID: "c1", IntegrationID: "anthropic", Active: true, Value: ocValueJSON("oauth", "fake-oc-access", "fake-r", "", testNow.Add(time.Hour), "")},
	}
	var n int
	for _, a := range Discover(f) {
		if a.Provider == Anthropic {
			n++
		}
	}
	if n != 2 {
		t.Errorf("got %d anthropic accounts, want 2", n)
	}
}

func TestPiDir(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.env["PI_CODING_AGENT_DIR"] = "/pi"
	f.files["/pi/auth.json"] = `{"openrouter":{"type":"api_key","key":"fake-key-1"}}`
	if got := readPi(f); len(got) != 1 {
		t.Errorf("got %d accounts", len(got))
	}
	f.files["/pi/auth.json"] = `{ broken fake-key-1`
	assertViews(t, readPi(f), []view{{Provider: "pi", Harnesses: []string{"pi"}, Auth: AuthNone, Status: "unavailable", Note: "credentials unreadable"}})
	assertViews(t, readPi(newFake("linux")), nil)
}
