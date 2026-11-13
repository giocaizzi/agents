package doctor

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/catalog"
	"github.com/giocaizzi/agents/internal/usage"
)

// resolved is a setting with its catalog model, when it has one.
type resolved struct {
	Setting

	ID    string // canonical id; "" when unknown
	Model catalog.Model
}

func resolveAll(c catalog.Catalog, settings []Setting) []resolved {
	out := make([]resolved, 0, len(settings))
	for _, s := range settings {
		r := resolved{Setting: s}
		if id, ok := resolve(c, s.Model); ok {
			r.ID, r.Model = id, c.Models[id]
		}
		out = append(out, r)
	}
	return out
}

// tierRank orders tiers by first appearance in settings.
func tierRank(tiers []string, tier string) int {
	if i := slices.Index(tiers, tier); i >= 0 {
		return i
	}
	return len(tiers)
}

// unknownModels is check (1): a configured model the catalog does not have,
// once per key and model.
func unknownModels(rs []resolved) []Finding {
	var out []Finding
	seen := map[[2]string]bool{}
	for _, r := range rs {
		k := [2]string{r.Key, r.Setting.Model}
		if r.ID != "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, Finding{
			Level: LevelWarn, Kind: KindUnknownModel, Tier: r.Tier,
			Text: fmt.Sprintf("%s (%s) is not in the model catalog: retired, renamed or not tracked", r.Setting.Model, r.Key),
			Fix:  "pick a tracked model from agents models",
		})
	}
	return out
}

// tierDrifts is check (1): a resolved model with no catalog tier, once per key.
func tierDrifts(c catalog.Catalog, rs []resolved) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, r := range rs {
		if r.ID == "" || seen[r.Key] {
			continue
		}
		seen[r.Key] = true
		if c.TierOf(r.ID) == "" {
			out = append(out, Finding{
				Level: LevelInfo, Kind: KindTierDrift, Tier: r.Tier,
				Text: fmt.Sprintf("%s (%s) has no tier: no API price or no ranking", name(r.ID, r.Model), r.Key),
			})
		}
	}
	return out
}

func name(id string, m catalog.Model) string {
	if m.Name != "" {
		return m.Name
	}
	return id
}

// harnessFindings are the per-harness checks, in order: no-account, capacity,
// headroom, better-model.
func harnessFindings(in Input, h string, all []resolved) []Finding {
	c := in.Catalog
	accts := accountsOf(in.Snapshots, h)
	avail := available(c, h, accts)
	var considered []resolved
	for _, r := range all {
		if r.Harness == h && r.ID != "" && slices.Contains(servable(c, h), r.Model.Provider) {
			considered = append(considered, r)
		}
	}
	out := noAccounts(h, considered, avail, in.Settings)
	advs := usage.Advisories(accts, in.Now)
	for _, a := range advs {
		lvl := LevelInfo
		if a.Level == usage.LevelWarn {
			lvl = LevelWarn
		}
		out = append(out, Finding{Level: lvl, Kind: KindCapacity, Harness: h, Text: a.Text})
	}
	if f, ok := headroom(in, h, accts, advs); ok {
		out = append(out, f)
	}
	for _, r := range considered {
		if f, ok := betterModel(in, h, r, accts, avail); ok {
			out = append(out, f)
		}
	}
	return out
}

// noAccounts is check (5): configured models whose provider the harness has no
// account for, one finding per account provider.
func noAccounts(h string, considered []resolved, avail []string, settings []Setting) []Finding {
	var order []string
	for _, s := range settings {
		if !slices.Contains(order, s.Tier) {
			order = append(order, s.Tier)
		}
	}
	byAcct := map[string][]resolved{}
	var accts []string
	for _, r := range considered {
		if slices.Contains(avail, r.Model.Provider) {
			continue
		}
		ap := accountProvider(h, r.Model.Provider)
		if _, ok := byAcct[ap]; !ok {
			accts = append(accts, ap)
		}
		byAcct[ap] = append(byAcct[ap], r)
	}
	slices.Sort(accts)
	out := make([]Finding, 0, len(accts))
	for _, ap := range accts {
		rs := slices.Clone(byAcct[ap])
		slices.SortStableFunc(rs, func(a, b resolved) int { return tierRank(order, a.Tier) - tierRank(order, b.Tier) })
		var tiers, models []string
		for _, r := range rs {
			if !slices.Contains(tiers, r.Tier) {
				tiers = append(tiers, r.Tier)
			}
			if !slices.Contains(models, r.Setting.Model) {
				models = append(models, r.Setting.Model)
			}
		}
		out = append(out, Finding{
			Level: LevelWarn, Kind: KindNoAccount, Harness: h,
			Text: fmt.Sprintf("no %s account found for %s (%s)", ap, strings.Join(tiers, ", "), strings.Join(models, ", ")),
			Fix:  fmt.Sprintf("log in to %s, then agents usage", h),
		})
	}
	return out
}

// headroom is check (4b): when h is near a limit, another provider's account
// of the selected harnesses with room to spare.
func headroom(in Input, h string, accts []usage.Snapshot, advs []usage.Advisory) (Finding, bool) {
	warned := map[string]bool{}
	for _, a := range advs {
		if a.Level == usage.LevelWarn {
			warned[a.Provider] = true
		}
	}
	if len(warned) == 0 {
		return Finding{}, false
	}
	var others []usage.Snapshot
	for _, s := range in.Snapshots {
		if s.Status != usage.StatusNotLoggedIn && !warned[s.Provider] &&
			slices.ContainsFunc(s.Harnesses, func(x string) bool { return slices.Contains(in.Harnesses, x) }) {
			others = append(others, s)
		}
	}
	q, left, ok := mostHeadroom(usage.Summaries(others, in.Now))
	if !ok {
		return Finding{}, false
	}
	pct := int(math.Round(left * 100))
	text := fmt.Sprintf("%s can use its %s account instead (%d%% left)", h, q, pct)
	if !hasAccount(accts, q) {
		var via []string
		for _, x := range in.Harnesses {
			if hasAccount(accountsOf(others, x), q) {
				via = append(via, x)
			}
		}
		text = fmt.Sprintf("%s has %d%% left through %s", q, pct, strings.Join(via, ", "))
	}
	return Finding{Level: LevelInfo, Kind: KindHeadroom, Harness: h, Text: text}, true
}

// mostHeadroom is the provider with the largest headroom of at least
// LowHeadroom; the first by provider on a tie.
func mostHeadroom(sums []usage.Summary) (string, float64, bool) {
	provider, best, found := "", 0.0, false
	for _, s := range sums {
		if s.Headroom == nil || *s.Headroom < LowHeadroom {
			continue
		}
		if !found || *s.Headroom > best {
			provider, best, found = s.Provider, *s.Headroom, true
		}
	}
	return provider, best, found
}
