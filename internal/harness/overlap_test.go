package harness

import (
	"slices"
	"strings"
	"testing"
)

// TestOverlapping pins when an allow prefix overlaps a pattern a prefix-rule
// harness cannot express: some command matches both. A * is a glob as in
// Claude's Bash(...) (it spans spaces), so it can absorb any number of the
// allow's words. A row that overlaps names a command matching both.
func TestOverlapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, allow, pattern string
		want                 bool
		command              string // matched by both, when want
	}{
		{"allow is a prefix of the pattern's leading words", "git", "git push * HEAD:main", true, "git push x HEAD:main"},
		{"allow reaches the wildcard", "git push", "git push * HEAD:main", true, "git push x HEAD:main"},
		{"allow contradicts a literal word before the wildcard", "git pull", "git push * HEAD:main", false, ""},
		{"unrelated command", "make", "git push * HEAD:main", false, ""},
		{"word-internal wildcard", "gh pr merge 12", "gh pr merge * --admin*", true, "gh pr merge 12 --admin"},
		{"* spans two allow words", "git push --force-with-lease origin", "git push * HEAD:main", true, "git push --force-with-lease origin HEAD:main"},
		{"* spans three allow words", "rm foo bar baz", "rm * -rf", true, "rm foo bar baz -rf"},
		{"leading *", "git push", "* --force", true, "git push --force"},
		{"leading * overlaps an unrelated allow", "make", "* --force", true, "make --force"},
		{"word-internal * spans words", "git status", "gi* push", true, "git status push"},
		{"several wildcards", "git status", "git * push * --force", true, "git status push x --force"},
		{"several wildcards, contradicted before the first", "make", "git * push * --force", false, ""},
		{"* alone", "make", "*", true, "make"},
		{"empty pattern blocks every allow", "make", "", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			words, _ := prefixWords(tt.allow)
			if _, got := overlapping(words, []unexpressible{{tt.pattern, "deny pattern"}}); got != tt.want {
				t.Errorf("overlapping(%q, %q) = %v, want %v (both match %q)", tt.allow, tt.pattern, got, tt.want, tt.command)
			}
		})
	}
}

// TestOverlappingSound checks, over a small vocabulary, that overlapping never
// misses: whenever some command starting with the allow's words matches the
// pattern, the allow is reported. Reporting more (a partial glob such as
// "gi*" blocks every git allow) is the safe side and is not checked.
func TestOverlappingSound(t *testing.T) {
	t.Parallel()
	vocab := []string{"git", "push", "main"}
	tails := wordSeqs(vocab, 3)
	for _, pw := range wordSeqs(append([]string{"*", "gi*", "*n"}, vocab...), 3)[1:] {
		pat := strings.Join(pw, " ")
		if _, ok := prefixWords(pat); ok {
			continue // expressible: never on this path
		}
		for _, allow := range wordSeqs(vocab, 3)[1:] {
			if _, hit := overlapping(allow, []unexpressible{{pat, "deny pattern"}}); hit {
				continue
			}
			if cmd, ok := witness(allow, pat, tails); ok {
				t.Errorf("overlapping(%q, %q) = false, but %q matches both", allow, pat, cmd)
			}
		}
	}
}

// wordSeqs is every sequence of up to maxLen words from alphabet, the empty
// one first.
func wordSeqs(alphabet []string, maxLen int) [][]string {
	out := make([][]string, 0, 64)
	out = append(out, nil) // the empty sequence
	for i := 0; i < len(out); i++ {
		if len(out[i]) == maxLen {
			continue
		}
		for _, w := range alphabet {
			out = append(out, append(slices.Clone(out[i]), w))
		}
	}
	return out
}

// witness returns a command that starts with the allow's words, continues
// with one of tails and matches pattern.
func witness(allow []string, pattern string, tails [][]string) (string, bool) {
	for _, tail := range tails {
		if cmd := strings.Join(slices.Concat(allow, tail), " "); globMatch(pattern, cmd) {
			return cmd, true
		}
	}
	return "", false
}

// globMatch reports whether s matches pattern, where * matches any run of
// characters, spaces included.
func globMatch(pattern, s string) bool {
	before, after, found := strings.Cut(pattern, "*")
	if !found {
		return pattern == s
	}
	if !strings.HasPrefix(s, before) {
		return false
	}
	rest := s[len(before):]
	for i := range len(rest) + 1 {
		if globMatch(after, rest[i:]) {
			return true
		}
	}
	return false
}
