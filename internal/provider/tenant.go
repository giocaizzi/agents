package provider

import (
	"cmp"
	"strings"
	"time"

	"github.com/giocaizzi/agents/internal/usage"
)

// Credential types of the multi-tenant stores, normalized.
const (
	tenantOAuth    = "oauth"
	tenantKey      = "key"
	tenantExternal = "external"
)

// inactiveNote is the note of a credential that is not the active one.
const inactiveNote = "(inactive)"

// tenantEntry is one credential of a multi-tenant harness (opencode, pi),
// before it becomes an Account.
type tenantEntry struct {
	harness   string
	provider  string // models.dev id; the stores already use it
	typ       string // tenantOAuth, tenantKey or tenantExternal
	access    string
	refresh   string
	key       string
	expires   time.Time
	accountID string
	rowID     string // opencode credential row id
	storeKey  string // raw key of a multi-tenant store entry (pi)
	label     string
	inactive  bool
	source    string
	host      string // GitHub Enterprise URL or domain the login was made on, raw
	// tokenSharing marks a store entry known to be a token-sharing login by
	// its key (pi's "openai"), whatever its token claims say.
	tokenSharing bool
}

// applyOpenAI sets the ChatGPT login facts of the entry on a.
func (e tenantEntry) applyOpenAI(a *Account) {
	o := openaiOAuth(e.harness, e.access, "", e.accountID)
	if e.tokenSharing && !o.TokenSharing {
		markTokenSharing(&o)
	}
	a.Key, a.Plan, a.Expires, a.Cred = o.Key, o.Plan, o.Expires, o.Cred
	a.TokenSharing, a.Status, a.Note = o.TokenSharing, o.Status, o.Note
	a.IdentityKnown, a.Fingerprints = o.IdentityKnown, o.Fingerprints
	if o.Label != "" {
		a.Label = o.Label
	}
	if o.IdentityKnown {
		a.ID = o.ID
	}
}

// account maps the entry to an Account.
func (e tenantEntry) account(now time.Time) Account {
	a := Account{
		Provider:  e.provider,
		ID:        e.harness + ":" + cmp.Or(e.rowID, e.storeKey, e.provider),
		Harnesses: []string{e.harness},
		Plan:      unknownPlan(),
		Source:    e.source,
	}
	switch e.typ {
	case tenantExternal:
		a.Auth = AuthAPIKey
		a.Label = e.label
		a.Status = usage.StatusNotVisible
		a.Note = "external credential"
	case tenantKey:
		a.Label = e.label
		if strings.HasPrefix(e.key, "!") {
			a.Auth = AuthCommand
			a.Status = usage.StatusSkipped
			a.Note = "key from a command, not run"
			break
		}
		a.Auth = AuthAPIKey
		a.Plan = usage.Plan{Kind: usage.KindAPI, Billing: usage.BillingMetered, Payer: usage.PayerUnknown}
		a.Status = usage.StatusNoAPI
		a.Note = "no usage API for API keys"
	default: // oauth
		a.Auth = AuthOAuth
		switch e.provider {
		case OpenAI:
			e.applyOpenAI(&a)
		case GitHubCopilot:
			token := cmp.Or(e.refresh, e.access) // the GitHub OAuth token
			unproven(&a, token)
			a.Cred = NewCredential(token, "")
			a.Host = normalizeHost(e.host)
			if e.refresh != "" {
				// The stored expiry is the short-lived Copilot API token's.
				e.expires = time.Time{}
			}
		case Anthropic:
			// Claude Code's credential holds no identity, so a login of
			// another harness never merges with it unless proven the same.
			unproven(&a, e.access)
			a.Cred = NewCredential(e.access, "")
		default:
			a.Label = e.label
			a.Key = e.accountID
			a.Cred = NewCredential(e.access, e.accountID)
		}
		if !e.expires.IsZero() {
			a.Expires = e.expires
		}
		expiredStatus(&a, now, e.harness)
	}
	a.Inactive = e.inactive
	if e.inactive {
		a.Note = strings.TrimSpace(a.Note + " " + inactiveNote)
	}
	return a
}
