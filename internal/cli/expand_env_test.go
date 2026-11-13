package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const expandSecret = "expanded-header-value"

// expandSetup writes a config with an expand_env workspace under the home
// directory; with gitDir the home is a git work tree.
func expandSetup(t *testing.T, gitDir bool) (e *stdEnv, ws, cfg string) {
	t.Helper()
	e = newStdEnv(t, "")
	env := map[string]string{"HOME": e.home, "DOCS_TOKEN": expandSecret}
	e.deps.Getenv = func(k string) string { return env[k] }
	ws = filepath.Join(e.home, "ops")
	if gitDir {
		if err := os.MkdirAll(filepath.Join(e.home, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg = e.writeConfig(t, claudeConfig+"[mcp.workspaces.ops]\npath = \""+filepath.ToSlash(ws)+"\"\nexpand_env = true\n"+
		"[mcp.workspaces.ops.servers.docs]\nurl = \"https://mcp.example.com/mcp\"\nheaders = { Authorization = \"Bearer ${DOCS_TOKEN}\" }\n")
	return e, ws, cfg
}

func TestExpandEnv_DryRunRedacts(t *testing.T) {
	t.Parallel()
	e, ws, cfg := expandSetup(t, false)
	if code := e.run("apply", "-n", "-file", cfg); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	if strings.Contains(e.out.String(), expandSecret) || strings.Contains(e.errOut.String(), expandSecret) {
		t.Errorf("dry run printed the secret:\n%s", e.out)
	}
	if !strings.Contains(e.out.String(), filepath.Join(ws, ".mcp.json")) {
		t.Errorf("dry run does not list the workspace file:\n%s", e.out)
	}
}

func TestExpandEnv_ApplyWritesValue(t *testing.T) {
	t.Parallel()
	e, ws, cfg := expandSetup(t, false)
	if code := e.run("apply", "-file", cfg); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	data, err := os.ReadFile(filepath.Join(ws, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Bearer "+expandSecret) {
		t.Errorf(".mcp.json = %s, want the expanded value", data)
	}
}

func TestExpandEnv_ApplyAndCheckRefuseWorkTree(t *testing.T) {
	t.Parallel()
	e, ws, cfg := expandSetup(t, true)
	if code := e.run("check", "-file", cfg); code != 1 {
		t.Errorf("check exit = %d, want 1", code)
	}
	if !strings.Contains(e.out.String()+e.errOut.String(), "mcp.workspaces.ops.expand_env") {
		t.Errorf("check output lacks the finding:\n%s\n%s", e.out, e.errOut)
	}
	if code := e.run("apply", "-file", cfg); code != 1 {
		t.Errorf("apply exit = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(ws, ".mcp.json")); err == nil {
		t.Error("apply wrote a workspace file inside a work tree")
	}
}

func TestExpandEnv_MCPSyncRefusesWorkTree(t *testing.T) {
	t.Parallel()
	e, ws, cfg := expandSetup(t, true)
	if code := e.run("mcp", "sync", "-file", cfg); code != 1 {
		t.Errorf("mcp sync exit = %d, want 1 (out %s, err %s)", code, e.out, e.errOut)
	}
	if _, err := os.Stat(filepath.Join(ws, ".mcp.json")); err == nil {
		t.Error("mcp sync wrote a workspace file inside a work tree")
	}
}

// TestExpandEnv_ExistingFileMode: the expanded secrets land in a file that is
// 0600 even when the workspace file already existed with wider permissions
// (as a committed project file usually does).
func TestExpandEnv_ExistingFileMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	e, ws, cfg := expandSetup(t, false)
	file := filepath.Join(ws, ".mcp.json")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := e.run("apply", "-file", cfg); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600 for a file holding expanded secrets", got)
	}
}
