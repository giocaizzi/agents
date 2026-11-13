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
	if d := c.Autonomy.SubagentDepth; d > 0 {
		cfg["experimental"] = map[string]any{"subagent_depth": int64(d)}
	}
	if lsp := b.lspServers(); len(lsp) > 0 {
		out := map[string]any{}
		for _, s := range lsp {
			out[s.Name] = map[string]any{
				"command":    stringSlice(s.Command),
				"extensions": stringSlice(slices.Sorted(maps.Keys(s.Extensions))),
			}
		}
		cfg["lsp"] = out
	}
	if servers := openCodeServers(b.mcpServers()); len(servers) > 0 {
		cfg["mcp"] = servers
	}
	permissions, policies := openCodeRules(&c.Permissions)
	if len(permissions) > 0 {
		cfg["permissions"] = permissions
	}
	if len(policies) > 0 {
		cfg["policies"] = policies
	}
	mergeSettings(cfg, b.expandSettings("harness.opencode.settings", c.Harness.OpenCode.Settings))

	b.add(apply.Target{
		Owner: string(config.OpenCode), Path: b.home(".config", "opencode", "opencode.jsonc"),
		Format: apply.JSONC, Managed: cfg, Mode: 0o600,
		Union: [][]string{{"permissions"}, {"policies"}},
	})
	b.instructions(config.OpenCode, b.home(".config", "opencode", "AGENTS.md"))
	return nil
}

// openCodeServers renders MCP servers as opencode mcp entries.
func openCodeServers(in map[string]config.MCPServer) map[string]any {
	out := map[string]any{}
	for name, s := range in {
		e := map[string]any{"enabled": true}
		switch {
		case s.URL != "":
			e["type"] = "remote"
			e["url"] = s.URL
			if len(s.Headers) > 0 {
				e["headers"] = stringMap(s.Headers)
			}
			if s.OAuth.ClientID != "" {
				e["oauth"] = map[string]any{"clientId": s.OAuth.ClientID}
			}
		case s.Command != "":
			e["type"] = "local"
			e["command"] = stringSlice(append([]string{s.Command}, s.Args...))
			if len(s.Env) > 0 {
				e["environment"] = stringMap(s.Env)
			}
		default:
			continue
		}
		out[name] = e
	}
	return out
}

// openCodeRules renders permissions into opencode's permissions (allow and
// ask) and policies (deny), as arrays that merge into the live ones.
func openCodeRules(p *config.Permissions) (permissions, policies []any) {
	rule := func(action, effect, resource string) map[string]any {
		return map[string]any{"action": action, "effect": effect, "resource": resource}
	}
	for _, pat := range p.AllowCommandPatterns {
		permissions = append(permissions, rule("shell", "allow", pat))
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
