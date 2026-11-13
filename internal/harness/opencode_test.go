package harness

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

func TestOpenCodeConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		Autonomy: config.Autonomy{SubagentDepth: 2},
		LSP: config.LSP{Enabled: true, Servers: map[string]config.LSPServer{
			"ruff": {Command: []string{"ruff", "server"}, Extensions: map[string]string{".pyi": "python", ".py": "python"}, Format: true},
		}},
		MCP: config.MCP{Servers: map[string]config.MCPServer{
			"remote": {URL: "https://a.example/mcp", Headers: map[string]string{"X-K": "v"}},
			"local":  {Command: "srv", Args: []string{"-v"}, Env: map[string]string{"K": "v"}},
		}},
		Permissions: config.Permissions{
			AllowCommandPatterns: []string{"git push *"},
			AskCommandPrefixes:   []string{"bw"},
			DenyRead:             []string{"**/.env"},
			DenyWrite:            []string{"**/.ssh/**"},
			DenyCommandExact:     []string{"env"},
			DenyCommandPrefixes:  []string{"pip"},
		},
		Harness: config.HarnessTable{OpenCode: config.OpenCodeSettings{Settings: map[string]any{"theme": "dark"}}},
	}
	home := t.TempDir()
	plan := renderOne(t, cfg, config.OpenCode, home)
	got := targetAt(t, plan, filepath.Join(home, ".config", "opencode", "opencode.jsonc"))
	if got.Format != apply.JSONC || got.Mode != 0o600 {
		t.Errorf("format/mode = %v/%o", got.Format, got.Mode)
	}
	rule := func(action, effect, resource string) any {
		return map[string]any{"action": action, "effect": effect, "resource": resource}
	}
	want := map[string]any{
		"$schema":      "https://opencode.ai/config.json",
		"theme":        "dark",
		"experimental": map[string]any{"subagent_depth": int64(2)},
		"lsp": map[string]any{
			"ruff": map[string]any{"command": []any{"ruff", "server"}, "extensions": []any{".py", ".pyi"}},
		},
		"mcp": map[string]any{
			"remote": map[string]any{"enabled": true, "type": "remote", "url": "https://a.example/mcp", "headers": map[string]any{"X-K": "v"}},
			"local":  map[string]any{"enabled": true, "type": "local", "command": []any{"srv", "-v"}, "environment": map[string]any{"K": "v"}},
		},
		"permissions": []any{
			rule("shell", "allow", "git push *"),
			rule("shell", "ask", "bw"),
			rule("shell", "ask", "bw *"),
		},
		"policies": []any{
			rule("tool.use", "deny", "read:**/.env"),
			rule("tool.use", "deny", "edit:**/.ssh/**"),
			rule("tool.use", "deny", "shell:env"),
			rule("tool.use", "deny", "shell:pip"),
			rule("tool.use", "deny", "shell:pip *"),
		},
	}
	if diff := cmp.Diff(want, got.Managed); diff != "" {
		t.Errorf("opencode.jsonc (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([][]string{{"permissions"}, {"policies"}}, got.Union); diff != "" {
		t.Errorf("union (-want +got):\n%s", diff)
	}
}

func TestOpenCodeLSPOff(t *testing.T) {
	t.Parallel()
	cfg := config.Config{LSP: config.LSP{Servers: map[string]config.LSPServer{"ruff": {Command: []string{"ruff"}}}}}
	home := t.TempDir()
	plan := renderOne(t, cfg, config.OpenCode, home)
	m, _ := targetAt(t, plan, filepath.Join(home, ".config", "opencode", "opencode.jsonc")).Managed.(map[string]any)
	if _, ok := m["lsp"]; ok {
		t.Errorf("lsp = %v with lsp disabled, want none", m["lsp"])
	}
}
