package translate

import (
	"github.com/giocaizzi/agents/internal/config"
)

// OpenCodeSettings returns the managed subset of opencode settings.
func OpenCodeSettings(p *config.Policy) map[string]any {
	cfg := map[string]any{
		"bell": p.UI.TerminalBell || p.Notifications.TerminalBell,
	}
	if p.Native.OpenCode.DefaultModel != "" {
		cfg["model"] = p.Native.OpenCode.DefaultModel
	}
	if p.Native.OpenCode.Sandbox {
		cfg["sandbox"] = true
	}
	return cfg
}
