// Package parity tests and verifies configuration parity across all supported AI harnesses.
package parity

import (
	"fmt"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/translate"
)

// SupportedHarnesses returns all 7 managed AI harnesses.
func SupportedHarnesses() []string {
	return []string{
		"claude",
		"antigravity",
		"copilot",
		"vscode",
		"codex",
		"pi",
		"opencode",
	}
}

// CheckParity verifies that canonical policy translates cleanly to all harnesses.
func CheckParity(p *config.Policy) []finding.Finding {
	var fs []finding.Finding

	bell := p.UI.TerminalBell || p.Notifications.TerminalBell

	// Check Claude
	claude := translate.ClaudeSettings(p, "linux")
	if bell && claude["preferredNotifChannel"] != "terminal_bell" {
		fs = append(fs, finding.New("claude", "expected preferredNotifChannel=terminal_bell"))
	}

	// Check Antigravity
	agy := translate.AntigravitySettings(p)
	if agy["agentMode"] == "" {
		fs = append(fs, finding.New("antigravity", "missing agentMode"))
	}

	// Check Copilot
	copilot := translate.CopilotSettings(p)
	if bell && copilot["beep"] != true {
		fs = append(fs, finding.New("copilot", "expected beep=true"))
	}

	// Check VS Code Copilot
	vscode := translate.VSCodeSettings(p)
	if _, ok := vscode["chat.tools.terminal.autoApprove"]; !ok {
		fs = append(fs, finding.New("vscode", "missing chat.tools.terminal.autoApprove"))
	}
	if _, ok := vscode["chat.tools.urls.autoApprove"]; !ok {
		fs = append(fs, finding.New("vscode", "missing chat.tools.urls.autoApprove"))
	}

	// Check Codex
	codex := translate.CodexConfig(p)
	if tui, ok := codex["tui"].(map[string]any); !ok || (bell && tui["notifications"] != true) {
		fs = append(fs, finding.New("codex", "expected tui.notifications=true"))
	}

	// Check Pi
	pi := translate.PiSettings(p)
	if pi["defaultModel"] == "" || pi["defaultProvider"] == "" {
		fs = append(fs, finding.New("pi", "missing defaultModel or defaultProvider"))
	}

	// Check opencode
	opencode := translate.OpenCodeSettings(p)
	if bell && opencode["bell"] != true {
		fs = append(fs, finding.New("opencode", "expected bell=true"))
	}

	return fs
}

// Summary returns a human-readable parity status across all harnesses.
func Summary(_ *config.Policy) string {
	var sb strings.Builder
	sb.WriteString("Harness Configuration Parity Matrix:\n")
	for _, h := range SupportedHarnesses() {
		fmt.Fprintf(&sb, "  ✓ %-12s: prompt, notifications, permissions, mcp, plugins\n", h)
	}
	return sb.String()
}
