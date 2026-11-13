package feature

import "strings"

// Files agents writes, as the table names them. "~" is the home directory.
const (
	claudeSettings   = "~/.claude/settings.json"
	claudeUser       = "~/.claude.json"
	claudeMarketMD   = "<data dir>/claude-plugins/.claude-plugin/marketplace.json"
	claudePluginJSON = "<data dir>/claude-plugins/lsp/.claude-plugin/plugin.json"
	claudeLSPJSON    = "<data dir>/claude-plugins/lsp/.lsp.json"

	copilotSettings = "~/.copilot/settings.json"
	copilotLSP      = "~/.copilot/lsp-config.json"
	copilotMCP      = "~/.copilot/mcp-config.json"

	vscodeSettings = "<Code user dir>/settings.json"

	codexConfig = "~/.codex/config.toml"
	codexRules  = "~/.codex/rules/agents.rules"

	opencodeConfig = "~/.config/opencode/opencode.jsonc"

	piSettings = "~/.pi/agent/settings.json"
	piMCP      = "~/.pi/agent/mcp.json"

	antigravitySettings = "~/.gemini/antigravity-cli/settings.json"
	antigravityMCP      = "~/.gemini/config/mcp_config.json"

	anyKey    = "(any key)"
	mcpServer = "mcpServers"
	perms     = "permissions"
)

// instructionFile is the instruction file of each harness.
var instructionFile = map[string]string{ //nolint:gochecknoglobals // constant lookup
	claude:      "~/.claude/CLAUDE.md",
	copilot:     "~/.copilot/copilot-instructions.md",
	vscode:      "~/.copilot/copilot-instructions.md",
	codex:       "~/.codex/AGENTS.md",
	opencode:    "~/.config/opencode/AGENTS.md",
	pi:          "~/.pi/agent/AGENTS.md",
	antigravity: "~/.gemini/config/AGENTS.md",
}

// settingsFile is the main settings file of each harness.
var settingsFile = map[string]string{ //nolint:gochecknoglobals // constant lookup
	claude:      claudeSettings,
	copilot:     copilotSettings,
	vscode:      vscodeSettings,
	codex:       codexConfig,
	opencode:    opencodeConfig,
	pi:          piSettings,
	antigravity: antigravitySettings,
}

// pair is one cell of a feature under construction.
type pair struct {
	h string
	c Cell
}

// rd is a rendered cell.
func rd(h, target string) pair { return pair{h, Cell{Status: StatusRendered, Target: target}} }

// pl is a planned cell: the harness has the setting, agents does not write it.
func pl(h, what string) pair { return pair{h, Cell{Status: StatusPlanned, Evidence: reason(h, what)}} }

// un is an unsupported cell: the harness has no such setting.
func un(h, what string) pair { return pair{h, Cell{Status: StatusUnsupported, Evidence: reason(h, what)}} }

// reason builds the evidence of a non-rendered cell. A finding that could not
// be decided starts with "ambiguous:" and stands alone.
func reason(h, what string) string {
	if strings.HasPrefix(what, "ambiguous:") {
		return what
	}
	return evidence(h, what)
}

// at names the keys written in file; with no key it names the whole file.
func at(file string, keys ...string) string {
	if len(keys) == 0 {
		return file
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = file + ": " + k
	}
	return strings.Join(parts, "; ")
}

// all joins several targets of one cell.
func all(targets ...string) string { return strings.Join(targets, "; ") }

// internal is a key no harness sees.
func internal(key, summary string) Feature {
	return Feature{Key: key, Scope: ScopeInternal, Summary: summary}
}

// shared is a key every harness has a cell for. The harnesses ps do not name
// get an unsupported cell that cites rest.
func shared(key, summary, rest string, ps ...pair) Feature {
	cells := make(map[string]Cell, len(HarnessIDs()))
	for _, p := range ps {
		cells[p.h] = p.c
	}
	for _, h := range HarnessIDs() {
		if _, ok := cells[h]; !ok {
			cells[h] = un(h, rest).c
		}
	}
	return Feature{Key: key, Scope: ScopeShared, Summary: summary, Cells: cells}
}

// own is a key of one harness's table.
func own(key, summary string, p pair) Feature {
	return Feature{Key: key, Scope: ScopeHarness, Summary: summary, Cells: map[string]Cell{p.h: p.c}}
}
