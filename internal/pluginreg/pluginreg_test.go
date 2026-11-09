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

func TestCandidateDirs(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()

	// Create ~/Documents/projects/my-harness and ~/src/my-harness.
	docsProject := filepath.Join(tmpHome, "Documents", "projects", "my-harness")
	srcRepo := filepath.Join(tmpHome, "src", "my-harness")
	managed := filepath.Join(tmpHome, ".local", "share", "example-my-harness")
	if err := os.MkdirAll(docsProject, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcRepo, 0o755); err != nil {
		t.Fatal(err)
	}

	cands := pluginreg.CandidateDirs("", "example/my-harness", tmpHome)
	indexes := make(map[string]int)
	for i, c := range cands {
		indexes[c] = i
	}
	for _, dir := range []string{managed, docsProject, srcRepo} {
		if _, ok := indexes[dir]; !ok {
			t.Errorf("CandidateDirs missing %s; got %v", dir, cands)
		}
	}
	if indexes[managed] > indexes[docsProject] || indexes[managed] > indexes[srcRepo] {
		t.Errorf("CandidateDirs() = %v, managed directory must precede developer checkouts", cands)
	}
}

func TestCandidateDirsOverridesManagedAndImplicitLocations(t *testing.T) {
	tmpHome := t.TempDir()
	explicit := filepath.Join(t.TempDir(), "explicit")
	environment := filepath.Join(t.TempDir(), "environment")
	t.Setenv("HARNESS_DIR", environment)

	cands := pluginreg.CandidateDirs(explicit, "example/my-harness", tmpHome)
	if len(cands) < 3 {
		t.Fatalf("CandidateDirs() = %v, want explicit, environment, and managed candidates", cands)
	}
	if got, want := cands[0], explicit; got != want {
		t.Errorf("CandidateDirs()[0] = %q, want explicit override %q", got, want)
	}
	if got, want := cands[1], environment; got != want {
		t.Errorf("CandidateDirs()[1] = %q, want HARNESS_DIR override %q", got, want)
	}
	if got, want := cands[2], filepath.Join(tmpHome, ".local", "share", "example-my-harness"); got != want {
		t.Errorf("CandidateDirs()[2] = %q, want managed installation %q", got, want)
	}
}

func TestCandidateDirsCurrentWorkingDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("# Agents\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(tmp)

	cands := pluginreg.CandidateDirs("", "example/harness", "")
	var found bool
	for _, c := range cands {
		if c == "." || c == tmp {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected . or %s in candidates, got %v", tmp, cands)
	}
}

func TestDiscoverMarketplaceNamespace(t *testing.T) {
	t.Parallel()

	t.Run("from-marketplace-json", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		claudePluginDir := filepath.Join(dir, ".claude-plugin")
		if err := os.MkdirAll(claudePluginDir, 0o755); err != nil {
			t.Fatal(err)
		}
		marketplaceJSON := `{"name": "octocat-harness", "owner": {"name": "octocat"}}`
		if err := os.WriteFile(filepath.Join(claudePluginDir, "marketplace.json"), []byte(marketplaceJSON), 0o600); err != nil {
			t.Fatal(err)
		}

		ns := pluginreg.DiscoverMarketplaceNamespace(dir)
		if ns != "octocat-harness" {
			t.Errorf("got %q, want octocat-harness", ns)
		}
	})

	t.Run("from-git-config", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		gitDir := filepath.Join(dir, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitConfig := `
[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@github.com:octocat/custom-repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`
		if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(gitConfig), 0o600); err != nil {
			t.Fatal(err)
		}

		ns := pluginreg.DiscoverMarketplaceNamespace(dir)
		if ns != "octocat-custom-repo" {
			t.Errorf("got namespace %q, want octocat-custom-repo", ns)
		}

		reg := pluginreg.DiscoverRegistry(dir)
		if reg != "octocat/custom-repo" {
			t.Errorf("got registry %q, want octocat/custom-repo", reg)
		}
	})

	t.Run("empty-dir", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		ns := pluginreg.DiscoverMarketplaceNamespace(dir)
		if ns != "" {
			t.Errorf("expected empty namespace, got %q", ns)
		}
	})
}
