package catalog

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// priced is a ranked model whose blended price is exactly blended: In = Out = blended.
func priced(blended, eci float64, date string) Model {
	return Model{
		API:         &Price{In: blended, Out: blended},
		Arena:       map[string]Rank{"text": {Rank: 1}},
		External:    External{ECI: eci},
		ReleaseDate: date,
	}
}

func TestBlendedPrice(t *testing.T) {
	t.Parallel()
	if got := BlendedPrice(Price{In: 2, Out: 6}); got != 3 {
		t.Errorf("BlendedPrice = %v, want 3", got)
	}
}

func TestComputeTiersBandEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		blended float64
		want    string
	}{
		{0.1, "lightweight"},
		{2.49, "lightweight"},
		{BandVersatile, "versatile"},
		{6.99, "versatile"},
		{BandPowerful, "powerful"},
		{14.99, "powerful"},
		{BandExtra, "extra"},
		{50, "extra"},
	}
	for _, tt := range tests {
		got := ComputeTiers(map[string]Model{"a/m": priced(tt.blended, 0, "")}, nil)
		if diff := cmp.Diff(map[string][]string{tt.want: {"a/m"}}, got); diff != "" {
			t.Errorf("blended %v (-want +got):\n%s", tt.blended, diff)
		}
	}
}

// TestBandEdgesDecimalPrices pins decimal prices whose blend lands exactly on a
// limit: the band must not depend on the CPU's multiply-add fusion.
func TestBandEdgesDecimalPrices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		price Price
		want  string
	}{
		{Price{In: 5.35, Out: 11.95}, "powerful"},
		{Price{In: 2.8, Out: 1.6}, "versatile"},
		{Price{In: 3.3, Out: 0.1}, "versatile"},
		{Price{In: 6.1, Out: 9.7}, "powerful"},
		{Price{In: 12.5, Out: 22.5}, "extra"},
		{Price{In: 13.1, Out: 20.7}, "extra"},
	}
	for _, tt := range tests {
		m := Model{API: &Price{In: tt.price.In, Out: tt.price.Out}, Arena: map[string]Rank{"text": {Rank: 1}}}
		got := ComputeTiers(map[string]Model{"a/m": m}, nil)
		if diff := cmp.Diff(map[string][]string{tt.want: {"a/m"}}, got); diff != "" {
			t.Errorf("%+v (blend %v) (-want +got):\n%s", tt.price, BlendedPrice(tt.price), diff)
		}
	}
}

func eciModels(scores ...float64) map[string]Model {
	models := map[string]Model{}
	for i, s := range scores {
		models["a/m"+string(rune('a'+i))] = priced(1, s, "")
	}
	return models
}

func TestECICut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		scores []float64
		want   float64
	}{
		{"none", nil, 0},
		{"zero scores ignored", []float64{0, 0}, 0},
		{"one", []float64{120}, 120},
		{"three, k=1", []float64{100, 130, 110}, 130},
		{"eight, k=2", []float64{1, 2, 3, 4, 5, 6, 7, 8}, 7},
		{"zeros do not count toward n", []float64{0, 0, 0, 0, 0, 0, 1, 2, 3, 4}, 4},
	}
	for _, tt := range tests {
		if got := ECICut(eciModels(tt.scores...)); got != tt.want {
			t.Errorf("%s: cut = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestComputeTiersPromotion(t *testing.T) {
	t.Parallel()
	models := map[string]Model{
		"a/light":    priced(1, 150, ""),  // top ECI: lightweight -> versatile
		"a/versa":    priced(5, 140, ""),  // top ECI too (k=2): versatile -> powerful
		"a/power":    priced(10, 130, ""), // below the cut: stays powerful
		"a/powerTop": priced(10, 200, ""), // top ECI: powerful stays, never extra
		"a/extra":    priced(20, 210, ""),
		"a/noECI":    priced(1, 0, ""),
		"a/low1":     priced(1, 100, ""),
		"a/low2":     priced(1, 90, ""),
	}
	// 7 ECIs, k = 1: the cut is 210, only the extra model reaches it and it stays extra.
	got := ComputeTiers(models, nil)
	want := map[string][]string{
		"lightweight": {"a/light", "a/low1", "a/low2", "a/noECI"},
		"versatile":   {"a/versa"},
		"powerful":    {"a/power", "a/powerTop"},
		"extra":       {"a/extra"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("k=1 (-want +got):\n%s", diff)
	}

	// 8 ECIs, k = 2: the cut is 200, the powerful model at 200 stays powerful.
	models["a/more"] = priced(1, 80, "")
	got = ComputeTiers(models, nil)
	if diff := cmp.Diff([]string{"a/versa"}, got["versatile"]); diff != "" {
		t.Errorf("k=2 versatile (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"a/power", "a/powerTop"}, got["powerful"]); diff != "" {
		t.Errorf("k=2 powerful (-want +got):\n%s", diff)
	}
}

func TestComputeTiersPromotesOneBand(t *testing.T) {
	t.Parallel()
	models := map[string]Model{
		"a/light": priced(1, 150, ""),
		"a/versa": priced(5, 140, ""),
		"a/x":     priced(1, 10, ""),
		"a/y":     priced(1, 10, ""),
	}
	// 4 ECIs, k = 1: cut 150, only a/light moves up.
	got := ComputeTiers(models, nil)
	want := map[string][]string{
		"lightweight": {"a/x", "a/y"},
		"versatile":   {"a/light", "a/versa"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
	// With k = 2 (8 ECIs) the versatile model moves up as well.
	for _, id := range []string{"a/p", "a/q", "a/r", "a/s"} {
		models[id] = priced(1, 5, "")
	}
	got = ComputeTiers(models, nil)
	if diff := cmp.Diff([]string{"a/versa"}, got["powerful"]); diff != "" {
		t.Errorf("powerful (-want +got):\n%s", diff)
	}
}

func TestComputeTiersUntiered(t *testing.T) {
	t.Parallel()
	noPrice := priced(1, 200, "")
	noPrice.API = nil
	noRank := priced(1, 0, "")
	noRank.Arena = nil
	eciOnly := priced(1, 100, "")
	eciOnly.Arena = nil
	models := map[string]Model{"a/noprice": noPrice, "a/norank": noRank, "a/ecionly": eciOnly}

	got := ComputeTiers(models, nil)
	if diff := cmp.Diff(map[string][]string{"lightweight": {"a/ecionly"}}, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
	for id, want := range map[string]string{"a/noprice": ReasonNoPrice, "a/norank": ReasonNoRanking, "a/ecionly": ""} {
		if r := UntieredReason(models[id]); r != want {
			t.Errorf("%s reason = %q, want %q", id, r, want)
		}
	}
}

func TestComputeTiersPinWins(t *testing.T) {
	t.Parallel()
	noPrice := Model{}
	models := map[string]Model{"a/cheap": priced(1, 0, ""), "a/bare": noPrice}
	got := ComputeTiers(models, map[string]string{"a/cheap": "extra", "a/bare": "powerful"})
	want := map[string][]string{"extra": {"a/cheap"}, "powerful": {"a/bare"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
	// A pin on an id absent from models does nothing.
	if got := ComputeTiers(nil, map[string]string{"a/gone": "extra"}); len(got) != 0 {
		t.Errorf("tiers = %v, want none", got)
	}
}

func TestComputeTiersOrder(t *testing.T) {
	t.Parallel()
	models := map[string]Model{
		"a/old":   priced(1, 0, "2026-01-01"),
		"a/new":   priced(1, 0, "2026-09-01"),
		"b/same":  priced(1, 0, "2026-05-01"),
		"a/same":  priced(1, 0, "2026-05-01"),
		"a/nodat": priced(1, 0, ""),
	}
	got := ComputeTiers(models, nil)["lightweight"]
	want := []string{"a/new", "a/same", "b/same", "a/old", "a/nodat"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestApplyComputesTiers(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	src := arenaSrc(ArenaRow{Board: ArenaBoard, Category: "overall", ModelName: "Claude Opus 5.5", Org: "anthropic", Rank: 1, PublishDate: "d"})
	src.ModelsDev = []ProviderModel{
		pm("anthropic", "claude-opus-5-5", "Claude Opus 5.5", "anthropic/claude-opus-5-5", "2026-09-22", 4, 20),
		pm("openai", "gpt-6-luna", "GPT-6 Luna", "openai/gpt-6-luna", "2026-09-22", 0.1, 0.5),
	}
	c.Pin = map[string]string{"anthropic/claude-opus-5-5": "extra"}
	got, rep := Apply(c, src)
	if diff := cmp.Diff(map[string][]string{"extra": {"anthropic/claude-opus-5-5"}}, got.Tiers); diff != "" {
		t.Errorf("tiers (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"openai/gpt-6-luna"}, rep.Untiered); diff != "" {
		t.Errorf("untiered (-want +got):\n%s", diff)
	}
}

func TestValidatePin(t *testing.T) {
	t.Parallel()
	c := baseCatalog()
	c.Models["anthropic/claude-opus-5-5"] = Model{}
	c.Tiers = map[string][]string{"powerful": {"anthropic/claude-opus-5-5"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid catalog: %v", err)
	}

	c.Pin["openai/x"] = "huge"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "unknown tier") {
		t.Errorf("bad pin value: Validate() = %v", err)
	}
	delete(c.Pin, "openai/x")

	c.Pin["openai/gpt-realtime-2.1"] = "extra" // also in Ignore
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "ignore") {
		t.Errorf("pin of an ignored id: Validate() = %v", err)
	}
}
