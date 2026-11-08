package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/cli"
)

func TestInit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(_ string) string { return "" },
	}

	code := d.Run(context.Background(), []string{"init", "-dir", tmpDir})
	if code != 0 {
		t.Fatalf("init exit code = %d, want 0; stderr = %s", code, stderr.String())
	}

	cfgPath := filepath.Join(tmpDir, "agents.toml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("agents.toml not created: %v", err)
	}

	if !strings.Contains(string(data), "[agents]") {
		t.Errorf("agents.toml missing [agents] section, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), "[models]") {
		t.Errorf("agents.toml missing [models] section, got:\n%s", string(data))
	}
}
