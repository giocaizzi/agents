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

func setupMCPFixture(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	mcpJSON := `{
		"mcpServers": {
			"grafana": {
				"type": "http",
				"url": "https://mcp-connector.${MCP_CONNECTOR_ACCOUNT}.workers.dev/mcp"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, mcpsync.SourceFilename), []byte(mcpJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return tmpDir
}

func TestRunMCPSync_CheckMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := setupMCPFixture(t)

	var stdout, stderr bytes.Buffer
	deps := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(_ string) string { return "" },
	}
	code := deps.Run(ctx, []string{"mcp", "sync", "-check", "-account", "my-acct", tmpDir})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "workspace MCP source is valid") {
		t.Errorf("stdout mismatch: %q", stdout.String())
	}
}

func TestRunMCPSync_SyncMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := setupMCPFixture(t)

	var stdout, stderr bytes.Buffer
	deps := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			if k == "MCP_CONNECTOR_ACCOUNT" {
				return "my-acct"
			}
			return ""
		},
	}
	code := deps.Run(ctx, []string{"mcp", "sync", tmpDir})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "generated workspace MCP configuration") {
		t.Errorf("stdout mismatch: %q", stdout.String())
	}

	for _, rel := range []string{mcpsync.CodexTargetRelPath, mcpsync.PiTargetRelPath, mcpsync.AntigravityRelPath} {
		p := filepath.Join(tmpDir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected target file %s to exist: %v", rel, err)
		}
	}
}

func TestRunMCPSync_OAuthWarnings(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mcpJSON := `{
		"mcpServers": {
			"example": {
				"type": "http",
				"url": "https://mcp.example.com/mcp",
				"oauth": {"clientId": "example-client", "callbackPort": 8765},
				"oauthClientId": "example-client",
				"oauthPublicClient": false,
				"auth": {"redirectPort": 8765}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, mcpsync.SourceFilename), []byte(mcpJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"mcp", "sync", "-check", tmpDir},
		{"mcp", "sync", tmpDir},
	} {
		var stdout, stderr bytes.Buffer
		deps := cli.Deps{Stdout: &stdout, Stderr: &stderr, Getenv: func(_ string) string { return "" }}
		if code := deps.Run(t.Context(), args); code != 0 {
			t.Fatalf("%v: exit code %d, stderr: %s", args, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "warning:") || !strings.Contains(stderr.String(), "callback port 8765") {
			t.Errorf("%v: stderr lacks the callback port warning: %q", args, stderr.String())
		}
	}
}

func TestRunMCPSync_InvalidDir(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	deps := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(_ string) string { return "" },
	}
	nonExistent := filepath.Join(tmpDir, "nonexistent")
	code := deps.Run(ctx, []string{"mcp", "sync", nonExistent})
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Errorf("stderr mismatch: %q", stderr.String())
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

func TestRunMCPAdd_HTTP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	deps := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(_ string) string { return "" },
	}

	code := deps.Run(ctx, []string{
		"mcp", "add",
		"-dir", tmpDir,
		"-header", "Authorization: Bearer token123",
		"remote-docs",
		"https://mcp.example.com/mcp",
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "added server \"remote-docs\"") {
		t.Errorf("stdout mismatch: %q", stdout.String())
	}

	for _, rel := range []string{mcpsync.CodexTargetRelPath, mcpsync.PiTargetRelPath, mcpsync.AntigravityRelPath} {
		p := filepath.Join(tmpDir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected target file %s to exist: %v", rel, err)
		}
	}
}

func TestRunMCPAdd_StdioWithEnv(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	deps := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(_ string) string { return "" },
	}

	code := deps.Run(ctx, []string{
		"mcp", "add",
		"-dir", tmpDir,
		"-env", "API_KEY=testkey",
		"context7",
		"npx",
		"-y", "@upstash/context7-mcp",
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "added server \"context7\"") {
		t.Errorf("stdout mismatch: %q", stdout.String())
	}

	for _, rel := range []string{mcpsync.CodexTargetRelPath, mcpsync.PiTargetRelPath, mcpsync.AntigravityRelPath} {
		p := filepath.Join(tmpDir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected target file %s to exist: %v", rel, err)
		}
	}
}

func TestRunMCPAdd_NoSync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	deps := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(_ string) string { return "" },
	}

	code := deps.Run(ctx, []string{
		"mcp", "add",
		"-dir", tmpDir,
		"-no-sync",
		"local-srv",
		"uvx", "mcp-server-fetch",
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "added server \"local-srv\" to .mcp.json") {
		t.Errorf("stdout mismatch: %q", stdout.String())
	}

	codexPath := filepath.Join(tmpDir, mcpsync.CodexTargetRelPath)
	if _, err := os.Stat(codexPath); !os.IsNotExist(err) {
		t.Errorf("expected codex target file to not exist when -no-sync is specified: %v", err)
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
