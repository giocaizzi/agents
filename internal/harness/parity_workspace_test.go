package harness

// adjustWorkspace edits a workspace golden (decoded) for the differences agents
// makes on purpose; every edit carries its reason.
func adjustWorkspace(_ parityCase, file string, want any) any {
	m, _ := want.(map[string]any)
	const crm = "https://crm.example.com/mcp"
	// Project files are committed, so agents never expands ${MCP_ACCOUNT} into them: the
	// reference stays where the harness expands it, in its own spelling, and a server that
	// holds one is left out of a harness that expands none (codex, pi, antigravity).
	switch file {
	case ".mcp.json":
		servers := table(m, "mcpServers")
		table(servers, "alpha")["url"] = "https://alpha.${MCP_ACCOUNT}.example.com/mcp"
		table(servers, "beta")["url"] = "https://beta.${MCP_ACCOUNT}.example.com/mcp?toolset=admin"
	case "opencode.json":
		// opencode 2 reads mcp.servers, with no enabled field, and takes the OAuth client
		// of crm as oauth.client_id and oauth.callback_port.
		servers := map[string]any{}
		for name, v := range table(m, "mcp") {
			entry, _ := v.(map[string]any)
			delete(entry, "enabled")
			servers[name] = entry
		}
		m["mcp"] = map[string]any{"servers": servers}
		servers["crm"] = map[string]any{
			"type": "remote", "url": crm,
			"oauth": map[string]any{"client_id": "client-123", "callback_port": float64(8765)},
		}
		table(servers, "alpha")["url"] = "https://alpha.{env:MCP_ACCOUNT}.example.com/mcp"
		table(servers, "beta")["url"] = "https://beta.{env:MCP_ACCOUNT}.example.com/mcp?toolset=admin"
	case ".codex/config.toml":
		// The old templates dropped OAuth servers for Codex; agents translates
		// them since the OAuth client sync: crm gets its oauth client.
		table(m, "mcp_servers")["crm"] = map[string]any{
			"url":   crm,
			"oauth": map[string]any{"client_id": "client-123", "callback_port": int64(8765)},
		}
		dropAccountServers(table(m, "mcp_servers"))
	case ".pi/mcp.json":
		// Same for pi, which reads the nested oauth object, and its servers
		// carry their transport type as the mcp sync always wrote them.
		servers := table(m, "mcpServers")
		servers["crm"] = map[string]any{
			"url":   crm,
			"oauth": map[string]any{"clientId": "client-123", "callbackPort": float64(8765)},
		}
		addType(servers)
		dropAccountServers(servers)
	case ".agents/mcp_config.json":
		// Antigravity keeps the client id of crm (no callbackPort, which it
		// cannot set) and the transport type, as the mcp sync always wrote them.
		servers := table(m, "mcpServers")
		table(servers, "crm")["oauth"] = map[string]any{"clientId": "client-123"}
		addType(servers)
		dropAccountServers(servers)
	}
	return m
}

// table returns the object at m[key] (nil when absent or not an object).
func table(m map[string]any, key string) map[string]any {
	t, _ := m[key].(map[string]any)
	return t
}

// addType sets "type": "http" on every server of the decoded golden.
func addType(servers map[string]any) {
	for name := range servers {
		table(servers, name)["type"] = "http"
	}
}

// dropAccountServers removes the servers whose url holds ${MCP_ACCOUNT}.
func dropAccountServers(servers map[string]any) {
	delete(servers, "alpha")
	delete(servers, "beta")
}
