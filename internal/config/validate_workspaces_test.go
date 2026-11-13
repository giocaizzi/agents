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

// TestValidateWorkspacesSymlinkIntoRepo: a workspace path that reaches a
// directory below a repository root through symlinks is inside that work
// tree, and the files apply writes through it land there. A link loop is not
// a work tree and must not hang the walk.
func TestValidateWorkspacesSymlinkIntoRepo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// links maps a name under the scratch dir to its target; "@" stands
		// for the repository root, "%" for the scratch dir.
		links map[string]string
		ws    string // the workspace path, under the scratch dir
		want  int
	}{
		{"link to a subdirectory", map[string]string{"ws": "@/sub"}, "ws", 1},
		{"chain of two links", map[string]string{"a": "%/b", "b": "@/sub"}, "a", 1},
		{"relative link target", map[string]string{"ws": "../repo/sub"}, "ws", 1},
		{"missing directories below the link", map[string]string{"ws": "@/sub"}, "ws/new/dir", 1},
		{"link out of the repository", map[string]string{"ws": "%"}, "ws", 0},
		{"link loop", map[string]string{"a": "%/b", "b": "%/a"}, "a/x", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			repo := filepath.Join(root, "repo")
			scratch := filepath.Join(root, "scratch")
			for _, d := range []string{filepath.Join(repo, ".git"), filepath.Join(repo, "sub"), scratch} {
				if err := os.MkdirAll(d, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for name, target := range tt.links {
				target = strings.NewReplacer("@", repo, "%", scratch).Replace(target)
				if err := os.Symlink(target, filepath.Join(scratch, name)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			ws := filepath.Join(scratch, tt.ws)
			c := config.Config{MCP: config.MCP{Workspaces: map[string]config.Workspace{
				"app": {Path: ws, ExpandEnv: true, Servers: map[string]config.MCPServer{"s": {URL: "u"}}},
			}}}
			if got := config.ValidateWorkspaces(c, t.TempDir(), func(string) string { return "" }, os.Stat); len(got) != tt.want {
				t.Errorf("findings = %v, want %d for %s (work tree %s)", got, tt.want, ws, repo)
			}
		})
	}
}
