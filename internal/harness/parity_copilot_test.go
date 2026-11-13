package harness

// adjustCopilot edits a Copilot CLI golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustCopilot(c parityCase, file string, want any) any {
	root, _ := want.(map[string]any)
	if root == nil {
		return want
	}
	if file == "settings.json" {
		// agents now renders the default tier's effort and the subagent depth, which the old
		// dotfile templates never wrote.
		root["effortLevel"] = "medium"
		sub, _ := root["subagents"].(map[string]any)
		if sub == nil {
			sub = map[string]any{}
			root["subagents"] = sub
		}
		sub["maxDepth"] = float64(2)
	}
	if c.Start != "live" {
		return want
	}
	switch file {
	case "settings.json":
		// agents never wrote statusLine, so the live one stays (the old templates deleted it).
		root["statusLine"] = map[string]any{"type": "command", "command": "old"}
	case "lsp-config.json":
		// On the first apply there is no manifest, so a live entry for a server agents does
		// not render (gopls is not installed on the work profile) stays as it is.
		if servers, ok := root["lspServers"].(map[string]any); ok && c.Profile == "work" {
			if g, ok := servers["gopls"].(map[string]any); ok {
				g["command"] = "old-gopls"
			}
		}
	}
	return want
}
