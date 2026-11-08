package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/giocaizzi/agents/internal/usage"
)

// chatgptTokens returns fake access and id tokens for one ChatGPT account.
func chatgptTokens(t *testing.T, exp time.Time, accountID string) (access, id string) {
	t.Helper()
	access = jwt(t, map[string]any{
		"exp":           exp.Unix(),
		openaiAuthClaim: map[string]any{"chatgpt_account_id": accountID, "chatgpt_user_id": "user-1"},
	})
	id = jwt(t, map[string]any{
		"email":         "me@example.com",
		openaiAuthClaim: map[string]any{"chatgpt_plan_type": "plus", "chatgpt_account_id": accountID, "chatgpt_user_id": "user-1"},
	})
	return access, id
}

func codexAuthJSON(t *testing.T, access, id, accountID string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens": map[string]string{
			"access_token": access, "id_token": id, "refresh_token": "fake-refresh-1", "account_id": accountID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCodex(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	access, id := chatgptTokens(t, exp, "acct-1")
	oauth := view{Provider: OpenAI, Label: "me@example.com", Key: "openai:acct-1:user-1", Harnesses: []string{"codex"}, Auth: AuthOAuth, Token: access, AccountID: "acct-1", Expires: exp, Source: "file"}

	t.Run("chatgpt file", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, access, id, "acct-1")
		assertViews(t, readCodex(f), []view{oauth})
	})
	t.Run("CODEX_HOME", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.env["CODEX_HOME"] = "/codex"
		f.files["/codex/auth.json"] = codexAuthJSON(t, access, id, "acct-1")
		assertViews(t, readCodex(f), []view{oauth})
	})
	t.Run("keyring fallback", func(t *testing.T) {
		t.Parallel()
		f := newFake("darwin")
		f.secrets["Codex Auth\x00"+codexStoreKey(f.path(".codex"))] = codexAuthJSON(t, access, id, "acct-1")
		want := oauth
		want.Source = "keychain"
		assertViews(t, readCodex(f), []view{want})
	})
	t.Run("keyring of another home is not read", func(t *testing.T) {
		t.Parallel()
		f := newFake("darwin")
		f.secrets["Codex Auth\x00"+codexStoreKey("/other/.codex")] = codexAuthJSON(t, access, id, "acct-1")
		f.secrets["Codex Auth\x00"] = codexAuthJSON(t, access, id, "acct-1")
		assertViews(t, readCodex(f), nil)
	})
	t.Run("keyring uses the canonical CODEX_HOME", func(t *testing.T) {
		t.Parallel()
		f := newFake("darwin")
		f.env["CODEX_HOME"] = "/link/codex"
		f.canon = map[string]string{"/link/codex": "/real/codex"}
		f.secrets["Codex Auth\x00"+codexStoreKey("/real/codex")] = codexAuthJSON(t, access, id, "acct-1")
		want := oauth
		want.Source = "keychain"
		assertViews(t, readCodex(f), []view{want})
	})
	t.Run("keyring not queried off darwin", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.secrets["Codex Auth\x00"+codexStoreKey(f.path(".codex"))] = codexAuthJSON(t, access, id, "acct-1")
		assertViews(t, readCodex(f), nil)
	})
	t.Run("account id from claim when absent", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, access, id, "")
		got := readCodex(f)
		if len(got) != 1 || got[0].Key != "openai:acct-1:user-1" {
			t.Errorf("got %+v", views(got))
		}
	})
	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		past := testNow.Add(-time.Minute)
		old, oldID := chatgptTokens(t, past, "acct-1")
		f := newFake("linux")
		f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, old, oldID, "acct-1")
		got := readCodex(f)
		if len(got) != 1 || got[0].Status != usage.StatusExpired || got[0].Note != "token expired, open codex to refresh" {
			t.Errorf("got %+v", views(got))
		}
	})
	t.Run("apikey mode", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".codex", "auth.json")] = `{"auth_mode":"apikey","OPENAI_API_KEY":"fake-key-1"}`
		got := readCodex(f)
		assertViews(t, got, []view{{Provider: OpenAI, Harnesses: []string{"codex"}, Auth: AuthAPIKey, Status: "no-api", Note: "no usage API for API keys", Source: "file"}})
		want := usage.Plan{Kind: usage.KindAPI, Billing: usage.BillingMetered, Payer: usage.PayerUnknown}
		if got[0].Plan != want {
			t.Errorf("Plan = %+v", got[0].Plan)
		}
	})
	t.Run("env key without auth file", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.env["OPENAI_API_KEY"] = "fake-key-1"
		assertViews(t, readCodex(f), []view{{Provider: OpenAI, Harnesses: []string{"codex"}, Auth: AuthAPIKey, Status: "no-api", Note: "no usage API for API keys", Source: "env"}})
	})
	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		assertViews(t, readCodex(newFake("linux")), nil)
	})
	t.Run("malformed", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".codex", "auth.json")] = `{"tokens": fake-token-1`
		assertViews(t, readCodex(f), []view{{Provider: OpenAI, Harnesses: []string{"codex"}, Auth: AuthNone, Status: "unavailable", Note: "credentials unreadable", Source: "file"}})
	})
}

func TestCodexStoreKey(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("/home/user/.codex"))
	tests := []struct{ name, home, want string }{
		{"computed", "/home/user/.codex", "cli|" + hex.EncodeToString(sum[:])[:16]},
		{"hard-coded", "/home/user/.codex", "cli|59448a44901f7128"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := codexStoreKey(tt.home); got != tt.want {
				t.Errorf("codexStoreKey(%q) = %q, want %q", tt.home, got, tt.want)
			}
		})
	}
	if codexStoreKey("/home/user/.codex") == codexStoreKey("/other/.codex") {
		t.Error("different homes share a key")
	}
}
