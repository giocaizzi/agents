package catalog

import (
	"regexp"
	"strings"
)

var (
	trailingGroup  = regexp.MustCompile(`\s*\([^()]*\)$`)
	trailingSuffix = regexp.MustCompile(`_[a-z0-9]*$`)
	trailingDate   = regexp.MustCompile(`-(\d{4}-\d{2}-\d{2}|\d{8})$`)
	separatorRun   = regexp.MustCompile(`[ ._]+`)
)

// Key normalizes a model name or id from any source so they compare equal:
// lowercase, drop a trailing "(...)", "_suffix" and date, then collapse runs
// of spaces, dots and underscores into "-". The steps repeat until nothing
// changes, which makes Key idempotent.
func Key(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for {
		next := keyStep(s)
		if next == s {
			return s
		}
		s = next
	}
}

func keyStep(s string) string {
	s = trailingGroup.ReplaceAllString(s, "")
	s = trailingSuffix.ReplaceAllString(s, "")
	s = trailingDate.ReplaceAllString(s, "")
	s = separatorRun.ReplaceAllString(s, "-")
	return strings.TrimSpace(s)
}
