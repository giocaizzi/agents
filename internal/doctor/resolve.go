package doctor

import (
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/catalog"
	"github.com/giocaizzi/agents/internal/usage"
)

// copilotBilling is the account provider of a GitHub Copilot subscription. It
// is a billing provider, never a catalog provider.
const copilotBilling = "github-copilot"

// CopilotProviders are the first-party providers whose models Copilot serves
// through a github-copilot account; github-copilot itself is a billing
// provider, never a catalog provider.
func CopilotProviders() []string {
	return []string{"anthropic", "google", "openai", "xai"}
}

// resolve maps a model as written in the policy to its canonical catalog id:
// an exact key, then "<provider>/<id>" compared by catalog.Key, then a bare id
// compared by catalog.Key (preferring the hint provider). The sorted-first id
// wins among several matches.
func resolve(c catalog.Catalog, model, hint string) (string, bool) {
	if _, ok := c.Models[model]; ok {
		return model, true
	}
	if p, rest, ok := strings.Cut(model, "/"); ok {
		if id, found := firstMatch(c, catalog.Key(rest), func(m catalog.Model) bool { return m.Provider == p }); found {
			return id, true
		}
	}
	if hint != "" {
		if id, found := firstMatch(c, catalog.Key(model), func(m catalog.Model) bool { return m.Provider == hint }); found {
			return id, true
		}
	}
	return firstMatch(c, catalog.Key(model), func(catalog.Model) bool { return true })
}

// firstMatch is the sorted-first id whose model id has the given key and passes
// keep.
func firstMatch(c catalog.Catalog, key string, keep func(catalog.Model) bool) (string, bool) {
	var ids []string
	for id, m := range c.Models {
		if catalog.Key(m.ID) == key && keep(m) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "", false
	}
	slices.Sort(ids)
	return ids[0], true
}

// servable are the catalog providers whose models harness h can run.
func servable(c catalog.Catalog, h string) []string {
	switch h {
	case hClaude:
		return []string{"anthropic"}
	case hCodex:
		return []string{"openai"}
	case hAntigravity:
		return []string{"google"}
	case hCopilot:
		return CopilotProviders()
	case hOpenCode, hPi:
		return c.Providers
	}
	return nil
}

// accountProvider is the provider of the account that serves catalog provider
// p through harness h.
func accountProvider(h, p string) string {
	if h == hCopilot {
		return copilotBilling
	}
	return p
}

// accountsOf are the logged-in accounts of harness h.
func accountsOf(snaps []usage.Snapshot, h string) []usage.Snapshot {
	var out []usage.Snapshot
	for _, s := range snaps {
		if slices.Contains(s.Harnesses, h) && s.Status != usage.StatusNotLoggedIn {
			out = append(out, s)
		}
	}
	return out
}

func hasAccount(accts []usage.Snapshot, provider string) bool {
	return slices.ContainsFunc(accts, func(s usage.Snapshot) bool { return s.Provider == provider })
}

// available are the catalog providers harness h has an account to use.
func available(c catalog.Catalog, h string, accts []usage.Snapshot) []string {
	switch h {
	case hClaude, hCodex, hAntigravity:
		if p := servable(c, h)[0]; hasAccount(accts, p) {
			return []string{p}
		}
	case hCopilot:
		if hasAccount(accts, copilotBilling) {
			return CopilotProviders()
		}
	case hOpenCode, hPi:
		var out []string
		for _, s := range accts {
			if slices.Contains(c.Providers, s.Provider) && !slices.Contains(out, s.Provider) {
				out = append(out, s.Provider)
			}
		}
		if hasAccount(accts, copilotBilling) {
			for _, p := range CopilotProviders() {
				if !slices.Contains(out, p) {
					out = append(out, p)
				}
			}
		}
		return out
	}
	return nil
}

// serving are the accounts of h that serve a model of catalog provider p.
func serving(h, p string, accts []usage.Snapshot) []usage.Snapshot {
	pick := func(provider string) []usage.Snapshot {
		return slices.DeleteFunc(slices.Clone(accts), func(s usage.Snapshot) bool { return s.Provider != provider })
	}
	switch h {
	case hCopilot:
		return pick(copilotBilling)
	case hOpenCode, hPi:
		if own := pick(p); len(own) > 0 || !slices.Contains(CopilotProviders(), p) {
			return own
		}
		return pick(copilotBilling)
	}
	return pick(p)
}
