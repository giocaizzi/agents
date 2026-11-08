package translate

import (
	"github.com/giocaizzi/agents/internal/config"
)

// PiSettings returns the managed subset of Pi settings.
func PiSettings(p *config.Policy) map[string]any {
	provider := p.Native.Pi.DefaultProvider
	if provider == "" {
		provider = "openai"
	}
	model := p.Native.Pi.DefaultModel
	if model == "" {
		model = "gpt-5.6-terra"
	}
	thinking := p.Native.Pi.DefaultThinkingLevel
	if thinking == "" {
		thinking = "medium"
	}

	pkgs := p.Native.Pi.Packages
	if pkgs == nil {
		pkgs = []string{}
	}

	return map[string]any{
		"defaultProvider":      provider,
		"defaultModel":         model,
		"defaultThinkingLevel": thinking,
		"packages":             pkgs,
	}
}
