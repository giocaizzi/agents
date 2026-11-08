package catalog

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/term"
)

var update = flag.Bool("update", false, "rewrite golden files")

func sample() Catalog {
	rank := func(r int, v string) Rank { return Rank{Rank: r, Variant: v} }
	return Catalog{
		Schema:    Schema,
		SyncedAt:  "2026-11-06",
		Providers: []string{"anthropic", "openai", "google"},
		Sources:   SourceInfo{Arena: ArenaInfo{Board: ArenaBoard, PublishDate: "2026-10-28"}},
		Tiers: map[string][]string{
			"lightweight": {"openai/gpt-6-luna", "google/gemma-4-31b-it"},
			"versatile":   {"anthropic/claude-sonnet-5-5", "openai/ghost"},
			"powerful":    {"anthropic/claude-opus-5-5"},
		},
		Models: map[string]Model{
			"openai/gpt-6-luna": {
				Name: "GPT-6 Luna", Provider: "openai", ID: "gpt-6-luna", Context: 1050000,
				API:   &Price{In: 0.1, Out: 0.5, CacheRead: 0.01},
				Arena: map[string]Rank{"text": rank(12, "Low"), "coding": rank(60, "")},
			},
			"google/gemma-4-31b-it": {
				Name: "Gemma 4 31B", Provider: "google", ID: "gemma-4-31b-it", Context: 262144,
				Arena: map[string]Rank{"text": rank(30, "")},
			},
			"anthropic/claude-sonnet-5-5": {
				Name: "Claude Sonnet 5.5", Provider: "anthropic", ID: "claude-sonnet-5-5", Context: 200000,
				API:      &Price{In: 3, Out: 15, CacheRead: 0.3, CacheWrite: 3.75},
				Arena:    map[string]Rank{"text": rank(4, "High"), "coding": rank(2, ""), "math": rank(9, "")},
				External: External{ECI: 151.4, METRHorizonMin: 95},
			},
			"anthropic/claude-opus-5-5": {
				Name: "Claude Opus 5.5", Provider: "anthropic", ID: "claude-opus-5-5", Context: 1000000,
				API:   &Price{In: 5, Out: 25, CacheRead: 0.5},
				Arena: map[string]Rank{"text": rank(1, "High"), "coding": rank(2, "High"), "webdev": rank(3, "")},
			},
			"openai/untiered-pipe": {
				Name: "Untiered | Pipe", Provider: "openai", ID: "x", Context: 400000,
				API:   &Price{In: 1, Out: 4},
				Arena: map[string]Rank{"agent": rank(7, "")},
			},
			"openai/gpt-6-sol": {
				Name: "GPT-6 Sol", Provider: "openai", ID: "gpt-6-sol", Context: 1048576,
				API: &Price{In: 2.5, Out: 10},
			},
		},
	}
}

func TestContextCell(t *testing.T) {
	t.Parallel()
	for in, want := range map[int]string{
		0: "", 512: "512", 200000: "200k", 262144: "262k", 400000: "400k",
		1000000: "1M", 1048576: "1.05M", 1050000: "1.05M", 2000000: "2M",
	} {
		if got := contextCell(in); got != want {
			t.Errorf("contextCell(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPriceCellsNotAvailable(t *testing.T) {
	t.Parallel()
	in, out, cache := priceCells(Model{})
	if in != "n/a" || out != "n/a" || cache != "" {
		t.Errorf("priceCells = %q %q %q", in, out, cache)
	}
	md := string(Markdown(sample()))
	if !strings.Contains(md, "| Gemma 4 31B | google | google/gemma-4-31b-it | n/a | n/a |  | 262k |") {
		t.Errorf("n/a row missing:\n%s", md)
	}
}

var colorStyle = term.Style{B: "<b>", M: "<m>", R: "<r>", G: "<g>", Y: "<y>", C: "<c>", D: "<d>", O: "</>"}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(string(want), string(got)); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", name, diff)
	}
}

func terminal(st term.Style, c Catalog) string {
	var b bytes.Buffer
	WriteTerminal(&b, st, c)
	return b.String()
}

func TestRenderGolden(t *testing.T) {
	t.Parallel()
	c := sample()
	t.Run("markdown", func(t *testing.T) {
		t.Parallel()
		golden(t, "render_markdown.golden", Markdown(c))
	})
	t.Run("plain", func(t *testing.T) {
		t.Parallel()
		golden(t, "render_terminal_plain.golden", []byte(terminal(term.Style{}, c)))
	})
	t.Run("color", func(t *testing.T) {
		t.Parallel()
		golden(t, "render_terminal_color.golden", []byte(terminal(colorStyle, c)))
	})
}

func TestExternalColumnsHiddenWhenZero(t *testing.T) {
	t.Parallel()
	c := sample()
	m := c.Models["anthropic/claude-sonnet-5-5"]
	m.External = External{}
	c.Models["anthropic/claude-sonnet-5-5"] = m
	var rows []string
	for line := range strings.SplitSeq(string(Markdown(c)), "\n") {
		if strings.HasPrefix(line, "|") {
			rows = append(rows, line)
		}
	}
	md := strings.Join(rows, "\n")
	for _, h := range []string{"ECI", "TB", "SWE-V", "METR", "OSWorld", "Epoch"} {
		if strings.Contains(md, h) {
			t.Errorf("markdown shows %q with no external data", h)
		}
	}
	if out := terminal(term.Style{}, c); strings.Contains(out, "ECI") || strings.Contains(out, "Epoch") {
		t.Errorf("terminal shows external data:\n%s", out)
	}
}

func TestExternalFormats(t *testing.T) {
	t.Parallel()
	c := Catalog{
		Tiers: map[string][]string{"extra": {"a/x"}},
		Models: map[string]Model{"a/x": {Name: "X", External: External{
			ECI: 150.6, TerminalBench: 0.512, SWEBenchVerified: 80.4, METRHorizonMin: 120, OSWorld: 0.5,
		}}},
	}
	want := "| X |  | a/x | n/a | n/a |  |  |  |  |  |  |  |  |  |  | 151 | 51% | 80% | 120m | 50% |"
	if got := string(Markdown(c)); !strings.Contains(got, want) {
		t.Errorf("row %q not in:\n%s", want, got)
	}
}

func TestUntieredEmpty(t *testing.T) {
	t.Parallel()
	c := Catalog{Tiers: map[string][]string{}, Models: map[string]Model{}}
	out := terminal(term.Style{}, c)
	if !strings.Contains(out, "  ✓ every tracked model has a tier\n") {
		t.Errorf("missing check line:\n%s", out)
	}
	if md := string(Markdown(c)); !strings.Contains(md, "## Untiered\n\n_None._\n") {
		t.Errorf("markdown untiered not empty:\n%s", md)
	}
}

func TestMarkdownPipeEscapingAndEnding(t *testing.T) {
	t.Parallel()
	md := string(Markdown(sample()))
	if !strings.Contains(md, `| Untiered \| Pipe |`) {
		t.Errorf("pipe not escaped:\n%s", md)
	}
	if !strings.HasSuffix(md, ".\n") || strings.HasSuffix(md, "\n\n") {
		t.Errorf("want exactly one trailing newline, got %q", md[len(md)-10:])
	}
}

func TestBestWhenTieBreak(t *testing.T) {
	t.Parallel()
	c := Catalog{Models: map[string]Model{
		"b/two": {Name: "Two", Arena: map[string]Rank{"text": {Rank: 1}}},
		"a/one": {Name: "One", Arena: map[string]Rank{"text": {Rank: 1, Variant: "High"}}},
	}}
	got := bestWhen(c)
	want := []best{{"Text", "One", "High", 1}}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(best{})); diff != "" {
		t.Errorf("bestWhen mismatch (-want +got):\n%s", diff)
	}
}
