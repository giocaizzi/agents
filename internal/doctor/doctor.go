// Package doctor is the advisory of agents: it compares the models
// the harness policy configures with the model catalog and the provider
// accounts the usage tool found, and says what is retired, drifted, beaten by
// a model available to the same accounts, missing an account, or near a
// capacity limit. It is pure: callers load the catalog, the policy and the
// usage snapshots, and render the findings.
package doctor

import (
	"fmt"
	"slices"
	"time"

	"github.com/giocaizzi/agents/internal/catalog"
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/usage"
)

// Agent roles of harnessPolicy.models.tiers, plus RoleDefault for a harness's
// own default model (codex, pi, opencode native settings).
const (
	RoleScout    = "scout"
	RoleWorker   = "worker"
	RoleDriver   = "driver"
	RoleEscalate = "escalate"
	RoleDefault  = "default"
)

// Thresholds of the checks.
const (
	// StaleAfter is the catalog age (now - syncedAt) above which it is stale.
	// syncedAt moves only when synced data changes, so this is well above
	// the weekly sync.
	StaleAfter = 30 * 24 * time.Hour
	// MinRankGain is how many arena places better a candidate must rank to be
	// "better-ranked".
	MinRankGain = 3
	// MinSaving is the fraction of the blended price a candidate must save to
	// be "cheaper".
	MinSaving = 0.2
	// LowHeadroom is the headroom (0-1) under which another provider's account
	// is hinted.
	LowHeadroom = 0.10
)

// Roles are the agent roles in display order.
func Roles() []string {
	return []string{RoleScout, RoleWorker, RoleDriver, RoleEscalate}
}

// RoleTier is the catalog tier an agent role belongs in; "" for RoleDefault
// (a default model is compared within its own tier) and unknown roles.
func RoleTier(role string) string {
	switch role {
	case RoleScout, RoleWorker:
		return catalog.TierLightweight
	case RoleDriver:
		return catalog.TierPowerful
	case RoleEscalate:
		return catalog.TierExtra
	}
	return ""
}

// Setting is one model a harness is configured to use for a role.
type Setting struct {
	Harness  string // claude, copilot, codex, antigravity, opencode, pi
	Role     string // one of Roles() or RoleDefault
	Model    string // as written in the policy: a bare id or "<provider>/<id>"
	Provider string // provider hint from the policy (pi defaultProvider); may be empty
	Key      string // where it is set, e.g. harnessPolicy.models.tiers.driver
}

// Input is everything Check reads.
type Input struct {
	Catalog   catalog.Catalog
	Settings  []Setting
	Harnesses []string         // the selected harnesses, in display order
	Snapshots []usage.Snapshot // the usage cache or a live run; nil when HaveUsage is false
	HaveUsage bool             // usage data exists (a cache was written or -live ran)
	Now       time.Time
}

// Level is how loud a finding is. Quiet mode prints only when a finding is
// LevelWarn or LevelSuggest.
type Level string

// Levels.
const (
	LevelWarn    Level = "warn"
	LevelSuggest Level = "suggest"
	LevelInfo    Level = "info"
)

// Kind names a check.
type Kind string

// Kinds.
const (
	KindStaleCatalog Kind = "stale-catalog" // (3) catalog older than StaleAfter
	KindUnknownModel Kind = "unknown-model" // (1) configured model not in the catalog
	KindTierDrift    Kind = "tier-drift"    // (1) configured role model outside its role's tier, or untiered
	KindNoUsage      Kind = "no-usage"      // no usage data: account checks skipped
	KindNoAccount    Kind = "no-account"    // (5) no account for the provider of a configured model
	KindCapacity     Kind = "capacity"      // (4) a limit at 90% or more, or an allowance used up
	KindHeadroom     Kind = "headroom"      // (4) another provider's account has headroom
	KindBetterModel  Kind = "better-model"  // (2) a better, newer or cheaper model is available
)

// ModelRef is a catalog model as a finding shows it.
type ModelRef struct {
	ID       string  `json:"id"` // canonical id <provider>/<id>
	Name     string  `json:"name"`
	Tier     string  `json:"tier,omitempty"`
	In       float64 `json:"in"`  // first-party API $ per Mtok
	Out      float64 `json:"out"` // first-party API $ per Mtok
	Category string  `json:"category,omitempty"`
	Rank     int     `json:"rank,omitempty"` // arena rank in Category; 0 when unranked
}

// Finding is one thing the doctor says. Harness is empty for a global finding
// (catalog, policy keys, no usage data). Text is the complete plain sentence;
// Fix is the command or key that resolves it, empty when there is nothing to do.
type Finding struct {
	Level     Level     `json:"level"`
	Kind      Kind      `json:"kind"`
	Harness   string    `json:"harness,omitempty"`
	Role      string    `json:"role,omitempty"`
	Text      string    `json:"text"`
	Fix       string    `json:"fix,omitempty"`
	Current   *ModelRef `json:"current,omitempty"`
	Suggested *ModelRef `json:"suggested,omitempty"`
	Reason    string    `json:"reason,omitempty"` // better-model: better-ranked, newer, cheaper
	Billing   string    `json:"billing,omitempty"`
}

// Loud reports whether quiet mode prints the findings: any warn or suggest.
func Loud(fs []Finding) bool {
	for _, f := range fs {
		if f.Level == LevelWarn || f.Level == LevelSuggest {
			return true
		}
	}
	return false
}

// harnessOrder is the display order of the harnesses.
func harnessOrder() []string {
	return []string{hClaude, hCopilot, hCodex, hAntigravity, hOpenCode, hPi}
}

// Harness names.
const (
	hClaude      = "claude"
	hCopilot     = "copilot"
	hCodex       = "codex"
	hAntigravity = "antigravity"
	hOpenCode    = "opencode"
	hPi          = "pi"
)

// Settings lists the configured models of the given harnesses from the
// policy, in harness then role order. Harness names it does not know are
// ignored.
func Settings(p config.Policy, harnesses []string) []Setting {
	var out []Setting
	for _, h := range harnessOrder() {
		if !slices.Contains(harnesses, h) {
			continue
		}
		for _, role := range Roles() {
			model := p.Models.Tiers[role].Model
			key := "harnessPolicy.models.tiers." + role
			if h == hCopilot {
				if b, ok := p.Native.Copilot.Agents["dev-tools:"+role]; ok && b.Model != "" {
					model = b.Model
					key = fmt.Sprintf("harnessPolicy.native.copilot.agents.%q", "dev-tools:"+role)
				}
			}
			if model != "" {
				out = append(out, Setting{Harness: h, Role: role, Model: model, Key: key})
			}
		}
		if s, ok := defaultSetting(p, h); ok {
			out = append(out, s)
		}
	}
	return out
}

// defaultSetting is the harness's own default model, for the harnesses whose
// native settings have one.
func defaultSetting(p config.Policy, h string) (Setting, bool) {
	s := Setting{Harness: h, Role: RoleDefault}
	switch h {
	case hCodex:
		if p.Native.Codex.Model == nil {
			return s, false
		}
		s.Model, s.Key = *p.Native.Codex.Model, "harnessPolicy.native.codex.model"
	case hPi:
		s.Model, s.Provider, s.Key = p.Native.Pi.DefaultModel, p.Native.Pi.DefaultProvider, "harnessPolicy.native.pi.defaultModel"
	case hOpenCode:
		s.Model, s.Key = p.Native.OpenCode.DefaultModel, "harnessPolicy.native.opencode.defaultModel"
	}
	return s, s.Model != ""
}

// Check runs every check and returns the findings: global ones first, then
// per harness in Input.Harnesses order.
func Check(in Input) []Finding {
	var out []Finding
	if f, ok := staleCatalog(in.Catalog, in.Now); ok {
		out = append(out, f)
	}
	rs := resolveAll(in.Catalog, in.Settings)
	out = append(out, unknownModels(rs)...)
	out = append(out, tierDrifts(in.Catalog, rs)...)
	if !in.HaveUsage {
		return append(out, Finding{
			Level: LevelInfo, Kind: KindNoUsage,
			Text: "no usage data yet: account, capacity and model checks skipped",
			Fix:  "agents usage -live",
		})
	}
	for _, h := range in.Harnesses {
		out = append(out, harnessFindings(in, h, rs)...)
	}
	return out
}

// staleCatalog is check (3): the catalog was never synced, or longer ago
// than StaleAfter.
func staleCatalog(c catalog.Catalog, now time.Time) (Finding, bool) {
	const fix = "agents models sync, or merge the weekly models-sync PR"
	if c.SyncedAt == "" {
		return Finding{Level: LevelWarn, Kind: KindStaleCatalog, Text: "model catalog never synced", Fix: fix}, true
	}
	synced, err := time.Parse(time.DateOnly, c.SyncedAt)
	if err != nil {
		return Finding{Level: LevelWarn, Kind: KindStaleCatalog, Text: fmt.Sprintf("model catalog syncedAt %q is not a date", c.SyncedAt), Fix: fix}, true
	}
	age := now.Sub(synced)
	if age <= StaleAfter {
		return Finding{}, false
	}
	days := int(age / (24 * time.Hour))
	return Finding{
		Level: LevelWarn, Kind: KindStaleCatalog,
		Text: fmt.Sprintf("model catalog synced %d days ago (%s)", days, c.SyncedAt),
		Fix:  fix,
	}, true
}
