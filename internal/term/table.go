package term

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Width is the number of runes s shows on screen, ignoring ANSI escapes.
func Width(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			j := i + 1
			for j < len(s) && (s[j] < '@' || s[j] > '~' || s[j] == '[') {
				j++
			}
			i = j + 1
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		w++
	}
	return w
}

// Pad right-pads s with spaces to width visible columns.
func Pad(s string, width int) string {
	if n := width - Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// Table writes an aligned table indented by two spaces: a dim header, then the
// rows, columns two spaces apart. Cells may carry their own colour escapes.
func (s Style) Table(w io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = Width(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths) {
				widths[i] = max(widths[i], Width(c))
			}
		}
	}
	line := func(cells []string) string {
		var b strings.Builder
		for i, c := range cells {
			if i >= len(widths) {
				break
			}
			if i == len(cells)-1 || i == len(widths)-1 {
				b.WriteString(c)
				break
			}
			b.WriteString(Pad(c, widths[i]+2))
		}
		return strings.TrimRight(b.String(), " ")
	}
	fmt.Fprintf(w, "  %s%s%s\n", s.D, line(header), s.O)
	for _, r := range rows {
		fmt.Fprintf(w, "  %s\n", line(r))
	}
}

// Warn writes one warning line: a yellow "!" then msg. It is the one warning
// surface of chezmoi-agents.
func (s Style) Warn(w io.Writer, msg string) {
	fmt.Fprintf(w, "%s!%s %s\n", s.Y, s.O, msg)
}
