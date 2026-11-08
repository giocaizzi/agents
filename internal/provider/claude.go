package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/agents/internal/usage"
)

const claudeKeychainService = "Claude Code-credentials"

type claudeSettings struct {
	Env          map[string]any `json:"env"`
	APIKeyHelper string         `json:"apiKeyHelper"`
}

type claudeCreds struct {
	ClaudeAiOauth struct {
		AccessToken      string  `json:"accessToken"`
		ExpiresAt        float64 `json:"expiresAt"`
		SubscriptionType string  `json:"subscriptionType"`
		RateLimitTier    string  `json:"rateLimitTier"`
	} `json:"claudeAiOauth"`
}

// claudeProfile is the non-secret account profile Claude Code keeps in its
// global config, written from /api/oauth/profile at login.
type claudeProfile struct {
	OauthAccount struct {
		AccountUUID      string `json:"accountUuid"`
		EmailAddress     string `json:"emailAddress"`
		OrganizationUUID string `json:"organizationUuid"`
	} `json:"oauthAccount"`
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// claudeOverride returns the account of a cloud, gateway or API setup, if the
// environment (active profile only) or settings.json holds one. It overrides the OAuth login, as in
// Claude Code itself.
func claudeOverride(st Store, p profile, settings claudeSettings) (Account, bool) {
	env := func(k string) string {
		// The process environment belongs to the active profile only.
		if v := st.Getenv(k); v != "" && p.active {
			return v
		}
		s, _ := settings.Env[k].(string)
		return s
	}
	a := Account{Provider: Anthropic, ID: p.label(), Harnesses: []string{p.label()}, Source: srcEnv, Status: usage.StatusNotVisible}
	for _, c := range []struct{ env, name string }{
		{"CLAUDE_CODE_USE_BEDROCK", "Bedrock"},
		{"CLAUDE_CODE_USE_VERTEX", "Vertex AI"},
		{"CLAUDE_CODE_USE_FOUNDRY", "Foundry"},
	} {
		if truthy(env(c.env)) {
			a.Key = "claude-cloud" + p.keySuffix()
			a.Auth = AuthCloud
			a.Plan = usage.Plan{Kind: usage.KindCloud, Billing: usage.BillingMetered, Payer: usage.PayerCloud, Name: c.name}
			a.Note = "usage in your cloud billing console"
			return a, true
		}
	}

	hasToken := env("ANTHROPIC_AUTH_TOKEN") != ""
	if env("ANTHROPIC_BASE_URL") != "" && (hasToken || settings.APIKeyHelper != "") {
		a.Key = "claude-gateway" + p.keySuffix()
		a.Auth = AuthGateway
		a.Plan = usage.Plan{Kind: usage.KindGateway, Billing: usage.BillingUnknown, Payer: usage.PayerOrg}
		a.Note = "usage in your organization's gateway"
		return a, true
	}
	if env("ANTHROPIC_API_KEY") != "" || hasToken || settings.APIKeyHelper != "" {
		a.Key = "claude-api-key" + p.keySuffix()
		a.Auth = AuthAPIKey
		a.Plan = usage.Plan{Kind: usage.KindAPI, Billing: usage.BillingMetered, Payer: usage.PayerUnknown}
		a.Status = usage.StatusNoAPI
		a.Note = "usage in the Console"
		return a, true
	}
	return Account{}, false
}

// claudeSecret reads the OAuth credentials JSON of a profile: the keychain on
// darwin, else (or when the keychain has no item) <dir>/.credentials.json. A
// missing source returns an error matching fs.ErrNotExist.
func claudeSecret(st Store, p profile) (raw []byte, source string, err error) {
	if st.GOOS() == "darwin" {
		service := claudeKeychainService
		if p.explicit() {
			sum := sha256.Sum256([]byte(p.dir))
			service += "-" + hex.EncodeToString(sum[:])[:8]
		}
		if s, kerr := st.Secret(service, ""); kerr == nil && s != "" {
			return []byte(s), srcKeychain, nil
		}
		// Claude Code stores the login in the file when the keychain write fails.
	}
	b, rerr := st.ReadFile(filepath.Join(p.dir, ".credentials.json"))
	if rerr != nil {
		rerr = fmt.Errorf("read credentials file: %w", rerr)
	}
	return b, srcFile, rerr
}

// readClaude finds the Claude Code accounts of every login profile.
func readClaude(st Store) []Account {
	return claudeSpec().collect(st, readClaudeProfile)
}

func readClaudeProfile(st Store, p profile) []Account {
	var settings claudeSettings
	_, _ = readJSON(st, filepath.Join(p.dir, "settings.json"), &settings) // malformed settings count as absent
	if a, ok := claudeOverride(st, p, settings); ok {
		return []Account{a}
	}

	oauth := Account{Provider: Anthropic, ID: p.label(), Key: "claude-oauth" + p.keySuffix(), Harnesses: []string{p.label()}, Auth: AuthOAuth}
	raw, source, err := claudeSecret(st, p)
	oauth.Source = source
	if err != nil && isNotExist(err) {
		return nil // no credentials
	}
	var c claudeCreds
	if err != nil || unmarshalQuiet(raw, &c) != nil {
		if p.silent() {
			return nil
		}
		oauth.Plan = unknownPlan()
		oauth.Status = usage.StatusUnavailable
		oauth.Note = noteUnreadable
		return []Account{oauth}
	}
	o := c.ClaudeAiOauth
	if o.AccessToken == "" {
		return nil
	}
	oauth.Cred = NewCredential(o.AccessToken, "")
	var prof claudeProfile
	cfgPath := filepath.Join(st.Home(), ".claude.json")
	if p.explicit() {
		cfgPath = filepath.Join(p.dir, ".claude.json")
	}
	tooLarge := false
	if _, err := readJSON(st, cfgPath, &prof); errors.Is(err, ErrTooLarge) { // missing or malformed counts as absent
		tooLarge = true
		prof = claudeProfile{}
	}
	if pr := prof.OauthAccount; pr.AccountUUID != "" {
		proven(&oauth, AnthropicKey(pr.AccountUUID, pr.OrganizationUUID))
		oauth.Label = pr.EmailAddress
	} else {
		unproven(&oauth, o.AccessToken)
		if tooLarge {
			oauth.Note = "account profile too large (.claude.json): identity unknown"
		}
	}
	oauth.Plan = usage.AnthropicPlan(o.SubscriptionType, o.RateLimitTier)
	oauth.Expires = msTime(o.ExpiresAt)
	expiredStatus(&oauth, st.Now(), p.label())
	return []Account{oauth}
}
