package usage

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CopilotPlan maps copilot_plan (and whether the seat comes from an
// organization) to a Plan.
func CopilotPlan(copilotPlan string, orgSeat bool) Plan {
	var p Plan
	switch strings.ToLower(strings.TrimSpace(copilotPlan)) {
	case "free":
		p = Plan{Kind: KindFree, Billing: BillingFlat, Payer: PayerSelf, Name: "Free"}
	case "individual", namePro:
		p = Plan{Kind: KindIndividual, Billing: BillingMixed, Payer: PayerSelf, Name: "Pro"}
	case "pro_plus":
		p = Plan{Kind: KindIndividual, Billing: BillingMixed, Payer: PayerSelf, Name: "Pro+"}
	case "max":
		p = Plan{Kind: KindIndividual, Billing: BillingMixed, Payer: PayerSelf, Name: "Max"}
	case "business":
		p = Plan{Kind: KindTeam, Billing: BillingMixed, Payer: PayerOrg, Name: "Business seat"}
	case string(KindEnterprise):
		p = Plan{Kind: KindEnterprise, Billing: BillingMixed, Payer: PayerOrg, Name: nameEnterpriseSeat}
	default:
		p = Plan{Kind: KindUnknown, Billing: BillingUnknown, Payer: PayerUnknown}
	}
	p.Raw = copilotPlan
	if orgSeat && (p.Kind == KindIndividual || p.Kind == KindUnknown) {
		p.Payer = PayerOrg
	}
	return p
}

type copilotQuota struct {
	Entitlement      *float64 `json:"entitlement"`
	Remaining        *float64 `json:"remaining"`
	PercentRemaining *float64 `json:"percent_remaining"`
	Unlimited        bool     `json:"unlimited"`
	OveragePermitted *bool    `json:"overage_permitted"`
}

type copilotUser struct {
	Plan           string                     `json:"copilot_plan"`
	Orgs           []json.RawMessage          `json:"organization_login_list"`
	QuotaResetDate string                     `json:"quota_reset_date"`
	Snapshots      map[string]json.RawMessage `json:"quota_snapshots"`
}

const copilotPremium = "premium_interactions"

// copilotKeys orders the snapshot keys: the known three first, then the rest.
func copilotKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	rank := map[string]int{copilotPremium: 0, "chat": 1, "completions": 2}
	sort.Slice(keys, func(i, j int) bool {
		ri, iok := rank[keys[i]]
		rj, jok := rank[keys[j]]
		switch {
		case iok != jok:
			return iok
		case iok:
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	return keys
}

// copilotLimit maps one quota snapshot; ok is false for ones to omit.
func copilotLimit(key string, raw json.RawMessage, scope Scope, resets *time.Time) (Limit, bool) {
	var q copilotQuota
	if json.Unmarshal(raw, &q) != nil {
		return Limit{}, false
	}
	premium := key == copilotPremium
	unlimited := q.Unlimited || (q.Entitlement != nil && *q.Entitlement < 0)
	if unlimited && !premium {
		return Limit{}, false
	}
	if !unlimited && q.PercentRemaining == nil && q.Entitlement == nil {
		return Limit{}, false
	}
	name := spaced(key)
	if premium {
		name = "premium"
	}
	l := Limit{Name: name, Kind: LimitAllowance, Scope: scope, ResetsAt: resets, Overage: q.OveragePermitted, Unlimited: unlimited}
	if unlimited {
		return l, true
	}
	if q.Entitlement != nil && *q.Entitlement == 0 {
		// An explicit zero entitlement is "0 / 0 reported", not fully consumed.
		l.ResetsAt = nil
		l.Note = "none included"
		return l, true
	}
	switch {
	case q.PercentRemaining != nil:
		l.UsedPct = new(100 - *q.PercentRemaining)
	case q.Entitlement != nil && q.Remaining != nil && *q.Entitlement > 0:
		l.UsedPct = new(100 * (*q.Entitlement - *q.Remaining) / *q.Entitlement)
	}
	if q.Entitlement != nil {
		l.Cap = &Amount{Value: *q.Entitlement, Unit: unitRequests}
	}
	if q.Remaining != nil {
		l.Remaining = &Amount{Value: *q.Remaining, Unit: unitRequests}
	}
	return l, true
}

// ParseCopilotUser parses GET api.github.com/copilot_internal/user.
func ParseCopilotUser(body []byte) (Plan, []Limit, error) {
	var u copilotUser
	if err := json.Unmarshal(body, &u); err != nil {
		return Plan{}, nil, fmt.Errorf("parse copilot user: %w", err)
	}
	org := len(u.Orgs) > 0
	plan := CopilotPlan(u.Plan, org)
	scope := ScopeAccount
	if org {
		scope = ScopePool
	}
	var resets *time.Time
	if d, err := time.Parse("2006-01-02", u.QuotaResetDate); err == nil {
		resets = &d
	}
	var limits []Limit
	for _, k := range copilotKeys(u.Snapshots) {
		if l, ok := copilotLimit(k, u.Snapshots[k], scope, resets); ok {
			limits = append(limits, l)
		}
	}
	return plan, limits, nil
}

type copilotBilling struct {
	UsageItems []struct {
		NetAmount float64 `json:"netAmount"`
	} `json:"usageItems"`
}

// ParseCopilotBilling parses the user premium request / AI credit usage report
// of the GitHub billing REST API into one spend limit for the current month.
func ParseCopilotBilling(body []byte) (Limit, error) {
	var b copilotBilling
	if err := json.Unmarshal(body, &b); err != nil {
		return Limit{}, fmt.Errorf("parse copilot billing: %w", err)
	}
	var sum float64
	for _, it := range b.UsageItems {
		sum += it.NetAmount
	}
	return Limit{
		Name: "spend this month", Kind: LimitSpend, Scope: ScopeAccount,
		Used: &Amount{Value: sum, Unit: "USD"}, Note: "billed to you",
	}, nil
}
