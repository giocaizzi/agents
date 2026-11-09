package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChezmoiSourcePathsFallback_HomeSubdir(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	chezmoiHome := filepath.Join(tmpHome, ".local", "share", "chezmoi", "home")
	chezmoiData := filepath.Join(chezmoiHome, ".chezmoidata")
	if err := os.MkdirAll(chezmoiData, 0o755); err != nil {
		t.Fatal(err)
	}

	policyContent := `
[models.tiers.driver]
model = "claude-3-7-sonnet"
effort = "high"
`
	if err := os.WriteFile(filepath.Join(chezmoiData, "agents.toml"), []byte(policyContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chezmoiData, "harness.toml"), []byte("[harnessPlugins]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := Deps{
		Getenv: func(k string) string {
			if k == "HOME" {
				return tmpHome
			}
			return ""
		},
	}

	pol, polPath, err := d.loadPolicy("")
	if err != nil {
		t.Fatalf("loadPolicy failed: %v", err)
	}
	if pol == nil || polPath != filepath.Join(chezmoiData, "agents.toml") {
		t.Errorf("loadPolicy path = %q, want %q", polPath, filepath.Join(chezmoiData, "agents.toml"))
	}

	harnessPath, err := d.findHarnessToml("")
	if err != nil {
		t.Fatalf("findHarnessToml failed: %v", err)
	}
	if harnessPath != filepath.Join(chezmoiData, "harness.toml") {
		t.Errorf("findHarnessToml path = %q, want %q", harnessPath, filepath.Join(chezmoiData, "harness.toml"))
	}

	srcDir, err := d.findPolicySourceDir()
	if err != nil {
		t.Fatalf("findPolicySourceDir failed: %v", err)
	}
	if srcDir != chezmoiHome {
		t.Errorf("findPolicySourceDir = %q, want %q", srcDir, chezmoiHome)
	}
}

func TestChezmoiSourcePathsFallback_Root(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	chezmoiRoot := filepath.Join(tmpHome, ".local", "share", "chezmoi")
	chezmoiData := filepath.Join(chezmoiRoot, ".chezmoidata")
	if err := os.MkdirAll(chezmoiData, 0o755); err != nil {
		t.Fatal(err)
	}

	policyContent := `
[models.tiers.driver]
model = "claude-3-7-sonnet"
effort = "high"
`
	if err := os.WriteFile(filepath.Join(chezmoiData, "harness-policy.toml"), []byte(policyContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chezmoiData, "harness.toml"), []byte("[harnessPlugins]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := Deps{
		Getenv: func(k string) string {
			if k == "HOME" {
				return tmpHome
			}
			return ""
		},
	}

	pol, polPath, err := d.loadPolicy("")
	if err != nil {
		t.Fatalf("loadPolicy failed: %v", err)
	}
	if pol == nil || polPath != filepath.Join(chezmoiData, "harness-policy.toml") {
		t.Errorf("loadPolicy path = %q, want %q", polPath, filepath.Join(chezmoiData, "harness-policy.toml"))
	}

	harnessPath, err := d.findHarnessToml("")
	if err != nil {
		t.Fatalf("findHarnessToml failed: %v", err)
	}
	if harnessPath != filepath.Join(chezmoiData, "harness.toml") {
		t.Errorf("findHarnessToml path = %q, want %q", harnessPath, filepath.Join(chezmoiData, "harness.toml"))
	}

	srcDir, err := d.findPolicySourceDir()
	if err != nil {
		t.Fatalf("findPolicySourceDir failed: %v", err)
	}
	if srcDir != chezmoiRoot {
		t.Errorf("findPolicySourceDir = %q, want %q", srcDir, chezmoiRoot)
	}
}
