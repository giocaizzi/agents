// Package catalog is the model catalog of agents: model tiers plus
// model data synced from models.dev (first-party API prices), the LMArena
// leaderboard dataset (ranks) and Epoch AI (external benchmarks). It is pure:
// callers fetch the raw bytes and write the results.
//
// A model is keyed by its canonical id `<provider>/<id>`; its Provider is the
// models.dev key of the first-party provider (the canonical id's prefix), the
// same vocabulary as internal/provider. Prices are always that provider's API
// per-token prices, never a harness's or a reseller's.
package catalog

// Schema is the catalog format version.
const Schema = 2

// ArenaBoard is the LMArena text board ranks come from.
const ArenaBoard = "text_style_control"

// Arena category keys of Model.Arena that appear in more than one place.
const (
	CatHardPrompts          = "hardPrompts"
	CatInstructionFollowing = "instructionFollowing"
	CatMultiTurn            = "multiTurn"
)

// Tier names.
const (
	TierLightweight = "lightweight"
	TierVersatile   = "versatile"
	TierPowerful    = "powerful"
	TierExtra       = "extra"
)

// Tiers are the tier names, in display order.
func Tiers() []string {
	return []string{TierLightweight, TierVersatile, TierPowerful, TierExtra}
}

// Categories are the arena rank keys of Model.Arena, in display order.
func Categories() []string {
	return []string{
		"text", "coding", "agent", "webdev",
		CatHardPrompts, "math", CatInstructionFollowing, CatMultiTurn,
	}
}

// File is the top level of models.json: one key, so the chezmoi template data
// namespace stays clean.
type File struct {
	Catalog Catalog `json:"modelCatalog"`
}

// Catalog is the model catalog. Providers, Since, Ignore, Pin and Aliases are
// manual; sync rewrites Models, Sources, SyncedAt and Tiers (see tier.go).
type Catalog struct {
	Schema    int                 `json:"schema"`
	SyncedAt  string              `json:"syncedAt,omitzero"`
	Since     string              `json:"since"`
	Providers []string            `json:"providers"` // tracked first-party models.dev provider keys
	Sources   SourceInfo          `json:"sources"`
	Tiers     map[string][]string `json:"tiers"`
	Ignore    []string            `json:"ignore,omitzero"`
	Pin       map[string]string   `json:"pin,omitzero"` // canonical id -> tier, wins over the computed tier
	Aliases   Aliases             `json:"aliases,omitzero"`
	Models    map[string]Model    `json:"models"`
}

// SourceInfo records where the synced data came from.
type SourceInfo struct {
	Arena ArenaInfo `json:"arena"`
}

// ArenaInfo is the arena board and its publish date at sync time.
type ArenaInfo struct {
	PublishDate string `json:"publishDate,omitzero"`
	Board       string `json:"board"`
}

// Aliases map a source's model name to a canonical id; they win over Key matching.
type Aliases struct {
	Arena map[string]string `json:"arena,omitzero"`
	Epoch map[string]string `json:"epoch,omitzero"`
}

// Model is one model, keyed in Catalog.Models by its models.dev canonical id
// (`<provider>/<id>`, e.g. `anthropic/claude-opus-5-5`).
type Model struct {
	Name        string          `json:"name"`
	Provider    string          `json:"provider"` // first-party models.dev provider key
	ID          string          `json:"id"`       // model id at Provider's API
	ReleaseDate string          `json:"releaseDate,omitzero"`
	Context     int             `json:"context,omitzero"` // context window, tokens
	API         *Price          `json:"api,omitzero"`     // nil: no first-party API price (n/a)
	Arena       map[string]Rank `json:"arena,omitzero"`
	External    External        `json:"external,omitzero"`
}

// Price is the first-party API price in $ per million tokens; zero cache
// prices mean the provider lists none.
type Price struct {
	In         float64 `json:"in"`
	Out        float64 `json:"out"`
	CacheRead  float64 `json:"cacheRead,omitzero"`
	CacheWrite float64 `json:"cacheWrite,omitzero"`
}

// Rank is a model's place in one arena category; Variant is the reasoning
// effort of its best-ranked arena entry (e.g. "High"), empty when there is none.
type Rank struct {
	Rank    int     `json:"rank"`
	Rating  float64 `json:"rating"`
	Votes   int     `json:"votes,omitzero"`
	Variant string  `json:"variant,omitzero"`
}

// External holds Epoch AI benchmark scores; zero means no data.
type External struct {
	ECI              float64 `json:"eci,omitzero"`
	TerminalBench    float64 `json:"terminalBench,omitzero"`
	SWEBenchVerified float64 `json:"sweBenchVerified,omitzero"`
	METRHorizonMin   float64 `json:"metrHorizonMin,omitzero"`
	OSWorld          float64 `json:"osworld,omitzero"`
}

// ProviderModel is one models.dev entry of a tracked provider.
type ProviderModel struct {
	Provider    string // models.dev provider key
	ID          string // id at that provider
	Name        string
	CanonicalID string // canonical_model_id, or Provider/ID when absent
	ReleaseDate string
	Context     int
	Price       *Price // nil when models.dev lists no cost
	ToolCall    bool
	TextOutput  bool
	Deprecated  bool // status "deprecated"
}

// ArenaRow is one row of the LMArena leaderboard dataset.
type ArenaRow struct {
	Board       string // dataset config: text_style_control, webdev, agent
	Category    string // dataset category, e.g. overall, coding, hard_prompts
	ModelName   string // display name, e.g. "Claude Opus 5.5 (High)"
	Org         string
	Rank        int
	Rating      float64 // rating, or score on the agent board
	Votes       int     // vote_count, or session_count on the agent board
	PublishDate string
}

// EpochRow is one benchmark score from Epoch AI's benchmark_data.zip.
type EpochRow struct {
	Benchmark string // eci, terminalBench, sweBenchVerified, metrHorizonMin, osworld
	Model     string // `Model` (ECI) or `Model version` column
	Score     float64
}

// Sources is everything one sync fetched.
type Sources struct {
	ModelsDev []ProviderModel
	Arena     []ArenaRow
	Epoch     []EpochRow
	Today     string // YYYY-MM-DD, becomes SyncedAt
}

// Report is what a sync found that needs a human.
type Report struct {
	Unmatched []string // arena/epoch names of tracked orgs that matched no model
	Untiered  []string // canonical ids in Models with no tier
	Stale     []string // pinned ids no provider lists any more (kept)
	Missing   []string // pinned ids with no data at all (sync fails on them)
}
