package harness

// adjustAntigravity edits an Antigravity golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustAntigravity(_ parityCase, file string, want any) any {
	if file != "settings.json" {
		return want
	}
	m, _ := want.(map[string]any)
	// The templates had no terminal-bell setting; agents renders notifications.terminal_bell.
	m["notifications"] = true
	perms, _ := m["permissions"].(map[string]any)
	allow, _ := perms["allow"].([]any)
	// The templates allowed only the typed rules and one pattern. agents also
	// renders permissions.allow_commands (before the patterns) and allowed_urls
	// (host-wide entries only, as read_url).
	cmds := []any{
		"command(git)", "command(gh)", "command(make)", "command(uv)", "command(jq)",
		"command(npm)", "command(cargo)", "command(go)", "command(terraform)",
	}
	urls := []any{
		"read_url(github.com)", "read_url(api.github.com)", "read_url(docs.github.com)",
		"read_url(learn.microsoft.com)", "read_url(example.org)", "read_url(claude.ai)",
		"read_url(claude.com)", "read_url(docs.anthropic.com)", "read_url(openai.com)",
		"read_url(google.com)",
	}
	const typed = 5 // the harness.antigravity.allow rules come first
	out := append([]any{}, allow[:typed]...)
	out = append(out, cmds...)
	out = append(out, allow[typed:]...)
	perms["allow"] = append(out, urls...)
	return want
}
