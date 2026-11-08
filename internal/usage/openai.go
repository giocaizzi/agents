package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// OpenAIPlan maps a ChatGPT plan_type to a Plan.
func OpenAIPlan(planType string) Plan {
	p := strings.ToLower(strings.TrimSpace(planType))
	metered := strings.HasSuffix(p, "_usage_based")
	base := strings.TrimSuffix(p, "_usage_based")
	billing := BillingFlat
	if metered {
		billing = BillingMetered
	}
	switch {
	case base == "free":
		return Plan{Kind: KindFree, Billing: BillingFlat, Payer: PayerSelf, Name: "Free", Raw: planType}
	case base == "go" || base == "plus" || base == namePro || base == "prolite" || base == "promax":
		return Plan{Kind: KindIndividual, Billing: billing, Payer: PayerSelf, Name: upperFirst(base), Raw: planType}
	case base == string(KindTeam) || base == "business" || strings.HasPrefix(base, "self_serve_business"):
		name := "Business seat"
		if base == string(KindTeam) {
			name = "Team seat"
		}
		return Plan{Kind: KindTeam, Billing: billing, Payer: PayerOrg, Name: name, Raw: planType}
	case strings.HasPrefix(base, "enterprise") || strings.HasPrefix(base, "ent") ||
		strings.HasPrefix(base, "edu") || strings.HasPrefix(base, "sci"):
		return Plan{Kind: KindEnterprise, Billing: billing, Payer: PayerOrg, Name: nameEnterpriseSeat, Raw: planType}
	}
	return Plan{Kind: KindUnknown, Billing: BillingUnknown, Payer: PayerUnknown, Raw: planType}
}

type openaiWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *int64   `json:"limit_window_seconds"`
	ResetAt            *int64   `json:"reset_at"`
}

type openaiRateLimit struct {
	Primary   *openaiWindow `json:"primary_window"`
	Secondary *openaiWindow `json:"secondary_window"`
}

type openaiCredits struct {
	HasCredits bool            `json:"has_credits"`
	Unlimited  bool            `json:"unlimited"`
	Balance    json.RawMessage `json:"balance"`
}

type openaiExtraLimit struct {
	openaiRateLimit

	LimitName string           `json:"limit_name"`
	LimitID   string           `json:"limit_id"`
	Feature   string           `json:"metered_feature"`
	RateLimit *openaiRateLimit `json:"rate_limit"`
}

func (x openaiExtraLimit) name() string {
	for _, n := range []string{x.LimitName, x.LimitID, x.Feature} {
		if n != "" {
			return n
		}
	}
	return "extra"
}

func (x openaiExtraLimit) rateLimit() openaiRateLimit {
	if x.RateLimit != nil {
		return *x.RateLimit
	}
	return x.openaiRateLimit
}

type openaiUsage struct {
	PlanType         string             `json:"plan_type"`
	RateLimit        *openaiRateLimit   `json:"rate_limit"`
	AdditionalLimits []openaiExtraLimit `json:"additional_rate_limits"`
	Credits          *openaiCredits     `json:"credits"`
	SpendControl     json.RawMessage    `json:"spend_control"`
}

func openaiWindowLimits(prefix string, rl openaiRateLimit, scope Scope) []Limit {
	var out []Limit
	for _, w := range []*openaiWindow{rl.Primary, rl.Secondary} {
		if w == nil || w.UsedPercent == nil {
			continue
		}
		name := unnamedWindow
		if w.LimitWindowSeconds != nil {
			name = humanSeconds(*w.LimitWindowSeconds)
		}
		if prefix != "" {
			name = prefix + " · " + name
		}
		out = append(out, Limit{Name: name, Kind: LimitWindow, Scope: scope, UsedPct: w.UsedPercent, ResetsAt: unixTime(w.ResetAt)})
	}
	sortWindows(out)
	return out
}

func creditsLimit(c *openaiCredits, scope Scope) (Limit, bool) {
	if c == nil || (!c.HasCredits && !c.Unlimited) {
		return Limit{}, false
	}
	l := Limit{Name: unitCredits, Kind: LimitCredits, Scope: scope, Unlimited: c.Unlimited}
	if v, ok := numberOrString(c.Balance); ok {
		l.Remaining = &Amount{Value: v, Unit: unitCredits}
	}
	return l, true
}

// ParseOpenAI parses GET chatgpt.com/backend-api/wham/usage.
func ParseOpenAI(body []byte) (Plan, []Limit, error) {
	var u openaiUsage
	if err := json.Unmarshal(body, &u); err != nil {
		return Plan{}, nil, fmt.Errorf("parse openai usage: %w", err)
	}
	plan := OpenAIPlan(u.PlanType)
	scope := scopeFor(plan)
	var limits []Limit
	if u.RateLimit != nil {
		limits = openaiWindowLimits("", *u.RateLimit, scope)
	}
	for _, x := range u.AdditionalLimits {
		limits = append(limits, openaiWindowLimits(x.name(), x.rateLimit(), scope)...)
	}
	if l, ok := creditsLimit(u.Credits, scope); ok {
		limits = append(limits, l)
	}
	if len(u.SpendControl) > 0 && string(u.SpendControl) != "null" {
		limits = append(limits, Limit{Name: "spend limit", Kind: LimitSpend, Scope: scope, Hidden: true, Note: "managed by org"})
	}
	return plan, limits, nil
}

type codexWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes *int64   `json:"window_minutes"`
	ResetsAt      *int64   `json:"resets_at"`
}

type codexRateLimits struct {
	PlanType  string         `json:"plan_type"`
	Primary   *codexWindow   `json:"primary"`
	Secondary *codexWindow   `json:"secondary"`
	Credits   *openaiCredits `json:"credits"`
}

type codexLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type       string           `json:"type"`
		RateLimits *codexRateLimits `json:"rate_limits"`
	} `json:"payload"`
}

// lastCodexSnapshot returns the last token_count line that carries rate_limits.
func lastCodexSnapshot(r io.Reader) (*codexLine, error) {
	br := bufio.NewReader(r)
	var last *codexLine
	for {
		line, err := br.ReadBytes('\n')
		if bytes.Contains(line, []byte("token_count")) {
			var l codexLine
			if json.Unmarshal(line, &l) == nil && l.Type == "event_msg" &&
				l.Payload.Type == "token_count" && l.Payload.RateLimits != nil {
				last = &l
			}
		}
		if errors.Is(err, io.EOF) {
			return last, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read codex session: %w", err)
		}
	}
}

// ParseCodexSession reads a Codex rollout JSONL and returns the plan and limits
// of its last token_count rate_limits snapshot, and the time of that line.
func ParseCodexSession(r io.Reader) (Plan, []Limit, time.Time, error) {
	last, err := lastCodexSnapshot(r)
	if err != nil {
		return Plan{}, nil, time.Time{}, err
	}
	if last == nil {
		return Plan{}, nil, time.Time{}, errors.New("codex session has no rate_limits snapshot")
	}
	rl := last.Payload.RateLimits
	plan := OpenAIPlan(rl.PlanType)
	scope := scopeFor(plan)
	var limits []Limit
	for _, w := range []*codexWindow{rl.Primary, rl.Secondary} {
		if w == nil || w.UsedPercent == nil {
			continue
		}
		name := unnamedWindow
		if w.WindowMinutes != nil {
			name = humanSeconds(*w.WindowMinutes * 60)
		}
		limits = append(limits, Limit{Name: name, Kind: LimitWindow, Scope: scope, UsedPct: w.UsedPercent, ResetsAt: unixTime(w.ResetsAt)})
	}
	sortWindows(limits)
	if l, ok := creditsLimit(rl.Credits, scope); ok {
		limits = append(limits, l)
	}
	var at time.Time
	if t := parseTime(last.Timestamp); t != nil {
		at = *t
	}
	return plan, limits, at, nil
}
