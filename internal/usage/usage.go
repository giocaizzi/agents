// Package usage models the plan, billing and capacity of one provider account
// (rolling windows, allowances, spend caps, credits) and parses each provider's
// usage payloads into that model. It is pure: no network, no processes.
package usage

import "time"

// Kind is the kind of plan behind an account. Unmapped values become KindUnknown
// and keep the provider's own string in Plan.Raw.
type Kind string

// Plan kinds.
const (
	KindFree       Kind = "free"
	KindIndividual Kind = "individual" // a plan the user pays for: Pro, Max, Plus, Copilot Pro...
	KindTeam       Kind = "team"       // a seat in a team or business workspace
	KindEnterprise Kind = "enterprise" // a seat in an enterprise workspace, seat- or usage-based
	KindAPI        Kind = "api"        // a provider API key or console login
	KindCloud      Kind = "cloud"      // served through a cloud platform account (Bedrock, Vertex, Foundry)
	KindGateway    Kind = "gateway"    // served through an organization's LLM gateway
	KindUnknown    Kind = "unknown"
)

// Billing says how usage is paid.
type Billing string

// Billing models.
const (
	BillingFlat    Billing = "flat"    // allowance included in the plan or seat
	BillingMetered Billing = "metered" // pay per token, request or credit
	BillingMixed   Billing = "mixed"   // flat allowance plus overage or credits
	BillingUnknown Billing = "unknown"
)

// Payer says who pays.
type Payer string

// Payers.
const (
	PayerSelf    Payer = "self"
	PayerOrg     Payer = "org"
	PayerCloud   Payer = "cloud"
	PayerUnknown Payer = "unknown"
)

// Plan is the subscription or billing model of one account.
type Plan struct {
	Kind    Kind    `json:"kind"`
	Billing Billing `json:"billing"`
	Payer   Payer   `json:"payer"`
	Name    string  `json:"name,omitempty"` // display name, e.g. "Max 5x", "Plus", "Business"
	Raw     string  `json:"raw,omitempty"`  // the provider's own plan string, e.g. "max", "plus", "business"
}

// NameTokenSharing is the plan name of a "Sign in with ChatGPT" login made for
// the API (token sharing): ChatGPT's usage API does not answer for it.
const NameTokenSharing = "ChatGPT sign-in (token sharing)" //nolint:gosec // a display name, not a credential

// NoteSharingNotVisible is the note of a token-sharing login.
const NoteSharingNotVisible = "usage not visible for this login type"

// TokenSharingPlan is the plan of a token-sharing login: nothing is known of
// the account behind it.
func TokenSharingPlan() Plan {
	return Plan{Kind: KindUnknown, Billing: BillingUnknown, Payer: PayerUnknown, Name: NameTokenSharing}
}

// LimitKind is the shape of one limit.
type LimitKind string

// Limit kinds.
const (
	LimitWindow    LimitKind = "window"    // rolling window, e.g. 5h or weekly
	LimitAllowance LimitKind = "allowance" // monthly allowance, e.g. premium requests or AI credits
	LimitSpend     LimitKind = "spend"     // spend against a cap or budget
	LimitCredits   LimitKind = "credits"   // prepaid credit balance
)

// Scope says whose capacity a limit measures.
type Scope string

// Scopes.
const (
	ScopeSeat    Scope = "seat"    // this user's seat
	ScopePool    Scope = "pool"    // shared by an organization
	ScopeAccount Scope = "account" // the whole (individual) account
)

// Amount is a quantity with its unit: "requests", "credits", "USD", ...
type Amount struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Limit is one capacity measure. Unknown fields stay nil; nothing is guessed.
type Limit struct {
	Name      string     `json:"name"` // "5h", "7d", "7d opus", "premium", "monthly spend"
	Kind      LimitKind  `json:"kind"`
	Scope     Scope      `json:"scope,omitempty"`
	UsedPct   *float64   `json:"usedPct,omitempty"` // 0-100
	Used      *Amount    `json:"used,omitempty"`
	Cap       *Amount    `json:"cap,omitempty"`
	Remaining *Amount    `json:"remaining,omitempty"`
	ResetsAt  *time.Time `json:"resetsAt,omitempty"`
	Overage   *bool      `json:"overage,omitempty"` // extra usage or paid overage enabled
	Unlimited bool       `json:"unlimited,omitempty"`
	Disabled  bool       `json:"disabled,omitempty"` // the provider says the limit does not apply now
	Hidden    bool       `json:"hidden,omitempty"`   // managed by the organization, not visible to this seat
	Note      string     `json:"note,omitempty"`
}

// Status is the state of one account's snapshot.
type Status string

// Statuses.
const (
	StatusOK          Status = "ok"
	StatusStale       Status = "stale"         // from the cache or an offline source
	StatusExpired     Status = "expired"       // the access token expired; the harness refreshes it
	StatusUnavailable Status = "unavailable"   // the provider call failed (401, 403, 429, timeout, bad payload)
	StatusNoAPI       Status = "no-api"        // the provider has no usage API for this kind of account
	StatusNotVisible  Status = "not-visible"   // usage lives with the organization, cloud console or gateway
	StatusNotLoggedIn Status = "not-logged-in" // the harness is installed but has no login
	StatusSkipped     Status = "skipped"       // e.g. an API key that comes from a command
)

// Snapshot is the usage of one provider account at one time.
type Snapshot struct {
	ID        string    `json:"id,omitempty"` // the account's stable, non-secret id
	Provider  string    `json:"provider"`     // models.dev provider id: anthropic, openai, github-copilot, google, ...
	Account   string    `json:"account"`      // display label: email or login, never a token or an id
	Harnesses []string  `json:"harnesses"`    // harnesses logged into this account
	Plan      Plan      `json:"plan"`
	Limits    []Limit   `json:"limits,omitempty"`
	Status    Status    `json:"status"`
	Note      string    `json:"note,omitempty"`
	Source    string    `json:"source,omitempty"` // "api", "agy", "codex-session", "agy-log", "cache"
	FetchedAt time.Time `json:"fetchedAt"`
	// IdentityUnknown is true when the account's identity is not proven, so
	// it may be the same account as another row of its provider.
	IdentityUnknown bool `json:"identityUnknown,omitempty"`
	// Fingerprints identify the credentials behind the account (a truncated
	// hash, never a token), so an offline run can bind the same credential to
	// this account again. Never displayed.
	Fingerprints []string `json:"fingerprints,omitempty"`
}

// Profile is the identity a provider reports for a token.
type Profile struct {
	AccountID string // Anthropic account uuid; ChatGPT account (workspace) id
	UserID    string // ChatGPT user id; empty for other providers
	OrgID     string // Anthropic organization uuid
	Email     string // Anthropic account email
	Login     string // GitHub login
	Plan      Plan   // from the organization type; Kind "" when not reported
}

// Summary is what the models feature reads per provider id.
type Summary struct {
	Provider string `json:"provider"`
	Plans    []Plan `json:"plans"`
	// Headroom is the fraction (0-1) left on the tightest visible, enabled limit
	// whose reset is in the future, best across the provider's accounts; nil when
	// unknown or not visible.
	Headroom *float64 `json:"headroom,omitempty"`
}

// Level is the severity of an advisory.
type Level string

// Levels.
const (
	LevelInfo Level = "info"
	LevelWarn Level = "warn"
)

// Advisory is one capacity fact for the warning surface (agents doctor).
type Advisory struct {
	Provider string `json:"provider"`
	Level    Level  `json:"level"`
	Text     string `json:"text"`
}
