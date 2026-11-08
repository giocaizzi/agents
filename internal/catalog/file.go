package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
)

// Parse decodes models.json.
func Parse(data []byte) (Catalog, error) {
	var f File
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Catalog{}, fmt.Errorf("decode model catalog: %w", err)
	}
	return f.Catalog, nil
}

// Marshal encodes c deterministically: sorted map keys, 2-space indent,
// trailing newline.
func Marshal(c Catalog) ([]byte, error) {
	data, err := json.MarshalIndent(File{Catalog: c}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode model catalog: %w", err)
	}
	return append(data, '\n'), nil
}

// Validate checks the catalog: known schema, providers, tier and pin names, no
// pin on an ignored id, every tiered id present in Models, no id in two tiers, tiers equal to the rule.
func (c Catalog) Validate() error {
	var errs []error
	if c.Schema != Schema {
		errs = append(errs, fmt.Errorf("schema %d, want %d", c.Schema, Schema))
	}
	if len(c.Providers) == 0 {
		errs = append(errs, errors.New("providers: empty (list the tracked models.dev provider keys)"))
	}
	for _, id := range sortedKeys(c.Pin) {
		if !slices.Contains(Tiers(), c.Pin[id]) {
			errs = append(errs, fmt.Errorf("pin.%s: unknown tier %q (want one of %v)", id, c.Pin[id], Tiers()))
		}
		if slices.Contains(c.Ignore, id) {
			errs = append(errs, fmt.Errorf("pin.%s: the id is also in ignore", id))
		}
	}
	seen := map[string]string{}
	for _, tier := range sortedKeys(c.Tiers) {
		if !slices.Contains(Tiers(), tier) {
			errs = append(errs, fmt.Errorf("tiers.%s: unknown tier (want one of %v)", tier, Tiers()))
		}
		for _, id := range c.Tiers[tier] {
			if prev, ok := seen[id]; ok {
				errs = append(errs, fmt.Errorf("tiers.%s: %s is already in tiers.%s", tier, id, prev))
				continue
			}
			seen[id] = tier
			if _, ok := c.Models[id]; !ok {
				errs = append(errs, fmt.Errorf("tiers.%s: %s is not in models (run models sync, or fix the id)", tier, id))
			}
		}
	}
	errs = append(errs, c.tierDrift()...)
	return errors.Join(errs...)
}

// tierDrift reports the tiers that differ from the rule, so a hand edit of
// tiers fails; an absent and an empty tier are the same. Pinned ids are left
// out of both sides: a pin takes effect at the next sync, so adding one must
// not fail Validate before it (pins do not move the ECI cut, so the other ids
// are unaffected). Removing a pin whose id still sits in its pinned tier fails,
// as the id is no longer pinned.
func (c Catalog) tierDrift() []error {
	var errs []error
	want := ComputeTiers(c.Models, c.Pin)
	for _, tier := range Tiers() {
		if !slices.Equal(withoutPinned(c.Tiers[tier], c.Pin), withoutPinned(want[tier], c.Pin)) {
			errs = append(errs, fmt.Errorf("tiers.%s: differs from the rule (run agents models sync)", tier))
		}
	}
	return errs
}

// withoutPinned returns ids without the pinned ones, order kept.
func withoutPinned(ids []string, pin map[string]string) []string {
	var out []string
	for _, id := range ids {
		if _, ok := pin[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

// TierOf returns the tier of id, or "" when it has none.
func (c Catalog) TierOf(id string) string {
	for tier, ids := range c.Tiers {
		if slices.Contains(ids, id) {
			return tier
		}
	}
	return ""
}

// Untiered lists, sorted, the ids in Models that have no tier and are not ignored.
func (c Catalog) Untiered() []string {
	var out []string
	for _, id := range sortedKeys(c.Models) {
		if c.TierOf(id) == "" && !slices.Contains(c.Ignore, id) {
			out = append(out, id)
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
