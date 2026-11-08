// Package term provides terminal styling and Catppuccin ANSI colors.
package term

import (
	"io"
	"os"
)

// Style holds the colour escapes for one output stream: blue headings (B), mauve
// labels (M), red (R), green (G), yellow (Y), cyan (C), dim (D) and reset (O).
type Style struct{ B, M, R, G, Y, C, D, O string }

// NewStyle decides the colours for w.
func NewStyle(w io.Writer, getenv func(string) string) Style {
	if getenv == nil {
		getenv = os.Getenv
	}
	f, ok := w.(*os.File)
	if !ok || getenv("NO_COLOR") != "" || getenv("TERM") == "dumb" {
		return Style{}
	}
	if fi, err := f.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return Style{}
	}
	const esc = "\x1b["
	return Style{
		B: esc + "1;34m",
		M: esc + "35m",
		R: esc + "31m",
		G: esc + "32m",
		Y: esc + "33m",
		C: esc + "36m",
		D: esc + "2m",
		O: esc + "0m",
	}
}
