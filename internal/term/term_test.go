package term_test

import (
	"bytes"
	"testing"

	"github.com/giocaizzi/agents/internal/term"
)

func TestNewStyle_NonTerminal(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	s := term.NewStyle(&buf, func(_ string) string { return "" })
	if s.B != "" || s.M != "" || s.O != "" {
		t.Errorf("expected empty style for non-terminal writer, got %+v", s)
	}
}
