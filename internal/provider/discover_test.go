package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giocaizzi/agents/internal/usage"
)

func filepathFromSlash(s string) string { return filepath.FromSlash(s) }

// fullStore logs one ChatGPT account into codex, opencode and pi, with the
// access token of each expiring at a different time.
func fullStore(t *testing.T) *fakeStore {
	t.Helper()
	f := newFake("linux")
	codexExp := testNow.Add(1 * time.Hour)
	ocExp := testNow.Add(3 * time.Hour) // the newest
	piExp := testNow.Add(2 * time.Hour)

	cAccess, cID := chatgptTokens(t, codexExp, "acct-1")
	f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, cAccess, cID, "acct-1")

	ocAccess, _ := chatgptTokens(t, ocExp, "acct-1")
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = []OpencodeRow{
		{ID: "c1", IntegrationID: "openai", Active: true, Value: ocValueJSON("oauth", ocAccess, "fake-r", "", ocExp, "acct-1")},
	}

	piAccess, _ := chatgptTokens(t, piExp, "acct-1")
	f.files[f.path(".pi", "agent", "auth.json")] = fmt.Sprintf(
		`{"openai-codex":{"type":"oauth","access":%q,"refresh":"fake-r","expires":%d}}`, piAccess, piExp.UnixMilli())

	f.files[f.path(".claude", ".credentials.json")] = claudeCredsJSON(testNow.Add(time.Hour))
	f.files[f.path(".copilot", "config.json")] = `{"loggedInUsers":["octocat"],"lastLoggedInUser":"octocat"}`
	f.secrets["copilot-cli\x00https://github.com:octocat:github"] = "fake-token-1"
	f.files[f.path(".gemini", "antigravity-cli", "x")] = ""
	return f
}

func TestDiscoverMergesOneChatGPTAccount(t *testing.T) {
	t.Parallel()
	f := fullStore(t)
	got := Discover(f)

	providers := make([]string, 0, len(got))
	for _, a := range got {
		providers = append(providers, a.Provider)
	}
	if want := "anthropic,github-copilot,google,openai"; strings.Join(providers, ",") != want {
		t.Fatalf("providers = %v, want %s (sorted, openai merged)", providers, want)
	}
	oa := got[3]
	if want := "codex,opencode,pi"; strings.Join(oa.Harnesses, ",") != want {
		t.Errorf("harnesses = %v, want %s", oa.Harnesses, want)
	}
	if oa.Label != "me@example.com" || oa.Key != "openai:acct-1:user-1" {
		t.Errorf("label/key = %q/%q", oa.Label, oa.Key)
	}
	// The opencode token expires last, so it is the newest and wins.
	if want := testNow.Add(3 * time.Hour).Unix(); oa.Expires.Unix() != want {
		t.Errorf("Expires = %v, want the latest", oa.Expires)
	}
	ocAccess, _ := chatgptTokens(t, testNow.Add(3*time.Hour), "acct-1")
	if oa.Cred.Token() != ocAccess {
		t.Errorf("credential is not the newest one")
	}
}

func TestMergeLaterExpiryWins(t *testing.T) {
	t.Parallel()
	a := Account{Provider: "openai", Key: "k", Harnesses: []string{"codex"}, Expires: testNow.Add(time.Hour), Cred: NewCredential("fake-token-1", "")}
	b := Account{Provider: "openai", Key: "k", Harnesses: []string{"pi", "codex"}, Expires: testNow.Add(5 * time.Hour), Label: "me@example.com", Cred: NewCredential("fake-token-2", "")}
	got := merge(a, b)
	if got.Cred.Token() != "fake-token-2" {
		t.Errorf("the later-expiring account should win")
	}
	if got.Label != "me@example.com" {
		t.Errorf("label not taken from the other account")
	}
}

func TestDiscoverKeepsAccountsWithoutKeySeparate(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.files[f.path(".pi", "agent", "auth.json")] = `{"openrouter":{"type":"api_key","key":"fake-key-1"}}`
	f.rows[f.path(".local", "share", "opencode", "opencode.db")] = []OpencodeRow{
		{ID: "c", IntegrationID: "openrouter", Label: "b", Active: true, Value: []byte(`{"type":"key","key":"fake-key-1"}`)},
	}
	got := Discover(f)
	if len(got) != 2 {
		t.Fatalf("got %d accounts, want 2 (no key, no merge)", len(got))
	}
	if got[0].Label != "" || got[1].Label != "b" {
		t.Errorf("not sorted by label: %q, %q", got[0].Label, got[1].Label)
	}
}

func TestDiscoverEmpty(t *testing.T) {
	t.Parallel()
	if got := Discover(newFake("linux")); len(got) != 0 {
		t.Errorf("got %+v", views(got))
	}
}

func TestRedaction(t *testing.T) {
	t.Parallel()
	const secret = "fake-token-1"
	a := Account{
		Provider: "anthropic", Label: "me@example.com", Key: "claude-oauth",
		Harnesses: []string{"claude"}, Auth: AuthOAuth, Plan: usage.Plan{Kind: usage.KindIndividual},
		Cred: NewCredential(secret, "acct-1"),
	}
	c := a.Cred

	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).LogAttrs(context.Background(), slog.LevelInfo, "found", slog.Any("account", a), slog.Any("cred", c), slog.Any("cred_ptr", &c))

	js, err := json.Marshal(a) //nolint:musttag // Account is untagged by design; the test asserts its output is redacted
	if err != nil {
		t.Fatal(err)
	}
	jc, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}

	outputs := map[string]string{
		"Sprint(Account)":    fmt.Sprint(a),
		"%v(Account)":        fmt.Sprintf("%v", a),
		"%+v(Account)":       fmt.Sprintf("%+v", a),
		"%#v(Account)":       fmt.Sprintf("%#v", a),
		"Sprint(Credential)": fmt.Sprint(c),
		"%v(Credential)":     fmt.Sprintf("%v", c),
		"%+v(Credential)":    fmt.Sprintf("%+v", c),
		"%#v(Credential)":    fmt.Sprintf("%#v", c),
		"%s(Credential)":     fmt.Sprintf("%s", c),
		"json(Account)":      string(js),
		"json(Credential)":   string(jc),
		"slog":               logs.String(),
	}
	for name, out := range outputs {
		if strings.Contains(out, secret) {
			t.Errorf("%s leaks the token", name)
		}
	}
}

func TestDiscoverNeverLeaksToken(t *testing.T) {
	t.Parallel()
	f := fullStore(t)
	for _, a := range Discover(f) {
		for _, s := range []string{fmt.Sprintf("%+v", a), fmt.Sprintf("%#v", a)} {
			if strings.Contains(s, "fake-token-1") || strings.Contains(s, "fake-gh-token") {
				t.Errorf("account %s/%s leaks a token", a.Provider, a.Key)
			}
		}
		if strings.Contains(a.Note, "fake-") {
			t.Errorf("note leaks content: %q", a.Note)
		}
	}
}

func TestMergeKeepsSmallestID(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{"pi:x", "opencode:y"}, {"opencode:y", "pi:x"}} {
		got := merge(Account{ID: pair[0], Harnesses: []string{"a"}}, Account{ID: pair[1], Harnesses: []string{"b"}})
		if got.ID != "opencode:y" {
			t.Errorf("merge(%v).ID = %q, want opencode:y", pair, got.ID)
		}
	}
}
