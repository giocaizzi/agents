package catalog

import (
	"strings"
	"testing"
)

func TestValidateRequiresProviders(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Models["anthropic/claude-opus-5-5"] = Model{Name: "Claude Opus 5.5"}
	c.Tiers = map[string][]string{"powerful": {"anthropic/claude-opus-5-5"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid catalog: %v", err)
	}
	c.Providers = nil
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "providers") {
		t.Errorf("Validate() = %v, want a providers error", err)
	}
}

func TestValidateRejectsHandEditedTiers(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Models["anthropic/claude-opus-5-5"] = Model{Name: "Claude Opus 5.5"}
	c.Models["a/cheap"] = Model{Name: "Cheap", API: &Price{In: 0.1, Out: 0.1}, Arena: map[string]Rank{"text": {Rank: 1}}}
	c.Tiers = map[string][]string{"powerful": {"anthropic/claude-opus-5-5"}, "lightweight": {"a/cheap"}, "extra": {}}
	if err := c.Validate(); err != nil {
		t.Fatalf("rule-consistent tiers (empty tier allowed): %v", err)
	}
	c.Tiers = map[string][]string{"powerful": {"anthropic/claude-opus-5-5", "a/cheap"}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "tiers.powerful: differs from the rule") ||
		!strings.Contains(err.Error(), "tiers.lightweight: differs from the rule") {
		t.Errorf("Validate() = %v, want tiers.powerful and tiers.lightweight drift", err)
	}
}

func TestValidateTierDriftIgnoresPins(t *testing.T) {
	t.Parallel()
	price := &Price{In: 0.1, Out: 0.1}
	tests := []struct {
		name    string
		pin     map[string]string
		tiers   map[string][]string
		wantErr bool
	}{
		{
			"pin added without sync",
			map[string]string{"a/pinned": "powerful"},
			map[string][]string{"lightweight": {"a/cheap", "a/pinned"}},
			false,
		},
		{
			"pinned id in the right tier",
			map[string]string{"a/pinned": "powerful"},
			map[string][]string{"lightweight": {"a/cheap"}, "powerful": {"a/pinned"}},
			false,
		},
		{
			"hand-moved unpinned id",
			map[string]string{"a/pinned": "powerful"},
			map[string][]string{"powerful": {"a/cheap", "a/pinned"}},
			true,
		},
		{
			"pin removed without sync", nil,
			map[string][]string{"lightweight": {"a/cheap"}, "powerful": {"a/pinned"}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := baseCatalog()
			c.Models = map[string]Model{
				"a/cheap":  {Name: "Cheap", API: price, Arena: map[string]Rank{"text": {Rank: 1}}},
				"a/pinned": {Name: "Pinned", API: price, Arena: map[string]Rank{"text": {Rank: 2}}},
			}
			c.Pin = tt.pin
			c.Tiers = tt.tiers
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
