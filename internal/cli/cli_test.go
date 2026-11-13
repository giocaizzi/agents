package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/cli"
)

func TestCLIRun_Help(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: hermeticGetenv(t),
	}

	code := d.Run(context.Background(), []string{"help"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "agents") || !strings.Contains(stdout.String(), "USAGE") {
		t.Errorf("expected usage output, got %q", stdout.String())
	}
}

func TestCLIRun_Version(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	d := cli.Deps{
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  &stderr,
		Getenv:  hermeticGetenv(t),
		Version: "0.2.0",
	}

	code := d.Run(context.Background(), []string{"version"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "agents 0.2.0") {
		t.Errorf("expected version output, got %q", stdout.String())
	}
}

func TestCLIRun_NilGetenv(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	d := cli.Deps{Stdout: &stdout, Stderr: &stderr}

	if code := d.Run(context.Background(), []string{"version"}); code != 2 {
		t.Fatalf("expected code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Getenv is nil") || stdout.Len() != 0 {
		t.Errorf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
