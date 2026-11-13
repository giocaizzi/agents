package harness

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

func TestCodexRulesDecisionsAndReasons(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Permissions: config.Permissions{
		AllowCommands:       []string{"git *"},
		DenyCommandExact:    []string{"env"},
		DenyCommandPrefixes: []string{"pip", "python -m pip"},
		AskCommandPrefixes:  []string{"bw"},
		Reasons:             map[string]string{"pip": "use uv", "bw": "secrets"},
	}}
	home := t.TempDir()
	plan := renderOne(t, cfg, config.Codex, home)
	got := planTargetAt(t, plan, filepath.Join(home, ".codex", "rules", "agents.rules")).Managed
	want := `# Managed by agents from permissions: do not edit.
prefix_rule(
    pattern = ["git"],
    decision = "allow",
    justification = "Allowed by the agents permissions.",
    match = ["git"],
)
prefix_rule(
    pattern = ["env"],
    decision = "forbidden",
    justification = "Denied by the agents permissions.",
    match = ["env"],
)
prefix_rule(
    pattern = ["pip"],
    decision = "forbidden",
    justification = "use uv",
    match = ["pip"],
)
prefix_rule(
    pattern = ["python","-m","pip"],
    decision = "forbidden",
    justification = "Denied by the agents permissions.",
    match = ["python -m pip"],
)
prefix_rule(
    pattern = ["bw"],
    decision = "prompt",
    justification = "secrets",
    match = ["bw"],
)
`
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rules (-want +got):\n%s", diff)
	}
}

func TestCodexEffortAndPlugins(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		Models: config.Models{DefaultEffortLevel: "low"},
		Plugins: config.Plugins{
			Registry: "acme/registry",
			Enabled:  map[string][]config.Harness{"on": {config.Codex}, "off": {config.Codex}, "other": {config.Claude}},
			Disabled: []string{"off"},
		},
	}
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	got, _ := planTargetAt(t, renderOne(t, cfg, config.Codex, home), path).Managed.(map[string]any)
	if got["model_reasoning_effort"] != "low" {
		t.Errorf("model_reasoning_effort = %v, want low", got["model_reasoning_effort"])
	}
	want := map[string]any{
		"on@acme-registry":  map[string]any{"enabled": true},
		"off@acme-registry": map[string]any{"enabled": false},
	}
	if diff := cmp.Diff(want, got["plugins"]); diff != "" {
		t.Errorf("plugins (-want +got):\n%s", diff)
	}

	cfg.Harness.Codex.ModelReasoningEffort = "high"
	got, _ = planTargetAt(t, renderOne(t, cfg, config.Codex, home), path).Managed.(map[string]any)
	if got["model_reasoning_effort"] != "high" {
		t.Errorf("typed field: model_reasoning_effort = %v, want high", got["model_reasoning_effort"])
	}
}
