package harness

import (
	"maps"
	"slices"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// renderOpenCode adds the opencode targets: opencode.jsonc and the
// instructions.
func renderOpenCode(c *config.Config, b *builder) error {
	cfg := map[string]any{"$schema": "https://opencode.ai/config.json"}
	model, _ := sessionModel(c, config.OpenCode) // opencode has no session-level effort
	setIf(cfg, keyModel, model)
	if agents := openCodeAgentModels(c, b.env.Agents); len(agents) > 0 {
		cfg["agents"] = agents
	}
	if d := c.Autonomy.SubagentDepth; d > 0 {
		cfg["experimental"] = map[string]any{"subagent_depth": int64(d)}
	}
	if lsp := b.lspServers(); len(lsp) > 0 {
		out := map[string]any{}
		for _, s := range lsp {
			out[s.Name] = map[string]any{
				"command":    toAnySlice(s.Command),
				"extensions": toAnySlice(slices.Sorted(maps.Keys(s.Extensions))),
			}
		}
		cfg["lsp"] = out
	}
	servers := openCodeServers(b.mcpServers())
	if len(servers) > 0 {
		cfg["mcp"] = map[string]any{"servers": servers}
	}
	if c.Context.AutoCompact {
		cfg["compaction"] = map[string]any{"auto": true}
	}
	permissions, policies := openCodeRules(c)
	if len(permissions) > 0 {
		cfg["permissions"] = permissions
	}
	if len(policies) > 0 {
		cfg["policies"] = policies
	}
	deepMerge(cfg, b.expandSettings("harness.opencode.settings", c.Harness.OpenCode.Settings))

	b.add(apply.Target{
		Owner: string(config.OpenCode), Path: b.home(".config", "opencode", "opencode.jsonc"),
		Format: apply.JSONC, Managed: cfg, Mode: 0o600,
		Union:  [][]string{{keyPermissions}, {"policies"}},
		Legacy: openCodeLegacy(servers),
	})
	b.instructions(config.OpenCode, b.home(".config", "opencode", "AGENTS.md"))
	return nil
}

// openCodeLegacy are the server entries earlier managers wrote straight under
// mcp, a layout opencode 2 deprecates in favour of mcp.servers.
func openCodeLegacy(servers map[string]any) [][]string {
	names := slices.Sorted(maps.Keys(servers))
	out := make([][]string, 0, len(names))
	for _, name := range names {
		out = append(out, []string{"mcp", name})
	}
	return out
}

// openCodeServers renders MCP servers as entries of opencode's mcp.servers.
func openCodeServers(in map[string]config.MCPServer) map[string]any {
	out := map[string]any{}
	for name, s := range in {
		e := map[string]any{}
		switch {
		case s.URL != "":
			e["type"] = "remote"
			e["url"] = s.URL
			if len(s.Headers) > 0 {
				e["headers"] = anyMap(s.Headers)
			}
			if oauth := openCodeOAuth(s.OAuth); len(oauth) > 0 {
				e["oauth"] = oauth
			}
		case s.Command != "":
			e["type"] = "local"
			e["command"] = toAnySlice(append([]string{s.Command}, s.Args...))
			if len(s.Env) > 0 {
				e["environment"] = anyMap(s.Env)
			}
		default:
			continue
		}
		out[name] = e
	}
	return out
}

// openCodeOAuth renders a pre-registered OAuth client as opencode's oauth.
func openCodeOAuth(o config.OAuth) map[string]any {
	out := map[string]any{}
	setIf(out, "client_id", o.ClientID)
	if o.CallbackPort > 0 {
		out["callback_port"] = int64(o.CallbackPort)
	}
	return out
}

// openCodeRules renders permissions into opencode's permissions (allow and
// ask) and policies (deny), as arrays that merge into the live ones. Allow
// rules come before ask rules because the last matching rule wins.
func openCodeRules(c *config.Config) (permissions, policies []any) {
	p := &c.Permissions
	rule := func(action, effect, resource string) map[string]any {
		return map[string]any{"action": action, "effect": effect, "resource": resource}
	}
	if c.Autonomy.AllowNonWorkspaceAccess {
		permissions = append(permissions, rule("external_directory", "allow", "*"))
	}
	for _, pat := range slices.Concat(p.AllowCommands, p.AllowCommandPatterns) {
		permissions = append(permissions, rule("shell", "allow", pat))
	}
	for _, u := range p.AllowedURLs {
		permissions = append(permissions, rule("webfetch", "allow", u))
	}
	for _, pre := range p.AskCommandPrefixes {
		permissions = append(permissions, rule("shell", "ask", pre), rule("shell", "ask", pre+" *"))
	}
	deny := func(prefix string, resources ...string) {
		for _, r := range resources {
			policies = append(policies, rule("tool.use", "deny", prefix+":"+r))
		}
	}
	deny("read", p.DenyRead...)
	deny("edit", p.DenyWrite...)
	deny("shell", p.DenyCommandPatterns...)
	deny("shell", p.DenyCommandExact...)
	for _, pre := range p.DenyCommandPrefixes {
		deny("shell", pre, pre+" *")
	}
	return permissions, policies
}

// openCodeAgentModels maps each assigned agent's opencode name to its tier's
// model. Two plugins' agents of the same name share one entry: the later id
// in sorted order wins.
func openCodeAgentModels(c *config.Config, agents []string) map[string]any {
	out := map[string]any{}
	tiers := agentTiers(c, config.OpenCode, agents)
	for _, id := range slices.Sorted(maps.Keys(tiers)) {
		out[openCodeAgentName(id)] = map[string]any{keyModel: tiers[id].Model(config.OpenCode)}
	}
	return out
}
