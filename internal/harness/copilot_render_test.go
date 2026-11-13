package harness

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

// TestCopilotSessionKeys: effort, reasoning display and subagent depth land in
// settings.json; the depth sits next to the per-agent models.
func TestCopilotSessionKeys(t *testing.T) {
	t.Parallel()
	var c config.Config
	c.Models = config.Models{DefaultEffortLevel: "high", ShowThinkingSummaries: true}
	c.Autonomy.SubagentDepth = 3
	plan := renderFor(t, c, "linux", config.Copilot)
	got, _ := findTarget(t, plan, filepath.Join("/h", ".copilot", "settings.json")).Managed.(map[string]any)
	if got["effortLevel"] != "high" || got["showReasoning"] != true {
		t.Errorf("effort/reasoning = %v, %v", got["effortLevel"], got["showReasoning"])
	}
	want := map[string]any{"maxDepth": int64(3)}
	if diff := cmp.Diff(want, got["subagents"]); diff != "" {
		t.Errorf("subagents (-want +got):\n%s", diff)
	}
}

func TestVSCodeAutonomyContextTelemetry(t *testing.T) {
	t.Parallel()
	var c config.Config
	c.Permissions.DenyCommandPrefixes = []string{"sudo"}
	c.Autonomy.AllowSubagentInvocations = true
	c.Context.AutoCompact = true
	c.Telemetry = config.Telemetry{Enabled: true, Endpoint: "http://localhost:4318"}
	plan := renderFor(t, c, "linux", config.VSCode)
	got, _ := findTarget(t, plan, filepath.Join("/h", ".config", "Code", "User", "settings.json")).Managed.(map[string]any)
	want := map[string]any{
		"chat.subagents.allowInvocationsFromSubagents":                  true,
		"github.copilot.chat.summarizeAgentConversationHistory.enabled": true,
		"github.copilot.chat.otel.enabled":                              true,
		"github.copilot.chat.otel.otlpEndpoint":                         "http://localhost:4318",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if cmds, _ := got["chat.tools.terminal.autoApprove"].(map[string]any); cmds["sudo"] != false {
		t.Errorf("autoApprove = %v, want sudo false", cmds)
	}
}
