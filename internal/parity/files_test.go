package parity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/parity"
	"github.com/giocaizzi/agents/internal/policy"
)

const goodClaude = `{"voice": {"enabled": {{ .harnessPolicy.voice.enabled | default false | toJson }}, "mode": {{ .harnessPolicy.voice.mode | quote }}, "autoSubmit": {{ .harnessPolicy.voice.autoSubmit | default false | toJson }}}}`

const goodCopilot = `{"voice": {"enabled": {{ .harnessPolicy.voice.enabled | default false | toJson }}}}`

func writeSource(t *testing.T, vscode, claude string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"dot_config/Code/User/settings.json":      vscode,
		".chezmoitemplates/claude-settings.json":  claude,
		".chezmoitemplates/copilot-settings.json": goodCopilot,
		".chezmoidata/packages.toml": "[package.pyright]\n[package.ruff]\n[package.typescript]\n[package.gopls]\n[package.bash-language-server]\n" +
			"[package.yaml-language-server]\n[package.vscode-langservers-extracted]\n[package.terraform-ls]\n[package.tflint]\n",
	}
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckSourceFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		enabled  bool
		language string
		vscode   string
		claude   string
		want     []string // substrings, one per expected finding
	}{
		{name: "dictation true vs policy false", enabled: false, vscode: `{"dictation.enabled": true}`, claude: goodClaude, want: []string{"dictation.enabled"}},
		{name: "dictation false vs policy true", enabled: true, vscode: `{"dictation.enabled": false}`, claude: goodClaude, want: []string{"dictation.enabled"}},
		{name: "key absent and policy true", enabled: true, vscode: `{}`, claude: goodClaude},
		{name: "key absent and policy false", enabled: false, vscode: `{}`, claude: goodClaude, want: []string{"dictation.enabled"}},
		{name: "language policy but key missing", enabled: true, language: "it", vscode: `{"dictation.enabled": true}`, claude: goodClaude, want: []string{"agents.voice.language"}},
		{name: "language matches", enabled: true, language: "it", vscode: `{"agents.voice.language": "it"}`, claude: goodClaude},
		{name: "language differs", enabled: true, language: "it", vscode: `{"agents.voice.language": "fr"}`, claude: goodClaude, want: []string{"agents.voice.language"}},
		{name: "no policy language, key auto", enabled: true, vscode: `{"agents.voice.language": "auto"}`, claude: goodClaude},
		{name: "no policy language, key set", enabled: true, vscode: `{"agents.voice.language": "it"}`, claude: goodClaude, want: []string{"agents.voice.language"}},
		{
			name:    "comments, urls and trailing commas",
			enabled: true,
			vscode: `{
  // line comment
  /* block "comment" */
  "url": "https://example.com/a//b", // tail
  "esc": "quote \" // not a comment",
  "list": [1, 2,],
  "dictation.enabled": true,
}`,
			claude: goodClaude,
		},
		{name: "claude hard-coded enabled", enabled: true, vscode: `{}`, claude: `{"voice": {"enabled": true, "mode": {{ .harnessPolicy.voice.mode }}, "autoSubmit": {{ .harnessPolicy.voice.autoSubmit }}}}`, want: []string{".harnessPolicy.voice.enabled"}},
		{name: "claude language key", enabled: true, vscode: `{}`, claude: goodClaude + "\n" + `"language": "italian"`, want: []string{`"language"`}},
		{name: "invalid vscode json", enabled: true, vscode: `{`, claude: goodClaude, want: []string{"cannot parse"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := loadPolicy(t)
			p.Voice.Enabled = tt.enabled
			p.Voice.Language = tt.language
			dir := writeSource(t, tt.vscode, tt.claude)

			var got []string
			for _, f := range parity.CheckSourceFiles(p, dir) {
				got = append(got, f.String())
			}
			if len(got) != len(tt.want) {
				t.Fatalf("findings = %v, want %d matching %v", got, len(tt.want), tt.want)
			}
			for i, sub := range tt.want {
				if !strings.Contains(got[i], sub) {
					t.Errorf("finding %d = %q, want substring %q", i, got[i], sub)
				}
			}
		})
	}
}

// setCopilotTemplate overwrites the Copilot template, or removes it when content is empty.
func setCopilotTemplate(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, ".chezmoitemplates", "copilot-settings.json")
	if content == "" {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSourceFiles_CopilotTemplate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		template string   // empty: remove the file
		want     []string // substrings, one per expected finding
	}{
		{name: "reads agents", template: `{"voice": {"enabled": {{ .agents.voice.enabled | toJson }}}}`},
		{name: "reads harnessPolicy", template: goodCopilot},
		{name: "hard-coded", template: `{"voice": {"enabled": true}}`, want: []string{".agents.voice.enabled"}},
		{name: "file missing", want: []string{"cannot read"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := writeSource(t, `{"dictation.enabled": false}`, goodClaude)
			setCopilotTemplate(t, dir, tt.template)
			p := loadPolicy(t)
			p.Voice.Enabled = false
			findings := parity.CheckSourceFiles(p, dir)
			got := make([]string, 0, len(findings))
			for _, f := range findings {
				got = append(got, f.String())
			}
			if len(got) != len(tt.want) {
				t.Fatalf("findings = %v, want %d matching %v", got, len(tt.want), tt.want)
			}
			for i, sub := range tt.want {
				if !strings.Contains(got[i], sub) || !strings.Contains(got[i], "copilot") {
					t.Errorf("finding %d = %q, want substring %q and copilot", i, got[i], sub)
				}
			}
		})
	}
}

func TestCheckSourceFiles_MissingFiles(t *testing.T) {
	t.Parallel()
	got := parity.CheckSourceFiles(loadPolicy(t), t.TempDir())
	if len(got) != 4 {
		t.Errorf("findings = %v, want 4 (one per missing file)", got)
	}
}

func TestCheckSourceFiles_LSPPackages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		packages string
		want     []string // substrings, one per expected finding
	}{
		{name: "all present", packages: "[package.a]\n[package.b]\n"},
		{name: "one missing", packages: "[package.a]\n", want: []string{`lsp.server.two.package "b" is not in packages.toml`}},
		{name: "both missing", packages: "[release.a]\n", want: []string{`"a" is not in`, `"b" is not in`}},
		{name: "invalid toml", packages: "[package", want: []string{"cannot parse"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &policy.Policy{LSP: policy.LSP{Server: map[string]policy.LSPServer{
				"one": {Package: "a"},
				"two": {Package: "b"},
			}}}
			dir := writeSource(t, `{"dictation.enabled": false}`, goodClaude)
			path := filepath.Join(dir, ".chezmoidata", "packages.toml")
			if err := os.WriteFile(path, []byte(tt.packages), 0o600); err != nil {
				t.Fatal(err)
			}
			findings := parity.CheckSourceFiles(p, dir)
			got := make([]string, 0, len(findings))
			for _, f := range findings {
				got = append(got, f.String())
			}
			if len(got) != len(tt.want) {
				t.Fatalf("findings = %v, want %d matching %v", got, len(tt.want), tt.want)
			}
			for i, sub := range tt.want {
				if !strings.Contains(got[i], sub) {
					t.Errorf("finding %d = %q, want substring %q", i, got[i], sub)
				}
			}
		})
	}
}

func TestCheckSourceFiles_RealSource(t *testing.T) {
	t.Parallel()
	src := filepath.Join("..", "..", "..", "..", "home")
	if _, err := os.Stat(src); err != nil {
		t.Skip("skipping real source test: standalone repo context")
	}
	if diff := cmp.Diff(0, len(parity.CheckSourceFiles(loadPolicy(t), src))); diff != "" {
		t.Errorf("real source has findings: %v\n%s", parity.CheckSourceFiles(loadPolicy(t), src), diff)
	}
}
