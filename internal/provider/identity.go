package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/usage"
)

// Identity is a proven, non-secret account identity: read from token claims
// or a harness's local profile, or answered by the provider for the token.
type Identity struct {
	Key   string // provider-scoped stable id; becomes Account.Key and Account.ID
	Label string // email or login for display
	Login string // GitHub login (Copilot); empty otherwise
	// AccountID is the provider account id the credential sends (the
	// ChatGPT-Account-Id of an OpenAI login); empty when not known.
	AccountID string
	Plan      usage.Plan // filled when the account's plan is unknown; Kind "" when not known
}

// OpenAIKey is the identity of a ChatGPT login: the account (workspace) id
// and the user id. The account id is shared by every member of a workspace,
// so userID must be non-empty; callers without one have no proven identity.
func OpenAIKey(accountID, userID string) string {
	return "openai:" + accountID + ":" + userID
}

// CopilotKey is the identity of a GitHub login on a host; logins are
// case-insensitive.
func CopilotKey(host, login string) string {
	return "copilot:" + normalizeHost(host) + ":" + strings.ToLower(login)
}

// AnthropicKey is the identity of a Claude login: the account and the
// organization the token is scoped to (usage is per organization).
func AnthropicKey(accountUUID, orgUUID string) string {
	k := "anthropic:" + accountUUID
	if orgUUID != "" {
		k += ":" + orgUUID
	}
	return k
}

// fingerprint identifies one credential without revealing it: a truncated,
// domain-separated SHA-256 of the token. It is the merge key of an account
// whose identity is not proven, and lets a later offline run recognise the
// same credential in the cache. Never displayed.
func fingerprint(provider, token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("agents/credential/v1\x00" + provider + "\x00" + token))
	return "fp:" + hex.EncodeToString(sum[:])[:16]
}

// unproven marks a with no proven identity: its merge key is the fingerprint
// of its credential, so only the same credential merges with it.
func unproven(a *Account, token string) {
	fp := fingerprint(a.Provider, token)
	a.IdentityKnown = false
	a.Key = fp
	a.Fingerprints = nil
	if fp != "" {
		a.Fingerprints = []string{fp}
	}
}

// proven sets a proven identity key on a: it is both the merge key and the
// cache id.
func proven(a *Account, key string) {
	a.IdentityKnown = true
	a.Key = key
	a.ID = key
}

// Identify returns a with a proven identity: Key and ID become id.Key, the
// label and login are set, and the plan is filled when a's is unknown. The
// credential fingerprints are kept, so the cache can bind them offline.
func Identify(a Account, id Identity) Account {
	if id.Key == "" {
		return a
	}
	proven(&a, id.Key)
	if id.Label != "" {
		a.Label = id.Label
	}
	if id.Login != "" {
		a.Login = id.Login
	}
	if id.AccountID != "" && a.Cred.AccountID() == "" && !a.Cred.Empty() {
		a.Cred = NewCredential(a.Cred.Token(), id.AccountID)
	}
	if (a.Plan.Kind == "" || a.Plan.Kind == usage.KindUnknown) && id.Plan.Kind != "" {
		a.Plan = id.Plan
	}
	return a
}

// unionFingerprints returns the sorted union of two fingerprint lists.
func unionFingerprints(a, b []string) []string {
	out := append(slices.Clone(a), b...)
	slices.Sort(out)
	return slices.Compact(out)
}
