package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/giocaizzi/agents/internal/usage"
)

func claudeCredsJSON(expires time.Time) string {
	return fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"fake-token-1","refreshToken":"fake-refresh-1","expiresAt":%d,"subscriptionType":"max","rateLimitTier":"default_claude_max_5x"},"mcpOAuth":{"srv":{"accessToken":"fake-mcp-token"}}}`, expires.UnixMilli())
}

func TestClaudeOAuth(t *testing.T) {
	t.Parallel()
	valid := testNow.Add(time.Hour)
	suffix := sha256.Sum256([]byte("/cfg"))
	cfgService := "Claude Code-credentials-" + hex.EncodeToString(suffix[:])[:8]

	tests := []struct {
		name  string
		setup func(f *fakeStore)
		goos  string
		want  []view
	}{
		{
			name: "linux file",
			goos: "linux",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsJSON(valid)
			},
			want: []view{{Provider: Anthropic, Key: fingerprint(Anthropic, "fake-token-1"), Harnesses: []string{"claude"}, Auth: AuthOAuth, Token: "fake-token-1", Expires: valid, Source: "file"}},
		},
		{
			name: "darwin keychain",
			goos: "darwin",
			setup: func(f *fakeStore) {
				f.secrets["Claude Code-credentials\x00"] = claudeCredsJSON(valid)
				f.files[f.path(".claude", ".credentials.json")] = `{"claudeAiOauth":{"accessToken":"fake-file-token"}}` // ignored on darwin
			},
			want: []view{{Provider: Anthropic, Key: fingerprint(Anthropic, "fake-token-1"), Harnesses: []string{"claude"}, Auth: AuthOAuth, Token: "fake-token-1", Expires: valid, Source: "keychain"}},
		},
		{
			name: "darwin CLAUDE_CONFIG_DIR suffix",
			goos: "darwin",
			setup: func(f *fakeStore) {
				f.env["CLAUDE_CONFIG_DIR"] = "/cfg"
				f.secrets["Claude Code-credentials\x00"] = `{"claudeAiOauth":{"accessToken":"fake-unsuffixed"}}`
				f.secrets[cfgService+"\x00"] = claudeCredsJSON(valid)
			},
			// The env dir reads its suffixed item; the default dir still reads the unsuffixed one.
			want: []view{
				{Provider: Anthropic, Key: fingerprint(Anthropic, "fake-token-1"), Harnesses: []string{"claude:cfg"}, Auth: AuthOAuth, Token: "fake-token-1", Expires: valid, Source: "keychain"},
				{Provider: Anthropic, Key: fingerprint(Anthropic, "fake-unsuffixed"), Harnesses: []string{"claude"}, Auth: AuthOAuth, Token: "fake-unsuffixed", Source: "keychain"},
			},
		},
		{
			name: "linux CLAUDE_CONFIG_DIR",
			goos: "linux",
			setup: func(f *fakeStore) {
				f.env["CLAUDE_CONFIG_DIR"] = "/cfg"
				f.files["/cfg/.credentials.json"] = claudeCredsJSON(valid)
			},
			want: []view{{Provider: Anthropic, Key: fingerprint(Anthropic, "fake-token-1"), Harnesses: []string{"claude"}, Auth: AuthOAuth, Token: "fake-token-1", Expires: valid, Source: "file"}},
		},
		{
			name: "expired",
			goos: "linux",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsJSON(testNow.Add(-time.Hour))
			},
			want: []view{{Provider: Anthropic, Key: fingerprint(Anthropic, "fake-token-1"), Harnesses: []string{"claude"}, Auth: AuthOAuth, Token: "fake-token-1", Expires: testNow.Add(-time.Hour), Status: "expired", Note: "token expired, open claude to refresh", Source: "file"}},
		},
		{
			name: "malformed",
			goos: "linux",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude", ".credentials.json")] = `{not json fake-token-1`
			},
			want: []view{{Provider: Anthropic, Key: "claude-oauth", Harnesses: []string{"claude"}, Auth: AuthOAuth, Status: "unavailable", Note: "credentials unreadable", Source: "file"}},
		},
		{
			name:  "missing",
			goos:  "linux",
			setup: func(*fakeStore) {},
			want:  nil,
		},
		{
			name:  "keychain missing",
			goos:  "darwin",
			setup: func(*fakeStore) {},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake(tt.goos)
			tt.setup(f)
			assertViews(t, readClaude(f), tt.want)
		})
	}
}

func TestClaudeModesOverrideOAuth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		env      map[string]string
		settings string
		want     view
		plan     usage.Plan
	}{
		{
			name: "bedrock",
			env:  map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "ANTHROPIC_API_KEY": "fake-key"},
			want: view{Auth: AuthCloud, Key: "claude-cloud", Status: "not-visible", Note: "usage in your cloud billing console"},
			plan: usage.Plan{Kind: usage.KindCloud, Billing: usage.BillingMetered, Payer: usage.PayerCloud, Name: "Bedrock"},
		},
		{
			name:     "vertex from settings env",
			settings: `{"env":{"CLAUDE_CODE_USE_VERTEX":"true"}}`,
			want:     view{Auth: AuthCloud, Key: "claude-cloud", Status: "not-visible", Note: "usage in your cloud billing console"},
			plan:     usage.Plan{Kind: usage.KindCloud, Billing: usage.BillingMetered, Payer: usage.PayerCloud, Name: "Vertex AI"},
		},
		{
			name: "foundry",
			env:  map[string]string{"CLAUDE_CODE_USE_FOUNDRY": "yes"},
			want: view{Auth: AuthCloud, Key: "claude-cloud", Status: "not-visible", Note: "usage in your cloud billing console"},
			plan: usage.Plan{Kind: usage.KindCloud, Billing: usage.BillingMetered, Payer: usage.PayerCloud, Name: "Foundry"},
		},
		{
			name: "falsy cloud flag falls through to api key",
			env:  map[string]string{"CLAUDE_CODE_USE_BEDROCK": "0", "ANTHROPIC_API_KEY": "fake-key"},
			want: view{Auth: AuthAPIKey, Key: "claude-api-key", Status: "no-api", Note: "usage in the Console"},
			plan: usage.Plan{Kind: usage.KindAPI, Billing: usage.BillingMetered, Payer: usage.PayerUnknown},
		},
		{
			name: "gateway with token",
			env:  map[string]string{"ANTHROPIC_BASE_URL": "https://gw.example.com", "ANTHROPIC_AUTH_TOKEN": "fake-token-1"},
			want: view{Auth: AuthGateway, Key: "claude-gateway", Status: "not-visible", Note: "usage in your organization's gateway"},
			plan: usage.Plan{Kind: usage.KindGateway, Billing: usage.BillingUnknown, Payer: usage.PayerOrg},
		},
		{
			name:     "gateway with apiKeyHelper",
			env:      map[string]string{"ANTHROPIC_BASE_URL": "https://gw.example.com"},
			settings: `{"apiKeyHelper":"print-key.sh"}`,
			want:     view{Auth: AuthGateway, Key: "claude-gateway", Status: "not-visible", Note: "usage in your organization's gateway"},
			plan:     usage.Plan{Kind: usage.KindGateway, Billing: usage.BillingUnknown, Payer: usage.PayerOrg},
		},
		{
			name:     "base url alone is not a gateway",
			env:      map[string]string{"ANTHROPIC_BASE_URL": "https://gw.example.com"},
			settings: `{}`,
			want:     view{}, // falls through to OAuth
		},
		{
			name:     "api key from settings env",
			settings: `{"env":{"ANTHROPIC_API_KEY":"fake-key"}}`,
			want:     view{Auth: AuthAPIKey, Key: "claude-api-key", Status: "no-api", Note: "usage in the Console"},
			plan:     usage.Plan{Kind: usage.KindAPI, Billing: usage.BillingMetered, Payer: usage.PayerUnknown},
		},
		{
			name:     "auth token only",
			env:      map[string]string{"ANTHROPIC_AUTH_TOKEN": "fake-token-1"},
			settings: `{}`,
			want:     view{Auth: AuthAPIKey, Key: "claude-api-key", Status: "no-api", Note: "usage in the Console"},
			plan:     usage.Plan{Kind: usage.KindAPI, Billing: usage.BillingMetered, Payer: usage.PayerUnknown},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake("linux")
			f.files[f.path(".claude", ".credentials.json")] = claudeCredsJSON(testNow.Add(time.Hour))
			maps.Copy(f.env, tt.env)
			if tt.settings != "" {
				f.files[f.path(".claude", "settings.json")] = tt.settings
			}
			got := readClaude(f)
			if len(got) != 1 {
				t.Fatalf("got %d accounts, want 1", len(got))
			}
			if tt.want.Auth == "" { // OAuth fallthrough
				if got[0].Auth != AuthOAuth {
					t.Errorf("Auth = %q, want oauth", got[0].Auth)
				}
				return
			}
			tt.want.Provider, tt.want.Harnesses, tt.want.Source = Anthropic, []string{"claude"}, "env"
			assertViews(t, got, []view{tt.want})
			if got[0].Plan != tt.plan {
				t.Errorf("Plan = %+v, want %+v", got[0].Plan, tt.plan)
			}
			if !got[0].Cred.Empty() {
				t.Errorf("non-OAuth account holds a credential")
			}
		})
	}
}

func TestClaudeSettingsUnderConfigDir(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.env["CLAUDE_CONFIG_DIR"] = "/cfg"
	f.files["/cfg/settings.json"] = `{"env":{"CLAUDE_CODE_USE_BEDROCK":"1"}}`
	got := readClaude(f)
	if len(got) != 1 || got[0].Auth != AuthCloud {
		t.Errorf("got %+v, want one cloud account", views(got))
	}
}
