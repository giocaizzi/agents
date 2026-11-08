package translate

import (
	"github.com/giocaizzi/agents/internal/config"
)

// CopilotSettings returns the managed subset of Copilot CLI settings.
func CopilotSettings(p *config.Policy) map[string]any {
	agents := make(map[string]any, len(p.Native.Copilot.Agents))
	for name, a := range p.Native.Copilot.Agents {
		agents[name] = map[string]any{
			"model":       a.Model,
			"effortLevel": a.EffortLevel,
		}
	}

	return map[string]any{
		"beep": p.UI.TerminalBell || p.Notifications.TerminalBell,
		"subagents": map[string]any{
			"agents": agents,
		},
	}
}
