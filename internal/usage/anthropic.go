package usage

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AnthropicPlan maps Claude Code's claudeAiOauth.subscriptionType and
// rateLimitTier to a Plan.
func AnthropicPlan(subscriptionType, rateLimitTier string) Plan {
	raw := subscriptionType
	switch strings.ToLower(strings.TrimSpace(subscriptionType)) {
	case namePro:
		return Plan{Kind: KindIndividual, Billing: BillingFlat, Payer: PayerSelf, Name: "Pro", Raw: raw}
	case "max":
		name := "Max"
		tier := strings.ToLower(rateLimitTier)
		for _, m := range []string{"5x", "20x"} {
			if strings.HasSuffix(tier, "max_"+m) {
				name = "Max " + m
			}
		}
		return Plan{Kind: KindIndividual, Billing: BillingFlat, Payer: PayerSelf, Name: name, Raw: raw}
	case string(KindTeam):
		return Plan{Kind: KindTeam, Billing: BillingFlat, Payer: PayerOrg, Name: "Team seat", Raw: raw}
	case string(KindEnterprise):
		return Plan{Kind: KindEnterprise, Billing: BillingUnknown, Payer: PayerOrg, Name: nameEnterpriseSeat, Raw: raw}
	}
	return Plan{Kind: KindUnknown, Billing: BillingUnknown, Payer: PayerUnknown, Raw: raw}
}

type anthropicWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

type anthropicExtra struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

func anthropicWindowName(key string) string {
	switch {
	case key == "five_hour":
		return "5h"
	case key == "seven_day":
		return "7d"
	case strings.HasPrefix(key, "seven_day_"):
		return "7d " + spaced(strings.TrimPrefix(key, "seven_day_"))
	}
	return spaced(key)
}

func anthropicKnown(key string) bool {
	switch key {
	case "five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet":
		return true
	}
	return false
}

const noSeatWindow = "no per-seat window (org-managed or usage-based)"

// anthropicWindowLimit maps one top-level key; ok is false for keys that are
// not windows and for null windows on individual plans.
func anthropicWindowLimit(key string, raw json.RawMessage, scope Scope) (Limit, bool) {
	var w anthropicWindow
	isNull := string(raw) == "null"
	if !isNull && json.Unmarshal(raw, &w) != nil {
		return Limit{}, false // not an object
	}
	if w.Utilization == nil {
		// A null window shows only for a known key on a seat; unknown keys without a number are not windows.
		if !anthropicKnown(key) || scope != ScopeSeat {
			return Limit{}, false
		}
		return Limit{Name: anthropicWindowName(key), Kind: LimitWindow, Scope: scope, Note: noSeatWindow}, true
	}
	l := Limit{Name: anthropicWindowName(key), Kind: LimitWindow, Scope: scope, UsedPct: w.Utilization}
	if w.ResetsAt != nil {
		l.ResetsAt = parseTime(*w.ResetsAt)
	}
	return l, true
}

func anthropicExtraLimit(raw json.RawMessage, scope Scope) (Limit, bool) {
	var e anthropicExtra
	if json.Unmarshal(raw, &e) != nil || !e.IsEnabled {
		return Limit{}, false
	}
	l := Limit{Name: "extra usage", Kind: LimitSpend, Scope: scope, UsedPct: e.Utilization, Overage: new(true)}
	if e.UsedCredits != nil {
		l.Used = &Amount{Value: *e.UsedCredits, Unit: unitCredits}
	}
	if e.MonthlyLimit != nil {
		l.Cap = &Amount{Value: *e.MonthlyLimit, Unit: unitCredits}
	}
	return l, true
}

// ParseAnthropic parses GET api.anthropic.com/api/oauth/usage.
func ParseAnthropic(body []byte, plan Plan) ([]Limit, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("parse anthropic usage: %w", err)
	}
	scope := scopeFor(plan)
	var limits []Limit
	for key, raw := range top {
		if key == "extra_usage" {
			continue
		}
		if l, ok := anthropicWindowLimit(key, raw, scope); ok {
			limits = append(limits, l)
		}
	}
	sortWindows(limits)
	if raw, ok := top["extra_usage"]; ok {
		if l, enabled := anthropicExtraLimit(raw, scope); enabled {
			limits = append(limits, l)
		}
	}
	return limits, nil
}
