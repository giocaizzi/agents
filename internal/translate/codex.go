package translate

import (
	"github.com/giocaizzi/agents/internal/policy"
)

// CodexConfig returns the managed subset of Codex configuration as a map.
func CodexConfig(p *policy.Policy) map[string]any {
	bell := p.UI.TerminalBell || p.Notifications.TerminalBell
	cfg := map[string]any{
		"tui": map[string]any{
			"notifications":       bell,
			"notification_method": "bel",
		},
	}

	if p.Native.Codex.Model != nil {
		cfg["model"] = *p.Native.Codex.Model
	}
	if p.Native.Codex.ModelReasoningEffort != nil {
		cfg["model_reasoning_effort"] = *p.Native.Codex.ModelReasoningEffort
	}
	if p.Native.Codex.ApprovalPolicy != nil {
		cfg["approval_policy"] = *p.Native.Codex.ApprovalPolicy
	}
	if p.Native.Codex.SandboxMode != nil {
		cfg["sandbox_mode"] = *p.Native.Codex.SandboxMode
	}

	return cfg
}
