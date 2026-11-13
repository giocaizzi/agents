package harness

// adjustClaude edits a Claude Code golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustClaude(c parityCase, file string, want any) any {
	root, _ := want.(map[string]any)
	if file != "settings.json" || root == nil {
		return want
	}
	// The LSP plugin now lives in agents' own data directory, under the marketplace "agents".
	if plugins, ok := root["enabledPlugins"].(map[string]any); ok {
		if v, ok := plugins["lsp@dotfiles"]; ok {
			delete(plugins, "lsp@dotfiles")
			plugins["lsp@agents"] = v
		}
	}
	if markets, ok := root["extraKnownMarketplaces"].(map[string]any); ok {
		if _, ok := markets["dotfiles"]; ok {
			delete(markets, "dotfiles")
			markets["agents"] = map[string]any{"source": map[string]any{
				"source": "directory", "path": goldenHome + "/.local/share/agents/claude-plugins",
			}}
		}
	}
	// agents now also renders context.auto_compact, context.max_tokens and permissions.allow_commands
	// (all set in the parity config) for Claude, which the dotfiles manager left out.
	root["autoCompactEnabled"] = true
	env, _ := root["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
		root["env"] = env
	}
	env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = "200000"
	if perms, ok := root["permissions"].(map[string]any); ok {
		allow, _ := perms["allow"].([]any)
		// permissions.allow merges as a union: the live file's own rule stays, first.
		if c.Start == "live" {
			allow = append([]any{"Bash(ls)"}, allow...)
		}
		for _, cmd := range []string{"git", "gh", "make", "uv", "jq", "npm", "cargo", "go", "terraform"} {
			allow = append(allow, "Bash("+cmd+" *)")
		}
		// permissions.allowed_urls entries that cover a whole host become WebFetch(domain:) rules.
		for _, host := range []string{"github.com", "api.github.com", "docs.github.com", "learn.microsoft.com", "*.example.org", "*.claude.ai", "*.claude.com", "docs.anthropic.com", "*.openai.com", "*.google.com"} {
			allow = append(allow, "WebFetch(domain:"+host+")")
		}
		perms["allow"] = allow
	}
	// agents removes only the env keys it wrote (known from its manifest), so with telemetry
	// off the live OTEL_OLD and CLAUDE_CODE_ENABLE_TELEMETRY stay on the first apply.
	if c.Profile == "work" && c.Start == "live" {
		if env, ok := root["env"].(map[string]any); ok {
			env["OTEL_OLD"] = "x"
			env["CLAUDE_CODE_ENABLE_TELEMETRY"] = "1"
		}
	}
	return want
}
