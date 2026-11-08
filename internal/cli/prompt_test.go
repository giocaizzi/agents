package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/cli"
	"github.com/giocaizzi/agents/internal/config"
)

func TestPromptWithPolicyProfiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Write prompt layer files
	coreFile := filepath.Join(tmpDir, "core.md")
	if err := os.WriteFile(coreFile, []byte("# Core Rules\nBe precise."), 0o600); err != nil {
		t.Fatal(err)
	}
	devFile := filepath.Join(tmpDir, "dev.md")
	if err := os.WriteFile(devFile, []byte("# Dev Mode\nUse fast iterations."), 0o600); err != nil {
		t.Fatal(err)
	}

	// Write agents.toml with profiles
	agentsToml := filepath.Join(tmpDir, "agents.toml")
	tomlContent := `
[models.tiers.driver]
model = "claude-3-7-sonnet"
effort = "high"

[profiles.default]
instructions = ["core.md"]

[profiles.dev]
instructions = ["core.md", "dev.md"]

[profiles.dev.models.tiers.driver]
model = "claude-3-5-haiku"
effort = "low"
`
	if err := os.WriteFile(agentsToml, []byte(tomlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	// Test default profile prompt assembly
	code := d.Run(context.Background(), []string{"prompt", "-file", agentsToml, "-profile", "default"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "# Core Rules") {
		t.Errorf("stdout lacks core rules: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "# Dev Mode") {
		t.Errorf("default profile unexpectedly included dev rules: %s", stdout.String())
	}

	// Test dev profile prompt assembly
	stdout.Reset()
	stderr.Reset()
	code = d.Run(context.Background(), []string{"prompt", "-file", agentsToml, "-profile", "dev"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "# Core Rules") || !strings.Contains(stdout.String(), "# Dev Mode") {
		t.Errorf("dev profile output incomplete: %s", stdout.String())
	}

	// Test check mode on dev profile
	stdout.Reset()
	stderr.Reset()
	code = d.Run(context.Background(), []string{"prompt", "-file", agentsToml, "-profile", "dev", "-check"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "✓ prompt (dev profile: 2 layer(s) present)") {
		t.Errorf("unexpected check output: %s", stdout.String())
	}

	// Test check with profile override
	stdout.Reset()
	stderr.Reset()
	code = d.Run(context.Background(), []string{"check", "-file", agentsToml, "-profile", "dev"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr.String())
	}
}

func TestPolicyWithProfile(t *testing.T) {
	t.Parallel()
	p := config.Policy{
		Models: config.Models{
			Tiers: map[string]config.ModelTier{
				"driver": {Model: "base-driver", Effort: "high"},
			},
		},
		Profiles: map[string]config.Profile{
			"work": {
				Models: &config.Models{
					Tiers: map[string]config.ModelTier{
						"driver": {Model: "work-driver", Effort: "low"},
					},
				},
				Permissions: &config.Permissions{
					AllowCommands: []string{"npm test"},
				},
			},
		},
	}

	applied := p.WithProfile("work")
	if applied.Models.Tiers["driver"].Model != "work-driver" {
		t.Errorf("model = %q, want work-driver", applied.Models.Tiers["driver"].Model)
	}
	if len(applied.Permissions.AllowCommands) != 1 || applied.Permissions.AllowCommands[0] != "npm test" {
		t.Errorf("unexpected allowCommands: %v", applied.Permissions.AllowCommands)
	}

	// Unchanged base
	if p.Models.Tiers["driver"].Model != "base-driver" {
		t.Errorf("base policy was mutated")
	}
}
