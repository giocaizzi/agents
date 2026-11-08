// Package provider knows which provider accounts the AI harnesses are logged
// into. It reads the credentials each harness already stores, through an
// injected Store, into memory only: a Credential never prints, marshals or logs
// its secret. It is pure: no network, no processes.
package provider

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/giocaizzi/agents/internal/usage"
)

// Provider ids are models.dev provider keys, shared with the models catalog.
const (
	Anthropic     = "anthropic"
	OpenAI        = "openai"
	GitHubCopilot = "github-copilot"
	Google        = "google"
)

// Harness is a fixed fact about one harness: the providers it can log into,
// and whether it holds several provider accounts at once.
type Harness struct {
	Name        string
	Providers   []string // fixed providers; empty for multi-tenant harnesses
	MultiTenant bool
}

// Harnesses lists the harnesses whose credentials are discovered.
func Harnesses() []Harness {
	return []Harness{
		{Name: harnessClaude, Providers: []string{Anthropic}},
		{Name: "codex", Providers: []string{OpenAI}},
		{Name: "copilot", Providers: []string{GitHubCopilot}},
		{Name: harnessAntigravity, Providers: []string{Google}},
		{Name: harnessOpencode, MultiTenant: true},
		{Name: "pi", MultiTenant: true},
	}
}

// Auth is how an account authenticates.
type Auth string

// Auth kinds.
const (
	AuthOAuth   Auth = "oauth"   // subscription login; Credential holds the access token
	AuthAPIKey  Auth = "api-key" // provider API key
	AuthCloud   Auth = "cloud"   // a cloud platform account (Bedrock, Vertex, Foundry)
	AuthGateway Auth = "gateway" // an LLM gateway (custom base URL, key helper)
	AuthCLI     Auth = "cli"     // the harness CLI itself answers (agy /usage); no credential read
	AuthCommand Auth = "command" // a key produced by a command; never run, skipped
	AuthNone    Auth = "none"    // harness present, not logged in
)

// Credential is a secret held in memory. Its Format, String, GoString, MarshalJSON and
// LogValue never reveal it.
type Credential struct {
	// The secret sits behind a pointer: fmt's %p on a non-pointer is a bad
	// verb, which dumps the struct fields without calling Format, so a
	// pointer keeps the token out of that dump (it shows an address).
	s *secret
}

type secret struct {
	token     string
	accountID string // e.g. ChatGPT-Account-Id; not secret, but never displayed
}

// NewCredential wraps a token and an optional provider account id.
func NewCredential(token, accountID string) Credential {
	return Credential{s: &secret{token: token, accountID: accountID}}
}

// Token returns the secret for an Authorization header. Callers never print it.
func (c Credential) Token() string {
	if c.s == nil {
		return ""
	}
	return c.s.token
}

// AccountID returns the provider account id sent as a request header.
func (c Credential) AccountID() string {
	if c.s == nil {
		return ""
	}
	return c.s.accountID
}

// Fingerprint identifies the credential for providerID without revealing it
// (see fingerprint); empty when no token is held. Never displayed.
func (c Credential) Fingerprint(providerID string) string {
	return fingerprint(providerID, c.Token())
}

// Empty reports whether no token is held.
func (c Credential) Empty() bool { return c.Token() == "" }

const redacted = "[redacted]"

// String redacts the credential.
func (c Credential) String() string { return redacted }

// GoString redacts the credential for %#v.
func (c Credential) GoString() string { return redacted }

// MarshalJSON redacts the credential.
func (c Credential) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Format redacts the credential for every verb and flag, %x and %d included.
func (c Credential) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// LogValue redacts the credential in slog.
func (c Credential) LogValue() slog.Value { return slog.StringValue(redacted) }

// Account is one provider account found on this machine.
type Account struct {
	Provider  string       // models.dev provider id
	ID        string       // stable, non-secret identity for caching, never derived from a token
	Label     string       // email or login for display; never a token or an id
	Key       string       // merge key: provider account id, login or email; never displayed
	Harnesses []string     // harnesses logged into this account, sorted
	Auth      Auth         // how it authenticates
	Plan      usage.Plan   // from the credential when it says (subscriptionType, plan claims), else Kind unknown
	Expires   time.Time    // access token expiry; zero when unknown
	Status    usage.Status // set when the account cannot be queried: expired, not-logged-in, skipped, no-api, not-visible
	Note      string       // e.g. "usage lives in your cloud console"
	Cred      Credential   // the secret, in memory only
	Login     string       // GitHub login for Copilot billing; empty otherwise
	Host      string       // GitHub host the Copilot token was issued for, e.g. "https://github.com"; empty for other providers
	Source    string       // where it was read: "keychain", "file", "sqlite", "env"
	// CodexHome is the Codex profile directory the login was read from; empty
	// for every other account. Its session logs belong to this account alone.
	// Never displayed, never cached.
	CodexHome string
	// IdentityKnown is true when Key and ID name a proven account identity
	// (token claims, a harness's local profile, or the provider's answer for
	// the token). Accounts of different harnesses merge only on a proven
	// identity or on the very same credential.
	IdentityKnown bool
	// Fingerprints identify the OAuth credentials behind the account without
	// revealing them (see fingerprint); never displayed.
	Fingerprints []string
	// Inactive is true when the credential is not the active one of its
	// harness store; never displayed directly.
	Inactive bool
	// TokenSharing is true for a "Sign in with ChatGPT" login made for the API
	// (scope chatgpt.tokens.use.direct): its token is never sent to
	// chatgpt.com, never merged, never displayed directly. Cred stays empty.
	TokenSharing bool
	// LookupFailed is true when a live identity lookup sent the credential and
	// did not prove the identity; a cached identity is never re-attached to
	// it. Never displayed, never cached.
	LookupFailed bool
}

// Store is everything Discover reads from the machine. The real one lives in
// cli; tests use a fake.
type Store interface {
	// ReadFile reads a regular file of at most 64 MiB; a missing file returns
	// an error matching fs.ErrNotExist, a larger one an error matching
	// ErrTooLarge, any other kind of file an error.
	ReadFile(path string) ([]byte, error)
	// Secret reads one item from the OS credential store (go-keyring). An empty
	// account means "any account" for that service.
	Secret(service, account string) (string, error)
	// OpencodeCredentials reads the rows of opencode's V2 credential table,
	// read-only. A missing database returns an error matching fs.ErrNotExist.
	OpencodeCredentials(dbPath string) ([]OpencodeRow, error)
	// Exists reports whether a path exists.
	Exists(path string) bool
	// ListDir returns the sorted entry names of a directory; nil on error.
	ListDir(dir string) []string
	// Canonical is the path with symlinks resolved, or the cleaned path as
	// given when that fails.
	Canonical(path string) string
	// SameFile reports whether two paths are the same file or directory (it
	// sees through case-insensitive file systems and links); false on error.
	SameFile(a, b string) bool
	Getenv(key string) string
	Home() string
	GOOS() string
	Now() time.Time
}

// ErrTooLarge is the error ReadFile matches for a file over its size cap.
var ErrTooLarge = errors.New("file too large")

// OpencodeRow is one row of opencode's `credential` table.
type OpencodeRow struct {
	ID            string
	IntegrationID string
	Label         string
	Active        bool
	Value         []byte // JSON {type, access, refresh, expires, key, metadata}
}
