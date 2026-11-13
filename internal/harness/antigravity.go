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
	if perms := antigravityPermissions(c, b); len(perms) > 0 {
		settings["permissions"] = perms
	}
	// The typed fields win over the shared modes.
	setIf(settings, "agentMode", valueOr(a.AgentMode,
		b.nativeMode(keyExecution, c.Autonomy.ExecutionMode, "Antigravity", "agentMode", antigravityExecution())))
	toolPermission := a.ToolPermission
	if c.Autonomy.ExecutionMode == config.ExecutionFull && a.AgentMode == "" { // only when agentMode comes from the shared value
		toolPermission = valueOr(toolPermission, "always-proceed") // agentMode auto alone still pauses risky commands
	}
	setIf(settings, "toolPermission", toolPermission)
	sandbox := b.sandboxOn(c.Autonomy.SandboxMode, "Antigravity", "enableTerminalSandbox")
	if a.EnableTerminalSandbox != nil { // the typed field wins, true or false
		sandbox = *a.EnableTerminalSandbox
	}
	settings["enableTerminalSandbox"] = sandbox // always written, so off is false
	setIf(settings, "colorScheme", c.UI.Theme)
	if c.Notifications.TerminalBell {
		settings["notifications"] = true
	}
	settings["allowNonWorkspaceAccess"] = a.AllowNonWorkspaceAccess || c.Autonomy.AllowNonWorkspaceAccess
	deepMerge(settings, a.Settings)

	gemini := b.home(".gemini")
	b.add(apply.Target{
		Owner: string(config.Antigravity), Path: filepath.Join(gemini, "antigravity-cli", "settings.json"),
		Format: apply.JSON, Managed: settings, Mode: 0o600,
	})
	if mcp := b.mcpServers(); len(mcp) > 0 { // never an empty mcpServers object
		b.add(apply.Target{
			Owner: string(config.Antigravity), Path: filepath.Join(gemini, "config", "mcp_config.json"),
			Format: apply.JSON, Managed: map[string]any{keyMCPServers: mcpJSON(mcp, false)}, Mode: 0o600,
		})
	}
	b.instructions(config.Antigravity, filepath.Join(gemini, "config", "AGENTS.md"))
	return nil
}

// antigravityPermissions builds the allow, ask and deny rules in Antigravity's
// native syntax. command() matches a token prefix and treats * as a literal:
// a trailing " *" becomes the prefix, and a pattern with an inner * cannot be
// expressed, so it is skipped.
func antigravityPermissions(c *config.Config, b *builder) map[string]any {
	p := c.Permissions
	allow := antigravityAllow(c, b)

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

// antigravityAllow is the typed allow rules, then the shared commands, URLs
// and MCP servers as command(), read_url() and mcp() rules.
func antigravityAllow(c *config.Config, b *builder) []string {
	p := c.Permissions
	unexpressible := unexpressibleDenies(&p)
	allow := append([]string(nil), c.Harness.Antigravity.Allow...)
	for _, pat := range slices.Concat(p.AllowCommands, p.AllowCommandPatterns) {
		prefix := strings.TrimSuffix(pat, " *")
		if strings.Contains(prefix, "*") {
			continue
		}
		// command() matches word prefixes only: skip an allow that an
		// inexpressible deny would have overridden (see overlap.go).
		if words, ok := prefixWords(pat); ok && b.allowOverlapsDeny("Antigravity", "permissions.allow_commands", pat, words, unexpressible) {
			continue
		}
		allow = appendUnique(allow, "command("+prefix+")")
	}
	for _, u := range p.AllowedURLs {
		if host, ok := antigravityHost(u); ok {
			allow = appendUnique(allow, "read_url("+host+")")
		}
	}
	for _, m := range p.AllowedMCP {
		if !strings.Contains(m, "/") && m != "*" {
			m += "/*" // a bare name is a server: all its tools
		}
		allow = appendUnique(allow, "mcp("+m+")")
	}
	return allow
}

// antigravityHost is the read_url target of an allowed URL. read_url matches a
// hostname and its subdomains and ignores the path, so only a URL that covers
// a whole host (no path, or a trailing /, /* or /**) can be expressed, and a
// leading "*." is dropped because the bare domain already covers subdomains.
func antigravityHost(u string) (string, bool) {
	_, rest, found := strings.Cut(u, "://")
	if !found {
		rest = u
	}
	host, path, _ := strings.Cut(rest, "/")
	if path != "" && path != "*" && path != "**" {
		return "", false
	}
	host = strings.TrimPrefix(host, "*.")
	if host == "" || strings.Contains(host, "*") {
		return "", false
	}
	return host, true
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
