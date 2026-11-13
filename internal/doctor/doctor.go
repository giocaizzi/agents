// Package doctor is the advisory of agents: it compares the models
// the configured tiers name with the model catalog and the provider
// accounts the usage tool found, and says what is retired, drifted, beaten by
// a model available to the same accounts, missing an account, or near a
// capacity limit. It is pure: callers load the catalog, the config and the
// usage snapshots, and render the findings.
package doctor

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/giocaizzi/agents/internal/catalog"
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/usage"
)

// Thresholds of the checks.
const (
	// StaleAfter is the catalog age (now - syncedAt) above which it is stale.
	// syncedAt moves only when synced data changes, so this is well above
	// a regular sync.
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

// Setting is one model a harness gets for a configured tier.
type Setting struct {
	Harness string // claude, copilot, codex, antigravity, opencode, pi
	Tier    string // the tier name in the config
	Model   string // as written in the config: a bare id or "<provider>/<id>"
	Key     string // where it is set, e.g. models.tiers.driver.claude
}

// Input is everything Check reads.
type Input struct {
	Catalog   catalog.Catalog
	Settings  []Setting
	Harnesses []string         // the selected harnesses, in display order
	Snapshots []usage.Snapshot // the usage cache; nil when HaveUsage is false
	HaveUsage bool             // the usage cache exists (agents usage wrote it)
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
	KindTierDrift    Kind = "tier-drift"    // (1) configured tier model without a catalog tier
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
// (catalog, config keys, no usage data). Text is the complete plain sentence;
// Fix is the command or key that resolves it, empty when there is nothing to do.
type Finding struct {
	Level     Level     `json:"level"`
	Kind      Kind      `json:"kind"`
	Harness   string    `json:"harness,omitempty"`
	Tier      string    `json:"tier,omitempty"`
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

// Settings lists the models the config's tiers give the given harnesses, in
// harness order, then the default tier, then the other tiers by name. Harness
// names it does not know are ignored.
func Settings(c config.Config, harnesses []string) []Setting {
	names := slices.Sorted(maps.Keys(c.Models.Tiers))
	if d := c.Models.Default; d != "" {
		if i := slices.Index(names, d); i > 0 {
			names = slices.Insert(slices.Delete(names, i, i+1), 0, d)
		}
	}
	var out []Setting
	for _, h := range harnessOrder() {
		if !slices.Contains(harnesses, h) {
			continue
		}
		for _, name := range names {
			if model := c.Models.Tiers[name].Model(config.Harness(h)); model != "" {
				out = append(out, Setting{Harness: h, Tier: name, Model: model, Key: fmt.Sprintf("models.tiers.%s.%s", name, h)})
			}
		}
	}
	return out
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
			Fix:  "agents usage",
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
	const fix = "agents models sync"
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
