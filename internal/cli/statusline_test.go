package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/cli"
)

func TestStatusline(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(_ string) string { return "" },
	}

	code := d.Run(context.Background(), []string{"statusline", "claude"})
	if code != 0 {
		t.Fatalf("statusline exit code = %d, want 0; stderr = %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "[") || !strings.Contains(out, "]") {
		t.Errorf("statusline output should contain bracketed segments, got %q", out)
	}
}
