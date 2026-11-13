package harness

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/config"
)

// TestOverlappingDeny pins when an allow prefix overlaps a deny pattern a
// prefix-rule harness cannot express: some command matches both. A deny * is
// a glob as in Claude's Bash(...) (it spans spaces), so it can absorb any
// number of the allow's words.
func TestOverlappingDeny(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		allow string
		deny  string
		want  bool
	}{
		{"allow is a prefix of the deny's leading words", "git", "git push * HEAD:main", true},
		{"allow reaches the wildcard", "git push", "git push * HEAD:main", true},
		{"allow contradicts a literal word before the wildcard", "git pull", "git push * HEAD:main", false},
		{"unrelated command", "make", "git push * HEAD:main", false},
		{"word-internal wildcard", "gh pr merge 12", "gh pr merge * --admin*", true},
		{"leading wildcard overlaps everything", "make", "* --force", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			words, _ := prefixWords(tt.allow)
			if _, got := overlapping(words, []unexpressible{{tt.deny, "deny pattern"}}); got != tt.want {
				t.Errorf("overlapping(%q, %q) = %v, want %v", tt.allow, tt.deny, got, tt.want)
			}
		})
	}

	// A deny * spans several words, so an allow that reaches past it overlaps
	// whatever literal words follow the *: the allow prefix leaves the rest of
	// the command free, where the deny's tail can sit.
	spans := []struct {
		name, allow, deny, command string
	}{
		{"* spans two allow words", "git push --force-with-lease origin", "git push * HEAD:main", "git push --force-with-lease origin HEAD:main"},
		{"* spans three allow words", "rm foo bar baz", "rm * -rf", "rm foo bar baz -rf"},
		{"leading *", "git push", "* --force", "git push --force"},
		{"word-internal * spans words", "git status", "gi* push", "git status push"},
	}
	for _, tt := range spans {
		t.Run("spans: "+tt.name, func(t *testing.T) {
			t.Parallel()
			words, _ := prefixWords(tt.allow)
			if _, got := overlapping(words, []unexpressible{{tt.deny, "deny pattern"}}); !got {
				t.Errorf("overlapping(%q, %q) = false, want true: %q matches both", tt.allow, tt.deny, tt.command)
			}
		})
	}
}

// TestCodexAllowEscalatesPastMultiWordWildcard reproduces the escalation at
// the rendered file: Codex auto-allows "git push --force-with-lease origin
// HEAD:main", which deny "git push * HEAD:main" forbids on Claude.
func TestCodexAllowEscalatesPastMultiWordWildcard(t *testing.T) {
	t.Parallel()
	t.Run("allow prefix past a deny * is not emitted", func(t *testing.T) {
		t.Parallel()
		cfg := config.Config{Permissions: config.Permissions{
			AllowCommandPatterns: []string{"git push --force-with-lease origin *"},
			DenyCommandPatterns:  []string{"git push * HEAD:main"},
		}}
		home := t.TempDir()
		plan := renderOne(t, cfg, config.Codex, home)
		got, _ := planTargetAt(t, plan, filepath.Join(home, ".codex", "rules", "agents.rules")).Managed.(string)
		if strings.Contains(got, `decision = "allow"`) {
			t.Errorf("allow rule emitted past the unexpressible deny:\n%s", got)
		}
	})
}
