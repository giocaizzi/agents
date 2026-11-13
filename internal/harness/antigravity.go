package harness

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// renderAntigravity adds the Antigravity targets: settings, MCP servers and
// instructions.
func renderAntigravity(c *config.Config, b *builder) error {
	a := c.Harness.Antigravity
	settings := map[string]any{}
	if perms := antigravityPermissions(c); len(perms) > 0 {
		settings["permissions"] = perms
	}
	mode := a.AgentMode
	if mode == "" {
		mode = c.Autonomy.ExecutionMode
	}
	setIf(settings, "agentMode", mode)
	setIf(settings, "toolPermission", a.ToolPermission)
	settings["enableTerminalSandbox"] = a.EnableTerminalSandbox
	settings["allowNonWorkspaceAccess"] = a.AllowNonWorkspaceAccess || c.Autonomy.AllowNonWorkspaceAccess
	deepMerge(settings, a.Settings)

	gemini := b.home(".gemini")
	b.add(apply.Target{
		Owner: string(config.Antigravity), Path: filepath.Join(gemini, "antigravity-cli", "settings.json"),
		Format: apply.JSON, Managed: settings, Mode: 0o600,
	})
	b.add(apply.Target{
		Owner: string(config.Antigravity), Path: filepath.Join(gemini, "config", "mcp_config.json"),
		Format: apply.JSON, Managed: map[string]any{keyMCPServers: mcpJSON(b.mcpServers(), false)}, Mode: 0o600,
	})
	b.instructions(config.Antigravity, filepath.Join(gemini, "config", "AGENTS.md"))
	return nil
}

// antigravityPermissions builds the allow, ask and deny rules in Antigravity's
// native syntax. command() matches a token prefix and treats * as a literal:
// a trailing " *" becomes the prefix, and a pattern with an inner * cannot be
// expressed, so it is skipped.
func antigravityPermissions(c *config.Config) map[string]any {
	p := c.Permissions
	allow := append([]string(nil), c.Harness.Antigravity.Allow...)
	for _, pat := range p.AllowCommandPatterns {
		if prefix := strings.TrimSuffix(pat, " *"); !strings.Contains(prefix, "*") {
			allow = appendUnique(allow, "command("+prefix+")")
		}
	}

	var deny []string
	for _, pat := range p.DenyCommandPatterns {
		if !strings.HasPrefix(pat, "git push ") {
			deny = append(deny, "command("+pat+")")
			continue
		}
		if prefix := strings.TrimSuffix(pat, " *"); !strings.Contains(prefix, "*") {
			deny = appendUnique(deny, "command("+prefix+")")
		}
	}
	for _, cmd := range p.DenyCommandExact {
		deny = append(deny, "command("+cmd+")")
	}
	deny = append(deny, prefixRules(p.DenyCommandPrefixes)...)
	for _, path := range p.DenyRead {
		deny = append(deny, "read_file("+path+")")
	}
	for _, path := range p.DenyWrite {
		deny = append(deny, "write_file("+path+")")
	}

	out := map[string]any{}
	for key, list := range map[string][]string{"allow": allow, "ask": prefixRules(p.AskCommandPrefixes), "deny": deny} {
		if len(list) > 0 {
			out[key] = toAnySlice(list)
		}
	}
	return out
}

// prefixRules is command(p) for a single-word prefix and command(p *) for all.
func prefixRules(prefixes []string) []string {
	var out []string
	for _, pfx := range prefixes {
		if !strings.Contains(pfx, " ") {
			out = append(out, "command("+pfx+")")
		}
		out = append(out, "command("+pfx+" *)")
	}
	return out
}

func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
