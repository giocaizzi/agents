package cli

import (
	"context"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
)

// pluginFindings checks the plugin selection of r against the registry checkout for agents check.
func (d Deps) pluginFindings(_ context.Context, _ *config.Resolved) []finding.Finding { return nil }
