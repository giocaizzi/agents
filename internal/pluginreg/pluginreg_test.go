package pluginreg_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/pluginreg"
)

func TestCheck(t *testing.T) {
	t.Parallel()
	choices := []string{"claude", "codex"}
	plugins := map[string][]string{"api": {"claude"}, "go": {"claude", "codex"}}
	tests := []struct {
		name     string
		plugins  map[string][]string
		personal []string
		upstream []string
		want     string
	}{
		{"in step", plugins, []string{"go"}, []string{"api", "go"}, ""},
		{"missing here", plugins, nil, []string{"api", "go", "hud"}, "hud: plugin of example/harness missing here"},
		{"gone upstream", plugins, nil, []string{"api"}, "go: not a plugin of example/harness"},
		{"unknown personal", plugins, []string{"nope"}, []string{"api", "go"}, "nope is not in [harnessPlugins]"},
		{"unknown harness", map[string][]string{"api": {"emacs"}}, nil, []string{"api"}, "unknown harness emacs"},
		{"nothing upstream", plugins, nil, nil, "no plugin directories found upstream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := finding.Lines(pluginreg.Check(tt.plugins, tt.personal, choices, tt.upstream))
			if tt.want == "" && got != "" || !strings.Contains(got, tt.want) {
				t.Errorf("Check() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractHarnesses(t *testing.T) {
	t.Parallel()
	tmpl := []byte(`
{{- $all := list "claude" "copilot" "vscode" "codex" -}}
`)
	got, err := pluginreg.ExtractHarnesses(tmpl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 || got[0] != "claude" || got[3] != "codex" {
		t.Errorf("ExtractHarnesses() = %v, want 4 harnesses", got)
	}

	if _, err := pluginreg.ExtractHarnesses([]byte("invalid")); err == nil {
		t.Error("expected error for invalid tmpl, got nil")
	}
}

func TestValidateFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create fake upstream harness repo
	harnessDir := filepath.Join(dir, "harness")
	pDir := filepath.Join(harnessDir, "plugins")
	if err := os.MkdirAll(filepath.Join(pDir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pDir, "guard"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create harness.toml
	harnessToml := filepath.Join(dir, "harness.toml")
	content := `
harnessPersonalPlugins = ["api"]

[harnessPlugins]
api = ["claude", "pi"]
guard = ["claude"]
`
	if err := os.WriteFile(harnessToml, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := pluginreg.ValidateFile(context.Background(), harnessToml, harnessDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) > 0 {
		t.Errorf("expected 0 findings, got %v", findings)
	}
}
