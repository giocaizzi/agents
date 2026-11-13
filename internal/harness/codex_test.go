package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// renderOne renders cfg for one harness into a temp home.
func renderOne(t *testing.T, cfg config.Config, h config.Harness, home string) Plan {
	t.Helper()
	plan, err := Render(&config.Resolved{Config: cfg, Profile: "p", Harnesses: []config.Harness{h}}, Env{
		Home: home, Hostname: "host", Arch: "arm64", GOOS: "linux",
		Getenv:   func(k string) string { return map[string]string{"HOME": "/h"}[k] },
		LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func planTargetAt(t *testing.T, plan Plan, path string) apply.Target {
	t.Helper()
	for _, tg := range plan.Targets {
		if tg.Path == filepath.FromSlash(path) {
			return tg
		}
	}
	t.Fatalf("no target at %s", path)
	return apply.Target{}
}

func TestCodexRules(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Permissions: config.Permissions{
		AllowCommandPatterns: []string{"git push --force-with-lease origin *"},
		DenyCommandPatterns: []string{
			"git push origin main",
			"git push origin main *",
			"gh pr merge * --admin*",
		},
	}}
	home := t.TempDir()
	plan := renderOne(t, cfg, config.Codex, home)
	got := planTargetAt(t, plan, filepath.Join(home, ".codex", "rules", "agents.rules")).Managed
	want := `# Managed by agents from permissions: do not edit.
prefix_rule(
    pattern = ["git","push","--force-with-lease","origin"],
    decision = "allow",
    justification = "Allowed by the agents permissions.",
    match = ["git push --force-with-lease origin"],
)
prefix_rule(
    pattern = ["git","push","origin","main"],
    decision = "forbidden",
    justification = "Denied by the agents permissions.",
    match = ["git push origin main"],
)
`
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rules (-want +got):\n%s", diff)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "gh pr merge * --admin*") {
		t.Errorf("warnings = %q, want one for the infix wildcard", plan.Warnings)
	}
}

// TestCodexAllowSkippedWhenDenyUnexpressible pins that an allow prefix is not
// emitted while a deny pattern Codex cannot match overlaps it, and that the
// skipped allow is warned once, naming both.
func TestCodexAllowSkippedWhenDenyUnexpressible(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Permissions: config.Permissions{
		AllowCommands:        []string{"git", "gh", "make", "gh pr view", "git push"},
		AllowCommandPatterns: []string{"git push --force-with-lease origin *"},
		DenyCommandPatterns: []string{
			"git push * HEAD:main",
			"gh pr merge * --admin*",
			"git push --force-with-lease origin main",
		},
	}}
	home := t.TempDir()
	plan := renderOne(t, cfg, config.Codex, home)
	got, _ := planTargetAt(t, plan, filepath.Join(home, ".codex", "rules", "agents.rules")).Managed.(string)
	allowed := func(words string) bool {
		return strings.Contains(got, "pattern = ["+words+"],\n    decision = \"allow\"")
	}
	for words, want := range map[string]bool{
		`"git"`:            false,
		`"gh"`:             false,
		`"git","push"`:     false,
		`"make"`:           true,
		`"gh","pr","view"`: true,
		// The deny * spans words: git push --force-with-lease origin HEAD:main
		// matches both, so the lease allow goes too.
		`"git","push","--force-with-lease","origin"`: false,
	} {
		if allowed(words) != want {
			t.Errorf("allow rule [%s] emitted = %v, want %v:\n%s", words, !want, want, got)
		}
	}
	for _, skipped := range []string{`allow "git" `, `allow "gh" `, `allow "git push" `, `allow "git push --force-with-lease origin *" `} {
		n := 0
		for _, w := range plan.Warnings {
			if strings.Contains(w, skipped) && strings.Contains(w, "Codex rules cannot express it") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("warnings for %s = %d, want 1: %q", skipped, n, plan.Warnings)
		}
	}
}

// TestCodexAllowSkippedWhenAskUnexpressible pins the same rule for an ask
// prefix with a wildcard before its last word, which Codex skips too.
func TestCodexAllowSkippedWhenAskUnexpressible(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Permissions: config.Permissions{
		AllowCommands:      []string{"git", "make"},
		AskCommandPrefixes: []string{"git push * prod"},
	}}
	home := t.TempDir()
	plan := renderOne(t, cfg, config.Codex, home)
	got, _ := planTargetAt(t, plan, filepath.Join(home, ".codex", "rules", "agents.rules")).Managed.(string)
	if strings.Contains(got, `pattern = ["git"],`+"\n    decision = \"allow\"") {
		t.Errorf("allow git emitted past the unexpressible ask:\n%s", got)
	}
	if !strings.Contains(got, `pattern = ["make"],`+"\n    decision = \"allow\"") {
		t.Errorf("allow make missing:\n%s", got)
	}
	n := 0
	for _, w := range plan.Warnings {
		if strings.Contains(w, `allow "git" `) && strings.Contains(w, `ask pattern "git push * prod"`) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("ask-overlap warnings = %d, want 1: %q", n, plan.Warnings)
	}
}

func TestCodexConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		Notifications: config.Notifications{TerminalBell: true},
		Harness: config.HarnessTable{Codex: config.CodexSettings{
			ApprovalPolicy: "on-request",
			Settings:       map[string]any{"tui": map[string]any{"theme": "dark"}, "x": "${HOME}/y"},
		}},
		MCP: config.MCP{
			Servers: map[string]config.MCPServer{
				"remote": {URL: "https://a.example/mcp", Headers: map[string]string{"X-K": "v"}, OAuth: config.OAuth{ClientID: "c", CallbackPort: 1234}},
				"local":  {Command: "srv", Args: []string{"-v"}, Env: map[string]string{"K": "v"}},
			},
			Workspaces: map[string]config.Workspace{
				"b": {Path: "~/b"}, "a": {Path: "/work/a"},
			},
		},
	}
	home := t.TempDir()
	plan := renderOne(t, cfg, config.Codex, home)
	got := planTargetAt(t, plan, filepath.Join(home, ".codex", "config.toml"))
	if got.Format != apply.TOML || got.Mode != 0o600 {
		t.Errorf("format/mode = %v/%o", got.Format, got.Mode)
	}
	want := map[string]any{
		"approval_policy": "on-request",
		"tui":             map[string]any{"notifications": true, "notification_method": "bel", "theme": "dark"},
		"x":               "/h/y",
		"mcp_servers": map[string]any{
			"remote": map[string]any{
				"url": "https://a.example/mcp", "http_headers": map[string]any{"X-K": "v"},
				"oauth": map[string]any{"client_id": "c", "callback_port": int64(1234)},
			},
			"local": map[string]any{"command": "srv", "args": []any{"-v"}, "env": map[string]any{"K": "v"}},
		},
		"projects": map[string]any{
			"/work/a":                map[string]any{"trust_level": "trusted"},
			filepath.Join(home, "b"): map[string]any{"trust_level": "trusted"},
		},
	}
	if diff := cmp.Diff(want, got.Managed); diff != "" {
		t.Errorf("config (-want +got):\n%s", diff)
	}
}

func TestCodexTelemetryOffRemovesOTel(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	cfg := config.Config{Telemetry: config.Telemetry{
		Enabled: true, Endpoint: "https://otel.example/", Protocol: "http/protobuf",
		Attributes: map[string]string{"deployment.environment.name": "production"},
	}}
	eng := apply.Engine{StatePath: filepath.Join(t.TempDir(), "apply.json")}
	path := filepath.Join(home, ".codex", "config.toml")
	run := func(cfg config.Config) map[string]any {
		t.Helper()
		plan := renderOne(t, cfg, config.Codex, home)
		results, err := eng.Run(plan.Targets)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Err != nil {
				t.Fatal(r.Err)
			}
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatal(rerr)
		}
		var m map[string]any
		if err := toml.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	on := run(cfg)
	otel, _ := on["otel"].(map[string]any)
	if otel["environment"] != "production" {
		t.Fatalf("otel = %v, want environment production", otel)
	}
	exp, _ := otel["exporter"].(map[string]any)
	http, _ := exp["otlp-http"].(map[string]any)
	if http["endpoint"] != "https://otel.example/v1/logs" || http["protocol"] != "binary" {
		t.Errorf("logs exporter = %v", http)
	}
	cfg.Telemetry.Enabled = false
	if off := run(cfg); off["otel"] != nil {
		t.Errorf("otel = %v after telemetry off, want it removed", off["otel"])
	}
}
