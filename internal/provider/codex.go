package provider

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/giocaizzi/agents/internal/usage"
)

const codexKeyringService = "Codex Auth"

// codexStoreKey is the keyring account Codex uses for a CODEX_HOME: "cli|" and
// the first 16 hex chars of the sha256 of the canonical path.
func codexStoreKey(canonicalHome string) string {
	sum := sha256.Sum256([]byte(canonicalHome))
	return "cli|" + hex.EncodeToString(sum[:])[:16]
}

type codexAuth struct {
	AuthMode string `json:"auth_mode"`
	APIKey   string `json:"OPENAI_API_KEY"`
	Tokens   struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

func openaiAPIKeyAccount(harness string) Account {
	return Account{
		Provider:  OpenAI,
		ID:        harness,
		Harnesses: []string{harness},
		Auth:      AuthAPIKey,
		Plan:      usage.Plan{Kind: usage.KindAPI, Billing: usage.BillingMetered, Payer: usage.PayerUnknown},
		Status:    usage.StatusNoAPI,
		Note:      "no usage API for API keys",
	}
}

// openaiOAuth builds a ChatGPT-login account from an access token, an optional
// id_token and a known account id. The id_token is decoded and dropped.
func openaiOAuth(harness, access, idToken, accountID string) Account {
	a := Account{
		Provider:  OpenAI,
		ID:        harness,
		Harnesses: []string{harness},
		Auth:      AuthOAuth,
		Plan:      unknownPlan(),
	}
	accessClaims := jwtClaims(access)
	idClaims := jwtClaims(idToken)
	accessAuth, idAuth := openaiAuth(accessClaims), openaiAuth(idClaims)
	accountID = cmp.Or(accountID, claimString(accessAuth, "chatgpt_account_id"), claimString(idAuth, "chatgpt_account_id"))
	userID := cmp.Or(
		claimString(accessAuth, "chatgpt_user_id"), claimString(accessAuth, "user_id"),
		claimString(idAuth, "chatgpt_user_id"), claimString(idAuth, "user_id"),
	)
	a.Label = cmp.Or(
		claimString(idClaims, "email"), claimString(openaiProfile(idClaims), "email"),
		claimString(accessClaims, "email"), claimString(openaiProfile(accessClaims), "email"),
	)
	if plan := cmp.Or(claimString(idAuth, "chatgpt_plan_type"), claimString(accessAuth, "chatgpt_plan_type")); plan != "" {
		a.Plan = usage.OpenAIPlan(plan)
	}
	// The account id is the workspace, shared by every member: it never
	// identifies a login on its own.
	if accountID != "" && userID != "" {
		proven(&a, OpenAIKey(accountID, userID))
	} else {
		unproven(&a, access)
	}
	a.Expires = claimExp(accessClaims)
	a.Cred = NewCredential(access, accountID)
	if tokenSharing(accessClaims) {
		markTokenSharing(&a)
	}
	return a
}

// readCodex finds the Codex accounts of every login profile.
func readCodex(st Store) []Account {
	return codexSpec().collect(st, readCodexProfile)
}

// readCodexProfile reads the Codex login of one CODEX_HOME and records the
// directory on the accounts, for the session-log fallback.
func readCodexProfile(st Store, p profile) []Account {
	accs := codexAccounts(st, p)
	for i := range accs {
		accs[i].CodexHome = p.dir
	}
	return accs
}

// codexAccounts reads auth.json, else the OS credential store
// (cli_auth_credentials_store = keyring), else, for the active profile,
// OPENAI_API_KEY.
func codexAccounts(st Store, p profile) []Account {
	label := p.label()
	apiKeyEnv := p.active && st.Getenv("OPENAI_API_KEY") != ""

	var raw []byte
	source := srcFile
	b, err := st.ReadFile(filepath.Join(p.dir, "auth.json"))
	switch {
	case err == nil:
		raw = b
	case isNotExist(err):
		if st.GOOS() == "darwin" {
			if s, serr := st.Secret(codexKeyringService, codexStoreKey(st.Canonical(p.dir))); serr == nil && s != "" {
				raw, source = []byte(s), srcKeychain
			}
		}
	default:
		return unreadableProfile(p, OpenAI, source)
	}

	if raw == nil {
		if apiKeyEnv {
			a := openaiAPIKeyAccount(label)
			a.Source = srcEnv
			return []Account{a}
		}
		return nil
	}

	var c codexAuth
	if err := unmarshalQuiet(raw, &c); err != nil {
		return unreadableProfile(p, OpenAI, source)
	}

	if c.AuthMode == "chatgpt" || (c.AuthMode == "" && c.Tokens.AccessToken != "") {
		if c.Tokens.AccessToken == "" {
			return nil
		}
		a := openaiOAuth(label, c.Tokens.AccessToken, c.Tokens.IDToken, c.Tokens.AccountID)
		a.Source = source
		expiredStatus(&a, st.Now(), label)
		return []Account{a}
	}
	if c.AuthMode == "apikey" || c.APIKey != "" || apiKeyEnv {
		a := openaiAPIKeyAccount(label)
		a.Source = source
		return []Account{a}
	}
	return nil
}

// unreadableProfile is the row of a profile whose store cannot be read; a
// globbed profile gets none.
func unreadableProfile(p profile, providerID, source string) []Account {
	if p.silent() {
		return nil
	}
	a := unreadable(providerID, p.label())
	a.Source = source
	return []Account{a}
}
