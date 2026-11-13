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

// allowOverlapsDeny reports whether the allow rule overlaps an unexpressible
// deny, and warns once for it, naming both.
func (b *builder) allowOverlapsDeny(harness, where, pat string, words, deny []string) bool {
	d, hit := overlappingDeny(words, deny)
	if hit {
		b.warnf("%s: skipped, allow %q could cover commands that deny pattern %q forbids and %s cannot express it", where, pat, d, harness)
	}
	return hit
}

// unexpressibleDenies are the deny patterns a prefix-rule harness (Codex rules,
// Antigravity command()) cannot match: a wildcard before the last word.
func unexpressibleDenies(p *config.Permissions) []string {
	var out []string
	for _, list := range [][]string{p.DenyCommandPatterns, p.DenyCommandExact, p.DenyCommandPrefixes} {
		for _, pat := range list {
			if _, ok := prefixWords(pat); !ok {
				out = append(out, pat)
			}
		}
	}
	return out
}

// overlappingDeny returns the first of the deny patterns that the allow
// prefix could overlap: some command that both match. Words compare position
// by position over the shorter of the two; a deny word with a * is a
// wildcard, any other pair must be equal. An allow that is a prefix of the
// deny's leading words (git against "git push * HEAD:main") overlaps it, and
// so does a more specific one that agrees with every deny word it reaches;
// one that contradicts a literal deny word does not
// ("git push --force-with-lease origin" against "git push * HEAD:main").
func overlappingDeny(allow, deny []string) (string, bool) {
	for _, d := range deny {
		dw := strings.Fields(d)
		overlap := true
		for i := range min(len(allow), len(dw)) {
			if allow[i] != dw[i] && !strings.Contains(dw[i], "*") {
				overlap = false
				break
			}
		}
		if overlap {
			return d, true
		}
	}
	return "", false
}
