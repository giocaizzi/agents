package catalog

import (
	"bytes"
	"cmp"
	"maps"
	"regexp"
	"slices"
	"strings"
)

const (
	boardWebdev = "webdev"
	boardAgent  = "agent"

	catOverall = "overall"
	catCoding  = "coding"
	catMath    = "math"

	benchECI        = "eci"
	colModelVersion = "Model version"
)

var variantGroup = regexp.MustCompile(`\s*\(([^()]*)\)\s*$`)

// arenaKey maps (board, dataset category) to the Model.Arena key.
func arenaKey(board, category string) (string, bool) {
	switch board {
	case ArenaBoard:
		k, ok := map[string]string{
			catOverall:              "text",
			catCoding:               catCoding,
			"hard_prompts":          CatHardPrompts,
			catMath:                 catMath,
			"instruction_following": CatInstructionFollowing,
			"multi_turn":            CatMultiTurn,
		}[category]
		return k, ok
	case boardWebdev:
		return boardWebdev, category == catOverall
	case boardAgent:
		return boardAgent, category == catOverall
	}
	return "", false
}

// Apply merges freshly fetched sources into c, keeping its manual keys. It
// returns a new catalog and never mutates c's maps.
func Apply(c Catalog, src Sources) (Catalog, Report) {
	var rep Report
	out := c

	fresh := freshModels(c, src.ModelsDev)
	models := maps.Clone(fresh)
	for _, id := range sortedKeys(c.Pin) {
		if _, ok := fresh[id]; ok {
			continue
		}
		if old, ok := c.Models[id]; ok {
			models[id] = old
			rep.Stale = append(rep.Stale, id)
		} else {
			rep.Missing = append(rep.Missing, id)
		}
	}

	idx := newIndex(models)
	for id, m := range models {
		m.Arena = nil
		m.External = External{}
		models[id] = m
	}

	out.Schema = Schema
	out.Sources.Arena = ArenaInfo{Board: ArenaBoard, PublishDate: arenaPublishDate(c, src.Arena)}
	rep.Unmatched = append(rep.Unmatched, applyArena(c, idx, models, fresh, src.Arena)...)
	rep.Unmatched = append(rep.Unmatched, applyEpoch(c, idx, models, src.Epoch)...)
	out.Models = models
	out.Tiers = ComputeTiers(models, c.Pin)

	out.SyncedAt = syncedAt(c, out, src.Today)

	rep.Untiered = out.Untiered()
	rep.Unmatched = append(rep.Unmatched, danglingAliases(c.Aliases, models)...)
	rep.Unmatched = sortedUnique(rep.Unmatched)
	rep.Stale = sortedUnique(rep.Stale)
	rep.Missing = sortedUnique(rep.Missing)
	return out, rep
}

// syncedAt keeps c's date when the data did not change, so a sync without news
// leaves the file (and the page rendered from it) byte-identical.
func syncedAt(c, out Catalog, today string) string {
	out.SyncedAt = c.SyncedAt
	old, err1 := Marshal(c)
	cur, err2 := Marshal(out)
	if err1 == nil && err2 == nil && bytes.Equal(old, cur) {
		return c.SyncedAt
	}
	return today
}

// freshModels builds the models of the kept canonical ids from models.dev,
// using only first-party entries of the tracked providers.
func freshModels(c Catalog, entries []ProviderModel) map[string]Model {
	groups := map[string][]ProviderModel{}
	for _, e := range entries {
		org, _, _ := strings.Cut(e.CanonicalID, "/")
		if e.Provider != org || !slices.Contains(c.Providers, e.Provider) {
			continue
		}
		groups[e.CanonicalID] = append(groups[e.CanonicalID], e)
	}
	fresh := map[string]Model{}
	for id, offers := range groups {
		if slices.Contains(c.Ignore, id) && c.Pin[id] == "" {
			continue
		}
		if c.Pin[id] != "" || keep(c.Since, offers) {
			fresh[id] = buildModel(id, offers)
		}
	}
	return fresh
}

// keep reports whether some first-party entry is a recent, live text model
// with tool calling.
func keep(since string, offers []ProviderModel) bool {
	for _, o := range offers {
		if o.ReleaseDate >= since && o.TextOutput && o.ToolCall && !o.Deprecated {
			return true
		}
	}
	return false
}

// buildModel takes every field from the entry whose id is the canonical id's
// part after the provider, else the first entry by id.
func buildModel(id string, offers []ProviderModel) Model {
	org, part, _ := strings.Cut(id, "/")
	src := slices.MinFunc(offers, func(a, b ProviderModel) int { return strings.Compare(a.ID, b.ID) })
	if i := slices.IndexFunc(offers, func(o ProviderModel) bool { return o.ID == part }); i >= 0 {
		src = offers[i]
	}
	m := Model{Name: src.Name, Provider: org, ID: src.ID, ReleaseDate: src.ReleaseDate, Context: src.Context}
	if src.Price != nil {
		p := *src.Price
		m.API = &p
	}
	return m
}

// index maps a Key to the canonical ids of the models it identifies.
type index map[string][]string

func newIndex(models map[string]Model) index {
	idx := index{}
	add := func(key, id string) {
		if key != "" && !slices.Contains(idx[key], id) {
			idx[key] = append(idx[key], id)
		}
	}
	for _, id := range sortedKeys(models) {
		m := models[id]
		_, part, _ := strings.Cut(id, "/")
		add(Key(part), id)
		add(Key(m.Name), id)
		add(Key(m.ID), id)
	}
	return idx
}

// match resolves a source name to canonical ids: the alias if there is one,
// else Key matching. A key shared by several models is ambiguous: it matches
// none of them, and ambiguous lists them.
func (idx index) match(models map[string]Model, aliases map[string]string, name string) (ids, ambiguous []string) {
	if target, ok := aliases[name]; ok {
		if _, ok := models[target]; ok {
			return []string{target}, nil
		}
		return nil, nil
	}
	ids = idx[Key(name)]
	if len(ids) > 1 {
		return nil, ids
	}
	return ids, nil
}

func arenaPublishDate(c Catalog, rows []ArenaRow) string {
	date := ""
	for _, r := range rows {
		if r.Board == ArenaBoard {
			date = max(date, r.PublishDate)
		}
	}
	if date == "" {
		return c.Sources.Arena.PublishDate
	}
	return date
}

func splitVariant(name string) (base, variant string) {
	m := variantGroup.FindStringSubmatchIndex(name)
	if m == nil {
		return strings.TrimSpace(name), ""
	}
	return strings.TrimSpace(name[:m[0]]), strings.TrimSpace(name[m[2]:m[3]])
}

// better reports whether a beats b: lower rank, then higher rating, then name.
func better(a, b ArenaRow) bool {
	return cmp.Or(
		cmp.Compare(a.Rank, b.Rank),
		-cmp.Compare(a.Rating, b.Rating),
		strings.Compare(a.ModelName, b.ModelName),
	) < 0
}

// unmatchedTopRank limits the unmatched report to arena rows ranked this high or
// better in their category: the rest are legacy models nobody needs to alias.
const unmatchedTopRank = 20

// reportUnmatched reports whether an unmatched arena row is worth listing: a
// tracked org's model ranked in the top unmatchedTopRank.
func reportUnmatched(r ArenaRow, orgs map[string]bool) bool {
	return r.Rank <= unmatchedTopRank && orgs[strings.ToLower(r.Org)]
}

// unmatchedLine is the report line of an unmatched arena row; ambiguous lists
// the models its key is shared by, if any.
func unmatchedLine(r ArenaRow, ambiguous []string) string {
	base, _ := splitVariant(r.ModelName)
	line := "arena: " + base
	if len(ambiguous) > 0 {
		line += " (ambiguous: " + strings.Join(ambiguous, ", ") + ")"
	}
	return line
}

// applyArena sets Model.Arena and returns the unmatched report lines.
func applyArena(c Catalog, idx index, models, fresh map[string]Model, rows []ArenaRow) []string {
	best := map[string]map[string]ArenaRow{}
	orgs := orgSet(fresh)
	var unmatched []string
	for _, r := range rows {
		key, ok := arenaKey(r.Board, r.Category)
		if !ok {
			continue
		}
		ids, ambiguous := idx.match(models, c.Aliases.Arena, r.ModelName)
		if len(ids) == 0 {
			if reportUnmatched(r, orgs) {
				unmatched = append(unmatched, unmatchedLine(r, ambiguous))
			}
			continue
		}
		for _, id := range ids {
			if best[id] == nil {
				best[id] = map[string]ArenaRow{}
			}
			if cur, ok := best[id][key]; !ok || better(r, cur) {
				best[id][key] = r
			}
		}
	}
	for id, byKey := range best {
		m := models[id]
		m.Arena = map[string]Rank{}
		for key, r := range byKey {
			_, variant := splitVariant(r.ModelName)
			m.Arena[key] = Rank{Rank: r.Rank, Rating: r.Rating, Votes: r.Votes, Variant: variant}
		}
		models[id] = m
	}
	return unmatched
}

// applyEpoch rebuilds Model.External and returns one report line per distinct
// Epoch model name whose key is ambiguous (shared by several models) and has no
// alias; aliases.epoch resolves it. Other unmatched Epoch rows are not
// reported: Epoch's history is mostly legacy models, and aliases.epoch covers a
// gap.
func applyEpoch(c Catalog, idx index, models map[string]Model, rows []EpochRow) []string {
	var unmatched []string
	for _, r := range rows {
		ids, ambiguous := idx.match(models, c.Aliases.Epoch, r.Model)
		if len(ids) == 0 && len(ambiguous) > 0 {
			unmatched = append(unmatched, "epoch: "+r.Model+" (ambiguous: "+strings.Join(sortedUnique(slices.Clone(ambiguous)), ", ")+")")
		}
		for _, id := range ids {
			m := models[id]
			setExternal(&m.External, r)
			models[id] = m
		}
	}
	return unmatched
}

func setExternal(e *External, r EpochRow) {
	var field *float64
	switch r.Benchmark {
	case benchECI:
		field = &e.ECI
	case "terminalBench":
		field = &e.TerminalBench
	case "sweBenchVerified":
		field = &e.SWEBenchVerified
	case "metrHorizonMin":
		field = &e.METRHorizonMin
	case "osworld":
		field = &e.OSWorld
	default:
		return
	}
	*field = max(*field, r.Score)
}

func orgSet(models map[string]Model) map[string]bool {
	orgs := map[string]bool{}
	for id := range models {
		org, _, _ := strings.Cut(id, "/")
		orgs[org] = true
	}
	return orgs
}

func sortedUnique(s []string) []string {
	slices.Sort(s)
	return slices.Compact(s)
}

// danglingAliases reports aliases whose target is not in models, so a typo in
// aliases.* does not go unnoticed.
func danglingAliases(a Aliases, models map[string]Model) []string {
	var out []string
	for source, m := range map[string]map[string]string{"arena": a.Arena, "epoch": a.Epoch} {
		for name, target := range m {
			if _, ok := models[target]; !ok {
				out = append(out, "alias "+source+": "+name+" -> "+target+" (not in models)")
			}
		}
	}
	return out
}
