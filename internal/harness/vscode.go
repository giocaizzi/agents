package harness

import (
	"path/filepath"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// renderVSCode adds the VS Code targets: the user settings (drift only, the
// file belongs to the user's dotfiles) and the ~/.copilot files that VS
// Code's agent chat reads. The copilot harness keeps its copy when both are
// selected.
func renderVSCode(c *config.Config, b *builder) error {
	b.add(apply.Target{
		Owner: string(config.VSCode), Path: vscodeSettingsPath(b), Format: apply.JSONC,
		Managed: vscodeSettings(c, b), CheckOnly: true,
	})
	renderCopilotFiles(c, b, string(config.VSCode))
	return nil
}

// vscodeSettingsPath is VS Code's user settings.json on the target OS.
func vscodeSettingsPath(b *builder) string {
	switch b.env.GOOS {
	case "darwin":
		return b.home("Library", "Application Support", "Code", "User", "settings.json")
	case goosWindows:
		appdata := b.env.Getenv("APPDATA")
		if appdata == "" {
			appdata = b.home("AppData", "Roaming")
		}
		return filepath.Join(appdata, "Code", "User", "settings.json")
	default:
		return b.home(".config", "Code", "User", "settings.json")
	}
}

// vscodeSettings derives the managed keys of VS Code's settings.json: Copilot
// auto-approve lists from the permissions, dictation and theme, then
// harness.vscode.settings merged over them.
func vscodeSettings(c *config.Config, b *builder) map[string]any {
	p := c.Permissions
	commands := map[string]any{}
	for _, list := range []struct {
		items []string
		allow bool
	}{
		{p.AllowCommands, true},
		{p.AllowCommandPatterns, true},
		{p.DenyCommandExact, false},
		{p.DenyCommandPatterns, false},
		{p.AskCommandPrefixes, false},
	} {
		for _, cmd := range list.items {
			commands[cmd] = list.allow
		}
	}
	urls := map[string]any{}
	for _, u := range p.AllowedURLs {
		urls[u] = true
	}
	out := map[string]any{
		"dictation.enabled":               c.Voice.Enabled,
		"chat.tools.terminal.autoApprove": commands,
		"chat.tools.urls.autoApprove":     urls,
		"workbench.colorTheme":            valueOr(c.UI.Theme, "Catppuccin Mocha"),
	}
	if c.Voice.Language != "" {
		out["agents.voice.language"] = c.Voice.Language
	}
	extra := b.expandSettings("harness.vscode.settings", c.Harness.VSCode.Settings)
	return mergeSettings(out, extra)
}
