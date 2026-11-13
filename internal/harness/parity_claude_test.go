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
