package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/translate"
)

func (d Deps) runRender(_ context.Context, args []string) int {
	fs := newFlags("render")
	harnessFlag := fs.String("harness", "", "target client harness (claude, antigravity, copilot, codex, pi, opencode)")
	partFlag := fs.String("part", "", "output granularity (permissions, settings, prompt, all)")
	osFlag := fs.String("os", "", "target operating system (darwin, linux, windows)")
	fileFlag := fs.String("file", "", "path to harness-policy.toml or agents.toml")
	profileFlag := fs.String("profile", "", "optional configuration profile to apply")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(d.Stderr, "flag parse error: %v\n", err)
		return 2
	}

	if strings.ToLower(strings.TrimSpace(*partFlag)) == "prompt" {
		harness := strings.ToLower(strings.TrimSpace(*harnessFlag))
		content, _, err := d.resolveHarnessPrompt(harness, *profileFlag, *fileFlag)
		if err != nil {
			fmt.Fprintf(d.Stderr, "render error: %v\n", err)
			return 1
		}
		if _, err := d.Stdout.Write([]byte(content)); err != nil {
			fmt.Fprintf(d.Stderr, "render error: %v\n", err)
			return 1
		}
		return 0
	}

	targetOS := *osFlag
	if targetOS == "" {
		if d.GOOS != "" {
			targetOS = d.GOOS
		} else {
			targetOS = "linux"
		}
	}

	p, _, err := d.loadPolicy(*fileFlag)
	if err != nil {
		fmt.Fprintf(d.Stderr, "error loading policy: %v\n", err)
		return 1
	}
	if *profileFlag != "" {
		p = p.WithProfile(*profileFlag)
	}

	if err := d.renderHarness(p, strings.ToLower(*harnessFlag), strings.ToLower(*partFlag), targetOS); err != nil {
		fmt.Fprintf(d.Stderr, "render error: %v\n", err)
		return 1
	}
	return 0
}

func (d Deps) renderHarness(p *config.Policy, harness, part, targetOS string) error {
	var out any
	format := "json"

	switch harness {
	case harnessClaude:
		switch part {
		case "permissions":
			out = translate.ClaudePermissions(p, targetOS)
		default:
			out = translate.ClaudeSettings(p, targetOS)
		}
	case harnessAntigravity:
		switch part {
		case "permissions":
			out = translate.AntigravityPermissions(p)
		default:
			out = translate.AntigravitySettings(p)
		}
	case harnessCopilot:
		out = translate.CopilotSettings(p)
	case harnessCodex:
		out = translate.CodexConfig(p)
		format = "toml"
	case harnessPi:
		out = translate.PiSettings(p)
	case harnessOpencode:
		out = translate.OpenCodeSettings(p)
	default:
		return fmt.Errorf("unsupported harness %q (expected claude, antigravity, copilot, codex, pi, or opencode)", harness)
	}

	if format == "toml" {
		b, err := toml.Marshal(out)
		if err != nil {
			return fmt.Errorf("marshal toml: %w", err)
		}
		if _, err := d.Stdout.Write(b); err != nil {
			return fmt.Errorf("write toml: %w", err)
		}
		return nil
	}

	enc := json.NewEncoder(d.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
