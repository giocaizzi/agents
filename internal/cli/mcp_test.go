package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/cli"
	"github.com/giocaizzi/agents/internal/mcpsync"
)

func TestRunMCP_SubcommandRouting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("missing subcommand returns exit code 2", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		deps := cli.Deps{
			Stdout: &stdout,
			Stderr: &stderr,
		}
		code := deps.Run(ctx, []string{"mcp"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
		if !strings.Contains(stderr.String(), "missing mcp subcommand") {
			t.Errorf("stderr mismatch: %q", stderr.String())
		}
	})

	t.Run("unknown subcommand returns exit code 2", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		deps := cli.Deps{
			Stdout: &stdout,
			Stderr: &stderr,
		}
		code := deps.Run(ctx, []string{"mcp", "unknown"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
		if !strings.Contains(stderr.String(), "unknown mcp subcommand") {
			t.Errorf("stderr mismatch: %q", stderr.String())
		}
	})

	t.Run("mcp help returns exit code 0", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		deps := cli.Deps{
			Stdout: &stdout,
			Stderr: &stderr,
		}
		code := deps.Run(ctx, []string{"mcp", "help"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !strings.Contains(stdout.String(), "agents") {
			t.Errorf("stdout mismatch: %q", stdout.String())
		}
	})
}

// mcpRun is one hermetic invocation: HOME is a temp directory, the harnesses
// are picked through AGENTS_HARNESSES and nothing else reads the machine.
type mcpRun struct {
	home           string
	stdout, stderr bytes.Buffer
}

func newMCPRun(t *testing.T) *mcpRun {
	t.Helper()
	return &mcpRun{home: t.TempDir()}
}

func (m *mcpRun) run(t *testing.T, harnesses string, args ...string) int {
	t.Helper()
	m.stdout.Reset()
	m.stderr.Reset()
	deps := cli.Deps{
		Stdout: &m.stdout,
		Stderr: &m.stderr,
		Getenv: func(k string) string {
			switch k {
			case "HOME":
				return m.home
			case "AGENTS_HARNESSES":
				return harnesses
			case "MCP_CONNECTOR_ACCOUNT":
				return "my-acct"
			}
			return ""
		},
	}
	return deps.Run(t.Context(), append([]string{"mcp"}, args...))
}

func (m *mcpRun) writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

const mcpSource = `{
	"mcpServers": {
		"grafana": {"type": "http", "url": "https://mcp-connector.${MCP_CONNECTOR_ACCOUNT}.workers.dev/mcp"},
		"crm": {"type": "http", "url": "https://crm.example.com/mcp", "oauth": {"clientId": "c"}, "oauthPublicClient": true}
	}
}`

func (m *mcpRun) mustRun(t *testing.T, harnesses string, args ...string) {
	t.Helper()
	if code := m.run(t, harnesses, args...); code != 0 {
		t.Fatalf("mcp %v: exit %d, stderr: %s", args, code, m.stderr.String())
	}
}

func contains(t *testing.T, what, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s lacks %q: %q", what, want, got)
	}
}

func existsAll(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if !exists(filepath.Join(root, rel)) {
			t.Errorf("%s not written", rel)
		}
	}
}

// configWorkspace writes a config with one workspace ("ops") and returns the
// run, the workspace directory and the config path.
func configWorkspace(t *testing.T) (m *mcpRun, ws, cfg string) {
	t.Helper()
	m = newMCPRun(t)
	ws = filepath.Join(m.home, "ops")
	cfg = filepath.Join(m.home, "agents.toml")
	m.writeFile(t, cfg, `
[mcp.workspaces.ops]
path = "`+filepath.ToSlash(ws)+`"

[mcp.workspaces.ops.servers.docs]
url = "https://docs.example.com/mcp"

[mcp.workspaces.ops.servers.crm]
url = "https://crm.example.com/mcp"
oauth = { client_id = "c", public_client = true }
`)
	return m, ws, cfg
}

var workspaceFiles = []string{".mcp.json", ".codex/config.toml", "opencode.json"}

func TestRunMCPSync_FromConfigDryRun(t *testing.T) {
	t.Parallel()

	m, ws, cfg := configWorkspace(t)
	m.mustRun(t, "claude,codex,opencode", "sync", "-n", "-file", cfg)
	contains(t, "stdout", m.stdout.String(), "create "+filepath.Join(ws, ".mcp.json"))
	contains(t, "stdout", m.stdout.String(), "https://docs.example.com/mcp")
	for _, f := range workspaceFiles {
		if exists(filepath.Join(ws, f)) {
			t.Errorf("%s written by a dry run", f)
		}
	}
}

func TestRunMCPSync_FromConfigIdempotent(t *testing.T) {
	t.Parallel()

	m, ws, cfg := configWorkspace(t)
	m.mustRun(t, "claude,codex,opencode", "sync", "-file", cfg)
	existsAll(t, ws, workspaceFiles...)
	contains(t, "stderr", m.stderr.String(), `opencode skips server "crm": OAuth clients are not supported`)

	m.mustRun(t, "claude,codex,opencode", "sync", "-file", cfg)
	if got := strings.Count(m.stdout.String(), "unchanged "); got != len(workspaceFiles) {
		t.Errorf("unchanged lines = %d, want %d: %q", got, len(workspaceFiles), m.stdout.String())
	}
}

func TestRunMCPSync_Path(t *testing.T) {
	t.Parallel()

	m := newMCPRun(t)
	dir := filepath.Join(m.home, "proj")
	m.writeFile(t, filepath.Join(dir, mcpsync.SourceFilename), mcpSource)
	harnesses := "claude,codex,pi,opencode"

	m.mustRun(t, harnesses, "sync", "-n", dir)
	if exists(filepath.Join(dir, mcpsync.CodexTargetRelPath)) {
		t.Error("dry run wrote the Codex file")
	}

	m.mustRun(t, harnesses, "sync", dir)
	existsAll(t, dir, mcpsync.CodexTargetRelPath, mcpsync.PiTargetRelPath, "opencode.json")
	if strings.Contains(m.stdout.String(), mcpsync.SourceFilename+"\n") {
		t.Errorf("the source was applied to: %q", m.stdout.String())
	}
	contains(t, "stderr", m.stderr.String(), `opencode skips server "crm": OAuth clients are not supported`)
	src, err := os.ReadFile(filepath.Join(dir, mcpsync.SourceFilename))
	if err != nil || string(src) != mcpSource {
		t.Errorf("source changed (err %v): %s", err, src)
	}
	codex, err := os.ReadFile(filepath.Join(dir, mcpsync.CodexTargetRelPath))
	if err != nil {
		t.Fatal(err)
	}
	contains(t, "Codex file", string(codex), "https://mcp-connector.my-acct.workers.dev/mcp")
}

func TestRunMCPSync_Errors(t *testing.T) {
	t.Parallel()

	m := newMCPRun(t)
	empty := filepath.Join(m.home, "empty")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(m.home, "bad")
	m.writeFile(t, filepath.Join(bad, mcpsync.SourceFilename), `{"mcpServers": {"x": {"type": "ftp"}}}`)

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"missing directory", []string{"sync", filepath.Join(m.home, "nonexistent")}, 1},
		{"directory without source", []string{"sync", empty}, 1},
		{"invalid source", []string{"sync", bad}, 1},
		{"missing config file", []string{"sync", "-file", filepath.Join(m.home, "none.toml")}, 1},
		{"two paths", []string{"sync", empty, bad}, 2},
		{"removed -check flag", []string{"sync", "-check", empty}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &mcpRun{home: m.home}
			if code := r.run(t, "codex", tt.args...); code != tt.want {
				t.Errorf("exit %d, want %d; stderr: %s", code, tt.want, r.stderr.String())
			}
		})
	}
}

func TestRunMCPAdd_MissingArgs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	deps := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
	}
	code := deps.Run(ctx, []string{"mcp", "add", "onlyName"})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "mcp add requires <name> and <commandOrUrl>") {
		t.Errorf("stderr mismatch: %q", stderr.String())
	}
}

func TestRunMCPAdd_ThenSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{"http", []string{"-header", "Authorization: Bearer token123", "remote-docs", "https://mcp.example.com/mcp"}},
		{"stdio with env", []string{"-env", "API_KEY=testkey", "context7", "npx", "-y", "@upstash/context7-mcp"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newMCPRun(t)
			dir := filepath.Join(m.home, "proj")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"add", "-dir", dir}, tt.args...)
			if code := m.run(t, "codex,pi,antigravity", args...); code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, m.stderr.String())
			}
			if !strings.Contains(m.stdout.String(), "added server") {
				t.Errorf("stdout lacks the added line: %q", m.stdout.String())
			}
			for _, rel := range []string{mcpsync.SourceFilename, mcpsync.CodexTargetRelPath, mcpsync.PiTargetRelPath, mcpsync.AntigravityRelPath} {
				if !exists(filepath.Join(dir, rel)) {
					t.Errorf("%s not written", rel)
				}
			}
			if !strings.Contains(m.stdout.String(), "create "+filepath.Join(dir, mcpsync.CodexTargetRelPath)) {
				t.Errorf("stdout lacks the create line: %q", m.stdout.String())
			}
		})
	}
}

func TestRunMCPAdd_NoSync(t *testing.T) {
	t.Parallel()

	m := newMCPRun(t)
	dir := filepath.Join(m.home, "proj")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	code := m.run(t, "codex", "add", "-dir", dir, "-no-sync", "local-srv", "uvx", "mcp-server-fetch")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, m.stderr.String())
	}
	if !strings.Contains(m.stdout.String(), "added server \"local-srv\" to .mcp.json") {
		t.Errorf("stdout mismatch: %q", m.stdout.String())
	}
	if exists(filepath.Join(dir, mcpsync.CodexTargetRelPath)) {
		t.Error("Codex file written with -no-sync")
	}
}

func TestRunMCPAdd_InvalidEnvAndHeader(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	t.Run("invalid env", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		deps := cli.Deps{Stdout: &stdout, Stderr: &stderr}
		code := deps.Run(ctx, []string{"mcp", "add", "-dir", tmpDir, "-env", "invalid_no_equal", "srv", "cmd"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid env format") {
			t.Errorf("stderr mismatch: %q", stderr.String())
		}
	})

	t.Run("invalid header", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		deps := cli.Deps{Stdout: &stdout, Stderr: &stderr}
		code := deps.Run(ctx, []string{"mcp", "add", "-dir", tmpDir, "-header", "invalid_no_colon", "srv", "https://example.com"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid header format") {
			t.Errorf("stderr mismatch: %q", stderr.String())
		}
	})
}
