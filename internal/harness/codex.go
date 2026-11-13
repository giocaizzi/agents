package harness

import (
	"fmt"
	"strings"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// renderCodex adds the Codex targets: config.toml, the exec-policy rules and
// the instructions.
func renderCodex(c *config.Config, b *builder) error {
	cfg := map[string]any{
		"tui": map[string]any{
			"notifications":       c.Notifications.TerminalBell,
			"notification_method": "bel",
		},
	}
	h := c.Harness.Codex
	tierModel, tierEffort := sessionModel(c, config.Codex)
	h.Model = valueOr(h.Model, tierModel) // the typed field wins over the default tier
	// The typed field wins, then the default tier's effort, then models.default_effort_level.
	h.ModelReasoningEffort = valueOr(h.ModelReasoningEffort, valueOr(tierEffort, c.Models.DefaultEffortLevel))
	// The typed fields win over the shared modes.
	h.ApprovalPolicy = valueOr(h.ApprovalPolicy,
		b.nativeMode(keyExecution, c.Autonomy.ExecutionMode, "Codex", "approval_policy", codexExecution()))
	h.SandboxMode = valueOr(h.SandboxMode,
		b.nativeMode(keySandbox, c.Autonomy.SandboxMode, "Codex", "sandbox_mode", codexSandbox()))
	for k, v := range map[string]string{
		keyModel:                 h.Model,
		"model_reasoning_effort": h.ModelReasoningEffort,
		"approval_policy":        h.ApprovalPolicy,
		"sandbox_mode":           h.SandboxMode,
	} {
		if v != "" {
			cfg[k] = v
		}
	}
	if servers := codexServers(b.mcpServers()); len(servers) > 0 {
		cfg["mcp_servers"] = servers
	}
	if plugins := codexPlugins(c); len(plugins) > 0 {
		cfg["plugins"] = plugins
	}
	if t, ok := b.telemetry(config.Codex); ok {
		cfg["otel"] = codexOTel(t)
	}
	if ws := b.workspaces(); len(ws) > 0 {
		projects := map[string]any{}
		for _, w := range ws {
			projects[w.Path] = map[string]any{"trust_level": "trusted"}
		}
		cfg["projects"] = projects
	}
	deepMerge(cfg, b.expandSettings("harness.codex.settings", h.Settings))

	b.add(apply.Target{Owner: string(config.Codex), Path: b.home(".codex", "config.toml"), Format: apply.TOML, Managed: cfg, Mode: 0o600})
	b.add(apply.Target{
		Owner: string(config.Codex), Path: b.home(".codex", "rules", "agents.rules"),
		Format: apply.Text, Managed: codexRules(&c.Permissions, b), Mode: 0o644,
	})
	b.instructions(config.Codex, b.home(".codex", "AGENTS.md"))
	return nil
}

// codexPlugins renders plugins.<name>@<marketplace>.enabled for every plugin
// enabled for Codex; plugins.disabled turns it off. The installer registers
// the marketplace itself.
func codexPlugins(c *config.Config) map[string]any {
	mkt := MarketplaceName(c)
	if mkt == "" {
		return nil
	}
	out := map[string]any{}
	for name, on := range PluginStates(c, config.Codex) {
		out[name+"@"+mkt] = map[string]any{keyEnabled: on}
	}
	return out
}

// codexServers renders MCP servers as Codex mcp_servers tables.
func codexServers(in map[string]config.MCPServer) map[string]any {
	out := map[string]any{}
	for name, s := range in {
		e := map[string]any{}
		switch {
		case s.URL != "":
			e["url"] = s.URL
			if len(s.Headers) > 0 {
				e["http_headers"] = anyMap(s.Headers)
			}
			if o := codexOAuth(s.OAuth); o != nil {
				e["oauth"] = o
			}
		case s.Command != "":
			e["command"] = s.Command
			if len(s.Args) > 0 {
				e[keyArgs] = toAnySlice(s.Args)
			}
			if len(s.Env) > 0 {
				e["env"] = anyMap(s.Env)
			}
		default:
			continue
		}
		out[name] = e
	}
	return out
}

// codexOAuth renders a pre-registered OAuth client, nil when there is none.
func codexOAuth(oa config.OAuth) map[string]any {
	if oa.ClientID == "" && oa.CallbackPort == 0 {
		return nil
	}
	o := map[string]any{}
	if oa.ClientID != "" {
		o["client_id"] = oa.ClientID
	}
	if oa.CallbackPort != 0 {
		o["callback_port"] = int64(oa.CallbackPort)
	}
	return o
}

// codexOTel renders the OpenTelemetry export: logs, metrics and traces over
// OTLP/HTTP to the collector.
func codexOTel(t Telemetry) map[string]any {
	protocol := "binary"
	if strings.HasSuffix(t.Protocol, "json") {
		protocol = "json"
	}
	exporter := func(signal string) map[string]any {
		return map[string]any{"otlp-http": map[string]any{
			"endpoint": strings.TrimRight(t.Endpoint, "/") + "/v1/" + signal,
			"protocol": protocol,
		}}
	}
	out := map[string]any{
		"exporter":         exporter("logs"),
		"metrics_exporter": exporter("metrics"),
		"trace_exporter":   exporter("traces"),
	}
	if env := t.Attributes["deployment.environment.name"]; env != "" {
		out["environment"] = env
	}
	return out
}

// codexRules renders ~/.codex/rules/agents.rules: one prefix_rule per command
// pattern that is a literal prefix, optionally ending in a wildcard.
//
// A deny or ask pattern Codex cannot match (a wildcard before the last word,
// as in "git push * HEAD:main") is skipped with a warning, which would leave
// the command allowed by any allow prefix covering it. So an allow rule is not
// emitted while such a pattern overlaps it (see overlapping), and is warned
// once.
func codexRules(p *config.Permissions, b *builder) string {
	var sb strings.Builder
	sb.WriteString("# Managed by agents from permissions: do not edit.\n")
	seen := map[string]bool{}
	unexpressible := unexpressibleRules(p)
	emit := func(where string, patterns []string, decision, verb string) {
		for _, pat := range patterns {
			words, ok := prefixWords(pat)
			if !ok {
				b.warnf("%s: skipped, %q is not a command prefix Codex rules can match", where, pat)
				continue
			}
			if decision == "allow" && b.allowOverlaps("Codex rules", where, pat, words, unexpressible) {
				continue
			}
			key := decision + "\x00" + strings.Join(words, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			justification := p.Reasons[pat]
			if justification == "" {
				justification = verb + " by the agents permissions."
			}
			quoted := make([]string, len(words))
			for i, w := range words {
				quoted[i] = fmt.Sprintf("%q", w)
			}
			fmt.Fprintf(&sb, "prefix_rule(\n    pattern = [%s],\n    decision = %q,\n    justification = %q,\n    match = [%q],\n)\n",
				strings.Join(quoted, ","), decision, justification, strings.Join(words, " "))
		}
	}
	emit("permissions.allow_commands", p.AllowCommands, "allow", "Allowed")
	emit("permissions.allow_command_patterns", p.AllowCommandPatterns, "allow", "Allowed")
	emit("permissions.deny_command_patterns", p.DenyCommandPatterns, "forbidden", "Denied")
	// Codex matches prefixes only, so an exact line also blocks its extensions.
	emit("permissions.deny_command_exact", p.DenyCommandExact, "forbidden", "Denied")
	emit("permissions.deny_command_prefixes", p.DenyCommandPrefixes, "forbidden", "Denied")
	emit("permissions.ask_command_prefixes", p.AskCommandPrefixes, "prompt", "Confirmation required")
	return sb.String()
}
