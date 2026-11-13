package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

func tierConfig() config.Config {
	return config.Config{Models: config.Models{
		Default: "main",
		Tiers: map[string]config.Tier{
			"main": {
				Claude: "opus", Copilot: "gpt-big", Codex: "gpt-x", OpenCode: "acme/big", Pi: "pi-big",
				Effort: "high", Efforts: map[string]string{"codex": "xhigh"},
			},
			"cheap": {Claude: "haiku", Copilot: "gpt-mini", OpenCode: "acme/mini", Effort: "low"},
			"bare":  {Claude: "sonnet"},
		},
		Assign: map[string]string{
			"dev-tools:scout": "cheap",
			"*:reviewer":      "main",
			"dev-*":           "bare",
			"dev-tools:*":     "cheap",
		},
	}}
}

func TestResolveAssign(t *testing.T) {
	t.Parallel()
	c := tierConfig()
	agents := []string{"dev-tools:scout", "dev-tools:reviewer", "dev-tools:worker", "foundations:reviewer", "other:x"}
	got := ResolveAssign(&c, agents)
	// Sorted glob order: "*:reviewer" < "dev-*" < "dev-tools:*".
	want := map[string]string{
		"dev-tools:scout":      "cheap", // the exact key wins over "dev-*"
		"dev-tools:reviewer":   "main",
		"dev-tools:worker":     "bare",
		"foundations:reviewer": "main",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ResolveAssign (-want +got):\n%s", diff)
	}
	// An exact key is kept without the agent being installed.
	if got := ResolveAssign(&c, nil); got["dev-tools:scout"] != "cheap" || len(got) != 1 {
		t.Errorf("exact keys without agents = %v", got)
	}
}

func TestInstalledAgents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, f := range []string{"plugins/b/agents/y.md", "plugins/a/agents/x.md", "plugins/a/agents/x.yaml", "plugins/c/skills/s.md"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := InstalledAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"a:x", "b:y"}, got); diff != "" {
		t.Errorf("agents (-want +got):\n%s", diff)
	}
	if _, err := InstalledAgents(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing checkout: want an error")
	}
}

func renderTiers(t *testing.T, c config.Config, h config.Harness, agents []string) (home string, plan Plan) {
	t.Helper()
	home = t.TempDir()
	plan, err := Render(&config.Resolved{Config: c, Profile: "p", Harnesses: []config.Harness{h}}, Env{
		Home: home, Hostname: "host", Arch: "arm64", GOOS: "linux", Agents: agents,
		Getenv:   func(string) string { return "" },
		LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return home, plan
}

func TestTierSessionAndAgents(t *testing.T) {
	t.Parallel()
	agents := []string{"dev-tools:scout", "dev-tools:reviewer", "foundations:reviewer", "other:x"}
	c := tierConfig()
	delete(c.Models.Assign, "dev-*")
	delete(c.Models.Assign, "dev-tools:*")

	tests := []struct {
		name string
		h    config.Harness
		path []string
		key  []string // path inside the managed map
		want any
	}{
		{"claude model", config.Claude, []string{".claude", "settings.json"}, []string{"model"}, "opus"},
		{"claude effort", config.Claude, []string{".claude", "settings.json"}, []string{"effortLevel"}, "high"},
		{"copilot model", config.Copilot, []string{".copilot", "settings.json"}, []string{"model"}, "gpt-big"},
		{
			"copilot agents", config.Copilot,
			[]string{".copilot", "settings.json"},
			[]string{"subagents", "agents"},
			map[string]any{
				"dev-tools:scout":      map[string]any{"model": "gpt-mini", "effortLevel": "low"},
				"dev-tools:reviewer":   map[string]any{"model": "gpt-big", "effortLevel": "high"},
				"foundations:reviewer": map[string]any{"model": "gpt-big", "effortLevel": "high"},
			},
		},
		{"codex model", config.Codex, []string{".codex", "config.toml"}, []string{"model"}, "gpt-x"},
		{"codex effort", config.Codex, []string{".codex", "config.toml"}, []string{"model_reasoning_effort"}, "xhigh"},
		{"pi model", config.Pi, []string{".pi", "agent", "settings.json"}, []string{"defaultModel"}, "pi-big"},
		{"pi thinking", config.Pi, []string{".pi", "agent", "settings.json"}, []string{"defaultThinkingLevel"}, "high"},
		{"opencode model", config.OpenCode, []string{".config", "opencode", "opencode.jsonc"}, []string{"model"}, "acme/big"},
		{
			"opencode agents", config.OpenCode,
			[]string{".config", "opencode", "opencode.jsonc"},
			[]string{"agents"},
			map[string]any{
				"scout":    map[string]any{"model": "acme/mini"},
				"reviewer": map[string]any{"model": "acme/big"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home, plan := renderTiers(t, c, tt.h, agents)
			got := planTargetAt(t, plan, filepath.Join(append([]string{home}, tt.path...)...)).Managed
			for _, k := range tt.key {
				m, ok := got.(map[string]any)
				if !ok {
					t.Fatalf("%v: %T is not a map", tt.key, got)
				}
				got = m[k]
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%v (-want +got):\n%s", tt.key, diff)
			}
		})
	}
}

func TestTierNoWrite(t *testing.T) {
	t.Parallel()
	c := tierConfig()
	// A typed field wins over the default tier.
	c.Harness.Codex.Model = "typed"
	c.Harness.Pi.DefaultModel = "typed-pi"
	c.Harness.OpenCode.DefaultModel = "acme/typed"
	// No default tier: no session model anywhere.
	none := tierConfig()
	none.Models.Default = ""
	// A tier without a model for the harness keeps the plugin default.
	bare := tierConfig()
	bare.Models.Default = "bare"

	tests := []struct {
		name string
		c    config.Config
		h    config.Harness
		path []string
		key  string
		want any
	}{
		{"typed codex", c, config.Codex, []string{".codex", "config.toml"}, "model", "typed"},
		{"typed pi", c, config.Pi, []string{".pi", "agent", "settings.json"}, "defaultModel", "typed-pi"},
		{"typed opencode", c, config.OpenCode, []string{".config", "opencode", "opencode.jsonc"}, "model", nil},
		{"no default claude", none, config.Claude, []string{".claude", "settings.json"}, "model", nil},
		{"no default copilot", none, config.Copilot, []string{".copilot", "settings.json"}, "model", nil},
		{"tier without codex model", bare, config.Codex, []string{".codex", "config.toml"}, "model", nil},
		{"tier without pi model", bare, config.Pi, []string{".pi", "agent", "settings.json"}, "defaultModel", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home, plan := renderTiers(t, tt.c, tt.h, nil)
			managed, ok := planTargetAt(t, plan, filepath.Join(append([]string{home}, tt.path...)...)).Managed.(map[string]any)
			if !ok {
				t.Fatal("managed is not a map")
			}
			got := managed[tt.key]
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tt.key, diff)
			}
		})
	}
}

func TestSupportTable(t *testing.T) {
	t.Parallel()
	for _, h := range config.All() {
		perAgent := h == config.Copilot || h == config.OpenCode
		session := h != config.VSCode && h != config.Antigravity
		if SupportsPerAgent(h) != perAgent || SupportsSession(h) != session {
			t.Errorf("%s: per-agent %v session %v", h, SupportsPerAgent(h), SupportsSession(h))
		}
	}
}
