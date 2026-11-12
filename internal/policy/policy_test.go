package policy_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/policy"
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
	p, err := policy.ParseTOML(strings.NewReader(input))
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

	wantLSP := policy.LSP{Enabled: true, Server: map[string]policy.LSPServer{
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
		voice policy.Voice
		want  int
	}{
		{"empty", policy.Voice{}, 0},
		{"hold", policy.Voice{Enabled: true, Mode: "hold"}, 0},
		{"tap", policy.Voice{Mode: "tap"}, 0},
		{"bad mode", policy.Voice{Mode: "toggle"}, 1},
		{"language short", policy.Voice{Language: "it"}, 0},
		{"language region", policy.Voice{Language: "it-IT"}, 0},
		{"bad language", policy.Voice{Language: "english"}, 1},
		{"bad language underscore", policy.Voice{Language: "en_US"}, 1},
		{"both bad", policy.Voice{Mode: "x", Language: "1"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := policy.Policy{Voice: tt.voice}
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
	p, err := policy.ParseTOML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTOML failed: %v", err)
	}
	want := map[string]policy.LSPServer{
		"gopls": {Package: "gopls", Command: []string{"gopls"}, Extensions: map[string]string{".go": "go"}},
	}
	if diff := cmp.Diff(want, p.LSP.Server); diff != "" {
		t.Errorf("LSP.Server mismatch (-want +got):\n%s", diff)
	}
}

func TestValidate_LSP(t *testing.T) {
	t.Parallel()
	ok := policy.LSPServer{Package: "p", Command: []string{"c"}, Extensions: map[string]string{".x": "x"}}
	with := func(f func(*policy.LSPServer)) policy.LSPServer {
		s := ok
		f(&s)
		return s
	}
	tests := []struct {
		name string
		srv  policy.LSPServer
		want []string
	}{
		{"valid", ok, nil},
		{"no package", with(func(s *policy.LSPServer) { s.Package = "" }), []string{"lsp.server.s.package is empty"}},
		{"no command", with(func(s *policy.LSPServer) { s.Command = nil }), []string{"lsp.server.s.command is empty"}},
		{"blank command", with(func(s *policy.LSPServer) { s.Command = []string{" "} }), []string{"lsp.server.s.command is empty"}},
		{"no extensions", with(func(s *policy.LSPServer) { s.Extensions = nil }), []string{"lsp.server.s.extensions is empty"}},
		{"extension without dot", with(func(s *policy.LSPServer) { s.Extensions = map[string]string{"x": "x"} }), []string{`extensions key "x" must start with`}},
		{"empty language id", with(func(s *policy.LSPServer) { s.Extensions = map[string]string{".x": ""} }), []string{`extensions[".x"] has an empty language id`}},
		{"blank filetype", with(func(s *policy.LSPServer) { s.Filetypes = []string{"sh", " "} }), []string{"lsp.server.s.filetypes has an empty entry"}},
		{"filetypes ok", with(func(s *policy.LSPServer) { s.Filetypes = []string{"sh", "bash"} }), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := policy.Policy{LSP: policy.LSP{Server: map[string]policy.LSPServer{"s": tt.srv}}}
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
