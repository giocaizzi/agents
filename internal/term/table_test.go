package term_test

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/term"
)

func TestWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "plain", in: "abc", want: 3},
		{name: "colored", in: "\x1b[1;34mabc\x1b[0m", want: 3},
		{name: "multibyte", in: "$1 · #3", want: 7},
		{name: "empty", in: "", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := term.Width(tt.in); got != tt.want {
				t.Errorf("Width(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestPad(t *testing.T) {
	t.Parallel()
	if got, want := term.Pad("\x1b[35mab\x1b[0m", 4), "\x1b[35mab\x1b[0m  "; got != want {
		t.Errorf("Pad = %q, want %q", got, want)
	}
	if got, want := term.Pad("abcdef", 3), "abcdef"; got != want {
		t.Errorf("Pad = %q, want %q", got, want)
	}
}

func TestTable(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	s := term.Style{D: "<d>", O: "</>"}
	s.Table(&buf, []string{"MODEL", "TEXT", "CODE"}, [][]string{
		{"GPT-6 Luna", "#12", ""},
		{"Claude Haiku 5.5", "", "#3"},
	})
	want := "  <d>MODEL             TEXT  CODE</>\n" +
		"  GPT-6 Luna        #12\n" +
		"  Claude Haiku 5.5        #3\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Errorf("Table() mismatch (-want +got):\n%s", diff)
	}
}

func TestWarn(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	term.Style{Y: "<y>", O: "</>"}.Warn(&buf, "catalog is 40 days old")
	if got, want := buf.String(), "<y>!</> catalog is 40 days old\n"; got != want {
		t.Errorf("Warn = %q, want %q", got, want)
	}
}
