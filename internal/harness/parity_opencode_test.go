package harness

import "slices"

// adjustOpenCode edits a opencode golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustOpenCode(c parityCase, file string, want any) any {
	m, _ := want.(map[string]any)
	if file != "opencode.jsonc" || m == nil {
		return want
	}
	if mcp, ok := m["mcp"].(map[string]any); ok {
		moveOpenCodeServers(mcp)
	}
	// The templates did not render context.auto_compact, autonomy.allow_non_workspace_access,
	// permissions.allow_commands or permissions.allowed_urls for opencode.
	m["compaction"] = map[string]any{"auto": true}
	perms, _ := m["permissions"].([]any)
	rule := func(action, resource string) any {
		return map[string]any{"action": action, "effect": "allow", "resource": resource}
	}
	before := []any{rule("external_directory", "*")}
	for _, cmd := range []string{"git *", "gh *", "make *", "uv *", "jq *", "npm *", "cargo *", "go *", "terraform *"} {
		before = append(before, rule("shell", cmd))
	}
	var after []any
	for _, u := range []string{
		"https://github.com/**", "https://api.github.com/**", "https://docs.github.com/**",
		"https://learn.microsoft.com/**", "https://*.example.org/**", "https://*.claude.ai/**",
		"https://*.claude.com/**", "https://docs.anthropic.com/**", "https://*.openai.com/**",
		"https://*.google.com/**",
	} {
		after = append(after, rule("webfetch", u))
	}
	// Managed allow rules go before the pattern rule and the URL rules after it, ahead of the
	// ask rules.
	at := slices.IndexFunc(perms, func(p any) bool {
		r, _ := p.(map[string]any)
		return r["resource"] == "git push --force-with-lease origin *"
	})
	if at >= 0 {
		perms = slices.Concat(perms[:at], before, perms[at:at+1], after, perms[at+1:])
		m["permissions"] = perms
	}
	// The templates dropped the live permission {read **/.env deny}; the engine
	// keeps live array elements it does not manage, in their place.
	if c.Start == "live" && len(perms) > 0 {
		kept := map[string]any{"action": "read", "effect": "deny", "resource": "**/.env"}
		m["permissions"] = append([]any{perms[0], kept}, perms[1:]...)
	}
	return m
}

// moveOpenCodeServers rewrites the managed servers of a golden mcp object to opencode 2's
// layout: under mcp.servers (the flat layout is deprecated), the client id named client_id,
// and no enabled field (servers connect unless disabled). The live unmanaged server stays
// where it was.
func moveOpenCodeServers(mcp map[string]any) {
	servers := map[string]any{}
	for name, v := range mcp {
		entry, _ := v.(map[string]any)
		if _, managed := entry["enabled"]; !managed {
			continue
		}
		delete(entry, "enabled")
		if oauth, ok := entry["oauth"].(map[string]any); ok {
			if id, ok := oauth["clientId"]; ok {
				delete(oauth, "clientId")
				oauth["client_id"] = id
			}
		}
		servers[name] = entry
		delete(mcp, name)
	}
	if len(servers) > 0 {
		mcp["servers"] = servers
	}
}
