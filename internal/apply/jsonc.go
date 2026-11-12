package apply

// stripJSONC removes // and /* */ comments and trailing commas that sit
// outside strings. Comments become a space (newlines are kept) so positions in
// error messages stay close to the original.
func stripJSONC(in []byte) []byte {
	return dropTrailingCommas(dropComments(in))
}

// copyString appends the string literal that starts at in[i] (an opening
// quote) to out and returns the index after it.
func copyString(out, in []byte, i int) ([]byte, int) {
	out = append(out, in[i])
	for i++; i < len(in); i++ {
		out = append(out, in[i])
		switch in[i] {
		case '\\':
			if i+1 < len(in) {
				i++
				out = append(out, in[i])
			}
		case '"':
			return out, i + 1
		}
	}
	return out, i
}

// skipLineComment returns the index of the newline ending the comment at in[i].
func skipLineComment(in []byte, i int) int {
	for i < len(in) && in[i] != '\n' {
		i++
	}
	return i
}

// skipBlockComment returns the index after the comment at in[i] and the newlines it spans.
func skipBlockComment(in []byte, i int) (int, int) {
	newlines := 0
	for i += 2; i < len(in); i++ {
		if in[i] == '*' && i+1 < len(in) && in[i+1] == '/' {
			return i + 2, newlines
		}
		if in[i] == '\n' {
			newlines++
		}
	}
	return i, newlines
}

func dropComments(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); {
		c := in[i]
		switch {
		case c == '"':
			out, i = copyString(out, in, i)
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			i = skipLineComment(in, i)
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			var newlines int
			i, newlines = skipBlockComment(in, i)
			for range newlines {
				out = append(out, '\n')
			}
			out = append(out, ' ')
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func dropTrailingCommas(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); {
		c := in[i]
		switch {
		case c == '"':
			out, i = copyString(out, in, i)
		case c == ',' && closesNext(in, i+1):
			i++
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

// closesNext reports whether the next non-space byte at or after i closes an object or array.
func closesNext(in []byte, i int) bool {
	for i < len(in) && isSpace(in[i]) {
		i++
	}
	return i < len(in) && (in[i] == '}' || in[i] == ']')
}
