package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentsMD = "AGENTS.md"

func (d Deps) resolveHarnessPrompt(harness, profile, file string) (string, string, error) {
	if !isKnownHarness(harness) {
		return "", "", fmt.Errorf("unsupported harness %q (expected claude, antigravity, copilot, codex, pi, or opencode)", harness)
	}

	// 1. Workspace instructions
	if content, origin, ok := d.findWorkspacePrompt(harness); ok {
		return content, origin, nil
	}

	// 2. User home instructions
	if content, origin, ok := d.findHomePrompt(harness); ok {
		return content, origin, nil
	}

	// 3. Fallback to policy profile instructions or template layers
	if content, ok, err := d.tryPolicyProfile(file, profile, false); ok && err == nil && content != "" {
		return content, profile + " profile", nil
	}
	if content, ok, err := d.tryTemplatePrompt(profile, false); ok && err == nil && content != "" {
		return content, profile + " profile layers", nil
	}

	return "", "", fmt.Errorf("no prompt found for harness %q", harness)
}

func isKnownHarness(h string) bool {
	switch h {
	case harnessClaude, harnessAntigravity, harnessCopilot, harnessCodex, harnessPi, harnessOpencode:
		return true
	default:
		return false
	}
}

func (d Deps) findWorkspacePrompt(harness string) (string, string, bool) {
	candidates := workspacePromptCandidates(harness)
	for _, rel := range candidates {
		data, err := os.ReadFile(filepath.Clean(rel))
		if err == nil && len(data) > 0 {
			return string(data), "workspace " + rel, true
		}
	}
	return "", "", false
}

func workspacePromptCandidates(harness string) []string {
	switch harness {
	case harnessClaude:
		return []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md")}
	case harnessCopilot:
		return []string{filepath.Join(".github", "copilot-instructions.md"), ".copilot-instructions.md"}
	case harnessCodex:
		return []string{agentsMD, filepath.Join(".codex", agentsMD)}
	case harnessOpencode:
		return []string{agentsMD, filepath.Join(".config", "opencode", agentsMD)}
	case harnessPi:
		return []string{agentsMD, filepath.Join(".pi", agentsMD)}
	case harnessAntigravity:
		return []string{agentsMD, filepath.Join(".gemini", agentsMD)}
	default:
		return nil
	}
}

func (d Deps) findHomePrompt(harness string) (string, string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", "", false
	}

	candidates := homePromptCandidates(home, harness)
	for _, path := range candidates {
		data, err := os.ReadFile(filepath.Clean(path))
		if err == nil && len(data) > 0 {
			rel := path
			if strings.HasPrefix(path, home) {
				rel = "~" + path[len(home):]
			}
			return string(data), rel, true
		}
	}
	return "", "", false
}

func homePromptCandidates(home, harness string) []string {
	switch harness {
	case harnessClaude:
		return []string{filepath.Join(home, ".claude", "CLAUDE.md")}
	case harnessCopilot:
		return []string{
			filepath.Join(home, ".copilot", "copilot-instructions.md"),
			filepath.Join(home, ".config", "github-copilot", "copilot-instructions.md"),
		}
	case harnessCodex:
		return []string{filepath.Join(home, ".codex", agentsMD)}
	case harnessOpencode:
		return []string{filepath.Join(home, ".config", "opencode", agentsMD)}
	case harnessPi:
		return []string{
			filepath.Join(home, ".pi", "agent", agentsMD),
			filepath.Join(home, ".pi", agentsMD),
		}
	case harnessAntigravity:
		return []string{
			filepath.Join(home, ".gemini", "antigravity-cli", agentsMD),
			filepath.Join(home, ".gemini", "config", agentsMD),
		}
	default:
		return nil
	}
}
