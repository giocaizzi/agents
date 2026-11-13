package harness

import (
	"path/filepath"
	"slices"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// renderPi adds the pi targets: settings, MCP servers and instructions.
func renderPi(c *config.Config, b *builder) error {
	p := c.Harness.Pi
	settings := map[string]any{}
	setIf(settings, "defaultProvider", p.DefaultProvider)
	setIf(settings, "defaultModel", p.DefaultModel)
	setIf(settings, "defaultThinkingLevel", p.DefaultThinkingLevel)

	var packages []string
	if dir := RegistryDir(c, b.env.Dirs); dir != "" {
		packages = append(packages, dir)
	}
	packages = append(packages, p.Packages...)
	var union [][]string
	if len(packages) > 0 {
		settings["packages"] = toAnySlice(slices.Compact(packages))
		union = [][]string{{"packages"}}
	}
	deepMerge(settings, p.Settings)

	base := b.home(".pi", "agent")
	b.add(apply.Target{
		Owner: string(config.Pi), Path: filepath.Join(base, "settings.json"), Format: apply.JSON,
		Managed: settings, Union: union, Mode: 0o600,
	})
	b.add(apply.Target{
		Owner: string(config.Pi), Path: filepath.Join(base, "mcp.json"), Format: apply.JSON,
		Managed: map[string]any{"mcpServers": mcpJSON(b.mcpServers(), true)}, Mode: 0o600,
	})
	b.instructions(config.Pi, filepath.Join(base, "AGENTS.md"))
	return nil
}

// mcpJSON is the server shape pi and Antigravity share: Claude Code's nested
// OAuth object. withPort keeps oauth.callbackPort, which Antigravity cannot set.
func mcpJSON(servers map[string]config.MCPServer, withPort bool) map[string]any {
	out := make(map[string]any, len(servers))
	for name, s := range servers {
		e := map[string]any{}
		setIf(e, "type", s.Type)
		setIf(e, "url", s.URL)
		setIf(e, "command", s.Command)
		if len(s.Headers) > 0 {
			e["headers"] = anyMap(s.Headers)
		}
		if len(s.Args) > 0 {
			e["args"] = toAnySlice(s.Args)
		}
		if len(s.Env) > 0 {
			e["env"] = anyMap(s.Env)
		}
		oauth := map[string]any{}
		setIf(oauth, "clientId", s.OAuth.ClientID)
		if withPort && s.OAuth.CallbackPort != 0 {
			oauth["callbackPort"] = s.OAuth.CallbackPort
		}
		if len(oauth) > 0 {
			e["oauth"] = oauth
		}
		out[name] = e
	}
	return out
}
