package harness

import (
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
)

// The path shared by every harness whose command rules match word prefixes
// only (Codex rules, Antigravity command()): a deny pattern that cannot be
// written as a prefix is skipped with a warning, which would leave the command
// allowed by any allow prefix covering it. So an allow rule is not emitted
// while such a deny overlaps it, and is warned once. Harnesses whose rules take
// wildcards anywhere (Claude, VS Code, opencode) never reach this path.

// prefixWords splits a command pattern into words, dropping a trailing
// wildcard word. A wildcard anywhere else cannot be a prefix.
func prefixWords(pat string) ([]string, bool) {
	words := strings.Fields(pat)
	if n := len(words); n > 0 && words[n-1] == "*" {
		words = words[:n-1]
	}
	if len(words) == 0 || slices.ContainsFunc(words, func(w string) bool { return strings.Contains(w, "*") }) {
		return nil, false
	}
	return words, true
}

// unexpressible is a deny or ask pattern a prefix-rule harness cannot write.
type unexpressible struct {
	pattern string
	kind    string // "deny pattern" or "ask pattern", for the warning
}

// allowOverlaps reports whether the allow rule overlaps an unexpressible
// pattern, and warns once for it, naming both.
func (b *builder) allowOverlaps(harness, where, pat string, words []string, un []unexpressible) bool {
	u, hit := overlapping(words, un)
	if hit {
		b.warnf("%s: skipped, allow %q could cover commands that %s %q applies to and %s cannot express it", where, pat, u.kind, u.pattern, harness)
	}
	return hit
}

// unexpressibleRules are the deny and ask patterns a prefix-rule harness
// (Codex rules, Antigravity command()) cannot match: a wildcard before the
// last word. Skipped, an ask would be auto-approved by an allow prefix
// covering it, as a deny would be.
func unexpressibleRules(p *config.Permissions) []unexpressible {
	var out []unexpressible
	add := func(kind string, lists ...[]string) {
		for _, list := range lists {
			for _, pat := range list {
				if _, ok := prefixWords(pat); !ok {
					out = append(out, unexpressible{pat, kind})
				}
			}
		}
	}
	add("deny pattern", p.DenyCommandPatterns, p.DenyCommandExact, p.DenyCommandPrefixes)
	add("ask pattern", p.AskCommandPrefixes)
	return out
}

// overlapping returns the first of the patterns that the allow prefix could
// overlap: some command that both match. A * is a glob that spans spaces, so
// it can swallow any number of the allow's words. Let L be the pattern's
// literal words before its first word containing a * (a partial glob such as
// "gi*" counts as a wildcard from there). The allow overlaps when it agrees
// with L over their common length: it is a prefix of L, or L is a prefix of
// it, because once the allow reaches the wildcard it may continue to match
// ("git push --force-with-lease origin" against "git push * HEAD:main").
func overlapping(allow []string, un []unexpressible) (unexpressible, bool) {
	for _, u := range un {
		var lit []string
		for w := range strings.FieldsSeq(u.pattern) {
			if strings.Contains(w, "*") {
				break
			}
			lit = append(lit, w)
		}
		n := min(len(allow), len(lit))
		if slices.Equal(allow[:n], lit[:n]) {
			return u, true
		}
	}
	return unexpressible{}, false
}
