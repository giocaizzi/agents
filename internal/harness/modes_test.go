package harness

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

// nested reads a key path of a managed map; nil when any step is missing.
func nested(m any, keys ...string) any {
	cur := m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

// TestSharedModes covers the mapping of autonomy.execution_mode and
// autonomy.sandbox_mode to the native keys of every harness that has one:
// the value written, nothing written, or nothing written with a warning for a
// value with no exact equivalent.
func TestSharedModes(t *testing.T) {
	t.Parallel()
	exec := func(v string) config.Autonomy { return config.Autonomy{ExecutionMode: v} }
	sandbox := func(v string) config.Autonomy { return config.Autonomy{SandboxMode: v} }
	claudeFile := filepath.Join("/h", ".claude", "settings.json")
	copilotFile := filepath.Join("/h", ".copilot", "settings.json")
	vscodeFile := filepath.Join("/h", ".config", "Code", "User", "settings.json")
	codexFile := filepath.Join("/h", ".codex", "config.toml")
	antigravityFile := filepath.Join("/h", ".gemini", "antigravity-cli", "settings.json")

	tests := []struct {
		name     string
		harness  config.Harness
		autonomy config.Autonomy
		file     string
		key      []string
		want     any    // nil: the key is absent
		warn     string // substring of a warning; "" for none
	}{
		{"claude ask", config.Claude, exec("ask"), claudeFile, []string{"permissions", "defaultMode"}, "default", ""},
		{"claude auto", config.Claude, exec("auto"), claudeFile, []string{"permissions", "defaultMode"}, "auto", ""},
		{"claude full", config.Claude, exec("full"), claudeFile, []string{"permissions", "defaultMode"}, "bypassPermissions", ""},
		{"claude unset keeps auto", config.Claude, config.Autonomy{}, claudeFile, []string{"permissions", "defaultMode"}, "auto", ""},
		{"claude sandbox workspace", config.Claude, sandbox("workspace"), claudeFile, []string{"sandbox", "enabled"}, true, ""},
		{"claude sandbox off writes nothing", config.Claude, sandbox("off"), claudeFile, []string{"sandbox", "enabled"}, nil, ""},
		{"claude sandbox read-only maps to on with a note", config.Claude, sandbox("read-only"), claudeFile, []string{"sandbox", "enabled"}, true, "note: autonomy.sandbox_mode = \"read-only\": Claude Code"},

		{"copilot ask", config.Copilot, exec("ask"), copilotFile, []string{"defaultPermissionMode"}, "manual", ""},
		{"copilot auto", config.Copilot, exec("auto"), copilotFile, []string{"defaultPermissionMode"}, "assisted", ""},
		{"copilot full", config.Copilot, exec("full"), copilotFile, []string{"defaultPermissionMode"}, "allow-all", ""},
		{"copilot unset", config.Copilot, config.Autonomy{}, copilotFile, []string{"defaultPermissionMode"}, nil, ""},
		{"copilot sandbox workspace", config.Copilot, sandbox("workspace"), copilotFile, []string{"sandbox", "enabled"}, true, ""},
		{"copilot sandbox off writes nothing", config.Copilot, sandbox("off"), copilotFile, []string{"sandbox", "enabled"}, nil, ""},
		{"copilot sandbox read-only maps to on with a note", config.Copilot, sandbox("read-only"), copilotFile, []string{"sandbox", "enabled"}, true, "note: autonomy.sandbox_mode = \"read-only\": Copilot CLI"},

		{"vscode ask", config.VSCode, exec("ask"), vscodeFile, []string{"chat.permissions.default"}, "default", ""},
		{"vscode auto warns", config.VSCode, exec("auto"), vscodeFile, []string{"chat.permissions.default"}, nil, "VS Code has no exact equivalent"},
		{"vscode full", config.VSCode, exec("full"), vscodeFile, []string{"chat.permissions.default"}, "autoApprove", ""},
		{"vscode sandbox workspace", config.VSCode, sandbox("workspace"), vscodeFile, []string{"chat.agent.sandbox.enabled"}, "on", ""},
		{"vscode sandbox off writes nothing", config.VSCode, sandbox("off"), vscodeFile, []string{"chat.agent.sandbox.enabled"}, nil, ""},
		{"vscode sandbox read-only maps to on with a note", config.VSCode, sandbox("read-only"), vscodeFile, []string{"chat.agent.sandbox.enabled"}, "on", "note: autonomy.sandbox_mode = \"read-only\": VS Code"},

		{"codex ask warns", config.Codex, exec("ask"), codexFile, []string{"approval_policy"}, nil, "Codex has no exact equivalent"},
		{"codex auto", config.Codex, exec("auto"), codexFile, []string{"approval_policy"}, "on-request", ""},
		{"codex full", config.Codex, exec("full"), codexFile, []string{"approval_policy"}, "never", ""},
		{"codex sandbox off", config.Codex, sandbox("off"), codexFile, []string{"sandbox_mode"}, "danger-full-access", ""},
		{"codex sandbox workspace", config.Codex, sandbox("workspace"), codexFile, []string{"sandbox_mode"}, "workspace-write", ""},
		{"codex sandbox read-only", config.Codex, sandbox("read-only"), codexFile, []string{"sandbox_mode"}, "read-only", ""},

		{"antigravity ask", config.Antigravity, exec("ask"), antigravityFile, []string{"agentMode"}, "default", ""},
		{"antigravity auto", config.Antigravity, exec("auto"), antigravityFile, []string{"agentMode"}, "auto", ""},
		{"antigravity auto leaves toolPermission", config.Antigravity, exec("auto"), antigravityFile, []string{"toolPermission"}, nil, ""},
		{"antigravity full mode", config.Antigravity, exec("full"), antigravityFile, []string{"agentMode"}, "auto", ""},
		{"antigravity full tool permission", config.Antigravity, exec("full"), antigravityFile, []string{"toolPermission"}, "always-proceed", ""},
		{"antigravity sandbox workspace", config.Antigravity, sandbox("workspace"), antigravityFile, []string{"enableTerminalSandbox"}, true, ""},
		{"antigravity sandbox off is written false", config.Antigravity, sandbox("off"), antigravityFile, []string{"enableTerminalSandbox"}, false, ""},
		{"antigravity sandbox read-only maps to on with a note", config.Antigravity, sandbox("read-only"), antigravityFile, []string{"enableTerminalSandbox"}, true, "note: autonomy.sandbox_mode = \"read-only\": Antigravity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := renderFor(t, config.Config{Autonomy: tt.autonomy}, "linux", tt.harness)
			got := nested(findTarget(t, plan, tt.file).Managed, tt.key...)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", strings.Join(tt.key, "."), diff)
			}
			if note, isNote := strings.CutPrefix(tt.warn, "note: "); isNote {
				// A note is informational: in Notes, never in Warnings.
				if !slices.ContainsFunc(plan.Notes, func(n string) bool { return strings.Contains(n, note) }) {
					t.Errorf("notes = %q, want one containing %q", plan.Notes, note)
				}
				if slices.ContainsFunc(plan.Warnings, func(w string) bool { return strings.Contains(w, note) }) {
					t.Errorf("warnings = %q, the note must not be a warning", plan.Warnings)
				}
				return
			}
			switch {
			case tt.warn == "" && len(plan.Warnings) != 0:
				t.Errorf("unexpected warnings: %q", plan.Warnings)
			case tt.warn != "" && !slices.ContainsFunc(plan.Warnings, func(w string) bool { return strings.Contains(w, tt.warn) }):
				// VS Code also writes the shared ~/.copilot files, so it may warn for Copilot CLI too.
				t.Errorf("warnings = %q, want one containing %q", plan.Warnings, tt.warn)
			}
		})
	}
}

// TestTypedFieldsWinOverSharedModes pins that the harness's own typed field
// beats the shared value, as everywhere else.
func TestTypedFieldsWinOverSharedModes(t *testing.T) {
	t.Parallel()
	shared := config.Autonomy{ExecutionMode: "full", SandboxMode: "workspace"}
	var c config.Config
	c.Autonomy = shared
	c.Harness.Claude.DefaultMode = "acceptEdits"
	c.Harness.Codex.ApprovalPolicy = "on-request"
	c.Harness.Codex.SandboxMode = "read-only"
	c.Harness.Antigravity.AgentMode = "plan"
	c.Harness.Antigravity.ToolPermission = "strict"

	claude := renderFor(t, c, "linux", config.Claude)
	if got := nested(findTarget(t, claude, filepath.Join("/h", ".claude", "settings.json")).Managed, "permissions", "defaultMode"); got != "acceptEdits" {
		t.Errorf("claude defaultMode = %v, want the typed acceptEdits", got)
	}
	codex := renderFor(t, c, "linux", config.Codex)
	cfg := findTarget(t, codex, filepath.Join("/h", ".codex", "config.toml")).Managed
	if nested(cfg, "approval_policy") != "on-request" || nested(cfg, "sandbox_mode") != "read-only" {
		t.Errorf("codex = %v, want the typed on-request and read-only", cfg)
	}
	ag := renderFor(t, c, "linux", config.Antigravity)
	s := findTarget(t, ag, filepath.Join("/h", ".gemini", "antigravity-cli", "settings.json")).Managed
	if nested(s, "agentMode") != "plan" || nested(s, "toolPermission") != "strict" {
		t.Errorf("antigravity = %v, want the typed plan and strict", s)
	}
}

// TestClaudeSandboxKeepsAsk pins that turning Claude's sandbox on does not
// loosen execution_mode = "ask": sandbox.autoAllowBashIfSandboxed defaults to
// true (https://code.claude.com/docs/en/sandboxing), which runs sandboxed Bash
// commands without a prompt.
func TestClaudeSandboxKeepsAsk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		exec string
		sbx  string
		want any // nil: the key is absent
	}{
		{"ask", "ask", "workspace", false},
		{"unset", "", "workspace", false},
		{"auto", "auto", "workspace", nil},
		{"full", "full", "workspace", nil},
		{"sandbox off", "ask", "off", nil},
		{"read-only", "ask", "read-only", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := config.Config{Autonomy: config.Autonomy{ExecutionMode: tt.exec, SandboxMode: tt.sbx}}
			plan := renderFor(t, c, "linux", config.Claude)
			s := findTarget(t, plan, filepath.Join("/h", ".claude", "settings.json")).Managed
			if got := nested(s, "sandbox", "autoAllowBashIfSandboxed"); got != tt.want {
				t.Errorf("sandbox.autoAllowBashIfSandboxed = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAntigravitySandboxAndToolPermission pins the typed-field precedence:
// a typed enable_terminal_sandbox (true or false) beats the shared sandbox
// mode, and execution_mode = full sets toolPermission only when agentMode
// comes from the shared value.
func TestAntigravitySandboxAndToolPermission(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	file := filepath.Join("/h", ".gemini", "antigravity-cli", "settings.json")
	tests := []struct {
		name     string
		autonomy config.Autonomy
		typed    config.AntigravitySettings
		key      string
		want     any
	}{
		{"sandbox unset follows workspace", config.Autonomy{SandboxMode: "workspace"}, config.AntigravitySettings{}, "enableTerminalSandbox", true},
		{"sandbox typed true over off", config.Autonomy{SandboxMode: "off"}, config.AntigravitySettings{EnableTerminalSandbox: &yes}, "enableTerminalSandbox", true},
		{"sandbox typed false over workspace", config.Autonomy{SandboxMode: "workspace"}, config.AntigravitySettings{EnableTerminalSandbox: &no}, "enableTerminalSandbox", false},
		{"full sets toolPermission", config.Autonomy{ExecutionMode: "full"}, config.AntigravitySettings{}, "toolPermission", "always-proceed"},
		{"full with typed agent_mode skips toolPermission", config.Autonomy{ExecutionMode: "full"}, config.AntigravitySettings{AgentMode: "auto"}, "toolPermission", nil},
		{"typed tool_permission wins over full", config.Autonomy{ExecutionMode: "full"}, config.AntigravitySettings{ToolPermission: "strict"}, "toolPermission", "strict"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := config.Config{Autonomy: tt.autonomy}
			c.Harness.Antigravity = tt.typed
			plan := renderFor(t, c, "linux", config.Antigravity)
			got := nested(findTarget(t, plan, file).Managed, tt.key)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tt.key, diff)
			}
		})
	}
}

// TestClaudeSandboxFollowsEffectiveMode pins that autoAllowBashIfSandboxed
// follows the defaultMode Claude really gets: a typed default_mode that asks
// beats a shared execution_mode of full.
func TestClaudeSandboxFollowsEffectiveMode(t *testing.T) {
	t.Parallel()
	file := filepath.Join("/h", ".claude", "settings.json")
	tests := []struct {
		name      string
		execution string
		typed     string
		wantMode  string
		wantAuto  any // nil: the key is absent
	}{
		{"typed default beats shared full", "full", "default", "default", false},
		{"typed acceptEdits beats shared auto", "auto", "acceptEdits", "acceptEdits", false},
		{"shared full", "full", "", "bypassPermissions", nil},
		{"shared auto", "auto", "", "auto", nil},
		{"typed auto beats shared ask", "ask", "auto", "auto", nil},
		{"shared ask", "ask", "", "default", false},
		{"unset", "", "", "auto", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var c config.Config
			c.Autonomy = config.Autonomy{ExecutionMode: tt.execution, SandboxMode: "workspace"}
			c.Harness.Claude.DefaultMode = tt.typed
			managed := findTarget(t, renderFor(t, c, "linux", config.Claude), file).Managed
			if diff := cmp.Diff(tt.wantMode, nested(managed, "permissions", "defaultMode")); diff != "" {
				t.Errorf("permissions.defaultMode (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantAuto, nested(managed, "sandbox", "autoAllowBashIfSandboxed")); diff != "" {
				t.Errorf("sandbox.autoAllowBashIfSandboxed (-want +got):\n%s", diff)
			}
		})
	}
}
