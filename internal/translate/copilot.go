package translate

import (
	"github.com/giocaizzi/agents/internal/config"
)

// CopilotSettings returns the managed subset of Copilot CLI settings. Voice
// input honors only voice.enabled; the other voice keys stay user-managed.
func CopilotSettings(p *config.Policy) map[string]any {
	agents := make(map[string]any, len(p.Native.Copilot.Agents))
	for name, a := range p.Native.Copilot.Agents {
		agents[name] = map[string]any{
			"model":       a.Model,
			"effortLevel": a.EffortLevel,
		}
	}

	return map[string]any{
		"beep":  p.UI.TerminalBell || p.Notifications.TerminalBell,
		"voice": map[string]any{"enabled": p.Voice.Enabled},
		"subagents": map[string]any{
			"agents": agents,
		},
	}
}
