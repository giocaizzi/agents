package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/finding"
)

const tiersConfig = `
harnesses = ["claude", "copilot", "codex"]

[plugins]
registry = "acme/harness"

[models]
default = "main"

[models.tiers.main]
claude = "opus"
copilot = "gpt-big"

[models.tiers.cheap]
claude = "haiku"
copilot = "gpt-mini"
codex = "gpt-mini-x"

[models.assign]
"dev-tools:scout" = "cheap"
"*:reviewer" = "main"
"ghost:none" = "cheap"
`

// tiersDeps writes the config and a registry checkout with three agents, and
// returns Deps reading them plus the config file path.
func tiersDeps(t *testing.T, withCheckout bool) (d Deps, out, errOut *bytes.Buffer, file string) {
	t.Helper()
	root := t.TempDir()
	file = filepath.Join(root, "config.toml")
	if err := os.WriteFile(file, []byte(tiersConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if withCheckout {
		for _, f := range []string{"dev-tools/agents/scout.md", "dev-tools/agents/reviewer.md", "dev-tools/agents/worker.md"} {
			p := filepath.Join(root, "data", "agents", "registries", "acme", "harness", "plugins", filepath.FromSlash(f))
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	env := map[string]string{"HOME": root, "XDG_DATA_HOME": filepath.Join(root, "data"), "XDG_CONFIG_HOME": filepath.Join(root, "config")}
	out, errOut = &bytes.Buffer{}, &bytes.Buffer{}
	d = Deps{Stdout: out, Stderr: errOut, Stdin: strings.NewReader(""), GOOS: "linux", Getenv: func(k string) string { return env[k] }}
	return d, out, errOut, file
}

func TestModelsTiersJSON(t *testing.T) {
	t.Parallel()
	d, out, errOut, file := tiersDeps(t, true)
	if code := d.Run(context.Background(), []string{"models", "tiers", "-file", file, "-json"}); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got tiersReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := tiersReport{
		Harnesses: []string{"claude", "copilot", "codex"},
		Default:   "main",
		Tiers: []tierRow{
			{Name: "cheap", Models: map[string]string{"claude": "haiku", "copilot": "gpt-mini", "codex": "gpt-mini-x"}},
			{Name: "main", Models: map[string]string{"claude": "opus", "copilot": "gpt-big", "codex": "-"}},
		},
		Agents: []agentRow{
			{Agent: "(session)", Tier: "main", Effects: map[string]agentModel{
				"claude": {Model: "opus", Mode: modeSession}, "copilot": {Model: "gpt-big", Mode: modeSession},
				"codex": {Model: "-", Mode: modePluginDefault},
			}},
			{Agent: "dev-tools:reviewer", Tier: "main", Effects: map[string]agentModel{
				"claude": {Model: "-", Mode: modeUnsupported}, "copilot": {Model: "gpt-big", Mode: modeAssigned},
				"codex": {Model: "-", Mode: modePluginDefault},
			}},
			{Agent: "dev-tools:scout", Tier: "cheap", Effects: map[string]agentModel{
				"claude": {Model: "-", Mode: modeUnsupported}, "copilot": {Model: "gpt-mini", Mode: modeAssigned},
				"codex": {Model: "-", Mode: modeUnsupported},
			}},
			{Agent: "ghost:none", Tier: "cheap", Effects: map[string]agentModel{
				"claude": {Model: "-", Mode: modeUnsupported}, "copilot": {Model: "gpt-mini", Mode: modeAssigned},
				"codex": {Model: "-", Mode: modeUnsupported},
			}},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("models tiers -json (-want +got):\n%s", diff)
	}
}

func TestModelsTiersText(t *testing.T) {
	t.Parallel()
	d, out, errOut, file := tiersDeps(t, true)
	if code := d.Run(context.Background(), []string{"models", "tiers", "-file", file}); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"TIER", "opus (session model)", "dev-tools:scout", "unsupported", "plugin default", "gpt-mini"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestModelFindings(t *testing.T) {
	t.Parallel()
	t.Run("checkout", func(t *testing.T) {
		t.Parallel()
		d, _, _, file := tiersDeps(t, true)
		r, err := d.loadConfig(&configFlags{file: file})
		if err != nil {
			t.Fatal(err)
		}
		got := d.modelFindings(r)
		want := []finding.Finding{
			finding.Warn("models.assign.ghost:none", "matches no installed agent"),
			finding.Note("models.assign", "dev-tools:reviewer: claude keeps its plugin default (no per-agent setting)"),
			finding.Note("models.assign", "dev-tools:scout: claude keeps its plugin default (no per-agent setting)"),
			finding.Note("models.assign", "dev-tools:scout: codex keeps its plugin default (no per-agent setting)"),
			finding.Note("models.assign", "ghost:none: claude keeps its plugin default (no per-agent setting)"),
			finding.Note("models.assign", "ghost:none: codex keeps its plugin default (no per-agent setting)"),
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("findings (-want +got):\n%s", diff)
		}
	})
	t.Run("missing checkout", func(t *testing.T) {
		t.Parallel()
		d, _, _, file := tiersDeps(t, false)
		r, err := d.loadConfig(&configFlags{file: file})
		if err != nil {
			t.Fatal(err)
		}
		warnings := 0
		for _, f := range d.modelFindings(r) {
			if f.Severity == finding.Warning {
				warnings++
			}
		}
		if warnings != 1 {
			t.Errorf("warnings = %d, want the single checkout warning", warnings)
		}
	})
}
