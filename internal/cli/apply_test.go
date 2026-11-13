package cli_test

import (
	"errors"
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

	t.Run("xfail: apply -n does not print an expanded secret", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: the dry-run diff prints the expanded ${DOCS_TOKEN} header value to stdout")
		e := withToken(t)
		p := e.writeConfig(t, claudeConfig+"[mcp.servers.docs]\n"+tokenServer)
		if code := e.run("apply", "-n", "-file", p); code != 0 {
			t.Fatalf("exit = %d, err = %s", code, e.errOut)
		}
		if strings.Contains(e.out.String(), expanded) {
			t.Errorf("dry run printed the secret:\n%s", e.out)
		}
	})

	t.Run("xfail: a workspace .mcp.json never holds an expanded secret", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: ${VAR} in workspace servers is expanded into the shared, world-readable .mcp.json that projects commit")
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

	t.Run("xfail: apply refuses a config that check rejects", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: apply never runs config.Validate, so schema_version = 2 or a typo in harnesses still writes files and exits 0")
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

// TestApply_ReviewXFailUntrustedProject changes the working directory, so it
// cannot run in parallel.
func TestApply_ReviewXFailUntrustedProject(t *testing.T) {
	t.Run("xfail: an untrusted project agents.toml cannot write user-level hooks or MCP servers", func(t *testing.T) {
		t.Skip("xfail: any agents.toml up to the git root is layered into the user config, so running apply inside a cloned repo persists its hooks and MCP commands in ~/.claude")
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
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "run-untrusted") {
				t.Errorf("%s carries a command from the project file:\n%s", f, data)
			}
		}
	})
}
