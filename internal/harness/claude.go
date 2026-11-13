package harness

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

const (
	claudeOwner       = "claude"
	claudeLSPPlugin   = "lsp"
	claudeLocalMarket = "agents"

	keySource = "source"
	keyName   = "name"
	keyDesc   = "description"
	otlp      = "otlp"
)

// claudeTelemetryEnv are the env keys agents sets when telemetry is on; with
// telemetry off they are removed from the live file.
//
//nolint:gochecknoglobals // immutable list
var claudeTelemetryEnv = []string{
	"CLAUDE_CODE_ENABLE_TELEMETRY",
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE",
	"OTEL_EXPORTER_OTLP_PROTOCOL",
	"OTEL_LOGS_EXPORTER",
	"OTEL_LOG_TOOL_CONTENT",
	"OTEL_LOG_TOOL_DETAILS",
	"OTEL_METRICS_EXPORTER",
	"OTEL_RESOURCE_ATTRIBUTES",
	"OTEL_TRACES_EXPORTER",
}

// renderClaude adds the Claude Code targets: settings, user MCP servers, the
// local LSP plugin and the instructions.
func renderClaude(c *config.Config, b *builder) error {
	settings := claudeSettings(c, b)
	servers := b.lspServers()

	enabled, _ := settings["enabledPlugins"].(map[string]any)
	if enabled == nil {
		enabled = map[string]any{}
	}
	markets, _ := settings["extraKnownMarketplaces"].(map[string]any)
	if markets == nil {
		markets = map[string]any{}
	}
	if name := MarketplaceName(c); name != "" {
		for plugin, on := range PluginStates(c, config.Claude) {
			enabled[plugin+"@"+name] = on
		}
		markets[name] = map[string]any{
			keySource:    map[string]any{keySource: "github", "repo": c.Plugins.Registry},
			"autoUpdate": true,
		}
	}
	pluginDir := filepath.Join(b.env.Dirs.Data, "claude-plugins")
	if len(servers) > 0 {
		enabled[claudeLSPPlugin+"@"+claudeLocalMarket] = true
		markets[claudeLocalMarket] = map[string]any{
			keySource: map[string]any{keySource: "directory", "path": pluginDir},
		}
	}
	setIfAny(settings, "enabledPlugins", enabled)
	setIfAny(settings, "extraKnownMarketplaces", markets)

	// One-time cleanup of the old local marketplace.
	remove := [][]string{
		{"enabledPlugins", "lsp@dotfiles"},
		{"extraKnownMarketplaces", "dotfiles"},
	}
	if _, on := b.telemetry(config.Claude); !on {
		for _, k := range claudeTelemetryEnv {
			remove = append(remove, []string{"env", k})
		}
	}
	b.add(apply.Target{
		Owner: claudeOwner, Path: b.home(".claude", "settings.json"), Format: apply.JSON,
		Managed: settings, Legacy: remove, Mode: 0o600,
	})

	if len(servers) > 0 {
		claudeLSPPluginTargets(b, pluginDir, servers)
	}

	b.add(apply.Target{
		Owner: claudeOwner, Path: b.home(".claude.json"), Format: apply.JSON,
		Managed: map[string]any{keyMCPServers: claudeMCPServers(b.mcpServers())}, Mode: 0o600,
	})

	b.instructions(config.Claude, b.home(".claude", "CLAUDE.md"))
	return nil
}

func setIfAny(m map[string]any, key string, v map[string]any) {
	if len(v) > 0 {
		m[key] = v
	}
}

// claudeSettings builds the managed keys of ~/.claude/settings.json except
// the plugin and marketplace maps the caller completes.
func claudeSettings(c *config.Config, b *builder) map[string]any {
	cl := c.Harness.Claude
	s := map[string]any{}
	set := func(key string, v any) {
		switch t := v.(type) {
		case string:
			if t == "" {
				return
			}
		case int:
			if t == 0 {
				return
			}
		case bool:
			if !t {
				return
			}
		}
		s[key] = v
	}

	set("askUserQuestionTimeout", c.Autonomy.AskTimeout)
	set("effortLevel", c.Models.DefaultEffortLevel)
	if model, effort := sessionModel(c, config.Claude); model != "" {
		set(keyModel, model)
		set("effortLevel", effort) // only when the default tier sets one
	}
	set("showThinkingSummaries", c.Models.ShowThinkingSummaries)
	set("autoCompactWindow", c.Context.AutoCompactWindow)
	set("promptCacheTtl", c.Context.PromptCacheTTL)
	set("subagentPromptCacheTtl", c.Context.SubagentPromptCacheTTL)
	set("theme", c.UI.Theme)
	set("tui", c.UI.TUIMode)
	set("viewMode", c.UI.ViewMode)
	set("outputStyle", c.UI.OutputStyle)
	set("showTurnDuration", c.UI.ShowTurnDuration)
	set("terminalProgressBarEnabled", c.UI.ProgressBar)
	set("timeFormat", c.UI.TimeFormat)
	if c.Notifications.TerminalBell {
		s["preferredNotifChannel"] = "terminal_bell"
	}
	if v := c.Voice; v != (config.Voice{}) {
		voice := map[string]any{keyEnabled: v.Enabled, "autoSubmit": v.AutoSubmit}
		if v.Mode != "" {
			voice["mode"] = v.Mode
		}
		if v.Language != "" {
			voice["language"] = v.Language
		}
		s["voice"] = voice
	}
	set("cleanupPeriodDays", cl.CleanupPeriodDays)
	set("disableClaudeAiConnectors", cl.DisableClaudeAIConnectors)
	set("skipAutoPermissionPrompt", cl.SkipAutoPermissionPrompt)
	s["permissions"] = claudePermissions(c, b.env.GOOS)

	if tel, on := b.telemetry(config.Claude); on {
		s["env"] = claudeTelemetry(tel)
	}
	deepMerge(s, b.expandSettings("harness.claude.settings", cl.Settings))
	return s
}

// claudePermissions maps the shared permission rules to Claude's schema.
func claudePermissions(c *config.Config, goos string) map[string]any {
	p := c.Permissions
	allow := slices.Clone(c.Harness.Claude.Allow)
	for _, pat := range p.AllowCommandPatterns {
		allow = append(allow, "Bash("+pat+")")
	}
	var deny []string
	for _, pat := range p.DenyCommandPatterns {
		deny = append(deny, "Bash("+pat+")")
	}
	for _, path := range p.DenyRead {
		deny = append(deny, "Read("+path+")")
	}
	for _, path := range p.DenyWrite {
		deny = append(deny, "Edit("+path+")")
	}
	for _, cmd := range p.DenyCommandExact {
		deny = append(deny, "Bash("+cmd+")")
	}
	for _, pfx := range p.DenyCommandPrefixes {
		deny = append(deny, "Bash("+pfx+":*)")
	}
	if goos == goosWindows {
		for _, pfx := range p.DenyCommandPrefixes {
			if pfx != "printenv" {
				deny = append(deny, "PowerShell("+pfx+" *)")
			}
		}
	}
	ask := make([]string, 0, 2*len(p.AskCommandPrefixes))
	for _, pfx := range p.AskCommandPrefixes {
		ask = append(ask, "Bash("+pfx+")", "Bash("+pfx+" *)")
	}
	mode := c.Harness.Claude.DefaultMode
	if mode == "" {
		mode = "auto"
	}
	out := map[string]any{"defaultMode": mode}
	for k, v := range map[string][]string{"allow": allow, "deny": deny, "ask": ask} {
		if len(v) > 0 {
			out[k] = v
		}
	}
	return out
}

// claudeTelemetry is the env of Claude Code's OpenTelemetry export.
func claudeTelemetry(t Telemetry) map[string]any {
	flag := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	res := []string{"service.namespace=" + t.Service, "host.name=" + t.Hostname, "host.arch=" + t.Arch}
	for _, k := range slices.Sorted(maps.Keys(t.Attributes)) {
		if k == "service.namespace" {
			continue
		}
		res = append(res, k+"="+t.Attributes[k])
	}
	return map[string]any{
		"CLAUDE_CODE_ENABLE_TELEMETRY":                      "1",
		"OTEL_EXPORTER_OTLP_ENDPOINT":                       t.Endpoint,
		"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE": t.Temporality,
		"OTEL_EXPORTER_OTLP_PROTOCOL":                       t.Protocol,
		"OTEL_LOGS_EXPORTER":                                otlp,
		"OTEL_LOG_TOOL_CONTENT":                             flag(t.LogToolContent),
		"OTEL_LOG_TOOL_DETAILS":                             flag(t.LogToolDetails),
		"OTEL_METRICS_EXPORTER":                             otlp,
		"OTEL_RESOURCE_ATTRIBUTES":                          strings.Join(res, ","),
		"OTEL_TRACES_EXPORTER":                              otlp,
	}
}

// expandSettings expands ${VAR} in every string of in. A map entry or list
// element with an unset variable is skipped and warned.
func (b *builder) expandSettings(where string, in map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range slices.Sorted(maps.Keys(in)) {
		if v, ok := b.expandValue(where+"."+k, in[k]); ok {
			out[k] = v
		}
	}
	return out
}

func (b *builder) expandValue(where string, v any) (any, bool) {
	switch t := v.(type) {
	case string:
		return b.expand(where, t)
	case map[string]any:
		return b.expandSettings(where, t), true
	case []any:
		out := make([]any, 0, len(t))
		for i, e := range t {
			if x, ok := b.expandValue(fmt.Sprintf("%s[%d]", where, i), e); ok {
				out = append(out, x)
			}
		}
		return out, true
	default:
		return v, true
	}
}

// claudeLSPPluginTargets writes the local marketplace and plugin that carry
// the language servers.
func claudeLSPPluginTargets(b *builder, dir string, servers []LSPServer) {
	const desc = "Language servers from the agents configuration"
	add := func(path string, m map[string]any) {
		b.add(apply.Target{Owner: claudeOwner, Path: filepath.Join(dir, filepath.FromSlash(path)), Format: apply.JSON, Managed: m, Mode: 0o644})
	}
	add(".claude-plugin/marketplace.json", map[string]any{
		keyName: claudeLocalMarket,
		"owner": map[string]any{keyName: claudeLocalMarket},
		keyDesc: "Local plugins generated by agents",
		"plugins": []any{map[string]any{
			keyName: claudeLSPPlugin, keySource: "./" + claudeLSPPlugin, keyDesc: desc,
		}},
	})
	add("lsp/.claude-plugin/plugin.json", map[string]any{
		keyName: claudeLSPPlugin, keyDesc: desc, "author": map[string]any{keyName: claudeLocalMarket},
	})
	add("lsp/.lsp.json", claudeLSPConfig(servers))
}

// claudeLSPConfig lists the servers by name. Claude gives each extension to
// one server, so a server keeps only the extensions no earlier server claimed
// and is left out when none remain.
func claudeLSPConfig(servers []LSPServer) map[string]any {
	out := map[string]any{}
	claimed := map[string]bool{}
	for _, s := range servers {
		ext := map[string]any{}
		for e, lang := range s.Extensions {
			if !claimed[e] {
				ext[e] = lang
			}
		}
		if len(ext) == 0 {
			continue
		}
		for e := range ext {
			claimed[e] = true
		}
		out[s.Name] = map[string]any{
			"command":             s.Command[0],
			keyArgs:               append([]string{}, s.Command[1:]...),
			"extensionToLanguage": ext,
		}
	}
	return out
}

// claudeMCPServers is the mcpServers value of ~/.claude.json.
func claudeMCPServers(in map[string]config.MCPServer) map[string]any {
	out := map[string]any{}
	for name, s := range in {
		m := map[string]any{}
		if s.URL != "" {
			m["type"] = "http"
			m["url"] = s.URL
			if len(s.Headers) > 0 {
				m["headers"] = s.Headers
			}
			if oauth := claudeOAuth(s.OAuth); len(oauth) > 0 {
				m["oauth"] = oauth
			}
		} else {
			m["type"] = "stdio"
			m["command"] = s.Command
			m[keyArgs] = append([]string{}, s.Args...)
			m["env"] = s.Env
		}
		out[name] = m
	}
	return out
}

func claudeOAuth(o config.OAuth) map[string]any {
	out := map[string]any{}
	if o.ClientID != "" {
		out["clientId"] = o.ClientID
	}
	if o.CallbackPort != 0 {
		out["callbackPort"] = o.CallbackPort
	}
	return out
}
