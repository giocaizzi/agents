package apply

import (
	"cmp"
	"errors"
	"slices"
	"strings"
)

// edit replaces src[start:end] with text; a zero-width edit inserts.
type edit struct {
	start, end int
	text       string
}

// applyEdits applies non-overlapping edits to src. Edits at the same offset
// keep the order they were given in, zero-width ones first.
func applyEdits(src string, edits []edit) (string, error) {
	slices.SortStableFunc(edits, func(a, b edit) int {
		if c := cmp.Compare(a.start, b.start); c != 0 {
			return c
		}
		return cmp.Compare(a.end, b.end)
	})
	var sb strings.Builder
	sb.Grow(len(src))
	pos := 0
	for _, e := range edits {
		if e.start < pos || e.end < e.start || e.end > len(src) {
			return "", errors.New("overlapping edits")
		}
		sb.WriteString(src[pos:e.start])
		sb.WriteString(e.text)
		pos = e.end
	}
	sb.WriteString(src[pos:])
	return sb.String(), nil
}

// unmanagedMark replaces the value of a key agents does not manage in a diff.
const unmanagedMark = "«unmanaged»"

// redactedMark replaces a secret value in a diff.
const redactedMark = "«redacted»"

// lineStart returns the offset of the first byte of the line holding pos.
func lineStart(src string, pos int) int {
	return strings.LastIndexByte(src[:pos], '\n') + 1
}

// lineIndent returns the leading blanks of the line holding pos.
func lineIndent(src string, pos int) string {
	ls := lineStart(src, pos)
	end := ls
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return src[ls:end]
}

// hasPrefixPath reports whether prefix is the leading part of path.
func hasPrefixPath(path, prefix []string) bool {
	return len(prefix) <= len(path) && slices.Equal(path[:len(prefix)], prefix)
}
