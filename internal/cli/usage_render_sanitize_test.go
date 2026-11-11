package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/term"
	usg "github.com/giocaizzi/agents/internal/usage"
)

func TestSanitize(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"plain text":                           "plain text",
		"\x1b[31mX\x1b[0m":                     "[31mX[0m",
		"a\u009bb\u0085c\x7fd":                 "abcd",
		"tab\there\nnew\rline\x00z":            "tabherenewlinez",
		"héllo · ünï":                          "héllo · ünï",
		"a\u202ab\u202bc\u202cd\u202de\u202ef": "abcdef",
		"a\u2066b\u2067c\u2068d\u2069e":        "abcde",
		"a\u200eb\u200fc":                      "abc",
		"a\u200bb\u2060c\ufeffd\u061ce":        "abcde",
		"Ali\u200cReza":                        "Ali\u200cReza",
		"\U0001F469\u200d\U0001F4BB":           "\U0001F469\u200d\U0001F4BB",
	}
	for in, want := range tests {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderStripsControlCharacters(t *testing.T) {
	t.Parallel()
	snap := usg.Snapshot{
		Provider: "any\x1b]0;x\aprov", Account: "me\u009b@example.com", Harnesses: []string{"h\x1b[2J"},
		Plan:   usg.Plan{Name: "Pl\x1bus", Raw: "raw\x1b"},
		Status: usg.StatusStale, Note: "note\x1b[1m", Source: "src\x1b",
		FetchedAt: renderNow,
		Limits: []usg.Limit{
			{Name: "\x1b[31mX\x1b[0m and \u009b", Kind: usg.LimitWindow},
			{Name: "amount", Kind: usg.LimitCredits, Remaining: &usg.Amount{Value: 1, Unit: "US\x1bD"}},
		},
	}
	var out bytes.Buffer

	renderUsage(&out, term.Style{}, []usg.Snapshot{snap}, renderNow)

	for _, r := range out.String() {
		if r == '\x1b' || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("output holds control character %q:\n%q", r, out.String())
		}
	}
	if !strings.Contains(out.String(), "[31mX[0m and ") {
		t.Errorf("limit name lost:\n%s", out.String())
	}
	if snap.Limits[0].Name != "\x1b[31mX\x1b[0m and \u009b" {
		t.Error("render mutated its input")
	}
}
