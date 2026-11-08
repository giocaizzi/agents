package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// needsIdentity reports accounts whose identity can be proven, and whose
// unproven state is therefore worth showing.
func needsIdentity(a provider.Account) bool {
	// A token-sharing login has no identity to prove: it is never queried.
	if a.Auth != provider.AuthOAuth || a.TokenSharing {
		return false
	}
	switch a.Provider {
	case provider.Anthropic, provider.OpenAI, provider.GitHubCopilot:
		return true
	}
	return false
}

// identityUnknown reports an account that may be the same as another of its
// provider because no identity was proven for it.
func identityUnknown(a provider.Account) bool { return needsIdentity(a) && !a.IdentityKnown }

// sharesFingerprint reports whether any of fps is in other.
func sharesFingerprint(fps, other []string) bool {
	return slices.ContainsFunc(fps, func(fp string) bool { return slices.Contains(other, fp) })
}

// resolveIdentities asks the provider who owns each unproven OAuth token, so
// accounts of different harnesses can merge. Every request is bounded by
// liveTimeout; any failure leaves the account unproven. A token is only sent
// to the endpoint the usage fetch uses (for OpenAI, the usage API itself,
// whose answer names the account and user); an expired token is sent nowhere.
func (f usageFetcher) resolveIdentities(ctx context.Context, accounts []provider.Account) []provider.Account {
	out := slices.Clone(accounts)
	var wg sync.WaitGroup
	for i, acct := range out {
		if acct.IdentityKnown || acct.TokenSharing || acct.Auth != provider.AuthOAuth || acct.Cred.Empty() ||
			(acct.Status != "" && acct.Status != usg.StatusOK) {
			continue
		}
		wg.Go(func() {
			rctx, cancel := context.WithTimeout(ctx, liveTimeout)
			defer cancel()
			id, err := f.identify(rctx, acct)
			switch {
			case err == nil:
				out[i] = provider.Identify(acct, id)
			case !errors.Is(err, errNotSent):
				// The credential was sent and did not prove who it is: the
				// cache must not vouch for it.
				out[i].LookupFailed = true
			}
		})
	}
	wg.Wait()
	return out
}

// errNotSent is identify's answer for a credential that was not sent anywhere
// (an unsupported host). Any other error means a request went out.
var errNotSent = errors.New("credential not sent")

// errIdentityMismatch is a usage answer for another account than the one sent.
var errIdentityMismatch = errors.New("account id differs from the one sent")

// identify asks the provider who owns the credential. The error is errNotSent
// when nothing was sent, and otherwise the reason the identity is unproven.
func (f usageFetcher) identify(ctx context.Context, acct provider.Account) (provider.Identity, error) {
	switch acct.Provider {
	case provider.GitHubCopilot:
		if acct.Host != githubHost {
			return provider.Identity{}, errNotSent
		}
		body, err := f.get(ctx, f.githubURL+"/user", map[string]string{
			hdrAuth:                "token " + acct.Cred.Token(),
			hdrAccept:              "application/vnd.github+json",
			"X-GitHub-Api-Version": "2026-03-10",
		})
		if err != nil {
			return provider.Identity{}, err
		}
		p, err := usg.ParseGitHubUser(body)
		if err != nil {
			return provider.Identity{}, fmt.Errorf("github identity: %w", err)
		}
		return provider.Identity{Key: provider.CopilotKey(acct.Host, p.Login), Label: p.Login, Login: p.Login}, nil
	case provider.Anthropic:
		body, err := f.get(ctx, f.anthropicURL+"/api/oauth/profile", map[string]string{
			hdrAuth:          "Bearer " + acct.Cred.Token(),
			"anthropic-beta": "oauth-2025-04-20",
			hdrAccept:        jsonType,
		})
		if err != nil {
			return provider.Identity{}, err
		}
		p, err := usg.ParseAnthropicProfile(body)
		if err != nil {
			return provider.Identity{}, fmt.Errorf("anthropic identity: %w", err)
		}
		return provider.Identity{Key: provider.AnthropicKey(p.AccountID, p.OrgID), Label: p.Email, Plan: p.Plan}, nil
	case provider.OpenAI:
		body, err := f.openaiUsage(ctx, acct)
		if err != nil {
			return provider.Identity{}, err
		}
		p, err := usg.ParseOpenAIIdentity(body)
		if err != nil {
			return provider.Identity{}, fmt.Errorf("openai identity: %w", err)
		}
		if sent := acct.Cred.AccountID(); sent != "" && sent != p.AccountID {
			return provider.Identity{}, errIdentityMismatch
		}
		return provider.Identity{Key: provider.OpenAIKey(p.AccountID, p.UserID), AccountID: p.AccountID, Plan: p.Plan}, nil
	}
	return provider.Identity{}, errNotSent
}

// bindCached gives an unproven account the identity of the cached snapshot
// proven for the same credential (by fingerprint), so the cached view stays
// merged without network access.
func bindCached(accounts []provider.Account, cached []usg.Snapshot) []provider.Account {
	out := slices.Clone(accounts)
	for i, a := range out {
		// A credential whose live lookup was sent and failed is never vouched
		// for by the cache; a token-sharing login has no identity to bind.
		if a.IdentityKnown || a.LookupFailed || a.TokenSharing || len(a.Fingerprints) == 0 {
			continue
		}
		for _, s := range cached {
			if s.Provider != a.Provider || s.IdentityUnknown || s.ID == "" || !sharesFingerprint(a.Fingerprints, s.Fingerprints) {
				continue
			}
			id := provider.Identity{Key: s.ID, Label: s.Account}
			if a.Provider == provider.GitHubCopilot {
				id.Login = s.Account
			}
			out[i] = provider.Identify(a, id)
			break
		}
	}
	return out
}
