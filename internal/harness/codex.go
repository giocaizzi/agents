package harness

import (
	"fmt"
	"maps"
	"slices"
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
	for k, v := range map[string]string{
		"model":                  h.Model,
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
	mergeSettings(cfg, b.expandSettings("harness.codex.settings", h.Settings))

	b.add(apply.Target{Owner: string(config.Codex), Path: b.home(".codex", "config.toml"), Format: apply.TOML, Managed: cfg, Mode: 0o600})
	b.add(apply.Target{
		Owner: string(config.Codex), Path: b.home(".codex", "rules", "agents.rules"),
		Format: apply.Text, Managed: codexRules(&c.Permissions, b), Mode: 0o644,
	})
	b.instructions(config.Codex, b.home(".codex", "AGENTS.md"))
	return nil
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
				e["http_headers"] = stringMap(s.Headers)
			}
			if o := codexOAuth(s.OAuth); o != nil {
				e["oauth"] = o
			}
		case s.Command != "":
			e["command"] = s.Command
			if len(s.Args) > 0 {
				e["args"] = stringSlice(s.Args)
			}
			if len(s.Env) > 0 {
				e["env"] = stringMap(s.Env)
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
func codexRules(p *config.Permissions, b *builder) string {
	var sb strings.Builder
	sb.WriteString("# Managed by agents from permissions: do not edit.\n")
	seen := map[string]bool{}
	emit := func(where string, patterns []string, decision, justification string) {
		for _, pat := range patterns {
			words, ok := prefixWords(pat)
			if !ok {
				b.warn("%s: skipped, %q is not a command prefix Codex rules can match", where, pat)
				continue
			}
			key := decision + "\x00" + strings.Join(words, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			quoted := make([]string, len(words))
			for i, w := range words {
				quoted[i] = fmt.Sprintf("%q", w)
			}
			fmt.Fprintf(&sb, "prefix_rule(\n    pattern = [%s],\n    decision = %q,\n    justification = %q,\n    match = [%q],\n)\n",
				strings.Join(quoted, ","), decision, justification, strings.Join(words, " "))
		}
	}
	emit("permissions.allow_command_patterns", p.AllowCommandPatterns, "allow", "Allowed by the agents permissions.")
	emit("permissions.deny_command_patterns", p.DenyCommandPatterns, "forbidden", "Denied by the agents permissions.")
	return sb.String()
}

// prefixWords splits a command pattern into words, dropping a trailing
// wildcard word. A wildcard anywhere else cannot be a prefix.
func prefixWords(pat string) ([]string, bool) {
	words := strings.Fields(pat)
	if n := len(words); n > 0 && words[n-1] == "*" {
		words = words[:n-1]
	}
	if len(words) == 0 || slices.ContainsFunc(words, func(w string) bool { return strings.Contains(w, "*") }) {
		return nil, false
	}
	return words, true
}

func stringMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func stringSlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// mergeSettings deep-merges src into dst: tables merge, anything else in src
// replaces.
func mergeSettings(dst, src map[string]any) {
	for _, k := range slices.Sorted(maps.Keys(src)) {
		sm, sIsMap := src[k].(map[string]any)
		dm, dIsMap := dst[k].(map[string]any)
		if sIsMap && dIsMap {
			mergeSettings(dm, sm)
			continue
		}
		dst[k] = src[k]
	}
}

// expandSettings returns native settings with ${VAR} expanded in every
// string; an entry with an unset variable is skipped (warned).
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
	switch x := v.(type) {
	case string:
		return b.expand(where, x)
	case map[string]any:
		return b.expandSettings(where, x), true
	case []any:
		out := make([]any, 0, len(x))
		for i, e := range x {
			if ev, ok := b.expandValue(fmt.Sprintf("%s[%d]", where, i), e); ok {
				out = append(out, ev)
			}
		}
		return out, true
	default:
		return v, true
	}
}
