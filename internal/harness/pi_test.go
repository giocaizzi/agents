package harness

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
)

// newTestBuilder is a builder for c with an empty environment under /home/u.
func newTestBuilder(c *config.Config) *builder {
	return &builder{
		cfg: c,
		env: Env{
			Home: "/home/u", GOOS: "linux",
			Getenv: func(string) string { return "" },
			Dirs:   config.Dirs{Data: "/data"},
		},
		seen: map[string]bool{},
	}
}

// targetAt returns the managed map and union of the target at path.
func targetAt(t *testing.T, b *builder, path string) apply.Target {
	t.Helper()
	for _, tg := range b.targets {
		if tg.Path == path {
			return tg
		}
	}
	t.Fatalf("no target at %s", path)
	return apply.Target{}
}

func TestRenderPiPackages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		registry  string
		packages  []string
		want      any
		wantUnion bool
	}{
		{name: "no registry, no packages"},
		{name: "no registry keeps extra packages", packages: []string{"npm:x"}, want: []any{"npm:x"}, wantUnion: true},
		{
			name: "registry and extra", registry: "acme/app", packages: []string{"npm:x"},
			want: []any{filepath.Join("/data", "registries", "acme", "app"), "npm:x"}, wantUnion: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &config.Config{}
			c.Plugins.Registry = tt.registry
			c.Harness.Pi.Packages = tt.packages
			b := newTestBuilder(c)
			if err := renderPi(c, b); err != nil {
				t.Fatal(err)
			}
			tg := targetAt(t, b, "/home/u/.pi/agent/settings.json")
			managed, _ := tg.Managed.(map[string]any)
			var got any
			if v, ok := managed["packages"]; ok {
				got = v
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("packages (-want +got):\n%s", diff)
			}
			if gotUnion := len(tg.Union) > 0; gotUnion != tt.wantUnion {
				t.Errorf("union = %v, want %v", tg.Union, tt.wantUnion)
			}
		})
	}
}

func TestRenderPiMCPKeepsCallbackPort(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.MCP.Servers = map[string]config.MCPServer{
		"crm": {URL: "https://crm.example.com/mcp", OAuth: config.OAuth{ClientID: "id", CallbackPort: 8765}},
	}
	b := newTestBuilder(c)
	if err := renderPi(c, b); err != nil {
		t.Fatal(err)
	}
	got := targetAt(t, b, "/home/u/.pi/agent/mcp.json").Managed
	want := map[string]any{"mcpServers": map[string]any{"crm": map[string]any{
		"url": "https://crm.example.com/mcp", "oauth": map[string]any{"clientId": "id", "callbackPort": 8765},
	}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mcp.json (-want +got):\n%s", diff)
	}
}
