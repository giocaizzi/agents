package doctor

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/giocaizzi/agents/internal/catalog"
	"github.com/giocaizzi/agents/internal/usage"
)

// Billing modes: how the accounts serving a model are paid for.
const (
	modeFlat     = "flat"
	modeOrg      = "org"
	modeMetered  = "metered"
	modeRanking  = "ranking"
	reasonBetter = "better-ranked"
	reasonNewer  = "newer"
	reasonCheap  = "cheaper, ranked as well or better"
)

// rankCategories are the arena categories compared, in preference order.
func rankCategories() []string { return []string{"agent", "coding", "text"} }

// candidate is a model that might replace the current one.
type candidate struct {
	id      string
	m       catalog.Model
	rank    int // in the shared category; MaxInt when none
	blended float64
}

// contest compares candidates with the current model.
type contest struct{ cur catalog.Model }

func (k contest) curRank(cd candidate) int {
	if cat := sharedCategory(k.cur, cd.m); cat != "" {
		return k.cur.Arena[cat].Rank
	}
	return 0
}

func (k contest) better(cd candidate) bool {
	cr := k.curRank(cd)
	return cr > 0 && cd.rank+MinRankGain <= cr
}

func (k contest) notWorse(cd candidate) bool {
	cr := k.curRank(cd)
	return cr > 0 && cd.rank <= cr
}

func (k contest) cheaper(cd candidate) bool {
	return k.cur.API != nil && cd.blended <= catalog.BlendedPrice(*k.cur.API)*(1-MinSaving)
}

func (k contest) newer(cd candidate) bool {
	return cd.m.Provider == k.cur.Provider && cd.m.ReleaseDate > k.cur.ReleaseDate &&
		(sharedCategory(k.cur, cd.m) == "" || k.notWorse(cd))
}

func (k contest) cheapRanked(cd candidate) bool { return k.cheaper(cd) && k.notWorse(cd) }

// step is one way a candidate can win: why, who qualifies, how they sort.
type step struct {
	reason string
	ok     func(candidate) bool
	less   func(a, b candidate) int
}

// steps are the ways to win, strongest first. A candidate paid by a metered
// account or an org seat can win only as cheaper (see betterModel).
func (k contest) steps() []step {
	return []step{
		{reasonBetter, k.better, byRank},
		{reasonNewer, k.newer, byRelease},
		{reasonCheap, k.cheapRanked, byBlended},
	}
}

// candidates are the tier's models the harness can use in place of the
// current one.
func candidates(c catalog.Catalog, tier, curID string, cur catalog.Model, avail []string) []candidate {
	var out []candidate
	for _, id := range c.Tiers[tier] {
		m, ok := c.Models[id]
		if !ok || id == curID || !slices.Contains(avail, m.Provider) || m.API == nil {
			continue
		}
		cd := candidate{id: id, m: m, rank: math.MaxInt, blended: catalog.BlendedPrice(*m.API)}
		if cat := sharedCategory(cur, m); cat != "" {
			cd.rank = m.Arena[cat].Rank
		}
		out = append(out, cd)
	}
	return out
}

// betterModel is check (2): the best replacement in the model's catalog tier among
// the models the harness has an account for.
func betterModel(in Input, h string, r resolved, accts []usage.Snapshot, avail []string) (Finding, bool) {
	c := in.Catalog
	curTier := c.TierOf(r.ID)
	if curTier == "" {
		return Finding{}, false
	}
	cands := candidates(c, curTier, r.ID, r.Model, avail)
	// Billing comes from the account that would pay for each candidate.
	modes := map[string]string{}
	modeOf := func(cd candidate) string {
		m, ok := modes[cd.m.Provider]
		if !ok {
			m = billingMode(serving(h, cd.m.Provider, accts))
			modes[cd.m.Provider] = m
		}
		return m
	}
	for _, st := range (contest{r.Model}).steps() {
		var pool []candidate
		for _, cd := range cands {
			perUse := modeOf(cd) == modeMetered || modeOf(cd) == modeOrg
			if st.ok(cd) && (!perUse || st.reason == reasonCheap) {
				pool = append(pool, cd)
			}
		}
		if len(pool) == 0 {
			continue
		}
		slices.SortFunc(pool, func(a, b candidate) int {
			return cmp.Or(st.less(a, b), byRank(a, b), byRelease(a, b), cmp.Compare(a.id, b.id))
		})
		return suggestion(in, h, r, pool[0], st.reason, modeOf(pool[0]), accts), true
	}
	return Finding{}, false
}

func byRank(a, b candidate) int    { return cmp.Compare(a.rank, b.rank) }
func byRelease(a, b candidate) int { return cmp.Compare(b.m.ReleaseDate, a.m.ReleaseDate) }
func byBlended(a, b candidate) int { return cmp.Compare(a.blended, b.blended) }

// sharedCategory is the first of agent, coding and text in which both models
// have an arena rank, "" when none.
func sharedCategory(a, b catalog.Model) string {
	for _, cat := range rankCategories() {
		if a.Arena[cat].Rank > 0 && b.Arena[cat].Rank > 0 {
			return cat
		}
	}
	return ""
}

// billingMode classifies the accounts serving a model.
func billingMode(accts []usage.Snapshot) string {
	has := func(f func(usage.Plan) bool) bool {
		return slices.ContainsFunc(accts, func(s usage.Snapshot) bool { return f(s.Plan) })
	}
	switch {
	case has(func(p usage.Plan) bool {
		return (p.Billing == usage.BillingFlat || p.Billing == usage.BillingMixed) && p.Payer == usage.PayerSelf
	}):
		return modeFlat
	case has(func(p usage.Plan) bool {
		return p.Payer == usage.PayerOrg || p.Kind == usage.KindTeam || p.Kind == usage.KindEnterprise
	}):
		return modeOrg
	case has(func(p usage.Plan) bool {
		return p.Billing == usage.BillingMetered || p.Kind == usage.KindAPI || p.Kind == usage.KindCloud || p.Kind == usage.KindGateway
	}):
		return modeMetered
	}
	return modeRanking
}

// suggestion builds the better-model finding; mode is the billing of the
// account that would pay for cd.
func suggestion(in Input, h string, r resolved, cd candidate, reason, mode string, accts []usage.Snapshot) Finding {
	c := in.Catalog
	cat := sharedCategory(r.Model, cd.m)
	curRef := modelRef(c, r.ID, r.Model, cat)
	sugRef := modelRef(c, cd.id, cd.m, cat)

	billing := ""
	switch mode {
	case modeFlat:
		ok := true
		for _, s := range usage.Summaries(serving(h, cd.m.Provider, accts), in.Now) {
			if s.Headroom != nil && *s.Headroom < LowHeadroom {
				ok = false
			}
		}
		if ok {
			billing = "within your flat plan"
		}
	case modeOrg:
		billing = "seat managed by org"
	case modeMetered:
		billing = "metered billing"
	}

	detail := reason
	if cat != "" {
		detail += fmt.Sprintf("; %s #%d → #%d", cat, curRef.Rank, sugRef.Rank)
	}
	detail += fmt.Sprintf("; $%s/$%s → $%s/$%s per Mtok", price(curRef.In), price(curRef.Out), price(sugRef.In), price(sugRef.Out))
	if billing != "" {
		detail += "; " + billing
	}
	return Finding{
		Level: LevelSuggest, Kind: KindBetterModel, Harness: h, Tier: r.Tier,
		Text:    fmt.Sprintf("%s: %s → %s (%s)", r.Tier, curRef.Name, sugRef.Name, detail),
		Current: &curRef, Suggested: &sugRef, Reason: reason, Billing: billing,
	}
}

func modelRef(c catalog.Catalog, id string, m catalog.Model, cat string) ModelRef {
	ref := ModelRef{ID: id, Name: name(id, m), Tier: c.TierOf(id), Category: cat}
	if m.API != nil {
		ref.In, ref.Out = m.API.In, m.API.Out
	}
	if cat != "" {
		ref.Rank = m.Arena[cat].Rank
	}
	return ref
}

func price(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
