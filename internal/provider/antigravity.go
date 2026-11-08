package provider

import (
	"path/filepath"

	"github.com/giocaizzi/agents/internal/usage"
)

// readAntigravity reports an account when agy's config directory exists. It
// reads no credential: agy answers /usage itself.
func readAntigravity(st Store) []Account {
	if !st.Exists(filepath.Join(st.Home(), ".gemini", "antigravity-cli")) {
		return nil
	}
	return []Account{{
		Provider:  Google,
		ID:        harnessAntigravity,
		Key:       harnessAntigravity,
		Harnesses: []string{harnessAntigravity},
		Auth:      AuthCLI,
		Plan: usage.Plan{
			Kind: usage.KindUnknown, Billing: usage.BillingUnknown, Payer: usage.PayerUnknown,
			Name: "Antigravity", Raw: "antigravity",
		},
		Source: srcFile,
	}}
}
