package usage

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

func sortedByProviderAccount(snaps []Snapshot) []Snapshot {
	s := slices.Clone(snaps)
	slices.SortStableFunc(s, func(a, b Snapshot) int {
		if c := strings.Compare(a.Provider, b.Provider); c != 0 {
			return c
		}
		return strings.Compare(a.Account, b.Account)
	})
	return s
}

// active reports whether a limit counts: visible, enabled, and not already reset.
func active(l Limit, now time.Time) bool {
	return !l.Hidden && !l.Disabled && (l.ResetsAt == nil || l.ResetsAt.After(now))
}

// Summaries reduces snapshots to one Summary per provider, sorted by provider.
func Summaries(snaps []Snapshot, now time.Time) []Summary {
	var out []Summary
	var tight map[string]float64 // per account: lowest headroom
	for _, s := range sortedByProviderAccount(snaps) {
		if len(out) == 0 || out[len(out)-1].Provider != s.Provider {
			out = append(out, Summary{Provider: s.Provider})
			tight = map[string]float64{}
		}
		cur := &out[len(out)-1]
		if !slices.Contains(cur.Plans, s.Plan) {
			cur.Plans = append(cur.Plans, s.Plan)
		}
		noteHeadroom(tight, s, now)
		cur.Headroom = bestHeadroom(tight)
	}
	return out
}

// noteHeadroom lowers the account's headroom to its tightest counted limit.
func noteHeadroom(tight map[string]float64, s Snapshot, now time.Time) {
	for _, l := range s.Limits {
		if l.UsedPct == nil || !active(l, now) {
			continue
		}
		h := (100 - *l.UsedPct) / 100
		if v, ok := tight[s.Account]; !ok || h < v {
			tight[s.Account] = h
		}
	}
}

// bestHeadroom is the largest per-account headroom, nil when there is none.
func bestHeadroom(tight map[string]float64) *float64 {
	var best *float64
	for _, v := range tight {
		if best == nil || v > *best {
			best = new(v)
		}
	}
	return best
}

// Advisories lists capacity facts: a visible, enabled limit at 90% or more whose
// reset is in the future (warn), an exhausted allowance with overage on (info).
// Unknown plans and hidden limits give none.
func Advisories(snaps []Snapshot, now time.Time) []Advisory {
	var out []Advisory
	for _, s := range snaps {
		if s.Plan.Kind == KindUnknown {
			continue
		}
		for _, l := range s.Limits {
			if l.UsedPct == nil || *l.UsedPct < 90 || !active(l, now) {
				continue
			}
			if l.Kind == LimitAllowance && *l.UsedPct >= 100 && l.Overage != nil && *l.Overage {
				out = append(out, Advisory{
					Provider: s.Provider, Level: LevelInfo,
					Text: fmt.Sprintf("%s %s allowance used up, overage on", s.Provider, l.Name),
				})
				continue
			}
			text := fmt.Sprintf("%s %s at %.0f%%", s.Provider, l.Name, *l.UsedPct)
			if l.ResetsAt != nil {
				text += ", resets in " + HumanDuration(l.ResetsAt.Sub(now))
			}
			out = append(out, Advisory{Provider: s.Provider, Level: LevelWarn, Text: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Text < out[j].Text
	})
	return out
}
