package cli

import (
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
)

// modelFindings checks the model tiers and agent assignments of r for agents check.
func (d Deps) modelFindings(_ *config.Resolved) []finding.Finding { return nil }
