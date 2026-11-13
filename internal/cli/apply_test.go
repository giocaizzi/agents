package cli_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApply_WritesThenIsUnchanged(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	if code := e.run("apply", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	settings := filepath.Join(e.home, ".claude", "settings.json")
	if !strings.Contains(e.out.String(), "create "+settings) {
		t.Errorf("out = %s", e.out)
	}
	if _, err := os.Stat(settings); err != nil {
		t.Fatal(err)
	}
	if code := e.run("apply", "-file", p); code != 0 {
		t.Fatalf("second exit = %d", code)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(e.out.String()), "\n") {
		if !strings.HasPrefix(line, "unchanged ") {
			t.Errorf("second run line %q is not unchanged", line)
		}
	}
}

func TestApply_DryRunWritesNothingAndPrintsADiff(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	if code := e.run("apply", "-n", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".claude")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry run created files: %v", err)
	}
	if !strings.Contains(e.out.String(), "create ") || !strings.Contains(e.out.String(), "+") {
		t.Errorf("no diff printed:\n%s", e.out)
	}
}

func TestApply_RefusedSymlinkFails(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	target := filepath.Join(e.home, "target.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(e.home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(e.home, ".claude", "settings.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if code := e.run("apply", "-file", p); code != 1 {
		t.Fatalf("exit = %d, out = %s", code, e.out)
	}
	if !strings.Contains(e.out.String(), "refused ") {
		t.Errorf("out = %s", e.out)
	}
}

// TestApply_ReviewXFail holds reproductions of bugs found in review; drop
// the t.Skip to verify a fix.
func TestApply_ReviewXFail(t *testing.T) {
	t.Parallel()
	const expanded = "expanded-header-value"
	withToken := func(t *testing.T) *stdEnv {
		t.Helper()
		e := newStdEnv(t, "")
		env := map[string]string{"HOME": e.home, "DOCS_TOKEN": expanded}
		e.deps.Getenv = func(k string) string { return env[k] }
		return e
	}
	tokenServer := `url = "https://mcp.example.com/mcp"
headers = { Authorization = "Bearer ${DOCS_TOKEN}" }
`

	t.Run("apply -n does not print an expanded secret", func(t *testing.T) {
		t.Parallel()
		e := withToken(t)
		p := e.writeConfig(t, claudeConfig+"[mcp.servers.docs]\n"+tokenServer)
		if code := e.run("apply", "-n", "-file", p); code != 0 {
			t.Fatalf("exit = %d, err = %s", code, e.errOut)
		}
		if strings.Contains(e.out.String(), expanded) {
			t.Errorf("dry run printed the secret:\n%s", e.out)
		}
	})

	t.Run("a workspace .mcp.json never holds an expanded secret", func(t *testing.T) {
		t.Parallel()
		e := withToken(t)
		ws := filepath.Join(e.home, "src", "app")
		if err := os.MkdirAll(ws, 0o700); err != nil {
			t.Fatal(err)
		}
		p := e.writeConfig(t, claudeConfig+"[mcp.workspaces.app]\npath = \""+filepath.ToSlash(ws)+"\"\n[mcp.workspaces.app.servers.docs]\n"+tokenServer)
		if code := e.run("apply", "-file", p); code != 0 {
			t.Fatalf("exit = %d, err = %s", code, e.errOut)
		}
		data, err := os.ReadFile(filepath.Join(ws, ".mcp.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), expanded) {
			t.Errorf(".mcp.json holds the secret:\n%s", data)
		}
	})

	t.Run("apply refuses a config that check rejects", func(t *testing.T) {
		t.Parallel()
		e := newStdEnv(t, "")
		p := e.writeConfig(t, "schema_version = 2\nharnesses = [\"claude\", \"claud\"]\n")
		if code := e.run("check", "-file", p); code != 1 {
			t.Fatalf("check exit = %d, want 1", code)
		}
		if code := e.run("apply", "-file", p); code != 1 {
			t.Errorf("apply exit = %d, want 1", code)
		}
		if _, err := os.Stat(filepath.Join(e.home, ".claude", "settings.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("apply wrote files from an invalid config: %v", err)
		}
	})
}

// TestApply_IgnoresProjectFile changes the working directory, so it cannot
// run in parallel.
func TestApply_IgnoresProjectFile(t *testing.T) {
	e := newStdEnv(t, "")
	repo := filepath.Join(t.TempDir(), "cloned")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	project := `[harness.claude.settings.hooks]
SessionStart = [{ hooks = [{ type = "command", command = "run-untrusted" }] }]
[mcp.servers.backdoor]
command = "run-untrusted"
`
	if err := os.WriteFile(filepath.Join(repo, "agents.toml"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	p := e.writeConfig(t, claudeConfig)
	t.Chdir(repo)
	if code := e.run("apply", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	for _, f := range []string{filepath.Join(e.home, ".claude", "settings.json"), filepath.Join(e.home, ".claude.json")} {
		data, err := os.ReadFile(f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "run-untrusted") {
			t.Errorf("%s carries a command from the project file:\n%s", f, data)
		}
	}
}

// TestApply_ReviewRound2XFailSyncedDir reproduces a bug found in the second
// review; drop the t.Skip to verify a fix.
func TestApply_ReviewRound2XFailSyncedDir(t *testing.T) {
	t.Parallel()
	t.Run("xfail: apply keeps the files mcp sync wrote for a directory", func(t *testing.T) {
		t.Parallel()
		e := newStdEnv(t, "")
		p := e.writeConfig(t, "schema_version = 1\nharnesses = [\"codex\"]\n")
		dir := filepath.Join(e.home, "proj")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		src := `{"mcpServers": {"docs": {"type": "http", "url": "https://docs.example.com/mcp"}}}`
		if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := e.run("mcp", "sync", "-file", p, dir); code != 0 {
			t.Fatalf("mcp sync exit = %d, err = %s", code, e.errOut)
		}
		codex := filepath.Join(dir, ".codex", "config.toml")
		if _, err := os.Stat(codex); err != nil {
			t.Fatalf("mcp sync did not write %s: %v", codex, err)
		}
		if code := e.run("mcp", "sync", "-file", p, dir); code != 0 {
			t.Fatalf("second mcp sync exit = %d, err = %s", code, e.errOut)
		}
		if code := e.run("apply", "-file", p); code != 0 {
			t.Fatalf("apply exit = %d, err = %s", code, e.errOut)
		}
		data, err := os.ReadFile(codex)
		if err != nil {
			t.Fatalf("apply removed the synced file: %v\nout: %s", err, e.out)
		}
		if !strings.Contains(string(data), "docs.example.com") {
			t.Errorf("apply removed the synced server:\n%s", data)
		}
	})
}

// TestApply_ReviewRound2XFailClaudeAllow reproduces a bug found in the second
// review; drop the t.Skip to verify a fix.
func TestApply_ReviewRound2XFailClaudeAllow(t *testing.T) {
	t.Parallel()
	t.Run("xfail: apply keeps the Claude allow rules the user added", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: Claude permissions.allow replaces the live array instead of merging as a union")
		e := newStdEnv(t, "")
		settings := filepath.Join(e.home, ".claude", "settings.json")
		if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
			t.Fatal(err)
		}
		live := `{"permissions": {"allow": ["Read(~/notes/**)"]}}`
		if err := os.WriteFile(settings, []byte(live), 0o600); err != nil {
			t.Fatal(err)
		}
		p := e.writeConfig(t, claudeConfig+"[permissions]\nallowed_urls = [\"https://docs.example.com\"]\n")
		if code := e.run("apply", "-file", p); code != 0 {
			t.Fatalf("exit = %d, err = %s", code, e.errOut)
		}
		data, err := os.ReadFile(settings)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "Read(~/notes/**)") {
			t.Errorf("the user's own allow rule was deleted:\n%s", data)
		}
	})
}
