package config_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
)

func TestParseTOML_Valid(t *testing.T) {
	t.Parallel()
	input := `
[harnessPolicy.autonomy]
executionMode = "auto"

[harnessPolicy.notifications]
terminalBell = true

[harnessPolicy.permissions]
allowCommands = ["git *"]
allowedUrls = ["https://github.com/**"]
denyRead = ["**/.env"]
denyWrite = ["**/.env"]
denyCommandPatterns = ["gh pr merge * --admin*"]
denyCommandExact = ["env"]
denyCommandPrefixes = ["printenv"]
askCommandPrefixes = ["bw"]

[harnessPolicy.models.tiers.scout]
model = "gpt-6-luna"
effort = "low"

[harnessPolicy.lsp]
enabled = true

[harnessPolicy.lsp.server.gopls]
package = "gopls"
command = ["gopls"]
extensions = { ".go" = "go" }
format = true

[harnessPolicy.native.claude]
defaultMode = "auto"

[harnessPolicy.native.pi]
defaultProvider = "openai"
defaultModel = "gpt-5.5"
defaultThinkingLevel = "medium"
packages = ["local/pkg"]
`
	p, err := config.ParseTOML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTOML failed: %v", err)
	}

	if !p.Notifications.TerminalBell {
		t.Errorf("expected terminalBell true")
	}
	if len(p.Permissions.DenyRead) != 1 {
		t.Errorf("expected 1 denyRead entry, got %d", len(p.Permissions.DenyRead))
	}
	if len(p.Permissions.AllowCommands) != 1 {
		t.Errorf("expected 1 allowCommands entry, got %d", len(p.Permissions.AllowCommands))
	}
	if p.Native.Claude.DefaultMode != "auto" {
		t.Errorf("expected defaultMode auto, got %q", p.Native.Claude.DefaultMode)
	}

	wantLSP := config.LSP{Enabled: true, Server: map[string]config.LSPServer{
		"gopls": {Package: "gopls", Command: []string{"gopls"}, Extensions: map[string]string{".go": "go"}, Format: true},
	}}
	if diff := cmp.Diff(wantLSP, p.LSP); diff != "" {
		t.Errorf("LSP mismatch (-want +got):\n%s", diff)
	}

	findings := p.Validate("test")
	if len(findings) > 0 {
		t.Errorf("unexpected validation findings: %v", findings)
	}
}

func TestValidate_Voice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		voice config.Voice
		want  int
	}{
		{"empty", config.Voice{}, 0},
		{"hold", config.Voice{Enabled: true, Mode: "hold"}, 0},
		{"tap", config.Voice{Mode: "tap"}, 0},
		{"bad mode", config.Voice{Mode: "toggle"}, 1},
		{"language short", config.Voice{Language: "it"}, 0},
		{"language region", config.Voice{Language: "it-IT"}, 0},
		{"bad language", config.Voice{Language: "english"}, 1},
		{"bad language underscore", config.Voice{Language: "en_US"}, 1},
		{"both bad", config.Voice{Mode: "x", Language: "1"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := config.Policy{Voice: tt.voice}
			if got := p.Validate("test"); len(got) != tt.want {
				t.Errorf("Validate() = %v, want %d findings", got, tt.want)
			}
		})
	}
}

func TestParseTOML_LSPRootTable(t *testing.T) {
	t.Parallel()
	input := `
[permissions]
denyRead = ["**/.env"]

[lsp.server.gopls]
package = "gopls"
command = ["gopls"]
extensions = { ".go" = "go" }
`
	p, err := config.ParseTOML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTOML failed: %v", err)
	}
	want := map[string]config.LSPServer{
		"gopls": {Package: "gopls", Command: []string{"gopls"}, Extensions: map[string]string{".go": "go"}},
	}
	if diff := cmp.Diff(want, p.LSP.Server); diff != "" {
		t.Errorf("LSP.Server mismatch (-want +got):\n%s", diff)
	}
}

func TestValidate_LSP(t *testing.T) {
	t.Parallel()
	ok := config.LSPServer{Package: "p", Command: []string{"c"}, Extensions: map[string]string{".x": "x"}}
	with := func(f func(*config.LSPServer)) config.LSPServer {
		s := ok
		f(&s)
		return s
	}
	tests := []struct {
		name string
		srv  config.LSPServer
		want []string
	}{
		{"valid", ok, nil},
		{"no package", with(func(s *config.LSPServer) { s.Package = "" }), []string{"lsp.server.s.package is empty"}},
		{"no command", with(func(s *config.LSPServer) { s.Command = nil }), []string{"lsp.server.s.command is empty"}},
		{"blank command", with(func(s *config.LSPServer) { s.Command = []string{" "} }), []string{"lsp.server.s.command is empty"}},
		{"no extensions", with(func(s *config.LSPServer) { s.Extensions = nil }), []string{"lsp.server.s.extensions is empty"}},
		{"extension without dot", with(func(s *config.LSPServer) { s.Extensions = map[string]string{"x": "x"} }), []string{`extensions key "x" must start with`}},
		{"empty language id", with(func(s *config.LSPServer) { s.Extensions = map[string]string{".x": ""} }), []string{`extensions[".x"] has an empty language id`}},
		{"blank filetype", with(func(s *config.LSPServer) { s.Filetypes = []string{"sh", " "} }), []string{"lsp.server.s.filetypes has an empty entry"}},
		{"filetypes ok", with(func(s *config.LSPServer) { s.Filetypes = []string{"sh", "bash"} }), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := config.Policy{LSP: config.LSP{Server: map[string]config.LSPServer{"s": tt.srv}}}
			findings := p.Validate("test")
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
