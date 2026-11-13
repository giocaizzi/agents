package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/config"
)

func TestValidateWorkspacesExpandEnv(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked") // a linked work tree has a .git file
	if err := os.MkdirAll(linked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()
	getenv := func(k string) string {
		if k == "WS" {
			return repo
		}
		return ""
	}
	tests := []struct {
		name   string
		path   string
		expand bool
		want   string // "" = no finding
	}{
		{"inside a repo", filepath.Join(repo, "sub", "deep"), true, "mcp.workspaces.app.expand_env"},
		{"the repo root", repo, true, "mcp.workspaces.app.expand_env"},
		{"nonexistent below a repo", filepath.Join(repo, "new"), true, "git work tree"},
		{"linked work tree", filepath.Join(linked, "x"), true, "git work tree"},
		{"variable path", "${WS}/sub", true, "mcp.workspaces.app.expand_env"},
		{"outside any repo", plain, true, ""},
		{"nonexistent outside", filepath.Join(plain, "a", "b"), true, ""},
		{"ignored when false", filepath.Join(repo, "sub"), false, ""},
		{"unset variable is left to the renderer", "${NOPE}/x", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := config.Config{MCP: config.MCP{Workspaces: map[string]config.Workspace{
				"app": {Path: tt.path, ExpandEnv: tt.expand, Servers: map[string]config.MCPServer{"s": {URL: "u"}}},
			}}}
			got := config.ValidateWorkspaces(c, home, getenv, os.Stat)
			switch {
			case tt.want == "" && len(got) != 0:
				t.Errorf("unexpected findings: %v", got)
			case tt.want != "" && (len(got) != 1 || !strings.Contains(got[0].String(), tt.want)):
				t.Errorf("findings = %v, want one containing %q", got, tt.want)
			}
		})
	}
}
