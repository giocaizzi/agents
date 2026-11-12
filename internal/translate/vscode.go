package translate

import (
	"github.com/giocaizzi/agents/internal/config"
)

// VSCodeSettings translates policy permissions into VS Code Copilot autoApprove settings.
func VSCodeSettings(p *config.Policy) map[string]any {
	terminalAutoApprove := make(map[string]bool)
	for _, cmd := range p.Permissions.AllowCommands {
		terminalAutoApprove[cmd] = true
	}
	for _, cmd := range p.Permissions.AllowCommandPatterns {
		terminalAutoApprove[cmd] = true
	}
	for _, cmd := range p.Permissions.DenyCommandExact {
		terminalAutoApprove[cmd] = false
	}
	for _, cmd := range p.Permissions.DenyCommandPatterns {
		terminalAutoApprove[cmd] = false
	}
	for _, pfx := range p.Permissions.AskCommandPrefixes {
		terminalAutoApprove[pfx] = false
	}

	urlsAutoApprove := make(map[string]bool)
	for _, url := range p.Permissions.AllowedUrls {
		urlsAutoApprove[url] = true
	}

	theme := p.UI.Theme
	if theme == "" {
		theme = "Catppuccin Mocha"
	}

	out := map[string]any{
		"dictation.enabled":               p.Voice.Enabled,
		"chat.tools.terminal.autoApprove": terminalAutoApprove,
		"chat.tools.urls.autoApprove":     urlsAutoApprove,
		"workbench.colorTheme":            theme,
	}
	if p.Voice.Language != "" {
		out["agents.voice.language"] = p.Voice.Language
	}
	return out
}
