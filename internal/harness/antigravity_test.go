package harness

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

func TestRenderAntigravityMCPDropsCallbackPort(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.MCP.Servers = map[string]config.MCPServer{
		"crm":   {URL: "https://crm.example.com/mcp", OAuth: config.OAuth{ClientID: "id", CallbackPort: 8765}},
		"local": {Command: "x", Args: []string{"-y"}, Env: map[string]string{"K": "v"}},
	}
	b := newTestBuilder(c)
	if err := renderAntigravity(c, b); err != nil {
		t.Fatal(err)
	}
	got := targetAt(t, b, "/home/u/.gemini/config/mcp_config.json").Managed
	want := map[string]any{"mcpServers": map[string]any{
		"crm":   map[string]any{"url": "https://crm.example.com/mcp", "oauth": map[string]any{"clientId": "id"}},
		"local": map[string]any{"command": "x", "args": []any{"-y"}, "env": map[string]any{"K": "v"}},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mcp_config.json (-want +got):\n%s", diff)
	}
}

func TestAntigravityPermissions(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.Permissions = config.Permissions{
		AllowCommandPatterns: []string{"git push origin *", "a * b"},
		DenyCommandPatterns:  []string{"gh pr merge *", "git push * main", "git push origin main", "git push origin main *"},
		DenyCommandExact:     []string{"env"},
		DenyCommandPrefixes:  []string{"pip", "python -m pip"},
		AskCommandPrefixes:   []string{"bw"},
		DenyRead:             []string{"**/.env"},
		DenyWrite:            []string{"**/.ssh/**"},
	}
	c.Harness.Antigravity.Allow = []string{"mcp(*)"}
	got := antigravityPermissions(c, &builder{})
	want := map[string]any{
		"allow": []any{"mcp(*)"}, // command(git push origin) overlaps the inexpressible deny "git push * main"
		"ask":   []any{"command(bw)", "command(bw *)"},
		"deny": []any{
			"command(gh pr merge *)", "command(git push origin main)", "command(env)",
			"command(pip)", "command(pip *)", "command(python -m pip *)",
			"read_file(**/.env)", "write_file(**/.ssh/**)",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("permissions (-want +got):\n%s", diff)
	}
}

func TestAntigravitySharedAllowRules(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.Permissions = config.Permissions{
		AllowCommands: []string{"git status", "make *", "a * b"},
		AllowedURLs: []string{
			"https://github.com/**", "https://*.example.org/*", "docs.example.com", "https://github.com/org/**",
			"https://a.*.com/**", "http://localhost:3000/",
		},
		AllowedMCP: []string{"docs", "crm/search", "*"},
	}
	got := antigravityPermissions(c, &builder{})
	want := map[string]any{"allow": []any{
		"command(git status)", "command(make)",
		"read_url(github.com)", "read_url(example.org)", "read_url(docs.example.com)", "read_url(localhost:3000)",
		"mcp(docs/*)", "mcp(crm/search)", "mcp(*)",
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("permissions (-want +got):\n%s", diff)
	}
}

func TestRenderAntigravityUI(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.UI.Theme = "tokyo night"
	c.Notifications.TerminalBell = true
	b := newTestBuilder(c)
	if err := renderAntigravity(c, b); err != nil {
		t.Fatal(err)
	}
	managed, _ := targetAt(t, b, "/home/u/.gemini/antigravity-cli/settings.json").Managed.(map[string]any)
	if managed["colorScheme"] != "tokyo night" || managed["notifications"] != true {
		t.Errorf("colorScheme/notifications = %v/%v", managed["colorScheme"], managed["notifications"])
	}
}

// TestAntigravityAllowSkipsOverlappingDeny pins the shared rule (overlap.go): an
// allow prefix is not emitted while a deny command() cannot express covers it,
// with one warning per skipped allow; non-overlapping allows stay.
func TestAntigravityAllowSkipsOverlappingDeny(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.Permissions = config.Permissions{
		AllowCommands:        []string{"git *", "gh *", "make *", "gh pr view"},
		AllowCommandPatterns: []string{"git push --force-with-lease origin *", "git push *"},
		DenyCommandPatterns:  []string{"git push * HEAD:main", "gh pr merge * --admin*"},
	}
	b := &builder{}
	got := antigravityPermissions(c, b)
	want := []any{"command(make)", "command(gh pr view)"}
	if diff := cmp.Diff(want, got["allow"]); diff != "" {
		t.Errorf("allow (-want +got):\n%s", diff)
	}
	for _, skipped := range []string{`allow "git *"`, `allow "gh *"`, `allow "git push *"`, `allow "git push --force-with-lease origin *"`} {
		n := 0
		for _, w := range b.warnings {
			if strings.Contains(w, skipped) && strings.Contains(w, "Antigravity cannot express it") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("warnings for %s = %d, want 1: %q", skipped, n, b.warnings)
		}
	}
}

// TestAntigravityAllowSkipsOverlappingAsk pins the rule for an ask prefix with
// a wildcard before its last word, and that the warning names the field the
// skipped allow came from.
func TestAntigravityAllowSkipsOverlappingAsk(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.Permissions = config.Permissions{
		AllowCommands:        []string{"git", "make"},
		AllowCommandPatterns: []string{"git push *"},
		AskCommandPrefixes:   []string{"git push * prod"},
	}
	b := &builder{}
	got := antigravityPermissions(c, b)
	if diff := cmp.Diff([]any{"command(make)"}, got["allow"]); diff != "" {
		t.Errorf("allow (-want +got):\n%s", diff)
	}
	for _, want := range []string{
		`permissions.allow_commands: skipped, allow "git" could cover commands that ask pattern "git push * prod"`,
		`permissions.allow_command_patterns: skipped, allow "git push *" could cover commands that ask pattern "git push * prod"`,
	} {
		if !slices.ContainsFunc(b.warnings, func(w string) bool { return strings.Contains(w, want) }) {
			t.Errorf("missing warning %q in %q", want, b.warnings)
		}
	}
}

// TestAntigravityAllowWithoutWords: an allow that has no prefix words ("" or
// " *", which means every command) cannot be checked against the
// unexpressible patterns, so it must not be written as command(), a rule
// whose empty prefix covers every command, "git push x HEAD:main" included.
func TestAntigravityAllowWithoutWords(t *testing.T) {
	t.Parallel()
	for _, allow := range []string{"", " *"} {
		t.Run("allow "+strconv.Quote(allow), func(t *testing.T) {
			t.Parallel()
			c := &config.Config{}
			c.Permissions = config.Permissions{
				AllowCommandPatterns: []string{allow},
				DenyCommandPatterns:  []string{"git push * HEAD:main"},
			}
			if got := antigravityPermissions(c, &builder{})["allow"]; got != nil {
				t.Errorf("allow = %v, want none", got)
			}
		})
	}
}
