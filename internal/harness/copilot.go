package harness

import (
	"maps"
	"slices"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// copilotKeyCommand is the key of a server's executable in Copilot's config files.
const copilotKeyCommand = "command"

// renderCopilot adds the Copilot CLI targets.
func renderCopilot(c *config.Config, b *builder) error {
	renderCopilotFiles(c, b, string(config.Copilot))
	return nil
}

// renderCopilotFiles adds the files under ~/.copilot, owned by owner: VS
// Code's agent chat reads the same files, so renderVSCode calls it too.
func renderCopilotFiles(c *config.Config, b *builder, owner string) {
	b.add(apply.Target{
		Owner: owner, Path: b.home(".copilot", "settings.json"), Format: apply.JSON,
		Managed: copilotSettings(c, b), Union: [][]string{{"allowedUrls"}},
		Legacy: [][]string{{"statusLine"}}, Mode: 0o600,
	})
	if servers := b.lspServers(); len(servers) > 0 || len(c.LSP.Servers) > 0 {
		b.add(apply.Target{
			Owner: owner, Path: b.home(".copilot", "lsp-config.json"), Format: apply.JSON,
			Managed: map[string]any{"lspServers": copilotLSP(servers)},
			Legacy:  copilotLSPRemove(c, servers), Mode: 0o600,
		})
	}
	if servers := b.mcpServers(); len(servers) > 0 {
		b.add(apply.Target{
			Owner: owner, Path: b.home(".copilot", "mcp-config.json"), Format: apply.JSON,
			Managed: map[string]any{keyMCPServers: copilotMCP(servers)}, Mode: 0o600,
		})
	}
	before := len(b.targets)
	b.instructions(config.Copilot, b.home(".copilot", "copilot-instructions.md"))
	if len(b.targets) > before {
		b.targets[len(b.targets)-1].Owner = owner
	}
}

// copilotSettings is the managed part of ~/.copilot/settings.json.
func copilotSettings(c *config.Config, b *builder) map[string]any {
	out := map[string]any{}
	if c.Notifications.TerminalBell {
		out["beep"] = true
	}
	out["voice"] = map[string]any{keyEnabled: c.Voice.Enabled}
	model, _ := sessionModel(c, config.Copilot) // no session effortLevel: model only
	setIf(out, keyModel, model)
	if len(c.Permissions.AllowedURLs) > 0 {
		out["allowedUrls"] = slices.Clone(c.Permissions.AllowedURLs)
	}
	if agents := copilotAgentModels(c, b.env.Agents); len(agents) > 0 {
		out["subagents"] = map[string]any{keyCopilotAgents: agents}
	}
	if mkt := MarketplaceName(c); mkt != "" {
		plugins := map[string]any{}
		for name, on := range PluginStates(c, config.Copilot) {
			plugins[name+"@"+mkt] = on
		}
		if len(plugins) > 0 {
			out["enabledPlugins"] = plugins
		}
		out["extraKnownMarketplaces"] = map[string]any{
			mkt: map[string]any{"source": map[string]any{"source": "github", "repo": c.Plugins.Registry}},
		}
	}
	extra := b.expandSettings("harness.copilot.settings", c.Harness.Copilot.Settings)
	return mergeSettings(out, extra)
}

// copilotLSP renders the lspServers of lsp-config.json: argv split into
// command and args.
func copilotLSP(servers []LSPServer) map[string]any {
	out := map[string]any{}
	for _, s := range servers {
		out[s.Name] = map[string]any{
			copilotKeyCommand: s.Command[0],
			keyArgs:           append([]string{}, s.Command[1:]...),
			"fileExtensions":  maps.Clone(s.Extensions),
		}
	}
	return out
}

// copilotLSPRemove lists the managed keys of every configured server that is
// not rendered (lsp off, or its command is not on PATH), so an entry from an
// earlier run goes while keys the user added to it stay.
func copilotLSPRemove(c *config.Config, rendered []LSPServer) [][]string {
	var out [][]string
	for _, name := range slices.Sorted(maps.Keys(c.LSP.Servers)) {
		if slices.ContainsFunc(rendered, func(s LSPServer) bool { return s.Name == name }) {
			continue
		}
		for _, key := range []string{copilotKeyCommand, keyArgs, "fileExtensions"} {
			out = append(out, []string{"lspServers", name, key})
		}
	}
	return out
}

// copilotMCP renders the mcpServers of mcp-config.json. A pre-registered
// OAuth client uses Copilot's flat keys.
func copilotMCP(servers map[string]config.MCPServer) map[string]any {
	out := map[string]any{}
	for name, s := range servers {
		if s.URL != "" {
			out[name] = copilotRemoteServer(s)
		} else {
			out[name] = copilotLocalServer(s)
		}
	}
	return out
}

func copilotRemoteServer(s config.MCPServer) map[string]any {
	e := map[string]any{"type": valueOr(s.Type, "http"), "url": s.URL}
	if len(s.Headers) > 0 {
		e["headers"] = maps.Clone(s.Headers)
	}
	if s.OAuth.ClientID == "" {
		return e
	}
	e["oauthClientId"] = s.OAuth.ClientID
	if s.OAuth.PublicClient != nil {
		e["oauthPublicClient"] = *s.OAuth.PublicClient
	}
	if s.OAuth.CallbackPort != 0 {
		e["auth"] = map[string]any{"redirectPort": s.OAuth.CallbackPort}
	}
	return e
}

func copilotLocalServer(s config.MCPServer) map[string]any {
	e := map[string]any{"type": valueOr(s.Type, "stdio"), copilotKeyCommand: s.Command}
	if len(s.Args) > 0 {
		e[keyArgs] = slices.Clone(s.Args)
	}
	if len(s.Env) > 0 {
		e["env"] = maps.Clone(s.Env)
	}
	return e
}

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// copilotAgentModels maps each assigned agent id to its tier's Copilot model
// and effort; an agent whose tier has no Copilot model keeps the plugin
// default. Glob assignments expand against the installed agents.
func copilotAgentModels(c *config.Config, agents []string) map[string]any {
	out := map[string]any{}
	for id, t := range agentTiers(c, config.Copilot, agents) {
		e := map[string]any{keyModel: t.Model(config.Copilot)}
		setIf(e, "effortLevel", t.EffortFor(config.Copilot))
		out[id] = e
	}
	return out
}
