package harness

import (
	"path/filepath"
	"testing"

	"github.com/giocaizzi/agents/internal/config"
)

// TestNoServersNoMCPTargets: without MCP servers no harness emits its MCP
// file, so an empty mcpServers object is never managed content.
func TestNoServersNoMCPTargets(t *testing.T) {
	t.Parallel()
	skip := map[string]bool{"mcp-config.json": true, ".claude.json": true, "mcp_config.json": true, "mcp.json": true}
	for _, h := range []config.Harness{config.Claude, config.Copilot, config.VSCode, config.Pi, config.Antigravity} {
		t.Run(string(h), func(t *testing.T) {
			t.Parallel()
			plan := renderFor(t, config.Config{}, "linux", h)
			for _, tg := range plan.Targets {
				if skip[filepath.Base(tg.Path)] {
					t.Errorf("%s emitted without servers: %v", tg.Path, tg.Managed)
				}
			}
		})
	}
}
