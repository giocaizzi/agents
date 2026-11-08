package term

import "strings"

// Bar draws a meter of width cells for percent (0-100, clamped): filled cells
// green below 70, yellow from 70, red from 90; empty cells dim.
func (s Style) Bar(percent float64, width int) string {
	p := min(max(percent, 0), 100)
	full := int(p/100*float64(width) + 0.5)
	c := s.G
	switch {
	case p >= 90:
		c = s.R
	case p >= 70:
		c = s.Y
	}
	return c + strings.Repeat("█", full) + s.O + s.D + strings.Repeat("░", width-full) + s.O
}
